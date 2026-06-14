---
name: web-browsing
description: >
  Fetch, extract, scrape, or search web content. First try
  `python3 <skill-path>/scripts/extract_page.py <URL>`: it auto-tiers across
  PDFs, metadata APIs, trafilatura, BeautifulSoup, Playwright, Jina, and AI
  search. Read this router when the script fails, you need site/tier routing,
  or you are composing a multi-step web/research pipeline.
version: 3.1.0
---

# web-browsing — Router

> **Browse the web with progressive disclosure.** Start with the bundled
> auto-tier extractor. Drill into nested references only when the script fails,
> you need a custom extraction shape, or you are changing the skill itself.

## Try this first

For most fetches the bundled script is the right answer. It auto-tiers, falls
back on failure, and handles PDFs / APIs / static articles / dynamic pages
without you writing custom code:

```bash
# Auto-tier: extractor picks the cheapest viable strategy
python3 <skill-path>/scripts/extract_page.py "https://example.com/article"

# Fallback chain: try, escalate on each failure
python3 <skill-path>/scripts/extract_page.py "https://example.com" --fallback

# Force a specific tier when you know better than the auto-router
python3 <skill-path>/scripts/extract_page.py "https://example.com" --tier 3

# Search mode (no URL, just a query)
python3 <skill-path>/scripts/extract_page.py "quantum computing" --search

# Save as JSON
python3 <skill-path>/scripts/extract_page.py "https://example.com" --json out.json
```

Read further only if that returns nothing useful, you need a custom extraction
shape, or you are composing a multi-step pipeline such as academic search → DOI
→ free PDF → text.

## Nested reference catalog

`web-browsing` owns these nested references. They are parent-owned drill-down
files, not standalone top-level skills. Existing deep-dive `.md` files under
`reference/` remain available and are indexed from the nested references.

```yaml
- name: web-browsing-tier-quick-refs
  location: reference/tier-quick-refs/SKILL.md
  description: |
    Manual commands for each extraction tier: PDF direct download, metadata
    APIs, Trafilatura, BeautifulSoup, Playwright stealth, Jina/Firecrawl, and
    AI-native search.
- name: web-browsing-routing-and-sites
  location: reference/routing-and-sites/SKILL.md
  description: |
    Auto-tier decision tree, per-site recommendations, known limitations and
    gotchas, and real-time data endpoints.
- name: web-browsing-maintenance-bundles
  location: reference/maintenance-bundles/SKILL.md
  description: |
    Maintenance protocol, semantic sweeps, dirty-first testing, bundled JSON
    JSON asset files, deep-dive reference files, and explicit decision flowchart.
```

## Quick decision tree

```text
URL arrives → run scripts/extract_page.py first
  ├─ PDF?                         → Tier 0; details in tier quick refs
  ├─ Known API?                   → Tier 1; details in tier quick refs
  ├─ Static HTML article?         → Tier 1.5 Trafilatura
  ├─ Needs structured scraping?   → Tier 2 BeautifulSoup
  ├─ JS-rendered/protected?       → Tier 3 Playwright stealth
  ├─ Still failing?               → Tier 4 Jina Reader / Firecrawl
  └─ Need to discover content?    → Tier 5 search / AI-native search
```

## Router table

| Need / keywords | Read |
|---|---|
| Specific tier commands; manual PDF/API/Trafilatura/BeautifulSoup/Playwright/Jina/Firecrawl/search examples | `reference/tier-quick-refs/SKILL.md` |
| Auto-tier misroutes a page; choose a tier; per-site recommendations; limitations; real-time data endpoints | `reference/routing-and-sites/SKILL.md` |
| Editing or validating this skill; bundled JSON asset files; deep-dive reference index; semantic sweep and dirty-first testing | `reference/maintenance-bundles/SKILL.md` |

## Tier overview

| Tier | Method | Speed | Tools | Reference |
|------|--------|-------|-------|-----------|
| **0** | PDF Direct Download | ~1s | `curl` + `fitz` | [tier-0-pdf.md](reference/tier-0-pdf.md) |
| **1** | API Metadata Queries | ~0.5s | `requests` | [tier-1-apis.md](reference/tier-1-apis.md) |
| **1.5** | Trafilatura Fast Extraction | ~2s | `trafilatura` | [tier-1-5-trafilatura.md](reference/tier-1-5-trafilatura.md) |
| **2** | BeautifulSoup Structured Extraction | ~5s | `requests` + `BS4` | [tier-2-beautifulsoup.md](reference/tier-2-beautifulsoup.md) |
| **3** | Playwright Stealth | ~15s | `playwright` + stealth | [tier-3-playwright.md](reference/tier-3-playwright.md) |
| **4** | API Fallback | ~3s | Jina / Firecrawl | [tier-4-jina-firecrawl.md](reference/tier-4-jina-firecrawl.md) |
| **5** | AI-Native Search | ~5s | `ddgs` / Tavily / Exa | [tier-5-ai-search.md](reference/tier-5-ai-search.md) |

## Core rules to keep resident

- Use the bundled `extract_page.py` before hand-writing scrapers unless you have
  a clear reason not to.
- Escalate tiers only on failure or when the site class demands it; each tier is
  heavier than the previous.
- Prefer source-specific APIs for structured/current data when available.
- Do not use web browsing for content already in the conversation or when an MCP
  or first-class tool covers the source more cleanly.
- When changing this skill, run the maintenance reference's semantic sweep so the
  script, JSON asset files, and docs stay aligned.
