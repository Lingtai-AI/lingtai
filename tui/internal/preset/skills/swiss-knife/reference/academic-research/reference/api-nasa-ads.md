# NASA ADS API Reference

NASA ADS (Astrophysics Data System) is the primary search engine for astrophysics, astronomy, and space physics literature. Heavily used by astronomers and physicists.

> Last verified: 2026-04-28 | API version: v1

## API Overview

| Item | Description |
|------|-------------|
| Base URL | `https://api.adsabs.harvard.edu/v1` |
| Authentication | **Required** — free API key |
| Rate limit | Reasonable for free tier; be respectful |
| Response format | JSON |
| Best for | Astrophysics/astronomy paper search, BibTeX export, citation networks |

### Getting an API Key

1. Register at https://ui.adsabs.harvard.edu/user/settings/token
2. Free for all users
3. Include as header: `Authorization: Bearer {YOUR_TOKEN}`

## Endpoints & Parameters

### Search Papers

**Endpoint**: `GET https://api.adsabs.harvard.edu/v1/search/query`

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `q` | string | Search query (supports field prefixes, boolean) | `title:"dark matter"` |
| `fl` | string | Comma-separated fields to return | `title,author,year,bibcode,citation_count` |
| `rows` | int | Results per page (max 2000) | `25` |
| `start` | int | Pagination offset | `0` |
| `sort` | string | Sort field and direction | `citation_count desc` |

**Query field prefixes**: `title:`, `author:`, `abstract:`, `keyword:`, `bibcode:`, `year:`, `doi:`

### Export BibTeX

**Endpoint**: `POST https://api.adsabs.harvard.edu/v1/export/bibtex`

Body: `{"bibcode": ["2023ApJ...942...71V"]}`

Returns BibTeX-formatted citations.

### Citation Networks

**Endpoint**: `GET https://api.adsabs.harvard.edu/v1/search/query?q=citations(bibcode:{BIBCODE})` — papers that cite this work
**Endpoint**: `GET https://api.adsabs.harvard.edu/v1/search/query?q=references(bibcode:{BIBCODE})` — papers cited by this work

### Metrics

**Endpoint**: `GET https://api.adsabs.harvard.edu/v1/metrics?bibcodes={BIBCODE1},{BIBCODE2}`

Returns citation statistics, usage stats, and indicators (h-index, i10-index, etc.).

---

## Code Examples

### Basic Search

```python
import requests

ADS_TOKEN = "YOUR_TOKEN"  # Register at ui.adsabs.harvard.edu

def search_ads(query, rows=10):
    """Search NASA ADS for astrophysics papers."""
    url = "https://api.adsabs.harvard.edu/v1/search/query"
    headers = {"Authorization": f"Bearer {ADS_TOKEN}"}
    params = {
        "q": query,
        "fl": "title,author,year,bibcode,citation_count,doi",
        "rows": rows,
        "sort": "citation_count desc"
    }
    r = requests.get(url, headers=headers, params=params)
    r.raise_for_status()
    return r.json()["response"]["docs"]
```

### Get BibTeX

```python
def get_bibtex(bibcodes):
    """Export BibTeX for a list of ADS bibcodes."""
    url = "https://api.adsabs.harvard.edu/v1/export/bibtex"
    headers = {
        "Authorization": f"Bearer {ADS_TOKEN}",
        "Content-Type": "application/json"
    }
    body = {"bibcode": bibcodes}
    r = requests.post(url, headers=headers, json=body)
    r.raise_for_status()
    return r.json()["export"]
```

---

## Bibcode Format

ADS uses **bibcodes** (19-character identifiers): `YYYYJJJJJVVVVVPPPpage`

Example: `2023ApJ...942...71V` = 2023, Astrophysical Journal, volume 942, page 71, first author V.

When you have a DOI but need a bibcode: search `doi:"10.xxxx/..."` to resolve.

---

## Rate Limits & Error Handling

| Status | Meaning | Action |
|--------|---------|--------|
| 200 | Success | Parse response |
| 401 | Missing/invalid token | Check API key |
| 429 | Rate limited | Wait 10s, retry |
| 500 | Server error | Wait 30s, retry once |

---

## See Also

- [api-arxiv.md](api-arxiv.md) — arXiv preprint search (overlaps with ADS coverage)
- [api-crossref.md](api-crossref.md) — DOI metadata resolution
- [api-inspire-hep.md](api-inspire-hep.md) — high-energy physics (complementary)
- [decision-tree.md](decision-tree.md) — routing: bibcode → NASA ADS
