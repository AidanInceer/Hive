"""
Hive Orchestrator
─────────────────
POST /chat  {"message": "..."}
  Phase 1 — LLM routing: decide which specialist agents to invoke.
  Phase 2 — Fan-out: call selected agents in parallel via HTTP.
  Phase 3 — LLM synthesis: merge all agent responses into one answer.
  Returns   {"result": "...", "agents_used": [...], "agent_results": {...}}
"""
import asyncio
import json
import logging
import os
import re
from typing import Any

import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException
from fastapi.responses import StreamingResponse
from openai import AsyncOpenAI
from pydantic import BaseModel

load_dotenv()

# ── Logging ────────────────────────────────────────────────────────────────────
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s — %(message)s",
)
log = logging.getLogger("orchestrator")

# ── Configuration ──────────────────────────────────────────────────────────────
LLM_API_KEY: str = os.getenv("LLM_API_KEY", "ollama")
LLM_BASE_URL: str = os.getenv("LLM_BASE_URL", "http://host.docker.internal:11434/v1")
LLM_MODEL: str = os.getenv("LLM_MODEL", "qwen3-coder-next:latest")
MAX_TOKENS: int = int(os.getenv("MAX_TOKENS", "4096"))
TEMPERATURE: float = float(os.getenv("TEMPERATURE", "0.6"))
AGENT_TIMEOUT: float = float(os.getenv("AGENT_TIMEOUT", "120"))

# ── Agent registry ─────────────────────────────────────────────────────────────
# Maps role name → in-cluster URL.  Override via AGENT_<ROLE>_URL env vars.
_DEFAULT_AGENT_URLS: dict[str, str] = {
    "code_reviewer": "http://agent-code-reviewer.hive.svc.cluster.local:8000",
    "planner":       "http://agent-planner.hive.svc.cluster.local:8000",
    "researcher":    "http://agent-researcher.hive.svc.cluster.local:8000",
    "debugger":      "http://agent-debugger.hive.svc.cluster.local:8000",
    "architect":     "http://agent-architect.hive.svc.cluster.local:8000",
}

AGENT_URLS: dict[str, str] = {
    role: os.getenv(f"AGENT_{role.upper()}_URL", url)
    for role, url in _DEFAULT_AGENT_URLS.items()
}

VALID_AGENTS = list(AGENT_URLS.keys())

# ── LLM client ─────────────────────────────────────────────────────────────────
llm = AsyncOpenAI(
    api_key=LLM_API_KEY or "ollama",
    base_url=LLM_BASE_URL,
)

# ── FastAPI ────────────────────────────────────────────────────────────────────
app = FastAPI(
    title="Hive Orchestrator",
    description=(
        "Receives user tasks, routes them to specialist sub-agents running in "
        "Kubernetes, and synthesises the results into a single coherent answer."
    ),
    version="0.1.0",
)

# ── Schemas ────────────────────────────────────────────────────────────────────
class ChatRequest(BaseModel):
    message: str


class ChatResponse(BaseModel):
    result: str
    agents_used: list[str]
    agent_results: dict[str, str]


# ── Phase 1: routing ───────────────────────────────────────────────────────────
_ROUTING_SYSTEM = f"""You are a task router for a multi-agent AI system called Hive.
Available agents: {json.dumps(VALID_AGENTS)}

Given a user task, decide which agents should handle it.
- Pick only the agents that are genuinely useful for this specific task.
- You may pick 1 to {len(VALID_AGENTS)} agents.
- Return ONLY a valid JSON array of agent names from the list above. No explanation.

Examples:
  Task: "Review my Python function" → ["code_reviewer"]
  Task: "Build a REST API for a todo app" → ["planner","architect","code_reviewer"]
  Task: "Why is my app crashing?" → ["debugger","researcher"]
"""

async def route(message: str) -> list[str]:
    """Ask the LLM which agents to invoke. Falls back to all agents on error."""
    try:
        resp = await llm.chat.completions.create(
            model=LLM_MODEL,
            messages=[
                {"role": "system", "content": _ROUTING_SYSTEM},
                {"role": "user",   "content": message},
            ],
            max_tokens=128,
            temperature=0.0,   # deterministic routing
        )
        raw = resp.choices[0].message.content or "[]"
        # Strip markdown code fences if present
        raw = re.sub(r"```[a-z]*\n?", "", raw).strip().rstrip("`").strip()
        agents: list[str] = json.loads(raw)
        valid = [a for a in agents if a in VALID_AGENTS]
        if not valid:
            raise ValueError(f"No valid agents in response: {agents}")
        log.info("Routing → %s", valid)
        return valid
    except Exception as exc:
        log.warning("Routing failed (%s) — falling back to all agents.", exc)
        return VALID_AGENTS


# ── Phase 2: fan-out ───────────────────────────────────────────────────────────
async def call_agent(
    client: httpx.AsyncClient,
    role: str,
    message: str,
) -> tuple[str, str]:
    """Call a single agent and return (role, result). Never raises."""
    url = f"{AGENT_URLS[role]}/task"
    try:
        r = await client.post(
            url,
            json={"description": message},
            timeout=AGENT_TIMEOUT,
        )
        r.raise_for_status()
        return role, r.json().get("result", "")
    except Exception as exc:
        log.error("Agent '%s' failed: %s", role, exc)
        return role, f"[Agent '{role}' failed: {exc}]"


