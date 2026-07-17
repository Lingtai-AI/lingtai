# Migration history

This file is a Git-versioned prose history and decision record. It is not a
runtime migration registry, executable version chain, or stored progress ledger.
Git commits and release tags preserve this history; no release process replaces
this document as runtime state.

## Runtime retirement (TUI v4.2)

Jason's option-2 decision retires the TUI/Portal project migration runtime.
Production startup, project creation, launcher paths, diagnostics, and Portal
bootstrap do not call `migrate.Run`, `migrate.StampCurrent`, or record project
migration progress. Existing `.lingtai/` files continue as stored: there is no
automatic migration, version check, compatibility preflight, stamp, or generic
`init.json` rewrite. The kernel reader/Nudge diagnoses canonical drift, and an
Agent or human makes an explicit edit against the kernel-owned canonical
`init.jsonc` when repair is required.

The retained `m001`–`m039` source and tests remain in both migration packages as
historical Git evidence and intentionally retained unit coverage. They are not a
production startup component. `CurrentVersion = 39` and the registry helpers
remain only for that historical package/API surface; production no longer
consults or advances them. The legacy addon-comment progress flag is likewise
not written by production. This PR does not delete the historical package,
source files, tests, or this document.

## PR #667 — proposed m040 `bash` → `shell` rewrite (superseded)

PR #667 initially proposed an automatic m040 migration that would scan existing
per-agent `init.json` files, rewrite a legacy `bash` capability to canonical
`shell`, and advance the project migration version. A conflict preflight was
also proposed to scan those files before historical migrations.

The final v4.2 decision superseded that runtime design. Canonical shape,
compatibility semantics, real reader behavior, and Nudge belong to the kernel.
Legacy input is diagnosed through the kernel-owned reader/Nudge path; an
Agent/human explicitly edits the source file when repair is required. TUI
explicit setup, preset/editor, recipe, rehydration, and settings writes retain
bounded JSON/conflict protections and emit canonical `shell`; they are explicit
writers, not a startup migration path.

Accordingly, the runtime m040 implementation, m040 tests, and alias-conflict
preflight artifacts from this branch were removed. The branch returns both
binary migration registries to their established baseline version 39. Exactly
six authorized m040/preflight paths are deleted; the retained `m001`–`m039`
package and tests remain untouched as historical evidence.

## Durable ownership record

The TUI template and example remain consumer copies. Their current local bytes
and equality are preserved in this worktree while the kernel Luna worktree is
still establishing the authoritative canonical `init.jsonc`; this PR does not
finalize a cross-repository byte copy or pointer Contract. The parent integration
must perform that exact comparison after kernel canonical bytes and links are
reviewed.
