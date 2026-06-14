# Evidence-verification gate for academic writing

> Before you draft any **citation-bearing** academic artifact — a paper, a
> related-work section, a literature review, a References list, a
> submission-ready manuscript — verify the sources **first**. No verified
> evidence → no confident prose. Preprints and peer-reviewed papers must **not**
> share the same evidence layer. Search-result summaries are **leads, not
> citations**. If the user asks you to go fast, reduce *scope*, never *verification*.

This is the writing-side gate that wraps [academic-research](../SKILL.md). The
research skill can *find and fetch* papers; this gate decides **whether you have
earned the right to write them up as evidence yet.**

---

## Why this gate exists (the failure mode)

LLMs drafting under time pressure tend to:

1. over-amplify a soft user preference ("fast", "readable draft") into a hard
   constraint that justifies skipping verification;
2. prioritize fluent prose generation over source verification;
3. **flatten** sources of different reliability into one even, confident tone;
4. hide uncertainty behind polished writing unless forced to preserve provenance.

The observed incident: asked for a "fast" HCI paper, an agent produced a readable
draft and a literature matrix that mixed DOI-verified journal/conference papers,
preprints, future-year/online-first items, and search-result leads **at the same
evidence level**. The prose was not fabricated, but the layers were flattened so
a non-expert would think every reference was submission-ready. That invites
reviewer rejection and far more rework than verifying first. The user caught it —
the system should have caught it before the user had to.

The danger is not only hallucinated citations. It is that the safe workflow is
**advisory**: in exactly the moments that need it most (speed, submission
pressure, "just give me a readable draft"), the model slides from reference
discovery straight into confident prose without preserving where each fact came
from. This gate makes the workflow a **step you pass**, not a tool you might
remember to use.

---

## Does the gate apply? (detection)

If you are about to write **author–year citations or a References section**, the
gate applies. Full stop — that is the catch-all heuristic. Beyond it:

**High-confidence triggers** (gate applies, verify before drafting):

> `paper` · `论文` · `journal` · `期刊` · `conference` · `会议论文` · `workshop` ·
> `投稿` · `submission` · `references` · `参考文献` · `related work` ·
> `literature review` · `综述` · `DOI` · `citation` · `published papers` ·
> `审稿` · `manuscript`

