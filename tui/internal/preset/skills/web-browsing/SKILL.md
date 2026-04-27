---
name: web-browsing
description: >
  Comprehensive web browsing and content extraction skill for Claude Code.
  Seven-tier progressive strategy: PDF download, API metadata, trafilatura fast extraction,
  BeautifulSoup scraping, Playwright stealth, Jina Reader/Firecrawl fallback, AI search.
  Covers academic search (arXiv, CrossRef, OpenAlex, Unpaywall, CORE, Europe PMC, DBLP,
  Semantic Scholar, PubMed, Papers With Code, DOAJ, Zenodo, NASA ADS), search engines
  (DuckDuckGo, Tavily, Exa, Serper, Brave, Google Custom Search), real-time data (yfinance,
  Open-Meteo, Stack Exchange, Wikipedia, Google News RSS, Reddit JSON, Hacker News),
  social media extraction (Reddit, HN, Mastodon, X/Twitter, GitHub), and anti-detection
  techniques (fingerprinting, User-Agent rotation, proxy strategies, CAPTCHA handling).
  Includes auto-tier decision tree, per-site recommendations, CSS-selector templates,
  regex patterns, and a runnable extraction script with error-driven fallback.
  Use this skill whenever you need to fetch, scrape, extract, or search web content.
version: 3.0.0
---

# web-browsing

> **Browse the web — a seven-tier progressive playbook.**
> From a one-line `curl` to AI-native search; pick the cheapest tier that works, escalate only on failure.
>
> **Tier escalation:** 0 → 1 → 1.5 → 2 → 3 → 4 → 5
> Each tier is ~10× heavier than the previous. Auto-tier picks the cheapest viable one.

---

## Quick Decision Tree

```
URL arrives → Is it a PDF? → Tier 0 (curl + fitz)
            → Has a known API? → Tier 1 (API query)
            → Static HTML article? → Tier 1.5 (trafilatura, 10-50× faster than BS)
            → Needs structured data? → Tier 2 (BeautifulSoup)
            → JS-rendered / protected? → Tier 3 (Playwright stealth)
            → Still failing? → Tier 4 (Jina Reader / Firecrawl)
            → Need to discover content? → Tier 5 (Tavily / Exa AI search)
```

---

## Tier Overview

| Tier | Method | Speed | Tools | Reference |
|------|--------|-------|-------|-----------|
| **0** | PDF Direct Download | ~1s | `curl` + `fitz` | [tier-0-pdf.md](reference/tier-0-pdf.md) |
| **1** | API Metadata Queries | ~0.5s | `requests` / `web_read` | [tier-1-apis.md](reference/tier-1-apis.md) |
| **1.5** | Trafilatura Fast Extraction | ~2s | `trafilatura` | [tier-1-5-trafilatura.md](reference/tier-1-5-trafilatura.md) |
| **2** | BeautifulSoup Structured Extraction | ~5s | `requests` + `BS4` | [tier-2-beautifulsoup.md](reference/tier-2-beautifulsoup.md) |
| **3** | Playwright Stealth | ~15s | `playwright` + stealth | [tier-3-playwright.md](reference/tier-3-playwright.md) |
| **4** | API Fallback (Jina / Firecrawl) | ~3s | `requests` to API | [tier-4-jina-firecrawl.md](reference/tier-4-jina-firecrawl.md) |
| **5** | AI-Native Search | ~5s | `ddgs` / Tavily / Exa | [tier-5-ai-search.md](reference/tier-5-ai-search.md) |

---

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

→ Full details: [reference/tier-0-pdf.md](reference/tier-0-pdf.md)

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

→ Full details: [reference/tier-1-apis.md](reference/tier-1-apis.md)
→ Academic search pipeline: [reference/academic-pipeline.md](reference/academic-pipeline.md)

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

→ Full details: [reference/tier-1-5-trafilatura.md](reference/tier-1-5-trafilatura.md)

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

→ Full details: [reference/tier-2-beautifulsoup.md](reference/tier-2-beautifulsoup.md)

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

