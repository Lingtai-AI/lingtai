# Google Scholar Reference

> **Note:** This file mentions `web_read` as a tool — that tool was removed.
> URL-fetching guidance now lives in the built-in `web-search-manual`,
> returned by `web_search(action="manual")`. Wherever you see
> `web_read(url=...)` below, substitute either `curl` (then parse with
> BeautifulSoup — see the manual's `reference/tier-2-beautifulsoup.md`) or
> `playwright` for JS-rendered / bot-protected pages
> (see `reference/tier-3-playwright.md` in that manual).

## API Overview

Google Scholar does not offer an official public API. This reference document provides two complementary approaches for accessing Scholar data:

1. **Scraping** — Use curl (or playwright for bot-protected pages) to fetch the raw content of Scholar pages
2. **HTML Parsing** — Use BeautifulSoup to extract structured bibliographic metadata from the raw HTML

Each approach serves a different purpose: scraping solves the "get data" problem, while parsing solves the "understand data" problem.

| Attribute | Description |
|-----------|-------------|
| Base URL | `https://scholar.google.com/` |
| Authentication | No API key required |
| Anti-scraping | Google employs strong anti-bot policies; high-frequency requests will trigger CAPTCHA |
| Use cases | Citation data, author profiles, cross-disciplinary search |
| Alternatives | For large-scale needs, consider Semantic Scholar or OpenAlex → see [api-pubmed.md](api-pubmed.md) |

---

## Part 1: Scraping

### Profile Pages

Retrieve a scholar's publication list, citation counts, and publication years.

**URL formats:**

| Purpose | URL |
|---------|-----|
| Scholar homepage | `https://scholar.google.com/citations?user={USER_ID}&hl=en` |
| Sorted by publication date | `https://scholar.google.com/citations?hl=en&user={USER_ID}&view_op=list_works&sortby=pubdate` |
| Sorted by citation count (default) | `https://scholar.google.com/citations?hl=en&user={USER_ID}&view_op=list_works&sortby=citationcount` |

`{USER_ID}` is the Google Scholar user ID (e.g., `rcQwoOoAAAAJ`).

### Search Result Pages

Search for academic papers by keyword.

```
https://scholar.google.com/scholar?q={keywords}&hl=en
```

### Fetching the HTML

`web_read` was removed — save the page with curl (spoof a browser User-Agent),
then parse with BeautifulSoup (Part 2). For JS-rendered/bot-protected pages use
playwright via `web-search-manual`.

```bash
curl -s "https://scholar.google.com/citations?user=rcQwoOoAAAAJ&hl=en" \
  -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36" \
  -H "Accept: text/html,application/xhtml+xml" -H "Accept-Language: en-US,en;q=0.5" \
  --max-time 15 -o /tmp/scholar.html
```

Profile-page records span 2–3 lines (title / author+journal / `venue | citations
| year`, e.g. `The Astrophysical Journal 950 (2), 154, 2023 | 38 | 2023`); split
each work list on `|`. Search-result pages parse via the CSS selectors below.

---

## Part 2: HTML Parsing

After curl fetches the raw HTML of Google Scholar search results, use BeautifulSoup to extract structured metadata.

### CSS Selector Reference

| Element | Selector | Extracted Content |
|---------|----------|-------------------|
| Bibliographic entry | `.gs_ri` | Container for the entire bibliographic record |
| Title + link | `.gs_rt a` | Paper title and navigation link |
| Authors / journal / year | `.gs_a` | Authors, publication journal, year (green line) |
| Abstract | `.gs_rs` | Abstract snippet displayed in search results |
| Citation / related links | `.gs_fl` | Bottom link area (Cited by N, Related articles) |
| Related articles link | `.gs_fl a[href*="related:"]` | "Related articles" link |
| All versions link | `.gs_fl a[href*="cluster="]` | "All N versions" link |

### Parsing script

Iterate `.gs_ri` containers and pull fields with the selectors above. Two
non-obvious steps: fix `<b>`-split words in title/meta/abstract with
`re.sub(r'([a-z])([A-Z])', r'\1 \2', text)`, and read citation count from `Cited
by (\d+)` in the container text.

```python
from bs4 import BeautifulSoup   # pip install beautifulsoup4 requests
import re

def parse_scholar_html(html_path):
    soup = BeautifulSoup(open(html_path, encoding="utf-8").read(), "html.parser")
    fix = lambda t: re.sub(r"([a-z])([A-Z])", r"\1 \2", t or "")
    out = []
    for art in soup.select(".gs_ri"):
        t = art.select_one(".gs_rt a")
        a = art.select_one(".gs_a")
        meta = a.get_text(strip=True) if a else ""
        cit = re.search(r"Cited by (\d+)", art.get_text())
        yr = re.search(r"\b(19|20)\d{2}\b", meta)
        links = {"related_link": "", "versions_link": ""}
        for ln in art.select(".gs_fl a"):
            href = ln.get("href", "")
            if "related:" in href: links["related_link"] = "https://scholar.google.com" + href
            if "cluster=" in href and "cites=" not in href: links["versions_link"] = "https://scholar.google.com" + href
        out.append({
            "title": fix(t.get_text() if t else ""), "link": t.get("href", "") if t else "",
            "authors": [x.get_text(strip=True) for x in a.select("a")] if a else [],
            "meta": fix(meta), "year": yr.group() if yr else None,
            "abstract": fix(art.select_one(".gs_rs").get_text() if art.select_one(".gs_rs") else ""),
            "citations": int(cit.group(1)) if cit else 0, **links,
        })
    return out
```

---

## PDF Download for arXiv Papers

arXiv results have direct PDFs at `https://arxiv.org/pdf/{arXiv_ID}.pdf`
(`curl -s "https://arxiv.org/pdf/2512.12585" -o paper.pdf`). To find the ID from
a title, query the arXiv API (`GET https://export.arxiv.org/api/query?search_query=ti:{keywords}&max_results=1`)
and read the entry's `<id>` — see [api-arxiv.md](api-arxiv.md).

---

## Rate Limits and Anti-Scraping Strategies

| Strategy | Description |
|----------|-------------|
| Request interval | Wait at least 2–3 seconds between requests (random 2–5s jitter avoids blocks) |
| User-Agent | Use a real browser User-Agent (for curl scenarios) |
| CAPTCHA | High-frequency requests will trigger CAPTCHA; reduce frequency or rotate IPs |
| Large-scale needs | Use Semantic Scholar API or OpenAlex API as alternatives |

For polite fetching, add a `random.uniform(2, 5)` delay before each request plus
a browser User-Agent; `web-search-manual` has richer tiers (trafilatura,
Playwright stealth, Jina/Firecrawl).

## Error Handling

| Scenario | Handling |
|----------|----------|
| CAPTCHA page | Stop scraping, wait 10–30 minutes before retrying, or use Playwright/stealth browsing (see `web-search-manual`) |
| Empty results | Verify the USER_ID is correct, or try different search keywords |
| Parsing results are empty | Google may have updated the HTML structure; check if CSS selectors are still valid |
| Abnormal spaces in title | Caused by `<b>` tag splitting; fix with `re.sub(r'([a-z])([A-Z])', r'\1 \2', title)` |
| PDF link returns 403 | Some PDFs require institutional access; try finding a free version via Unpaywall |
| Timeout | Set `timeout=15`, retry up to 3 times |

## Known Limitations

1. **Title spacing issues**: Text in HTML may be split by `<b>` tags and requires regex post-processing
2. **Anti-scraping limits**: Large volumes of requests may trigger CAPTCHA; adding delays is recommended
3. **Logged-in users**: The HTML structure may differ when logged in
4. **No official API**: The structure may change at any time; parsing scripts require maintenance

## Related APIs

- **Unpaywall**: After finding a paper on Scholar, use the DOI to find a free PDF → see [api-unpaywall.md](api-unpaywall.md)
- **PubMed**: Official bibliographic API for biomedicine, more stable → see [api-pubmed.md](api-pubmed.md)
- **arXiv API**: `https://export.arxiv.org/api/query` — official search interface for arXiv papers
- **Alternatives**: For large-scale needs, Semantic Scholar (`https://api.semanticscholar.org/graph/v1/paper/search`) or OpenAlex (`https://api.openalex.org/works`) are recommended
