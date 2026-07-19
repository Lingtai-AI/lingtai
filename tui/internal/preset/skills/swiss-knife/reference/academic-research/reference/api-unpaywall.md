# Unpaywall API Reference

## API Overview

Unpaywall is an Open Access (OA) article lookup service. It queries by DOI and returns a free PDF link for the paper if one is available. Unpaywall aggregates OA information from multiple sources including publishers, preprint servers, and institutional repositories.

| Property | Description |
|----------|-------------|
| Base URL | `https://api.unpaywall.org/v2/` |
| Authentication | `email` parameter (**required**) — used to identify your application; not a placeholder |
| Rate Limit | No official hard limit; recommended maximum of 10 requests/second |
| Response Format | JSON |
| Data Sources | Crossref, DOAJ, PubMed Central, arXiv, etc. |

> **About the `email` parameter**: This is the only "authentication" required by the Unpaywall API. Please use a real institutional or personal email address — Unpaywall uses it to identify your application and contact you if issues arise. Do not use fake addresses such as `test@example.com`, as this may result in requests being rejected.

## Endpoints and Parameters

### Query a Single Paper

```
GET https://api.unpaywall.org/v2/{DOI}?email={your_email}
```

| Parameter | Position | Description | Example |
|-----------|----------|-------------|---------|
| `DOI` | Path | Paper DOI | `10.1038/nature12373` |
| `email` | Query | Your email address (identifies your application) | `my@university.edu` |

### Batch Queries

Unpaywall does not offer an official batch endpoint. It is recommended to loop through individual calls while controlling the request rate:

```python
import time

for doi in doi_list:
    result = find_free_pdf(doi, email="my@university.edu")
    time.sleep(0.1)  # No more than ~10 requests per second
```

## Code Example

One GET returns everything. `best_oa_location` is Unpaywall's own pick; to
choose yourself, rank `oa_locations` by version priority (`publishedVersion` >
`acceptedVersion` > `submittedVersion`). Use a `mailto` User-Agent when
downloading the PDF.

```python
import requests

def find_free_pdf(doi, email="my@university.edu"):
    """Unpaywall OA lookup. Returns the raw record; is_oa=False means no free copy."""
    r = requests.get(f"https://api.unpaywall.org/v2/{doi}", params={"email": email}, timeout=10)
    r.raise_for_status()
    return r.json()

def best_pdf_url(data):
    """Best PDF: prefer best_oa_location, else highest-version oa_locations entry."""
    if not data.get("is_oa"):
        return None
    best = data.get("best_oa_location") or {}
    if best.get("url_for_pdf"):
        return best["url_for_pdf"]
    prio = {"publishedVersion": 3, "acceptedVersion": 2, "submittedVersion": 1}
    locs = [l for l in data.get("oa_locations", []) if l.get("url_for_pdf")]
    return max(locs, key=lambda l: prio.get(l.get("version"), 0))["url_for_pdf"] if locs else None

data = find_free_pdf("10.1038/nature12373", email="researcher@university.edu")
url = best_pdf_url(data)
if url:
    pdf = requests.get(url, timeout=30, headers={"User-Agent": "Academic Research Tool (mailto:researcher@university.edu)"})
    open("/tmp/paper.pdf", "wb").write(pdf.content)
```

Or ad hoc: `curl -s "https://api.unpaywall.org/v2/10.1038/nature12373?email=my@university.edu" | python3 -m json.tool`

## Response Formats

### Full Response Structure

```json
{
  "doi": "10.1038/nature12373",
  "title": "The geodesic response of the Gulf Stream...",
  "year": 2013,
  "is_oa": true,
  "best_oa_location": {
    "url_for_pdf": "https://www.nature.com/articles/nature12373.pdf",
    "url_for_landing_page": "https://www.nature.com/articles/nature12373",
    "evidence": "oa repository (via pmcid lookup)",
    "license": null,
    "version": "publishedVersion",
    "host_type": "publisher",
    "updated": "2024-01-15T00:00:00"
  },
  "oa_locations": [
    {
      "url_for_pdf": "https://www.nature.com/articles/nature12373.pdf",
      "url_for_landing_page": "https://www.nature.com/articles/nature12373",
      "evidence": "oa repository (via pmcid lookup)",
      "license": null,
      "version": "publishedVersion",
      "host_type": "publisher"
    }
  ],
  "journal_name": "Nature",
  "publisher": "Springer Nature"
}
```

### Version Types

| Version | Description | Typical Source |
|---------|-------------|----------------|
| `publishedVersion` | Publisher's final version (best) | Publisher website, PMC |
| `acceptedVersion` | Post peer-review, pre-typesetting | Institutional repository, arXiv |
| `submittedVersion` | Submitted manuscript | Preprint servers |

### Key Fields

| Field | Type | Description |
|-------|------|-------------|
| `is_oa` | bool | Whether any OA version exists |
| `best_oa_location` | object\|null | Best OA source as determined by Unpaywall |
| `oa_locations` | array | All known OA sources |
| `url_for_pdf` | string\|null | Direct PDF URL |
| `url_for_landing_page` | string\|null | OA landing page URL |
| `host_type` | string | `publisher` (publisher) or `repository` (repository) |
| `evidence` | string | Basis for determining OA status |

## Rate Limits

| Scenario | Recommendation |
|----------|----------------|
| Single query | No delay needed |
| Batch queries | `time.sleep(0.1)` (~10 per second) |
| Large scale (>1000 papers) | `time.sleep(0.5)`, process in batches |
| Result caching | Unpaywall has built-in caching; repeated queries are fast |

## Error Handling

| Scenario | Handling |
|----------|----------|
| HTTP 404 | DOI does not exist or is not indexed by Unpaywall; skip |
| `is_oa: false` | No free version available for this paper; try institutional subscriptions or interlibrary loan |
| `best_oa_location: null` but `is_oa: true` | OA version exists but no direct PDF link; check `url_for_landing_page` in `oa_locations` |
| PDF link returns 403/404 | OA link may be expired; try other sources in `oa_locations` |
| HTTP 429 | Reduce request frequency; increase `time.sleep` |
| Invalid DOI format | Ensure the DOI includes the `10.` prefix; strip any `https://doi.org/` from the URL |

## Related APIs

- **PubMed**: Retrieve DOI and pass to Unpaywall to find PDFs → See [api-pubmed.md](api-pubmed.md)
- **Google Scholar**: Search results may include direct PDF links (especially for arXiv papers) → See [api-google-scholar.md](api-google-scholar.md)
- **Cross-workflow**: PubMed retrieves `elocationid` (DOI) → Unpaywall finds PDF → Download
