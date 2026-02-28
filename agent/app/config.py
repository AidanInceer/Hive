"""Agent configuration — single source of truth for all settings."""

import os

from dotenv import load_dotenv

load_dotenv()

# ── Identity ────────────────────────────────────────────────────────────────────
AGENT_ROLE: str = os.getenv("AGENT_ROLE", "default").lower()

# ── LLM settings ───────────────────────────────────────────────────────────────
LLM_API_KEY: str = os.getenv("LLM_API_KEY", "ollama")
LLM_BASE_URL: str = os.getenv("LLM_BASE_URL", "http://host.docker.internal:11434/v1")
LLM_MODEL: str = os.getenv("LLM_MODEL", "qwen2.5-coder:7b")
MAX_TOKENS: int = int(os.getenv("MAX_TOKENS", "4096"))
TEMPERATURE: float = float(os.getenv("TEMPERATURE", "0.6"))
