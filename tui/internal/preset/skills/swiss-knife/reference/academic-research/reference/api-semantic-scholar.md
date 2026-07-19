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

## Code Example

All Graph API endpoints share one GET+params shape, so one helper covers
search, citations/references, and author lookup — swap the path suffix and
`fields`:

```python
import requests

BASE = "https://api.semanticscholar.org/graph/v1"

def s2_get(path, params, fields=None):
    """GET any Semantic Scholar Graph API endpoint. path e.g. 'paper/search',
    'paper/{id}/citations', 'paper/{id}/references', 'author/search', 'author/{id}'."""
    if fields:
        params = {**params, "fields": ",".join(fields)}
    r = requests.get(f"{BASE}/{path}", params=params, timeout=15)
    r.raise_for_status()
    return r.json()

# Paper search
results = s2_get("paper/search", {"query": "deep learning", "limit": 5},
                  fields=["title", "authors", "year", "abstract"])
for p in results.get("data", []):
    print(f"{p['title']} ({p.get('year')}) — {[a['name'] for a in p.get('authors', [])[:3]]}")

# Citations / references — same shape, paper data nested under citingPaper/citedPaper
citing = s2_get(f"paper/{results['data'][0]['paperId']}/citations", {"limit": 5})

# Author search then profile (papers array on the profile)
authors = s2_get("author/search", {"query": "yoshua bengio"}, fields=["name", "hIndex", "citationCount"])["data"]
if authors:
    profile = s2_get(f"author/{authors[0]['authorId']}", {}, fields=["name", "hIndex", "citationCount", "papers"])
```

`paperId` can also be `DOI:10.1234/...`, `ArXiv:2106.15928`, PMID, ACL, or a
URL. The Python SDK (`pip install semanticscholar`) wraps the same endpoints:
`SemanticScholar().search_paper(query, year=..., fields=..., limit=..., sort='citationCount:desc')`.

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

HTTP 429 = rate limited. Use the generic retry-with-backoff helper in
[error-handling.md](error-handling.md) with `base_delay=12` (no key) — Semantic
Scholar's free tier needs ~12s between requests, so start there rather than the
default 2.

---

## Related APIs

- → See [api-openalex.md](api-openalex.md) — OpenAlex paper/concept/institution queries (more generous limits without a key)
- → See [api-core.md](api-core.md) — CORE open-access paper full-text download
- → See [api-crossref.md](api-crossref.md) — CrossRef DOI metadata queries
