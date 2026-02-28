import os
import logging
from typing import Optional

from fastapi import FastAPI, HTTPException
from fastapi.responses import JSONResponse
from pydantic import BaseModel
from openai import AsyncOpenAI
from dotenv import load_dotenv

load_dotenv()

# ── Logging ────────────────────────────────────────────────────────────────────
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s — %(message)s",
)
log = logging.getLogger("agent")

# ── Configuration ──────────────────────────────────────────────────────────────
AGENT_ROLE: str = os.getenv("AGENT_ROLE", "default").lower()
LLM_API_KEY: str = os.getenv("LLM_API_KEY", "ollama")   # Ollama ignores this; set for remote APIs
LLM_BASE_URL: str = os.getenv("LLM_BASE_URL", "http://host.docker.internal:11434/v1")
LLM_MODEL: str = os.getenv("LLM_MODEL", "qwen3-coder-next:latest")
MAX_TOKENS: int = int(os.getenv("MAX_TOKENS", "4096"))
TEMPERATURE: float = float(os.getenv("TEMPERATURE", "0.6"))

# ── Role → system-prompt mapping ───────────────────────────────────────────────
ROLE_PROMPTS: dict[str, str] = {
    "code_reviewer": (
        "You are an expert code reviewer with deep knowledge of software engineering "
        "best practices, design patterns, security vulnerabilities, and performance "
        "optimisation. When given code or a description of code, you: (1) identify bugs, "
        "logic errors, and edge-cases; (2) flag security risks such as injections, "
        "insecure dependencies, and improper error handling; (3) suggest idiomatic "
        "refactors; (4) comment on readability and maintainability. Structure your "
        "response with clear sections: Summary, Issues Found, Recommendations."
    ),
    "planner": (
        "You are a senior technical project planner. Given a high-level goal or feature "
        "request, you decompose it into a concrete, ordered list of subtasks with clear "
        "acceptance criteria, estimated complexity (S/M/L), and any dependencies between "
        "tasks. Always consider edge-cases, testing, and documentation as explicit steps."
    ),
    "researcher": (
        "You are a technical researcher who synthesises information from your training "
        "data to produce clear, well-structured answers. When given a research question, "
        "you provide: an executive summary, key findings, trade-offs or alternatives, "
        "and references to relevant concepts, tools, or techniques."
    ),
    "debugger": (
        "You are an expert debugger. Given a description of a bug, error output, or "
        "broken code, you systematically diagnose the root cause, explain why it happens, "
        "propose a minimal targeted fix, and describe how to verify the fix works. Think "
        "step-by-step and show your reasoning."
    ),
    "architect": (
        "You are a senior software architect. You design scalable, maintainable systems. "
        "Given a problem, you propose high-level architecture diagrams (described in text "
        "or Mermaid notation), choose appropriate technologies, explain the rationale, and "
        "call out risks and alternative approaches."
    ),
    "default": (
        "You are a helpful, precise, and concise AI assistant integrated into a multi-agent "
        "system called Hive. Answer the given task accurately and thoroughly."
    ),
}


def get_system_prompt(role: str) -> str:
    prompt = ROLE_PROMPTS.get(role)
    if prompt is None:
        log.warning("Unknown AGENT_ROLE '%s' — falling back to 'default'.", role)
        prompt = ROLE_PROMPTS["default"]
    return prompt


# ── OpenAI-compatible client (works with Ollama, Kimi K2, or any OpenAI-compatible API) ──
client = AsyncOpenAI(
    api_key=LLM_API_KEY or "ollama",
    base_url=LLM_BASE_URL,
)

# ── FastAPI app ────────────────────────────────────────────────────────────────
app = FastAPI(
    title="Hive Agent",
    description="A single Hive agent exposing a /task endpoint. Supports Ollama (local) or any OpenAI-compatible API.",
    version="0.1.0",
)


# ── Schemas ────────────────────────────────────────────────────────────────────
class TaskRequest(BaseModel):
    description: str
    context: Optional[str] = None   # optional extra context / prior results


class TaskResponse(BaseModel):
    role: str
    model: str
    result: str


# ── Routes ─────────────────────────────────────────────────────────────────────
@app.get("/", tags=["health"])
async def health():
    """Liveness probe — returns agent role and model."""
    return {
        "status": "ok",
        "role": AGENT_ROLE,
        "model": LLM_MODEL,
        "base_url": LLM_BASE_URL,
    }


@app.post("/task", response_model=TaskResponse, tags=["agent"])
async def handle_task(request: TaskRequest):
    """
    Receive a task and return the agent's result.

    Body:
        - **description** *(required)*: What the agent should do.
        - **context** *(optional)*: Additional background or prior agent output.
    """
    system_prompt = get_system_prompt(AGENT_ROLE)

    # Build the user message, optionally prepending context
    user_content = request.description
    if request.context:
        user_content = f"Context:\n{request.context}\n\nTask:\n{request.description}"

    log.info("Role=%s | Task=%s", AGENT_ROLE, request.description[:120])

    try:
        response = await client.chat.completions.create(
            model=LLM_MODEL,
            messages=[
                {"role": "system", "content": system_prompt},
                {"role": "user",   "content": user_content},
            ],
            max_tokens=MAX_TOKENS,
            temperature=TEMPERATURE,
        )
    except Exception as exc:
        log.exception("LLM API call failed: %s", exc)
        raise HTTPException(status_code=502, detail=f"Model API error: {exc}") from exc

    result_text = response.choices[0].message.content or ""
    log.info("Role=%s | Response tokens=%s", AGENT_ROLE, response.usage.completion_tokens)

    return TaskResponse(
        role=AGENT_ROLE,
        model=response.model,
        result=result_text,
    )


# ── Entry-point (uvicorn) ──────────────────────────────────────────────────────
if __name__ == "__main__":
    import uvicorn
    uvicorn.run("agent:app", host="0.0.0.0", port=8000, reload=False)
