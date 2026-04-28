# PubMed API Reference

## API Overview

PubMed provides the NCBI E-utilities API for searching biomedical literature. It is completely free and requires no API key, making it ideal for literature retrieval in biomedical, life science, and medical research fields. It returns PubMed IDs (PMIDs) as unique article identifiers.

| Property | Description |
|----------|-------------|
| Base URL | `https://eutils.ncbi.nlm.nih.gov/entrez/eutils/` |
| Authentication | No API key required (optional `tool`/`email` parameters for tracking) |
| Rate Limit | ~3 requests/second (without API key); up to 10 req/s with a key |
| Response Format | JSON or XML (controlled by `retmode` parameter) |
| Article ID | PMID (PubMed ID) |

## Endpoints and Parameters

### esearch — Search Articles

| Parameter | Description | Example |
|-----------|-------------|---------|
| `db` | Database | `pubmed`, `pmc`, `books` |
| `term` | Search query | `transformer architecture` |
| `retmax` | Maximum results returned (default 20) | `10` |
| `retmode` | Response format | `json` or `xml` |
| `sort` | Sort field | `relevance`, `pub_date` |
| `field` | Restrict search to specific field | `tiab` (title + abstract) |
| `retstart` | Pagination offset | `0`, `20` |

**Search Field Quick Reference**:

| Code | Field | Example Query |
|------|-------|---------------|
| `ti` | Title | `cancer[ti]` |
| `ab` | Abstract | `treatment[ab]` |
| `tiab` | Title + Abstract | `transformer[tiab]` |
| `au` | Author | `vaswani[au]` |
| `dp` | Date | `2020:2024[dp]` |
| `mh` | MeSH term | `neural networks[mh]` |
| `mb` | MeSH major topic | `genomics[mb]` |

**Boolean combinations**: Use `AND`, `OR`, `NOT` to connect terms, e.g. `cancer[ti] AND review[pt] AND 2023:2024[dp]`.

### esummary — Article Summary

| Parameter | Description | Example |
|-----------|-------------|---------|
| `db` | Database | `pubmed` |
| `id` | PMID (comma-separated for batch) | `42018049` or `42018049,42014737` |
| `retmode` | Response format | `json` |

### efetch — Full Record

| Parameter | Description | Example |
|-----------|-------------|---------|
| `db` | Database | `pubmed` |
| `id` | PMID | `42018049` |
| `rettype` | Return type | `abstract`, `full`, `medline`, `uilist` |
| `retmode` | Response format | `text` (when `rettype=abstract`), `xml` (when `rettype=full`) |

## Code Examples

### Search and Retrieve Article Details

```python
import requests
import time

BASE = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"

def search_pubmed(query, retmax=5, field=None, sort="relevance"):
    """Search PubMed and return a list of PMIDs."""
    params = {
        "db": "pubmed",
        "term": query,
        "retmax": retmax,
        "retmode": "json",
        "sort": sort,
    }
    if field:
        params["field"] = field
    r = requests.get(f"{BASE}/esearch.fcgi", params=params, timeout=10)
    r.raise_for_status()
    return r.json()["esearchresult"]["idlist"]

def get_summaries(pmids):
    """Retrieve summary metadata for a batch of articles."""
    params = {
        "db": "pubmed",
        "id": ",".join(pmids),
        "retmode": "json",
    }
    r = requests.get(f"{BASE}/esummary.fcgi", params=params, timeout=10)
    r.raise_for_status()
    data = r.json()["result"]
    return {pmid: data[pmid] for pmid in data.get("uids", [])}

def fetch_abstract(pmid):
    """Fetch the full abstract for a single article."""
    params = {
        "db": "pubmed",
        "id": pmid,
        "rettype": "abstract",
        "retmode": "text",
    }
    r = requests.get(f"{BASE}/efetch.fcgi", params=params, timeout=10)
    r.raise_for_status()
    return r.text

# Usage example
ids = search_pubmed("transformer architecture in genomics", retmax=3)
summaries = get_summaries(ids)
for pmid, art in summaries.items():
    print(f"PMID:  {pmid}")
    print(f"Title:  {art.get('title', 'N/A')}")
    print(f"Journal:  {art.get('source', 'N/A')}")
    print(f"Authors:  {[a['name'] for a in art.get('authors', [])[:3]]}")
    print(f"Date:  {art.get('pubdate', 'N/A')}")
    print("---")
    time.sleep(0.34)  # Rate limit: ~3 requests/second
```

### Search by Author

```python
ids = search_pubmed("vaswani[au]", retmax=5)
```

### Search by MeSH Term + Date Range

```python
ids = search_pubmed(
    "neural networks[mh] AND genomics[mb] AND 2020:2024[dp]",
    retmax=10
)
```

### Direct curl Examples

```bash
# Search
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/e_utils/esearch.fcgi?db=pubmed&term=transformer+architecture&retmax=3&retmode=json"

# Get summary
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/e_utils/esummary.fcgi?db=pubmed&id=42018049&retmode=json"

# Get full abstract
curl -s "https://eutils.ncbi.nlm.nih.gov/entrez/e_utils/efetch.fcgi?db=pubmed&id=42018049&rettype=abstract&retmode=text"
```

## Response Formats

### esearch Response

```json
{
  "esearchresult": {
    "count": "5095",
    "retmax": "3",
    "retstart": "0",
    "idlist": ["42018049", "42014737", "42014555"],
    "querytranslation": "transformer[All Fields] AND architecture[All Fields]"
  }
}
```

### esummary Response

```json
{
  "result": {
    "uids": ["42018049"],
    "42018049": {
      "uid": "42018049",
      "title": "Deep learning approaches for...",
      "source": "Nat Methods",
      "authors": [{"name": "Smith J"}, {"name": "Lee K"}],
      "pubdate": "2025 Jan",
      "fulljournalname": "Nature methods",
      "elocationid": "doi:10.1038/s41592-025-xxxxx"
    }
  }
}
```

### efetch Response (rettype=abstract, retmode=text)

```
1. Author A, Author B.
Title of the article.
Journal Name. 2025 Jan;30(1):1-10.

Abstract text here...
```

## Rate Limits

| Scenario | Limit |
|----------|-------|
| No API key | ~3 requests/second |
| With API key (`api_key` parameter) | 10 requests/second |
| Batch requests | Up to 200 PMIDs per request (esummary/efetch) |

**Recommendations**:
- Add `time.sleep(0.34)` in loops to control the rate when no key is available
- Use `retstart` for pagination when retrieving large result sets, with `retmax=100` per page

## Error Handling

| Scenario | Handling |
|----------|----------|
| HTTP 429 (Too Many Requests) | Wait and retry; increase `time.sleep` interval |
| Empty result `idlist: []` | Check query syntax; broaden search terms or try MeSH terms |
| PMID has no abstract | Some older articles or letters may lack abstracts; check the `title` from `esummary` to determine relevance |
| Network timeout | Set `timeout=10`; retry up to 3 times |
| XML parsing error | Use `retmode=json` to avoid parsing XML |

## Related APIs

- **Unpaywall**: Find open-access PDFs via DOI → See [api-unpaywall.md](api-unpaywall.md)
- **Google Scholar**: Cross-disciplinary literature search and citation data → See [api-google-scholar.md](api-google-scholar.md)
- **Note**: The `elocationid` field returned by PubMed typically contains a DOI, which can be passed directly to Unpaywall to find free PDFs
