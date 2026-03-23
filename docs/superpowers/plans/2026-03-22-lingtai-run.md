# `lingtai run` CLI Entrypoint — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a `lingtai run <working_dir>` CLI command that reads `init.json` from the given directory, constructs an Agent, and runs it as a blocking foreground process.

**Architecture:** `init.json` is a user-authored, never-modified file with three required top-level keys: `manifest`, `covenant`, `memory`. The CLI reads it, constructs `LLMService` + `Agent`, acquires the lock, writes `.agent.pid`, and blocks until shutdown (via `.quell` signal file, SIGTERM, or vigil expiry). On resume (existing `.agent.json`), identity is preserved from `.agent.json` but LLM config is always re-read from `init.json` (key rotation support).

**Tech Stack:** Python 3.11+, `argparse` for CLI, `lingtai.Agent`, `lingtai.llm.service.LLMService`, `lingtai_kernel.services.mail.FilesystemMailService`, `lingtai_kernel.config.AgentConfig`

---

## File Structure

| File | Responsibility |
|------|---------------|
| Create: `src/lingtai/cli.py` | CLI entrypoint — `argparse`, `init.json` parsing, agent construction, main loop |
| Create: `src/lingtai/init_schema.py` | `init.json` validation — required fields, types, error messages |
| Create: `tests/test_cli.py` | Tests for CLI boot sequence, validation, resume, PID file, signal handling |
| Create: `tests/test_init_schema.py` | Tests for `init.json` validation (missing fields, bad types, etc.) |
| Modify: `pyproject.toml` | Add `[project.scripts]` entry: `lingtai = "lingtai.cli:main"` |

---

## `init.json` Schema (Final)

Every field is required. No defaults. Missing field = hard error.

```json
{
  "manifest": {
    "agent_name": "alice",
    "language": "en",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514",
      "api_key": "sk-ant-...",
      "base_url": null
    },
    "capabilities": {
      "file": {},
      "bash": {"yolo": true},
      "email": {},
      "psyche": {}
    },
    "vigil": 3600,
    "soul_delay": 120,
    "max_turns": 50,
    "admin": {"karma": true},
    "streaming": false
  },
  "covenant": "You speak English. Your mind is private...",
  "memory": ""
}
```

### Field reference

**`manifest`** (object, required):

| Field | Type | Description |
|-------|------|-------------|
| `agent_name` | `string` | True name. |
| `language` | `string` | `"en"`, `"zh"`, or `"wen"`. |
| `llm` | `object` | LLM provider config. |
| `llm.provider` | `string` | Provider name (e.g. `"anthropic"`, `"openai"`, `"gemini"`, `"minimax"`). |
| `llm.model` | `string` | Model identifier. |
| `llm.api_key` | `string \| null` | API key. `null` means rely on environment variable. |
| `llm.base_url` | `string \| null` | Custom endpoint. `null` means use provider default. |
| `capabilities` | `object` | Map of capability name to kwargs dict. Empty dict `{}` for no kwargs. |
| `vigil` | `number` | Max lifespan in seconds. |
| `soul_delay` | `number` | Idle seconds before soul whisper. |
| `max_turns` | `number` | Max tool calls per LLM turn. |
| `admin` | `object` | Admin privileges (e.g. `{"karma": true}`). |
| `streaming` | `boolean` | Enable streaming LLM responses. |

**`covenant`** (string, required): Protected system prompt section. Can be empty string `""`.

**`memory`** (string, required): Initial `system/memory.md` content. Can be empty string `""`. Only written on first boot (never overwrites existing memory).

---

## Boot Sequence

```
1. Parse args: working_dir from argv[1]
2. Read {working_dir}/init.json — fail if missing
3. Validate all required fields — fail with field path on first missing/bad field
4. Construct LLMService from manifest.llm
5. Construct AgentConfig from manifest fields
6. Construct FilesystemMailService(working_dir)
7. Construct Agent(service, working_dir=working_dir, ...)
   — BaseAgent.__init__ handles:
     - lock acquisition
     - .agent.json write
     - covenant/memory file writes (first boot only)
     - system prompt assembly
8. Write {working_dir}/.agent.pid with os.getpid()
9. Register signal handlers: SIGTERM → touch .quell, SIGINT → touch .quell
10. agent.start()
11. Block on agent._shutdown event (or equivalent)
12. agent.stop()
13. Remove .agent.pid
```

---

### Task 1: `init.json` Validation Module

**Files:**
- Create: `src/lingtai/init_schema.py`
- Create: `tests/test_init_schema.py`

