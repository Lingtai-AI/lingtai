---
related_files:
  - tui/ANATOMY.md
  - tui/internal/tui/ANATOMY.md
  - tui/internal/config/ANATOMY.md
  - tui/internal/doctorreport/report.go
  - tui/internal/doctorreport/report_test.go
  - tui/internal/tui/doctor.go
  - tui/internal/tui/doctor_report_test.go
maintenance: |
  Keep related_files as repo-relative paths to real files. Include neighboring
  ANATOMY.md files so the anatomy graph stays connected rather than isolated;
  anatomy links must be bidirectional. If you create a new ANATOMY.md, copy this
  maintenance field. If you notice drift between this anatomy and the code,
  report it. See lingtai-dev-guide for details.
---

# doctorreport

> **Maintenance:** see the `lingtai-tui-anatomy` skill at `tui/internal/preset/skills/lingtai-tui-anatomy/SKILL.md`. Coding agents update this file in the same commit as code changes.

`doctorreport` serializes a finished `/doctor` run to disk as a small, redacted, shareable bundle. It is deliberately a *writer only*: the TUI runs the diagnostic, builds a `Draft` from exactly the lines the user saw on screen, and hands it here when the user presses the save key. This package performs no diagnostics, makes no network calls, and reads no event logs — so what the user shares can never differ from what the user was shown.

## Components

| Symbol | Citation | Purpose |
|---|---|---|
| `ReportSchemaVersion` / `MetadataSchemaVersion` / `RedactionSchemaVersion` | `tui/internal/doctorreport/report.go:25-27` | the three `lingtai.doctor.*.v1` schema stamps, one per emitted file |
| `Severity` + `SeverityOK` / warn / fail / hint / info | `tui/internal/doctorreport/report.go:34,37-41` | mirrors the visible doctor line kinds so the saved report keeps the on-screen framing |
| `Line` | `tui/internal/doctorreport/report.go:45` | one captured diagnostic line: severity plus text |
| `LLMConfig` | `tui/internal/doctorreport/report.go:53` | the LLM provider/model/endpoint context captured with the run |
| `Draft` | `tui/internal/doctorreport/report.go:65` | the in-memory capture: `GeneratedAt`, optional `AgentName`, `Lines`, `LLM` |
| `redactionMarker` | `tui/internal/doctorreport/report.go:29` | the `[REDACTED]` substitution written in place of secret-shaped values |
| `Write` | `tui/internal/doctorreport/report.go:94` | redacts the draft and writes the three-file bundle into a caller-supplied unique directory |
| `metadataFile` / `redactionFile` | `tui/internal/doctorreport/report.go:72,80` | the `metadata.json` (line counts, LLM, UTC timestamp) and `redaction.json` (applied, marker, rules, note) payloads |

## Connections

- **Called by `tui/internal/tui/doctor.go`** — `writeDoctorReport` (`tui/internal/tui/doctor.go:85`) is the indirection point (`var writeDoctorReport = doctorreport.Write`) that tests replace; `createDoctorReportDir` (`tui/internal/tui/doctor.go:144`) owns the exclusive-create of the unique destination directory before `Write` is ever called.
- **Writes only.** The bundle lands under the global directory resolved by `tui/internal/config/`; nothing in the TUI reads a saved bundle back.

## Composition

- **Parent:** `tui/` (`tui/ANATOMY.md`)
- **Subfolders:** none — flat, two-file package
- **Siblings:** `tui/internal/tui/` builds the draft; this package owns only serialization and redaction.

## State

- **Writes** three files into the supplied directory: `report.md` (human-readable), `metadata.json`, and `redaction.json`.
- **Permissions are part of the contract:** the directory is created `0700` and every file `0600`, so a report stays private to the user who generated it.

## Notes

- **Directory uniqueness is the caller's job.** `Write` does `MkdirAll` on the path it is given; the TUI's exclusive-create path is what guarantees two saves never collide. Do not move that responsibility here without moving the exclusive-create too.
- **Redaction is unconditional.** Every draft passes through redaction before serialization, and `redaction.json` records that it was applied plus which rules ran — a reader can tell redaction happened without diffing against an unredacted copy. New secret-shaped fields need a new rule *and* a test.
- **Never re-run the diagnostic here.** `Write` receives a finished `Draft`. Recomputing anything at save time would let the saved report disagree with the screen the user approved.
