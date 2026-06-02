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
    5. Publisher-page extraction (zhiping0913/Download_paper) for
       10.1038 / 10.1103 / 10.1063 / 10.1088 / 10.1017
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
import json
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import Optional
from urllib.parse import quote

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
    """Use zhiping0913/Download_paper for publisher-page extraction.

    Lazy-installed on first invocation. Produces Markdown with preserved
    LaTeX formulas — better than PDF-to-text for math-heavy papers.
    """
    doi = meta.get("doi")
    if not doi:
        return None
    prefix = doi.split("/")[0]
    if prefix not in PUBLISHER_EXTRACTABLE_PREFIXES:
        return None

    if not _ensure_download_paper():
        return None

    try:
        from download_paper import download_paper  # type: ignore
    except ImportError:
        return None

    try:
        md_path = download_paper(doi)  # returns str path to .md
    except Exception as e:
        print(f"  [tier-5] Download_paper failed: {e}", file=sys.stderr)
        return None

    if md_path and Path(md_path).exists():
        dst = out_dir / "paper.md"
        shutil.copy(md_path, dst)
        return dst
    return None


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


def _ensure_download_paper() -> bool:
    """Lazy install zhiping0913/Download_paper on first use. Returns True if importable."""
    try:
        import download_paper  # type: ignore
        return True
    except ImportError:
        pass
    print("  [tier-5] installing publisher-extraction tool (Download_paper)...", file=sys.stderr)
    try:
        subprocess.check_call(
            [sys.executable, "-m", "pip", "install", "--quiet",
             "git+https://github.com/zhiping0913/Download_paper"],
            timeout=180,
        )
        try:
            import download_paper  # type: ignore  # noqa: F401
            return True
        except ImportError:
            return False
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as e:
        print(f"  [tier-5] install failed: {e}", file=sys.stderr)
        return False


# ──────────────────────────────────────────────────────────
#  Orchestration
# ──────────────────────────────────────────────────────────

TIERS = [
    ("arxiv", tier_arxiv),
    ("unpaywall", tier_unpaywall),
    ("europe_pmc", tier_europe_pmc),
    ("core", tier_core),
    ("publisher_extract", tier_publisher_extract),
    ("libgen", tier_libgen),
]


def fetch_one(identifier: str, out_root: Path, email: str, allow_libgen: bool = True) -> dict:
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
            results.append(fetch_one(ident, out_root, args.email, allow_libgen=not args.no_libgen))
        except Exception as e:
            print(f"  ERROR: {e}", file=sys.stderr)
            results.append({"status": "error", "reason": str(e), "identifier": ident})

    ok = sum(1 for r in results if r.get("status") == "ok")
    fail = len(results) - ok
    print(f"\nDone: {ok} ok, {fail} fail. See {out_root}/")
    return 0 if fail == 0 else 1


if __name__ == "__main__":
    sys.exit(main())
