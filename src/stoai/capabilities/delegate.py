"""Delegate capability — spawn peer agents on free TCP ports.

Maintains an append-only ledger (delegates/ledger.jsonl) that records every
delegation event.  Each line is a timestamped record of what was delegated,
to whom, with what mission, privileges, and capabilities.  The ledger is
never mutated — only appended to.  It forms a responsibility map that the
delegator can consult before re-delegating.

Lifecycle management (kill, revive) is handled by the email capability,
not here.  The delegate tool's only job is to delegate.

Usage:
    Agent(capabilities=["delegate"])
    # delegate(name="researcher", ...)   — spawn or re-activate
"""
from __future__ import annotations

import json
import socket
import time
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from ..agent import Agent

SCHEMA = {
    "type": "object",
    "properties": {
        "name": {
            "type": "string",
            "description": "Name for the delegatee (required, e.g. 'researcher', 'analyst')",
        },
        "covenant": {
            "type": "string",
            "description": "Covenant override for the delegatee (optional, default = copy parent)",
        },
        "memory": {
            "type": "string",
            "description": "Memory / context to inject (optional)",
        },
        "capabilities": {
            "type": "array",
            "items": {"type": "string"},
            "description": "Capability names for the delegatee (optional, default = same as delegator minus delegate)",
        },
        "admin": {
            "type": "object",
            "description": (
                "Admin privileges granted to the delegatee (optional, default = none). "
                "Dict of privilege name to boolean, e.g. {\"silence\": true}. "
                "Only grant privileges the peer needs."
            ),
        },
        "provider": {
            "type": "string",
            "description": (
                "LLM provider for the delegatee (optional, default = same as delegator). "
                "Use a provider name defined in config, e.g. 'minimax', 'gemini', 'openrouter'."
            ),
        },
        "model": {
            "type": "string",
            "description": (
                "LLM model for the delegatee (optional, default = same as delegator). "
                "e.g. 'gemini-3-flash-preview', 'anthropic/claude-sonnet-4.6'."
            ),
        },
    },
    "required": ["name"],
}

DESCRIPTION = (
    "Delegate responsibility to a peer agent. "
    "Each delegatee runs on its own TCP port with its own conversation. "
    "Use mail or email to communicate with delegatees. "
    "If the named peer already exists and is idle, re-sends the mission "
    "briefing to re-activate it.  If stuck or errored, advises to revive "
    "via email.  If stopped, spawns fresh (preserving the working dir). "
    "All delegations are recorded in an append-only ledger at "
    "delegates/ledger.jsonl — read it with the file read tool to review "
    "past delegations: who was entrusted, what mission, what privileges "
    "and capabilities were granted. Check the ledger before re-delegating. "
    "IMPORTANT: The reasoning field is sent as the first message "
    "to the peer — write a thorough mission briefing: what to do, why, "
    "what context is needed, and what to report back."
)


