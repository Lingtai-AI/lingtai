# Pipeline: Academic Paper Discovery

> Discover papers from any starting point: Google Scholar page → Author name → Keyword → DOI, progressively deepening as needed.

## Goal

Given a starting point (keyword / author name / Scholar page URL / DOI), quickly return a batch of candidate papers with their titles, authors, citation counts, abstracts, and source links.

---

## Workflow Steps

1. **Identify input type** — Keyword / Author name / Scholar URL / DOI?
2. **Select the optimal channel** — Choose option A / B / C / D based on the decision tree below.
3. **Execute scraping / API call** — Retrieve the candidate paper list.
4. **Standardize output** — Unify into a `{title, authors, year, citations, doi, url, snippet}` list.
5. **(Optional) Deep dive** — For individual papers of interest → see [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md).

---

## Decision Tree

```
What is the input?
├── Keyword / phrase
│   ├── Need Scholar page-level data (citation count, snippet)?
│   │   ├── Yes → Option B: curl + BeautifulSoup
│   │   └── No  → Option D: OpenAlex API (structured, fastest)
│   └── Physics or CS field?
│       └── Yes → Option D': arXiv API (preprints first)
│
├── Google Scholar URL (citations?user=... or scholar?q=...)
│   ├── Quick title browsing → curl the page and skim
│   └── Complete data        → Option B: curl + BeautifulSoup
│
├── Author name
│   └── Option D: OpenAlex /author endpoint → returns author profile + representative works
│
├── DOI
│   └── Already have an exact target → skip discovery, go directly to → see [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md)
│
└── Cannot determine → Default Option B: curl + BeautifulSoup (most versatile)
```

---

## Code Examples

Standardize every channel to `{title, authors, year, citations, doi, url, snippet}`.

**Option D — OpenAlex / arXiv (structured, fastest; prefer this):**

```python
import requests, re

def search_openalex(query, limit=10):
    r = requests.get("https://api.openalex.org/works", params={
        "filter": f"title_and_abstract.search:{query}",
        "sort": "cited_by_count:desc", "per_page": limit}, timeout=10).json()
    return [{"title": w.get("display_name"), "year": w.get("publication_year"),
             "citations": w.get("cited_by_count", 0), "doi": w.get("doi", ""),
             "url": w.get("id")} for w in r.get("results", [])]

# arXiv (physics/CS first): GET export.arxiv.org/api/query?search_query=all:{q}&sortBy=submittedDate
```

**Option B — curl + BeautifulSoup** (when you need Scholar page-level snippets/
citation counts): fetch `scholar.google.com/scholar?q={q}&hl=en` with a browser
User-Agent and parse `.gs_ri` — full selector/parser and the `<b>`-split title
fix are in [api-google-scholar.md](api-google-scholar.md).

**Option C — Camoufox** (when B is 429-blocked; migrated off the old
`playwright_stealth` API). `pip install camoufox && python -m camoufox fetch`,
then drive a headless browser and read profile rows `tr.gsc_a_tr` (title
`td.gsc_a_t a`, citations `td.gsc_a_c a`, year `td.gsc_a_y a`). Cap at 10–20
req/min to avoid 429.

(`web_read` was removed — for quick title browsing just curl the page and skim.)

---

## Failure Fallbacks

| Scenario | Symptom | Fallback Strategy |
|----------|---------|-------------------|
| Scholar returns 429 | curl is blocked | ① Wait 60s and retry ② Switch to OpenAlex API ③ Use Camoufox + proxy |
| BeautifulSoup selectors fail | Returns empty list | Scholar may have changed HTML structure; check if `.gs_ri` / `.gs_rt` still exist |
| OpenAlex returns empty | 0 results | Check query syntax, or fall back to Scholar scraping |
| Camoufox timeout | timeout error | Increase `timeout`, check network connectivity, or revert to Option B |
| English Scholar page metadata missing | Authors/citations are empty | Try the Chinese Scholar page (`hl=zh-CN`), or switch to API |

---

## Related Pipelines

- Get paper full text → see [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md)
- Analyze citation networks and trends → see [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md)
- Format references → see [pipeline-citation-tracking.md](pipeline-citation-tracking.md)
- Comprehensive entry: What information do I have, and which API should I use? → see [decision-tree.md](decision-tree.md)
