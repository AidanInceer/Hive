"""Route handlers for the orchestrator API.

All FastAPI route functions live here, grouped by concern:
  - Health / config endpoints (/, /model)
  - Synchronous chat (/chat)
  - Streaming chat (/chat/stream)
"""

import asyncio
import logging
from typing import Any

import httpx
from fastapi import APIRouter
from fastapi.responses import StreamingResponse

from . import config
from .agents import VALID_AGENTS, call_agent, fan_out
from .llm import route, synthesise
from .schemas import ChatRequest, ChatResponse, ModelRequest
from .sse import sse_event

log = logging.getLogger("orchestrator")

# ── Routers ─────────────────────────────────────────────────────────────────────
health_router = APIRouter(tags=["health"])
config_router = APIRouter(tags=["config"])
chat_router = APIRouter(tags=["orchestrator"])


# ── Health ──────────────────────────────────────────────────────────────────────
@health_router.get("/")
async def health() -> dict[str, Any]:
    """Liveness / readiness probe — returns status, version, model, and agent list."""
    return {
        "status": "ok",
        "version": config.VERSION,
        "model": config.LLM_MODEL,
        "agents": VALID_AGENTS,
    }


# ── Model config ────────────────────────────────────────────────────────────────
@config_router.get("/model")
async def get_model() -> dict[str, str]:
    """Return the currently active LLM model."""
    return {"model": config.LLM_MODEL}


@config_router.post("/model")
async def set_model(request: ModelRequest) -> dict[str, str]:
    """Hot-swap the active LLM model at runtime."""
    old = config.LLM_MODEL
    config.LLM_MODEL = request.model
    log.info("Model changed: %s → %s", old, config.LLM_MODEL)
    return {"previous": old, "current": config.LLM_MODEL}


# ── Synchronous chat ────────────────────────────────────────────────────────────
@chat_router.post("/chat", response_model=ChatResponse)
async def chat(request: ChatRequest) -> ChatResponse:
    """
    Main entry point.  Accepts a natural-language message and returns a
    synthesised answer produced by the relevant specialist agents.
    """
    log.info("Received: %s", request.message[:120])

    agents = await route(request.message)
    agent_results = await fan_out(agents, request.message)
    final = await synthesise(request.message, agent_results)

    return ChatResponse(
        result=final,
        agents_used=agents,
        agent_results=agent_results,
    )


# ── Streaming chat (SSE) ───────────────────────────────────────────────────────
@chat_router.post("/chat/stream")
async def chat_stream(request: ChatRequest) -> StreamingResponse:
    """
    Streaming endpoint.  Returns Server-Sent Events so the CLI can show
    real-time progress through routing, fan-out, and synthesis phases.
    """
    log.info("Stream request: %s", request.message[:120])

    async def _generate():
        # Phase 1 — route
        yield sse_event("routing_start", {"message": request.message[:120]})
        agents = await route(request.message)
        yield sse_event("routing_complete", {"agents": agents})

        # Phase 2 — fan-out with per-agent events
        for role in agents:
            yield sse_event("agent_start", {"agent": role})

        agent_results: dict[str, str] = {}
        queue: asyncio.Queue[tuple[str, str]] = asyncio.Queue()

        async def _tracked(client: httpx.AsyncClient, role: str) -> None:
            pair = await call_agent(client, role, request.message)
            await queue.put(pair)

        async with httpx.AsyncClient() as client:
            tasks = [asyncio.create_task(_tracked(client, role)) for role in agents]
            for _ in range(len(agents)):
                role, result = await queue.get()
                agent_results[role] = result
                yield sse_event(
                    "agent_complete",
                    {"agent": role, "preview": result[:300]},
                )
            await asyncio.gather(*tasks, return_exceptions=True)

        # Phase 3 — synthesis
        yield sse_event("synthesis_start", {"agents_used": list(agent_results.keys())})
        final = await synthesise(request.message, agent_results)
        yield sse_event("synthesis_complete", {})

        # Final payload
        yield sse_event(
            "done",
            {
                "result": final,
                "agents_used": list(agent_results.keys()),
                "agent_results": agent_results,
            },
        )

    return StreamingResponse(
        _generate(),
        media_type="text/event-stream",
        headers={
            "Cache-Control": "no-cache",
            "Connection": "keep-alive",
            "X-Accel-Buffering": "no",
        },
    )
