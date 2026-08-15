# Semantic Scholar Graph API — Batch Citation-Graph Construction

Build the full reference (outgoing-edge) graph of a large paper corpus with a
handful of no-key requests. The **batch endpoint** resolves hundreds of papers
per call and returns each paper's complete reference list together with the
ArXiv ID and DOI of every cited work — enough to match in-corpus edges exactly,
without parsing PDFs.

This is the standard free, no-key approach for citation-graph construction on
arXiv-scale corpora.

## When to use this

- You have a **large list of paper IDs** (hundreds to tens of thousands, e.g. an
  arXiv corpus) and need the reference list of every paper in it.
- You need **exact in-corpus citation edges**: each reference carries
  `externalIds` (ArXiv + DOI), so an edge `A -> B` is exact when B's ID appears
  in A's references — no fuzzy title matching.
- You want to **avoid PDF reference extraction** (GROBID etc.) as the primary
  source of reference lists.

> **Caution — PDF-only reference extraction can severely undercount.** In one
> real corpus of 13,417 arXiv papers, GROBID extraction from PDFs left **3,754
> papers with zero references** and many others with only 1–5 references out of
> the 26–61 actually present in the paper. Treat GROBID-derived reference counts
> as a lower bound, and prefer the Semantic Scholar batch endpoint (or arXiv
> e-print sources, below) for citation-graph construction.

## Endpoint

| Property | Description |
|---|---|
| Endpoint | `POST https://api.semanticscholar.org/graph/v1/paper/batch` |
| Method | POST (IDs go in the JSON request body, not the URL) |
| Max IDs per request | **500** (API maximum) |
| Recommended batch size | **50–100** — responses with nested `references` get large; keep batches small enough to retry cheaply |
| Authentication | None required (no key); see Rate Limits |
| Content-Type | `application/json` |

**Request body** — `ids` accepts any Semantic Scholar ID form (S2 `paperId`,
`DOI:...`, `arXiv:...`, PMID, ACL, URL):

```json
{
  "ids": [
    "arXiv:2401.00096",
    "arXiv:2305.14325v2",
    "DOI:10.1103/PhysRevLett.125.015001"
  ]
}
```

- **ArXiv IDs**: use the `arXiv:NNNN.NNNNN` form. **Strip version suffixes** —
  `arXiv:2305.14325v2` → `arXiv:2305.14325` — so both sides of the match are
  normalized (the API accepts suffixed IDs, but exact in-corpus matching needs a
  canonical form on both sides).
- **Response order matches request order**; an entry is `null` when the ID was
  not found or the paper is not in S2.

**Fields to request**:

```
fields=title,externalIds,references.paperId,references.title,references.externalIds
```

- `externalIds` on the paper and on each reference contains the `ArXiv` and
  `DOI` (and other) identifiers — this is what enables exact in-corpus edge
  matching.
- `references[]` is the paper's complete outgoing reference list (equivalent to
  `GET /paper/{id}/references`, but for hundreds of papers in one call).

## Sample response

```json
[
  {
    "paperId": "abc123...",
    "title": "...",
    "externalIds": {"ArXiv": "2401.00096", "DOI": "10.xxxx/yyyy"},
    "references": [
      {
        "paperId": "def456...",
        "title": "...",
        "externalIds": {"ArXiv": "2305.14325", "DOI": "10.zzzz/wwww"}
      }
    ]
  },
  null
]
```

## Rate limits (no key)

| Scenario | Quota | Notes |
|---|---|---|
| No API key | ~100 requests/day/IP | Each batch POST counts as one request |
| Practical throughput | ~1 req/s | Space requests ~1s apart |
| Free API key | 1000 req/day | More headroom for very large corpora |
| Rate-limited | HTTP 429 | Back off exponentially; honor `Retry-After` if present |

- A 13,417-paper corpus at 100 IDs/request is ~135 requests — more than one
  day's no-key budget. Spread the work across days, use a free API key, or lower
  the batch size. At 50–100 IDs per request and ~1 req/s you stay inside the
  per-second limit; the **daily cap is the binding constraint**.
- If 429s persist even at ~1 req/s, slow down to the more conservative ~12s
  cadence documented in [api-semantic-scholar.md](api-semantic-scholar.md).
- See [api-semantic-scholar.md](api-semantic-scholar.md) for the single-paper
  endpoints and [error-handling.md](error-handling.md) for the shared retry
  helper.

## Python example — fetch references and build in-corpus edges

