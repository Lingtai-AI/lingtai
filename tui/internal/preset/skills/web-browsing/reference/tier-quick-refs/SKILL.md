---
name: web-browsing-tier-quick-refs
description: >
  Nested web-browsing reference for tier quick-reference commands: Tier 0 PDF,
  Tier 1 APIs, Tier 1.5 Trafilatura, Tier 2 BeautifulSoup, Tier 3 Playwright
  stealth, Tier 4 Jina/Firecrawl, and Tier 5 AI-native search.
version: 1.0.0
---

# Web Browsing Tier Quick References

Nested web-browsing reference. Open this after the top-level router when you need
manual commands for a specific extraction tier instead of the auto-tier script.

## Tier 0 — PDF Direct Download (Quick Reference)

```bash
# Direct PDF link
curl -L "https://arxiv.org/pdf/1706.03762.pdf" -o paper.pdf

# arXiv ID → derive the PDF path
curl -L "https://arxiv.org/pdf/2401.12345.pdf" -o paper.pdf
```

```python
import fitz  # pip install pymupdf
doc = fitz.open("paper.pdf")
text = doc[0].get_text()  # first page
```

→ Full details: [reference/tier-0-pdf.md](../tier-0-pdf.md)

---

## Tier 1 — API Metadata Queries (Quick Reference)

### Academic APIs at a Glance

| API | Best for | Free? |
|-----|----------|-------|
| **OpenAlex** | Any DOI → metadata + citations | ✅ |
| **CrossRef** | DOI → title, authors, journal | ✅ |
| **Semantic Scholar** | AI/ML papers, citation graphs | ✅* |
| **arXiv** | CS/Physics/Math papers | ✅ |
| **Unpaywall** | Find free PDF for any DOI | ✅ |
| **PubMed E-utilities** | Biomedical literature | ✅ |
| **CORE** | Open access full text (30M+) | ✅† |
| **Europe PMC** | Biomedical + PMC full text | ✅ |
| **DBLP** | CS conference papers | ✅ |
| **Papers With Code** | ML papers with code + benchmarks | ✅ |
| **DOAJ** | Open access journal articles | ✅ |
| **Zenodo** | Research data, datasets | ✅ |
| **NASA ADS** | Astrophysics/astronomy | ✅ |

```python
import requests

# OpenAlex — most powerful, completely free
r = requests.get("https://api.openalex.org/works/https://doi.org/10.1038/s41586-023-05995-9")
data = r.json()

# Unpaywall — find free PDF
r = requests.get(f"https://api.unpaywall.org/v2/{doi}?email=lingtai@users.noreply.github.com")
if r.json().get("best_oa_location"):
    pdf_url = r.json()["best_oa_location"].get("url_for_pdf")
```

→ Full details: [reference/tier-1-apis.md](../tier-1-apis.md)
→ Academic search pipeline: [reference/academic-pipeline.md](../academic-pipeline.md)

---

## Tier 1.5 — Trafilatura (Quick Reference)

```python
import trafilatura

# Fetch + extract in one call
downloaded = trafilatura.fetch_url("https://example.com/article")
text = trafilatura.extract(downloaded)

# With metadata
metadata = trafilatura.extract(downloaded, output_format="json", include_metadata=True)

# Batch extraction
results = [trafilatura.extract(trafilatura.fetch_url(url)) for url in urls]
```

→ Full details: [reference/tier-1-5-trafilatura.md](../tier-1-5-trafilatura.md)

---

## Tier 2 — BeautifulSoup (Quick Reference)

```python
import requests
from bs4 import BeautifulSoup

resp = requests.get(url, headers={"User-Agent": "Mozilla/5.0..."})
soup = BeautifulSoup(resp.text, "html.parser")

# Common patterns
title = soup.find("h1").get_text(strip=True)
links = [a["href"] for a in soup.select("a[href]") if a["href"].startswith("http")]
article = "\n".join(p.get_text() for p in soup.select("article p"))
meta_desc = soup.find("meta", attrs={"name": "description"})["content"]
```

→ Full details: [reference/tier-2-beautifulsoup.md](../tier-2-beautifulsoup.md)

---

## Tier 3 — Playwright Stealth (Quick Reference)

```python
from playwright.sync_api import sync_playwright
try:
    from playwright_stealth import Stealth
    _apply_stealth = lambda page: Stealth().use_sync(page)
except ImportError:
    from playwright_stealth import stealth_sync
    _apply_stealth = lambda page: stealth_sync(page)

with sync_playwright() as p:
    browser = p.chromium.launch(headless=True)
    ctx = browser.new_context(
        user_agent="Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) ...",
        viewport={"width": 1920, "height": 1080},
    )
    page = ctx.new_page()
    _apply_stealth(page)  # ← 关键：绕过反爬虫检测
    # Block heavy resources
    page.route("**/*.{png,jpg,jpeg,gif,svg,woff,woff2}", lambda r: r.abort())
    page.goto(url, wait_until="domcontentloaded")  # NOT networkidle!
    page.wait_for_timeout(2000)
    content = page.content()
    browser.close()
```

→ Full details: [reference/tier-3-playwright.md](../tier-3-playwright.md)
→ Anti-detection deep-dive: [reference/stealth.md](../stealth.md)

---

## Tier 4 — Jina Reader / Firecrawl (Quick Reference)

```python
import requests

# Jina Reader — FREE, no key needed
resp = requests.get(f"https://r.jina.ai/{url}",
                    headers={"Accept": "text/markdown"})
markdown = resp.text

# Firecrawl — production-grade (needs API key)
resp = requests.post("https://api.firecrawl.dev/v1/scrape",
    json={"url": url},
    headers={"Authorization": "Bearer YOUR_KEY"})
```

→ Full details: [reference/tier-4-jina-firecrawl.md](../tier-4-jina-firecrawl.md)

---

## Tier 5 — AI-Native Search (Quick Reference)

```python
# DuckDuckGo — free, no key
from ddgs import DDGS
with DDGS() as ddgs:
    results = list(ddgs.text("attention is all you need", max_results=5))

# Tavily — search + extract + answer (needs key)
import requests
r = requests.post("https://api.tavily.com/search",
    json={"api_key": "tvly-xxx", "query": "transformer paper", "max_results": 5})

# Exa — neural/semantic search (needs key)
r = requests.post("https://api.exa.ai/search",
    json={"query": "transformer architecture", "numResults": 5},
    headers={"x-api-key": "xxx"})
```

→ Full details: [reference/tier-5-ai-search.md](../tier-5-ai-search.md)
→ Search strategy guide: [reference/search-strategies.md](../search-strategies.md)

---