→ Full details: [reference/tier-3-playwright.md](reference/tier-3-playwright.md)
→ Anti-detection deep-dive: [reference/stealth.md](reference/stealth.md)

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

→ Full details: [reference/tier-4-jina-firecrawl.md](reference/tier-4-jina-firecrawl.md)

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

→ Full details: [reference/tier-5-ai-search.md](reference/tier-5-ai-search.md)
→ Search strategy guide: [reference/search-strategies.md](reference/search-strategies.md)

---

## Auto-Tier Decision Tree

The bundled `extract_page.py` script auto-selects the cheapest viable tier.完整逻辑见 `scripts/extract_page.py` 的 `auto_tier()` 函数。

**分层规则概要：**

| URL 特征 | 分配 Tier |
|----------|----------|
| PDF (.pdf 后缀 或 /pdf/ 路径) | Tier 0 |
| 学术 API (arXiv, CrossRef, DOI, PubMed, etc.) | Tier 1 |
| 静态内容站 (Wikipedia, BBC, Stack Overflow, etc.) | Tier 1.5 |
| 需结构化提取 (GitHub, Reddit, Nature) | Tier 2 |
| 需 JS 渲染/反爬 (Scholar, Springer, Medium, Reuters) | Tier 3 |
| 其他 → 默认 Tier 1.5 (trafilatura) | Tier 1.5 |

---

## Per-Site Tier Recommendations

| Site | Recommended tier | Success rate | Notes |
|------|------------------|--------------|-------|
| arXiv abstract | Tier 1 | high | API or web_read |
| arXiv PDF | Tier 0 | high | curl -L + fitz |
| OpenAlex / CrossRef | Tier 1 | high | Fully free, most reliable |
| Unpaywall | Tier 1 | high | Finds OA PDF for any DOI |
| DBLP | Tier 1 | high | CS papers, conference proceedings |
| CORE | Tier 1 | high | OA full text (30M+ papers) |
| Europe PMC | Tier 1 | high | Biomedical + PMC full text |
| Papers With Code | Tier 1 | high | ML papers with code |
| Google Scholar list | Tier 2 | medium-high | curl + BS, needs clean IP |
| Nature.com | Tier 2/3 | medium | og meta cheap; full body needs JS |
| Springer paywalled | Tier 3 | low | Needs cookies/session |
| Medium / Substack | Tier 1.5 | high | trafilatura extracts clean text |
| Reddit | Tier 2 | high | `.json` API or old.reddit.com + BS |
| GitHub | Tier 2 | high | API or BS |
| Wikipedia | Tier 1 | high | REST API `/page/summary/{title}` |
| Hacker News | Tier 1 | high | Firebase API, fully free |
| Google News | Tier 2 | high | RSS feed, free, no key |
| Twitter/X | Tier 3 | low | Aggressive bot detection |
| LinkedIn | Tier 3 | low | Requires login + stealth |
| Any generic article | Tier 1.5 | high | trafilatura — your default |

---

## Real-Time Data Quick Reference

| Source | Method | Free? | Endpoint / Pattern |
|--------|--------|-------|-------------------|
| **Google News** | RSS | ✅ | `https://news.google.com/rss/search?q={query}` |
| **Reddit** | JSON API | ✅ | Append `.json` to any URL + User-Agent header |
| **Hacker News** | Firebase API | ✅ | `https://hacker-news.firebaseio.com/v0/topstories.json` |
| **GitHub** | REST API | ✅* | `https://api.github.com/search/repositories?q={q}&sort=stars` |
| **Stack Exchange** | API | ✅ | `https://api.stackexchange.com/2.3/search?intitle={q}&site=stackoverflow` |
| **Wikipedia** | REST API | ✅ | `https://en.wikipedia.org/api/rest_v1/page/summary/{title}` |
| **Wayback Machine** | API | ✅ | `https://archive.org/wayback/available?url={url}` |
| **Stock data** | yfinance | ✅ | `yf.Ticker("AAPL").history(period="1mo")` |
| **Weather** | Open-Meteo | ✅ | `https://api.open-meteo.com/v1/forecast?...` |

