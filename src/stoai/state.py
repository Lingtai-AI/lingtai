"""AgentState — lifecycle state enum for StoAI agents."""

from __future__ import annotations

import enum


class AgentState(enum.Enum):
    """Lifecycle state of an agent.

    SLEEPING --(inbox message)---> ACTIVE
    ACTIVE   --(all done)--------> SLEEPING
    """

    ACTIVE = "active"
    SLEEPING = "sleeping"
