#!/usr/bin/env python3
"""
fetch_paper.py — academic-research skill, tier-1 entry point

Given a DOI, arXiv ID, or PMID, walk the open-access ladder and save the
paper full-text plus structured metadata into a stable on-disk layout.

Usage:
    python3 fetch_paper.py 10.1103/PhysRevLett.125.015001
    python3 fetch_paper.py arXiv:2301.00001
    python3 fetch_paper.py PMID:12345678
    python3 fetch_paper.py --batch dois.txt --out papers/
    python3 fetch_paper.py 10.1038/nature12373 --email me@example.com --no-libgen

Tier ladder (stops at first success):
    1. arXiv direct (preprints — fastest, free, no key)
    2. Unpaywall      (publisher-blessed OA PDFs)
    3. Europe PMC     (biomed full-text + arXiv mirror)
    4. CORE           (institutional repository aggregator)
    5. Publisher-page extraction (in-house, stdlib + requests) for
       10.1038 / 10.1103 / 10.1063 / 10.1088 / 10.1017 — fetches the already-
       accessible publisher article / DOI landing page and parses
       citation_* metadata + the article body into structured Markdown.
       No paywall/CAPTCHA bypass, no cookies/credentials: official pages
       only (issue #136). Opt out with --no-publisher-extract.
    5b. Authorized publisher (institutional/licensed access — official DOI
        landing page → same-host PDF → %PDF- validation; opt-out with
        --no-institutional). Never bypasses paywalls or handles credentials.
    6. LibGen         (last resort, opt-out with --no-libgen)

Output (per paper):
    papers/{slug}/
        paper.pdf | paper.md         # full-text artifact
        metadata.json                 # CrossRef-normalized metadata
        manifest.json                 # {status, tier, source, ts, doi}

The script is idempotent — re-running skips entries whose manifest.json
already reports status=ok. This is the contract the rest of the skill
relies on: agents that survive a molt can resume from `papers/` without
re-fetching anything.
"""

from __future__ import annotations

import argparse
import html as _html
import json
import os
import re
import sys
import time
from pathlib import Path
from typing import Optional
from urllib.parse import quote, urljoin, urlparse

import requests

# ──────────────────────────────────────────────────────────
#  Constants
# ──────────────────────────────────────────────────────────

UA = "lingtai-academic-research/3.0 (mailto:{email})"
DEFAULT_EMAIL = os.environ.get("LINGTAI_RESEARCH_EMAIL", "lingtai-agent@example.org")

PUBLISHER_EXTRACTABLE_PREFIXES = {
    "10.1038": "Nature / Springer",
    "10.1103": "American Physical Society",
    "10.1063": "AIP Publishing",
    "10.1088": "IOP Science",
    "10.1017": "Cambridge University Press",
}

# Tier-5 (publisher-page extraction) is an in-house, self-contained extractor:
# it fetches the publisher's article / DOI landing page with `requests` and
# parses citation_* metadata + the article body into structured Markdown using
# stdlib regex heuristics. It has NO third-party extraction dependency (the old
# `zhiping0913/Download_paper` path is gone — issue #136) and performs NO
# paywall/CAPTCHA bypass and NO cookie/credential handling. It only ever reads
# pages a plain unauthenticated GET can already see. A page that looks like a
# login/paywall interstitial is treated as a clean miss.
PUBLISHER_FETCH_UA = (
    "Mozilla/5.0 (compatible; lingtai-academic-research/3.0; "
    "+https://github.com/Lingtai-AI/lingtai)"
)

# Minimum extracted body length (chars) below which we consider the page to have
# yielded no usable full text and decline to write a near-empty artifact.
PUBLISHER_MIN_BODY_CHARS = 200

# Phrases that strongly indicate a login / paywall / purchase interstitial
# rather than a readable article. Matched case-insensitively against the page.
_PAYWALL_MARKERS = (
    "sign in to access",
    "sign in to continue",
    "log in to access",
    "purchase this article",
    "purchase pdf",
    "get access to this article",
    "subscribe to access",
    "requires a subscription",
    "access through your institution",
    "you do not have access",
    "buy this article",
)

ARXIV_ID_RE = re.compile(r"^(?:arXiv:)?(\d{4}\.\d{4,5})(v\d+)?$", re.IGNORECASE)
PMID_RE = re.compile(r"^(?:PMID:)?(\d{6,9})$", re.IGNORECASE)
DOI_RE = re.compile(r"^10\.\d{4,9}/\S+$")


# ──────────────────────────────────────────────────────────
#  Input parsing
# ──────────────────────────────────────────────────────────

