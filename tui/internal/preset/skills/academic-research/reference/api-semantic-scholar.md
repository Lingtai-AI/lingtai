# Semantic Scholar API Reference

The Semantic Scholar Graph API provides academic paper search, citation network analysis, author profiles, and more. The free tier is available without an API key; adding an API key significantly increases your quota.

## API Overview

| Property | Description |
|---|---|
| Base URL | `https://api.semanticscholar.org/graph/v1` |
| Authentication | Usable without a key (100 req/day/IP); with API Key → 1000 req/day (free) |
| Rate Limit | Without key: ~5 successful requests/minute/IP; with key: significantly higher |
| Response Format | JSON |
| Python SDK | `pip install semanticscholar` |
| Best Use Cases | Citation network analysis, author profiling, paper metadata retrieval |

Core endpoints:

| Endpoint | Purpose |
|---|---|
| `GET /paper/search` | Paper search |
| `GET /paper/{paperId}` | Paper details |
| `GET /paper/{paperId}/citations` | Get papers citing this paper |
| `GET /paper/{paperId}/references` | Get papers referenced by this paper |
| `GET /author/search` | Search authors |
| `GET /author/{authorId}` | Author details + paper list |

---

## Endpoints and Parameters

### Paper Queries

#### Search Papers

**Endpoint**: `GET /paper/search`

| Parameter | Description | Example |
|---|---|---|
| `query` | Search query string | `query=attention is all you need` |
| `limit` | Maximum number of results (default 100) | `limit=10` |
| `offset` | Pagination offset | `offset=10` |
| `fields` | Returned fields (comma-separated) | `fields=title,authors,year` |
| `year` | Year filter | `year=2020` or `year=2018-2022` |
| `publicationTypes` | Publication type | `publicationTypes=JournalArticle` |
| `openAccessPdf` | Only return papers with OA PDFs | `openAccessPdf=true` |
| `venue` | Publication venue | `venue=NeurIPS` |
| `fieldsOfStudy` | Research field | `fieldsOfStudy=Computer Science` |
| `minCitationCount` | Minimum citation count | `minCitationCount=100` |
| `sort` | Sort order | `sort=citationCount:desc` |

**Common `fields` values**: `title`, `authors`, `year`, `abstract`, `venue`, `citationCount`, `referenceCount`, `url`, `paperId`, `externalIds`, `openAccessPdf`, `fieldsOfStudy`, `publicationTypes`, `journal`

Nested fields: `authors.authorId`, `authors.name`, `authors.url`

#### Paper Details

**Endpoint**: `GET /paper/{paperId}`

`paperId` can be:
- Semantic Scholar ID (40-character hash)
- DOI: `DOI:10.1234/...`
- ArXiv: `ArXiv:2106.15928`
- PMID, ACL, URL, etc.

#### Citations and References

| Endpoint | Description |
|---|---|
| `GET /paper/{paperId}/citations` | Get papers that have cited this paper |
| `GET /paper/{paperId}/references` | Get papers referenced by this paper |

Both support `limit`, `offset`, and `fields` parameters. In the response, paper data is nested under the `citingPaper` or `citedPaper` key.

### Author Queries

#### Search Authors

**Endpoint**: `GET /author/search`

| Parameter | Description | Example |
|---|---|---|
| `query` | Author name | `query=yoshua bengio` |
| `limit` | Maximum number of results | `limit=5` |
| `fields` | Returned fields | `fields=name,hIndex,citationCount` |

#### Author Details and Papers

**Endpoint**: `GET /author/{authorId}`

| Parameter | Description | Example |
|---|---|---|
| `fields` | Returned fields | `fields=name,hIndex,citationCount,papers` |

The returned `papers` array contains the author's paper list (each entry includes `paperId`, `title`, etc.).

---

## Code Examples

### Paper Search (Direct HTTP)

```python
import requests, time

def search_papers(query, limit=10, fields=None):
    """Search Semantic Scholar papers."""
    url = "https://api.semanticscholar.org/graph/v1/paper/search"
    params = {"query": query, "limit": limit}
    if fields:
        params["fields"] = ",".join(fields)
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()

results = search_papers("deep learning", limit=5,
                        fields=["title", "authors", "year", "abstract"])
for p in results.get("data", []):
    print(f"Title: {p['title']}")
    print(f"Year: {p.get('year')}, Authors: {[a['name'] for a in p.get('authors', [])[:3]]}")
    print("---")
```

### Paper Search (Python SDK)

```python
from semanticscholar import SemanticScholar

# No key = limited quota
ss = SemanticScholar()
# With key:
# ss = SemanticScholar(api_key='your-key')

results = ss.search_paper(
    'attention is all you need',
    limit=5,
    fields=['title', 'authors', 'year', 'abstract']
)

for paper in results[:5]:
    print(f"Title: {paper.title}")
    print(f"Year: {paper.year}")
    print(f"Authors: {[a.name for a in paper.authors]}")
    print("---")
```