- [ ] **Step 1: Write failing tests for validation**

```python
# tests/test_init_schema.py
import json
import pytest
from lingtai.init_schema import validate_init


def _valid_init() -> dict:
    """Return a minimal valid init.json dict."""
    return {
        "manifest": {
            "agent_name": "alice",
            "language": "en",
            "llm": {
                "provider": "anthropic",
                "model": "claude-sonnet-4-20250514",
                "api_key": None,
                "base_url": None,
            },
            "capabilities": {},
            "vigil": 3600,
            "soul_delay": 120,
            "max_turns": 50,
            "admin": {"karma": True},
            "streaming": False,
        },
        "covenant": "",
        "memory": "",
    }


def test_valid_init_passes():
    validate_init(_valid_init())  # should not raise


def test_missing_top_level_key():
    data = _valid_init()
    del data["covenant"]
    with pytest.raises(ValueError, match="covenant"):
        validate_init(data)


def test_missing_manifest_field():
    data = _valid_init()
    del data["manifest"]["agent_name"]
    with pytest.raises(ValueError, match="manifest.agent_name"):
        validate_init(data)


def test_missing_llm_field():
    data = _valid_init()
    del data["manifest"]["llm"]["provider"]
    with pytest.raises(ValueError, match="manifest.llm.provider"):
        validate_init(data)


def test_wrong_type_top_level():
    data = _valid_init()
    data["covenant"] = 123
    with pytest.raises(ValueError, match="covenant.*str"):
        validate_init(data)


def test_wrong_type_manifest_field():
    data = _valid_init()
    data["manifest"]["vigil"] = "one hour"
    with pytest.raises(ValueError, match="manifest.vigil.*(int|float|number)"):
        validate_init(data)


def test_wrong_type_capabilities():
    data = _valid_init()
    data["manifest"]["capabilities"] = ["file", "bash"]
    with pytest.raises(ValueError, match="manifest.capabilities.*object"):
        validate_init(data)


def test_wrong_type_streaming():
    data = _valid_init()
    data["manifest"]["streaming"] = "yes"
    with pytest.raises(ValueError, match="manifest.streaming.*bool"):
        validate_init(data)


def test_bool_rejected_for_numeric_field():
    """bool is a subclass of int in Python — must be rejected for numeric fields."""
    data = _valid_init()
    data["manifest"]["vigil"] = True
    with pytest.raises(ValueError, match="manifest.vigil.*number.*bool"):
        validate_init(data)
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest tests/test_init_schema.py -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'lingtai.init_schema'`

- [ ] **Step 3: Write the validation module**

```python
# src/lingtai/init_schema.py
"""init.json validation — every field required, no defaults, fail loudly."""
from __future__ import annotations


def validate_init(data: dict) -> None:
    """Validate an init.json dict. Raises ValueError with field path on failure."""

    _require_keys(data, {
        "manifest": dict,
        "covenant": str,
        "memory": str,
    }, prefix="")

    manifest = data["manifest"]
    _require_keys(manifest, {
        "agent_name": str,
        "language": str,
        "llm": dict,
        "capabilities": dict,
        "vigil": (int, float),
        "soul_delay": (int, float),
        "max_turns": int,
        "admin": dict,
        "streaming": bool,
    }, prefix="manifest")

    llm = manifest["llm"]
    _require_keys(llm, {
        "provider": str,
        "model": str,
        "api_key": (str, type(None)),
        "base_url": (str, type(None)),
    }, prefix="manifest.llm")


def _require_keys(
    data: dict,
    schema: dict[str, type | tuple[type, ...]],
    prefix: str,
) -> None:
    """Check that all keys exist in data with correct types."""
    for key, expected_type in schema.items():
        path = f"{prefix}.{key}" if prefix else key

        if key not in data:
            raise ValueError(f"missing required field: {path}")

        value = data[key]

        # bool is a subclass of int in Python — reject bools for numeric fields
        if isinstance(value, bool) and expected_type in (int, (int, float)):
            raise ValueError(f"{path}: expected number, got bool")

        if not isinstance(value, expected_type):
            # Build readable type name
            if isinstance(expected_type, tuple):
                names = [t.__name__ for t in expected_type if t is not type(None)]
                type_str = (" | ".join(names) + " | null") if type(None) in expected_type else " | ".join(names)
            else:
                type_str = expected_type.__name__
                if expected_type is dict:
                    type_str = "object"
            raise ValueError(
                f"{path}: expected {type_str}, got {type(value).__name__}"
            )
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest tests/test_init_schema.py -v`
Expected: All 9 tests PASS

