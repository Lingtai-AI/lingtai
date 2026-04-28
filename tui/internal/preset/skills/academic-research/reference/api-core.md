# CORE API Reference

CORE is a free academic resource aggregation platform that indexes over 200 million open-access papers worldwide and provides direct PDF download links.

## API Overview

| Item | Description |
|---|---|
| Base URL | `https://api.core.ac.uk/v3` |
| Authentication | Basic functionality available without an API key; a key unlocks additional features |
| Rate limit | ~100 requests/second |
| Response format | JSON |
| Best for | Finding free full-text papers, institutional repository content, PDF downloads |

Core endpoints:

| Endpoint | Purpose |
|---|---|
| `POST /search/works` | Search for papers |
| `GET /works/{id}` | Retrieve detailed information for a single paper (includes PDF download URL) |

---

## Endpoints & Parameters

### Search Papers

**Endpoint**: `POST https://api.core.ac.uk/v3/search/works`

The request body is JSON:

| Parameter | Type | Description | Example |
|---|---|---|---|
| `q` | string | Search query | `"machine learning"` |
| `limit` | int | Maximum number of results | `10` |
| `offset` | int | Pagination offset | `0` |

### Retrieve Paper Details

**Endpoint**: `GET https://api.core.ac.uk/v3/works/{workId}`

Returns complete metadata, including `downloadUrl` (direct PDF link).

---

## Code Examples

### Search Papers

```python
import requests

def search_core(query, limit=10, offset=0):
    """Search CORE academic papers.

    Args:
        query: Search query string
        limit: Maximum number of results (default 10)
        offset: Pagination offset (default 0)

    Returns:
        dict: Contains totalHits and a results list
    """
    url = "https://api.core.ac.uk/v3/search/works"
    payload = {"q": query, "limit": limit, "offset": offset}
    r = requests.post(url, json=payload, timeout=10)
    r.raise_for_status()
    return r.json()

# Example
results = search_core("transformer architecture", limit=5)
print(f"Total hits: {results['totalHits']}")

for paper in results['results']:
    print(f"\nTitle: {paper['title']}")
    print(f"Authors: {[a['name'] for a in paper.get('authors', [])]}")
    print(f"Year: {paper.get('yearPublished', 'N/A')}")
    print(f"DOI: {paper.get('doi', 'N/A')}")
    if paper.get('downloadUrl'):
        print(f"PDF: {paper['downloadUrl']}")
```

### Retrieve Paper Details and Download PDF

```python
def get_core_work(work_id):
    """Retrieve complete information for a single paper."""
    url = f"https://api.core.ac.uk/v3/works/{work_id}"
    r = requests.get(url, timeout=10)
    r.raise_for_status()
    return r.json()

def download_core_pdf(work_id, output_path):
    """Download a paper's PDF.

    Args:
        work_id: CORE paper ID
        output_path: Local file save path

    Returns:
        str: Saved file path on success; None when no PDF is available
    """
    data = get_core_work(work_id)
    if data.get('downloadUrl'):
        pdf = requests.get(data['downloadUrl'], timeout=30)
        with open(output_path, 'wb') as f:
            f.write(pdf.content)
        return output_path
    return None
```

### Filtered Search

```python
def search_core_advanced(query, limit=10, year_from=None, year_to=None):
    """Advanced search using query syntax."""
    q = query
    if year_from or year_to:
        # CORE supports embedding year ranges in the query string
        q = f"{query} yearPublished>={year_from or 1900} yearPublished<={year_to or 2100}"

    url = "https://api.core.ac.uk/v3/search/works"
    payload = {"q": q, "limit": limit, "offset": 0}
    r = requests.post(url, json=payload, timeout=10)
    r.raise_for_status()
    return r.json()
```

---

## Response Format

### Search Response

```json
{
  "totalHits": 12345,
  "results": [
    {
      "id": 12345678,
      "title": "Attention Is All You Need",
      "authors": [
        {"name": "Ashish Vaswani"},
        {"name": "Noam Shazeer"}
      ],
      "yearPublished": 2017,
      "doi": "10.5555/3295222.3295349",
      "downloadUrl": "https://core.ac.uk/download/pdf/...",
      "abstract": "The dominant sequence transduction models...",
      "publisher": "Curran Associates",
      "citationCount": 50000,
      "sourceFulltextUrls": ["https://arxiv.org/pdf/1706.03762"]
    }
  ]
}
```

### Key Response Fields

| Field | Description |
|---|---|
| `totalHits` | Total number of matching papers |
| `results[].id` | CORE paper ID |
| `results[].title` | Paper title |
| `results[].authors` | Author list (`[{name: "..."}]`) |
| `results[].yearPublished` | Publication year |
| `results[].doi` | DOI |
| `results[].downloadUrl` | Direct PDF download URL |
| `results[].abstract` | Abstract |
| `results[].publisher` | Publisher |
| `results[].citationCount` | Citation count |
| `results[].sourceFulltextUrls` | Other full-text URLs (e.g. arXiv) |

---

## Rate Limits

| Scenario | Limit |
|---|---|
| No API key | ~100 req/s |
| With API key | Higher quota |

CORE's rate limits are relatively generous; special handling is typically unnecessary.

---

## Error Handling

```python
import requests, time

def core_search_safe(query, limit=10, retries=3, delay=2):
    """CORE search with retry logic."""
    url = "https://api.core.ac.uk/v3/search/works"
    payload = {"q": query, "limit": limit, "offset": 0}
    for attempt in range(retries):
        try:
            r = requests.post(url, json=payload, timeout=15)
            if r.status_code == 200:
                return r.json()
            elif r.status_code == 429:
                time.sleep(delay * (attempt + 1))
            else:
                raise Exception(f"CORE error {r.status_code}: {r.text[:200]}")
        except requests.exceptions.Timeout:
            time.sleep(delay)
    raise Exception(f"Max retries exceeded for CORE search: {query}")
```

---

## Related APIs

- → See [api-openalex.md](api-openalex.md) — OpenAlex paper/concept/institution queries (richer metadata)
- → See [api-semantic-scholar.md](api-semantic-scholar.md) — Semantic Scholar paper/author queries (stronger citation networks)
- → See [api-crossref.md](api-crossref.md) — CrossRef DOI metadata queries
