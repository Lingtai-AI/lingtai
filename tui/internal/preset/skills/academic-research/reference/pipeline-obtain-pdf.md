# Pipeline: Obtain Paper Full-Text & PDF

> Combines capabilities from scholar-obtainer + web-content-extractor.
> End-to-end from metadata to full text: DOI → Metadata → Free PDF → Download → Text Extraction, with support for web page content scraping.

## Goal

Given a DOI / arXiv ID / paper URL, retrieve the paper's full text (PDF or plain text) as completely as possible, along with full metadata.

---

## Workflow Steps

1. **Determine input type** — DOI / arXiv ID / PDF direct link / web page URL?
2. **Resolve metadata** — CrossRef (DOI) / OpenAlex / arXiv API.
3. **Find free PDF** — Unpaywall / arXiv direct link / PMC.
4. **Download PDF** — Direct download via curl / requests.
5. **(If web page, not PDF) Extract web page body** — Select BeautifulSoup or Camoufox based on the site.
6. **Extract text from PDF** — PyMuPDF text extraction.
7. **Output** — Return `(status, filepath_or_text, metadata)`.

---

## Decision Tree

```
What is the input?
├─ PDF direct link (ends with .pdf)
│   └─ curl download → PyMuPDF extract text
│
├─ DOI (10.xxxx/...)
│   ├─ CrossRef resolve metadata
│   ├─ Unpaywall find free PDF
│   │   ├─ Found → Download PDF → Extract text
│   │   └─ Not found → Return landing page URL
│   └─ OpenAlex supplementary metadata
│
├─ arXiv ID (e.g. 2301.00001)
│   ├─ arXiv API fetch metadata
│   └─ https://arxiv.org/pdf/{ID}.pdf download → Extract text
│
├─ Web page URL (nature.com / springer.com / scholar, etc.)
│   ├─ Tier 1: web_read tool (fastest)
│   ├─ Tier 2: curl + BeautifulSoup (structured extraction)
│   └─ Tier 3: Camoufox (JS rendering / login-required pages)
│
└─ Title / Keywords → Discover first, then obtain → See [pipeline-discovery.md](pipeline-discovery.md)
```

---

## Code Examples

### 1. DOI → Metadata (CrossRef)

```python
import requests


def resolve_doi(doi: str) -> dict:
    """Resolve DOI via CrossRef and return complete metadata."""
    doi = doi.replace("https://doi.org/", "").replace("http://doi.org/", "")
    r = requests.get(
        f"https://api.crossref.org/works/{doi}",
        headers={"User-Agent": "ResearchBot/1.0 (mailto:user@example.com)"},
        timeout=10,
    )
    d = r.json()["message"]
    return {
        "title": d["title"][0],
        "authors": [f"{a.get('given', '')} {a.get('family', '')}" for a in d.get("author", [])],
        "year": d.get("published-print", d.get("published-online", {})).get("date-parts", [[0]])[0][0],
        "journal": d.get("container-title", [""])[0],
        "doi": doi,
        "citations": d.get("is-referenced-by-count", 0),
        "url": d.get("URL", f"https://doi.org/{doi}"),
    }
```

### 2. Find Free PDF (Unpaywall)

```python
def find_free_pdf(doi: str, email: str = "user@example.com") -> dict:
    """Find free PDF URL via Unpaywall."""
    doi = doi.replace("https://doi.org/", "")
    r = requests.get(
        f"https://api.unpaywall.org/v2/{doi}",
        params={"email": email},
        timeout=10,
    ).json()

    if r.get("is_oa") and r.get("best_oa_location"):
        loc = r["best_oa_location"]
        return {
            "free": True,
            "pdf_url": loc.get("pdf_url"),
            "source": loc.get("repository_name", "Unknown"),
            "license": loc.get("license", "Unknown"),
            "landing_url": loc.get("landing_url"),
        }
    return {"free": False, "title": r.get("title")}
```

### 3. Download PDF

