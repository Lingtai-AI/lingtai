---
name: web-browsing-manual
description: How to browse the web — reach for this before fetching any URL. Four-tier progressive strategy covering PDFs, academic APIs (arXiv, CrossRef, OpenAlex), static pages (curl + BeautifulSoup), and JS-rendered or login-gated pages (Playwright stealth). Includes site-specific playbooks for Google Scholar, Nature, Springer, arXiv, PubMed, and NASA ADS, CSS-selector tables, regex cheat sheets, known-site API endpoints, and a runnable auto-tier script with error-driven fallback. Use when the agent needs to read, extract, or download web content.
version: 2.0.0
tags: [web, browsing, extraction, tier0, tier1, tier2, tier3]
---

# web-browsing-manual

> **Browsing the web — a four-tier progressive playbook.**
> From a one-line `curl` to headless Chromium; pick the cheapest tier that works, escalate only on failure.
>
> *Adapted from the `web-content-extractor` skill authored in the `skill-developer` network (avatars under `minimax_cn`).*

---

## Tier 0 — One-liner (PDF direct link / DOI / arXiv ID)

**When it applies:** PDF direct links, DOI strings, arXiv IDs.
**Tools:** `curl` + `fitz` (no script needed).

```bash
# Direct PDF link
curl -L "https://arxiv.org/pdf/1706.03762.pdf" -o paper.pdf

# arXiv ID → derive the PDF path
curl -L "https://arxiv.org/pdf/$(echo "2401.12345" | sed 's/\.//').pdf" -o paper.pdf
```

**Python (extract PDF text):**
```python
import fitz  # pip install pymupdf
doc = fitz.open("paper.pdf")
print(doc[0].get_text()[:500])  # first-page preview
```

**Use when:** the URL contains `.pdf`, or you already have a DOI / arXiv ID.

---

## Tier 1 — Fast metadata (API query / web_read)

**When it applies:** arXiv abstract pages, academic resources whose DOI or ID is known.
**Tools:** the `web_read` tool, or APIs (no extra libraries needed).

```python
# Method A: web_read (zero code, fastest)
web_read({
    url: "https://arxiv.org/abs/1706.03762",
    output_format: "markdown"
})

# Method B: arXiv API (precise metadata)
# GET https://export.arxiv.org/api/query?id_list=1706.03762
# Returns: title, abstract, authors, categories, PDF link

# Method C: CrossRef API (any DOI)
# GET https://api.crossref.org/works/10.1038/s41586-023-05995-9
# Returns: title, authors, journal, license, DOI

# Method D: OpenAlex API (most powerful, any DOI)
# GET https://api.openalex.org/works/https://doi.org/10.1038/s41586-023-05995-9
# Returns: full metadata + citation count + topic classification + PDF link
```

**Use when:** you have a DOI or arXiv ID and just want metadata + abstract quickly.

---

## Tier 2 — Structured extraction (curl + BeautifulSoup)

**When it applies:** Google Scholar list pages, Nature.com (`og:meta`), arXiv (structured metadata), open-access articles.
**Tools:** requires `pip install requests beautifulsoup4 lxml`.
**Speed:** ~0.15 s per page.

```python
import requests
from bs4 import BeautifulSoup
import re

def extract_layer2(url):
    """Tier 2: curl + BeautifulSoup structured extraction."""
    r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
    soup = BeautifulSoup(r.text, "lxml")

    # Generic: title
    title = soup.find("title").get_text(strip=True) if soup.find("title") else None

    # Google Scholar (search results page)
    if "scholar.google" in url:
        papers = []
        for card in soup.select("div.gs_ri"):
            title_el = card.select_one("h3.gs_rt")
            abstract_el = card.select_one("div.gs_rs")
            meta_el = card.select_one("div.gs_fl")
            papers.append({
                "title": title_el.get_text(strip=True) if title_el else None,
                "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
                "meta": meta_el.get_text(strip=True) if meta_el else None,
            })
        return {"title": title, "papers": papers}

    # arXiv: find PDF links
    elif "arxiv.org" in url:
        abstract_el = soup.find("blockquote", class_="abstract")
        pdf_links = re.findall(r'href="(/pdf/[^"]+\.pdf)"', r.text)
        return {
            "title": title,
            "abstract": abstract_el.get_text(strip=True) if abstract_el else None,
            "pdf_links": [urljoin(url, p) for p in pdf_links[:3]],
        }

    # Nature.com: og meta
    elif "nature.com" in url:
        og_title = soup.find("meta", property="og:title")
        og_desc = soup.find("meta", property="og:description")
        citation_doi = soup.find("meta", attrs={"name": "citation_doi"})
        return {
            "title": og_title["content"] if og_title else title,
            "description": og_desc["content"] if og_desc else None,
            "doi": citation_doi["content"] if citation_doi else None,
        }

    # Default
    else:
        return {"title": title}
```

