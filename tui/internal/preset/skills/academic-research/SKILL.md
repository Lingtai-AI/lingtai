---
name: academic-research
description: "Deep-dive academic research skill — 12 API references (arXiv, CrossRef, OpenAlex, Semantic Scholar, CORE, PubMed, Unpaywall, Google Scholar, DOI Resolver, Europe PMC, NASA ADS, INSPIRE-HEP) + 6 pipeline workflows (discovery, PDF acquisition, citation tracking, scholar analysis, LaTeX writing, decision tree) + error handling patterns. Use this when you need detailed API parameters, code examples, and fallback chains for scholarly search and paper writing."
version: 2.0.0
tags: [academic, research, arxiv, crossref, openalex, semantic-scholar, core, pubmed, unpaywall, google-scholar, doi, pdf, citation, pipeline, europe-pmc, nasa-ads, inspire-hep, error-handling]
parent: web-browsing
---

# Academic Research

> If you navigated here from the web-browsing skill — web-browsing answers "which tier to use," while this skill answers "how to use a specific API."

## When to Use

- You already have a DOI, title, or author and need to retrieve paper metadata or full text
- You need to systematically search scholarly literature
- You need to trace citation networks or analyze research trends
- You need a full-text PDF but aren't sure which source to use

## Decision Entry Point

**Not sure which API to start with?** → Read [reference/decision-tree.md](reference/decision-tree.md)

It routes you to the most appropriate API based on your input (DOI? arXiv ID? keywords? discipline?).

## API References (12)

Each reference includes: endpoint parameter tables, runnable code, response formats, rate limits, error handling, and cross-references.

| API | Reference | Best For | Requires Key? |
|-----|-----------|----------|---------------|
| arXiv | [api-arxiv.md](reference/api-arxiv.md) | Preprint retrieval, CS/physics/math | No |
| CrossRef | [api-crossref.md](reference/api-crossref.md) | DOI metadata, funder queries, new publications | No (mailto recommended) |
| DOI Resolver | [api-doi-resolver.md](reference/api-doi-resolver.md) | Single/batch DOI resolution to structured citations | No |
| OpenAlex | [api-openalex.md](reference/api-openalex.md) | Large-scale paper discovery, institution/concept analysis | No |
| Semantic Scholar | [api-semantic-scholar.md](reference/api-semantic-scholar.md) | Citation networks, TLDR summaries, author profiles | No |
| CORE | [api-core.md](reference/api-core.md) | Open-access full-text downloads | Optional |
| PubMed | [api-pubmed.md](reference/api-pubmed.md) | Biomedical literature search, PMC full text | No |
| Unpaywall | [api-unpaywall.md](reference/api-unpaywall.md) | Find OA versions/PDFs of papers | email parameter (not a placeholder) |
| Google Scholar | [api-google-scholar.md](reference/api-google-scholar.md) | Broadest discipline coverage, citation counts (requires scraping) | No (requires stealth) |
| Europe PMC | [api-europe-pmc.md](reference/api-europe-pmc.md) | Biomedical literature, PMID lookup, full-text XML | No |
| NASA ADS | [api-nasa-ads.md](reference/api-nasa-ads.md) | Astrophysics/astronomy, BibTeX export, citation networks | Yes (free key) |
| INSPIRE-HEP | [api-inspire-hep.md](reference/api-inspire-hep.md) | High-energy physics, author profiles, BibTeX export | No |

## Pipeline Workflows (6)

Each pipeline includes: workflow steps, decision trees, code examples, and failure fallbacks.

| Pipeline | Reference | Purpose |
|----------|-----------|---------|
| Paper Discovery | [pipeline-discovery.md](reference/pipeline-discovery.md) | From keywords to a set of candidate papers |
| PDF Acquisition | [pipeline-obtain-pdf.md](reference/pipeline-obtain-pdf.md) | From metadata to full-text PDF (with stealth) |
| Citation Tracking | [pipeline-citation-tracking.md](reference/pipeline-citation-tracking.md) | Forward/backward citation networks |
| Scholar Analysis | [pipeline-scholar-analysis.md](reference/pipeline-scholar-analysis.md) | Impact, trends, h-index |
| LaTeX Writing | [pipeline-latex-writing.md](reference/pipeline-latex-writing.md) | Compile, bibliography, figures, debugging |
| Decision Tree | [decision-tree.md](reference/decision-tree.md) | "I have X — which API should I use?" |

## Quick Paths

```
I have a DOI          → api-doi-resolver.md → api-crossref.md → api-unpaywall.md for PDF
I have an arXiv ID    → api-arxiv.md (direct PDF link)
I have a PMID         → api-europe-pmc.md
I have a bibcode      → api-nasa-ads.md (requires free key)
I only have keywords  → decision-tree.md → pick API by discipline
I need citation network → api-semantic-scholar.md or api-openalex.md
I need full-text PDF  → pipeline-obtain-pdf.md (Unpaywall → CORE → Europe PMC chain)
I need astrophysics   → api-nasa-ads.md
I need high-energy physics → api-inspire-hep.md
I need biomedical     → api-europe-pmc.md or api-pubmed.md
I need to write/compile a paper → pipeline-latex-writing.md (compile + bib + figures + debug)
I hit an API error    → error-handling.md (fallback chains, rate limit strategies)
```

## Error Handling

Common failure patterns and fallback chains are documented in [error-handling.md](reference/error-handling.md):
- 429 rate limiting → exponential backoff + API switch
- 403 publisher blocks → Unpaywall → CORE → Europe PMC chain
- Timeout patterns → per-API timeout guidance
- Empty results → query diagnosis checklist

## Relationship to web-browsing

- **web-browsing**: routing layer — "which tier to use?" (PDF direct? API metadata? trafilatura? Playwright?)
- **academic-research**: deep-dive layer — "how do I write filter parameters for OpenAlex? What email should I use for Unpaywall?"
- The two are complementary and non-overlapping.

## Known Caveats

- Google Scholar requires a stealth browser (camoufox or playwright-stealth v2); do not use the legacy `playwright_stealth` API
- Unpaywall's email parameter is **required** and **must be a real address** — it serves as the sole "authentication". Placeholder emails (e.g., `test@example.com`) will return 422 errors.
- arXiv enforces HTTPS; HTTP requests are automatically redirected via 301
- **CORE without an API key is extremely limited** — aggressive rate limits (429 after just a few requests). Register at https://core.ac.uk/services/api for a free key (increases quota from ~100/day to 10,000/day). See [api-core.md](reference/api-core.md).
- **Semantic Scholar free tier is very tight** — ~100 requests per 5 minutes without key, 1 req/s with key. Request an API key for any serious use. See [api-semantic-scholar.md](reference/api-semantic-scholar.md).
- For comprehensive error handling strategies, see [error-handling.md](reference/error-handling.md)
