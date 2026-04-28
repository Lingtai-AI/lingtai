# CrossRef API Reference

## API Overview

CrossRef is the largest scholarly DOI registration agency. Its public API provides access to paper metadata, funder information, and journal search.

- **Base Endpoint**: `https://api.crossref.org`
- **Authentication**: No API key required; include a `User-Agent` header to join the Polite Pool for higher rate limits
- **Response Format**: JSON
- **Protocol**: HTTPS
- **Use Cases**: Paper metadata retrieval, DOI lookup, funder tracking, publication trend analysis

### Polite Pool Configuration

Include contact information in the request header to join the Polite Pool (rate limit increases from ~10 req/s to ~50 req/s):

```python
HEADERS = {"User-Agent": "MyApp/1.0 (mailto:your@email.com)"}
```

---

## 1. Basic Queries (Works Endpoint)

### Endpoint

```
GET https://api.crossref.org/works
```

### Query Parameters

| Parameter | Description | Example |
|---|---|---|
| `query` | Full-text search | `query=attention is all you need` |
| `query.title` | Title search | `query.title=transformer` |
| `query.author` | Author search | `query.author=vaswani` |
| `query.bibliographic` | Bibliographic search | `query.bibliographic=deep learning NLP` |
| `rows` | Number of results (default 20, max 100) | `rows=5` |
| `offset` | Pagination offset | `offset=20` |
| `select` | Fields to return (comma-separated) | `select=DOI,title,author,published-print` |
| `sort` | Sort field | `sort=published-print` |
| `order` | Sort direction: `asc` / `desc` | `order=desc` |
| `filter` | Advanced filters (comma-separated for multiple conditions) | `filter=from-pub-date:2020-01-01,type:journal-article` |

### Selectable Return Fields

Commonly used fields: `DOI`, `title`, `author`, `published-print`, `published-online`, `journal`, `publisher`, `type`, `volume`, `issue`, `page`, `abstract`, `citationCount`, `subject`, `ISSN`, `URL`, `link`, `funder`, `award`

### Advanced Filters (filter)

| Filter | Description | Example |
|---|---|---|
| `from-pub-date` | Start publication date | `from-pub-date:2020-01-01` |
| `until-pub-date` | End publication date | `until-pub-date:2024-12-31` |
| `type` | Work type | `type:journal-article` |
| `issn` | Journal ISSN | `issn:0957-4174` |
| `prefix` | DOI prefix (publisher) | `prefix:10.1038` |
| `container-title` | Journal name | `container-title:Nature` |
| `funder` | Funder DOI | `funder:10.13039/100000001` |
| `award` | Grant number | `award:CBET-1234567` |
| `has-abstract` | Has abstract | `has-abstract:true` |
| `has-funder` | Has funder information | `has-funder:true` |

### Work Types (type)

Common values: `journal-article`, `book-chapter`, `book`, `proceedings-article`, `dissertation`, `report`, `dataset`, `preprint`

### Code Example

```python
import requests

HEADERS = {"User-Agent": "MyApp/1.0 (mailto:your@email.com)"}
BASE = "https://api.crossref.org"

def search_works(query, rows=5, select="DOI,title,author,published-print", **filters):
    """Search CrossRef papers.

    Args:
        query: Search terms
        rows: Number of results (1-100)
        select: Fields to return (comma-separated)
        **filters: Additional filters, e.g. type='journal-article', from_pub_date='2020-01-01'

    Returns:
        list[dict]: List of papers
    """
    params = {"query": query, "rows": rows, "select": select}
    if filters:
        filter_parts = []
        for k, v in filters.items():
            key = k.replace("_", "-")
            filter_parts.append(f"{key}:{v}")
        params["filter"] = ",".join(filter_parts)

    r = requests.get(f"{BASE}/works", params=params, headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]["items"]

# Basic search
papers = search_works("transformer architecture", rows=3)
for p in papers:
    title = p.get("title", ["N/A"])[0]
    authors = ", ".join(a["family"] for a in p.get("author", []))
    doi = p.get("DOI", "N/A")
    print(f"DOI: {doi}")
    print(f"Title: {title}")
    print(f"Authors: {authors}")
    print()
```

