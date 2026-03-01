"""
Hive Orchestrator
─────────────────
POST /chat         {"message": "...", "files": {...}}
POST /chat/stream  Same, with Server-Sent Events for live progress.
GET  /config       Show current config.
POST /config       Update config values.
GET  /agents       List available agent roles.
POST /model        Set the LLM model.

Phase 1 — LLM routing: decide which specialist agents to invoke.
Phase 2 — Fan-out: create ephemeral K8s Jobs for each agent.
Phase 3 — LLM synthesis: merge all agent responses into one answer.
"""
import asyncio
import json
import logging
import os
import re
import uuid
from typing import Any, Optional

from dotenv import load_dotenv
from fastapi import FastAPI, HTTPException
from fastapi.middleware.cors import CORSMiddleware
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

K8S_NAMESPACE: str = os.getenv("K8S_NAMESPACE", "hive")
AGENT_IMAGE: str = os.getenv("AGENT_IMAGE", "agent-image:latest")

VALID_AGENTS: list[str] = [
    "code_reviewer",
    "planner",
    "researcher",
    "debugger",
    "architect",
]

# ── K8s client (loaded lazily — falls back to httpx for local dev) ─────────────
_k8s_available = False
try:
    from kubernetes import client as k8s_client, config as k8s_config
    try:
        k8s_config.load_incluster_config()
        _k8s_available = True
        log.info("K8s in-cluster config loaded — agents will run as Jobs.")
    except k8s_config.ConfigException:
        try:
            k8s_config.load_kube_config()
            _k8s_available = True
            log.info("K8s kube-config loaded — agents will run as Jobs.")
        except Exception:
            log.info("K8s config not available — falling back to httpx agent calls.")
except ImportError:
    log.info("kubernetes package not installed — falling back to httpx agent calls.")

if _k8s_available:
    _batch_v1 = k8s_client.BatchV1Api()
    _core_v1 = k8s_client.CoreV1Api()

# ── Fallback: httpx agent URLs for local dev without K8s Jobs ──────────────────
import httpx

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
    version="0.2.0",
)
app.add_middleware(CORSMiddleware, allow_origins=["*"], allow_methods=["*"], allow_headers=["*"])

# ── Schemas ────────────────────────────────────────────────────────────────────
class ChatRequest(BaseModel):
    message: str
    files: Optional[dict[str, str]] = None


class ChatResponse(BaseModel):
    result: str
    agents_used: list[str]
    agent_results: dict[str, str]


class ModelRequest(BaseModel):
    model: str


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
            temperature=0.0,
        )
        raw = resp.choices[0].message.content or "[]"
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

# -- K8s Job-based agents --

def _build_job(role: str, task_json: str) -> "k8s_client.V1Job":
    """Build a K8s Job spec for an ephemeral agent."""
    job_id = f"hive-{role.replace('_', '-')}-{uuid.uuid4().hex[:8]}"

    env_vars = [
        k8s_client.V1EnvVar(name="AGENT_ROLE", value=role),
        k8s_client.V1EnvVar(name="TASK_JSON", value=task_json),
        k8s_client.V1EnvVar(
            name="LLM_API_KEY",
            value_from=k8s_client.V1EnvVarSource(
                secret_key_ref=k8s_client.V1SecretKeySelector(
                    name="hive-secret", key="LLM_API_KEY"
                )
            ),
        ),
    ]
    env_from = [
        k8s_client.V1EnvFromSource(
            config_map_ref=k8s_client.V1ConfigMapEnvSource(name="hive-config")
        )
    ]
    container = k8s_client.V1Container(
        name="agent",
        image=AGENT_IMAGE,
        image_pull_policy="Never",
        env=env_vars,
        env_from=env_from,
        resources=k8s_client.V1ResourceRequirements(
            requests={"cpu": "100m", "memory": "128Mi"},
            limits={"cpu": "500m", "memory": "256Mi"},
        ),
    )
    template = k8s_client.V1PodTemplateSpec(
        metadata=k8s_client.V1ObjectMeta(
            labels={"app": "hive-agent", "hive/role": role, "hive/job": job_id}
        ),
        spec=k8s_client.V1PodSpec(
            containers=[container],
            restart_policy="Never",
        ),
    )
    return k8s_client.V1Job(
        api_version="batch/v1",
        kind="Job",
        metadata=k8s_client.V1ObjectMeta(name=job_id, namespace=K8S_NAMESPACE),
        spec=k8s_client.V1JobSpec(
            template=template,
            backoff_limit=0,
            ttl_seconds_after_finished=120,
        ),
    )


