# INSPIRE-HEP API Reference

INSPIRE-HEP is the primary information system for high-energy physics (HEP) literature. It covers particle physics, nuclear physics, cosmology, and gravitational physics.

> Last verified: 2026-04-28 | API version: REST + GraphQL

## API Overview

| Item | Description |
|------|-------------|
| REST Base URL | `https://inspirehep.net/api` |
| GraphQL URL | `https://inspirehep.net/api/graphql` |
| Authentication | None required |
| Rate limit | Be respectful; no published limits |
| Response format | JSON |
| Best for | High-energy physics literature, author profiles, institutional data |

## Endpoints & Parameters

### Search Literature

**Endpoint**: `GET https://inspirehep.net/api/literature`

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `q` | string | Search query (supports field prefixes) | `find a ellis, j and t higgs` |
| `size` | int | Results per page (max 1000) | `25` |
| `page` | int | Page number | `1` |
| `sort` | string | Sort field | `mostrecent` |

**Query syntax**: `find a {author}`, `t {title}`, `e {experiment}`, `fin c {collaboration}`

### Get Literature Record

**Endpoint**: `GET https://inspirehep.net/api/literature/{control_number}`

Returns full metadata including DOI, arXiv ID, citations, references.

### Author Search

**Endpoint**: `GET https://inspirehep.net/api/authors?q={name}`

Returns author profiles with affiliations, publication counts, and INSPIRE IDs.

### Institution Search

**Endpoint**: `GET https://inspirehep.net/api/institutions?q={name}`

### Conferences

**Endpoint**: `GET https://inspirehep.net/api/conferences?q={query}`

### BibTeX Export

**Endpoint**: `GET https://inspirehep.net/api/literature/{id}?format=bibtex`

---

## Code Examples

### Search Papers

```python
import requests

def search_inspire(query, size=10):
    """Search INSPIRE-HEP for high-energy physics papers."""
    url = "https://inspirehep.net/api/literature"
    params = {"q": query, "size": size, "sort": "mostrecent"}
    r = requests.get(url, params=params)
    r.raise_for_status()
    return r.json()["hits"]["hits"]
```

### Get BibTeX

```python
def get_bibtex_inspire(literature_id):
    """Get BibTeX for an INSPIRE-HEP literature record."""
    url = f"https://inspirehep.net/api/literature/{literature_id}"
    params = {"format": "bibtex"}
    r = requests.get(url, params=params)
    r.raise_for_status()
    return r.text
```

### Author Lookup

```python
def find_author(name):
    """Find an author on INSPIRE-HEP."""
    url = "https://inspirehep.net/api/authors"
    params = {"q": name, "size": 5}
    r = requests.get(url, params=params)
    r.raise_for_status()
    return r.json()["hits"]["hits"]
```

---

## Response Format

Literature search returns:
```json
{
  "hits": {
    "total": 42,
    "hits": [
      {
        "id": "1234567",
        "created": "2023-01-15T00:00:00",
        "metadata": {
          "titles": [{"title": "..."}],
          "authors": [{"full_name": "Ellis, John"}],
          "arxiv_eprints": [{"value": "2301.00001"}],
          "dois": [{"value": "10.xxxx/..."}],
          "citation_count": 42
        }
      }
    ]
  }
}
```

---

## Rate Limits & Error Handling

| Status | Meaning | Action |
|--------|---------|--------|
| 200 | Success | Parse response |
| 400 | Bad query | Check query syntax |
| 429 | Rate limited | Wait 10s, retry |
| 500 | Server error | Wait 30s, retry once |

No API key required.

---

## When to Use INSPIRE-HEP vs. arXiv vs. NASA ADS

| Scenario | Use |
|----------|-----|
| High-energy physics (HEP) papers | INSPIRE-HEP |
| Astrophysics papers | NASA ADS |
| CS/math/physics preprints | arXiv |
| Cross-disciplinary search | OpenAlex or Semantic Scholar |
| Need BibTeX from HEP paper | INSPIRE-HEP (built-in) |

---

## See Also

- [api-arxiv.md](api-arxiv.md) — arXiv preprint search
- [api-nasa-ads.md](api-nasa-ads.md) — astrophysics (complementary discipline)
- [api-crossref.md](api-crossref.md) — DOI metadata resolution
- [decision-tree.md](decision-tree.md) — routing: bibcode → NASA ADS or INSPIRE-HEP
