"""Launch an Agent on a TCP port and chat with it.

Usage:
    python examples/chat_agent.py

The agent listens on port 8301. Type messages to chat.
Press Ctrl+C to quit.
"""
from __future__ import annotations

import os
import secrets
import sys
import time

# Load .env
from pathlib import Path
env_path = Path(__file__).parent.parent / ".env"
if env_path.exists():
    for line in env_path.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            key, _, val = line.partition("=")
            os.environ.setdefault(key.strip(), val.strip().strip("'\""))

from lingtai import Agent, AgentConfig, FilesystemMailService
from lingtai.llm import LLMService

PORT = 8301

def main():
    api_key = os.environ.get("MINIMAX_API_KEY")
    if not api_key:
        print("Error: MINIMAX_API_KEY not set. Check .env file.")
        sys.exit(1)

    print(f"Starting agent with MiniMax on port {PORT}...")

    llm = LLMService(
        provider="minimax",
        model="MiniMax-M2.5-highspeed",
        api_key=api_key,
        provider_defaults={
            "minimax": {"model": "MiniMax-M2.5-highspeed"},
        },
    )

    base_dir = Path(".")
    agent_id = secrets.token_hex(3)
    mail_svc = FilesystemMailService(working_dir=base_dir / agent_id)

    agent = Agent(
        agent_name="assistant",
        agent_id=agent_id,
        service=llm,
        mail_service=mail_svc,
        config=AgentConfig(max_turns=20),
        base_dir=base_dir,
        streaming=True,
        capabilities={
            "file": {},
            "bash": {"yolo": True},
            "email": {},
            "vision": {},
            "web_search": {},
            "psyche": {},
            "avatar": {},
        },
    )
    agent.start()

    agent_address = str(base_dir / agent_id)
    print(f"Agent address: {agent_address}")
    print("Type messages below. Press Ctrl+C to quit.\n")

    import tempfile
    sender_dir = Path(tempfile.mkdtemp(prefix="lingtai_chat_"))
    sender = FilesystemMailService(working_dir=sender_dir)

    try:
        while True:
            try:
                user_input = input("You: ")
            except EOFError:
                break
            if not user_input.strip():
                continue

            # Send message to agent
            payload = {
                "from": "user",
                "to": agent_address,
                "message": user_input,
            }
            err = sender.send(agent_address, payload)
            if err is not None:
                print(f"  [Failed to send: {err}]")
                continue

            # Wait for agent to process (poll inbox for reply)
            # The agent processes asynchronously — we need to wait for it to finish
            print("Agent: ", end="", flush=True)

            # Wait for agent to go active then back to sleeping
            time.sleep(0.2)  # give it time to pick up the message
            timeout = 120.0
            start = time.monotonic()
            while not agent.is_idle and time.monotonic() - start < timeout:
                time.sleep(0.1)

            # Get the response from the chat session
            if agent._chat is not None:
                last = agent._chat.interface.last_assistant_entry()
                if last:
                    from lingtai.llm.interface import TextBlock
                    text_parts = [b.text for b in last.content if isinstance(b, TextBlock)]
                    print("".join(text_parts))
                else:
                    print("[No response]")
            else:
                print("[No chat session]")

    except KeyboardInterrupt:
        print("\n\nShutting down...")
    finally:
        agent.stop(timeout=5.0)
        print("Done.")


if __name__ == "__main__":
    main()
