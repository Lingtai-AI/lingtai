# arXiv API Reference

## API Overview

arXiv provides a public search API based on the Open Archives Initiative (OAI) protocol for retrieving preprint metadata and full-text PDFs.

- **Endpoint**: `https://export.arxiv.org/api/query`
- **Authentication**: No API key required; fully open access
- **Response format**: Atom XML
- **Protocol**: HTTPS enforced (HTTP automatically redirects via 301)
- **Best for**: Preprint retrieval, paper metadata extraction, automated scholarly literature tracking

## Endpoints & Parameters

### Search Endpoint

| Parameter | Description | Example |
|---|---|---|
| `search_query` | Search expression; supports field prefixes and Boolean operators | `ti:transformer+AND+au:vaswani` |
| `start` | Result offset (default 0) | `start=5` |
| `max_results` | Maximum number of results (default 25) | `max_results=10` |
| `sortBy` | Sort field: `relevance` / `lastUpdatedDate` / `submittedDate` | `sortBy=submittedDate` |
| `sortOrder` | Sort direction: `descending` / `ascending` | `sortOrder=descending` |
| `id_list` | Comma-separated arXiv IDs for direct paper lookup | `id_list=1706.03762,1806.11202` |

### Field Prefixes

| Prefix | Field | Example |
|---|---|---|
| `ti:` | Title | `ti:attention` |
| `au:` | Author | `au:vaswani` |
| `abs:` | Abstract | `abs:neural machine translation` |
| `all:` | All fields | `all:transformer architecture` |
| `cat:` | Category | `cat:cs.CL` |
| `co:` | Comment | `co:NeurIPS` |
| `jr:` | Journal reference | `jr:JHEP` |
| `rn:` | Report number | `rn:NSF-1234` |

**Boolean operators** (must be uppercase): `AND`, `OR`, `ANDNOT`

```bash
# Compound query example: title contains "transformer" and author is not "smith"
curl -s "https://export.arxiv.org/api/query?search_query=ti:transformer+ANDNOT+au:smith&max_results=5"
```

### Common Category Codes

| Code | Discipline |
|---|---|
| `cs.CL` | Computation and Language |
| `cs.AI` | Artificial Intelligence |
| `cs.LG` | Machine Learning |
| `cs.CV` | Computer Vision |
| `math.CO` | Combinatorics |
| `physics.hep-th` | High Energy Physics — Theory |
| `q-bio.NC` | Neurons and Cognition |
| `stat.ML` | Statistics — Machine Learning |

## Code Examples

### Basic Search

```python
import urllib.request
import xml.etree.ElementTree as ET

def search_arxiv(query, max_results=10, sort_by="relevance", sort_order="descending"):
    """Search arXiv papers.

    Args:
        query: Search expression (supports field prefixes, e.g. ti:transformer)
        max_results: Maximum number of results to return
        sort_by: Sort field (relevance / lastUpdatedDate / submittedDate)
        sort_order: Sort direction (descending / ascending)

    Returns:
        list[dict]: Paper list, each containing title, authors, published, abstract, pdf_link, arxiv_id
    """
    url = (
        f"https://export.arxiv.org/api/query?"
        f"search_query={query}&max_results={max_results}"
        f"&sortBy={sort_by}&sortOrder={sort_order}"
    )
    data = urllib.request.urlopen(url, timeout=15).read().decode("utf-8")
    root = ET.fromstring(data)
    ns = {"atom": "http://www.w3.org/2005/Atom", "arxiv": "http://arxiv.org/schemas/atom"}

    results = []
    for entry in root.findall("atom:entry", ns):
        title = entry.find("atom:title", ns).text.strip().replace("\n", " ")
        authors = [a.find("atom:name", ns).text for a in entry.findall("atom:author", ns)]
        published = entry.find("atom:published", ns).text[:10]
        summary = entry.find("atom:summary", ns).text.strip().replace("\n", " ")
        arxiv_id = entry.find("atom:id", ns).text.split("/")[-1]

        pdf_link = None
        for link in entry.findall("atom:link", ns):
            if link.attrib.get("title") == "pdf":
                pdf_link = link.attrib["href"]
                break

        results.append({
            "arxiv_id": arxiv_id,
            "title": title,
            "authors": authors,
            "published": published,
            "abstract": summary,
            "pdf_link": pdf_link,
        })
    return results

# Usage example
papers = search_arxiv("ti:transformer+AND+au:vaswani", max_results=3)
for p in papers:
    print(f"[{p['published']}] {p['title']}")
    print(f"  Authors: {', '.join(p['authors'])}")
    print(f"  PDF: {p['pdf_link']}")
    print(f"  Abstract: {p['abstract'][:150]}...")
    print()
```

### Paginated Iteration

