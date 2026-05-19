# Intrinsics & Capabilities Tool-Schema Strict-Mode Scan

**Date:** 2026-05-19
**Repos affected:** upstream `lingtai` Python package + `lingtai_kernel`
(installed at `~/.lingtai-tui/runtime/venv/lib/python3.13/site-packages/`,
not the TUI repo).
**Companion to:** `cascade-skill-and-sentinel-ordering-patch.md` (Finding D)
where this regression first surfaced as a `shouyi` AED-trip on `gpt-5.5`,
and the per-tool fixes filed against `psyche` and `avatar`.

## TL;DR

OpenAI's structured-output / strict-mode schema validator (used by
`gpt-5*`, `gpt-5.5`, `deepseek-flash`, and any provider that opts into
strict-tool-schema validation) rejects function-tool schemas that have
**any of `oneOf` / `anyOf` / `allOf` / `enum` / `not` at the top level**:

```
400: Invalid schema for function 'psyche': schema must have type
'object' and not have 'oneOf'/'anyOf'/'allOf'/'enum'/'not' at the top
level.
```

A single offending intrinsic takes down every turn the agent tries to
use that tool — AED retries 3×, then `preset_auto_fallback` reverts the
preset (see `lingtai-preset-swap-silent-revert-patch.md` for the full
amplification chain). Two tools shipped with the offending construct:

| Tool | File | Construct |
|---|---|---|
| `psyche` | `lingtai_kernel/intrinsics/psyche/__init__.py` | top-level `allOf` of conditional `if/then` clauses pinning `(object, action)` valid pairs |
| `avatar` | `lingtai/core/avatar/__init__.py` | top-level `allOf` of conditional `if/then` clauses pinning per-action `required` fields |

Both have been patched (constraint moved into runtime validator, schema
left as bare `type: object` + `properties` + `required`). This document
covers the **systemic scan** that should accompany the per-tool patches:
make sure no third tool slipped through, and add a CI guard so the next
intrinsic doesn't regress us.

## Why "just fix `psyche`" isn't enough

The pattern that caused the regression isn't psyche-specific. It's the
intersection of three things every intrinsic author hits:

1. JSON Schema's `if/then` is the natural way to express
   "field X is required only when field Y has value Z" — a
   common shape for verb-driven tools (`action: <verb>` then required
   args differ per verb).
2. JSON Schema *forbids* `if/then` at the top level of a `type: object`
   schema unless wrapped in `allOf` (the `if/then` keywords don't
   compose with sibling validators directly the way one might expect).
3. So every author who tries to express per-verb required fields
   declaratively in the schema reaches for `allOf: [{if, then}, ...]`.
   Both psyche and avatar arrived at this independently.

The fix landing for those two tools — *delete the schema-level
constraint, enforce in the handler* — is the right resolution every
time, but it's invisible to any future intrinsic author who reads the
existing intrinsics, sees no `allOf`, and assumes the way to express
their constraint is to add it back. Hence the lint.

## Static AST scan (run anywhere, no runtime deps)

The scan walks every `.py` under `lingtai_kernel/intrinsics/`,
`lingtai/capabilities/`, and `lingtai/core/`, parses to AST, and flags
any dict literal whose own `"type"` key is the constant `"object"` and
which also contains any of `"oneOf"`, `"anyOf"`, `"allOf"`, `"enum"`,
`"not"` as a sibling key. That's exactly the construct strict mode
refuses, and only that construct — the same words inside a
nested `properties.<field>.enum` are legal (and used heavily by `soul`
and others).