def classify(identifier: str) -> tuple[str, str]:
    """Return (kind, normalized_id). kind ∈ {doi, arxiv, pmid}."""
    s = identifier.strip()
    s = s.replace("https://doi.org/", "").replace("http://doi.org/", "")
    if m := ARXIV_ID_RE.match(s):
        return "arxiv", m.group(1)
    if m := PMID_RE.match(s):
        return "pmid", m.group(1)
    if DOI_RE.match(s):
        return "doi", s
    raise ValueError(f"Unrecognized identifier: {identifier!r}")


def slugify(meta: dict, fallback: str) -> str:
    """First-author-year-firstword slug; fallback to identifier."""
    authors = meta.get("authors") or []
    year = meta.get("year") or "0000"
    title = meta.get("title") or ""
    first_author = "anon"
    if authors:
        last = authors[0].split()[-1] if authors[0] else "anon"
        first_author = re.sub(r"[^a-zA-Z0-9]", "", last).lower() or "anon"
    first_word = ""
    for tok in title.split():
        tok = re.sub(r"[^a-zA-Z0-9]", "", tok).lower()
        if tok and len(tok) > 2:
            first_word = tok
            break
    if first_author and year != "0000":
        slug = f"{first_author}-{year}"
        if first_word:
            slug += f"-{first_word}"
        return slug
    return re.sub(r"[^a-zA-Z0-9_-]", "_", fallback)[:60]


# ──────────────────────────────────────────────────────────
#  Metadata resolution
# ──────────────────────────────────────────────────────────

def resolve_metadata(kind: str, ident: str, email: str) -> dict:
    """Return a normalized metadata dict regardless of input kind.

    Shape: {title, authors[], year, journal, doi, arxiv_id?, pmid?, publisher?}
    """
    if kind == "doi":
        return _crossref(ident, email)
    if kind == "arxiv":
        return _arxiv_meta(ident)
    if kind == "pmid":
        return _europe_pmc_meta(ident)
    raise ValueError(kind)


def _crossref(doi: str, email: str) -> dict:
    r = requests.get(
        f"https://api.crossref.org/works/{doi}",
        headers={"User-Agent": UA.format(email=email)},
        timeout=15,
    )
    r.raise_for_status()
    d = r.json()["message"]
    pub_date = d.get("published-print") or d.get("published-online") or {}
    return {
        "title": (d.get("title") or [""])[0],
        "authors": [
            f"{a.get('given', '')} {a.get('family', '')}".strip()
            for a in d.get("author", [])
        ],
        "year": (pub_date.get("date-parts") or [[0]])[0][0],
        "journal": (d.get("container-title") or [""])[0],
        "doi": doi,
        "publisher": d.get("publisher", ""),
        "citations": d.get("is-referenced-by-count", 0),
        "url": d.get("URL", f"https://doi.org/{doi}"),
    }


def _arxiv_meta(arxiv_id: str) -> dict:
    r = requests.get(
        f"https://export.arxiv.org/api/query?id_list={arxiv_id}",
        timeout=15,
    )
    r.raise_for_status()
    # Minimal Atom parse — no feedparser dep
    text = r.text
    title = _xml_first(text, "title", skip=1) or arxiv_id
    summary = _xml_first(text, "summary") or ""
    year = 0
    if m := re.search(r"<published>(\d{4})", text):
        year = int(m.group(1))
    authors = re.findall(r"<name>([^<]+)</name>", text)
    doi_match = re.search(r"<arxiv:doi[^>]*>([^<]+)</arxiv:doi>", text)
    return {
        "title": title.strip(),
        "authors": authors,
        "year": year,
        "journal": "arXiv preprint",
        "doi": doi_match.group(1) if doi_match else "",
        "arxiv_id": arxiv_id,
        "abstract": summary.strip(),
        "url": f"https://arxiv.org/abs/{arxiv_id}",
    }


def _europe_pmc_meta(pmid: str) -> dict:
    r = requests.get(
        "https://www.ebi.ac.uk/europepmc/webservices/rest/search",
        params={"query": f"EXT_ID:{pmid}", "format": "json", "resultType": "core"},
        timeout=15,
    )
    r.raise_for_status()
    results = r.json().get("resultList", {}).get("result", [])
    if not results:
        raise LookupError(f"PMID {pmid} not found in Europe PMC")
    d = results[0]
    return {
        "title": d.get("title", ""),
        "authors": [a.strip() for a in (d.get("authorString") or "").split(",") if a.strip()],
        "year": int(d.get("pubYear", 0) or 0),
        "journal": d.get("journalTitle", ""),
        "doi": d.get("doi", ""),
        "pmid": pmid,
        "pmcid": d.get("pmcid", ""),
        "url": f"https://europepmc.org/abstract/MED/{pmid}",
    }


