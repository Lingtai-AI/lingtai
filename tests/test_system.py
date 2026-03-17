"""Tests for system intrinsic — agent identity management (role + ltm)."""
from __future__ import annotations

from stoai.intrinsics import ALL_INTRINSICS


def test_system_in_all_intrinsics():
    assert "system" in ALL_INTRINSICS
    info = ALL_INTRINSICS["system"]
    assert "schema" in info
    assert "description" in info
    assert info["handler"] is None  # handled by BaseAgent


def test_memory_not_in_all_intrinsics():
    """memory intrinsic should be completely removed."""
    assert "memory" not in ALL_INTRINSICS