```python
"""Lint: no top-level allOf/anyOf/oneOf/enum/not on a type:object schema.

OpenAI strict-mode tool schemas (gpt-5*, deepseek-flash, and friends)
reject these constructs at the top level. See
discussions/intrinsics-strict-schema-scan.md for context.

Run: python scripts/lint_strict_tool_schemas.py
Exit: 0 if clean, 1 if any forbidden construct found.

Or as pytest: place under tests/ and the assertion turns each finding
into a test failure with the exact file:line and key.
"""
import ast
import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]  # repo root
ROOTS_TO_SCAN = [
    ROOT / "src" / "lingtai_kernel" / "intrinsics",
    ROOT / "src" / "lingtai" / "capabilities",
    ROOT / "src" / "lingtai" / "core",
]
FORBIDDEN_AT_TOP = {"oneOf", "anyOf", "allOf", "enum", "not"}


class StrictModeViolationVisitor(ast.NodeVisitor):
    def __init__(self, path: pathlib.Path) -> None:
        self.path = path
        self.findings: list[tuple[int, str]] = []

    def visit_Dict(self, node: ast.Dict) -> None:
        keys: list[object | None] = []
        for k in node.keys:
            keys.append(k.value if isinstance(k, ast.Constant) else None)
        if "type" in keys:
            type_node = node.values[keys.index("type")]
            if isinstance(type_node, ast.Constant) and type_node.value == "object":
                for forbidden in FORBIDDEN_AT_TOP:
                    if forbidden in keys:
                        self.findings.append((node.lineno, forbidden))
        self.generic_visit(node)


def scan_file(path: pathlib.Path) -> list[tuple[int, str]]:
    try:
        tree = ast.parse(path.read_text(encoding="utf-8"))
    except SyntaxError:
        return []
    v = StrictModeViolationVisitor(path)
    v.visit(tree)
    return v.findings


def main() -> int:
    failures: list[str] = []
    n_files = 0
    for root in ROOTS_TO_SCAN:
        if not root.exists():
            continue
        for p in root.rglob("*.py"):
            if ".bak" in p.name or "__pycache__" in p.parts:
                continue
            n_files += 1
            for lineno, forbidden in scan_file(p):
                rel = p.relative_to(ROOT)
                failures.append(
                    f"{rel}:{lineno}  top-level type=object dict has "
                    f"forbidden key {forbidden!r} (OpenAI strict-mode "
                    f"validator rejects this; move the constraint into "
                    f"the runtime handler)"
                )
    if failures:
        print("✗ strict-tool-schema scan found violations:", file=sys.stderr)
        for f in failures:
            print("  " + f, file=sys.stderr)
        return 1
    print(f"✓ strict-tool-schema scan clean ({n_files} files)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
```

The scan deliberately uses AST instead of importing the modules:
intrinsics import their own deps at module-load time (httpx, anthropic,
psyche-internal models), and a lint that can run in a stripped
CI container with just stdlib is friction-free. The failure mode the
lint is preventing is a JSON-shape regression, not a runtime regression
— AST is the right level.

### Pytest variant (drop-in)

For the `tests/` tree:

```python
# tests/test_strict_tool_schemas.py
import pytest
from pathlib import Path
from scripts.lint_strict_tool_schemas import scan_file, ROOTS_TO_SCAN, ROOT


@pytest.mark.parametrize("py_file", [
    p for root in ROOTS_TO_SCAN
    if root.exists()
    for p in root.rglob("*.py")
    if ".bak" not in p.name and "__pycache__" not in p.parts
])
def test_no_top_level_strict_violations(py_file):
    findings = scan_file(py_file)
    assert not findings, (
        f"{py_file.relative_to(ROOT)} has top-level type=object dicts "
        f"with forbidden keys: {findings}. OpenAI strict-mode validator "
        f"(gpt-5*, deepseek-flash, …) rejects these. Move the "
        f"constraint into the runtime handler instead."
    )
```

Each offending file becomes its own pytest failure, so CI output points
straight at the regressing file. Adds maybe 2 ms to test-suite runtime
on the current 41-file working set.

## Scan results — May 19 2026

After the `psyche` + `avatar` patches landed, the scan was run against
the installed package tree at
`~/.lingtai-tui/runtime/venv/lib/python3.13/site-packages/`:

```
$ python lint_strict_tool_schemas.py
✓ strict-tool-schema scan clean (41 files)
```

Files scanned (the schema-bearing ones, by directory):

