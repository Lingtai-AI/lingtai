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

## Code Examples

### Find Open Access PDFs

```python
import requests

def find_free_pdf(doi, email="my@university.edu"):
    """Find a free PDF for a paper via Unpaywall.

    Args:
        doi: DOI string, e.g. '10.1038/nature12373'
        email: Your real email address, used to identify your application (not a placeholder)

    Returns:
        dict: {
            'doi': str,
            'is_oa': bool,
            'pdf_url': str|None,
            'landing_url': str|None,
            'version': str|None,
            'license': str|None,
            'oa_locations': list
        }
    """
    url = f"https://api.unpaywall.org/v2/{doi}"
    params = {"email": email}
    r = requests.get(url, params=params, timeout=10)
    r.raise_for_status()
    data = r.json()

    result = {
        "doi": data.get("doi"),
        "is_oa": data.get("is_oa", False),
        "pdf_url": None,
        "landing_url": None,
        "version": None,
        "license": None,
        "oa_locations": data.get("oa_locations", []),
    }

    best = data.get("best_oa_location")
    if best:
        result["pdf_url"] = best.get("url_for_pdf")
        result["landing_url"] = best.get("url_for_landing_page")
        result["version"] = best.get("version")
        result["license"] = best.get("license")

    return result

# Usage example
result = find_free_pdf("10.1038/nature12373", email="researcher@university.edu")
if result["is_oa"]:
    print(f"Free PDF: {result['pdf_url']}")
    print(f"Version: {result['version']}")
    print(f"License: {result['license']}")
else:
    print("No free version available")
```

### Download PDF

```python
def download_free_pdf(doi, output_path, email="my@university.edu"):
    """Find and download a free PDF via Unpaywall."""
    result = find_free_pdf(doi, email)
    if not result["pdf_url"]:
        print(f"No free PDF available: {doi}")
        return None

    headers = {"User-Agent": f"Academic Research Tool (mailto:{email})"}
    r = requests.get(result["pdf_url"], timeout=30, headers=headers)
    if r.status_code == 200:
        with open(output_path, "wb") as f:
            f.write(r.content)
        print(f"Downloaded: {output_path}")
        return output_path
    else:
        print(f"Download failed (HTTP {r.status_code}): {result['pdf_url']}")
        return None

# Usage example
download_free_pdf("10.1038/nature12373", "/tmp/paper.pdf", "researcher@university.edu")
```

### Select the Best OA Source from Multiple Locations

```python
def find_best_oa(doi, email="my@university.edu"):
    """Select the best PDF from all available OA sources.

    Priority: publishedVersion > acceptedVersion > submittedVersion
    """
    url = f"https://api.unpaywall.org/v2/{doi}"
    r = requests.get(url, params={"email": email}, timeout=10)
    r.raise_for_status()
    data = r.json()

    if not data.get("is_oa"):
        return None

    locations = data.get("oa_locations", [])
    priority = {"publishedVersion": 3, "acceptedVersion": 2, "submittedVersion": 1}

    best = None
    best_score = 0
    for loc in locations:
        pdf = loc.get("url_for_pdf")
        if not pdf:
            continue
        score = priority.get(loc.get("version"), 0)
        if score > best_score:
            best_score = score
            best = loc

    return best

# Usage example
best = find_best_oa("10.1038/nature12373", "researcher@university.edu")
if best:
    print(f"Best version: {best['version']}")
    print(f"PDF URL: {best['url_for_pdf']}")
```

### Direct curl Example

```bash
curl -s "https://api.unpaywall.org/v2/10.1038/nature12373?email=my@university.edu" | python3 -m json.tool
```

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