### Response Format

```json
{
  "status": "ok",
  "message-type": "work-list",
  "message": {
    "total-results": 1326968,
    "items-per-page": 5,
    "query": { "search-terms": "transformer", "start-index": 0 },
    "items": [
      {
        "DOI": "10.1007/978-3-031-84300-6_13",
        "title": ["Is Attention All You Need?"],
        "author": [
          { "given": "Patrick", "family": "Mineault", "sequence": "first" }
        ],
        "published-print": { "date-parts": [[2025, 6, 15]] },
        "type": "journal-article",
        "container-title": ["Nature Neuroscience"],
        "publisher": "Springer"
      }
    ]
  }
}
```

---

## 2. Funder Queries (Funders Endpoint)

### Endpoint

```
GET https://api.crossref.org/funders
```

### Parameters

| Parameter | Description | Example |
|---|---|---|
| `query` | Search for funding agencies | `query=NSF` |
| `rows` | Number of results | `rows=5` |

### Common Funder DOIs

| Funding Agency | DOI Identifier |
|---|---|
| NIH (National Institutes of Health) | `10.13039/100000002` |
| NSF (National Science Foundation) | `10.13039/100000001` |
| DOE (U.S. Department of Energy) | `10.13039/100000015` |
| EU (European Union) | `10.13039/501100000780` |
| Wellcome Trust | `10.13039/100004440` |
| DFG (German Research Foundation) | `10.13039/501100001659` |
| JSPS (Japan Society for the Promotion of Science) | `10.13039/501100001691` |
| NSFC (National Natural Science Foundation of China) | `10.13039/501100001809` |

### Code Example

```python
def search_funders(query, rows=5):
    """Search for funding agencies."""
    params = {"query": query, "rows": rows}
    r = requests.get(f"{BASE}/funders", params=params, headers=HEADERS, timeout=10)
    r.raise_for_status()
    return r.json()["message"]["items"]

def get_funder_works(funder_doi, rows=5):
    """Retrieve papers funded by a specific agency.

    Args:
        funder_doi: Funder DOI (e.g. '10.13039/100000001' for NSF)
        rows: Number of results
    """
    params = {
        "filter": f"funder:{funder_doi}",
        "rows": rows,
        "select": "DOI,title,author,published-print,funder,award",
        "sort": "published-print",
        "order": "desc",
    }
    r = requests.get(f"{BASE}/works", params=params, headers=HEADERS, timeout=10)
    r.raise_for_status()
    return r.json()["message"]["items"]

# Search for funding agencies
funders = search_funders("National Science Foundation")
for f in funders:
    print(f"{f['name']} (ID: {f['id']})")
    print(f"  Location: {f.get('location', 'N/A')}")

# Get recent papers funded by NSF
nsf_works = get_funder_works("10.13039/100000001", rows=5)
for w in nsf_works:
    title = w.get("title", ["N/A"])[0]
    awards = [a.get("award", []) for a in w.get("funder", [])]
    flat_awards = [str(a) for sub in awards for a in sub]
    print(f"[NSF] {title}")
    if flat_awards:
        print(f"  Award: {', '.join(flat_awards[:3])}")
```

### Response Format (Funders)

```json
{
  "message": {
    "items": [
      {
        "id": "100000001",
        "location": "United States",
        "name": "National Science Foundation",
        "alt-names": ["NSF"],
        "uri": "http://dx.doi.org/10.13039/100000001",
        "tokens": ["national", "science", "foundation"]
      }
    ]
  }
}
```

---

## 3. Recent Publications (Date-filtered Queries)

### How It Works

Combine the `from-pub-date` / `until-pub-date` filters with `sort=published-print` and `order=desc` to track the latest papers.

### Code Example

