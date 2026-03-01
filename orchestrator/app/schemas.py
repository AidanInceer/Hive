"""Pydantic request / response schemas for the orchestrator API."""

from pydantic import BaseModel


class ChatRequest(BaseModel):
    """User message to be processed by the multi-agent pipeline."""

    message: str


class ChatResponse(BaseModel):
    """Final synthesised result from all agents."""

    result: str
    agents_used: list[str]
    agent_results: dict[str, str]


class ModelRequest(BaseModel):
    """Request payload for switching the active LLM model."""

    model: str