- [ ] **Step 5: Smoke-test the module**

Run: `python -c "from lingtai.init_schema import validate_init; print('OK')"`
Expected: `OK`

- [ ] **Step 6: Commit**

```bash
git add src/lingtai/init_schema.py tests/test_init_schema.py
git commit -m "feat: add init.json validation module"
```

---

### Task 2: CLI Entrypoint — Core Boot Sequence

**Files:**
- Create: `src/lingtai/cli.py`
- Create: `tests/test_cli.py`
- Modify: `pyproject.toml`

- [ ] **Step 1: Write failing tests for the boot sequence**

The CLI is hard to unit-test end-to-end (it blocks), so test the pieces: `load_init`, `build_agent`, PID file, and the argument parser.

```python
# tests/test_cli.py
import json
import os
import signal
import pytest
from pathlib import Path
from unittest.mock import MagicMock, patch


def _write_init(tmp_path: Path, overrides: dict | None = None) -> Path:
    """Write a valid init.json to tmp_path and return the path."""
    data = {
        "manifest": {
            "agent_name": "test-agent",
            "language": "en",
            "llm": {
                "provider": "anthropic",
                "model": "test-model",
                "api_key": "test-key",
                "base_url": None,
            },
            "capabilities": {},
            "vigil": 60,
            "soul_delay": 30,
            "max_turns": 10,
            "admin": {"karma": True},
            "streaming": False,
        },
        "covenant": "Be helpful.",
        "memory": "I remember nothing.",
    }
    if overrides:
        data.update(overrides)
    init_path = tmp_path / "init.json"
    init_path.write_text(json.dumps(data))
    return tmp_path


def test_load_init_reads_file(tmp_path):
    from lingtai.cli import load_init
    _write_init(tmp_path)
    data = load_init(tmp_path)
    assert data["manifest"]["agent_name"] == "test-agent"


def test_load_init_missing_file(tmp_path):
    from lingtai.cli import load_init
    with pytest.raises(SystemExit):
        load_init(tmp_path)


def test_load_init_invalid_json(tmp_path):
    (tmp_path / "init.json").write_text("{bad json")
    from lingtai.cli import load_init
    with pytest.raises(SystemExit):
        load_init(tmp_path)


def test_load_init_validation_error(tmp_path):
    (tmp_path / "init.json").write_text(json.dumps({"manifest": {}}))
    from lingtai.cli import load_init
    with pytest.raises(SystemExit):
        load_init(tmp_path)


@patch("lingtai.cli.LLMService")
@patch("lingtai.cli.Agent")
@patch("lingtai.cli.FilesystemMailService")
def test_build_agent_constructs_correctly(mock_mail, mock_agent, mock_llm, tmp_path):
    from lingtai.cli import load_init, build_agent
    _write_init(tmp_path)
    data = load_init(tmp_path)
    build_agent(data, tmp_path)

    mock_llm.assert_called_once_with(
        provider="anthropic",
        model="test-model",
        api_key="test-key",
        base_url=None,
    )
    mock_mail.assert_called_once_with(working_dir=tmp_path)
    mock_agent.assert_called_once()
    call_kwargs = mock_agent.call_args
    assert call_kwargs.kwargs["agent_name"] == "test-agent"
    assert call_kwargs.kwargs["working_dir"] == tmp_path
    assert call_kwargs.kwargs["covenant"] == "Be helpful."
    assert call_kwargs.kwargs["memory"] == "I remember nothing."
    assert call_kwargs.kwargs["streaming"] is False


def test_pid_file_written_and_cleaned(tmp_path):
    from lingtai.cli import write_pid, remove_pid
    write_pid(tmp_path)
    pid_file = tmp_path / ".agent.pid"
    assert pid_file.is_file()
    assert pid_file.read_text().strip() == str(os.getpid())
    remove_pid(tmp_path)
    assert not pid_file.is_file()
```

- [ ] **Step 2: Run tests to verify they fail**

Run: `python -m pytest tests/test_cli.py -v`
Expected: FAIL — `ModuleNotFoundError: No module named 'lingtai.cli'`

- [ ] **Step 3: Write the CLI module**