```python
import time
import requests

BASE = "https://api.semanticscholar.org/graph/v1"
FIELDS = "title,externalIds,references.paperId,references.title,references.externalIds"
BATCH = 100          # 50-100 recommended; API max is 500
SLEEP = 1.0          # ~1 req/s practical no-key throughput


def normalize_arxiv(raw):
    """'arXiv:2305.14325v2' or '2305.14325v2' -> 'arXiv:2305.14325'."""
    s = str(raw).strip()
    if s.lower().startswith("arxiv:"):
        s = s[6:]
    if s.rsplit("v", 1)[-1].isdigit():   # strip version suffix (v1, v2, ...)
        s = s.rsplit("v", 1)[0]
    return f"arXiv:{s}"


def fetch_batch(ids, retries=5):
    """POST one batch. Returns {arxiv_id_or_input_id: paper_or_None}."""
    out = {}
    for attempt in range(retries):
        r = requests.post(
            f"{BASE}/paper/batch",
            params={"fields": FIELDS},
            json={"ids": ids},
            timeout=60,
        )
        if r.status_code == 200:
            for pid, paper in zip(ids, r.json()):
                ext = (paper or {}).get("externalIds") or {}
                key = normalize_arxiv(ext["ArXiv"]) if ext.get("ArXiv") else pid
                out[key] = paper
            return out
        if r.status_code == 429:           # rate limited: exponential backoff
            wait = float(r.headers.get("Retry-After", 2 ** attempt))
            time.sleep(min(wait, 60))
            continue
        r.raise_for_status()
    return out


def build_edges(corpus_arxiv_ids):
    """corpus_arxiv_ids: normalized 'arXiv:xxxx.xxxxx' IDs for the whole corpus.
    Returns (edges, refs_by_paper): edges are (citing, cited) pairs matched
    exactly by ArXiv ID; refs_by_paper maps paper -> [(arxiv_or_doi, title)]"""
    corpus_set = set(corpus_arxiv_ids)
    edges, refs_by_paper = [], {}
    for i in range(0, len(corpus_arxiv_ids), BATCH):
        chunk = corpus_arxiv_ids[i:i + BATCH]
        for arx, paper in fetch_batch(chunk).items():
            if not paper:
                continue
            refs = []
            for ref in paper.get("references", []):
                ext = ref.get("externalIds") or {}
                if ext.get("ArXiv"):
                    rid = normalize_arxiv(ext["ArXiv"])       # primary key
                elif ext.get("DOI"):
                    rid = "DOI:" + ext["DOI"]                 # fallback key
                else:
                    continue
                refs.append((rid, ref.get("title")))
                if rid in corpus_set:                          # exact edge
                    edges.append((arx, rid))
            refs_by_paper[arx] = refs
        time.sleep(SLEEP)
    return edges, refs_by_paper
```

Notes on the example:

- Normalize **both sides** of the match by stripping `vN` version suffixes.
- Match primarily on `ArXiv`; fall back to `DOI:` when a reference has no ArXiv
  ID (DOI matching is exact too).
- `references.paperId` is the S2 hash — a stable key across runs, but
  `externalIds` is what enables exact in-corpus matching without S2-only data.
- The example is deliberately dependency-light (`requests` only), matching the
  rest of this skill.

## Alternative ground truth: arXiv e-print source

For a paper that is on arXiv, the **LaTeX source is the ground truth** for its
reference list:

```
https://export.arxiv.org/e-print/<arxiv-id>     # e.g. https://export.arxiv.org/e-print/2401.00096
```

- Downloading this URL returns the submitted source archive (`.tar.gz`, `.gz`,
  or a plain `.tex`). Extract and parse the generated bibliography:
  - `\bibitem` entries in a hand-written `thebibliography` environment
  - the `.bbl` file (BibTeX output) when the paper uses BibTeX
- `\bibitem`/`.bbl` entries give the exact citation strings — the closest thing
  to the author's intended reference list. Use this to **audit Semantic
  Scholar's coverage** (S2 occasionally misses references too, though far less
  often than GROBID), or as the primary source for LaTeX-authored corpora.
- Cost: one download + parse per paper instead of one batch request per 100 —
  use it for auditing or for papers S2 cannot resolve.

## Fallbacks

| Failure | Fallback |
|---|---|
| ID not found (null entry) | Retry the ID alone; try the `DOI:` form; drop it or mark unverified |
| 429 rate limit | Exponential backoff (see [error-handling.md](error-handling.md)); honor `Retry-After`; spread work across days |
| S2 missing a paper | arXiv e-print source (above) for the ground-truth reference list |
| Need forward citations too | `GET /paper/{id}/citations` (single-paper; see [api-semantic-scholar.md](api-semantic-scholar.md)) or OpenAlex `filter=cites:{id}` |
