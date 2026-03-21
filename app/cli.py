"""Interactive CLI channel — stdin/stdout via inter-agent email."""
from __future__ import annotations

import sys
import tempfile
import threading
from pathlib import Path

from lingtai.services.mail import FilesystemMailService


class CLIChannel:
    """CLI channel that exchanges messages with the agent via filesystem mail.

    Creates a FilesystemMailService in a temp directory to receive replies.
    Sends messages to the agent's working directory.
    """

    def __init__(self, agent_address: str, cli_dir: str | Path | None = None) -> None:
        self._agent_address = agent_address
        self._cli_dir = Path(cli_dir) if cli_dir else Path(tempfile.mkdtemp(prefix="lingtai_cli_"))
        self._listener: FilesystemMailService | None = None
        self._sender: FilesystemMailService | None = None

    @property
    def address(self) -> str:
        return str(self._cli_dir)

    def start(self) -> None:
        """Start the filesystem mail listener for incoming replies."""
        # Create a minimal .agent.json and heartbeat so the CLI dir looks like an agent
        import json, time
        self._cli_dir.mkdir(parents=True, exist_ok=True)
        agent_json = self._cli_dir / ".agent.json"
        if not agent_json.is_file():
            agent_json.write_text(json.dumps({"agent_id": "cli", "agent_name": "cli"}))
        heartbeat = self._cli_dir / ".agent.heartbeat"
        heartbeat.write_text(str(time.time()))
        # Keep heartbeat alive
        self._heartbeat_stop = threading.Event()
        def _heartbeat_loop():
            while not self._heartbeat_stop.is_set():
                try:
                    heartbeat.write_text(str(time.time()))
                except OSError:
                    pass
                self._heartbeat_stop.wait(1.0)
        self._heartbeat_thread = threading.Thread(target=_heartbeat_loop, daemon=True)
        self._heartbeat_thread.start()

        self._listener = FilesystemMailService(working_dir=self._cli_dir)
        self._listener.listen(on_message=self._on_message)
        self._sender = FilesystemMailService(working_dir=self._cli_dir)

    def stop(self) -> None:
        """Stop the filesystem mail listener."""
        if hasattr(self, '_heartbeat_stop'):
            self._heartbeat_stop.set()
        if self._listener is not None:
            self._listener.stop()

    def send(self, text: str) -> None:
        """Send a message to the agent."""
        if self._sender is None:
            self._sender = FilesystemMailService(working_dir=self._cli_dir)
        self._sender.send(self._agent_address, {
            "from": self.address,
            "to": [self._agent_address],
            "subject": "",
            "message": text,
        })

    def _on_message(self, payload: dict) -> None:
        """Handle incoming message — print to stdout."""
        sender = payload.get("from", "agent")
        message = payload.get("message", "")
        if message:
            name = sender.split("@")[0] if "@" in sender else sender
            print(f"[{name}] {message}", flush=True)

    def interactive_loop(self) -> None:
        """Run the interactive stdin/stdout loop. Blocks until EOF or Ctrl+C."""
        try:
            while True:
                try:
                    line = input("> ")
                except EOFError:
                    break
                line = line.strip()
                if not line:
                    continue
                self.send(line)
        except KeyboardInterrupt:
            pass