```python
import os


def download_pdf(url: str, filepath: str, headers: dict | None = None) -> str:
    """Download PDF and save to filepath."""
    os.makedirs(os.path.dirname(filepath) or ".", exist_ok=True)
    if headers is None:
        headers = {"User-Agent": "ResearchBot/1.0 (mailto:user@example.com)"}
    r = requests.get(url, headers=headers, stream=True, timeout=30)
    r.raise_for_status()
    with open(filepath, "wb") as f:
        for chunk in r.iter_content(chunk_size=8192):
            f.write(chunk)
    return filepath
```

### 4. Extract Text from PDF (PyMuPDF)

```python
import fitz  # pip install pymupdf


def extract_pdf_text(filepath: str, max_pages: int | None = None) -> str:
    """Extract plain text from PDF."""
    doc = fitz.open(filepath)
    pages = max_pages or len(doc)
    return "\n".join(doc[i].get_text() for i in range(min(pages, len(doc))))


def extract_pdf_summary(filepath: str) -> dict:
    """Extract PDF summary (first 3 pages) and metadata."""
    doc = fitz.open(filepath)
    meta = doc.metadata
    first_pages = "\n".join(doc[i].get_text() for i in range(min(3, len(doc))))
    return {"meta": meta, "preview": first_pages[:1000]}
```

### 5. Web Page Content Extraction (Multi-Tier)

> ⚠️ Migrated from the legacy `playwright_stealth` API to Camoufox.

```python
import re
from urllib.parse import urljoin
import requests
from bs4 import BeautifulSoup


def extract_web_tier2(url: str) -> dict:
    """Tier 2: curl + BeautifulSoup structured extraction for static pages."""
    r = requests.get(url, headers={"User-Agent": "Mozilla/5.0"}, timeout=10)
    soup = BeautifulSoup(r.text, "lxml")
    title = soup.find("title").get_text(strip=True) if soup.find("title") else None

    # Google Scholar search results
    if "scholar.google" in url:
        papers = []
        for card in soup.select("div.gs_ri"):
            t = card.select_one("h3.gs_rt")
            a = card.select_one("div.gs_rs")
            papers.append({
                "title": t.get_text(strip=True) if t else None,
                "abstract": a.get_text(strip=True) if a else None,
            })
        return {"title": title, "papers": papers}

    # arXiv
    if "arxiv.org" in url:
        abstract_el = soup.find("blockquote", class_="abstract")
        pdf_links = [urljoin(url, p) for p in re.findall(r'href="(/pdf/[^"]+\.pdf)"', r.text)]
        return {"title": title, "abstract": abstract_el.get_text(strip=True) if abstract_el else None, "pdf_links": pdf_links[:3]}

    # Nature.com (og meta)
    if "nature.com" in url:
        og_title = soup.find("meta", property="og:title")
        og_desc = soup.find("meta", property="og:description")
        citation_doi = soup.find("meta", attrs={"name": "citation_doi"})
        return {
            "title": og_title["content"] if og_title else title,
            "description": og_desc["content"] if og_desc else None,
            "doi": citation_doi["content"] if citation_doi else None,
        }

    # Generic fallback
    return {"title": title}


def extract_web_tier3(url: str, wait_time: int = 3) -> dict:
    """Tier 3: Camoufox browser extraction for JS-rendered pages.
    
    Migrated from playwright_stealth to Camoufox.
    Dependency: pip install camoufox && python -m camoufox fetch
    """
    from camoufox.sync_api import Camoufox

    with Camoufox(headless=True) as browser:
        page = browser.new_page()
        # ⚠️ Do NOT use networkidle (Nature/Springer will load indefinitely)
        page.goto(url, wait_until="domcontentloaded", timeout=30000)
        page.wait_for_timeout(wait_time * 1000)

        return {
            "url": page.url,
            "title": page.title(),
            "body": page.inner_text("body")[:2000],
            "html_len": len(page.content()),
        }
```

### 6. One-Stop Obtain Function