```python
def search_arxiv_all(query, total=100, per_page=50):
    """Paginate through large result sets. arXiv recommends no more than 50 results per request to avoid timeouts."""
    all_results = []
    for start in range(0, total, per_page):
        count = min(per_page, total - start)
        batch = search_arxiv(query, max_results=count)
        all_results.extend(batch)
        if len(batch) < count:
            break  # No more results
    return all_results
```

### Direct Lookup by arXiv ID

```python
def get_by_arxiv_id(arxiv_id):
    """Fetch a single paper by its arXiv ID."""
    url = f"https://export.arxiv.org/api/query?id_list={arxiv_id}"
    data = urllib.request.urlopen(url, timeout=15).read().decode("utf-8")
    root = ET.fromstring(data)
    ns = {"atom": "http://www.w3.org/2005/Atom"}
    entry = root.find("atom:entry", ns)
    if entry is None:
        return None
    return {
        "title": entry.find("atom:title", ns).text.strip().replace("\n", " "),
        "id": entry.find("atom:id", ns).text,
        "published": entry.find("atom:published", ns).text[:10],
    }
```

## Response Format

The response is Atom XML with the following main structure:

```xml
<?xml version="1.0" encoding="UTF-8"?>
<feed xmlns="http://www.w3.org/2005/Atom"
      xmlns:opensearch="http://a9.com/-/spec/opensearch/1.1/"
      xmlns:arxiv="http://arxiv.org/schemas/atom">
  <opensearch:totalResults>1234</opensearch:totalResults>
  <opensearch:startIndex>0</opensearch:startIndex>
  <opensearch:itemsPerPage>10</opensearch:itemsPerPage>
  <entry>
    <id>http://arxiv.org/abs/1706.03762v1</id>
    <title>Attention Is All You Need</title>
    <summary>Abstract text...</summary>
    <published>2017-06-12T17:34:57Z</published>
    <updated>2017-12-06T17:05:42Z</updated>
    <author><name>Ashish Vaswani</name></author>
    <link rel="alternate" type="text/html" href="http://arxiv.org/abs/1706.03762v1"/>
    <link title="pdf" rel="related" type="application/pdf" href="http://arxiv.org/pdf/1706.03762v1"/>
    <arxiv:primary_category xmlns:arxiv="http://arxiv.org/schemas/atom" term="cs.CL"/>
    <category term="cs.CL"/>
    <category term="cs.AI"/>
    <arxiv:comment>12 pages, 5 figures</arxiv:comment>
  </entry>
</feed>
```

### Key XML Paths

| Path | Description |
|---|---|
| `feed/opensearch:totalResults` | Total match count |
| `entry/id` | arXiv ID (e.g. `1706.03762v1`) |
| `entry/title` | Paper title |
| `entry/summary` | Abstract (may contain LaTeX) |
| `entry/published` | Initial submission date |
| `entry/updated` | Last update date |
| `entry/author/name` | Author name |
| `entry/link[@title='pdf']/@href` | PDF download link |
| `entry/link[@rel='alternate']/@href` | HTML abstract page |
| `entry/arxiv:primary_category/@term` | Primary category |
| `entry/category/@term` | All categories |
| `entry/arxiv:comment` | Author comments |

## Rate Limits

| Limit Type | Value |
|---|---|
| Recommended max rate | ~3 requests/second |
| Max results per request | No hard limit, but ≤ 50 recommended |
| Connection timeout | 15 seconds recommended |
| Peak response time | May take several seconds |

**Best practices:**
- Add a 0.5–1 second delay between requests
- Fetch ≤ 50 results per page during pagination
- Avoid issuing a large number of concurrent requests in a short window
- Use `time.sleep()` for throttling

## Error Handling

| Scenario | Resolution |
|---|---|
| HTTP 301 | Request `https://` directly, or use `-L` in curl to follow redirects |
| Timeout | Increase timeout or retry with exponential backoff |
| Empty results | Verify query syntax, simplify search terms, try the `all:` prefix |
| XML parse error | Check whether the response is an HTML error page instead of Atom XML |
| Missing `title` in `entry` | Skip the entry (arXiv may still list removed entries in the index) |

```python
import time
import urllib.error

def search_arxiv_robust(query, max_results=10, retries=3):
    """Robust search with retry logic."""
    for attempt in range(retries):
        try:
            return search_arxiv(query, max_results)
        except urllib.error.URLError as e:
            if attempt < retries - 1:
                time.sleep(2 ** attempt)
            else:
                raise
```

## Related APIs

- → See [api-crossref.md](api-crossref.md) — Retrieve metadata for published papers via DOI
- → See [api-doi-resolver.md](api-doi-resolver.md) — Resolve DOIs to complete citation information
- arXiv papers generally lack DOIs; once formally published, you can search CrossRef by title to find the corresponding DOI
