"""Agent registry and fan-out logic.

Maps agent role names to their in-cluster URLs and provides helpers for
calling agents in parallel.
"""

import asyncio
import logging
import os

import httpx

from .config import AGENT_TIMEOUT

log = logging.getLogger("orchestrator")

# ── Agent registry ──────────────────────────────────────────────────────────────
# Maps role name → in-cluster URL.  Override via AGENT_<ROLE>_URL env vars.
_DEFAULT_AGENT_URLS: dict[str, str] = {
    "code_reviewer": "http://agent-code-reviewer.hive.svc.cluster.local:8000",
    "planner": "http://agent-planner.hive.svc.cluster.local:8000",
    "researcher": "http://agent-researcher.hive.svc.cluster.local:8000",
    "debugger": "http://agent-debugger.hive.svc.cluster.local:8000",
    "architect": "http://agent-architect.hive.svc.cluster.local:8000",
}

AGENT_URLS: dict[str, str] = {
    role: os.getenv(f"AGENT_{role.upper()}_URL", url)
    for role, url in _DEFAULT_AGENT_URLS.items()
}

VALID_AGENTS: list[str] = list(AGENT_URLS.keys())


# ── Fan-out helpers ─────────────────────────────────────────────────────────────
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
    """Call all selected agents in parallel and collect results."""
    async with httpx.AsyncClient() as client:
        tasks = [call_agent(client, role, message) for role in agents]
        results = await asyncio.gather(*tasks)
    return dict(results)