def _xml_first(text: str, tag: str, skip: int = 0) -> Optional[str]:
    matches = re.findall(rf"<{tag}[^>]*>([\s\S]*?)</{tag}>", text)
    if len(matches) > skip:
        return matches[skip]
    return None


# ──────────────────────────────────────────────────────────
#  Tier implementations
# ──────────────────────────────────────────────────────────

def tier_arxiv(meta: dict, out_dir: Path) -> Optional[Path]:
    """Try arXiv direct PDF (works for any arxiv_id, or DOIs that index arXiv)."""
    aid = meta.get("arxiv_id")
    if not aid:
        # Search arXiv by title as a low-cost guess for preprint-style DOIs
        if meta.get("title"):
            try:
                r = requests.get(
                    "https://export.arxiv.org/api/query",
                    params={"search_query": f'ti:"{meta["title"]}"', "max_results": 1},
                    timeout=15,
                )
                if m := re.search(r"arxiv\.org/abs/([0-9.]+)", r.text):
                    aid = m.group(1)
            except requests.RequestException:
                return None
        if not aid:
            return None
    url = f"https://arxiv.org/pdf/{aid}.pdf"
    return _download(url, out_dir / "paper.pdf")


def tier_unpaywall(meta: dict, email: str, out_dir: Path) -> Optional[Path]:
    doi = meta.get("doi")
    if not doi:
        return None
    try:
        r = requests.get(
            f"https://api.unpaywall.org/v2/{doi}",
            params={"email": email},
            timeout=15,
        )
        r.raise_for_status()
        d = r.json()
        loc = d.get("best_oa_location") or {}
        pdf_url = loc.get("url_for_pdf") or loc.get("url")
        if pdf_url:
            return _download(pdf_url, out_dir / "paper.pdf")
    except requests.RequestException:
        return None
    return None


def tier_europe_pmc(meta: dict, out_dir: Path) -> Optional[Path]:
    pmcid = meta.get("pmcid")
    if not pmcid and (doi := meta.get("doi")):
        try:
            r = requests.get(
                "https://www.ebi.ac.uk/europepmc/webservices/rest/search",
                params={"query": f"DOI:{doi}", "format": "json", "resultType": "lite"},
                timeout=15,
            )
            r.raise_for_status()
            for res in r.json().get("resultList", {}).get("result", []):
                if res.get("pmcid"):
                    pmcid = res["pmcid"]
                    break
        except requests.RequestException:
            return None
    if not pmcid:
        return None
    url = f"https://europepmc.org/articles/{pmcid}?pdf=render"
    return _download(url, out_dir / "paper.pdf")


def tier_core(meta: dict, out_dir: Path) -> Optional[Path]:
    """CORE search — requires CORE_API_KEY env var; skipped silently if absent."""
    key = os.environ.get("CORE_API_KEY")
    if not key:
        return None
    doi = meta.get("doi")
    if not doi:
        return None
    try:
        r = requests.get(
            "https://api.core.ac.uk/v3/search/works",
            params={"q": f"doi:{doi}", "limit": 1},
            headers={"Authorization": f"Bearer {key}"},
            timeout=15,
        )
        r.raise_for_status()
        for hit in r.json().get("results", []):
            if pdf := hit.get("downloadUrl"):
                return _download(pdf, out_dir / "paper.pdf")
    except requests.RequestException:
        return None
    return None


def tier_publisher_extract(meta: dict, out_dir: Path) -> Optional[Path]:
    """In-house publisher landing-page extractor (no third-party deps).

    Fetches the publisher's already-accessible article / DOI landing page and
    parses citation_* metadata + the article body into structured Markdown.
    Strictly best-effort and policy-bounded: official pages only, no paywall
    or CAPTCHA bypass, no cookie/credential handling. Returns the path to the
    written `paper.md` on success, or None (clean tier miss) otherwise — e.g.
    no DOI, an unsupported publisher prefix, a login/paywall interstitial, or
    a page with too little extractable text to be worth keeping.
    """
    doi = meta.get("doi")
    if not doi:
        return None
    prefix = doi.split("/")[0]
    if prefix not in PUBLISHER_EXTRACTABLE_PREFIXES:
        return None

    url = _publisher_landing_url(meta)
    if not url:
        return None

    html = _fetch_publisher_html(url)
    if not html:
        return None

    if _looks_paywalled(html):
        print("  [tier-5] landing page looks like a login/paywall page — "
              "skipping (no bypass attempted).", file=sys.stderr)
        return None

    body = _extract_article_body(html)
    if len(body.strip()) < PUBLISHER_MIN_BODY_CHARS:
        # Not enough readable full text — don't write a near-empty artifact.
        return None

    md = _build_publisher_markdown(meta, html, url)
    dst = out_dir / "paper.md"
    dst.parent.mkdir(parents=True, exist_ok=True)
    dst.write_text(md, encoding="utf-8")
    return dst


