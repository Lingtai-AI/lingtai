# Migration from v2 to v3

> Part of the [web-browsing](../SKILL.md) skill.

## Architecture Overview: What Changed

In **v2**, web browsing capabilities were spread across multiple separate sub-skills living in independent directories:

```
~/.lingtai-tui/utilities/
├── web-content-extractor/          ← core extraction (tiers 0–3)
├── academic-search-pipeline/       ← academic APIs
├── search-strategies/              ← search engine selection
├── news-and-rss/                   ← news acquisition
├── social-media-extraction/        ← social media
├── realtime-data/                  ← real-time sources
└── stealth-browsing/               ← anti-detection
```

In **v3**, all of these have been **merged into a single unified skill** (`web-browsing-manual`). The tier system was expanded from 4 tiers (0–3) to 7 tiers (0–5, with 1.5), and new capabilities were added at every level. The sub-skills still exist as reference directories, but the main `SKILL.md` is now the single source of truth.

### What's New in v3

| Feature | Tier | Description |
|---------|------|-------------|
| **Tier 1.5 — trafilatura** | NEW | Fast article extraction using the `trafilatura` library. 10–50× faster than BeautifulSoup (Tier 2). Now the default auto-tier choice for generic URLs. |
| **Tier 4 — Jina Reader / Firecrawl** | NEW | Cloud-based extraction APIs as fallback when all local methods fail (403, CAPTCHA, heavy JS). Jina Reader is free and should be the first fallback. |
| **Tier 5 — AI-Native Search** | NEW | Tavily and Exa APIs for discovering content (not extracting a known URL). Combines search + extraction in one call. |
| **Integrated search mode** | NEW | `--search --search-provider {ddg,tavily,exa}` flag brings search directly into the extraction script. |
| **Fallback chain** | NEW | `--fallback` flag enables automatic tier escalation on failure. |
| **Expanded academic APIs** | Enhanced | Tier 1 now also includes Semantic Scholar, Unpaywall, and PubMed E-utilities. |
| **Resource blocking in Tier 3** | Improved | Playwright now blocks images/CSS/fonts, making Tier 3 ~2× faster. |
| **Richer output** | Improved | Results now include `text_length`, `jsonld`, `opengraph`, `meta_*` fields. |
| **Built-in tests** | NEW | `--test` flag for verification. |

### What Moved Where

| v2 Location | v3 Equivalent |
|-------------|---------------|
| `web-content-extractor/SKILL.md` | Merged into `web-browsing-manual/SKILL.md` (tiers 0–5) |
| `academic-search-pipeline/` | Referenced as sub-skill; APIs integrated into Tier 1 |
| `search-strategies/` | Referenced as sub-skill; search integrated into Tier 5 |
| `news-and-rss/` | Referenced as sub-skill |
| `stealth-browsing/` | Referenced as sub-skill; techniques in Tier 3 |
| `social-media-extraction/` | Referenced as sub-skill |
| `realtime-data/` | Referenced as sub-skill |

---

## Migration Notes: v2.0.0 → v3.0

| v2.0 Feature | v3.0 Equivalent | Changes |
|-------------|----------------|---------|
| `--tier 0/1/2/3/auto` | `--tier 0/1/1.5/2/3/4/5/auto` | Added Tier 1.5 (trafilatura), Tier 4 (Jina Reader), Tier 5 (AI Search) |
| `tier2()` = BS4 extraction | `tier1_5()` = trafilatura (new default for most sites) | **Default auto-tier changed from 2 to 1.5** for generic URLs — trafilatura is 10-50× faster |
| No search mode | `--search --search-provider {ddg,tavily,exa}` | Search is now integrated into the script |
| No fallback chain | `--fallback` flag | Automatic tier escalation on failure |
| `extract_page.py --save pdf.pdf` | Same, unchanged | PDF save still works |
| Academic APIs: arXiv, CrossRef | + Semantic Scholar, Unpaywall, PubMed E-utilities | Tier 1 now tries multiple APIs |
| No resource blocking in Tier 3 | Blocks images/CSS/fonts in Playwright | Tier 3 is ~2× faster |
| Site detection: 6 domains | 30+ domains in auto_tier() | Better auto-detection |
| Output: text preview only | + `text_length`, `jsonld`, `opengraph`, `meta_*` | Richer structured output |
| v2.0 `--tier 2` for nature.com | v3.0 auto picks Tier 2 for nature.com | Same, but now falls through to Tier 3 on failure |
| No smoke tests | `--test` flag | Built-in verification |

### Breaking changes

- **Auto-tier default changed**: Most generic URLs now route to Tier 1.5 (trafilatura) instead of Tier 2 (BS4). This is faster and usually better, but if you relied on BS4-specific extraction (CSS selectors, site templates), use `--tier 2` explicitly.
- **Tier numbering**: Tier 2 is still BS4. Tier 1.5 is new (trafilatura). Your old `--tier 2` calls still work identically.
- **Output format**: Results now include additional fields (`text_length`, `jsonld`, `opengraph`). Existing JSON parsing should be forward-compatible.

### Post-release patches (2026-04-27)

| Issue | Symptom | Fix |
|-------|---------|-----|
| Bare DOI input crash | `python3 extract_page.py "10.1038/xxx"` → `Invalid URL: No scheme supplied` | Added input normalizer: bare DOI → `https://doi.org/...`, bare arXiv ID → `https://arxiv.org/abs/...`, non-URL text → auto search mode |
| `duckduckgo_search` renamed | `RuntimeWarning: This package has been renamed to ddgs` on every search | Import now tries `from ddgs import DDGS` first, falls back to `from duckduckgo_search import DDGS` |
