# Anti-pattern: Text-consistency drift vs. data correspondence

> When writing an **empirical** paper iteratively — especially with reviewer
> agents in the loop — prose can become more polished, more internally
> consistent, and *wrong*. Each editing round optimizes the text against itself
> instead of against the data on disk. The result reads cleanly and survives
> reviewer agreement, but no longer describes what the experiments actually did.

This is the academic-writing cousin of the multi-agent echo chamber: agreement
among writers/reviewers measures **text consistency**, not **data correspondence**.

---

## Trigger pattern

Watch for any of these during a writing session:

- You are describing experiments, results, or methods from **memory or from an
  earlier draft**, not from the data files and runner code in front of you.
- A reviewer (human or agent) flags a section as "confusing" or "inconsistent,"
  and your fix is to **rewrite the prose** until it reads smoothly again — without
  re-opening the data.
- The framing of an experiment has been **restructured several rounds in a row**
  (renamed, re-numbered, re-grouped) and each round only touched `.tex`/`.md`,
  never the `experiments/` tree.
- Multiple reviewer agents **agree** the draft is consistent, and you treat that
  agreement as evidence the *claims* are correct.
- You cannot, right now, point to the exact directory / file / column that backs
  the sentence you just wrote.

If you notice any of these, you are at risk of drift. Stop polishing and
re-anchor to data (below) before writing another paragraph.

The original incident: over 9+ reviewer rounds an "Experiment 2" was described as
a single-agent-with-tools setup. The actual Exp 2 data lived in three sibling
directories (a chat/tool/replaced split, 21 model pairs, **no tools**). The text
was self-consistent and reviewer-approved at every round — and described an
experiment that was never run.

---

## What to do instead — establish ground-truth correspondence FIRST

Before writing or revising any sentence that makes an empirical claim, anchor it
to artifacts on disk. Do this *before* prose, and again whenever framing changes.

1. **List the data.** Enumerate the experiment directories and result files, and
   write down which paper section each one backs.
   ```bash
   # What experiments actually exist on disk?
   ls -d experiments/*/ results/*/ runs/*/ 2>/dev/null
   # How many runs / pairs / trials per condition?
   find experiments -name '*.json' -o -name '*.csv' -o -name '*.parquet' | sort
   ```
2. **Inspect the result shape.** Open one real result file and confirm the
   columns/keys, the units, and the count — don't infer them from the draft.
   ```bash
   head -c 2000 experiments/<condition>/results.json   # peek at one sample
   # row / record count that a "N pairs" or "N trials" claim must match
   wc -l experiments/<condition>/*.csv
   ```
3. **Read the runner code.** The script that produced the data is the source of
   truth for *what the experiment was* — which conditions, whether tools were
   enabled, how many agents, what was held fixed.
   ```bash
   ls experiments/<condition>/        # config / run script / logs
   ```
4. **Build a claim → evidence map.** For each numbered experiment and each
   headline number in the abstract/results, record the backing path. Keep it
   next to the draft (a table, a comment block, a `claims.md`). Every later edit
   must keep this map true.

   | Paper claim | Backing artifact | Verified value |
   |-------------|------------------|----------------|
   | "Exp 2: 21 model pairs, no tools" | `experiments/<c>_{chat,tool,replaced}/` | 21 dirs, configs show `tools: []` |

5. **Re-derive on every reframe.** If an experiment gets renamed, renumbered, or
   regrouped, re-run steps 1–4 for the affected sections. A reframe that touches
   only prose is a red flag, not a checkpoint.

---

## Why review agents (and human reviewers) don't catch this

Reviewers read the **manuscript**, not the **repository**. Their job, as usually
prompted, is to judge whether the writing is clear, coherent, and internally
consistent. A draft can be all three and still misdescribe the data:

- Reviewer agreement converges on a *fixed point of the text*, not of the facts.
  Each round removes friction in the prose, which can mean removing the very
  roughness that hinted at a data mismatch.
- Agents asked to "improve clarity" optimize toward a confident, smooth draft —
  the opposite of the hedged, "let me check" tone that surfaces errors.
- Unless a reviewer is explicitly given the data and told to verify
  correspondence, **consensus is text-consistency evidence only.** Treat it that
  way. (See the multi-agent echo-chamber failure mode in Related.)

If you want a reviewer to catch data drift, you must hand them the data and
the claim→evidence map and ask them to check claims *against the files*, not the
prose against itself.

---

## Anti-fix — do NOT just rewrite for internal consistency

When feedback says a section is confusing or inconsistent, the tempting fix is to
edit until the words line up. **That is the failure mode, not the cure.**

- ❌ Rewriting prose so two paragraphs stop contradicting each other — without
  opening the data to learn *which* paragraph (if either) is right.
- ❌ Renaming/renumbering experiments to make the narrative flow, then propagating
  the new names through the draft as if that settled anything.
- ❌ Accepting "all reviewers now agree" as the done signal for a results section.
- ❌ Smoothing away a hedge ("it's unclear whether tools were used") instead of
  resolving it from the runner code.

The contradiction the reviewer felt is often a **true signal of data drift.**
Re-derive from disk; let the data decide which sentence survives. Only after the
claim→evidence map is true do you polish the wording.

---

## Detection checklist

Run through this before declaring any empirical section "done":

- [ ] Every numbered experiment maps to a specific directory/file on disk.
- [ ] Every headline number (N pairs, N trials, % improvement) was re-counted
      from the data this session, not copied from a prior draft.
- [ ] The experimental setup described (tools on/off, single vs. multi-agent,
      what's held fixed) matches the **runner code**, not just the draft's prose.
- [ ] No experiment was renamed/renumbered this session without re-checking its
      backing data.
- [ ] Reviewer agreement is recorded as "text is consistent," not "claims are
      correct."
- [ ] You can point to the file/column behind each results sentence right now.
- [ ] The claim→evidence map exists and is current.

If any box is unchecked, you are reporting text consistency as if it were data
correspondence. Re-anchor before shipping.

---

## Related

- [pipeline-latex-writing.md](pipeline-latex-writing.md) — the writing workflow this guard wraps; see its pre-writing data-anchoring note.
- **Multi-agent echo chamber / circular citation** — the general form: a group of
  agents converging on a shared but unverified belief. If a `multi-agent-echo-chamber`
  reference or skill is available in your environment, read it; the dynamics are
  identical, only the artifact (manuscript vs. citation graph) differs.
- [pipeline-scholar-analysis.md](pipeline-scholar-analysis.md) — when the claims are about *other* papers' results, verify against the source, not your summary of it.
