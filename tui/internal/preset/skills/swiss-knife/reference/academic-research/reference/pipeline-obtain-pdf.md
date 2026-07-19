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
5. **(If OA channels fail, DOI is a supported publisher) Publisher-page extraction** — Nature/APS/AIP/IOP/Cambridge → structured Markdown with LaTeX preserved. See [publisher-page-extraction.md](publisher-page-extraction.md).
5b. **(If paywalled but you have licensed access) Authorized institutional publisher** — official DOI landing page → same-host publisher PDF → validate `%PDF-` bytes + Content-Type → save with provenance. No paywall bypass, no credential/cookie handling. See [authorized-publisher-access.md](authorized-publisher-access.md).
6a. **(If you have a batch + the user's Zotero) Zotero institutional full-text handoff** — agent stages the failed batch into Zotero Desktop with a dated tag, the **human** runs UI Find Full Text (institutional access), agent harvests resulting PDFs with provenance. Human-in-the-loop; no UI automation/TCC bypass, no credential handling. See [zotero-institutional-fulltext-handoff.md](zotero-institutional-fulltext-handoff.md).
6. **(If all of the above fail) LibGen fallback** — See [libgen-fallback.md](libgen-fallback.md) for live mirror discovery and download.
6. **(If web page, not PDF) Extract web page body** — Select BeautifulSoup or Camoufox based on the site.
7. **Extract text from PDF** — PyMuPDF text extraction.
8. **Output** — Return `(status, filepath_or_text, metadata)`.

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
│   │   └─ Not found → CORE → Europe PMC → arXiv → publisher-page extract (see publisher-page-extraction.md) → authorized institutional publisher (if licensed, see authorized-publisher-access.md) → Zotero institutional handoff (human-in-the-loop, batch, see zotero-institutional-fulltext-handoff.md) → LibGen (last resort, see libgen-fallback.md)
│   └─ OpenAlex supplementary metadata
│
├─ arXiv ID (e.g. 2301.00001)
│   ├─ arXiv API fetch metadata
│   └─ https://arxiv.org/pdf/{ID}.pdf download → Extract text
│
├─ Web page URL (nature.com / springer.com / scholar, etc.)
│   ├─ Tier 2: curl + BeautifulSoup (structured extraction, start here)
│   └─ Tier 3: Camoufox (JS rendering / login-required pages)
│
└─ Title / Keywords → Discover first, then obtain → See [pipeline-discovery.md](pipeline-discovery.md)
```

---

## Code Examples

> The bundled `scripts/fetch_paper.py` already implements this whole ladder — hand-code only
> when escaping it. Metadata/PDF-finding details live in the per-API references
> ([api-crossref.md](api-crossref.md), [api-unpaywall.md](api-unpaywall.md), [api-arxiv.md](api-arxiv.md)).

**Metadata + free PDF + download.** `resolve_doi` → CrossRef `/works/{doi}`
(`message`); `find_free_pdf` → Unpaywall (`is_oa` + `best_oa_location.pdf_url`,
else return `landing_url`); download by streaming `iter_content` to disk with a
`mailto` User-Agent. Extract text with PyMuPDF (`fitz.open(path)` →
`page.get_text()`); a scanned PDF yields empty text and needs OCR (out of scope).

**One-stop `obtain_paper(identifier)` → `(status, path_or_url, metadata)`**,
`status ∈ {pdf, url, text, unknown}`:
- ends `.pdf` → download directly
- starts `10.` → resolve_doi + find_free_pdf; download if free, else return landing URL
- matches `\d{4}\.\d{4,5}` (opt. `arXiv:` prefix) → `https://arxiv.org/pdf/{id}.pdf`

**Web-page extraction (when the input is a page, not a PDF)** — migrated off the
legacy `playwright_stealth` API to Camoufox:
- *Tier 2, curl + BeautifulSoup* (static): per-site selectors — Scholar `div.gs_ri`
  (`h3.gs_rt`/`div.gs_rs`), arXiv `blockquote.abstract` + `/pdf/*.pdf` links,
  Nature `<meta og:title/og:description/citation_doi>`.
- *Tier 3, Camoufox* (JS-rendered/login): `pip install camoufox && python -m
  camoufox fetch`; `page.goto(url, wait_until="domcontentloaded")` then read
  `inner_text("body")`. **Do NOT use `networkidle`** — Nature/Springer never go idle.

---

## Failure Fallbacks

| Scenario | Symptom | Fallback Strategy |
|----------|---------|-------------------|
| Unpaywall has no free version | `free: False` | Return landing page URL, prompt user to obtain manually |
| PDF download returns 403 | `raise_for_status` fails | ① Switch OA source (PMC, CORE, arXiv mirror) ② If you have licensed institutional access, use the authorized-publisher tier ([authorized-publisher-access.md](authorized-publisher-access.md)) — it does not defeat the 403, it uses access you already have |
| PDF is a scanned copy (image format) | PyMuPDF extracts empty text | Requires OCR (pytesseract / Tesseract), outside the scope of this pipeline |
| Web Tier 2 extraction returns empty | BeautifulSoup finds no match | Fall back to Tier 3: Camoufox browser rendering |
| Nature/Springer timeout | `networkidle` waits indefinitely | Use `domcontentloaded` event instead (see code comment) |
| Scholar IP ban | 429 error | ① Wait 60s ② Switch API (OpenAlex) ③ Camoufox + proxy |
| Major publishers fully block (Wiley/Elsevier) | Cannot download anonymously | If you have **licensed institutional access** (campus/library IP), try the authorized-publisher tier — see [authorized-publisher-access.md](authorized-publisher-access.md). Otherwise only metadata is available via API. |
| OA fails but you're on a licensed network | Paywalled but subscribed | Authorized institutional publisher tier (5b) — official landing → same-host PDF → `%PDF-` validation, full provenance. See [authorized-publisher-access.md](authorized-publisher-access.md). No paywall bypass, no credential handling. |
| All OA channels exhausted | Unpaywall + CORE + Europe PMC + arXiv all fail | LibGen fallback — see [libgen-fallback.md](libgen-fallback.md) for live mirror discovery (last resort; legal status varies by jurisdiction) |

---

## Web Scraping Tier Auto-Selection

Pick the extraction tier by URL shape: `.pdf` → curl download; DOI/arXiv-ID →
API query; `scholar.google` and `nature.com` → Tier 2 (curl+BS, Nature via og
meta); `springer.com` → Tier 3 (Camoufox, session-gated); anything else → Tier 2,
falling back to Tier 3 on failure.

---

## Related Pipelines

- Paper discovery (from keywords/authors) → See [pipeline-discovery.md](pipeline-discovery.md)
- Citation network & trend analysis → See [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md)
- Format references → See [pipeline-citation-tracking.md](pipeline-citation-tracking.md)
- Authorized institutional publisher access (Tier 5b) → See [authorized-publisher-access.md](authorized-publisher-access.md)
- Zotero institutional full-text handoff, human-in-the-loop (Tier 6a) → See [zotero-institutional-fulltext-handoff.md](zotero-institutional-fulltext-handoff.md)
- Comprehensive entry point: What information do I have, and which API should I use? → See [decision-tree.md](decision-tree.md)
