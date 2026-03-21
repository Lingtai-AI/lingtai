# Project-Based Orchestration Structure — Implementation Plan

> **SUPERSEDED** by `2026-03-20-lingtai-cli-redesign.md` — cwd-based `.lingtai/` approach replaces the `~/.lingtai/projects/` approach.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Restructure the lingtai daemon so each orchestration is a "project" under `~/.lingtai/projects/<name>/`, with project-level configs and per-agent covenants.

**Architecture:** The `lingtai` binary becomes a single entry point for setup, launch, stop, and management. Each project is a self-contained orchestration with `configs/` (config.json, model.json, .env, bash_policy.json) and per-agent working directories (each with its own covenant.md). The wizard creates new projects; the launcher reads project configs and starts the Python app.

**Tech Stack:** Go (daemon), Python (app entry point)

---

## Current vs Target Layout

```
# CURRENT
~/.lingtai/
  config.json
  model.json
  covenant.md
  .env
  bash_policy.json
  orchestrator/          ← agent working dir

# TARGET
~/.lingtai/
  projects/
    EB1A-prep/           ← project name
      configs/
        config.json      ← project config (no more base_dir/covenant fields)
        model.json
        .env
        bash_policy.json
      <agent_id>/        ← 本我 working dir
        covenant.md      ← per-agent
        system/
        mailbox/
        delegates/
      <agent_id>/        ← 他我 working dir
        covenant.md
        ...
```

## Key Changes

- `config.json` drops `base_dir` (it IS the project dir) and `covenant` (per-agent, not project-level)
- `config.json` gains `language` propagation to Python `AgentConfig`
- Wizard `outputDir` becomes `~/.lingtai/projects/<project_name>/configs/`
- Default covenant written to 本我's working dir, not configs/
- `lingtai` binary gets subcommands: `setup`, `start`, `stop`, `list`, `manage`
- Python `app/__init__.py` resolves paths relative to project dir, not config dir

---

### Task 1: Restructure Go Config Loader

**Files:**
- Modify: `daemon/internal/config/loader.go`

- [ ] **Step 1: Add Language and drop Covenant from Config struct**

```go
type Config struct {
	Model      ModelConfig    `json:"-"`
	IMAP       IMAPConfig     `json:"imap,omitempty"`
	Telegram   TelegramConfig `json:"telegram,omitempty"`
	CLI        bool           `json:"cli"`
	AgentName  string         `json:"agent_name"`
	BashPolicy string         `json:"bash_policy,omitempty"`
	MaxTurns   int            `json:"max_turns"`
	AgentPort  int            `json:"agent_port"`
	CLIPort    int            `json:"cli_port,omitempty"`
	Language   string         `json:"language"`

	// Internal
	ConfigDir  string `json:"-"`
	ProjectDir string `json:"-"` // parent of ConfigDir
}
```

- [ ] **Step 2: Update Load() to derive ProjectDir**

In `Load()`, after resolving `configDir`:
```go
cfg.ProjectDir = filepath.Dir(cfg.ConfigDir) // configs/ -> project dir
```

Remove `BaseDir` handling (expand ~, defaults). `ProjectDir` replaces it.

- [ ] **Step 3: Update WorkingDir() to use ProjectDir**

```go
func (c *Config) WorkingDir() string {
	return filepath.Join(c.ProjectDir, c.AgentName)
}
```

- [ ] **Step 4: Verify Go builds**

Run: `cd daemon && go build ./...`
Expected: clean build

- [ ] **Step 5: Commit**

```bash
git add daemon/internal/config/loader.go
git commit -m "refactor(config): use project-based layout, add Language field"
```

---

### Task 2: Restructure Wizard Output

**Files:**
- Modify: `daemon/internal/setup/wizard.go`
- Modify: `daemon/main.go`

- [ ] **Step 1: Replace base_dir wizard field with project name**

In `StepGeneral` fields, replace the base_dir field with a project name field. The project name determines the directory under `~/.lingtai/projects/`.

- [ ] **Step 2: Update writeConfig() output paths**

Change `m.outputDir` usage so that:
- Config files (config.json, model.json, .env, bash_policy.json) write to `~/.lingtai/projects/<name>/configs/`
- Default covenant writes to the 本我's working dir: `~/.lingtai/projects/<name>/<agent_name>/covenant.md`

In `writeConfig()`:
```go
configsDir := filepath.Join(m.outputDir, "configs")
os.MkdirAll(configsDir, 0755)

// Write config.json, model.json, .env to configsDir
configPath := filepath.Join(configsDir, "config.json")

// Write covenant to agent working dir
agentDir := filepath.Join(m.outputDir, agentName)
os.MkdirAll(agentDir, 0755)
covenantPath := filepath.Join(agentDir, "covenant.md")
```

- [ ] **Step 3: Remove base_dir and covenant from config.json output**

In the `cfg` map that gets written to config.json, remove `"base_dir"` and `"covenant"` keys. Add `"language"` (already there).

- [ ] **Step 4: Update main.go paths**

```go
func main() {
	home, _ := os.UserHomeDir()
	projectsDir := filepath.Join(home, ".lingtai", "projects")
	// ...
}
```

For `lingtai setup`, pass the project directory (under projects/) as outputDir.

- [ ] **Step 5: Verify Go builds**

