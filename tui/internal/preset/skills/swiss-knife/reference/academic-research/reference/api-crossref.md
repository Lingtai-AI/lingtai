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
    """Search CrossRef papers. **filters e.g. type='journal-article', from_pub_date='2020-01-01'
    (keys use '_' here, sent as '-' in the filter param). Returns list[dict].
    """
    params = {"query": query, "rows": rows, "select": select}
    if filters:
        params["filter"] = ",".join(f"{k.replace('_', '-')}:{v}" for k, v in filters.items())
    r = requests.get(f"{BASE}/works", params=params, headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]["items"]

# Basic search
papers = search_works("transformer architecture", rows=3)
for p in papers:
    title = p.get("title", ["N/A"])[0]
    authors = ", ".join(a["family"] for a in p.get("author", []))
    print(f"DOI: {p.get('DOI', 'N/A')}  Title: {title}  Authors: {authors}")
```

The `/funders` endpoint (same auth/params shape) searches funding agencies by
name and returns each funder's DOI (e.g. NSF = `10.13039/100000001`, NIH =
`10.13039/100000002`); combine with `filter=funder:{funder_doi}` on `/works` to
pull an agency's funded papers. Date-range tracking combines
`filter=from-pub-date:...,until-pub-date:...` with `sort=published-print&order=desc`.

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

`GET https://api.crossref.org/funders?query=NSF&rows=5` searches funding
agencies (same request shape as `/works`, returns `{id, location, name,
alt-names, uri}` per item). Combine a funder DOI with `/works`:
`filter=funder:{funder_doi},sort=published-print,order=desc` to get that
agency's recent funded papers (`select` should include `funder,award` to see
grant numbers).

**Common Funder DOIs**: NIH `10.13039/100000002` · NSF `10.13039/100000001` ·
DOE `10.13039/100000015` · EU `10.13039/501100000780` · Wellcome Trust
`10.13039/100004440` · DFG `10.13039/501100001659` · JSPS
`10.13039/501100001691` · NSFC `10.13039/501100001809`

## 3. Recent Publications (Date-filtered Queries)

Combine `from-pub-date`/`until-pub-date` filters with `sort=published-print`
and `order=desc` to track the latest papers; add `funder:` and
`container-title:` filters to narrow by agency or journal:

```bash
curl -s "https://api.crossref.org/works?filter=from-pub-date:2026-04-01,until-pub-date:2026-04-22,type:journal-article,container-title:Nature&rows=5&select=DOI,title,published-print&sort=published-print&order=desc"
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

For the generic retry-with-backoff pattern (429/5xx handling), see
[error-handling.md](error-handling.md) — it applies unchanged here.

## Related APIs

- → See [api-arxiv.md](api-arxiv.md) — Searching preprints (arXiv papers are typically published here first)
- → See [api-doi-resolver.md](api-doi-resolver.md) — Resolve individual DOIs to full metadata