```python
import os


def obtain_paper(identifier: str, output_dir: str = "/tmp/papers", email: str = "user@example.com"):
    """
    One-stop paper retrieval:
    - Input: DOI / arXiv ID / PDF URL
    - Output: (status, filepath_or_url, metadata)
      status ∈ {"pdf", "url", "text", "unknown"}
    """
    os.makedirs(output_dir, exist_ok=True)

    # PDF direct link
    if identifier.endswith(".pdf"):
        fname = f"{output_dir}/{identifier.split('/')[-1]}"
        download_pdf(identifier, fname)
        return ("pdf", fname, {"source": "direct_link"})

    # DOI
    if identifier.startswith("10."):
        meta = resolve_doi(identifier)
        result = find_free_pdf(identifier, email)
        if result["free"] and result["pdf_url"]:
            fname = f"{output_dir}/{identifier.replace('/', '_')}.pdf"
            download_pdf(result["pdf_url"], fname)
            return ("pdf", fname, meta)
        return ("url", result.get("landing_url", meta["url"]), meta)

    # arXiv ID
    arxiv_clean = identifier.replace("arXiv:", "")
    if re.match(r"\d{4}\.\d{4,5}", arxiv_clean):
        pdf_url = f"https://arxiv.org/pdf/{arxiv_clean}.pdf"
        fname = f"{output_dir}/{arxiv_clean}.pdf"
        download_pdf(pdf_url, fname)
        return ("pdf", fname, {"id": arxiv_clean, "source": "arXiv"})

    return ("unknown", None, {"error": "Unrecognized format. Please provide a DOI / arXiv ID / PDF URL"})


# Usage examples
status, path, meta = obtain_paper("10.1038/nature12373")
print(f"Status: {status}, Path: {path}, Title: {meta.get('title', '?')[:50]}")

status, path, meta = obtain_paper("2301.00001")
print(f"Status: {status}, Path: {path}")
```

---

## Failure Fallbacks

| Scenario | Symptom | Fallback Strategy |
|----------|---------|-------------------|
| Unpaywall has no free version | `free: False` | Return landing page URL, prompt user to obtain manually |
| PDF download returns 403 | `raise_for_status` fails | ① Try downloading via Camoufox browser ② Switch source (PMC / Sci-Hub) |
| PDF is a scanned copy (image format) | PyMuPDF extracts empty text | Requires OCR (pytesseract / Tesseract), outside the scope of this pipeline |
| Web Tier 2 extraction returns empty | BeautifulSoup finds no match | Fall back to Tier 3: Camoufox browser rendering |
| Nature/Springer timeout | `networkidle` waits indefinitely | Use `domcontentloaded` event instead (see code comment) |
| Scholar IP ban | 429 error | ① Wait 60s ② Switch API (OpenAlex) ③ Camoufox + proxy |
| Major publishers fully block (Wiley/Elsevier) | Cannot download | Only metadata available via API; full text requires institutional access |

---

## Web Scraping Tier Auto-Selection

```python
import re


def auto_select_tier(url: str) -> tuple[int, str]:
    """Automatically select the optimal extraction tier."""
    url_lower = url.lower()

    if url_lower.endswith(".pdf"):
        return (0, "PDF direct link → curl download")
    if re.match(r"^(10\.\d{4,}|arXiv:)", url):
        return (1, "DOI/arXiv ID → API query")
    if "arxiv.org/abs" in url_lower:
        return (1, "arXiv paper page → web_read or API")
    if "scholar.google" in url_lower:
        return (2, "Scholar search → curl+BS")
    if "nature.com" in url_lower:
        return (2, "Nature → curl+BS (og meta)")
    if "springer.com" in url_lower:
        return (3, "Springer → Camoufox (requires session)")
    return (2, "Generic → curl+BS, fall back to Tier 3 on failure")
```

---

## Related Pipelines

- Paper discovery (from keywords/authors) → See [pipeline-discovery.md](pipeline-discovery.md)
- Citation network & trend analysis → See [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md)
- Format references → See [pipeline-citation-tracking.md](pipeline-citation-tracking.md)
- Comprehensive entry point: What information do I have, and which API should I use? → See [decision-tree.md](decision-tree.md)
