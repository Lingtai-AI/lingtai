# Comprehensive Decision Tree: From Input to Recommended API

## Goal

Quickly route to the optimal API or scraping method based on your input type (DOI, title, author, keyword, URL, etc.).

## Overview Decision Tree

```
What is your input?
│
├── DOI (10.xxxx/...)
│   ├── Need metadata?
│   │   ├── CrossRef API  →  GET /works/{DOI} (fastest, citation count + journal info)
│   │   └── OpenAlex API  →  GET /works/https://doi.org/{DOI} (includes topic classification + citation network)
│   ├── Need a free PDF?
│   │   └── Unpaywall API →  GET /v2/{DOI}?email=xxx (check open access status)
│   └── Need citation network?
│       └── OpenAlex API  →  referenced_works + cited_by
│
├── arXiv ID (2301.xxxxx / 2301.xxxxxv1)
│   ├── Need metadata + abstract?
│   │   └── arXiv API  →  GET /api/query?id_list={ID}
│   └── Need PDF?
│       └── Direct download  →  https://arxiv.org/pdf/{ID}.pdf
│
├── Bibcode (19-char ADS ID, e.g., 2023ApJ...942...71V)
│   └── NASA ADS API  →  GET /search/query?q=bibcode:{BIBCODE}
│       (BibTeX export built-in; requires free API key)
│
├── PMID (pure numeric, e.g., 12345678)
│   └── Europe PMC  →  GET /search?query=EXT_ID:{PMID}
│
├── Keyword / Topic phrase
│   ├── Need structured data (citations, year, DOI)?
│   │   └── OpenAlex API  →  filter=title_and_abstract.search:{q}
│   ├── Need physics/CS/math preprints?
│   │   └── arXiv API  →  search_query=all:{q}
│   ├── Need biomedicine?
│   │   └── Europe PMC  →  GET /search?query={q}
│   ├── Need astrophysics/astronomy?
│   │   └── NASA ADS  →  GET /search/query?q={q} (requires free key)
│   ├── Need high-energy physics?
│   │   └── INSPIRE-HEP  →  GET /literature?q={q}
│   └── Need Google Scholar rankings?
│       └── curl+BS scrape Scholar (max 1 request per session; fallback to OpenAlex on 429)
│
├── Author name
│   ├── Need h-index / paper count / impact?
│   │   └── OpenAlex Authors  →  filter=display_name.search:{name}
│   ├── Need all papers by this author?
│   │   └── OpenAlex Works  →  filter=author.id:{openalex_id}
│   └── Need Scholar profile page?
│       └── curl+BS scrape /citations?user={ID}
│
├── Paper title
│   ├── Exact match?
│   │   └── OpenAlex  →  filter=title.search:{title}
│   ├── Fuzzy search?
│   │   └── OpenAlex  →  filter=title_and_abstract.search:{title}
│   └── Need to find DOI?
│       └── CrossRef  →  query={title}
│
├── URL
│   ├── Ends with .pdf?
│   │   └── curl -L download → PyMuPDF extract text
│   ├── arxiv.org/abs/...
│   │   └── Extract ID → download PDF
│   ├── scholar.google.com/...
│   │   └── curl+BS (Tier 2) → on failure use camoufox (Tier 3)
│   ├── nature.com / springer.com
│   │   ├── Extract DOI (meta[name="citation_doi"]) → follow DOI workflow
│   │   └── camoufox render (domcontentloaded, not networkidle)
│   ├── Major paid publishers (Wiley/Elsevier/Science)
│   │   └── API is the only option; do not attempt direct scraping
│   └── Other URLs
│       └── web_read → curl+BS → camoufox (escalate by tier)
│
└── Existing paper list
    ├── Need formatted citations?
    │   └── citation-tracking pipeline → APA / BibTeX / IEEE
    ├── Need trend analysis?
    │   └── scholar-analysis pipeline → trend chart + gap identification
    ├── Need to generate a literature review?
    │   └── citation-tracking pipeline → compile_literature_review()
    └── Writing/compiling a paper?
        └── latex-writing pipeline → compile + bibliography + figures + debug
```

---

## "I Want to Write a Paper" Branch

When the goal is **producing** a paper (not just searching), the full pipeline is:

```
1. discovery → find papers (OpenAlex, arXiv, etc.)
2. obtain-pdf → get full text
3. citation-tracking → generate BibTeX entries
4. pipeline-latex-writing → compile paper with bibliography
```

Key integration points:
- **BibTeX from APIs**: CrossRef `/transform/application/x-bibtex`, NASA ADS `/export/bibtex`, INSPIRE-HEP `?format=bibtex` → append to `references.bib`
- **Citation style**: biblatex `style=numeric` for sciences, `style=apa` for social science
- **Engine**: `pdflatex` for English, `xelatex` for CJK/Chinese text