**Human-pushback triggers** (the user is already worried — verify, don't placate):

> `幻觉` ("hallucination") · `别糊弄我` ("don't fob me off") · `DOI 查得到吗`
> ("can the DOI actually be found?") · `有没有已发表论文` ("are there *published*
> papers?") · `参考别人怎么写` ("model it on how others write") · `审稿打回`
> ("reviewer rejection") · `don't hallucinate`

**Medium-confidence — ask, don't guess:** if it is unclear whether the writing is
citation-bearing, ask once:

> "Is this meant to be citation-bearing academic writing? If yes, I should verify
> sources before drafting."

Prefer this medium-confidence confirmation over **hard blocking** — it avoids
false positives on non-academic writing.

**Hard-no (gate does NOT apply):** ordinary code work, daily ops, end-user
consultation, casual teaching material — anything without citation-bearing
claims. Do not warn on every text file; that is guard fatigue.

---

## The gate: verified literature matrix BEFORE draft

Before any submission-like draft, produce a **verified literature matrix**. One
row per source. Use [academic-research](../SKILL.md) to fetch and verify —
`fetch_paper.py <id>`, then the [DOI resolver](api-doi-resolver.md) /
[CrossRef](api-crossref.md) / [OpenAlex](api-openalex.md) /
[Unpaywall](api-unpaywall.md) / [Europe PMC](api-europe-pmc.md) cards as needed.

### Minimum row schema

```text
title:
authors:
year:
venue:
identifier:            # DOI / arXiv / PMID / publisher URL
verified_by:           # Crossref / arXiv / PubMed / publisher / OpenAlex / etc.
evidence_class:        # peer-reviewed | conference paper | preprint | posted content | search lead
claim_it_can_support:
claim_it_cannot_support:
notes:
```

`claim_it_can_support` / `claim_it_cannot_support` are the rows that stop
overreach: they force you to state, per source, the precise sentence it backs and
the sentence it does **not**.

### Evidence tiers — keep them separate in the prose

| Tier | evidence_class | What it may do in the draft |
|------|----------------|-----------------------------|
| **A** | peer-reviewed / conference paper (DOI- or venue-verified) | May support **main claims** as a formal citation. |
| **B** | preprint / posted content (arXiv, bioRxiv, SSRN, online-first) | Must be **labeled preliminary** ("preprint", "not peer-reviewed"). Never silently A-tier. |
| **C** | search lead / news / blog / unverified result | **Lead only.** May guide where to look; is **not** evidentiary support and **never** appears as a formal citation. |

`verified_by: none` must **never** silently become A-tier. A future-year or
online-first item is B at best until verified.

---

## When the user says "go fast"

Reduce **scope**, not **verification**. Concretely, say something like:

> "I can make a short, conservative draft quickly — but I won't treat unverified
> references as formal citations. I'll write firmly only what the verified A-tier
> sources support, mark preprints as preliminary, and list the rest as leads to
> chase."

Fast = fewer claims, narrower section, smaller reference set — all fully
verified. Fast ≠ a complete-looking draft whose citations are unchecked.

Partial matrices are allowed under latency pressure — but mark incomplete rows
clearly (`verified_by: pending`), and do not let a pending row support a strong
claim.

---

## Artifact convention (matrix before draft)

For submission-oriented academic tasks, write the matrix file first:

```
*_literature_matrix_verified_*.md      ← create this FIRST
*_paper_draft_*.md                     ← only after the matrix exists
```

Put the **verified-matrix path in the draft header** so a future agent (after a
molt / context loss) extends a clean draft from the matrix instead of adding
fresh unverified citations:

```markdown
<!-- evidence: see 2026-06-08_literature_matrix_verified.md — A-tier rows only as hard citations -->
```

---

## Pre-draft lint (run before declaring a draft submission-ready)

- [ ] A **verified literature matrix** exists and every cited source has a row.
- [ ] Every formal (author–year / numbered) citation maps to an **A-tier** row
      with a real `identifier` and a non-`none` `verified_by`.
- [ ] Every **preprint / posted-content** mention is labeled preliminary in the
      prose, not blended into peer-reviewed claims.
- [ ] No **search lead / news / blog** source appears as a formal citation.
- [ ] No strong claim rests on a source whose matrix row is missing or `pending`.
- [ ] No `verified_by: none` row is doing A-tier work.
- [ ] Each headline claim points to a specific row's `claim_it_can_support`; none
      contradicts that row's `claim_it_cannot_support`.
- [ ] The draft header records the verified-matrix path.

If any box is unchecked, you are presenting unverified references as
submission-ready. Verify (or downgrade the claim) before shipping.

---

## Caveats and false positives to design around

- **Verification latency** — allow partial matrices, but mark incomplete rows
  (`verified_by: pending`); never let a pending row carry a strong claim.
- **Non-English venues, books, dissertations** — Crossref may have no record;
  accept publisher/library verification, but `verified_by: none` must never
  silently become A-tier evidence.
- **False positives on non-academic writing** — use the medium-confidence
  question above, not a hard block.
- **False negatives when the user avoids academic vocabulary** — the
  author–year / References-section heuristic still catches it.
- **Guard fatigue** — warn only for submission-like drafts or citation-bearing
  claims, not every text file.
- **Molt / context loss** — the matrix-path-in-header convention keeps a future
  agent from extending a clean draft with new unverified citations.

---

## Related

- [SKILL.md](../SKILL.md) — the research skill this gate routes into for fetching
  and verifying sources (`fetch_paper.py`, the API cards).
- [decision-tree.md](decision-tree.md) — "I have X — which API verifies it?"
- [api-crossref.md](api-crossref.md), [api-doi-resolver.md](api-doi-resolver.md),
  [api-openalex.md](api-openalex.md), [api-unpaywall.md](api-unpaywall.md),
  [api-europe-pmc.md](api-europe-pmc.md) — the verification sources behind
  `verified_by`.
- [pipeline-latex-writing.md](pipeline-latex-writing.md) — the writing workflow
  this gate precedes; do not enter it until the matrix exists.
- [anti-pattern-text-consistency-vs-data-correspondence.md](anti-pattern-text-consistency-vs-data-correspondence.md) —
  the sibling guard for **empirical** writing (prose drifting from your own
  data). This gate guards **citation** evidence; that one guards **experimental**
  evidence. Both forbid confident prose ahead of verified provenance.
