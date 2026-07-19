# Error Handling Patterns

> Cross-cutting reference for common error patterns across all academic APIs. Read this when your API call fails.

---

## Common Error Patterns

### 429 Rate Limiting

**Symptom**: HTTP 429 "Too Many Requests"

**Affected APIs**: Semantic Scholar (strict), CORE (very strict without key), Google Scholar (IP-level), all APIs at high volume

**Strategy**:
1. **Exponential backoff**: wait 5s → 10s → 20s → give up
2. **Switch API**: if OpenAlex rate limits, try Semantic Scholar; if both fail, wait
3. **Batch reduction**: request fewer results per call
4. **API key**: if available, use it (dramatically increases limits for CORE and Semantic Scholar)

Generic retry-with-backoff wrapper — the same shape reappears (with only the
base delay and status-code branches tweaked) throughout the per-API references;
they now link here instead of repeating it:

```python
import time, requests

def api_get_with_backoff(url, params=None, headers=None, retries=3, base_delay=2, timeout=15):
    """GET with exponential backoff on 429/5xx. Adjust base_delay per API
    (e.g. 2 for CrossRef/OpenAlex, 12 for Semantic Scholar without a key)."""
    for attempt in range(retries):
        r = requests.get(url, params=params, headers=headers, timeout=timeout)
        if r.status_code == 200:
            return r.json()
        elif r.status_code == 429 or r.status_code >= 500:
            time.sleep(base_delay * (2 ** attempt))
        else:
            r.raise_for_status()
    raise Exception(f"Request failed after {retries} retries: {url}")
```

### 403 Publisher Blocks

**Symptom**: HTTP 403 on publisher URLs (Nature, Springer, Elsevier, Wiley, Science)

**Strategy**: Never attempt direct scraping of paid publishers. Use the API chain:
1. Unpaywall → check for OA version
2. CORE → check for repository copy
3. Europe PMC → check for full text (biomedical)
4. arXiv → check for preprint version
5. If all fail: metadata only (no full text available)

### Timeout Patterns

**Symptom**: Request hangs or returns 504/503

**Affected APIs**: CrossRef (slow for bulk queries), Google Scholar (scraping), Semantic Scholar (under load)

**Timeout guidance**:
| API | Recommended Timeout |
|-----|-------------------|
| OpenAlex | 10s |
| CrossRef | 15s (Polite Pool: 10s) |
| arXiv | 20s |
| Semantic Scholar | 15s |
| CORE | 20s |
| Europe PMC | 15s |
| NASA ADS | 15s |
| INSPIRE-HEP | 15s |

### Empty Results

**Symptom**: 200 OK but zero results

**Possible causes**:
1. **Query too specific**: Try broader terms or fewer field prefixes
2. **API doesn't index this content**: arXiv won't find biology papers; PubMed won't find CS papers
3. **DOI/title mismatch**: Verify the DOI or title is correct
4. **Rate limit disguised**: Some APIs return empty results instead of 429 when throttled
5. **Encoding issues**: Special characters in queries may need URL encoding
6. **Abstract appears empty (OpenAlex)**: OpenAlex returns `abstract_inverted_index` (a dict), not a plain string — see [api-openalex.md](api-openalex.md) for the reconstruction function

---

## API Fallback Chains

### For Paper Discovery (by use case)
```
OpenAlex (general discovery, fast, broad) — start here for most queries
Semantic Scholar (citation networks, TLDR summaries) — use when you need citation context
arXiv (physics/CS/math preprints) — use when preprints are preferred
Google Scholar (broadest coverage) — last resort only (stealth required, IP-block risk)
```
Note: These are **complementary tools**, not a sequential fallback chain. OpenAlex rarely fails; switch based on **what you need** (citations? preprints? broadest coverage?), not because the previous API errored.

### For Full-Text PDF
```
Unpaywall (OA check) → CORE (repository copies) → Europe PMC (biomedical) → arXiv (preprints) → Publisher OA → give up
```

### For Citation Networks
```
Semantic Scholar (forward + backward) → OpenAlex (referenced_works) → NASA ADS (astrophysics) → INSPIRE-HEP (HEP)
```

---

## API Speed & Reliability Summary

| API | Speed | Reliability | Key Needed? | Best Use |
|-----|-------|------------|-------------|----------|
| OpenAlex | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | No | General discovery |
| CrossRef | ⭐⭐⭐ | ⭐⭐⭐⭐ | No (mailto helps) | DOI resolution |
| DOI Resolver | ⭐⭐⭐⭐⭐ | ⭐⭐⭐⭐⭐ | No | DOI → structured citation |
| arXiv | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | No | Physics/CS/math |
| Semantic Scholar | ⭐⭐⭐ | ⭐⭐⭐ | Recommended | Citation networks |
| CORE | ⭐⭐ | ⭐⭐ | Optional (big difference) | OA full text |
| PubMed | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | No | Biomedical literature |
| Europe PMC | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | No | Biomedical + full text XML |
| NASA ADS | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | Yes (free) | Astrophysics |
| INSPIRE-HEP | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | No | High-energy physics |
| Google Scholar | ⭐⭐ | ⭐⭐ | No (stealth needed) | Broadest coverage |

---

## See Also

- [decision-tree.md](decision-tree.md) — routing: which API for which input
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — PDF acquisition with fallback chain
- [pipeline-discovery.md](pipeline-discovery.md) — multi-API discovery workflow
- [pipeline-latex-writing.md](pipeline-latex-writing.md) — LaTeX compilation errors (undefined citations, missing packages, font issues, CJK) — these are not API errors but common in the academic writing workflow
