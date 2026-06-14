---
name: web-browsing-maintenance-bundles
description: >
  Nested web-browsing reference for maintenance protocol, semantic sweeps,
  dirty-first testing, bundled JSON asset files, deep-dive reference files, and the
  explicit decision flowchart.
version: 1.0.0
---

# Web Browsing Maintenance and Assets Reference

Nested web-browsing reference. Open this when changing the skill, validating
propagation, or choosing which bundled asset/reference file to inspect.

## Maintenance Protocol

When modifying any code or pattern in this skill, follow these three rules **without exception**:

### Rule 1: Grep Before You Ship — Semantic Sweep

Fixing a bug in one file is not enough. After every fix, search the entire codebase for **all occurrences of the same pattern** — not just the same text, but the same **semantic class**.

```bash
# Fix propagation sweep (run after every code change)
# 1. Same text
grep -rn 'bad_pattern' scripts/ reference/ SKILL.md --include='*.json' .
# 2. Same semantics (e.g., "placeholder email", "outdated API", "hardcoded path")
grep -rn 'example\.com' | grep -iE 'email|mailto|user-agent'
grep -rn 'stealth_sync|bare_extraction.*\.get'
```

If a bad pattern appears in `scripts/`, it almost certainly also appears in `reference/`, `SKILL.md`, or bundled JSON asset files. Find them all or they will drift apart.

### Rule 2: Dirty-First Testing

Smoke tests must include **dirty inputs** — real-world edge cases that expose runtime failures, not just clean decision-logic checks. Every tier's test must include at least one non-mock verification.

Minimum dirty test coverage:
- Non-standard URLs (e.g., `/pdf/ID` without `.pdf` suffix)
- Non-dict return types (e.g., `bare_extraction()` returning Document)
- API version incompatibilities (e.g., stealth v1 vs v2)
- Rejected placeholder values (e.g., Unpaywall 422 on `test@example.com`)
- Deprecated endpoints (e.g., Wikipedia `/page/related/`)

### Rule 3: Single Source of Truth

If the same logic appears in multiple places (e.g., `auto_tier()` in both `extract_page.py` and `SKILL.md`), **the script is the truth** and documentation should point to it, not duplicate it. Duplicated code drifts; references don't.

---

## Bundled Assets

| File | Contents |
|------|----------|
| `api-endpoints.json` in the bundled asset directory | Full API endpoints + parameters for every provider |
| `site-templates.json` in the bundled asset directory | CSS-selector templates for known sites |
| `css-selectors.json` in the bundled asset directory | Common-pattern CSS selector library |
| `regex-patterns.json` in the bundled asset directory | Regex templates for DOI / arXiv / PMID / PMC / ISBN |
| `search-providers.json` in the bundled asset directory | Search engine API configurations |
| `extraction-pipeline.json` in the bundled asset directory | Full extraction pipeline configuration |
| `scripts/extract_page.py` | Executable v3.0 script: `--tier 0-5 + auto`, `--search`, `--fallback` |
| `scripts/cached_get.py` | File-based HTTP cache with TTL support |

---

## Reference Files (Deep-Dives)

For more than quick-reference snippets, load the appropriate reference file:

| Reference | When to load |
|-----------|-------------|
| [tier-0-pdf.md](../tier-0-pdf.md) | PDF download + fitz extraction details |
| [tier-1-apis.md](../tier-1-apis.md) | All academic/metadata APIs, ID resolution chains |
| [tier-1-5-trafilatura.md](../tier-1-5-trafilatura.md) | Trafilatura configuration, batch mode, dedup |
| [tier-2-beautifulsoup.md](../tier-2-beautifulsoup.md) | BS4 patterns, CSS selectors, site templates |
| [tier-3-playwright.md](../tier-3-playwright.md) | Playwright stealth setup, resource blocking |
| [tier-4-jina-firecrawl.md](../tier-4-jina-firecrawl.md) | Jina Reader + Firecrawl API details |
| [tier-5-ai-search.md](../tier-5-ai-search.md) | DDG / Tavily / Exa search integration |
| [academic-pipeline.md](../academic-pipeline.md) | Full academic search: find → enrich → get PDF |
| [search-strategies.md](../search-strategies.md) | Engine selection, query optimization, pagination |
| [news-and-rss.md](../news-and-rss.md) | Google News RSS, Reddit JSON, RSS parsing |
| [social-media.md](../social-media.md) | Reddit, HN, Mastodon, X/Twitter, GitHub |
| [realtime-data.md](../realtime-data.md) | Financial, weather, Stack Exchange, Wikipedia |
| [stealth.md](../stealth.md) | Anti-detection, fingerprinting, proxy strategies |
| [migration-from-v2.md](../migration-from-v2.md) | What changed from v2 → v3 |

---

## Explicit Decision Flowchart

```
                         ┌──────────────────┐
                         │  URL or query    │
                         └────────┬─────────┘
                                  │
                     ┌──────────────────────────┐
                     │ Is it a URL or a keyword? │
                     └─────┬──────────────┬─────┘
                       URL  │              │ keyword
                             ▼              ▼
                    ┌──────────────┐   ┌──────────────┐
                    │ Is it PDF?   │   │ Tier 5:      │
                    └──┬──────┬───┘   │ AI Search    │
                   yes│    no│       │ (DDG/Tavily/ │
                      ▼      │       │  Exa)        │
               ┌──────────┐  │       └──────────────┘
               │ Tier 0:  │  │
               │ PDF+fitz │  │
               └──────────┘  │
                             ▼
                    ┌──────────────────┐
                    │ Known API?       │
                    │ (arXiv, DOI,     │
                    │  PubMed, etc.)   │
                    └──┬──────────┬───┘
                   yes│        no│
                      ▼          ▼
               ┌──────────┐  ┌──────────────────┐
               │ Tier 1:  │  │ Static HTML?     │
               │ API query│  │ (article/blog)   │
               └──────────┘  └──┬──────────┬─────┘
                            yes│       no│
                               ▼         ▼
                        ┌──────────┐  ┌──────────────────┐
                        │ Tier 1.5:│  │ Structured data? │
                        │trafilat. │  │ (tables, lists)  │
                        └──────────┘  └──┬──────────┬─────┘
                                     yes│       no│
                                        ▼         ▼
                                 ┌──────────┐  ┌──────────────┐
                                 │ Tier 2:  │  │ JS-rendered? │
                                 │   BS4    │  │ Protected?   │
                                 └──────────┘  └──┬──────┬─────┘
                                              yes│    no│
                                                 ▼      ▼
                                          ┌──────────┐ ┌──────────┐
                                          │ Tier 3:  │ │ Tier 4:  │
                                          │Playwright│ │   Jina   │
                                          │ stealth  │ │  Reader  │
                                          └──────────┘ └──────────┘
```

The compact decision tree near the top is the rule list; this flowchart shows the order of decisions and what each "no" branch does. Read `scripts/extract_page.py::auto_tier()` for the same logic in code (source of truth when this manual drifts).

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
