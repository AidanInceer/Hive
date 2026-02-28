"""Pydantic request / response schemas for the agent API."""

from typing import Optional

from pydantic import BaseModel


class TaskRequest(BaseModel):
    """Incoming task from the orchestrator."""

    description: str
    context: Optional[str] = None


class TaskResponse(BaseModel):
    """Agent result returned to the orchestrator."""

    role: str
    model: str
    result: str
