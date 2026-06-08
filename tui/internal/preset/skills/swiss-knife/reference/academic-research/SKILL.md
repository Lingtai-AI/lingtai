---
name: academic-research
description: >
  Nested swiss-knife reference for academic literature work — find papers, fetch
  full-text PDFs, trace citations, write LaTeX manuscripts.
  **First action for any "get me this paper" request:**
  `python3 <skill-path>/scripts/fetch_paper.py <DOI|arXiv-ID|PMID>` — walks
  arXiv → Unpaywall → Europe PMC → CORE → publisher-page extraction (Nature/APS/AIP/IOP/Cambridge)
  → authorized institutional publisher → LibGen and saves the paper, metadata, and a resumable
  manifest under `papers/{slug}/`.
  Read the body when you need to escape the script: custom query shapes, citation networks,
  scholar analysis, LaTeX writing, or a tier-specific API call. Indexes 12 deep-dive API
  references and 6 pipeline workflows under `reference/`.
version: 3.0.0
allowed-tools: Bash(python3 *) Bash(curl *) Bash(pip *) Bash(pip3 *)
tags: [academic, research, arxiv, crossref, openalex, semantic-scholar, core, pubmed, unpaywall, doi, pdf, citation, pipeline, europe-pmc, nasa-ads, inspire-hep, nested-skill]
---

# Academic Research

> **Nested swiss-knife reference.** A modular skill: try the bundled script
> first, then load specific reference files only when you need to escape it.

## Try this first

For 80% of "get me this paper" requests, the bundled script is the right answer.
It walks the open-access ladder, falls back automatically, and writes a manifest
the next session can resume from.

```bash
# Fetch by any identifier
python3 <skill-path>/scripts/fetch_paper.py 10.1103/PhysRevLett.125.015001
python3 <skill-path>/scripts/fetch_paper.py arXiv:2301.00001
python3 <skill-path>/scripts/fetch_paper.py PMID:12345678

# Batch (one identifier per line in the file)
python3 <skill-path>/scripts/fetch_paper.py --batch dois.txt --out papers/

# Resolve metadata only (no PDF download)
python3 <skill-path>/scripts/fetch_paper.py 10.1038/nature12373 --dry-run

# Skip LibGen (e.g. legal-sensitive environment)
python3 <skill-path>/scripts/fetch_paper.py <id> --no-libgen

# Skip the authorized-publisher probe (off-network / legal-sensitive)
python3 <skill-path>/scripts/fetch_paper.py <id> --no-institutional

# Most conservative: OA only, no institutional probe, no LibGen
python3 <skill-path>/scripts/fetch_paper.py <id> --no-institutional --no-libgen
```

**Output layout** (idempotent — re-runs skip entries with `status: ok`):

```
papers/{first-author-year-firstword}/
├── paper.pdf  |  paper.md      # full-text artifact
├── metadata.json                # CrossRef-normalized
└── manifest.json                # {status, tier, source, ts, doi}
```

**Tier ladder** (script stops at first hit):

