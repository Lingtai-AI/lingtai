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
│   ├── Quick title browsing → Option A: web_read tool
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

### Option A: web_read Tool (zero code, fastest)

Use when: Quickly browsing titles and type identification. Metadata (authors, citation count, DOI) is largely missing.

```python
# Using the web_read tool (not Python, call the tool directly)
# web_read({ url: "https://scholar.google.com/citations?user=XXXXXXX&hl=en", output_format: "text" })
```

### Option B: curl + BeautifulSoup (Recommended! Tested and Working)

Dependency: `pip install requests beautifulsoup4 lxml`

```python
import re
import requests
from bs4 import BeautifulSoup


def scrape_scholar(query: str, limit: int = 10) -> list[dict]:
    """Scrape Scholar search results with curl + BeautifulSoup, returning a standardized paper list."""
    headers = {
        "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
        "Accept-Language": "en-US,en;q=0.9",
    }
    search_url = f"https://scholar.google.com/scholar?q={query.replace(' ', '+')}&hl=en"
    r = requests.get(search_url, headers=headers, timeout=15)
    soup = BeautifulSoup(r.text, "lxml")

    papers = []
    for item in soup.select(".gs_ri")[:limit]:
        title_tag = item.select_one(".gs_rt a")
        raw_title = title_tag.get_text() if title_tag else ""
        # Key fix: Scholar HTML splits titles with <b> tags, spaces need to be restored
        title = re.sub(r"([a-z])([A-Z])", r"\1 \2", raw_title).strip()

        meta = item.select_one(".gs_a")
        meta_text = meta.get_text(strip=True) if meta else ""

        cite_tag = item.select_one(".gs_fl a")
        cit_match = re.search(r"Cited by (\d+)", cite_tag.get_text() if cite_tag else "")
        citations = int(cit_match.group(1)) if cit_match else 0

        link = title_tag["href"] if title_tag and title_tag.has_attr("href") else ""
        snippet_tag = item.select_one(".gs_rs")
        snippet = snippet_tag.get_text(strip=True) if snippet_tag else ""

        papers.append({
            "title": title,
            "authors_meta": meta_text,
            "citations": citations,
            "url": link,
            "snippet": snippet,
        })
    return papers


# Usage
for p in scrape_scholar("transformer attention is all you need"):
    print(f"[{p['citations']} cites] {p['title'][:60]}")
```

### Option C: Camoufox (When B Fails / Is Blocked)

> ⚠️ Migrated from the old `playwright_stealth` API to Camoufox.

Dependency: `pip install camoufox && python -m camoufox fetch`

```python
from camoufox.sync_api import Camoufox


def scrape_scholar_camoufox(url: str) -> list[dict]:
    """Use Camoufox browser to bypass anti-scraping and fetch Scholar pages."""
    with Camoufox(headless=True) as browser:
        page = browser.new_page()
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(3000)

        papers = []
        for row in page.query_selector_all("tr.gsc_a_tr"):
            title_el = row.query_selector("td.gsc_a_t a")
            cite_el = row.query_selector("td.gsc_a_c a")
            year_el = row.query_selector("td.gsc_a_y a")
            papers.append({
                "title": title_el.inner_text() if title_el else None,
                "url": title_el.get_attribute("href") if title_el else None,
                "citations": cite_el.inner_text() if cite_el else "0",
                "year": year_el.inner_text() if year_el else None,
            })
        return papers
```

**Rate limit**: No more than 10–20 requests per minute to avoid triggering 429.

### Option D: OpenAlex API (Structured Data, Fastest)

```python
import requests


def search_openalex(query: str, limit: int = 10) -> list[dict]:
    """Search papers with the OpenAlex API, returning structured data."""
    r = requests.get(
        "https://api.openalex.org/works",
        params={
            "filter": f"title_and_abstract.search:{query}",
            "sort": "cited_by_count:desc",
            "per_page": limit,
        },
        timeout=10,
    ).json()

    return [
        {
            "title": w.get("display_name"),
            "year": w.get("publication_year"),
            "citations": w.get("cited_by_count", 0),
            "doi": w.get("doi", ""),
            "url": w.get("id"),
        }
        for w in r.get("results", [])
    ]


# arXiv-specific
def search_arxiv(query: str, limit: int = 10) -> list[str]:
    """Search via arXiv API, returning a list of titles."""
    r = requests.get(
        "https://export.arxiv.org/api/query",
        params={
            "search_query": f"all:{query}",
            "max_results": limit,
            "sortBy": "submittedDate",
            "sortOrder": "descending",
        },
        timeout=10,
    )
    titles = re.findall(r"<title>(.+?)</title>", r.text)
    return [t for t in titles if not t.strip().startswith("arXiv")][:limit]
```

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