## API Quick Reference

| API | Free | Key Required | Best For | Rate Limit | Reference |
|-----|------|-------------|----------|------------|-----------|
| OpenAlex | ✅ | No | All-around: search, metadata, citation networks | ~10 req/s | [api-openalex.md](api-openalex.md) |
| CrossRef | ✅ | No | DOI metadata, citation counts | ~1 req/s | [api-crossref.md](api-crossref.md) |
| arXiv | ✅ | No | Physics/CS/math preprints | Relaxed | [api-arxiv.md](api-arxiv.md) |
| Unpaywall | ✅ | email | Open access status and free PDFs | ~10 req/s | [api-unpaywall.md](api-unpaywall.md) |
| Europe PMC | ✅ | No | Biomedical literature, PMID lookup, full text | ~5 req/s | [api-europe-pmc.md](api-europe-pmc.md) |
| Semantic Scholar | ✅ | Recommended | Citation networks, TLDR summaries | Strict without key | [api-semantic-scholar.md](api-semantic-scholar.md) |
| NASA ADS | ✅ | Yes (free) | Astrophysics/astronomy, BibTeX export | Reasonable | [api-nasa-ads.md](api-nasa-ads.md) |
| INSPIRE-HEP | ✅ | No | High-energy physics, author profiles | Be respectful | [api-inspire-hep.md](api-inspire-hep.md) |
| CORE | ✅ | Optional | OA full-text downloads | Very strict w/o key | [api-core.md](api-core.md) |
| Google Scholar | — | — | Broadest coverage (requires scraping) | IP-level throttling | [api-google-scholar.md](api-google-scholar.md) |

## Scraping Methods Quick Reference

| Method | Speed | Stability | Use Cases |
|--------|-------|-----------|-----------|
| web_read tool | ⭐⭐⭐⭐⭐ | ⭐⭐⭐ | Quick browsing; English page metadata may be missing |
| curl + BeautifulSoup | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | Scholar lists, Nature og meta, static pages |
| camoufox | ⭐⭐ | ⭐⭐⭐⭐⭐ | JS-rendered pages, anti-detection needs (recommended) |
| playwright-stealth v2 | ⭐⭐ | ⭐⭐⭐⭐ | JS-rendered pages (Chromium-based) |

## Common Scenario Quick Routing

| "I want to..." | Recommended API/Method | Reference Pipeline |
|---------|--------------|--------------|
| Search for highly-cited papers on a topic | OpenAlex `sort=cited_by_count:desc` | discovery |
| Look up detailed info for a DOI | CrossRef → OpenAlex | obtain-pdf |
| Find a free PDF for a paper | Unpaywall | obtain-pdf |
| Download an arXiv paper | Direct link `/pdf/{ID}.pdf` | obtain-pdf |
| Look up an astrophysics paper by bibcode | NASA ADS API | api-nasa-ads |
| Search high-energy physics literature | INSPIRE-HEP | api-inspire-hep |
| Look up a biomedical article by PMID | Europe PMC | api-europe-pmc |
| View yearly trends in a field | OpenAlex year-by-year query | scholar-analysis |
| Find all papers by an author | OpenAlex `filter=author.id:{id}` | scholar-analysis |
| Find who cited a paper | OpenAlex `filter=cites:{doi}` | scholar-analysis |
| Generate APA references | citation-tracking pipeline | citation-tracking |
| Export BibTeX | NASA ADS or INSPIRE-HEP (built-in), or citation-tracking | citation-tracking |
| Scrape Scholar search results | curl+BS (≤1 request/session) | discovery |
| Scrape Nature full text | camoufox + domcontentloaded | obtain-pdf |

## Key Notes

1. **Google Scholar max 1 request per session** — 429 risk is very high; on 429, fall back to OpenAlex
2. **Nature/Springer always use `domcontentloaded`** — `networkidle` causes infinite loading timeouts
3. **Major paid publishers return 403** — Wiley/Elsevier/Science/PNAS almost always 403; API is the only option
4. **arXiv PDF has no direct link** — No direct PDF link on the page; derive `/pdf/{ID}.pdf` from the ID
5. **Playwright stealth is outdated** — Use camoufox or playwright-stealth v2 instead of the old API
6. **Scanned PDFs** — PyMuPDF cannot extract text; OCR is needed (tesseract / ocrmypdf)

## Pipeline Relationships

```
discovery (find papers)
    ↓ paper list + DOI
obtain-pdf (get full text)
    ↓ PDF files + metadata
scholar-analysis (analyze trends)  →  citation-tracking (format citations + generate review)
```
