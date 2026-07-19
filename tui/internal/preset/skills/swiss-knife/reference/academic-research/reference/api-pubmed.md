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

## Code Example

The E-utilities pipeline is esearch (→ PMIDs) → esummary (metadata) → efetch
(abstract). Field codes go in `term` (`vaswani[au]`, `neural networks[mh] AND
2020:2024[dp]`); sleep 0.34s/request without a key.

```python
import requests, time

BASE = "https://eutils.ncbi.nlm.nih.gov/entrez/eutils"

def search_pubmed(query, retmax=5, field=None, sort="relevance"):
    """esearch → list of PMIDs."""
    params = {"db": "pubmed", "term": query, "retmax": retmax, "retmode": "json", "sort": sort}
    if field: params["field"] = field
    return requests.get(f"{BASE}/esearch.fcgi", params=params, timeout=10).json()["esearchresult"]["idlist"]

def get_summaries(pmids):
    """esummary → {pmid: metadata} (batch, comma-joined ids)."""
    d = requests.get(f"{BASE}/esummary.fcgi",
                     params={"db": "pubmed", "id": ",".join(pmids), "retmode": "json"}, timeout=10).json()["result"]
    return {p: d[p] for p in d.get("uids", [])}

def fetch_abstract(pmid):
    """efetch → plain-text abstract (rettype=abstract, retmode=text)."""
    return requests.get(f"{BASE}/efetch.fcgi",
                        params={"db": "pubmed", "id": pmid, "rettype": "abstract", "retmode": "text"}, timeout=10).text

ids = search_pubmed("transformer architecture in genomics", retmax=3)
for pmid, art in get_summaries(ids).items():
    print(f"{pmid}: {art.get('title','N/A')} — {art.get('source','N/A')} ({art.get('pubdate','N/A')})")
    time.sleep(0.34)  # ~3 req/s without a key
```

Ad hoc: `curl -s ".../esearch.fcgi?db=pubmed&term=transformer&retmax=3&retmode=json"` (swap
`esummary.fcgi?id={pmid}` or `efetch.fcgi?id={pmid}&rettype=abstract&retmode=text`).

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
