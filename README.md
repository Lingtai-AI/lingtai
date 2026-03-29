# 灵台 (Língtái)

An agent operating system. Named after 灵台方寸山 — where 孙悟空 learned his 72 transformations.

Lingtai provides the minimal kernel for AI agents: thinking (LLM), perceiving (vision, search), acting (file I/O), and communicating (inter-agent email). Domain tools, coordination, and orchestration are plugged in from outside via MCP-compatible interfaces.

## Architecture

Two packages:

- **[lingtai-kernel](https://github.com/huangzesen/lingtai-kernel)** — minimal agent runtime. BaseAgent, intrinsics, LLM protocol, mail/logging services. Zero hard dependencies.
- **lingtai** (this repo) — batteries-included layer. Agent with 19 capabilities, LLM adapters (Anthropic, OpenAI, Gemini, MiniMax, custom), FileIO/Vision/Search services, MCP integration, addons (IMAP, Telegram).

Three-layer agent hierarchy:

```
BaseAgent              — kernel (intrinsics, sealed tool surface)
    │
Agent(BaseAgent)       — kernel + capabilities + domain tools
    │
CustomAgent(Agent)     — your wrapper (subclass with domain logic)
```

## Quick Start

```bash
pip install lingtai

# Or from source
git clone https://github.com/huangzesen/lingtai-kernel.git
git clone https://github.com/huangzesen/lingtai.git
pip install -e lingtai-kernel -e lingtai
```

```python
from lingtai import Agent
from lingtai.llm import LLMService

svc = LLMService()
agent = Agent(
    service=svc,
    agent_name="alice",
    working_dir="/agents/alice",
    capabilities=["file", "email", "web_search", "bash"],
)
agent.start()
```

## TUI

Lingtai ships with a terminal UI for managing agents:

```bash
lingtai-tui              # launch TUI in current project
lingtai-tui tutorial     # guided tutorial
```

## Capabilities

| Capability | What it adds |
|-----------|-------------|
| `file` | read, write, edit, glob, grep (group) |
| `bash` | Shell command execution with policy |
| `email` | Reply, CC/BCC, contacts, archive, scheduled sends |
| `psyche` | Evolving identity, knowledge library |
| `avatar` | Spawn independent sub-agents (分身) |
| `daemon` | Ephemeral parallel workers (神識) |
| `vision` | Image understanding |
| `web_search` | Web search |
| `web_read` | Web page extraction |
| `talk` | Text-to-speech |
| `compose` | Music generation |
| `draw` | Image generation |
| `video` | Video generation |
| `listen` | Speech transcription, music analysis |

## The Metaphor

One heart-mind (一心), myriad forms (万相). Each agent can spawn avatars, and those avatars can spawn their own. The self-growing network of avatars IS the agent itself — memory becomes infinite through multiplication.

## License

MIT