| Tier | Source | Best for |
|------|--------|----------|
| 1 | arXiv direct | Preprints (physics, CS, math, q-bio, econ) |
| 2 | Unpaywall | Publisher-blessed gold/green OA |
| 3 | Europe PMC | Biomedical full-text + PMC mirror |
| 4 | CORE | Institutional repositories (needs `$CORE_API_KEY`) |
| 5 | Publisher-page extract | Nature/APS/AIP/IOP/Cambridge → structured Markdown with LaTeX preserved (auto-installs [zhiping0913/Download_paper](https://github.com/zhiping0913/Download_paper) on first use) |
| 5b | Authorized publisher | **Licensed/institutional access only** — official DOI landing page → same-host publisher PDF → `%PDF-` validation, full provenance. Recovers paywalled-but-subscribed papers without shadow libraries. Never bypasses paywalls or handles credentials. Opt out with `--no-institutional`. See [authorized-publisher-access.md](reference/authorized-publisher-access.md) |
| 6 | LibGen | Last resort; opt out with `--no-libgen` |

**Set `$LINGTAI_RESEARCH_EMAIL` to a real address** before first use — Unpaywall
rejects placeholder emails with HTTP 422. The default falls back to
`lingtai-agent@example.org` with a warning.

Read on only if: the script fails on your paper, you need a custom query shape,
or you're composing a multi-step workflow (search → fetch → cite → write).

## Escape hatch — quick paths

```
I have a DOI                → reference/api-doi-resolver.md → api-crossref.md
I have an arXiv ID          → reference/api-arxiv.md (direct PDF link)
I have a PMID               → reference/api-europe-pmc.md
I have a bibcode            → reference/api-nasa-ads.md (requires free key)
I only have keywords        → reference/decision-tree.md → pick API by discipline
I need a citation network   → reference/api-semantic-scholar.md or api-openalex.md
I need to override the PDF ladder → reference/pipeline-obtain-pdf.md
Tier-5 publisher-extract failed and I want to retry it manually → reference/publisher-page-extraction.md
Paper is paywalled but I'm on a licensed network → reference/authorized-publisher-access.md (Tier 5b)
All OA chains failed        → reference/libgen-fallback.md (last resort)
I need astrophysics         → reference/api-nasa-ads.md
I need high-energy physics  → reference/api-inspire-hep.md
I need biomedical           → reference/api-europe-pmc.md or api-pubmed.md
I need to write/compile a paper → reference/pipeline-latex-writing.md
My empirical draft keeps getting reframed / reviewers "agree" → reference/anti-pattern-text-consistency-vs-data-correspondence.md
I hit an API error          → reference/error-handling.md
```

## Reference index

### API references (12)

Each card includes endpoint parameters, runnable code, response shape, rate limits, and fallbacks.

| API | File | Best for | Key? |
|-----|------|----------|------|
| arXiv | [api-arxiv.md](reference/api-arxiv.md) | Preprint retrieval | No |
| CrossRef | [api-crossref.md](reference/api-crossref.md) | DOI metadata, funder queries | No (mailto recommended) |
| DOI Resolver | [api-doi-resolver.md](reference/api-doi-resolver.md) | Batch DOI → structured citation | No |
| OpenAlex | [api-openalex.md](reference/api-openalex.md) | Discovery, institution/concept analysis | No |
| Semantic Scholar | [api-semantic-scholar.md](reference/api-semantic-scholar.md) | Citation networks, TLDR | No (tight limits) |
| CORE | [api-core.md](reference/api-core.md) | OA full-text downloads | Optional (recommended) |
| PubMed | [api-pubmed.md](reference/api-pubmed.md) | Biomedical search, PMC full text | No |
| Unpaywall | [api-unpaywall.md](reference/api-unpaywall.md) | OA versions / PDFs | email (real) |
| Google Scholar | [api-google-scholar.md](reference/api-google-scholar.md) | Broadest discipline coverage | No (needs stealth) |
| Europe PMC | [api-europe-pmc.md](reference/api-europe-pmc.md) | Biomed, PMID, full-text XML | No |
| NASA ADS | [api-nasa-ads.md](reference/api-nasa-ads.md) | Astrophysics, BibTeX export | Yes (free) |
| INSPIRE-HEP | [api-inspire-hep.md](reference/api-inspire-hep.md) | High-energy physics | No |

### Pipeline workflows (6)

| Pipeline | File | Purpose |
|----------|------|---------|
| Paper discovery | [pipeline-discovery.md](reference/pipeline-discovery.md) | Keywords → candidate papers |
| PDF acquisition | [pipeline-obtain-pdf.md](reference/pipeline-obtain-pdf.md) | Metadata → full text (manual ladder) |
| Citation tracking | [pipeline-citation-tracking.md](reference/pipeline-citation-tracking.md) | Forward/backward citation networks |
| Scholar analysis | [pipeline-scholar-analysis.md](reference/pipeline-scholar-analysis.md) | Impact, trends, h-index |
| LaTeX writing | [pipeline-latex-writing.md](reference/pipeline-latex-writing.md) | Compile, bibliography, figures, debug |
| Decision tree | [decision-tree.md](reference/decision-tree.md) | "I have X — which API should I use?" |

### Standalone references

- [publisher-page-extraction.md](reference/publisher-page-extraction.md) — Tier-5 manual escape hatch (Nature/APS/AIP/IOP/Cambridge → structured Markdown)
- [authorized-publisher-access.md](reference/authorized-publisher-access.md) — Tier-5b authorized institutional publisher PDF access: official DOI landing → same-host PDF → validation, with hard policy boundaries (no paywall bypass, no credential/cookie handling)
- [libgen-fallback.md](reference/libgen-fallback.md) — Last-resort PDF source with legal/safety notes
- [error-handling.md](reference/error-handling.md) — 429 backoff, 403 publisher blocks, timeout patterns
- [anti-pattern-text-consistency-vs-data-correspondence.md](reference/anti-pattern-text-consistency-vs-data-correspondence.md) — empirical-writing failure mode: prose drifts from the data while reviewer rounds make it more polished. Trigger pattern, re-anchoring steps, detection checklist.

## Relationship to web-browsing

- **web-browsing** is the routing layer ("which tier to use for this URL?")
- **academic-research** is the deep-dive layer ("how do I write OpenAlex filter parameters? what email does Unpaywall want?")
- The two are complementary. If you're just scraping one publisher page and don't need the OA ladder, web-browsing's `extract_page.py` is lighter.

## Known caveats

- **Unpaywall's `email` parameter is required and must be real** — placeholder addresses get HTTP 422. Set `$LINGTAI_RESEARCH_EMAIL` once.
- **CORE without an API key is harshly rate-limited** (~100/day vs 10,000/day with a free key from https://core.ac.uk/services/api).
- **Semantic Scholar free tier is very tight** (~100 reqs / 5 min). Request a key for any serious citation-network work.
- **Google Scholar requires a stealth browser** (camoufox or playwright-stealth v2); legacy `playwright_stealth` API does not work.
- **arXiv enforces HTTPS** — HTTP requests are 301-redirected automatically.
- **Authorized-publisher tier (5b) only uses access you already have** — it follows the official DOI landing page and grabs a same-host publisher PDF, validating `%PDF-` bytes and Content-Type before saving. It never bypasses paywalls/CAPTCHAs and never reads, stores, or replays cookies/credentials; most institutional access is IP-based, so on a licensed network a plain HTTP GET just works. Off-network it harmlessly misses. Pass `--no-institutional` to disable in legal-sensitive environments. Cookies/auth headers are never written to provenance. See [reference/authorized-publisher-access.md](reference/authorized-publisher-access.md).
- **Library Genesis legality varies by jurisdiction** — use is the user's responsibility. Pass `--no-libgen` to opt out.
- **Publisher-page extraction (Tier 5)** uses Playwright + pandoc; first invocation installs `zhiping0913/Download_paper` from git. Requires Chromium (`playwright install chromium`) and pandoc on `$PATH`. See [reference/publisher-page-extraction.md](reference/publisher-page-extraction.md).
- **Writing an empirical paper iteratively can drift from the data** — reviewer rounds make prose more polished and internally consistent without verifying it still matches the experiments on disk. Reviewer agreement is text-consistency evidence, not data-correspondence evidence. Anchor every claim to data files/runner code *before* writing, and re-derive (don't just rewrite) when feedback flags confusion. See [reference/anti-pattern-text-consistency-vs-data-correspondence.md](reference/anti-pattern-text-consistency-vs-data-correspondence.md).

---
> **Found a bug or issue?** If you encounter any problems with this skill, load the `lingtai-issue-report` skill and follow its instructions to report it.