async def fan_out(agents: list[str], message: str) -> dict[str, str]:
    """Call all selected agents in parallel."""
    async with httpx.AsyncClient() as client:
        tasks = [call_agent(client, role, message) for role in agents]
        results = await asyncio.gather(*tasks)
    return dict(results)


# ── Phase 3: synthesis ─────────────────────────────────────────────────────────
_SYNTHESIS_SYSTEM = """You are the final synthesiser for Hive, a multi-agent AI system.
You receive a user's original task and the individual responses from specialist agents.
Your job:
1. Merge all agent responses into one coherent, well-structured answer.
2. Resolve any contradictions by applying sound judgement.
3. Do not repeat boilerplate from each agent — distil the most important insights.
4. Use clear headings if the answer has distinct sections.
5. Be concise but complete.
"""

async def synthesise(message: str, agent_results: dict[str, str]) -> str:
    """Merge agent responses into a single answer via LLM."""
    agent_section = "\n\n".join(
        f"### {role.replace('_', ' ').title()} response:\n{result}"
        for role, result in agent_results.items()
    )
    user_content = (
        f"Original user task:\n{message}\n\n"
        f"Agent responses:\n{agent_section}"
    )
    try:
        resp = await llm.chat.completions.create(
            model=LLM_MODEL,
            messages=[
                {"role": "system", "content": _SYNTHESIS_SYSTEM},
                {"role": "user",   "content": user_content},
            ],
            max_tokens=MAX_TOKENS,
            temperature=TEMPERATURE,
        )
        return resp.choices[0].message.content or ""
    except Exception as exc:
        log.exception("Synthesis failed: %s", exc)
        raise HTTPException(status_code=502, detail=f"Synthesis error: {exc}") from exc


# ── Routes ─────────────────────────────────────────────────────────────────────
@app.get("/", tags=["health"])
async def health() -> dict[str, Any]:
    return {
        "status": "ok",
        "model": LLM_MODEL,
        "agents": VALID_AGENTS,
    }


@app.post("/chat", response_model=ChatResponse, tags=["orchestrator"])
async def chat(request: ChatRequest) -> ChatResponse:
    """
    Main entry point. Accepts a natural language message and returns a
    synthesised answer produced by the relevant specialist agents.
    """
    log.info("Received: %s", request.message[:120])

    # Phase 1 — route
    agents = await route(request.message)

    # Phase 2 — fan-out
    agent_results = await fan_out(agents, request.message)

    # Phase 3 — synthesise
    final = await synthesise(request.message, agent_results)

    return ChatResponse(
        result=final,
        agents_used=agents,
        agent_results=agent_results,
    )


# ── SSE helpers ────────────────────────────────────────────────────────────────
def _sse(event: str, data: dict) -> str:
    """Format a single Server-Sent Event frame."""
    return f"event: {event}\ndata: {json.dumps(data)}\n\n"


@app.post("/chat/stream", tags=["orchestrator"])
async def chat_stream(request: ChatRequest):
    """
    Streaming endpoint.  Returns Server-Sent Events so the CLI can show
    real-time progress through routing, fan-out, and synthesis phases.
    """
    log.info("Stream request: %s", request.message[:120])

    async def _generate():
        # Phase 1 -- route
        yield _sse("routing_start", {"message": request.message[:120]})
        agents = await route(request.message)
        yield _sse("routing_complete", {"agents": agents})

        # Phase 2 -- fan-out with per-agent events
        for role in agents:
            yield _sse("agent_start", {"agent": role})

        agent_results: dict[str, str] = {}
        queue: asyncio.Queue[tuple[str, str]] = asyncio.Queue()

        async def _tracked(client: httpx.AsyncClient, role: str):
            pair = await call_agent(client, role, request.message)
            await queue.put(pair)

        async with httpx.AsyncClient() as client:
            tasks = [
                asyncio.create_task(_tracked(client, role))
                for role in agents
            ]
            for _ in range(len(agents)):
                role, result = await queue.get()
                agent_results[role] = result
                yield _sse("agent_complete", {
                    "agent": role,
                    "preview": result[:300],
                })
            await asyncio.gather(*tasks, return_exceptions=True)

        # Phase 3 -- synthesis
        yield _sse("synthesis_start", {
            "agents_used": list(agent_results.keys()),
        })
        final = await synthesise(request.message, agent_results)
        yield _sse("synthesis_complete", {})

        # Final payload
        yield _sse("done", {
            "result": final,
            "agents_used": list(agent_results.keys()),
            "agent_results": agent_results,
        })

    return StreamingResponse(
        _generate(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )


# ── Entry-point ────────────────────────────────────────────────────────────────
if __name__ == "__main__":
    import uvicorn
    uvicorn.run("orchestrator:app", host="0.0.0.0", port=9000, reload=False)