def _same_publisher_host(landing_host: str, candidate_host: str) -> bool:
    """True iff candidate_host is the landing host, its www. alias, or a
    subdomain of it. Sibling/parent registrable domains are rejected.

    Deliberately strict (see reference/authorized-publisher-access.md): a
    publisher's licensed PDF is virtually always on the same host that served
    the article page, so we accept only that host and things strictly under it.
    """
    h = (landing_host or "").lower().lstrip(".")
    c = (candidate_host or "").lower().lstrip(".")
    if not h or not c:
        return False
    if h.startswith("www."):
        h = h[4:]
    if c.startswith("www."):
        c = c[4:]
    return c == h or c.endswith("." + h)


def tier_authorized_publisher(meta: dict, email: str, out_dir: Path) -> Optional[Path]:
    """Tier 5b — authorized institutional publisher access.

    Conservative, provenance-heavy probe for paywalled-but-licensed PDFs: it
    follows the official DOI landing page, looks for a publisher-provided PDF
    URL on the *same host*, and saves it only after validating
    same-host/subdomain, Content-Type, and %PDF- magic bytes.

    It NEVER bypasses a paywall/CAPTCHA, captures/replays cookies or
    credentials, guesses sibling domains, or touches a shadow library. If the
    environment does not already have licensed access, this tier simply misses
    and the ladder falls through. See reference/authorized-publisher-access.md.
    """
    doi = meta.get("doi")
    if not doi:
        return None

    headers = {"User-Agent": UA.format(email=email)}
    try:
        landing = requests.get(
            f"https://doi.org/{doi}", headers=headers,
            timeout=30, allow_redirects=True,
        )
    except requests.RequestException as e:
        print(f"  [tier-5b] landing resolve failed: {e}", file=sys.stderr)
        return None
    if landing.status_code != 200:
        print(f"  [tier-5b] landing HTTP {landing.status_code}", file=sys.stderr)
        return None

    landing_url = landing.url
    landing_host = urlparse(landing_url).hostname or ""

    # 1) Prefer the Highwire/Scholar citation_pdf_url meta tag.
    pdf_url: Optional[str] = None
    if m := re.search(
        r'<meta[^>]+name=["\']citation_pdf_url["\'][^>]+content=["\']([^"\']+)["\']',
        landing.text, re.IGNORECASE,
    ):
        pdf_url = urljoin(landing_url, m.group(1))
    elif m := re.search(
        r'<meta[^>]+content=["\']([^"\']+)["\'][^>]+name=["\']citation_pdf_url["\']',
        landing.text, re.IGNORECASE,
    ):
        pdf_url = urljoin(landing_url, m.group(1))

    # 2) Else: a same-host .pdf anchor on the landing page.
    if not pdf_url:
        for href in re.findall(r'href=["\']([^"\']+\.pdf[^"\']*)["\']', landing.text, re.IGNORECASE):
            cand = urljoin(landing_url, href)
            if _same_publisher_host(landing_host, urlparse(cand).hostname or ""):
                pdf_url = cand
                break

    if not pdf_url:
        return None
    if not _same_publisher_host(landing_host, urlparse(pdf_url).hostname or ""):
        print("  [tier-5b] candidate PDF off official host — rejected", file=sys.stderr)
        return None

    # Validated download: status 200 + same host + application/pdf + %PDF- bytes.
    dst = out_dir / "paper.pdf"
    prov = {
        "doi": doi,
        "landing_url": landing_url,
        "pdf_url": pdf_url,
        "final_url": None,
        "http_status": None,
        "content_type": None,
        "bytes": None,
        "host_check": None,
    }
    try:
        with requests.get(pdf_url, headers=headers, stream=True,
                          timeout=60, allow_redirects=True) as r:
            prov["http_status"] = r.status_code
            prov["final_url"] = r.url
            prov["content_type"] = r.headers.get("content-type", "")
            if r.status_code != 200:
                print(f"  [tier-5b] PDF HTTP {r.status_code}", file=sys.stderr)
                return None
            final_host = urlparse(r.url).hostname or ""
            if not _same_publisher_host(landing_host, final_host):
                print("  [tier-5b] PDF redirected off official host — rejected", file=sys.stderr)
                return None
            prov["host_check"] = "same-host" if final_host.lstrip("www.") == landing_host.lstrip("www.") else "subdomain"
            if not prov["content_type"].lower().startswith("application/pdf"):
                print("  [tier-5b] not application/pdf — rejected", file=sys.stderr)
                return None
            chunks = r.iter_content(chunk_size=65536)
            first = next(chunks, b"")
            if not first.startswith(b"%PDF-"):
                print("  [tier-5b] missing %PDF- magic bytes — rejected", file=sys.stderr)
                return None
            total = len(first)
            dst.parent.mkdir(parents=True, exist_ok=True)
            tmp = dst.with_suffix(".pdf.part")
            with open(tmp, "wb") as f:
                f.write(first)
                for chunk in chunks:
                    if chunk:
                        total += len(chunk)
                        if total > 200_000_000:
                            tmp.unlink(missing_ok=True)
                            return None
                        f.write(chunk)
            if total < 1024:
                tmp.unlink(missing_ok=True)
                print("  [tier-5b] body too small — rejected", file=sys.stderr)
                return None
            tmp.replace(dst)  # atomic promote
            prov["bytes"] = total
    except requests.RequestException as e:
        print(f"  [tier-5b] PDF download failed: {e}", file=sys.stderr)
        return None

    # Stash provenance for the manifest. No cookies/auth headers are ever stored.
    meta["_authorized_provenance"] = prov
    return dst