```python
# src/lingtai/cli.py
"""lingtai run <working_dir> — boot an agent from init.json."""
from __future__ import annotations

import argparse
import json
import os
import signal
import sys
from pathlib import Path

from lingtai.init_schema import validate_init
from lingtai.llm.service import LLMService
from lingtai.agent import Agent
from lingtai_kernel.services.mail import FilesystemMailService
from lingtai_kernel.config import AgentConfig


def load_init(working_dir: Path) -> dict:
    """Read and validate init.json from working_dir. Exits on error."""
    init_path = working_dir / "init.json"
    if not init_path.is_file():
        print(f"error: {init_path} not found", file=sys.stderr)
        sys.exit(1)

    try:
        data = json.loads(init_path.read_text())
    except (json.JSONDecodeError, OSError) as e:
        print(f"error: failed to read {init_path}: {e}", file=sys.stderr)
        sys.exit(1)

    try:
        validate_init(data)
    except ValueError as e:
        print(f"error: invalid init.json: {e}", file=sys.stderr)
        sys.exit(1)

    return data


def build_agent(data: dict, working_dir: Path) -> Agent:
    """Construct LLMService, MailService, and Agent from validated init data."""
    m = data["manifest"]
    llm = m["llm"]

    service = LLMService(
        provider=llm["provider"],
        model=llm["model"],
        api_key=llm["api_key"],
        base_url=llm["base_url"],
    )

    mail_service = FilesystemMailService(working_dir=working_dir)

    config = AgentConfig(
        provider=llm["provider"],
        model=llm["model"],
        vigil=m["vigil"],
        soul_delay=m["soul_delay"],
        max_turns=m["max_turns"],
        language=m["language"],
    )

    agent = Agent(
        service,
        agent_name=m["agent_name"],
        working_dir=working_dir,
        mail_service=mail_service,
        config=config,
        admin=m["admin"],
        streaming=m["streaming"],
        covenant=data["covenant"],
        memory=data["memory"],
        capabilities=m["capabilities"],
    )

    return agent


def write_pid(working_dir: Path) -> None:
    (working_dir / ".agent.pid").write_text(str(os.getpid()))


def remove_pid(working_dir: Path) -> None:
    pid_file = working_dir / ".agent.pid"
    if pid_file.is_file():
        pid_file.unlink()


def run(working_dir: Path) -> None:
    """Full boot sequence: load, build, start, block, stop."""
    data = load_init(working_dir)
    agent = build_agent(data, working_dir)

    write_pid(working_dir)

    # Signal handlers: SIGTERM/SIGINT → touch .quell and unblock main thread
    quell_file = working_dir / ".quell"

    def _signal_handler(signum, frame):
        quell_file.touch()
        agent._shutdown.set()

    signal.signal(signal.SIGTERM, _signal_handler)
    signal.signal(signal.SIGINT, _signal_handler)

    try:
        agent.start()
        # Block until the agent shuts down (vigil, .quell, or external stop)
        agent._shutdown.wait()
    finally:
        try:
            agent.stop(timeout=10.0)
        except Exception:
            pass
        remove_pid(working_dir)


def main() -> None:
    parser = argparse.ArgumentParser(
        prog="lingtai",
        description="lingtai agent runtime",
    )
    sub = parser.add_subparsers(dest="command")

    run_parser = sub.add_parser("run", help="Boot an agent from init.json in working_dir")
    run_parser.add_argument("working_dir", type=Path, help="Agent working directory containing init.json")

    args = parser.parse_args()

    if args.command == "run":
        working_dir = args.working_dir.resolve()
        if not working_dir.is_dir():
            print(f"error: {working_dir} is not a directory", file=sys.stderr)
            sys.exit(1)
        run(working_dir)
    else:
        parser.print_help()
        sys.exit(1)


if __name__ == "__main__":
    main()
```

- [ ] **Step 4: Run tests to verify they pass**

Run: `python -m pytest tests/test_cli.py -v`
Expected: All 6 tests PASS

- [ ] **Step 5: Smoke-test the module**

Run: `python -c "from lingtai.cli import main; print('OK')"`
Expected: `OK`

- [ ] **Step 6: Commit**

```bash
git add src/lingtai/cli.py tests/test_cli.py
git commit -m "feat: add lingtai run CLI entrypoint"
```

---

### Task 3: Register Console Script in pyproject.toml

**Files:**
- Modify: `pyproject.toml`

- [ ] **Step 1: Read current pyproject.toml to find insertion point**

Look for `[project]` section. Add `[project.scripts]` after it.

- [ ] **Step 2: Add the console script entry**

Add to `pyproject.toml`:

```toml
[project.scripts]
lingtai = "lingtai.cli:main"
```