```python
from datetime import date, timedelta

def get_recent_papers(topic=None, days=7, rows=20, funder=None, journal=None):
    """Retrieve recent papers.

    Args:
        topic: Search keyword (optional)
        days: Number of days to look back
        rows: Number of results
        funder: Funder DOI (optional)
        journal: Journal name (optional)

    Returns:
        list[dict]: Papers sorted by publication date in descending order
    """
    today = date.today()
    since = today - timedelta(days=days)
    filters = [f"from-pub-date:{since}"]
    if funder:
        filters.append(f"funder:{funder}")
    if journal:
        filters.append(f"container-title:{journal}")

    params = {
        "rows": rows,
        "filter": ",".join(filters),
        "sort": "published-print",
        "order": "desc",
        "select": "DOI,title,author,published-print,published-online",
    }
    if topic:
        params["query"] = topic

    r = requests.get(f"{BASE}/works", params=params, headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]["items"]

def daily_digest(topic, rows=20):
    """Daily digest: retrieve today's papers on a specific topic."""
    return get_recent_papers(topic=topic, days=1, rows=rows)

# Usage examples
# Papers about "transformer" from the last 7 days
papers = get_recent_papers("transformer", days=7)
print(f"Found {len(papers)} recent papers on 'transformer'")
for p in papers[:5]:
    title = p.get("title", ["N/A"])[0]
    pub = p.get("published-print", p.get("published-online", {}))
    dp = pub.get("date-parts", [[None]])[0]
    date_str = "-".join(str(x) for x in dp if x is not None)
    print(f"  [{date_str}] {title[:80]}")

# Specific funder + specific journal
nsf_nature = get_recent_papers(days=30, funder="10.13039/100000001", journal="Nature")

# Daily digest
today_papers = daily_digest("large language model", rows=10)
```

### Advanced Filter Combinations

```bash
# Specific date range + journal type
curl -s "https://api.crossref.org/works?filter=from-pub-date:2026-04-01,until-pub-date:2026-04-22,type:journal-article&rows=5&select=DOI,title,published-print"

# Nature papers with abstracts
curl -s "https://api.crossref.org/works?filter=container-title:Nature,has-abstract:true&rows=3&select=DOI,title,abstract"
```

---

## Rate Limits

| Pool Type | Rate | How to Access |
|---|---|---|
| Public Pool | ~10 requests/s | Default |
| Polite Pool | ~50 requests/s | Include `User-Agent: AppName/Version (mailto:email)` in request header |
| Plus Pool | ~200 requests/s | Requires paid CrossRef Plus membership |

**Best Practices**:
- Always include a `User-Agent` header to join the Polite Pool
- Add 0.05–0.1 second delays between bulk requests
- Use the `select` parameter to retrieve only the fields you need, reducing response size

## Error Handling

| HTTP Status | Meaning | Handling |
|---|---|---|
| 200 | Success | Parse normally |
| 400 | Bad request | Check filter syntax and parameter values |
| 404 | DOI not found | Verify the DOI is correct |
| 429 | Rate limited | Back off and retry; verify you are in the Polite Pool |
| 503 | Service temporarily unavailable | Retry with exponential backoff |

```python
import time

def crossref_get(url, params=None, retries=3):
    """CrossRef request with retry and backoff."""
    for attempt in range(retries):
        r = requests.get(url, params=params, headers=HEADERS, timeout=15)
        if r.status_code == 200:
            return r.json()
        elif r.status_code == 429:
            wait = min(30, 2 ** attempt * 2)
            print(f"Rate limited, waiting {wait}s...")
            time.sleep(wait)
        elif r.status_code >= 500:
            time.sleep(2 ** attempt)
        else:
            r.raise_for_status()
    raise Exception(f"CrossRef request failed after {retries} retries: {url}")
```

## Related APIs

- → See [api-arxiv.md](api-arxiv.md) — Searching preprints (arXiv papers are typically published here first)
- → See [api-doi-resolver.md](api-doi-resolver.md) — Resolve individual DOIs to full metadata