class DelegateManager:
    """Delegates tasks to peer agents on free TCP ports.

    Keeps an in-memory reference table for live status checks and an
    append-only JSONL ledger on disk that records every delegation.
    """

    def __init__(self, agent: "Agent"):
        self._agent = agent
        self._peers: dict[str, "Agent"] = {}  # name -> live Agent reference

    # ------------------------------------------------------------------
    # Handler
    # ------------------------------------------------------------------

    def handle(self, args: dict) -> dict:
        return self._delegate(args)

    # ------------------------------------------------------------------
    # Ledger (append-only JSONL log of delegation events)
    # ------------------------------------------------------------------

    @property
    def _ledger_path(self) -> Path:
        return self._agent._working_dir / "delegates" / "ledger.jsonl"

    def _append_ledger(self, event: str, name: str, **fields) -> None:
        """Append a single event record to the ledger."""
        self._ledger_path.parent.mkdir(parents=True, exist_ok=True)
        record = {"ts": time.time(), "event": event, "name": name, **fields}
        with open(self._ledger_path, "a") as f:
            f.write(json.dumps(record, ensure_ascii=False) + "\n")

    # ------------------------------------------------------------------
    # Live status (reads from in-memory agent refs)
    # ------------------------------------------------------------------

    def _live_status(self, name: str) -> str:
        """Get live status of a delegatee: active/idle/stuck/error/stopped."""
        peer = self._peers.get(name)
        if peer is None:
            return "stopped"
        if peer._thread is None or not peer._thread.is_alive():
            return "stopped"
        if peer._state.value == "active":
            return "active"
        reason = getattr(peer, "_sleep_reason", "idle")
        if reason == "stuck":
            return "stuck"
        elif reason == "error":
            return "error"
        return "idle"

    # ------------------------------------------------------------------
    # Core delegation
    # ------------------------------------------------------------------

    def _delegate(self, args: dict) -> dict:
        from ..agent import Agent
        from stoai_kernel.services.mail import TCPMailService

        parent = self._agent
        reasoning = args.get("_reasoning")
        peer_name = args.get("name", "delegate")

        # Check if this peer already exists and is live
        existing = self._peers.get(peer_name)
        if existing is not None:
            status = self._live_status(peer_name)
            if status == "active":
                return {
                    "status": "already_active",
                    "address": existing._mail_service.address if existing._mail_service else None,
                    "agent_id": existing.agent_id,
                    "agent_name": existing.agent_name,
                    "message": f"'{peer_name}' is currently active. Use email to communicate.",
                }
            elif status == "idle":
                if reasoning:
                    existing.send(reasoning, sender=parent.agent_id)
                    self._append_ledger(
                        "reactivate", peer_name, mission=reasoning,
                    )
                return {
                    "status": "reactivated",
                    "address": existing._mail_service.address if existing._mail_service else None,
                    "agent_id": existing.agent_id,
                    "agent_name": existing.agent_name,
                    "message": f"'{peer_name}' was idle — sent new mission briefing.",
                }
            elif status in ("stuck", "error"):
                return {
                    "status": status,
                    "agent_name": peer_name,
                    "message": (
                        f"'{peer_name}' is {status}. "
                        f"To revive: re-delegate with the SAME agent name "
                        f"(use email to kill it first, then delegate again)."
                    ),
                }
            # stopped — fall through to spawn fresh

        # Resolve delegation parameters
        covenant = args.get("covenant") or parent._prompt_manager.read_section("covenant") or ""
        memory = args.get("memory", "")
        admin = args.get("admin") or {}

        requested = args.get("capabilities")
        caps: dict[str, dict] = {}
        cap_names: list[str] = []
        for cap_name, cap_kwargs in parent._capabilities:
            if cap_name == "delegate":
                continue
            if requested is not None and cap_name not in requested:
                continue
            caps[cap_name] = dict(cap_kwargs)
            cap_names.append(cap_name)

        # Spawn peer agent
        port = self._get_free_port()
        delegate_working_dir = parent._base_dir / peer_name
        mail_svc = TCPMailService(listen_port=port, working_dir=delegate_working_dir)

        from stoai_kernel.config import AgentConfig
        peer_provider = args.get("provider") or parent._config.provider
        peer_model = args.get("model") or parent._config.model
        peer_config = AgentConfig(
            max_turns=parent._config.max_turns,
            provider=peer_provider,
            model=peer_model,
            retry_timeout=parent._config.retry_timeout,
            thinking_budget=parent._config.thinking_budget,
        )

        delegate = Agent(
            agent_name=peer_name,
            service=parent.service,
            mail_service=mail_svc,
            config=peer_config,
            base_dir=parent._base_dir,
            streaming=parent._streaming,
            covenant=covenant,
            memory=memory,
            capabilities=caps,
            admin=admin,
        )
        delegate.start()

        if reasoning:
            delegate.send(reasoning, sender=parent.agent_id)

        # Record
        self._peers[peer_name] = delegate
        address = mail_svc.address
        self._append_ledger(
            "delegate", peer_name,
            address=address,
            agent_id=delegate.agent_id,
            port=port,
            mission=reasoning or "",
            privileges=admin,
            capabilities=cap_names,
            provider=peer_provider,
            model=peer_model,
        )

        return {
            "status": "ok",
            "address": address,
            "agent_id": delegate.agent_id,
            "agent_name": delegate.agent_name,
        }

    @staticmethod
    def _get_free_port() -> int:
        """Get an available TCP port from the OS."""
        sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
        sock.bind(("127.0.0.1", 0))
        port = sock.getsockname()[1]
        sock.close()
        return port


def _build_schema(agent: "Agent") -> dict:
    """Build delegate schema with available providers from LLMService."""
    import copy
    schema = copy.deepcopy(SCHEMA)

    try:
        defaults = agent.service._provider_defaults
        available = sorted(str(k) for k in defaults.keys() if isinstance(k, str))
    except (AttributeError, TypeError):
        available = []
    if not available:
        try:
            available = [str(agent.service.provider)]
        except (AttributeError, TypeError):
            available = []

    provider_models: list[str] = []
    try:
        for pname, pdefaults in agent.service._provider_defaults.items():
            if isinstance(pdefaults, dict):
                m = pdefaults.get("model", "")
                if m:
                    provider_models.append(f"{pname}: {m}")
    except (AttributeError, TypeError):
        pass

    schema["properties"]["provider"]["description"] = (
        f"LLM provider for the delegatee (optional, default = same as delegator). "
        f"Available: {', '.join(available)}."
    )
    schema["properties"]["provider"]["enum"] = available

    if provider_models:
        schema["properties"]["model"]["description"] = (
            f"LLM model for the delegatee (optional, default = same as delegator). "
            f"Known: {'; '.join(provider_models)}."
        )

    return schema


def setup(agent: "Agent") -> DelegateManager:
    """Set up the delegate capability on an agent."""
    mgr = DelegateManager(agent)
    schema = _build_schema(agent)
    agent.add_tool("delegate", schema=schema, handler=mgr.handle, description=DESCRIPTION)
    return mgr