| Directory | Files | Top-level violations |
|---|---:|---:|
| `lingtai_kernel/intrinsics/` (`email`, `psyche`, `soul`, `system`) | 8 | 0 (psyche fixed in same window) |
| `lingtai/capabilities/` (vision, library, file, web, mcp, sub_agents, …) | 25 | 0 |
| `lingtai/core/` (avatar, daemon, kv, mail, …) | 8 | 0 (avatar fixed in same window) |

Notable schema patterns that are **not** flagged because they're legal:

- `intrinsics/soul/__init__.py:87` — `properties.action.enum` (nested
  `enum`, OpenAI strict mode permits this; that's the recommended way
  to constrain a string field to a discrete vocabulary).
- `capabilities/library/__init__.py` — multi-action verb-driven schema,
  but per-action constraints sit in `properties.<field>` annotations
  + handler-side guards, not in a top-level `allOf`.

So the May 19 patch window (psyche + avatar) plugged the only two
existing offenders.

## Recommendation for upstream

1. Land `scripts/lint_strict_tool_schemas.py` from this document under
   the lingtai repo's `scripts/` directory.
2. Wire it into `pyproject.toml#tool.pytest.ini_options` as a collected
   test, or into the `pre-commit` config as a stage, or into the
   GitHub Actions workflow before the unit-test job. Any of those is
   sufficient — they catch the same class of bug.
3. Document in `CONTRIBUTING.md` (or wherever intrinsic-author docs
   live): "tool schemas must satisfy OpenAI strict-mode rules — no
   top-level `allOf`/`anyOf`/`oneOf`/`enum`/`not`. Use a bare
   `type: object` + `properties` + `required`, and enforce per-verb
   constraints in the handler. The lint will fail your PR if you
   forget."

The cost of (1)+(2) is < 50 lines of code and one CI minute. The cost
of *not* having it is what we just paid: a `gpt-5.5`-driven
orchestrator silently AED-tripping on every turn it tries to use
`psyche`, the operator chasing the symptom through three dead-end
hypotheses (mailbox queue, heartbeat, agent state), and finally
finding the schema-validator-400 in `agent.log` on careful read.

## Why the schema-level constraint felt natural and was wrong

A note for future intrinsic authors and reviewers, since "why was this
ever there" is the question that comes up first:

The `psyche` and `avatar` `allOf`/`if`/`then` blocks were genuinely
expressing real constraints — `psyche(object="lingtai", action=…)`
takes different valid `action` values than `psyche(object="pad", …)`,
and `avatar(action="rules", …)` requires `rules_content` while every
other action requires `name`. JSON Schema is supposed to be the right
place to express that.

The thing that's *new* — and is not obvious unless you've been bitten
— is that **strict-mode tool schema is a strict subset of JSON
Schema**. It deliberately excludes `oneOf`/`anyOf`/`allOf`/`enum`/`not`
at the top level because those constructs allow an LLM to emit a tool
call that satisfies one branch of a polymorphic schema but not
another, and OpenAI's validator wants a tool call to either obviously
match or obviously fail — no "valid under interpretation A, invalid
under interpretation B". The branchy validators are still allowed
*nested* inside a `properties.<field>` slot, where they constrain a
single concrete field rather than the top-level shape.

The migration is therefore always: lift the branchy constraint out of
the schema, replace with a `type: object` + `properties` + `required`
that admits the union of all valid shapes, and have the handler raise
a structured error for the per-verb cases. The handler-side error
message is also a better UX than a JSON-Schema validation diagnostic
— it can name the specific verb and missing field rather than
gesturing at "schema branch 2 of 4 failed".

## Files

| Path | Purpose |
|---|---|
| `lingtai_kernel/intrinsics/psyche/__init__.py` | Patched (this window) — comment block above the schema explains why |
| `lingtai/core/avatar/__init__.py` | Patched (this window) — same comment block |
| `scripts/lint_strict_tool_schemas.py` | New — embedded in this document, ready to land |
| `tests/test_strict_tool_schemas.py` | New — embedded in this document, ready to land |