async def _wait_for_job(job_name: str, role: str) -> str:
    """Poll until the Job completes, then read pod logs."""
    timeout = int(AGENT_TIMEOUT)
    poll_interval = 2
    elapsed = 0

    while elapsed < timeout:
        job_status = await asyncio.to_thread(
            _batch_v1.read_namespaced_job_status, name=job_name, namespace=K8S_NAMESPACE
        )
        if job_status.status.succeeded and job_status.status.succeeded > 0:
            return await _read_job_logs(job_name, role)
        if job_status.status.failed and job_status.status.failed > 0:
            logs = await _read_job_logs(job_name, role)
            return f"[Agent '{role}' failed]\n{logs}"
        await asyncio.sleep(poll_interval)
        elapsed += poll_interval

    return f"[Agent '{role}' timed out after {timeout}s]"


async def _read_job_logs(job_name: str, role: str) -> str:
    """Read logs from the Job's pod, parse JSON result."""
    try:
        pods = await asyncio.to_thread(
            _core_v1.list_namespaced_pod,
            namespace=K8S_NAMESPACE,
            label_selector=f"hive/job={job_name}",
        )
        if not pods.items:
            return "[No pod found for job]"

        pod_name = pods.items[0].metadata.name
        logs = await asyncio.to_thread(
            _core_v1.read_namespaced_pod_log,
            name=pod_name,
            namespace=K8S_NAMESPACE,
        )
        # Last JSON line contains the result
        for line in reversed(logs.strip().split("\n")):
            try:
                data = json.loads(line)
                if "result" in data:
                    return data["result"]
            except json.JSONDecodeError:
                continue
        return logs
    except Exception as exc:
        log.error("Failed to read logs for Job %s: %s", job_name, exc)
        return f"[Failed to read agent logs: {exc}]"


async def call_agent_job(
    role: str, message: str, files: dict[str, str] | None = None
) -> tuple[str, str]:
    """Create a K8s Job for the agent, wait for completion, return result."""
    task_data: dict[str, Any] = {"description": message}
    if files:
        task_data["files"] = files
    task_json = json.dumps(task_data)

    job = _build_job(role, task_json)
    try:
        created = await asyncio.to_thread(
            _batch_v1.create_namespaced_job, namespace=K8S_NAMESPACE, body=job
        )
        job_name = created.metadata.name
        log.info("Created Job %s for agent '%s'", job_name, role)
        result = await _wait_for_job(job_name, role)
        # Clean up (TTL also handles it)
        try:
            await asyncio.to_thread(
                _batch_v1.delete_namespaced_job,
                name=job_name,
                namespace=K8S_NAMESPACE,
                body=k8s_client.V1DeleteOptions(propagation_policy="Background"),
            )
        except Exception:
            pass
        return role, result
    except Exception as exc:
        log.error("Agent '%s' Job failed: %s", role, exc)
        return role, f"[Agent '{role}' failed: {exc}]"


# -- Fallback: httpx-based agent calls (persistent deployments) --

async def call_agent_http(
    client: httpx.AsyncClient,
    role: str,
    message: str,
    files: dict[str, str] | None = None,
) -> tuple[str, str]:
    """Call a persistent agent pod via HTTP. Never raises."""
    url = f"{AGENT_URLS[role]}/task"
    payload: dict[str, Any] = {"description": message}
    if files:
        payload["files"] = files
    try:
        r = await client.post(url, json=payload, timeout=AGENT_TIMEOUT)
        r.raise_for_status()
        return role, r.json().get("result", "")
    except Exception as exc:
        log.error("Agent '%s' failed: %s", role, exc)
        return role, f"[Agent '{role}' failed: {exc}]"


async def fan_out(
    agents: list[str], message: str, files: dict[str, str] | None = None
) -> dict[str, str]:
    """Call all selected agents in parallel (Jobs if K8s available, else httpx)."""
    if _k8s_available:
        tasks = [call_agent_job(role, message, files) for role in agents]
        results = await asyncio.gather(*tasks)
        return dict(results)
    else:
        async with httpx.AsyncClient() as client:
            tasks = [call_agent_http(client, role, message, files) for role in agents]
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
6. If files were provided, reference them in your answer.
7. If suggesting code changes to existing files, wrap each file in a ~~~file:path block:
   ~~~file:path/to/file
   full updated file content
   ~~~
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
        "jobs_enabled": _k8s_available,
    }


