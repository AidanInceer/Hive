"""
Hive Agent — app package.

Creates and configures the FastAPI application for a single agent pod.

Usage (uvicorn):
    uvicorn app:app --host 0.0.0.0 --port 8000
"""

import logging

from fastapi import FastAPI

from .config import AGENT_ROLE, LLM_MODEL
from .routes import router

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s — %(message)s",
)
log = logging.getLogger("agent")

app = FastAPI(
    title="Hive Agent",
    description=(
        "A single Hive agent exposing a /task endpoint. "
        "Supports Ollama (local) or any OpenAI-compatible API."
    ),
    version="1.0.0",
)

app.include_router(router)


@app.on_event("startup")
async def _startup() -> None:
    log.info("Agent starting — role=%s model=%s", AGENT_ROLE, LLM_MODEL)