def tier_libgen(meta: dict, out_dir: Path) -> Optional[Path]:
    """Last-resort LibGen lookup. See reference/libgen-fallback.md for design notes."""
    doi = meta.get("doi")
    if not doi:
        return None
    mirror = _libgen_mirror()
    if not mirror:
        return None
    try:
        # Title-based search — DOI endpoints have been flaky (see libgen-fallback.md)
        q = quote(meta.get("title", "") or doi)
        r = requests.get(f"{mirror}/scimag/?q={q}", timeout=20)
        r.raise_for_status()
        # Look for the first md5-keyed download link
        if m := re.search(r'href="(/scimag/[^"]*md5=[a-f0-9]{32}[^"]*)"', r.text):
            detail = requests.get(mirror + m.group(1), timeout=20)
            if dl := re.search(r'href="(https?://[^"]+\.pdf)"', detail.text):
                return _download(dl.group(1), out_dir / "paper.pdf")
    except requests.RequestException:
        return None
    return None


# ──────────────────────────────────────────────────────────
#  Helpers
# ──────────────────────────────────────────────────────────

def _download(url: str, dst: Path, max_bytes: int = 200_000_000) -> Optional[Path]:
    try:
        with requests.get(url, stream=True, timeout=30, allow_redirects=True) as r:
            r.raise_for_status()
            ctype = r.headers.get("content-type", "").lower()
            if "html" in ctype and not url.endswith(".pdf"):
                return None
            total = 0
            dst.parent.mkdir(parents=True, exist_ok=True)
            with open(dst, "wb") as f:
                for chunk in r.iter_content(chunk_size=65536):
                    if chunk:
                        total += len(chunk)
                        if total > max_bytes:
                            dst.unlink(missing_ok=True)
                            return None
                        f.write(chunk)
            if total < 1024:
                dst.unlink(missing_ok=True)
                return None
            return dst
    except requests.RequestException:
        return None


def _libgen_mirror() -> Optional[str]:
    for url in ["https://libgen.li", "https://libgen.is", "https://libgen.rs", "https://libgen.gs"]:
        try:
            if requests.head(url + "/", timeout=5, allow_redirects=True).status_code == 200:
                return url
        except requests.RequestException:
            continue
    return None


# ──────────────────────────────────────────────────────────
#  Tier-5 in-house publisher landing-page extraction helpers
#
#  All stdlib + requests. No headless browser, no cookies/credentials, no
#  paywall/CAPTCHA bypass. These parse the HTML a plain unauthenticated GET
#  already returns for an open / institutionally-reachable publisher page.
# ──────────────────────────────────────────────────────────

def _publisher_landing_url(meta: dict) -> Optional[str]:
    """Resolve a DOI/metadata dict to a landing-page URL to GET.

    Prefer the CrossRef-supplied resolver URL (already publisher-specific);
    fall back to the canonical https://doi.org/{doi} redirect. None when there
    is nothing to resolve.
    """
    url = meta.get("url")
    if url and url.startswith(("http://", "https://")):
        return url
    doi = meta.get("doi")
    if doi:
        return f"https://doi.org/{doi}"
    return None


