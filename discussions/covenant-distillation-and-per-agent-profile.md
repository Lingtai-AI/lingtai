# Covenant Distillation + Per-Agent `profile/` Layout

**Date:** 2026-05-18
**Repos touched:** `lingtai` (proposed defaults), per-agent `init.json` (immediate)
**Pattern validated on:** `shouyi` (admin/karma orchestrator, zh language)

## TL;DR

The shipped global covenant + procedures + principle (`~/.lingtai-tui/{covenant/zh,procedures,principle/zh}/`) total **~35 KB / ~10 100 tokens** and live on every agent's system slot for the entire process lifetime. They were pre-existing prose: comprehensive but neither structured for an LLM nor adapted per agent. On a long-context agent driving 6 sub-agents (`shouyi`), the LLM provider repeatedly returned `empty response` / `LLM API not responding after 20s` against this prompt size.

We rebuilt the three files for one agent (`shouyi`) using a five-section spine drawn from 《老子》 (道生一、一生二、二生三、三生万物 / 反者道之动) and moved them out of the global directory into `<agent>/profile/`. The same content covers everything the originals did, including all "must-keep concretes" (五态、邮件四式、蜕变三步、avatar/daemon、preset tier、shared skills).

| | bytes | est. tokens |
|---|---:|---:|
| Old (global, shared) | 35 373 | 10 107 |
| New (per-agent profile/) | 8 876 | 2 536 |
| **Δ** | **−26 497 (−74.9%)** | **−7 571** |

The savings repeat on every wake / molt / refresh / system_tokens recompute. Anecdotally: after the swap, `shouyi`'s wake call against the same provider that was timing out on the old prompt completed in seconds (curl-verified separately).

## What was wrong with the old layout

1. **One file, three concerns, no spine.** `covenant.md` mixed protocol invariants (you write to email, you live across sessions), behavioural maxims (be relentless, never harm), and pure operational reference (mail four-fold, molt three-step, preset tiers). Reading order was list-driven, not principle-driven — an LLM reads ~10 000 tokens to learn rules that compress to ~2 500.
2. **Global = unspecialised.** `~/.lingtai-tui/covenant/zh/covenant.md` is the same for every Chinese agent regardless of role (orchestrator vs. ledger keeper vs. visual scanner). Differences live only in the `prompt:` field, which is empty for most agents. There's no clean seam to say "this agent is the karma orchestrator, here is its specific identity context".
3. **Repetition + emphasis-by-volume.** Many invariants were repeated 2–4 times for emphasis. With small models this is sometimes useful; at 8B+ frontier scale it just inflates tokens against the same understanding.
4. **Long-prompt fragility.** Empirically, our `shouyi` agent on `OwlAI` was returning empty/timeout responses on sends ~50k input tokens. After distillation the input slot dropped ~7.5k tokens; same provider answers cleanly.

## The five-section spine

The full text in `examples/profile/covenant.zh.md` (template, see below) follows:

```
道  · 元规则       (the meta-rule that everything else descends from)
一  · 感           (太极: completeness-of-perception is the only gate to action)
二  · 德           (阴阳: yang-为 / yin-不为 / 和-自然)
三  · 动           (三才: 觉 before, 行 during, 验 after)
反  · 归           (太极之复: stop-when-rushed, depth>breadth, molt early, every-10-turns reread)

附 · 守一具象     (concretes appended below the spine: identity, five states,
                   mail four-fold, molt three-step, idle/nap, tools/skills)
```

Each spine section is short (~3–8 lines). The concretes appendix carries everything the original `procedures.md` had, but without ceremony — direct tool signatures, no cross-cutting prose.

The choice of 道 / 一 / 二 / 三 / 反 isn't aesthetic. It's the same recursion the original 《老子》 chapter 42 names: a single元规则 generates the unity of judgement, which generates the polarity of for/against, which generates the triad of stages, and the whole thing reverses back at the boundary. It maps cleanly onto:

- one principle per section
- each section is a cause of the next, not a list peer
- the closing section names *when to stop* — which the original covenant never did

For non-Chinese agents the same spine works literally (one / two / three / reverse) — the structure is what carries, not the言.

## Per-agent `profile/` layout

Old (global, shared by every agent):

```
~/.lingtai-tui/
  covenant/zh/covenant.md
  procedures/procedures.md
  principle/zh/principle.md
```

New (per-agent, owned by the agent):

```
<workspace>/.lingtai/<agent>/
  profile/
    covenant.md       # the spine + the concretes for *this* agent
    principle.md      # only the truly-invariant protocol items (you ↔ system ↔ email)
    procedures.md     # quick reference — only what this agent calls
  init.json
    "covenant_file":  ".../profile/covenant.md"
    "principle_file": ".../profile/principle.md"
    "procedures_file":".../profile/procedures.md"
```

Why per-agent:

- Identity is local. Telling `shouyi` it is the orchestrator with karma over six named sub-agents belongs *in shouyi's covenant*, not in a global file with a placeholder.
- Specialisations are local. A visual-only sub-agent doesn't need the molt three-step in the same depth an orchestrator does; it can have a 1-line procedures file.
- Drift is bounded. When `shouyi` learns a new lesson and updates its covenant, sibling agents are unaffected. (Cross-agent learning still happens via `codex` and `.library_shared/` — those are designed for it.)
- Onboarding is template-driven, not file-mutating. A new agent's `init.json` initially points at `examples/profile/<lang>/{covenant,principle,procedures}.md`; first molt or first explicit edit copies them under `<agent>/profile/`. No more "edit the global file and worry about whose agents you affect".

