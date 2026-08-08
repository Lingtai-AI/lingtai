# 灵台 — Implementation Status

## Core

The original prototype layout below (`intrinsics/`, `services/file_io.py` + friends, `layers/`) has since been restructured. The current kernel organizes tools under `tools/` (one package per capability/intrinsic, composed via `registry.py`'s `INTRINSICS`/`BUILTIN_TOOLS`) and keeps a much smaller `services/` directory:

- [x] `tools/` — `file` (read/edit/write/glob/grep, replaces the old separate intrinsics), `shell` (was `bash`), `web` (replaces `web_search`), `email`, `vision`, `knowledge`, `skills`, `avatar`, `daemon`, `mcp`, `task_card`, `notification`, `psyche`, `soul`, `system`, `context` — no standalone `layers/` package; the old layer concepts (diary/plan/bash/delegate) are folded into these tools or the kernel itself.
- [x] `services/` — `file_io.py`, `file_io_sidecar.py`, `mail.py`, `mcp.py`, `mcp_inbox.py`, `mcp_licc.py`, `mcp_registry.py`, `vision/`, `websearch/`
- [x] `kernel/services/` — `logging.py`, `mail.py`
- [x] `kernel/` — BaseAgent lifecycle, tool dispatch, compaction, loop guard, streaming, session save/restore

## LLM
- [x] `llm/base.py` — LLMAdapter, ChatSession, LLMResponse, ToolCall, FunctionSchema
- [x] `llm/service.py` — LLMService
- [x] `llm/interface.py` — ChatInterface (canonical conversation history)
- [x] `llm/interface_converters.py` — format converters
- [x] `llm/rate_limiter.py` — rate limiting
- [x] 11 provider adapters: anthropic, claude_code, custom, deepseek, gemini, kimi_code, mimo, minimax, openai, openrouter, zhipu

## Supporting Modules
- [x] `loop_guard.py` — repetitive tool call detection
- [x] `token_counter.py` — token counting
- [x] `tool_timing.py` — tool execution timing
- [x] `llm_utils.py` — LLM utilities (send_with_timeout, etc.)
- [x] `logging.py` — package logging

## Tests (121 passing)
- [x] `test_agent.py` — lifecycle, intrinsics, services, email, file I/O
- [x] `test_types.py`
- [x] `test_prompt.py`
- [x] `test_layers.py` — diary, plan
- [x] `test_layers_bash.py`
- [x] `test_layers_delegate.py`
- [x] `test_intrinsics_file.py`
- [x] `test_intrinsics_comm.py`
- [x] `test_llm_utils.py`
- [x] `test_loop_guard.py`
- [x] `test_token_counter.py`
- [x] `test_services_email.py`
- [x] `test_services_file_io.py`
- [x] `test_services_logging.py`

## What Remains

Delegate/avatar spawning is implemented: `tools/avatar/` spawns agents through `AvatarLauncherPort`/`AvatarLaunchRequest`, and every spawn is recorded in `delegates/ledger.jsonl`.

### Forum package (future)
- [ ] Registry — agents register, others discover by capability
- [ ] Bulletin board — agents post findings, others subscribe
- [ ] Reputation tracking