**Key CSS selectors:**

| Site | Selector | Extracts |
|------|----------|----------|
| Google Scholar | `div.gs_ri` | One paper card |
| Google Scholar | `h3.gs_rt` | Paper title |
| Google Scholar | `div.gs_rs` | Abstract / snippet |
| Google Scholar | `div.gs_fl` | Citation / link metadata |
| arXiv | `h1.title` | Title |
| arXiv | `blockquote.abstract` | Abstract |
| arXiv | `a[href*="/pdf/"]` | PDF link |
| Nature.com | `meta[property="og:title"]` | Title (curl-reachable) |
| Nature.com | `meta[name="citation_doi"]` | DOI |
| Springer | `meta[name="citation_doi"]` | DOI (open access) |

**Use when:** the page is static, no JS rendering required, and curl is fast enough.

---

## Tier 3 — Bespoke (Playwright stealth)

**When it applies:** Google Scholar logged-in views, persistently-404 pages (Springer), full body text that requires JS rendering.
**Tools:** requires `pip install playwright && playwright install chromium && pip install playwright-stealth`.
**Note:** for Nature / Springer use `domcontentloaded`, NOT `networkidle`.

```python
from playwright.sync_api import sync_playwright
from playwright_stealth import stealth_sync

def extract_layer3(url, wait_time=3):
    """Tier 3: Playwright stealth — JS-rendered or login-gated pages."""
    with sync_playwright() as p:
        browser = p.chromium.launch(headless=True)
        page = browser.new_page()
        stealth_sync(page)

        # CRITICAL: do NOT use networkidle (Nature / Springer hang forever)
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(wait_time * 1000)

        return {
            "url": page.url,
            "title": page.title(),
            "body": page.inner_text("body")[:2000],
            "html_len": len(page.content()),
        }
```

**Use when:** Tier 1 / 2 fail, or the page genuinely requires JS rendering.

---

## Auto-tier decision tree

```python
def auto_tier(url):
    """Pick the cheapest viable tier."""
    url_lower = url.lower()

    # Tier 0: PDF direct link
    if url_lower.endswith(".pdf"):
        return 0, "PDF direct link → curl -L"

    # Tier 1: API / static structured page
    if re.search(r"^(10\.\d{4,}|arXiv:)", url):
        return 1, "DOI / arXiv ID → API query"
    if "arxiv.org/abs" in url_lower and "scholar" not in url_lower:
        return 1, "arXiv abstract page → web_read"

    # Tier 2: curl + BS
    if "scholar.google" in url_lower:
        return 2, "Scholar search → curl + BS"
    if "nature.com" in url_lower:
        return 2, "Nature → curl + BS (og meta)"
    if "springer.com" in url_lower:
        return 3, "Springer → Playwright (session needed)"

    # Default
    return 2, "Generic → curl + BS, fall back to Tier 3 on failure"
```

---

## Per-site tier recommendations

| Site | Recommended tier | Success rate | Notes |
|------|------------------|--------------|-------|
| arXiv abstract page | Tier 1 | high | web_read or API |
| arXiv PDF direct link | Tier 0 | high | curl -L + fitz |
| Google Scholar list | Tier 2 | medium-high | curl + BS, needs a clean IP |
| Google Scholar detail | Tier 3 | low | needs logged-in session or stealth |
| Nature.com | Tier 2/3 | medium | og meta is cheap; full body needs JS |
| Springer open access | Tier 2 | medium | DOI meta is reachable |
| Springer paywalled | Tier 3 | low | needs cookies / session |
| OpenAlex API | Tier 1 | high | fully free, most reliable |
| CrossRef API | Tier 1 | high | fully free, most reliable |

---

## Bundled assets

| File | Contents |
|------|----------|
| `assets/api-endpoints.json` | API endpoints + parameters for each provider |
| `assets/site-templates.json` | CSS-selector templates for known sites |
| `assets/css-selectors.json` | Common-pattern CSS selector library |
| `assets/regex-patterns.json` | Regex templates for DOI / arXiv / PMID |
| `scripts/extract_page.py` | Executable script, accepts `--tier` argument |

---

## Known limitations

1. **Major publishers (Wiley / Science / PNAS / Elsevier):** almost always return 403; APIs are the only practical route.
2. **Nature.com:** do NOT use `networkidle` with Playwright — it will time out. Use `domcontentloaded`.
3. **Google Scholar:** rapid requests from the same IP get temporarily blocked; pace with `time.sleep(2)`.
4. **Semantic Scholar API:** needs an API key (free) for usable rate limits, otherwise heavily throttled.
5. **PDF links on arXiv:** the abstract page does NOT contain a direct PDF link in its HTML. Derive the path from the arXiv ID: `/pdf/{ID}.pdf`.
