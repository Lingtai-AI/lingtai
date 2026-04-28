# Google Scholar Reference

## API Overview

Google Scholar does not offer an official public API. This reference document provides two complementary approaches for accessing Scholar data:

1. **Scraping** — Use the `web_read` tool or curl to fetch the raw content of Scholar pages
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

### Scraping Method 1: web_read Tool (Recommended)

```python
# Use the web_read tool in the lingtai environment
from lingtai import web_read

# Fetch a scholar's publication list (sorted by date)
url = "https://scholar.google.com/citations?hl=en&user=rcQwoOoAAAAJ&view_op=list_works&sortby=pubdate"
content = web_read(url=url)
print(content[:2000])
```

**Advantages**: `web_read` has built-in anti-scraping handling and automatically manages User-Agent and page rendering.

### Scraping Method 2: curl

```bash
curl -s "https://scholar.google.com/citations?user=rcQwoOoAAAAJ&hl=en" \
  -H "User-Agent: Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36" \
  -H "Accept: text/html,application/xhtml+xml" \
  -H "Accept-Language: en-US,en;q=0.5" \
  --max-time 15 \
  -o /tmp/scholar.html
```

**Use when you need BeautifulSoup parsing** — first save the HTML with curl, then parse it with Python.

### Profile Page Data Format

The text returned by `web_read` uses a tab-separated format:

```
The Evolution of the 1/f Range within a Single Fast-solar-wind Stream...
N Davis, BDG Chandran, TA Bowen...
The Astrophysical Journal 950 (2), 154, 2023 | 38 | 2023
```

Each record typically spans 2–3 lines: a title line, an author/journal line, and a citation/year line (separated by `|`).

### Simple Parsing (web_read Scenario)

```python
import lingtai

USER_ID = "rcQwoOoAAAAJ"

url = f"https://scholar.google.com/citations?hl=en&user={USER_ID}&view_op=list_works&sortby=pubdate"
content = lingtai.web_read(url=url)

lines = content.strip().split('\n')
papers = []
i = 0
while i < len(lines):
    line = lines[i].strip()
    if line and '|' in line:
        parts = [p.strip() for p in line.split('|')]
        if len(parts) >= 3:
            paper = {
                'title': parts[0],
                'citations': parts[1] if parts[1] else '0',
                'year': parts[2],
                'authors_venue': lines[i-1].strip() if i > 0 else '',
                'raw': lines[i-2].strip() if i > 1 else ''
            }
            papers.append(paper)
    i += 1

print(f"Found {len(papers)} papers")
for p in papers[:3]:
    print(f"Title: {p['title']}")
    print(f"Year: {p['year']}, Citations: {p['citations']}")
    print("---")
```

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

### Complete Parsing Script