→ Deep-dive: [reference/realtime-data.md](reference/realtime-data.md)
→ News/RSS guide: [reference/news-and-rss.md](reference/news-and-rss.md)
→ Social media: [reference/social-media.md](reference/social-media.md)

---

## Maintenance Protocol

When modifying any code or pattern in this skill, follow these three rules **without exception**:

### Rule 1: Grep Before You Ship — Semantic Sweep

Fixing a bug in one file is not enough. After every fix, search the entire codebase for **all occurrences of the same pattern** — not just the same text, but the same **semantic class**.

```bash
# Fix propagation sweep (run after every code change)
# 1. Same text
grep -rn 'bad_pattern' scripts/ reference/ SKILL.md assets/
# 2. Same semantics (e.g., "placeholder email", "outdated API", "hardcoded path")
grep -rn 'example\.com' | grep -iE 'email|mailto|user-agent'
grep -rn 'stealth_sync|bare_extraction.*\.get'
```

If a bad pattern appears in `scripts/`, it almost certainly also appears in `reference/`, `SKILL.md`, or `assets/`. Find them all or they will drift apart.

### Rule 2: Dirty-First Testing

Smoke tests must include **dirty inputs** — real-world edge cases that expose runtime failures, not just clean decision-logic checks. Every tier's test must include at least one non-mock verification.

Minimum dirty test coverage:
- Non-standard URLs (e.g., `/pdf/ID` without `.pdf` suffix)
- Non-dict return types (e.g., `bare_extraction()` returning Document)
- API version incompatibilities (e.g., stealth v1 vs v2)
- Rejected placeholder values (e.g., Unpaywall 422 on `test@example.com`)
- Deprecated endpoints (e.g., Wikipedia `/page/related/`)

### Rule 3: Single Source of Truth

If the same logic appears in multiple places (e.g., `auto_tier()` in both `extract_page.py` and `SKILL.md`), **the script is the truth** and documentation should point to it, not duplicate it. Duplicated code drifts; references don't.

---

## Known Limitations & Gotchas

1. **Major publishers (Wiley / Science / PNAS / Elsevier):** almost always return 403; APIs are the only practical route. Use Unpaywall to find OA versions.
2. **Nature.com:** do NOT use `networkidle` with Playwright — it will time out. Use `domcontentloaded`.
3. **Google Scholar:** rapid requests get IP-blocked; pace with `time.sleep(2)`. Better: use SerpAPI/Serper.
4. **Semantic Scholar API:** needs free API key for usable rate limits (otherwise 100 req/5min).
5. **PDF links on arXiv:** the abstract page does NOT contain a direct PDF link. Derive: `/pdf/{ID}.pdf`.
6. **Jina Reader:** 20 req/min free tier. For heavy use, get an API key.
7. **Reddit:** must include a descriptive User-Agent header. Rate limit: ~60 req/min.
8. **Medium paywall:** trafilatura often extracts full text even from paywalled articles. If not, try Jina Reader.
9. **DuckDuckGo search:** no API key needed but rate-limited. Use responsibly.
10. **CORE API:** requires free API key from https://core.ac.uk/services/api for reasonable limits.

---

## Bundled Assets

| File | Contents |
|------|----------|
| `assets/api-endpoints.json` | Full API endpoints + parameters for every provider |
| `assets/site-templates.json` | CSS-selector templates for known sites |
| `assets/css-selectors.json` | Common-pattern CSS selector library |
| `assets/regex-patterns.json` | Regex templates for DOI / arXiv / PMID / PMC / ISBN |
| `assets/search-providers.json` | Search engine API configurations |
| `assets/extraction-pipeline.json` | Full extraction pipeline configuration |
| `scripts/extract_page.py` | Executable v3.0 script: `--tier 0-5 + auto`, `--search`, `--fallback` |
| `scripts/cached_get.py` | File-based HTTP cache with TTL support |

