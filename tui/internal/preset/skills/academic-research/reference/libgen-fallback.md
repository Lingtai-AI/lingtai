# Library Genesis (LibGen) Fallback

> **This is the last-resort fallback.** Use only after Unpaywall, CORE, Europe PMC, and arXiv have all failed. See [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) for the full acquisition chain.

> **Legal notice**: LibGen hosts content of unclear copyright status in many jurisdictions. Use is solely the user's responsibility. Always prefer legitimate open-access channels first. In sensitive jurisdictions, consider using Tor or a VPN.

---

## Coverage Reality Check

LibGen's hit rate varies dramatically by material type. Set expectations accordingly:

| Material Type | Hit Rate | Recommended Search | Notes |
|---|---|---|---|
| **Books / Textbooks / Monographs** | ⭐⭐⭐⭐⭐ | ISBN | LibGen's strongest category |
| **Older journal articles (>5 years)** | ⭐⭐⭐⭐ | DOI (if mirror supports) or title | Good cross-discipline coverage |
| **Mid-age academic papers** | ⭐⭐⭐ | Keywords + title + author | Often already on arXiv — check arXiv first |
| **Current-year paywall journals** (Nature/Science/Cell/Lancet) | ⭐ | Keywords (DOI often unreliable) | Very low hit rate for newest issues |
| **Current Elsevier/Wiley/Springer** | ⭐ | Keywords | Low hit rate; these publishers aggressively pursue takedowns |

**Key takeaway**: LibGen is excellent for books and older papers. For current-year paywall articles, expect low success — this is not a magic unlock for Nature's latest issue.

**DOI search caveat**: The `ads.php?doi=` endpoint returned HTTP 500 on tested mirrors (as of 2026-04-28). Use ISBN for books, keyword+title search for papers.

---

## Live Mirror Discovery (Critical)

LibGen mirrors change frequently. **Never hardcode URLs** — always discover the current live mirror at runtime.

### Discovery Strategies

1. **HTTP HEAD probe** (recommended): Try a list of candidate mirrors; first one returning HTTP 200 wins.

```python
import requests

CANDIDATE_MIRRORS = [
    "https://libgen.li",
    "https://libgen.is",
    "https://libgen.rs",
    "https://libgen.gs",
    "https://libgen.st",
]

def find_live_libgen_mirror(candidates=CANDIDATE_MIRRORS, timeout=5):
    """Find a currently live LibGen mirror by HTTP HEAD probe.
    Returns the base URL (e.g., 'https://libgen.li') or None.
    """
    for url in candidates:
        try:
            r = requests.head(url + "/", timeout=timeout, allow_redirects=True)
            if r.status_code == 200:
                return url
        except (requests.Timeout, requests.ConnectionError):
            continue
    return None
```

2. **DNS probe**: `dig +short libgen.is libgen.rs libgen.li libgen.gs` — if DNS resolves but HTTP fails, the mirror may be temporarily down.

3. **Community lists**: When automated discovery fails, consult:
   - Reddit r/Piracy megathread (updated community mirror list)
   - Wikipedia "Library Genesis" article (often lists current mirrors)
   - LibGen forum: `forum.mhut.org`

4. **Aggregator proxies**: Sites like `library-genesis.online` auto-redirect to a live mirror (but are themselves single points of failure).

> **Important**: All candidate URLs in this document may be dead by the time you read this. Run `find_live_libgen_mirror()` to confirm.

---

## Search Methods

All search URLs use `{MIRROR}` as a placeholder for the live mirror base URL (e.g., `https://libgen.li`).

### By DOI (Most Reliable)

```
GET {MIRROR}/ads.php?doi={DOI}
```

Example: `{MIRROR}/ads.php?doi=10.1038/nature12373`

DOI search is the most precise method **when available**. Some mirrors return HTTP 500 for DOI queries — fall back to title/author search if this happens.

### By ISBN

```
GET {MIRROR}/index.php?req={ISBN}&columns[]=i&open=0&res=25&view=simple
```

### By Title/Author (Keyword)

```
GET {MIRROR}/index.php?req={QUERY}&columns[]=t&columns[]=a&objects[]=f&open=0&res=25&view=simple&phrase=1
```

| Parameter | Description |
|-----------|-------------|
| `req` | Search query |
| `columns[]` | `t` = title, `a` = author, `i` = ISBN, `s` = series |
| `objects[]` | `f` = files (books/papers) |
| `res` | Results per page (max 100) |
| `view` | `simple` or `detailed` |
| `phrase` | `1` = exact phrase match |

### By LibGen Internal Search API (JSON)

Some mirrors expose a JSON API:

```python
def search_libgen(mirror, query, limit=10):
    """Search LibGen and parse results from HTML."""
    url = f"{mirror}/index.php"
    params = {
        "req": query,
        "columns[]": ["t", "a"],
        "objects[]": "f",
        "open": 0,
        "res": limit,
        "view": "simple",
        "phrase": 1,
    }
    r = requests.get(url, params=params, timeout=15)
    r.raise_for_status()
    return r.text  # Parse HTML for MD5 hashes and download links
```

---

## Download Method

LibGen download requires two steps: find the record's MD5 hash, then construct the download URL.

### Step 1: Get MD5 from search results

Search results contain MD5 hashes in the detail links. Extract via regex or HTML parsing:

```python
import re

def extract_md5_from_results(html):
    """Extract MD5 hashes from LibGen search result HTML."""
    return re.findall(r'[a-f0-9]{32}', html)
```

### Step 2: Construct download URL

Download URL patterns vary by mirror. Common patterns:

```
{MIRROR}/main/{MD5_UPPERCASE}
{MIRROR}/get.php?md5={MD5_LOWERCASE}
{MIRROR}/ads.php?md5={MD5_LOWERCASE}
```

If one pattern fails, try the next. The detail page (`ads.php?md5=...`) usually has direct download links.

---

## Common Failure Patterns

| Failure | Cause | Solution |
|---------|-------|----------|
| All mirrors return timeout/000 | Network blocks LibGen | Try VPN/Tor; check community lists for new mirrors |
| HTTP 200 but empty results | DOI not in LibGen database | Try title/author search as fallback |
| Download link gives wrong PDF | DOI mismatch in LibGen index | Verify title/authors match; try ISBN for books |
| Cloudflare CAPTCHA | Mirror behind Cloudflare | Use a different mirror or wait and retry |
| Download token expired | Session-based tokens time out | Re-fetch the detail page for a fresh link |
| 404 on search endpoint | Mirror uses different URL scheme | Try `index.php?req=` instead of `search.php?req=` |

---

## Integration with PDF Pipeline

LibGen sits at the end of the acquisition chain:

```
Unpaywall → CORE → Europe PMC → arXiv → Publisher OA → LibGen (last resort)
```

**Before trying LibGen**, confirm you have exhausted all legitimate OA channels:
1. Unpaywall: no OA version found
2. CORE: no repository copy
3. Europe PMC: not in PMC (or not biomedical)
4. arXiv: no preprint version
5. Publisher page: no free access

Only then proceed to LibGen.

---

## See Also

- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — full PDF acquisition chain
- [api-unpaywall.md](api-unpaywall.md) — OA status check (try first)
- [api-core.md](api-core.md) — repository full text (try second)
- [api-europe-pmc.md](api-europe-pmc.md) — biomedical full text (try third)
- [api-arxiv.md](api-arxiv.md) — preprint PDF (try fourth)