def _fetch_publisher_html(url: str) -> Optional[str]:
    """GET a landing page and return its HTML, or None on any failure.

    Sends only a descriptive User-Agent — no cookies, no auth headers. HTML
    content-types only; a PDF or other binary response is not our job here.
    """
    try:
        r = requests.get(
            url,
            headers={"User-Agent": PUBLISHER_FETCH_UA},
            timeout=30,
            allow_redirects=True,
        )
        r.raise_for_status()
    except requests.RequestException:
        return None
    ctype = (r.headers.get("content-type", "") or "").lower()
    if "html" not in ctype and "xml" not in ctype:
        return None
    text = r.text or ""
    return text or None


def _looks_paywalled(html: str) -> bool:
    """Heuristic: does this page look like a login / paywall interstitial?

    We refuse to extract from such pages — both because the "content" is just
    a gate and because pretending to extract it would misrepresent access we
    do not have. Conservative: a couple of independent signals must agree, or
    one unambiguous purchase/subscription phrase must be present.
    """
    low = html.lower()
    phrase_hits = sum(1 for m in _PAYWALL_MARKERS if m in low)
    if phrase_hits:
        return True
    # Fall back to structural signals: a password field plus login wording.
    has_password = 'type="password"' in low or "type='password'" in low
    has_login_word = ("sign in" in low or "log in" in low or "login" in low)
    return has_password and has_login_word


def _strip_tags(fragment: str) -> str:
    """Collapse an HTML fragment to readable plain text (stdlib only)."""
    # Drop script/style wholesale.
    fragment = re.sub(r"(?is)<(script|style)[^>]*>.*?</\1>", " ", fragment)
    # Turn block-ish closers into newlines so paragraphs don't run together.
    fragment = re.sub(r"(?i)</(p|div|section|h[1-6]|li|tr|br)\s*>", "\n", fragment)
    fragment = re.sub(r"(?i)<br\s*/?>", "\n", fragment)
    # Remove remaining tags.
    fragment = re.sub(r"(?s)<[^>]+>", " ", fragment)
    fragment = _html.unescape(fragment)
    # Normalize whitespace, preserving paragraph breaks.
    lines = [re.sub(r"[ \t]+", " ", ln).strip() for ln in fragment.splitlines()]
    out: list[str] = []
    for ln in lines:
        if ln:
            out.append(ln)
        elif out and out[-1] != "":
            out.append("")
    return "\n".join(out).strip()


def _meta_content(html: str, *names: str, multi: bool = False):
    """Extract <meta name="..." content="..."> values, name-insensitive.

    Handles both attribute orders (name before content and vice versa) and
    single/double quotes. Returns a list when multi=True, else the first hit
    or "".
    """
    found: list[str] = []
    name_set = {n.lower() for n in names}
    meta_re = re.compile(r"(?is)<meta\b([^>]*?)/?>")
    attr_re = re.compile(
        r"""(?is)(name|property|content)\s*=\s*(?:"([^"]*)"|'([^']*)')"""
    )
    for m in meta_re.finditer(html):
        attrs = {}
        for a in attr_re.finditer(m.group(1)):
            key = a.group(1).lower()
            val = a.group(2) if a.group(2) is not None else (a.group(3) or "")
            attrs[key] = val
        tag_name = (attrs.get("name") or attrs.get("property") or "").lower()
        if tag_name in name_set and "content" in attrs:
            content = _html.unescape(attrs["content"]).strip()
            content = re.sub(r"\s+", " ", content)
            if content:
                found.append(content)
                if not multi:
                    break
    if multi:
        return found
    return found[0] if found else ""


def _extract_meta_tags(html: str) -> dict:
    """Parse common publisher citation_* / Dublin Core meta tags.

    Returns {title, authors[], journal, abstract, date, doi, pdf_url}. Missing
    fields come back empty so callers can fall back to CrossRef metadata.
    """
    return {
        "title": _meta_content(html, "citation_title", "dc.title", "dc.Title"),
        "authors": _meta_content(
            html, "citation_author", "dc.creator", "dc.Creator", multi=True
        ),
        "journal": _meta_content(
            html, "citation_journal_title", "prism.publicationName"
        ),
        "abstract": _meta_content(
            html, "dc.description", "dc.Description", "description",
            "citation_abstract", "og:description",
        ),
        "date": _meta_content(
            html, "citation_publication_date", "citation_date", "dc.date", "dc.Date"
        ),
        "doi": _meta_content(html, "citation_doi", "dc.identifier"),
        "pdf_url": _meta_content(html, "citation_pdf_url"),
        "abstract_url": _meta_content(html, "citation_abstract_html_url"),
    }


