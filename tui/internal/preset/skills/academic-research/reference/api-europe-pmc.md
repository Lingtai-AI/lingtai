# Europe PMC API Reference

Europe PMC is a free academic search engine that indexes PubMed plus additional content (preprints, patents, books, guidelines). It provides full-text retrieval for open-access articles.

> Last verified: 2026-04-28 | API version: REST v6

## API Overview

| Item | Description |
|------|-------------|
| Base URL | `https://www.ebi.ac.uk/europepmc/webservices/rest` |
| Authentication | None required |
| Rate limit | ~5 req/s (be reasonable) |
| Response format | JSON or XML |
| Best for | Biomedical literature search, PMID lookup, full-text retrieval |

## Endpoints & Parameters

### Search Articles

**Endpoint**: `GET https://www.ebi.ac.uk/europepmc/webservices/rest/search`

| Parameter | Type | Description | Example |
|-----------|------|-------------|---------|
| `query` | string | Search query (supports field prefixes) | `EXT_ID:28980604` |
| `resultType` | string | `core` (full metadata) or `lite` (basic) | `core` |
| `format` | string | `json` or `xml` | `json` |
| `pageSize` | int | Results per page (max 1000) | `25` |
| `cursorMark` | string | Pagination cursor | `*` |
| `sort` | string | Sort field | `CITED desc` |

**Query field prefixes**: `AUTH:`, `TITLE:`, `JOURNAL:`, `AUTH_AFFIL:`, `SUBJECT:`, `EXT_ID:` (PMID/PMCID/DOI)

### Get Full Text

**Endpoint**: `GET https://www.ebi.ac.uk/europepmc/webservices/rest/{PMCID}/fullTextXML`

Returns full article text as structured XML (only for open-access articles with a PMCID).

### Grant Information

**Endpoint**: `GET https://www.ebi.ac.uk/europepmc/webservices/rest/search?query=GRANT_ID:{grant_id}`

---

## Code Examples

### Search by PMID

```python
import requests

def search_by_pmid(pmid):
    """Look up an article by its PubMed ID.
    Example: search_by_pmid(23903748) returns Kucsko et al. 2013 Nature paper.
    """
    url = "https://www.ebi.ac.uk/europepmc/webservices/rest/search"
    params = {
        "query": f"EXT_ID:{pmid}",
        "resultType": "core",
        "format": "json"
    }
    r = requests.get(url, params=params)
    r.raise_for_status()
    data = r.json()
    if data["hitCount"] > 0:
        result = data["resultList"]["result"][0]
        return {
            "title": result.get("title"),
            "authors": result.get("authorString"),
            "journal": result.get("journalTitle"),
            "year": result.get("pubYear"),
            "doi": result.get("doi"),
            "pmcid": result.get("pmcid"),
            "isOpenAccess": result.get("isOpenAccess") == "Y"
        }
    return None
```

### Search by Keyword

```python
def search_europepmc(query, page_size=25):
    """Search Europe PMC for biomedical articles."""
    url = "https://www.ebi.ac.uk/europepmc/webservices/rest/search"
    params = {
        "query": query,
        "resultType": "core",
        "format": "json",
        "pageSize": page_size,
        "sort": "CITED desc"
    }
    r = requests.get(url, params=params)
    r.raise_for_status()
    return r.json()
```

---

## Response Format

Search returns (example: PMID 23903748):
```json
{
  "hitCount": 1,
  "cursorMark": "*",
  "resultList": {
    "result": [
      {
        "id": "23903748",
        "source": "MED",
        "pmid": "23903748",
        "doi": "10.1038/nature12373",
        "title": "Nanometre-scale thermometry in a living cell.",
        "authorString": "Kucsko G, Maurer PC, Yao NY, Kubo M, Noh HJ, Lo PK, Park H, Lukin MD.",
        "journalTitle": "Nature",
        "pubYear": "2013",
        "isOpenAccess": "Y",
        "pmcid": "PMC4221854"
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
| 429 | Rate limited | Wait 5s, retry |
| 500 | Server error | Wait 10s, retry once |

No API key required. No daily quota.

---

## Relationship to PubMed

- **Europe PMC** indexes PubMed + preprints + patents + books + guidelines
- When you have a PMID, Europe PMC is often faster than PubMed E-utilities
- Full-text XML is available for PMC open-access articles (PubMed E-utilities don't provide this)
- For purely biomedical metadata, either works; for full-text retrieval, prefer Europe PMC

---

## See Also

- [api-pubmed.md](api-pubmed.md) — alternative PMID lookup
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — PDF acquisition chain
- [decision-tree.md](decision-tree.md) — routing: PMID → Europe PMC