Run: `cd daemon && go build ./...`
Expected: clean build

- [ ] **Step 6: Commit**

```bash
git add daemon/internal/setup/wizard.go daemon/main.go
git commit -m "refactor(wizard): write to projects/<name>/configs/ layout"
```

---

### Task 3: Add Subcommands to main.go

**Files:**
- Modify: `daemon/main.go`

- [ ] **Step 1: Add start subcommand**

```go
case "start":
	if len(positional) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: lingtai start <project>\n")
		os.Exit(1)
	}
	projectName := positional[1]
	projectDir := filepath.Join(projectsDir, projectName)
	configPath := filepath.Join(projectDir, "configs", "config.json")
	// Load config and start Python agent
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	proc, err := agent.Start(agent.StartOptions{
		ConfigPath: configPath,
		AgentPort:  cfg.AgentPort,
		WorkingDir: cfg.WorkingDir(),
		Headless:   true,
	})
	// ...
```

- [ ] **Step 2: Add stop subcommand**

Read PID from agent working dir, send SIGINT.

- [ ] **Step 3: Add list subcommand**

Scan `~/.lingtai/projects/`, show each project name and whether an agent.pid exists.

- [ ] **Step 4: Update no-arg behavior**

When run with no args:
- If no projects exist, run setup
- If projects exist, show list

- [ ] **Step 5: Update help text**

```
lingtai              List projects (or setup if none)
lingtai setup        Create a new project
lingtai start <name> Start a project
lingtai stop <name>  Stop a project
lingtai list         List all projects and status
lingtai manage       Show running agents
```

- [ ] **Step 6: Verify Go builds**

Run: `cd daemon && go build ./...`

- [ ] **Step 7: Commit**

```bash
git add daemon/main.go
git commit -m "feat(cli): add start/stop/list subcommands for project management"
```

---

### Task 4: Update Python App Entry Point

**Files:**
- Modify: `app/__init__.py`
- Modify: `app/config.py`

- [ ] **Step 1: Update config defaults — remove base_dir**

In `app/config.py`, remove `"base_dir"` from `_DEFAULTS`. The Python app derives base_dir from the config path: `config_path -> configs/ -> project_dir` (project_dir IS the base_dir).

```python
_DEFAULTS = {
    "agent_name": "orchestrator",
    "max_turns": 50,
    "agent_port": 8501,
    "cli": False,
}
```

- [ ] **Step 2: Derive base_dir from config_path in load_config()**

```python
def load_config(config_path: str) -> dict:
    path = Path(config_path)
    config_dir = path.parent          # .../configs/
    project_dir = config_dir.parent   # .../EB1A-prep/

    # ...existing loading...

    cfg["base_dir"] = str(project_dir)
    cfg["config_dir"] = str(config_dir)
    return cfg
```

- [ ] **Step 3: Propagate language to AgentConfig**

In `app/__init__.py`, line ~214:

```python
agent = Agent(
    agent_name=agent_name,
    service=llm,
    mail_service=mail_service,
    logging_service=logging_service,
    config=AgentConfig(
        max_turns=cfg.get("max_turns", 50),
        language=cfg.get("language", "en"),
    ),
    base_dir=base_dir,
    streaming=cfg.get("streaming", True),
    covenant=cfg.get("covenant", _DEFAULT_COVENANT),
    capabilities=capabilities,
    addons=addons,
)
```

- [ ] **Step 4: Load covenant from agent working dir**

Instead of `cfg.get("covenant", _DEFAULT_COVENANT)`, load from the agent's working dir:

```python
# Resolve covenant — per-agent, in working dir
covenant_path = base_dir / agent_name / "covenant.md"
if covenant_path.is_file():
    covenant = covenant_path.read_text(encoding="utf-8")
else:
    covenant = _DEFAULT_COVENANT
```

- [ ] **Step 5: Smoke-test Python import**

Run: `python -c "import lingtai"`
Expected: clean

- [ ] **Step 6: Commit**

```bash
git add app/__init__.py app/config.py
git commit -m "refactor(app): derive base_dir from config path, propagate language"
```

---

### Task 5: Migration — Existing Configs

**Files:**
- Modify: `daemon/main.go` (add migrate hint)

- [ ] **Step 1: Detect old-style config and print migration hint**

If `~/.lingtai/config.json` exists (old layout), print:

```
Old config layout detected at ~/.lingtai/config.json
Run 'lingtai setup' to create a new project, then remove the old files.
```

- [ ] **Step 2: Commit**

```bash
git add daemon/main.go
git commit -m "feat(cli): detect old config layout and suggest migration"
```

---

### Task 6: Update Go Tests

**Files:**
- Modify: `daemon/internal/config/loader_test.go`
- Modify: `daemon/internal/setup/tests_test.go`

- [ ] **Step 1: Update config loader tests for new layout**

Tests should create configs in `<tmp>/configs/config.json` and verify `ProjectDir` is derived correctly.

- [ ] **Step 2: Update wizard tests if any reference old paths**

- [ ] **Step 3: Run all Go tests**

Run: `cd daemon && go test ./...`
Expected: all pass

- [ ] **Step 4: Commit**

```bash
git add daemon/internal/config/ daemon/internal/setup/
git commit -m "test: update tests for project-based config layout"
```