---

## Reference Files (Deep-Dives)

For more than quick-reference snippets, load the appropriate reference file:

| Reference | When to load |
|-----------|-------------|
| [tier-0-pdf.md](reference/tier-0-pdf.md) | PDF download + fitz extraction details |
| [tier-1-apis.md](reference/tier-1-apis.md) | All academic/metadata APIs, ID resolution chains |
| [tier-1-5-trafilatura.md](reference/tier-1-5-trafilatura.md) | Trafilatura configuration, batch mode, dedup |
| [tier-2-beautifulsoup.md](reference/tier-2-beautifulsoup.md) | BS4 patterns, CSS selectors, site templates |
| [tier-3-playwright.md](reference/tier-3-playwright.md) | Playwright stealth setup, resource blocking |
| [tier-4-jina-firecrawl.md](reference/tier-4-jina-firecrawl.md) | Jina Reader + Firecrawl API details |
| [tier-5-ai-search.md](reference/tier-5-ai-search.md) | DDG / Tavily / Exa search integration |
| [academic-pipeline.md](reference/academic-pipeline.md) | Full academic search: find → enrich → get PDF |
| [search-strategies.md](reference/search-strategies.md) | Engine selection, query optimization, pagination |
| [news-and-rss.md](reference/news-and-rss.md) | Google News RSS, Reddit JSON, RSS parsing |
| [social-media.md](reference/social-media.md) | Reddit, HN, Mastodon, X/Twitter, GitHub |
| [realtime-data.md](reference/realtime-data.md) | Financial, weather, Stack Exchange, Wikipedia |
| [stealth.md](reference/stealth.md) | Anti-detection, fingerprinting, proxy strategies |
| [migration-from-v2.md](reference/migration-from-v2.md) | What changed from v2 → v3 |

---

## Explicit Decision Flowchart

```
                         ┌──────────────────┐
                         │  URL or query    │
                         └────────┬─────────┘
                                  │
                     ┌──────────────────────────┐
                     │ Is it a URL or a keyword? │
                     └─────┬──────────────┬─────┘
                       URL  │              │ keyword
                             ▼              ▼
                    ┌──────────────┐   ┌──────────────┐
                    │ Is it PDF?   │   │ Tier 5:      │
                    └──┬──────┬───┘   │ AI Search    │
                   yes│    no│       │ (DDG/Tavily/ │
                      ▼      │       │  Exa)        │
               ┌──────────┐  │       └──────────────┘
               │ Tier 0:  │  │
               │ PDF+fitz │  │
               └──────────┘  │
                             ▼
                    ┌──────────────────┐
                    │ Known API?       │
                    │ (arXiv, DOI,     │
                    │  PubMed, etc.)   │
                    └──┬──────────┬───┘
                   yes│        no│
                      ▼          ▼
               ┌──────────┐  ┌──────────────────┐
               │ Tier 1:  │  │ Static HTML?     │
               │ API query│  │ (article/blog)   │
               └──────────┘  └──┬──────────┬─────┘
                            yes│       no│
                               ▼         ▼
                        ┌──────────┐  ┌──────────────────┐
                        │ Tier 1.5:│  │ Structured data? │
                        │trafilat. │  │ (tables, lists)  │
                        └──────────┘  └──┬──────────┬─────┘
                                     yes│       no│
                                        ▼         ▼
                                 ┌──────────┐  ┌──────────────┐
                                 │ Tier 2:  │  │ JS-rendered? │
                                 │   BS4    │  │ Protected?   │
                                 └──────────┘  └──┬──────┬─────┘
                                              yes│    no│
                                                 ▼      ▼
                                          ┌──────────┐ ┌──────────┐
                                          │ Tier 3:  │ │ Tier 4:  │
                                          │Playwright│ │   Jina   │
                                          │ stealth  │ │  Reader  │
                                          └──────────┘ └──────────┘
```