```python
from bs4 import BeautifulSoup
import re
import json

def parse_scholar_html(html_path):
    """Parse Google Scholar search result HTML and extract bibliographic metadata.

    Args:
        html_path: Path to the saved HTML file

    Returns:
        list[dict]: Parsed bibliographic list, each entry containing:
            - title (str): Title
            - link (str): Paper link
            - authors (list[str]): Author list
            - meta (str): Raw author + journal + year text
            - year (str|None): Publication year
            - abstract (str): Abstract snippet
            - citations (int): Citation count
            - related_link (str): Related articles link
            - versions_link (str): All versions link
    """
    with open(html_path, 'r', encoding='utf-8') as f:
        html = f.read()

    soup = BeautifulSoup(html, 'html.parser')
    articles = soup.select('.gs_ri')

    results = []
    for art in articles:
        result = {}

        # Title and link
        title_tag = art.select_one('.gs_rt a')
        raw_title = title_tag.get_text() if title_tag else ''
        # Fix words split by <b> tags: e.g., "Trans former" → "Transformer"
        result['title'] = re.sub(r'([a-z])([A-Z])', r'\1 \2', raw_title)
        result['link'] = title_tag.get('href', '') if title_tag else ''

        # Authors, journal, year
        gs_a = art.select_one('.gs_a')
        if gs_a:
            authors = [a.get_text(strip=True) for a in gs_a.select('a')]
            result['authors'] = authors
            meta_text = gs_a.get_text(strip=True)
            result['meta'] = re.sub(r'([a-z])([A-Z])', r'\1 \2', meta_text)
            year_match = re.search(r'\b(19|20)\d{2}\b', meta_text)
            result['year'] = year_match.group() if year_match else None
        else:
            result['authors'] = []
            result['meta'] = ''
            result['year'] = None

        # Abstract
        snippet_tag = art.select_one('.gs_rs')
        raw_snippet = snippet_tag.get_text() if snippet_tag else ''
        result['abstract'] = re.sub(r'([a-z])([A-Z])', r'\1 \2', raw_snippet)

        # Citation count
        cit_match = re.search(r'Cited by (\d+)', art.get_text())
        result['citations'] = int(cit_match.group(1)) if cit_match else 0

        # Associated links
        result['related_link'] = ''
        result['versions_link'] = ''
        for link in art.select('.gs_fl a'):
            href = link.get('href', '')
            if 'related:' in href:
                result['related_link'] = 'https://scholar.google.com' + href
            if 'cluster=' in href and 'cites=' not in href:
                result['versions_link'] = 'https://scholar.google.com' + href

        results.append(result)

    return results

# Usage example
papers = parse_scholar_html('/tmp/scholar.html')
print(json.dumps(papers, ensure_ascii=False, indent=2))

# Extract specific fields
for p in papers[:5]:
    print(f"Title: {p['title']}")
    print(f"Authors: {', '.join(p['authors'])}")
    print(f"Year: {p['year']}")
    print(f"Citations: {p['citations']}")
    print(f"Abstract: {p['abstract'][:100]}...")
    print("---")
```

### Dependency Installation

```bash
pip install beautifulsoup4 requests
```

---

## PDF Download for arXiv Papers

arXiv papers found via Google Scholar search have direct PDF links.

### URL Format

```
https://arxiv.org/pdf/{arXiv_ID}.pdf
```

### Download Example

```bash
# Direct download
curl -s "https://arxiv.org/pdf/2512.12585" -o paper.pdf
```

### Finding the arXiv ID by Title

```python
import requests
import xml.etree.ElementTree as ET

def find_arxiv_pdf_url(title_keywords):
    """Search for a paper title via the arXiv API and return the PDF URL."""
    url = "https://export.arxiv.org/api/query"
    params = {
        "search_query": f"ti:{title_keywords}",
        "max_results": 1,
    }
    r = requests.get(url, params=params, timeout=10)
    root = ET.fromstring(r.text)
    ns = {"atom": "http://www.w3.org/2005/Atom"}
    entry = root.find("atom:entry", ns)
    if entry is not None:
        arxiv_id = entry.find("atom:id", ns).text.split("/")[-1]
        return f"https://arxiv.org/pdf/{arxiv_id}.pdf"
    return None
```

---

## Rate Limits and Anti-Scraping Strategies

| Strategy | Description |
|----------|-------------|
| Request interval | Wait at least 2–3 seconds between requests |
| User-Agent | Use a real browser User-Agent (for curl scenarios) |
| CAPTCHA | High-frequency requests will trigger CAPTCHA; reduce frequency or rotate IPs |
| Prefer `web_read` | lingtai's `web_read` has built-in anti-scraping handling; use it first |
| Large-scale needs | Use Semantic Scholar API or OpenAlex API as alternatives |

### Best Practices to Avoid Being Blocked

```python
import time
import random

def polite_fetch(url, method="web_read"):
    """Polite fetching: random delay + User-Agent."""
    delay = random.uniform(2, 5)
    time.sleep(delay)

    if method == "web_read":
        from lingtai import web_read
        return web_read(url=url)
    else:
        import requests
        headers = {
            "User-Agent": "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36",
            "Accept": "text/html,application/xhtml+xml",
            "Accept-Language": "en-US,en;q=0.5",
        }
        r = requests.get(url, headers=headers, timeout=15)
        r.raise_for_status()
        return r.text
```

## Error Handling

| Scenario | Handling |
|----------|----------|
| CAPTCHA page | Stop scraping, wait 10–30 minutes before retrying, or use `web_read` |
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