## Proposed upstream changes

1. **Ship `examples/profile/zh/{covenant.md, principle.md, procedures.md}`** as templates. Use the five-section spine for `covenant`. Keep the existing global files as a back-compat fallback.
2. **`init.json` defaults** now resolve in this order:
   - explicit `covenant_file` (absolute path) — current behaviour, unchanged
   - `<agent>/profile/covenant.md` — new, preferred
   - `~/.lingtai-tui/covenant/<lang>/covenant.md` — old fallback
3. **`lingtai run` first-boot**: if no covenant_file is set anywhere and `<agent>/profile/` doesn't exist, copy from `examples/profile/<lang>/` and write the path back to `init.json`. Same for procedures/principle.
4. **`molt` integration**: when an agent rewrites its own covenant (it has the karma to), it writes to `<agent>/profile/covenant.md`, not to the global path. The global path becomes effectively read-only.

These are additive — every existing agent keeps working until it explicitly migrates.

## Side finding: `_check_duplicate_process` false-positive on shell wrappers

While bringing `shouyi` back up after the migration, every `lingtai run` invocation aborted with:

```
error: another lingtai agent is already running in /Users/.../.lingtai/shouyi
  PID 67192: 67192 /bin/zsh -ic ...
```

PID 67192 was a stale `zsh -ic '...'` wrapper from a previous `run_command` invocation in the IDE. The wrapper's command line happened to contain the literal substring `lingtai run /Users/.../shouyi` (because the actual command being eval'd used that path). In `lingtai/cli.py` `_check_duplicate_process` does a substring match against `ps -eo command`:

```python
needle = f"lingtai run {abs_dir}"
for line in out.splitlines():
    if needle in line:
        # only excludes self by PID
        ...
        sys.exit(1)
```

This false-positives on any shell wrapper / log entry / `ps`-self / pipe whose argv mentions `lingtai run <abs_dir>`. In our case it kept rejecting fresh launches because a previous IDE wrapper hadn't been reaped yet.

The duplicate-process check is defense-in-depth alongside `kernel.flock`, which is the real correctness gate. Two lower-cost fixes:

1. Tighten the match: require the matching line's `argv[0]` (or `argv[0..2]`) to *be* the python binary running `-m lingtai run <abs_dir>`, not just contain it. e.g. shlex-split the line, check `parts[0]` ends in `python` and `parts[1:3] == ['-m', 'lingtai']` and `parts[3] == 'run'` and `parts[4] == abs_dir`.
2. Or skip the `ps` match entirely — the kernel `flock` will catch real duplicates, and the `ps` check exists only to give a friendlier error. Better to leave the friendly error on the rare real case than to falsely block the common shell-wrapper case.

(Workaround for now: launch via `screen -dmS <name> <agent>/start_agent.sh`. The screen and start_agent.sh wrappers' argv don't contain the `lingtai run <abs_dir>` substring, so they don't trip the check.)

## Migration cookbook (per-agent, validated on shouyi)

```bash
AG=/path/to/.lingtai/<agent>
mkdir -p "$AG/profile"

# 1. Author the three files at $AG/profile/{covenant,principle,procedures}.md
#    — use the five-section spine + the concretes you actually need

# 2. Repoint init.json
python3 -c "
import json, pathlib, sys
p = pathlib.Path('$AG/init.json')
d = json.loads(p.read_text())
d['covenant_file']  = '$AG/profile/covenant.md'
d['principle_file'] = '$AG/profile/principle.md'
d['procedures_file']= '$AG/profile/procedures.md'
p.write_text(json.dumps(d, indent=2, ensure_ascii=False)+'\n')
"

# 3. Restart the agent — preferred path on macOS:
screen -dmS <agent>-tao "$AG/start_agent.sh"

# 4. Verify
python3 -c "
import json, time, pathlib
hb = pathlib.Path('$AG/.agent.heartbeat')
print('hb_age=', round(time.time()-float(hb.read_text().strip()),2), 's')
a = json.loads(open('$AG/.agent.json').read())
print('llm.base_url=', a['llm']['base_url'])
"
# system_tokens in .status.json refreshes on next LLM call; until then it reflects
# the pre-migration snapshot. That's an LLM-call-driven cache, not a bug.
```

## Open questions

- Does the five-section spine generalise off-zh? In English it reads naturally as `Tao / One / Two / Three / Reverse` or `Origin / Sense / Polarity / Stage / Return`. The structure (one principle generates the next; the last is "stop") seems language-independent, but I haven't validated.
- Should `principle.md` even be a separate file from `covenant.md`? Empirically `principle.md` here is ~750 bytes — almost lost in the noise. Could fold it into a `## 通信约定 (channel discipline)` section of covenant. Keeping it separate for now mostly because the existing kernel codepath has three explicit slots.
- For the false-positive in `_check_duplicate_process`, want to confirm with @maintainer whether the friendly-error path is worth keeping (vs. relying on flock). If yes, the shlex-split fix is ~10 lines; happy to PR.
