"""Route handlers for the agent API."""

import logging

from fastapi import APIRouter, HTTPException
from openai import AsyncOpenAI

from . import config
from .prompts import get_system_prompt
from .schemas import TaskRequest, TaskResponse

log = logging.getLogger("agent")

router = APIRouter()

# ── LLM client ──────────────────────────────────────────────────────────────────
client = AsyncOpenAI(
    api_key=config.LLM_API_KEY or "ollama",
    base_url=config.LLM_BASE_URL,
)


@router.get("/", tags=["health"])
async def health() -> dict:
    """Liveness probe — returns agent role and model."""
    return {
        "status": "ok",
        "role": config.AGENT_ROLE,
        "model": config.LLM_MODEL,
        "base_url": config.LLM_BASE_URL,
    }


@router.post("/task", response_model=TaskResponse, tags=["agent"])
async def handle_task(request: TaskRequest) -> TaskResponse:
    """
    Receive a task and return the agent's result.

    Body:
        - **description** *(required)*: What the agent should do.
        - **context** *(optional)*: Additional background or prior agent output.
    """
    system_prompt = get_system_prompt(config.AGENT_ROLE)

    user_content = request.description
    if request.context:
        user_content = f"Context:\n{request.context}\n\nTask:\n{request.description}"

    log.info("Role=%s | Task=%s", config.AGENT_ROLE, request.description[:120])

    try:
        response = await client.chat.completions.create(
            model=config.LLM_MODEL,
            messages=[
                {"role": "system", "content": system_prompt},
                {"role": "user", "content": user_content},
            ],
            max_tokens=config.MAX_TOKENS,
            temperature=config.TEMPERATURE,
        )
    except Exception as exc:
        log.exception("LLM API call failed: %s", exc)
        raise HTTPException(status_code=502, detail=f"Model API error: {exc}") from exc

    result_text = response.choices[0].message.content or ""
    log.info("Role=%s | Response tokens=%s", config.AGENT_ROLE, response.usage.completion_tokens)

    return TaskResponse(
        role=config.AGENT_ROLE,
        model=response.model,
        result=result_text,
    )
