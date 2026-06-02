# Publisher Page Extraction (Tier 5)

> **Most agents do not need this file.** `scripts/fetch_paper.py` invokes
> this tier automatically when tiers 1–4 (arXiv / Unpaywall / Europe PMC /
> CORE) all miss and the DOI prefix matches a supported publisher.
> Read this only if you need to invoke the tool manually, debug a tier-5
> failure, or run it outside the script's slug/manifest contract.

---

## What it is

`zhiping0913/Download_paper` is an open-source, AI-friendly extractor that
opens a publisher's article page in headless Chromium, identifies the
article body, and produces structured **Markdown with LaTeX formulas
preserved**, plus high-resolution figures and supplementary materials.

This is qualitatively different from a generic PDF download:

- **PDF tier** (Unpaywall, CORE, arXiv) returns raw bytes; agents must then
  PyMuPDF-extract text and lose layout/equation fidelity.
- **Publisher-extract tier** returns Markdown that already has the section
  structure, citation list, and inline math intact. Better starting
  material for downstream tasks like literature review or LaTeX writing.

Repository: https://github.com/zhiping0913/Download_paper

## When this tier wins

Use only when **all** of the following are true:

1. The paper has no preprint or OA mirror (Unpaywall, Europe PMC, CORE, arXiv all returned no PDF).
2. The DOI prefix is in the supported set:

   | Prefix | Publisher |
   |--------|-----------|
   | 10.1038 | Nature / Springer |
   | 10.1103 | American Physical Society (PRL, PRD, PRX, …) |
   | 10.1063 | AIP Publishing (Phys. Plasmas, JCP, AIP Advances, …) |
   | 10.1088 | IOP Science (ApJ, ApJL, ApJS, JPhys, …) |
   | 10.1017 | Cambridge University Press |

3. You either have institutional access for the paywalled portion **or**
   the article is gold/hybrid OA on the publisher site.
4. The paper is math/formula-heavy and you want the equations preserved.

If any of these fail, prefer `tier_libgen` (last-resort PDF) or accept the
fetch failure and surface it to the user.

## Manual invocation

The script already does this automatically — only run by hand when
debugging.

### Install (one-time)

```bash
pip install git+https://github.com/zhiping0913/Download_paper

# System deps the extractor relies on:
playwright install chromium          # headless browser
# pandoc must be on $PATH             # formula conversion
# python-magic                        # auto-installed by the package
```

### Single DOI

```python
from download_paper import download_paper
md_path = download_paper("10.1103/PhysRevLett.125.015001")
print(md_path)  # → /path/to/output/PhysRevLett.125.015001.md
```

### Batch

```bash
# Their own CLI:
python -m download_paper.batch_process --input dois.txt --out papers/

# Or stay inside fetch_paper.py's contract:
python3 <skill-path>/scripts/fetch_paper.py --batch dois.txt --out papers/
```

The second form is preferred because it preserves the `manifest.json`
contract — re-runs are idempotent, and agents that survive a molt can
resume from `papers/` without re-fetching.

## Output shape

Default output (when invoked directly):

```
<output-dir>/
├── {DOI-slug}.md            # full article, sections preserved, LaTeX intact
├── {DOI-slug}/figures/      # high-res figures
├── {DOI-slug}/supp/         # supplementary materials
└── {DOI-slug}/metadata.json # title, authors, year, journal, doi
```

When called via `fetch_paper.py`, the Markdown is copied to
`papers/{slug}/paper.md` and the `manifest.json` records `tier:
publisher_extract`.

## Failure modes and recovery

| Symptom | Likely cause | Recovery |
|---------|--------------|----------|
| First call hangs ~30s then succeeds | Cold Chromium boot | Expected; subsequent calls are warm |
| `pip install` fails with playwright wheel error | Pinned Chromium for unsupported arch | Try `pip install playwright==1.46.* && playwright install chromium` separately |
| Anti-bot challenge (CAPTCHA / Cloudflare) | Publisher detected automation | Tool auto-switches to headed mode; needs `$DISPLAY` on Linux. Without a display, this tier will fail — fall through to LibGen |
| `pandoc: command not found` | pandoc missing from PATH | `brew install pandoc` (macOS), `apt install pandoc` (Linux) |
| Publisher returns 403 | No institutional access for subscription article | This tier cannot help — log the gap in `manifest.json` and fall through |
| DOI prefix not in supported set | No publisher handler implemented | Skip this tier; `fetch_paper.py` does this check before invoking |

When this tier fails, `fetch_paper.py` falls through to `tier_libgen`
(unless `--no-libgen` was passed). LibGen is qualitatively worse output
(PDF, not Markdown) but legally and technically a different surface, so
it often catches what publisher extraction misses.

## Legal note

This tool extracts content from publisher pages your browser would
normally render. Whether that is permitted depends on:

- Your institutional subscription terms
- The publisher's robots.txt and ToS
- Local copyright law

Use is the user's responsibility. The tool itself is open-source and does
not bypass authentication — if you don't have access to the paper in a
browser, this tier won't get it either.

## See also

- [pipeline-obtain-pdf.md](pipeline-obtain-pdf.md) — full manual ladder including this tier
- [libgen-fallback.md](libgen-fallback.md) — the next tier down when this one fails
- [error-handling.md](error-handling.md) — generic 403/429 patterns
