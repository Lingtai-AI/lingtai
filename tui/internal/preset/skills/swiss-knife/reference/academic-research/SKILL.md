---
name: academic-research
description: >
  Use when drafting citation-bearing academic writing or revising empirical
  claims. Nested swiss-knife reference for source verification and keeping
  manuscript claims anchored to data, not reviewer consensus.
version: 4.0.0
tags: [academic, evidence, citation, nested-skill]
last_changed_at: "2026-10-12T17:13:18Z"
maintenance: "If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths."
---

# Academic Evidence Gates

Nested swiss-knife reference. This ships the writing safeguards, not a paper
fetcher, API cookbook, citation-tracking pipeline, or LaTeX toolchain.

## Before writing

- For author–year citations, a References section, related work, or a literature
  review, read [evidence-verification-gate.md](reference/evidence-verification-gate.md).
  Build the verified literature matrix before drafting. Search results are leads,
  not citations; label preprints separately. Reduce scope, not verification.
- For empirical methods or results, read
  [anti-pattern-text-consistency-vs-data-correspondence.md](reference/anti-pattern-text-consistency-vs-data-correspondence.md).
  Re-open data and runner code and maintain a claim-to-evidence map. Reviewer
  agreement proves text consistency, not correspondence with the experiment.
- Use current official publisher, repository, library, or bibliographic sources
  to verify provenance. Work only with authorized/open access; do not bypass
  paywalls, logins, CAPTCHAs, or bot blocks, or expose credentials. If access is
  unavailable, report that limit and ask for an authorized source or handoff.
  Never read, store or replay publisher cookies/credentials. A Zotero or
  institutional full-text handoff remains human-operated: do not automate its UI
  through AppleScript/Accessibility or bypass OS privacy controls, and do not
  silently substitute a shadow-library download. Preserve provenance and copy,
  rather than move, human-provided originals.
- For source-derived teaching or HTML artifacts, read
  [html-report](../html-report/SKILL.md) for source limits and delivery checks.

The former bundled PDF-fetch scripts, API cards and pipeline recipes are retired.
Do not invoke old installed copies as if they were supported shipped utilities.