SDK `search_paper` full signature:

```python
search_paper(
    query: str,
    year: str = None,                # '2020' or '2018-2022'
    publication_types: list = None,
    open_access_pdf: bool = None,
    venue: list = None,
    fields_of_study: list = None,
    fields: list = None,
    publication_date_or_year: str = None,
    min_citation_count: int = None,
    limit: int = 100,
    bulk: bool = False,
    sort: str = None,                # 'citationCount:desc'
    match_title: bool = False
)
```

### Get Citations and References

```python
def get_citations(paper_id, limit=5, fields=None):
    """Get papers that have cited the specified paper."""
    url = f"https://api.semanticscholar.org/graph/v1/paper/{paper_id}/citations"
    params = {"limit": limit}
    if fields:
        params["fields"] = ",".join(fields)
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()

def get_references(paper_id, limit=5, fields=None):
    """Get references cited by the specified paper."""
    url = f"https://api.semanticscholar.org/graph/v1/paper/{paper_id}/references"
    params = {"limit": limit}
    if fields:
        params["fields"] = ",".join(fields)
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.json()
```

### Author Search and Profiles

```python
def search_authors(query, limit=5):
    """Search for authors."""
    url = "https://api.semanticscholar.org/graph/v1/author/search"
    params = {"query": query, "limit": limit, "fields": "name,hIndex,citationCount"}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()["data"]

def get_author_profile(author_id):
    """Get author details and paper list."""
    url = f"https://api.semanticscholar.org/graph/v1/author/{author_id}"
    params = {"fields": "name,hIndex,citationCount,papers"}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    return r.json()

# Example
authors = search_authors("yoshua bengio")
for a in authors:
    print(f"{a['name']} — h-index: {a.get('hIndex', 'N/A')}, citations: {a.get('citationCount', 'N/A')}")

if authors:
    profile = get_author_profile(authors[0]["authorId"])
    print(f"\nTop papers by {profile['name']}:")
    for p in profile.get('papers', [])[:5]:
        print(f"  {p.get('title', 'N/A')[:70]}")
```

---

## Response Formats

### Paper Search Response

```json
{
  "total": 8013996,
  "offset": 0,
  "next": 10,
  "data": [
    {
      "paperId": "3c8a4565...",
      "title": "PyTorch: An Imperative Style, High-Performance Deep Learning Library",
      "year": 2019,
      "authors": [
        {"authorId": "3407277", "name": "Adam Paszke"}
      ],
      "abstract": "...",
      "citationCount": 12000
    }
  ]
}
```

### Citations Response

```json
{
  "data": [
    {
      "citingPaper": {
        "paperId": "...",
        "title": "Bridging local and global representations...",
        "year": 2026,
        "authors": [...]
      }
    }
  ]
}
```

### Author Search Response

```json
{
  "total": 5,
  "offset": 0,
  "data": [
    {
      "authorId": "1751762",
      "name": "Yoshua Bengio",
      "hIndex": 187,
      "citationCount": 523456
    }
  ]
}
```

---

## Rate Limits

| Scenario | Quota | Notes |
|---|---|---|
| No API Key | ~100 req/day/IP | Approximately 5 successful requests/minute in practice |
| Free API Key | 1000 req/day | More stable rate |
| Paid Tier | Higher | Purchase as needed |

**Rate-limited response**: HTTP 429 Too Many Requests

**Best practice**: Wait at least 12 seconds between requests (without key); with a key, consecutive requests are acceptable.

---

## Error Handling

```python
import requests, time

def safe_semantic_scholar_get(url, params, retries=3, delay=12):
    """Semantic Scholar request with retry logic (no-key mode)."""
    for attempt in range(retries):
        try:
            r = requests.get(url, params=params, timeout=15)
            if r.status_code == 200:
                return r.json()
            elif r.status_code == 429:
                wait = delay * (attempt + 1)
                print(f"Rate limited (429), waiting {wait}s... (attempt {attempt+1}/{retries})")
                time.sleep(wait)
            else:
                raise Exception(f"S2 error {r.status_code}: {r.text[:200]}")
        except requests.exceptions.Timeout:
            print(f"Timeout, retrying... (attempt {attempt+1}/{retries})")
            time.sleep(delay)
    raise Exception(f"Max retries exceeded for {url}")
```

---

## Related APIs

- → See [api-openalex.md](api-openalex.md) — OpenAlex paper/concept/institution queries (more generous limits without a key)
- → See [api-core.md](api-core.md) — CORE open-access paper full-text download
- → See [api-crossref.md](api-crossref.md) — CrossRef DOI metadata queries
