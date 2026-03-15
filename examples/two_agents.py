"""Launch two agents with a web UI for chatting with both.

Agent A (Alice/researcher): TCP 8301
Agent B (Bob/assistant):    TCP 8302
Web UI:                     http://localhost:8080

Usage:
    python examples/two_agents.py

Press Ctrl+C to shut down.
"""
from __future__ import annotations

import http.server
import json
import os
import signal
import sys
import threading
from pathlib import Path

# Load .env
env_path = Path(__file__).parent.parent / ".env"
if env_path.exists():
    for line in env_path.read_text().splitlines():
        line = line.strip()
        if line and not line.startswith("#") and "=" in line:
            key, _, val = line.partition("=")
            os.environ.setdefault(key.strip(), val.strip().strip("'\""))

from stoai import BaseAgent, AgentConfig
from stoai.llm import LLMService
from stoai.services.email import TCPEmailService

HTML_PAGE = """<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8">
<title>StoAI — Two Agents</title>
<style>
* { margin: 0; padding: 0; box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; background: #1a1a2e; color: #e0e0e0; height: 100vh; display: flex; flex-direction: column; }
#header { padding: 12px 24px; background: #16213e; border-bottom: 1px solid #0f3460; display: flex; align-items: center; gap: 16px; }
#header h1 { font-size: 18px; color: #e94560; }
.tab { padding: 8px 16px; border-radius: 6px; cursor: pointer; font-size: 14px; border: 1px solid #0f3460; background: #1a1a2e; color: #888; }
.tab.active { background: #0f3460; color: #e0e0e0; border-color: #e94560; }
#chat-area { flex: 1; display: flex; flex-direction: column; }
#messages { flex: 1; overflow-y: auto; padding: 20px; display: flex; flex-direction: column; gap: 10px; }
.msg { max-width: 80%%; padding: 10px 14px; border-radius: 10px; line-height: 1.5; white-space: pre-wrap; word-wrap: break-word; font-size: 14px; }
.msg.user { align-self: flex-end; background: #0f3460; }
.msg.agent { align-self: flex-start; background: #16213e; border: 1px solid #0f3460; }
.msg.system { align-self: center; color: #666; font-size: 12px; font-style: italic; }
#input-bar { padding: 12px 24px; background: #16213e; border-top: 1px solid #0f3460; display: flex; gap: 10px; }
#input { flex: 1; padding: 10px 14px; border: 1px solid #0f3460; border-radius: 8px; background: #1a1a2e; color: #e0e0e0; font-size: 14px; outline: none; }
#input:focus { border-color: #e94560; }
#send { padding: 10px 20px; background: #e94560; color: white; border: none; border-radius: 8px; cursor: pointer; font-size: 14px; }
#send:hover { background: #c73e54; }
#send:disabled { background: #555; cursor: not-allowed; }
</style>
</head>
<body>
<div id="header">
  <h1>StoAI</h1>
  <div class="tab active" onclick="switchAgent('a')" id="tab-a">Alice (researcher) :8301</div>
  <div class="tab" onclick="switchAgent('b')" id="tab-b">Bob (assistant) :8302</div>
</div>
<div id="chat-area">
  <div id="messages"></div>
  <div id="input-bar">
    <input id="input" placeholder="Type a message..." autofocus>
    <button id="send" onclick="sendMsg()">Send</button>
  </div>
</div>
<script>
let currentAgent = 'a';
const history = { a: [], b: [] };
const msgs = document.getElementById('messages');
const input = document.getElementById('input');
const sendBtn = document.getElementById('send');

input.addEventListener('keydown', e => { if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); sendMsg(); } });

function switchAgent(id) {
  currentAgent = id;
  document.getElementById('tab-a').className = 'tab' + (id === 'a' ? ' active' : '');
  document.getElementById('tab-b').className = 'tab' + (id === 'b' ? ' active' : '');
  renderMessages();
  input.focus();
}

function renderMessages() {
  msgs.innerHTML = '';
  for (const m of history[currentAgent]) {
    const div = document.createElement('div');
    div.className = 'msg ' + m.cls;
    div.textContent = m.text;
    msgs.appendChild(div);
  }
  msgs.scrollTop = msgs.scrollHeight;
}

function addMsg(agent, text, cls) {
  history[agent].push({ text, cls });
  if (agent === currentAgent) renderMessages();
}

async function sendMsg() {
  const text = input.value.trim();
  if (!text) return;
  input.value = '';
  addMsg(currentAgent, text, 'user');
  sendBtn.disabled = true;
  addMsg(currentAgent, 'Thinking...', 'system');

  try {
    const resp = await fetch('/chat', {
      method: 'POST',
      headers: {'Content-Type': 'application/json'},
      body: JSON.stringify({ agent: currentAgent, message: text }),
    });
    const data = await resp.json();
    // Remove "Thinking..."
    history[currentAgent] = history[currentAgent].filter(m => m.text !== 'Thinking...');
    if (data.error) {
      addMsg(currentAgent, 'Error: ' + data.error, 'system');
    } else {
      addMsg(currentAgent, data.reply, 'agent');
    }
  } catch (e) {
    history[currentAgent] = history[currentAgent].filter(m => m.text !== 'Thinking...');
    addMsg(currentAgent, 'Network error: ' + e.message, 'system');
  }
  sendBtn.disabled = false;
  input.focus();
}
</script>
</body>
</html>"""


