# Tier 1.5 — Trafilatura Fast Extraction

> Part of the [web-browsing](../SKILL.md) skill.

**When it applies:** Any static HTML page — articles, blogs, news, documentation.
**Tools:** `trafilatura` (**already installed** — no setup needed).
**Speed:** ~0.05s per page (10-50× faster than BeautifulSoup, 200× faster than browser).

```python
import trafilatura

def tier1_5(url):
    """Fast article extraction — no browser, no JS, blazing speed."""
    html = trafilatura.fetch_url(url)
    if not html:
        return None
    text = trafilatura.extract(html)           # Main content as plain text
    meta = trafilatura.bare_extraction(html)   # Returns Document, NOT dict

    # bare_extraction() returns a Document object (has .title, .author etc.)
    # Convert to dict for safe .get() access — do NOT call meta.get() directly!
    if hasattr(meta, '__dict__'):
        meta = {k: v for k, v in meta.__dict__.items() if not k.startswith('_')}
    elif meta is None:
        meta = {}

    return {
        "url": url,
        "method": "tier1.5-trafilatura",
        "title": meta.get("title"),
        "author": meta.get("author"),
        "date": meta.get("date"),
        "description": meta.get("description"),
        "categories": meta.get("categories"),
        "tags": meta.get("tags"),
        "text": text,
        "text_len": len(text) if text else 0,
    }
```

**Also outputs:** Markdown (`output_format="markdown"`), JSON, XML.
**Batch mode:** `trafilatura.spider` for sitemap-based crawling.
**Feed support:** `trafilatura.feed` for RSS/Atom feed extraction.

**Use when:** the page is a static article/blog/news page and you just need the text content. This is your **default first attempt** for any non-API, non-PDF URL.
