# Contributing to LingTai

This guide covers how to make changes to each component of the LingTai project.

## General principles

1. **Filesystem-only IPC.** The TUI, portal, and kernel communicate exclusively through files. If you need cross-process communication, write a file and let the other side poll.
2. **Anatomy updates are part of the code change.** If your change moves, renames, splits, merges, or deletes a file/function/class cited by an `ANATOMY.md`, update the anatomy in the **same commit**. See the `lingtai-kernel-anatomy` and `lingtai-tui-anatomy` skills for the full convention.
3. **Three-locale rule.** Adding an i18n key means updating all three of `en.json`, `zh.json`, `wen.json` in both `tui/i18n/` and (where applicable) `portal/i18n/`. Missing translations render as the raw key on screen — they don't fall back.
4. **Binary naming.** The TUI binary is `lingtai-tui`, never `lingtai`. `lingtai` is the Python agent CLI inside the runtime venv.
5. **Human-facing summaries default to self-contained HTML.** When you report a non-trivial dev/research result to a human (design proposal, multi-PR roll-up, investigation write-up, postmortem, PR summary attachment), the deliverable is a single `.html` file with inline CSS, no remote assets, no build step — open it via `file://`. Plain text/Markdown stays for short summaries that *link to* the HTML. The required structure (TL;DR, baseline, what-was-done with diff snippets, validation, risks/decisions, next steps, source index) and hard rules (self-contained, inline assets, dark-theme default) live in the parent `SKILL.md` under "Human-facing reports default to self-contained HTML". Reference implementation: `reports/im-optimization-summary-2026-05-18.html` in the codex-gpt5.5 workspace.

## Changing the TUI (`tui/`)

### Where to look

- **Screens / UI models:** `tui/internal/tui/` — one file per screen (Bubble Tea convention)
- **Presets:** `tui/internal/preset/` — `preset.go` (~1900 lines) handles load/save/list
- **Migrations:** `tui/internal/migrate/` — append a new `m<NNN>_<name>.go` file
- **Filesystem access:** `tui/internal/fs/` — read-only window into agent working directories
- **Subprocess launch:** `tui/internal/process/` — how agents are spawned
- **i18n:** `tui/i18n/` — en/zh/wen JSON tables

### Build and test

```bash
cd ~/Documents/GitHub/lingtai/tui
make build                    # builds to tui/bin/lingtai-tui
make cross-compile            # all platforms
go test ./...                 # run tests
```

### Adding a migration

1. Create `tui/internal/migrate/m<NNN>_<name>.go` exporting `func migrate<Name>(lingtaiDir string) error`.
2. Register in `migrate.go`: append to the `migrations` slice, bump `CurrentVersion`.
3. **Also bump `CurrentVersion` in `portal/internal/migrate/migrate.go`** — the TUI and portal share the `meta.json` version space.
4. If the migration touches shared on-disk state (init.json schema, preset paths), implement it in both packages with identical logic.
5. If it's TUI-only, add a no-op stub `Fn: func(_ string) error { return nil }` in the portal registry to preserve the version slot.

### Adding a new screen

1. Create a new Bubble Tea model in `tui/internal/tui/`.
2. Wire it into the main app model's `Update` function.
3. Add i18n keys to all three locale files.
4. Handle `tea.PasteMsg` forwarding if the screen has text inputs (see gotchas).

## Changing the portal (`portal/`)

### Where to look

- **API handlers:** `portal/internal/api/` — `server.go`, `handlers.go`, `replay.go`
- **Filesystem access:** `portal/internal/fs/` — same shape as TUI's, portal-tailored
- **Web frontend:** `portal/web/src/` — React 19 + TypeScript + Vite
- **Migrations:** `portal/internal/migrate/` — shares version space with TUI
- **i18n:** `portal/i18n/` — independent of TUI's i18n, same three-locale rule

### Build and test

```bash
cd ~/Documents/GitHub/lingtai/portal
make build                    # builds web frontend + Go binary
# Output: portal/bin/lingtai-portal
```

The `make build` pipeline: `npm install` → `npm run build` (in `web/`) → `go build` (embeds `web/dist/` via `embed.go`).

### Changing the web frontend

1. Edit files in `portal/web/src/`.
2. `cd portal/web && npm run build` to rebuild the frontend.
3. `cd portal && make build` to embed the new frontend into the Go binary.
4. The frontend is embedded at compile time via `//go:embed all:web/dist` in `portal/embed.go`.

### Migrations

Same contract as TUI — see "Adding a migration" above. Portal-only migrations get a no-op stub in the TUI registry.

