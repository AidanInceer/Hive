"""LLM client, routing logic, and synthesis.

Centralises all direct LLM interactions so that model configuration
changes (hot-swap via /model) take effect everywhere.
"""

import json
import logging
import re

from fastapi import HTTPException
from openai import AsyncOpenAI

from . import config
from .agents import VALID_AGENTS

log = logging.getLogger("orchestrator")

# ── LLM client ──────────────────────────────────────────────────────────────────
client = AsyncOpenAI(
    api_key=config.LLM_API_KEY or "ollama",
    base_url=config.LLM_BASE_URL,
)

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
        resp = await client.chat.completions.create(
            model=config.LLM_MODEL,
            messages=[
                {"role": "system", "content": _ROUTING_SYSTEM},
                {"role": "user", "content": message},
            ],
            max_tokens=128,
            temperature=0.0,
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
        f"Original user task:\n{message}\n\n" f"Agent responses:\n{agent_section}"
    )
    try:
        resp = await client.chat.completions.create(
            model=config.LLM_MODEL,
            messages=[
                {"role": "system", "content": _SYNTHESIS_SYSTEM},
                {"role": "user", "content": user_content},
            ],
            max_tokens=config.MAX_TOKENS,
            temperature=config.TEMPERATURE,
        )
        return resp.choices[0].message.content or ""
    except Exception as exc:
        log.exception("Synthesis failed: %s", exc)
        raise HTTPException(status_code=502, detail=f"Synthesis error: {exc}") from exc