@app.get("/config", tags=["config"])
async def get_config() -> dict[str, str]:
    """Return current orchestrator configuration."""
    return {
        "model": LLM_MODEL,
        "temperature": str(TEMPERATURE),
        "max_tokens": str(MAX_TOKENS),
        "agent_timeout": str(AGENT_TIMEOUT),
        "llm_base_url": LLM_BASE_URL,
        "jobs_enabled": str(_k8s_available),
    }


@app.post("/config", tags=["config"])
async def update_config(payload: dict[str, str]) -> dict[str, str]:
    """Update configuration values at runtime."""
    global LLM_MODEL, TEMPERATURE, MAX_TOKENS, AGENT_TIMEOUT
    updated = {}
    if "model" in payload:
        LLM_MODEL = payload["model"]
        updated["model"] = LLM_MODEL
    if "temperature" in payload:
        TEMPERATURE = float(payload["temperature"])
        updated["temperature"] = str(TEMPERATURE)
    if "max_tokens" in payload:
        MAX_TOKENS = int(payload["max_tokens"])
        updated["max_tokens"] = str(MAX_TOKENS)
    if "agent_timeout" in payload:
        AGENT_TIMEOUT = float(payload["agent_timeout"])
        updated["agent_timeout"] = str(AGENT_TIMEOUT)
    if not updated:
        raise HTTPException(status_code=400, detail="No recognised config keys in payload.")
    log.info("Config updated: %s", updated)
    return updated


@app.get("/agents", tags=["config"])
async def list_agents() -> dict[str, list[str]]:
    """Return the list of available agent roles."""
    return {"agents": VALID_AGENTS}


@app.get("/model", tags=["config"])
async def get_model() -> dict[str, str]:
    return {"model": LLM_MODEL}


@app.post("/model", tags=["config"])
async def set_model(request: ModelRequest) -> dict[str, str]:
    global LLM_MODEL
    old = LLM_MODEL
    LLM_MODEL = request.model
    log.info("Model changed: %s → %s", old, LLM_MODEL)
    return {"previous": old, "current": LLM_MODEL}


@app.post("/chat", response_model=ChatResponse, tags=["orchestrator"])
async def chat(request: ChatRequest) -> ChatResponse:
    """Synchronous chat endpoint."""
    log.info("Received: %s", request.message[:120])
    agents = await route(request.message)
    agent_results = await fan_out(agents, request.message, request.files)
    final = await synthesise(request.message, agent_results)
    return ChatResponse(result=final, agents_used=agents, agent_results=agent_results)


# ── SSE helpers ────────────────────────────────────────────────────────────────
def _sse(event: str, data: dict) -> str:
    return f"event: {event}\ndata: {json.dumps(data)}\n\n"


@app.post("/chat/stream", tags=["orchestrator"])
async def chat_stream(request: ChatRequest):
    """Streaming endpoint with Server-Sent Events."""
    log.info("Stream request: %s", request.message[:120])

    async def _generate():
        yield _sse("routing_start", {"message": request.message[:120]})
        agents = await route(request.message)
        yield _sse("routing_complete", {"agents": agents})

        for role in agents:
            yield _sse("agent_start", {"agent": role})

        agent_results: dict[str, str] = {}
        queue: asyncio.Queue[tuple[str, str]] = asyncio.Queue()

        if _k8s_available:
            async def _tracked_job(role: str):
                pair = await call_agent_job(role, request.message, request.files)
                await queue.put(pair)
            tasks = [asyncio.create_task(_tracked_job(role)) for role in agents]
        else:
            _http_client = httpx.AsyncClient()
            async def _tracked_http(role: str):
                pair = await call_agent_http(_http_client, role, request.message, request.files)
                await queue.put(pair)
            tasks = [asyncio.create_task(_tracked_http(role)) for role in agents]

        for _ in range(len(agents)):
            role, result = await queue.get()
            agent_results[role] = result
            yield _sse("agent_complete", {"agent": role, "preview": result[:300]})
        await asyncio.gather(*tasks, return_exceptions=True)

        if not _k8s_available:
            await _http_client.aclose()

        yield _sse("synthesis_start", {"agents_used": list(agent_results.keys())})
        final = await synthesise(request.message, agent_results)
        yield _sse("synthesis_complete", {})
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
