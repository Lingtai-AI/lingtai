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

```python
import time

def api_call_with_backoff(func, max_retries=3):
    """Call an API with exponential backoff on 429."""
    for attempt in range(max_retries):
        try:
            return func()
        except Exception as e:
            if "429" in str(e) and attempt < max_retries - 1:
                wait = 5 * (2 ** attempt)  # 5, 10, 20
                time.sleep(wait)
            else:
                raise
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

---

## API Fallback Chains

### For Paper Discovery
```
OpenAlex (fast, broad) → Semantic Scholar (citation networks) → arXiv (preprints) → Google Scholar (last resort, stealth required)
```

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
| arXiv | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | No | Physics/CS/math |
| Semantic Scholar | ⭐⭐⭐ | ⭐⭐⭐ | Recommended | Citation networks |
| CORE | ⭐⭐ | ⭐⭐ | Optional (big difference) | OA full text |
| Europe PMC | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | No | Biomedical |
| NASA ADS | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | Yes (free) | Astrophysics |
| INSPIRE-HEP | ⭐⭐⭐⭐ | ⭐⭐⭐⭐ | No | High-energy physics |
| Google Scholar | ⭐⭐ | ⭐⭐ | No (stealth needed) | Broadest coverage |

---

## See Also

- [decision-tree.md](decision-tree.md) — routing: which API for which input
- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — PDF acquisition with fallback chain
- [pipeline-discovery.md](pipeline-discovery.md) — multi-API discovery workflow
