"""
Hive Orchestrator — app package.

Creates and configures the FastAPI application, wires up routers, middleware,
and lifecycle hooks.

Usage (uvicorn):
    uvicorn app:app --host 0.0.0.0 --port 9000
"""

import logging

from fastapi import FastAPI
from fastapi.middleware.cors import CORSMiddleware

from .config import VERSION
from .agents import VALID_AGENTS
from . import config
from .routes import chat_router, config_router, health_router

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(name)s — %(message)s",
)
log = logging.getLogger("orchestrator")

# ── App factory ─────────────────────────────────────────────────────────────────
app = FastAPI(
    title="Hive Orchestrator",
    description=(
        "Receives user tasks, routes them to specialist sub-agents running in "
        "Kubernetes, and synthesises the results into a single coherent answer."
    ),
    version=VERSION,
)

# CORS — allow CLI and browsers to call the API
app.add_middleware(
    CORSMiddleware,
    allow_origins=["*"],
    allow_methods=["*"],
    allow_headers=["*"],
)

# Wire up route modules
app.include_router(health_router)
app.include_router(config_router)
app.include_router(chat_router)


@app.on_event("startup")
async def _startup() -> None:
    log.info(
        "Hive Orchestrator v%s starting — model=%s agents=%s",
        VERSION,
        config.LLM_MODEL,
        VALID_AGENTS,
    )
