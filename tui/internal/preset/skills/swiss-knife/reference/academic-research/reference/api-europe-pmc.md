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

## Code Example

Keyword and PMID lookup are the same `search` call — a PMID query is just
`query="EXT_ID:{pmid}"`. Results are under `resultList.result`; each carries
`title`, `authorString`, `journalInfo.journal.title`, `doi`, `pmcid`, and
`isOpenAccess` (`"Y"`/`"N"`). Fetch full text (OA only) from
`/{pmcid}/fullTextXML`.

```python
import requests

def search_europepmc(query, page_size=25):
    """Search Europe PMC. For a PMID use query='EXT_ID:{pmid}'."""
    r = requests.get("https://www.ebi.ac.uk/europepmc/webservices/rest/search",
                     params={"query": query, "resultType": "core", "format": "json",
                             "pageSize": page_size, "sort": "CITED desc"})
    r.raise_for_status()
    return r.json()

data = search_europepmc("EXT_ID:23903748")
if data["hitCount"]:
    r0 = data["resultList"]["result"][0]
    print(r0["title"], r0.get("pmcid"), r0.get("isOpenAccess") == "Y")
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
        "journalInfo": {
          "yearOfPublication": 2013,
          "volume": "500",
          "issue": "7460",
          "journal": {
            "title": "Nature",
            "medlineAbbreviation": "Nature"
          }
        },
        "pubYear": "2013",
        "isOpenAccess": "N",
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
