# Tier 4 — API Fallback (Jina Reader / Firecrawl)

> Part of the [web-browsing](../SKILL.md) skill.

**When it applies:** Everything else fails. Jina Reader is the universal fallback — it renders JS server-side and returns clean markdown.
**Tools:** `requests` (Jina Reader) or `firecrawl-py` (Firecrawl).
**Speed:** ~2-5s (server-side rendering).
**Cost:** Jina Reader free (20 req/min), Firecrawl free (500 credits/month).

### Jina Reader — Universal Page-to-Markdown (FREE, no key)

```python
import requests

def jina_extract(url, fmt="markdown"):
    """Convert any URL to clean markdown via Jina Reader API."""
    r = requests.get(
        f"https://r.jina.ai/{url}",
        headers={
            "Accept": f"text/{fmt}",
            "X-Return-Format": fmt,
        },
        timeout=30,
    )
    r.raise_for_status()
    return r.text

# Also supports:
# X-With-Links: true → include link references
# X-With-Images: true → include image descriptions
# Batch: https://r.jina.ai/{url1},{url2}
```

**One-liner:** `curl -s "https://r.jina.ai/$URL"`

### Firecrawl — Production-Grade Scraping (Freemium)

```python
# pip install firecrawl-py
from firecrawl import FirecrawlApp

app = FirecrawlApp(api_key="your-key")
result = app.scrape_url(url, params={"formats": ["markdown", "html"]})
```

**Use when:** All local methods fail (403, CAPTCHA, heavy JS, etc.). Jina Reader should be your first fallback — it's free, fast, and handles most cases.