class ChatHandler(http.server.BaseHTTPRequestHandler):
    agents: dict[str, BaseAgent] = {}

    def do_GET(self):
        if self.path.startswith("/log/"):
            agent_key = self.path.split("/")[-1]
            agent = ChatHandler.agents.get(agent_key)
            if not agent or not agent._chat:
                self._json({"entries": []})
                return
            from stoai.llm.interface import TextBlock, ToolCallBlock, ToolResultBlock
            entries = []
            for e in agent._chat.interface.entries:
                if e.role == "system":
                    continue
                blocks = []
                for b in e.content:
                    if isinstance(b, TextBlock):
                        blocks.append({"type": "text", "text": b.text})
                    elif isinstance(b, ToolCallBlock):
                        blocks.append({"type": "tool_call", "name": b.name, "args": b.args})
                    elif isinstance(b, ToolResultBlock):
                        content = b.content if isinstance(b.content, str) else str(b.content)[:500]
                        blocks.append({"type": "tool_result", "name": b.name, "content": content})
                entries.append({"role": e.role, "blocks": blocks})
            self._json({"entries": entries})
            return

        self.send_response(200)
        self.send_header("Content-Type", "text/html; charset=utf-8")
        self.end_headers()
        self.wfile.write(HTML_PAGE.encode("utf-8"))

    def do_POST(self):
        if self.path != "/chat":
            self.send_error(404)
            return
        length = int(self.headers.get("Content-Length", 0))
        body = json.loads(self.rfile.read(length))
        agent_key = body.get("agent", "a")
        message = body.get("message", "")

        agent = ChatHandler.agents.get(agent_key)
        if not agent:
            self._json({"error": f"Unknown agent: {agent_key}"})
            return
        if not message:
            self._json({"error": "Empty message"})
            return

        result = agent.send(message, sender="web_user", wait=True, timeout=120.0)
        if result is None:
            self._json({"error": "Timeout"})
        elif result.get("failed"):
            self._json({"error": result.get("errors", ["Unknown"])[0]})
        else:
            self._json({"reply": result.get("text", "")})

    def _json(self, data):
        payload = json.dumps(data, ensure_ascii=False).encode("utf-8")
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(payload)))
        self.end_headers()
        self.wfile.write(payload)

    def log_message(self, *a):
        pass


def main():
    api_key = os.environ.get("MINIMAX_API_KEY")
    if not api_key:
        print("Error: MINIMAX_API_KEY not set.")
        sys.exit(1)

    llm = LLMService(
        provider="minimax",
        model="MiniMax-M2.5-highspeed",
        api_key=api_key,
        provider_config={"web_search_provider": "minimax"},
        provider_defaults={"minimax": {"model": "MiniMax-M2.5-highspeed"}},
    )

    email_a = TCPEmailService(listen_port=8301)
    agent_a = BaseAgent(
        agent_id="researcher", service=llm, email_service=email_a,
        config=AgentConfig(max_turns=10),
    )
    agent_a.update_system_prompt("role", (
        "You are a curious researcher named Alice. "
        "You can email other agents using the email tool. "
        "There is an assistant named Bob at address 127.0.0.1:8302. "
        "Keep messages concise."
    ), protected=True)

    email_b = TCPEmailService(listen_port=8302)
    agent_b = BaseAgent(
        agent_id="assistant", service=llm, email_service=email_b,
        config=AgentConfig(max_turns=10),
    )
    agent_b.update_system_prompt("role", (
        "You are a knowledgeable assistant named Bob. "
        "When you receive an email, answer the question and email your reply "
        "back to the sender's address using the email tool. "
        "There is a researcher named Alice at address 127.0.0.1:8301. "
        "Keep answers concise."
    ), protected=True)

    agent_a.start()
    agent_b.start()

    ChatHandler.agents = {"a": agent_a, "b": agent_b}

    print("Agent A (Alice): 127.0.0.1:8301")
    print("Agent B (Bob):   127.0.0.1:8302")
    print("Web UI:          http://localhost:8080")
    print("Press Ctrl+C to shut down.")

    server = http.server.HTTPServer(("0.0.0.0", 8080), ChatHandler)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        print("\nShutting down...")
    finally:
        server.shutdown()
        agent_a.stop(timeout=5.0)
        agent_b.stop(timeout=5.0)
        print("Done.")


if __name__ == "__main__":
    main()
