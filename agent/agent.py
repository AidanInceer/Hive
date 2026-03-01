"""
Hive Agent
──────────
Dual-mode agent that can run as:
  1. HTTP server (default): FastAPI app with POST /task
  2. Batch mode: When TASK_JSON env var is set, runs a single task,
     prints JSON result to stdout, and exits. Used by K8s Jobs.
"""
import asyncio
import json
import os
import logging
import sys
from typing import Optional

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
LLM_API_KEY: str = os.getenv("LLM_API_KEY", "ollama")
LLM_BASE_URL: str = os.getenv("LLM_BASE_URL", "http://host.docker.internal:11434/v1")
LLM_MODEL: str = os.getenv("LLM_MODEL", "qwen3-coder-next:latest")
MAX_TOKENS: int = int(os.getenv("MAX_TOKENS", "4096"))
TEMPERATURE: float = float(os.getenv("TEMPERATURE", "0.6"))

# ── File-handling instruction appended to all role prompts ─────────────────────
_FILE_INSTRUCTION = (
    "\n\nIf the user provides files, reference them in your analysis. "
    "When suggesting changes to existing files, wrap each file in a block:\n"
    "~~~file:path/to/file\n"
    "full updated file content\n"
    "~~~\n"
    "This allows the system to apply your changes automatically."
)

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
    return prompt + _FILE_INSTRUCTION


# ── OpenAI-compatible client ───────────────────────────────────────────────────
llm_client = AsyncOpenAI(
    api_key=LLM_API_KEY or "ollama",
    base_url=LLM_BASE_URL,
)


# ── Schemas ────────────────────────────────────────────────────────────────────
class TaskRequest(BaseModel):
    description: str
    context: Optional[str] = None
    files: Optional[dict[str, str]] = None


class TaskResponse(BaseModel):
    role: str
    model: str
    result: str


# ── Core task handler (shared between HTTP and batch modes) ────────────────────
async def run_task(description: str, context: str | None = None, files: dict[str, str] | None = None) -> TaskResponse:
    """Execute the agent's LLM call and return the response."""
    system_prompt = get_system_prompt(AGENT_ROLE)

    # Build user message
    parts: list[str] = []
    if context:
        parts.append(f"Context:\n{context}")
    if files:
        file_block = "\n\n".join(
            f"### File: {path}\n```\n{content}\n```" for path, content in files.items()
        )
        parts.append(f"Attached files:\n{file_block}")
    parts.append(f"Task:\n{description}" if (context or files) else description)
    user_content = "\n\n".join(parts)

    log.info("Role=%s | Task=%s", AGENT_ROLE, description[:120])

    try:
        response = await llm_client.chat.completions.create(
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
        return TaskResponse(role=AGENT_ROLE, model=LLM_MODEL, result=f"[LLM error: {exc}]")

    result_text = response.choices[0].message.content or ""
    log.info("Role=%s | Response tokens=%s", AGENT_ROLE, response.usage.completion_tokens if response.usage else "?")

    return TaskResponse(
        role=AGENT_ROLE,
        model=response.model,
        result=result_text,
    )


# ── Batch mode ─────────────────────────────────────────────────────────────────
async def _batch_main():
    """Read TASK_JSON, run the task, print JSON result to stdout, exit."""
    task_json = os.getenv("TASK_JSON", "")
    if not task_json:
        log.error("TASK_JSON env var is empty.")
        print(json.dumps({"result": "[TASK_JSON not set]"}))
        sys.exit(1)

    try:
        task_data = json.loads(task_json)
    except json.JSONDecodeError as exc:
        log.error("Invalid TASK_JSON: %s", exc)
        print(json.dumps({"result": f"[Invalid TASK_JSON: {exc}]"}))
        sys.exit(1)

    description = task_data.get("description", "")
    context = task_data.get("context")
    files = task_data.get("files")

    result = await run_task(description, context, files)
    # Print JSON to stdout for the orchestrator to read from pod logs
    print(json.dumps({"role": result.role, "model": result.model, "result": result.result}))


# ── Check for batch mode BEFORE importing FastAPI (lighter container exit) ─────
if os.getenv("TASK_JSON"):
    asyncio.run(_batch_main())
    sys.exit(0)

# ── HTTP server mode ───────────────────────────────────────────────────────────
from fastapi import FastAPI, HTTPException

app = FastAPI(
    title="Hive Agent",
    description="Hive agent — POST /task endpoint. Supports batch mode via TASK_JSON env.",
    version="0.2.0",
)


@app.get("/", tags=["health"])
async def health():
    return {
        "status": "ok",
        "role": AGENT_ROLE,
        "model": LLM_MODEL,
        "base_url": LLM_BASE_URL,
    }


@app.post("/task", response_model=TaskResponse, tags=["agent"])
async def handle_task(request: TaskRequest):
    result = await run_task(request.description, request.context, request.files)
    if result.result.startswith("[LLM error:"):
        raise HTTPException(status_code=502, detail=result.result)
    return result


# ── Entry-point (uvicorn) ──────────────────────────────────────────────────────
if __name__ == "__main__":
    import uvicorn
    uvicorn.run("agent:app", host="0.0.0.0", port=8000, reload=False)