- [ ] **Step 3: Reinstall the package in editable mode**

Run: `pip install -e .`

- [ ] **Step 4: Verify the CLI is available**

Run: `lingtai --help`
Expected: Shows help with `run` subcommand

Run: `lingtai run --help`
Expected: Shows help with `working_dir` argument

- [ ] **Step 5: Commit**

```bash
git add pyproject.toml
git commit -m "feat: register lingtai console script in pyproject.toml"
```

---

### Task 4: Integration Test — Full Boot and Shutdown

**Files:**
- Create: `tests/test_cli_integration.py`

- [ ] **Step 1: Write integration test**

```python
# tests/test_cli_integration.py
"""Integration test: lingtai run boots an agent and shuts down via .quell."""
import json
import time
from pathlib import Path
from unittest.mock import MagicMock, patch

from lingtai.cli import load_init, build_agent, write_pid, remove_pid


def _write_init(tmp_path: Path) -> None:
    data = {
        "manifest": {
            "agent_name": "integration-test",
            "language": "en",
            "llm": {
                "provider": "gemini",
                "model": "test-model",
                "api_key": "fake-key",
                "base_url": None,
            },
            "capabilities": {},
            "vigil": 10,
            "soul_delay": 5,
            "max_turns": 5,
            "admin": {},
            "streaming": False,
        },
        "covenant": "You are a test agent.",
        "memory": "",
    }
    (tmp_path / "init.json").write_text(json.dumps(data))


def _make_mock_service():
    """Build a mock LLMService that satisfies BaseAgent's contract."""
    svc = MagicMock()
    svc.provider = "gemini"
    svc.model = "test-model"
    adapter = MagicMock()
    adapter.model_family = "gemini"
    svc.get_adapter.return_value = adapter
    return svc


@patch("lingtai.cli.LLMService")
def test_full_boot_and_quell_shutdown(mock_llm_cls, tmp_path):
    """Boot agent, touch .quell, verify clean shutdown."""
    _write_init(tmp_path)
    mock_llm_cls.return_value = _make_mock_service()

    data = load_init(tmp_path)
    agent = build_agent(data, tmp_path)
    write_pid(tmp_path)

    agent.start()

    # Verify agent is running
    assert (tmp_path / ".agent.pid").is_file()
    assert (tmp_path / ".agent.json").is_file()

    # Touch .quell to trigger shutdown via heartbeat
    (tmp_path / ".quell").touch()

    # Wait for heartbeat to pick it up (beats every 1s)
    time.sleep(3)

    agent.stop(timeout=5.0)
    remove_pid(tmp_path)

    assert not (tmp_path / ".agent.pid").is_file()
```

- [ ] **Step 2: Run integration test**

Run: `python -m pytest tests/test_cli_integration.py -v --timeout=30`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/test_cli_integration.py
git commit -m "test: add integration test for lingtai run boot and shutdown"
```

---

### Task 5: Verify End-to-End with a Real init.json

Manual verification (not automated). This task confirms the CLI works as a user would use it.

- [ ] **Step 1: Create a test agent directory**

```bash
mkdir -p /tmp/lingtai-test-agent
```

- [ ] **Step 2: Write an init.json**

```bash
cat > /tmp/lingtai-test-agent/init.json << 'EOF'
{
  "manifest": {
    "agent_name": "test-runner",
    "language": "en",
    "llm": {
      "provider": "anthropic",
      "model": "claude-sonnet-4-20250514",
      "api_key": null,
      "base_url": null
    },
    "capabilities": {},
    "vigil": 30,
    "soul_delay": 10,
    "max_turns": 5,
    "admin": {},
    "streaming": false
  },
  "covenant": "You are a test agent. Say hello.",
  "memory": ""
}
EOF
```

- [ ] **Step 3: Run the agent**

```bash
lingtai run /tmp/lingtai-test-agent
```

Expected: Agent starts, acquires lock, writes `.agent.json` and `.agent.pid`. Blocks.

- [ ] **Step 4: Verify files created**

In another terminal:
```bash
ls -la /tmp/lingtai-test-agent/
cat /tmp/lingtai-test-agent/.agent.json
cat /tmp/lingtai-test-agent/.agent.pid
```

- [ ] **Step 5: Shut down via .quell**

```bash
touch /tmp/lingtai-test-agent/.quell
```

Expected: Agent exits cleanly, `.agent.pid` removed.

- [ ] **Step 6: Clean up**

```bash
rm -rf /tmp/lingtai-test-agent
```
