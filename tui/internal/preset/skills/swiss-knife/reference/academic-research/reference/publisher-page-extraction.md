# Publisher Page Extraction (Tier 5)

> **Most agents do not need this file.** `scripts/fetch_paper.py` invokes
> this tier automatically when tiers 1–4 (arXiv / Unpaywall / Europe PMC /
> CORE) all miss and the DOI prefix matches a supported publisher.
> Read this only if you need to invoke the extractor manually, debug a
> tier-5 miss, or understand its limits.

> ✅ **Status: in-house, self-contained (issue #136).** This tier no longer
> depends on any third-party package. The previous design relied on the
> upstream `zhiping0913/Download_paper` repo, which ships no
> `setup.py`/`pyproject.toml` and so could never be `pip install`ed — that
> path has been **removed**. The current extractor is pure stdlib + the
> `requests` library the skill already uses. There is nothing to install and
> no install that can fail.

---

## What it is

The Tier-5 extractor is a small set of helpers inside
`scripts/fetch_paper.py`. Given a DOI whose prefix is in the supported set,
it:

1. resolves the DOI to a landing-page URL (the CrossRef resolver URL if
   present, else `https://doi.org/{doi}`);
2. issues a single **unauthenticated** HTTP GET (descriptive User-Agent,
   no cookies, no auth headers);
3. refuses pages that look like a login / paywall interstitial;
4. parses `citation_*` / Dublin Core `<meta>` tags for title, authors,
   journal, abstract, and DOI;
5. extracts the article body with publisher-agnostic container heuristics
   (`<article>`, `article-body`/`fulltext`-class `<div>`s, `<main>`, …),
   stripping nav/script/footer boilerplate; and
6. writes `paper.md` with a header, the extracted abstract/body, and a
   **provenance + limitations** footer.

It is deliberately lightweight. It is **not** a headless-browser renderer
and does **not** preserve LaTeX, figures, or reference lists faithfully —
see *Limitations* below.

## Hard policy boundaries

These are enforced by the code, not just documentation:

- **Official pages only.** It fetches exactly the landing URL CrossRef/DOI
  gives it. It does not crawl, guess alternate hosts, or scrape mirrors.
- **No paywall or CAPTCHA bypass.** A page matching login/subscription/
  purchase markers (or a password field plus login wording) is treated as a
  clean miss — the extractor returns `None` and the ladder falls through to
  LibGen.
- **No cookies, sessions, or credentials.** No auth headers are ever sent.
  If your browser can't read the article without logging in, neither can
  this tier.
- **No near-empty artifacts.** If fewer than ~200 characters of body text
  are recoverable, the tier declines rather than writing a stub `paper.md`.

If you have institutional access, the way to use it is the same way you'd
read the paper: from a network/host where the publisher already serves the
HTML unauthenticated (e.g. on-campus IP). This tier captures that HTML; it
does not manufacture access.

## When this tier wins

Use only when **all** of the following are true:

1. The paper has no preprint or OA mirror (Unpaywall, Europe PMC, CORE,
   arXiv all returned no PDF).
2. The DOI prefix is in the supported set:

   | Prefix | Publisher |
   |--------|-----------|
   | 10.1038 | Nature / Springer |
   | 10.1103 | American Physical Society (PRL, PRD, PRX, …) |
   | 10.1063 | AIP Publishing (Phys. Plasmas, JCP, AIP Advances, …) |
   | 10.1088 | IOP Science (ApJ, ApJL, ApJS, JPhys, …) |
   | 10.1017 | Cambridge University Press |

3. The official article page is reachable **without logging in** from where
   the script runs (gold/hybrid OA, or you're on an institutionally-licensed
   network).

If any of these fail, prefer `tier_libgen` (last-resort PDF) or accept the
fetch failure and surface it to the user.

## Manual invocation

The script does this automatically — only run by hand when debugging. The
helpers live in `fetch_paper.py`, so import them from the scripts directory:

```python
import sys
sys.path.insert(0, "<skill-path>/scripts")
import fetch_paper

meta = {
    "doi": "10.1103/PhysRevLett.125.015001",
    "url": "https://link.aps.org/doi/10.1103/PhysRevLett.125.015001",
    "title": "…",            # optional; CrossRef metadata if you have it
}

from pathlib import Path
path = fetch_paper.tier_publisher_extract(meta, Path("./out"))
print(path)   # → out/paper.md, or None on a clean miss
```

For step-by-step debugging, the same module exposes lower-level helpers mirroring
the pipeline in *What it is*: `_publisher_landing_url(meta)`,
`_fetch_publisher_html(url)` (unauthenticated GET), `_looks_paywalled(html)`,
`_extract_meta_tags(html)`, `_extract_article_body(html)`, and
`_build_publisher_markdown(meta, html, url)`.

To stay inside the script's slug/manifest contract (idempotent re-runs,
molt-survivable `papers/` resume), prefer the normal entry point:

