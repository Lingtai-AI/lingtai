# DOI Resolver API Reference

## API Overview

Resolves a DOI (Digital Object Identifier) to complete paper metadata via the CrossRef Works endpoint. This is the most direct way to retrieve citation information from a DOI.

- **Endpoint**: `https://api.crossref.org/works/{DOI}`
- **Redirect Endpoint**: `https://doi.org/{DOI}` → publisher landing page
- **Authentication**: No API key required; Polite Pool rules apply
- **Response Format**: JSON
- **Typical Response Time**: < 200ms
- **Use Cases**: DOI → citation information, batch DOI resolution, retrieving paper full-text links

## Endpoint and Parameters

### Single DOI Resolution

| Item | Description |
|---|---|
| Endpoint | `GET https://api.crossref.org/works/{DOI}` |
| Path Parameter | `DOI` — DOI string, e.g. `10.1038/nature12373` |
| Request Header | Recommended `User-Agent: AppName/Version (mailto:email)` |
| Response | JSON; `message` field contains the full metadata |

### DOI URL Redirect

| Item | Description |
|---|---|
| Endpoint | `https://doi.org/{DOI}` |
| Purpose | Follow redirect to obtain the publisher page URL |
| Method | `HEAD` request + `allow_redirects=True` |

### The `select` Parameter

When resolving a single DOI, you can use the `select` parameter to limit returned fields (though typically you can just retrieve all fields directly).

## Code Examples

### Basic Resolution

```python
import requests

HEADERS = {"User-Agent": "MyApp/1.0 (mailto:your@email.com)"}

def resolve_doi(doi):
    """Resolve a DOI to complete paper metadata.

    Args:
        doi: DOI string, e.g. '10.1038/nature12373'

    Returns:
        dict: Metadata dictionary containing title, authors, journal, publisher, etc.
    """
    url = f"https://api.crossref.org/works/{doi}"
    r = requests.get(url, headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]

# Usage example
paper = resolve_doi("10.1038/nature12373")
print(f"Title: {paper.get('title', ['N/A'])[0]}")
print(f"Journal: {paper.get('container-title', ['N/A'])[0]}")
print(f"Publisher: {paper.get('publisher', 'N/A')}")
print(f"Type: {paper.get('type', 'N/A')}")

year = paper.get("published-print", paper.get("published-online", {}))
year_str = year.get("date-parts", [[None]])[0][0] if year else "N/A"
print(f"Year: {year_str}")

authors = paper.get("author", [])
author_names = [f"{a.get('given', '')} {a.get('family', '')}" for a in authors]
print(f"Authors: {', '.join(author_names[:5])}" + (" et al." if len(authors) > 5 else ""))
```

### Retrieving the Publisher URL

```python
def get_publisher_url(doi):
    """Follow DOI redirect to obtain the publisher page URL.

    Args:
        doi: DOI string

    Returns:
        str: Publisher page URL
    """
    r = requests.head(f"https://doi.org/{doi}", allow_redirects=True, timeout=10)
    return r.url

# Usage example
url = get_publisher_url("10.1038/nature12373")
print(f"Publisher URL: {url}")
```

### Batch DOI Resolution

```python
import time

def resolve_dois(doi_list, delay=0.1):
    """Batch resolve a list of DOIs.

    Args:
        doi_list: List of DOI strings
        delay: Delay between requests (seconds) to avoid rate limiting

    Returns:
        list[dict]: Each element is {'doi': ..., 'metadata': ...} or {'doi': ..., 'error': ...}
    """
    results = []
    for doi in doi_list:
        try:
            meta = resolve_doi(doi)
            results.append({"doi": doi, "metadata": meta})
        except requests.HTTPError as e:
            results.append({"doi": doi, "error": str(e)})
        time.sleep(delay)
    return results

# Usage example
dois = [
    "10.1038/nature12373",
    "10.1126/science.1248506",
    "10.1016/j.cell.2014.05.010",
]
resolved = resolve_dois(dois)
for item in resolved:
    if "metadata" in item:
        title = item["metadata"].get("title", ["N/A"])[0]
        print(f"✓ {item['doi']}: {title}")
    else:
        print(f"✗ {item['doi']}: {item['error']}")
```

### Extracting Structured Citations

```python
def format_citation(doi, style="apa"):
    """Generate a structured citation string from DOI metadata.

    Args:
        doi: DOI string
        style: Citation format ('apa', 'mla', 'brief')

    Returns:
        str: Formatted citation string
    """
    paper = resolve_doi(doi)
    title = paper.get("title", ["N/A"])[0]
    authors = paper.get("author", [])
    journal = paper.get("container-title", ["N/A"])[0]
    year_info = paper.get("published-print", paper.get("published-online", {}))
    year = year_info.get("date-parts", [[None]])[0][0] if year_info else "N/A"
    volume = paper.get("volume", "")
    issue = paper.get("issue", "")
    pages = paper.get("page", "")

    if style == "apa":
        auth_str = ", ".join(
            f"{a.get('family', '')}, {a.get('given', '')[0]}." for a in authors[:6]
        )
        if len(authors) > 6:
            auth_str += " et al."
        vi_str = f"{volume}({issue})" if issue else volume
        return f"{auth_str} ({year}). {title}. {journal}, {vi_str}, {pages}. https://doi.org/{doi}"

    elif style == "brief":
        first_author = authors[0].get("family", "Unknown") if authors else "Unknown"
        return f"{first_author} et al. ({year}). {title}. {journal}."

    return f"{title} ({year}). {journal}. DOI: {doi}"

# Usage examples
print(format_citation("10.1038/nature12373", style="apa"))
# Kucsko, G., ... et al. (2013). Nanometre-scale thermometry in a living cell. Nature, 500(7460), 54-58.

print(format_citation("10.1038/nature12373", style="brief"))
# Kucsko et al. (2013). Nanometre-scale thermometry in a living cell. Nature.
```

