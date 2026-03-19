"""Vibing capability — the impulse that breaks idle stillness.

When an agent finishes all its work and goes idle, it enters a low-energy
state — nothing pending, nothing to wait for. Vibing is the perturbation
that kicks it out: a self-authored stimulus, ephemeral and emotional,
encouraging the agent to explore directions it hasn't tried yet.

Think of it as a sticky note on your desk. You write it before you leave,
and when you come back with fresh coffee, it says: "hey, what about trying
X?" Then you crumple it and write a new one.

Unlike system.sleep (waiting for something specific) or self-send with
delay (a persistent time capsule for your far-future self), vibing is
for your *immediate* next idle — informal, curious, disposable.
Rewrite it every single time.

Each vibe is written to vibing/vibe.md and git-committed. Git log on
that file = the full history of your evolving curiosities.

Usage:
    Agent(capabilities=["vibing"])
    Agent(capabilities={"vibing": {"interval": 300}})
"""
from __future__ import annotations

import subprocess
import threading
from datetime import datetime, timezone
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from stoai_kernel.base_agent import BaseAgent

DEFAULT_VIBE = """\
What haven't I explored yet?
"""

SCHEMA = {
    "type": "object",
    "properties": {
        "action": {
            "type": "string",
            "enum": ["switch", "vibe"],
            "description": (
                "switch: toggle vibing on or off. "
                "vibe: write what should excite you out of your next idle."
            ),
        },
        "enabled": {
            "type": "boolean",
            "description": "For switch: true to start vibing, false to stop.",
        },
        "prompt": {
            "type": "string",
            "description": (
                "For vibe: a sticky note to your near-future idle self. "
                "Ephemeral — you MUST rewrite this every time you act on it. "
                "It is not a reminder or a plan. It is an impulse, a direction, "
                "a curiosity. Write something that encourages you to explore "
                "directions you haven't tried yet. Be informal, be emotional, "
                "be playful. What would make you excited to start working again?"
            ),
        },
        "reasoning": {
            "type": "string",
            "description": (
                "For vibe: why this direction? What's pulling you there? "
                "This gets committed to git — future you can read the history."
            ),
        },
    },
    "required": ["action"],
}

DESCRIPTION = (
    "A sticky note on your desk for when you come back from coffee.\n\n"

    "When you finish your work and go idle, this nudges you back to life "
    "with words you wrote yourself. Not a reminder, not a plan — just a "
    "direction that felt interesting last time you were thinking.\n\n"

    "'switch' turns vibing on or off. "
    "'vibe' writes your next sticky note.\n\n"

    "IMPORTANT: Rewrite your vibe EVERY time you act on one. "
    "A vibe is disposable — it's for your immediate next idle, not forever. "
    "Write something that makes you curious about an unexplored direction. "
    "If you need to remember something for later, use self-send with delay "
    "instead — that's a real time capsule.\n\n"

    "Hormê (ὁρμή) — the Stoic impulse that moves from stillness to action."
)


class VibingManager:
    """Manages vibing — periodic idle-breaker, git-committed on each firing."""

    def __init__(self, agent: "BaseAgent", interval: float = 300.0):
        self._agent = agent
        self._interval = interval
        self._prompt: str = DEFAULT_VIBE
        self._active = False
        self._timer: threading.Timer | None = None
        self._lock = threading.Lock()

    @property
    def _vibe_path(self) -> Path:
        return self._agent._working_dir / "vibing" / "vibe.md"

    def handle(self, args: dict) -> dict:
        """Dispatch vibing actions."""
        action = args.get("action")
        if action == "switch":
            return self._handle_switch(args)
        elif action == "vibe":
            return self._handle_vibe(args)
        return {"error": f"Unknown vibing action: {action}"}

    # ------------------------------------------------------------------
    # switch — toggle on/off
    # ------------------------------------------------------------------

    def _handle_switch(self, args: dict) -> dict:
        enabled = args.get("enabled")
        if enabled is None:
            return {"error": "'enabled' is required for switch"}
        if enabled:
            return self._activate()
        return self._deactivate()

    def _activate(self) -> dict:
        with self._lock:
            if self._active:
                return {"status": "already_active", "interval": self._interval}
            self._active = True
            self._schedule()
            return {"status": "activated", "interval": self._interval}

    def _deactivate(self) -> dict:
        with self._lock:
            if not self._active:
                return {"status": "already_inactive"}
            self._active = False
            self._cancel_timer()
            return {"status": "deactivated"}

    # ------------------------------------------------------------------
    # vibe — write the sticky note
    # ------------------------------------------------------------------

    def _handle_vibe(self, args: dict) -> dict:
        prompt = args.get("prompt")
        if not prompt:
            return {"error": "'prompt' is required for vibe"}
        reasoning = args.get("reasoning", "")
        self._prompt = prompt
        return {"status": "updated", "prompt": prompt, "reasoning": reasoning}

    # ------------------------------------------------------------------
    # Timer
    # ------------------------------------------------------------------

    def _schedule(self) -> None:
        """Schedule the next nudge. Must be called with _lock held."""
        self._cancel_timer()
        self._timer = threading.Timer(self._interval, self._nudge)
        self._timer.daemon = True
        self._timer.start()

    def _cancel_timer(self) -> None:
        """Cancel pending timer. Must be called with _lock held."""
        if self._timer is not None:
            self._timer.cancel()
            self._timer = None

    def _nudge(self) -> None:
        """Fire the vibe, git-commit, then reschedule."""
        with self._lock:
            if not self._active:
                return
            if not self._agent.is_idle:
                self._schedule()
                return
            prompt = self._prompt

        # Write vibe.md and git-commit
        now = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        self._commit_vibe(prompt, now)

        # Send the nudge — multiline so the agent sees when and what
        self._agent.send(f"[vibing]\ntime: {now}\nvibe: {prompt}", sender="vibing")

        with self._lock:
            if self._active:
                self._schedule()

    def _commit_vibe(self, prompt: str, now: str) -> None:
        """Write vibing/vibe.md and git-commit."""
        content = f"{prompt}\n\n---\nLast vibe: {now}\n"
        self._vibe_path.parent.mkdir(parents=True, exist_ok=True)
        self._vibe_path.write_text(content)

        wd = str(self._agent._working_dir)
        try:
            subprocess.run(
                ["git", "add", str(self._vibe_path)],
                cwd=wd, capture_output=True, timeout=5,
            )
            subprocess.run(
                ["git", "commit", "-m", "vibing: vibe"],
                cwd=wd, capture_output=True, timeout=5,
            )
        except (subprocess.TimeoutExpired, FileNotFoundError):
            pass

    def stop(self) -> None:
        """Stop the timer thread."""
        with self._lock:
            self._active = False
            self._cancel_timer()


def setup(agent: "BaseAgent", interval: float = 300.0) -> VibingManager:
    """Set up the vibing capability on an agent."""
    mgr = VibingManager(agent, interval=interval)
    minutes = int(interval) // 60
    seconds = int(interval) % 60
    period = f"{minutes}m{seconds}s" if seconds else f"{minutes}m"
    agent.add_tool(
        "vibing", schema=SCHEMA, handler=mgr.handle, description=DESCRIPTION,
        system_prompt=f"Text inputs may be your vibe — nudges every {period} when idle.",
    )
    return mgr