```bash
python3 <skill-path>/scripts/fetch_paper.py --batch dois.txt --out papers/
# or skip this tier entirely:
python3 <skill-path>/scripts/fetch_paper.py <id> --no-publisher-extract
```

## Output shape

When the tier hits, it writes a single Markdown file:

```
papers/{slug}/paper.md      # title, authors, abstract, extracted body,
                            # + provenance & limitations footer
```

and `fetch_paper.py` records `tier: publisher_extract` in
`papers/{slug}/manifest.json`. The Markdown always ends with a footer that
names the source URL, the extraction method, and the limitations, so any
downstream consumer can tell this is a heuristic landing-page copy.

## Limitations

This is a regex-based HTML extraction, **not** a typeset full text:

- **Equations** are taken as whatever inline text the page exposes — MathML
  / image-rendered math is lost or garbled.
- **Figures and tables** are dropped (only their surrounding text survives).
- **Reference lists** may be partially captured or missing.
- **Layout** (columns, sidebars, captions) is flattened to running text.

Treat `paper.md` as a convenience artifact for search/triage/quoting. For
anything load-bearing, consult the publisher page of record.

## Failure modes and recovery

| Symptom | Likely cause | Recovery |
|---------|--------------|----------|
| `tier_publisher_extract` returns `None`, page existed | Landing page looked like a login/paywall interstitial | Expected — no bypass is attempted. Provide an OA/institutionally-reachable page, or let the ladder fall through to LibGen |
| Returns `None`, very short body | Body container didn't match heuristics, or page is mostly an abstract gate | Inspect with `_extract_article_body(html)`; if the publisher uses an unusual container, add a pattern to `_BODY_PATTERNS` |
| Returns `None`, DOI prefix unsupported | No handler for that publisher | Skip this tier; `fetch_paper.py` checks the prefix before invoking |
| `requests` timeout / connection error | Network or publisher unreachable | Clean miss; retry later or fall through |
| Markdown body is garbled math | MathML / image equations | Known limitation — use the publisher page for equations |

When this tier misses, `fetch_paper.py` falls through to `tier_libgen`
(unless `--no-libgen` was passed). LibGen is a different surface (PDF, not
Markdown), so it often catches what landing-page extraction can't.

## Legal note

This tier extracts content from the publisher page your browser would
normally render at the same URL, using a plain unauthenticated GET. Whether
that is permitted depends on:

- your institutional subscription terms,
- the publisher's robots.txt and ToS, and
- local copyright law.

Use is the user's responsibility. The extractor does **not** bypass
authentication, paywalls, or CAPTCHAs — if you can't read the page in a
browser without logging in, this tier won't get it either.

## See also

- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — full manual ladder including this tier
- [libgen-fallback.md](libgen-fallback.md) — the next tier down when this one misses
- [error-handling.md](error-handling.md) — generic 403/429 patterns
