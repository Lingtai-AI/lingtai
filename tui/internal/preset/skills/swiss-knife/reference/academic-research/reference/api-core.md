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

## Code Example

Search is a **POST** with a JSON body; detail/download are GETs. Year filtering
goes inside the query string (`yearPublished>=2020 yearPublished<=2024`), not a
separate parameter.

```python
import requests

BASE = "https://api.core.ac.uk/v3"

def search_core(query, limit=10, offset=0):
    """Search CORE (POST). Returns dict with totalHits + results list."""
    r = requests.post(f"{BASE}/search/works",
                      json={"q": query, "limit": limit, "offset": offset}, timeout=10)
    r.raise_for_status()
    return r.json()

def download_core_pdf(work_id, output_path):
    """GET /works/{id}, then download its downloadUrl PDF. None if no PDF."""
    data = requests.get(f"{BASE}/works/{work_id}", timeout=10).json()
    if data.get("downloadUrl"):
        with open(output_path, "wb") as f:
            f.write(requests.get(data["downloadUrl"], timeout=30).content)
        return output_path
    return None

results = search_core("transformer architecture yearPublished>=2020", limit=5)
for p in results["results"]:
    print(f"{p['title']} ({p.get('yearPublished','N/A')}) — {p.get('downloadUrl','no PDF')}")
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

CORE's limits are generous, so special handling is rarely needed. For 429/5xx,
reuse the retry-with-backoff pattern in [error-handling.md](error-handling.md) —
note CORE search is a **POST**, so swap `requests.get` for `requests.post(...,
json=payload)` in that helper.

---

## Related APIs

- → See [api-openalex.md](api-openalex.md) — OpenAlex paper/concept/institution queries (richer metadata)
- → See [api-semantic-scholar.md](api-semantic-scholar.md) — Semantic Scholar paper/author queries (stronger citation networks)
- → See [api-crossref.md](api-crossref.md) — CrossRef DOI metadata queries