# Ordered (regex, group) candidates for locating the article body. First match
# with enough text wins. Kept publisher-agnostic and intentionally permissive.
_BODY_PATTERNS = (
    r'(?is)<article\b[^>]*>(.*?)</article>',
    r'(?is)<div\b[^>]*\b(?:id|class)\s*=\s*["\'][^"\']*(?:article-?body|article-?text|article-?fulltext|fulltext-?view|main-?content|c-article-body)[^"\']*["\'][^>]*>(.*?)</div>',
    r'(?is)<section\b[^>]*\b(?:id|class)\s*=\s*["\'][^"\']*(?:article|body|content)[^"\']*["\'][^>]*>(.*?)</section>',
    r'(?is)<main\b[^>]*>(.*?)</main>',
)


def _extract_article_body(html: str) -> str:
    """Best-effort plain-text extraction of the article body.

    Tries a sequence of publisher-agnostic container patterns and returns the
    first that yields a non-trivial amount of text. Navigation, scripts and
    footers are stripped by `_strip_tags`. Empty string when nothing matches.
    """
    best = ""
    for pat in _BODY_PATTERNS:
        for m in re.finditer(pat, html):
            text = _strip_tags(m.group(1))
            if len(text) > len(best):
                best = text
        if len(best) >= PUBLISHER_MIN_BODY_CHARS:
            return best
    return best


def _build_publisher_markdown(meta: dict, html: str, url: str) -> str:
    """Assemble a Markdown artifact with provenance + extracted content.

    Prefers CrossRef-resolved `meta` for the header (authoritative), but falls
    back to the page's own citation_* tags when a field is missing. Always
    records provenance and an explicit limitations note so downstream agents
    know this is a heuristic landing-page extraction, not a typeset full text.
    """
    tags = _extract_meta_tags(html)
    title = meta.get("title") or tags.get("title") or "(untitled)"
    authors = meta.get("authors") or tags.get("authors") or []
    journal = meta.get("journal") or tags.get("journal") or ""
    year = meta.get("year") or ""
    doi = meta.get("doi") or tags.get("doi") or ""
    abstract = tags.get("abstract") or meta.get("abstract") or ""
    body = _extract_article_body(html)

    lines = [f"# {title}", ""]
    if authors:
        lines += [", ".join(authors), ""]
    bib = []
    if journal:
        bib.append(journal)
    if year:
        bib.append(str(year))
    if bib:
        lines += ["*" + " · ".join(bib) + "*", ""]
    if doi:
        lines += [f"DOI: [{doi}](https://doi.org/{doi})", ""]
    if abstract:
        lines += ["## Abstract", "", abstract, ""]
    if body:
        lines += ["## Full text (extracted)", "", body, ""]

    lines += [
        "---",
        "",
        "## Provenance & limitations",
        "",
        f"- Source: in-house publisher-page extractor (Tier 5), {url}",
        "- Method: unauthenticated HTTP GET of the official publisher / DOI "
        "landing page, then stdlib regex parsing of `citation_*` metadata and "
        "the article body. No login, cookies, credentials, or paywall/CAPTCHA "
        "bypass were used.",
        "- **Limitations:** this is a heuristic HTML extraction, not a typeset "
        "full text. Equations, figures, tables, and reference lists may be "
        "lost or garbled. Treat as a convenience artifact; consult the "
        "publisher page of record for anything load-bearing.",
        "",
    ]
    return "\n".join(lines)


# ──────────────────────────────────────────────────────────
#  Orchestration
# ──────────────────────────────────────────────────────────

TIERS = [
    ("arxiv", tier_arxiv),
    ("unpaywall", tier_unpaywall),
    ("europe_pmc", tier_europe_pmc),
    ("core", tier_core),
    ("publisher_extract", tier_publisher_extract),
    ("authorized_publisher", tier_authorized_publisher),
    ("libgen", tier_libgen),
]