## Response Format

### Full Response Structure

```json
{
  "status": "ok",
  "message-type": "work",
  "message": {
    "DOI": "10.1038/nature12373",
    "title": ["Nanometre-scale thermometry in a living cell"],
    "author": [
      {"given": "G.", "family": "Kucsko", "sequence": "first", "affiliation": []}
    ],
    "published-print": {"date-parts": [[2013, 7, 31]]},
    "published-online": {"date-parts": [[2013, 6, 12]]},
    "container-title": ["Nature"],
    "publisher": "Springer Science and Business Media LLC",
    "type": "journal-article",
    "volume": "576",
    "issue": "7467",
    "page": "376-379",
    "abstract": "...",
    "reference-count": 45,
    "references-count": 45,
    "is-referenced-by-count": 95000,
    "subject": ["Computer Science"],
    "ISSN": ["0028-0836", "1476-4687"],
    "URL": "http://dx.doi.org/10.1038/nature12373",
    "link": [
      {"URL": "https://doi.org/10.1038/nature12373", "content-type": "text/html"},
      {"URL": "...pdf", "content-type": "application/pdf"}
    ],
    "license": [
      {"URL": "...", "content-version": "vor", "content-type": "text/html"}
    ],
    "funder": [
      {"DOI": "10.13039/100000001", "name": "National Science Foundation", "award": ["1234567"]}
    ]
  }
}
```

### Key Metadata Fields

| Field | Type | Description |
|---|---|---|
| `title[0]` | string | Paper title |
| `container-title[0]` | string | Journal or book name |
| `author[].given` | string | Author first name |
| `author[].family` | string | Author last name |
| `author[].sequence` | string | Author order: `first` / `additional` |
| `published-print.date-parts` | array | Print publication date [[year, month, day]] |
| `published-online.date-parts` | array | Online publication date [[year, month, day]] |
| `publisher` | string | Publisher |
| `type` | string | Work type (e.g. journal-article) |
| `volume` | string | Volume number |
| `issue` | string | Issue number |
| `page` | string | Page range |
| `DOI` | string | DOI identifier |
| `abstract` | string | Abstract (may be missing for some papers) |
| `reference-count` | number | Number of references |
| `is-referenced-by-count` | number | Citation count (within CrossRef index) |
| `subject` | array[string] | Subject categories |
| `ISSN` | array[string] | Journal ISSN |
| `URL` | string | CrossRef page link |
| `link[]` | array | Full-text link list |
| `license[]` | array | License information |
| `funder[]` | array | Funder information, containing `DOI`, `name`, `award` |

## Rate Limits

| Pool Type | Rate | How to Access |
|---|---|---|
| Public Pool | ~10 requests/s | Default |
| Polite Pool | ~50 requests/s | Include `User-Agent` in request header |

**Best Practices for Batch Resolution**:
- Maintain a request interval ≥ 0.1 seconds (Polite Pool) or ≥ 0.2 seconds (Public Pool)
- Use `try/except` to catch individual failures without interrupting the overall batch
- For large batches (>100 DOIs), consider splitting into sub-batches with longer pauses between them

## Error Handling

| Scenario | HTTP Status | Handling |
|---|---|---|
| DOI not found | 404 | Verify DOI spelling; some older DOIs may not be registered in CrossRef |
| Rate limited | 429 | Back off and retry; check User-Agent header |
| Service unavailable | 503 | Retry with exponential backoff |
| Malformed DOI | 400 | Check DOI format (should be `10.xxxx/yyyy`) |
| Publisher unresponsive | Timeout | Increase timeout to 15–30 seconds |
| Incomplete metadata | 200 but missing fields | Use `.get()` for defensive access; some fields are optional |

```python
def resolve_doi_safe(doi):
    """Safely resolve a DOI, returning a uniform structure."""
    try:
        paper = resolve_doi(doi)
        return {
            "doi": doi,
            "title": paper.get("title", [None])[0],
            "journal": paper.get("container-title", [None])[0],
            "year": (paper.get("published-print") or paper.get("published-online") or {}).get("date-parts", [[None]])[0][0],
            "authors": [f"{a.get('given', '')} {a.get('family', '')}" for a in paper.get("author", [])],
            "citations": paper.get("is-referenced-by-count", 0),
            "type": paper.get("type"),
            "success": True,
        }
    except requests.HTTPError as e:
        return {"doi": doi, "error": f"HTTP {e.response.status_code}", "success": False}
    except Exception as e:
        return {"doi": doi, "error": str(e), "success": False}
```

## Related APIs

- → See [api-crossref.md](api-crossref.md) — Paper search, funder queries, date filtering (full usage of the Works endpoint)
- → See [api-arxiv.md](api-arxiv.md) — Searching preprints (arXiv papers typically have no DOI)
- DOI resolution is a single-item specialization of the CrossRef Works endpoint; for bulk search needs, use the Works search endpoint
