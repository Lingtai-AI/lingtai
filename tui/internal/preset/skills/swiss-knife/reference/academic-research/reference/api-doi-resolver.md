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

## Code Example

`resolve_doi` is the core; batch and citation formatting build on it. The DOI's
publisher landing URL comes from a `HEAD` redirect on `doi.org`, not CrossRef.

```python
import requests, time

HEADERS = {"User-Agent": "MyApp/1.0 (mailto:your@email.com)"}

def resolve_doi(doi):
    """Resolve a DOI to full CrossRef metadata (the `message` object)."""
    r = requests.get(f"https://api.crossref.org/works/{doi}", headers=HEADERS, timeout=15)
    r.raise_for_status()
    return r.json()["message"]

def get_publisher_url(doi):
    """Publisher landing page via DOI redirect (HEAD, follow redirects)."""
    return requests.head(f"https://doi.org/{doi}", allow_redirects=True, timeout=10).url

def resolve_dois(doi_list, delay=0.1):
    """Batch-resolve; delay ≥0.1s (Polite) / ≥0.2s (Public). Per-item errors
    captured, not raised. Returns [{doi, metadata} | {doi, error}]."""
    out = []
    for doi in doi_list:
        try:
            out.append({"doi": doi, "metadata": resolve_doi(doi)})
        except requests.HTTPError as e:
            out.append({"doi": doi, "error": str(e)})
        time.sleep(delay)
    return out

# Citation string: pull title/author/container-title/published-*/volume/issue/page
# from resolve_doi(doi) and format per style, e.g. APA:
#   "{family}, {given[0]}. ({year}). {title}. {journal}, {vol}({issue}), {pages}. https://doi.org/{doi}"
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

Defensive access: metadata fields are optional — always use `.get()` with
fallbacks (`paper.get("title", [None])[0]`, `(paper.get("published-print") or
paper.get("published-online") or {}).get("date-parts", [[None]])[0][0]`) and
wrap `resolve_doi` in `try/except requests.HTTPError` so one bad DOI returns a
`{doi, error}` row instead of aborting the batch.

## Related APIs

- → See [api-crossref.md](api-crossref.md) — Paper search, funder queries, date filtering (full usage of the Works endpoint)
- → See [api-arxiv.md](api-arxiv.md) — Searching preprints (arXiv papers typically have no DOI)
- DOI resolution is a single-item specialization of the CrossRef Works endpoint; for bulk search needs, use the Works search endpoint