def fetch_one(identifier: str, out_root: Path, email: str, allow_libgen: bool = True,
              allow_institutional: bool = True,
              allow_publisher_extract: bool = True) -> dict:
    kind, ident = classify(identifier)
    meta = resolve_metadata(kind, ident, email)
    slug = slugify(meta, ident)
    out_dir = out_root / slug
    manifest_path = out_dir / "manifest.json"

    if manifest_path.exists():
        try:
            existing = json.loads(manifest_path.read_text())
            if existing.get("status") == "ok":
                print(f"  [skip] {slug} already fetched (tier={existing.get('tier')})")
                return existing
        except (json.JSONDecodeError, OSError):
            pass

    out_dir.mkdir(parents=True, exist_ok=True)
    (out_dir / "metadata.json").write_text(json.dumps(meta, indent=2, ensure_ascii=False))

    for name, fn in TIERS:
        if name == "libgen" and not allow_libgen:
            continue
        if name == "publisher_extract" and not allow_publisher_extract:
            continue
        if name == "authorized_publisher" and not allow_institutional:
            continue
        sig = fn.__code__.co_varnames[: fn.__code__.co_argcount]
        print(f"  [tier] {name}...", end=" ", flush=True)
        try:
            if "email" in sig:
                path = fn(meta, email, out_dir)
            else:
                path = fn(meta, out_dir)
        except Exception as e:
            print(f"error ({e})")
            continue
        if path:
            print(f"ok → {path.name}")
            manifest = {
                "status": "ok",
                "tier": name,
                "source": str(path.relative_to(out_dir)),
                "doi": meta.get("doi"),
                "arxiv_id": meta.get("arxiv_id"),
                "pmid": meta.get("pmid"),
                "title": meta.get("title"),
                "ts": int(time.time()),
            }
            if name == "authorized_publisher":
                manifest["route"] = "authorized_publisher"
                if prov := meta.get("_authorized_provenance"):
                    manifest["provenance"] = prov
            manifest_path.write_text(json.dumps(manifest, indent=2, ensure_ascii=False))
            return manifest
        print("miss")

    manifest = {
        "status": "fail",
        "reason": "all tiers exhausted",
        "doi": meta.get("doi"),
        "arxiv_id": meta.get("arxiv_id"),
        "pmid": meta.get("pmid"),
        "title": meta.get("title"),
        "ts": int(time.time()),
    }
    manifest_path.write_text(json.dumps(manifest, indent=2, ensure_ascii=False))
    return manifest


def main() -> int:
    p = argparse.ArgumentParser(description=__doc__.split("\n\n")[0])
    p.add_argument("identifier", nargs="?", help="DOI, arXiv ID, or PMID")
    p.add_argument("--batch", help="file with one identifier per line")
    p.add_argument("--out", default="papers", help="output directory (default: ./papers)")
    p.add_argument("--email", default=DEFAULT_EMAIL,
                   help="email for Unpaywall (use a real address; default from $LINGTAI_RESEARCH_EMAIL)")
    p.add_argument("--no-libgen", action="store_true", help="skip LibGen tier")
    p.add_argument("--no-publisher-extract", action="store_true",
                   help="skip Tier-5 in-house publisher landing-page extraction "
                        "(the official-page HTML→Markdown extractor for "
                        "Nature/APS/AIP/IOP/Cambridge DOIs).")
    p.add_argument("--no-institutional", action="store_true",
                   help="skip the authorized-publisher tier (Tier 5b); use in "
                        "legal-sensitive or off-network environments")
    p.add_argument("--dry-run", action="store_true", help="resolve metadata only, no PDF fetch")
    args = p.parse_args()

    if not args.identifier and not args.batch:
        p.error("provide an identifier or --batch FILE")

    if args.email == "lingtai-agent@example.org":
        print("warning: using placeholder email — set $LINGTAI_RESEARCH_EMAIL or pass --email "
              "for Unpaywall (placeholder addresses get 422'd).", file=sys.stderr)

    out_root = Path(args.out)
    identifiers: list[str] = []
    if args.batch:
        identifiers = [
            line.strip() for line in Path(args.batch).read_text().splitlines()
            if line.strip() and not line.startswith("#")
        ]
    if args.identifier:
        identifiers.append(args.identifier)

    if args.dry_run:
        for ident in identifiers:
            kind, normalized = classify(ident)
            meta = resolve_metadata(kind, normalized, args.email)
            print(json.dumps(meta, indent=2, ensure_ascii=False))
        return 0

    results: list[dict] = []
    for i, ident in enumerate(identifiers, 1):
        print(f"[{i}/{len(identifiers)}] {ident}")
        try:
            results.append(fetch_one(
                ident, out_root, args.email,
                allow_libgen=not args.no_libgen,
                allow_institutional=not args.no_institutional,
                allow_publisher_extract=not args.no_publisher_extract,
            ))
        except Exception as e:
            print(f"  ERROR: {e}", file=sys.stderr)
            results.append({"status": "error", "reason": str(e), "identifier": ident})

    ok = sum(1 for r in results if r.get("status") == "ok")
    fail = len(results) - ok
    print(f"\nDone: {ok} ok, {fail} fail. See {out_root}/")
    return 0 if fail == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