## Changing the kernel (`lingtai-kernel/`)

### Where to look

- **Agent runtime:** `src/lingtai_kernel/` — turn loop, lifecycle, tool dispatch, mailbox, soul/molt
- **Wrapper (CLI + services):** `src/lingtai/` — MCP, FileIO, Vision, Search, CLI
- **Intrinsics:** `src/lingtai_kernel/intrinsics/` — email, soul, system, psyche, codex, etc.
- **Skills:** `src/lingtai/intrinsic_skills/` — bundled skill manuals

The kernel-root anatomy at `src/lingtai_kernel/ANATOMY.md` is the entry point for navigating the source. See the `lingtai-kernel-anatomy` skill for the convention.

### Build and test

```bash
cd ~/Documents/GitHub/lingtai-kernel
pip install -e .              # editable install
python -m pytest              # run tests
```

With the TUI's runtime venv:
```bash
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-kernel \
    -p ~/.lingtai-tui/runtime/venv
```

Changes to the kernel source are reflected immediately in the running agent — no rebuild needed (editable install).

### Auto-upgrader gotcha

The TUI's auto-upgrader (`tui/main.go:283`, `config.CheckUpgrade`) compares `lingtai.__version__` to PyPI's latest. If your local source's `pyproject.toml` version is **lower** than PyPI's, the upgrader replaces the editable install with the PyPI wheel — silently undoing dev mode.

**Prevention:** Ensure `lingtai-kernel/pyproject.toml` `version` is `>=` PyPI's latest. After a release bump, pull the kernel repo so your local source matches.

**Recovery:**
```bash
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-kernel \
    -p ~/.lingtai-tui/runtime/venv
```

## Changing MCP addons

Each addon (imap, telegram, feishu, wechat) is a separate repo with its own MCP server. See the `mcp-manual` skill for the registration workflow.

```bash
# Install in editable mode
~/.local/bin/uv pip install -e ~/Documents/GitHub/lingtai-imap \
    -p ~/.lingtai-tui/runtime/venv

# Register the MCP server
# See mcp-manual skill for the workflow
```

## Changing skills

Skills live in two places:

| Location | Who owns it | Editable? |
|---|---|---|
| `<agent>/.library/intrinsic/` | CLI-managed. Wiped and rewritten on every refresh. | No — edits will be erased. |
| `<agent>/.library/custom/` | You. CLI never touches this. | Yes. |
| `../.library_shared/` | Network-shared. Add with `cp -r`, edit with admin permission. | Admin only. |
| `~/.lingtai-tui/utilities/` | TUI-shipped utilities. | Depends on the skill. |

To author a new skill, see the `library-manual` skill for the full workflow (frontmatter schema, template, validator, publishing).

## Anatomy maintenance

Every `ANATOMY.md` must be updated in the same commit as the code change it describes. The rules:

- **Every named symbol in Components has a `file:line` citation.**
- **Citations are line ranges, not paragraphs.**
- **Every citation has been verified** — open the cited line and confirm.
- **Cross-references use kernel-root-relative paths.**
- **No leaf stubs, no paraphrase.**

For the full convention, see the `lingtai-kernel-anatomy` skill (Python) or `lingtai-tui-anatomy` skill (Go).

### Cheap mechanical check (Go)

```bash
python - <<'PY'
import pathlib, re
root = pathlib.Path("tui")
for anatomy in root.rglob("ANATOMY.md"):
    text = anatomy.read_text()
    for rel, line in re.findall(r"`?([A-Za-z0-9_./-]+\.(?:go|ts|tsx)):(\d+)", text):
        path = root / rel if not rel.startswith("tui/") else pathlib.Path(rel)
        if not path.exists():
            print(f"{anatomy}: missing citation target {rel}:{line}")
            continue
        n = len(path.read_text().splitlines())
        if int(line) > n:
            print(f"{anatomy}: out-of-range citation {rel}:{line} > {n}")
PY
```

### Cheap mechanical check (Python)

```bash
python - <<'PY'
import pathlib, re
root = pathlib.Path("src/lingtai_kernel")
for anatomy in root.rglob("ANATOMY.md"):
    text = anatomy.read_text()
    for rel, line in re.findall(r"`?([A-Za-z0-9_./-]+\.py):(\d+)", text):
        path = root / rel if not rel.startswith("src/") else pathlib.Path(rel)
        if not path.exists():
            print(f"{anatomy}: missing citation target {rel}:{line}")
            continue
        n = len(path.read_text().splitlines())
        if int(line) > n:
            print(f"{anatomy}: out-of-range citation {rel}:{line} > {n}")
PY
```
