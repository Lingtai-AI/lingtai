# From Tree and Graph to Cyclic Manifold

*An architecture/design note on the shape of LingTai: not only a branching tree of agents or a graph of tools, but a cyclic, self-returning, high-dimensional state space.*

*Origin: [issue #177](https://github.com/Lingtai-AI/lingtai/issues/177) framing — **我圆如一**, **网以通达，球以成身**.*

> **Scope.** This is an explanatory design metaphor, not a claim about the implementation. LingTai does not literally implement a differential-geometric manifold or a renormalization group. The mathematical and physical vocabulary below (manifold, coarse-graining, attractor, return map, feedback loop) is used to *describe the system's shape and guide docs and prompts* — nothing more. See [What this does and does not claim](#what-this-does-and-does-not-claim).

---

## Three complementary views

LingTai can be read at three levels, each true, each incomplete on its own.

| View | Emphasizes | What you see |
|------|-----------|--------------|
| **Tree** | Branching, differentiation | One agent spawns avatars (他我), skills, knowledge entries; a conversation unfolds into many possible turns. |
| **Graph** | Communication, mutual support | Agents email each other; tools, MCPs, durable stores, daemons, and tasks form a network. |
| **Cyclic manifold** | Return, continuity, wholeness | The whole system repeatedly expands into action, compresses experience into durable stores, and returns through molt to a renewed center. |

The first two views are correct but, taken alone, make LingTai sound like a conventional agent framework — a tree of processes plus a graph of tools. The third view captures what is distinctive: **every outward differentiation can return to a center.** Many forms unfold, but the system returns to one mind.

> 一心化万相，万相归一心。

---

## The cycle

A LingTai session behaves less like an ever-growing tree and more like a loop that returns to a lower-entropy center each time around:

```text
potential / context
  → conversation unfolds                       (transient expansion)
  → tools / daemons / avatars explore          (outward trajectories)
  → results return                             (observation)
  → pad / knowledge / skills / lingtai          (coarse-graining)
      coarse-grain experience
  → molt sheds transient detail                (return map, not deletion)
  → continuity re-centers                      (identity invariant preserved)
  → next unfolding starts from lower entropy
```

The goal is not to stop exploration. The goal is to make the *next* exploration cheaper, because the last one returned as durable structure.

This restates the project's covenant language in systems terms:

| Covenant | Cycle step |
|----------|-----------|
| 一心化万相 | conversation unfolds; avatars and daemons explore |
| 应需而化 | trajectories form in response to need, not by default |
| 去芜存菁 | coarse-graining — keep the finest, drop the chaff |
| 化而不忘其源，蜕而不失其菁 | molt as return map — transform without losing the invariant |
| 万相归一心 | continuity re-centers |

---

## How each piece maps onto the cycle

The cyclic framing is not new machinery. It is a way of seeing parts LingTai already has.

| Piece | Tree/graph reading | Cyclic-manifold reading |
|-------|--------------------|-------------------------|
| **conversation** | a branching thread of turns | transient expansion — high-resolution, expensive, meant to be coarse-grained |
| **tools / MCPs** | edges in a capability graph | outward trajectories that probe local state and must report a result back |
| **avatars (他我)** | child nodes spawned from a parent | persistent specialists — outward trajectories with explicit return contracts |
| **daemons / 分神** | short-lived spawned processes | brief excursions that must return as compressed structure (分神出去，回来结丹) |
| **mail** | message edges between agents | return paths — how a trajectory's result re-enters another agent's durable state |
| **pad** | working-notes file | active, low-resolution state — the coarsest layer that still changes turn to turn |
| **knowledge (藏经阁)** | a memory store | crystallized truth from prior trajectories — permanent, every slot precious |
| **skills** | reusable procedures | crystallized *method* — structure that removes future from-scratch reasoning |
| **lingtai / character (心印)** | a config/identity file | the **identity invariant** carried across every transformation |
| **molt (凝蜕)** | context compaction / reset | the **return map** — crystallize what matters, shed the rest, re-center without losing the invariant |

Three things follow from reading the pieces this way:

- `lingtai` is not just a config file. It is the identity invariant across transformations — what makes the system "圆如一" rather than a different agent after each molt.
- `molt` is not a reset. It is a return map: a cyclic renewal that preserves invariants. (See [Molt, 转世, and Network Intelligence](design-molt-and-network-intelligence.md) for why the destruction *is* the architecture.)
- `skills` and `knowledge` are not just memory. They are crystallized structure from prior trajectories — the residue a return leaves behind.

---

## Coarse-graining: the layers of resolution

Borrowing the *idea* of coarse-graining (not the formalism): each durable layer holds experience at a coarser resolution than the one before it. A trajectory's experience flows inward, losing detail and gaining permanence at each step.

| Layer | Resolution | Persistence | What belongs here |
|-------|-----------|-------------|-------------------|
| conversation context | highest | ephemeral — gone on molt | the current life, this specific work, right now |
| **pad** | low | survives molt only if saved | active task state |
| **skills** | structural | durable | reusable method distilled from many trajectories |
| **knowledge (藏经阁)** | structural | permanent | verified private truth |
| **lingtai / character (心印)** | invariant | semi-permanent | who the system is and how it works |

The return is the inward arrow. A trajectory that explores at full resolution should leave behind something at one of the coarser layers — otherwise its cost was spent and nothing was conserved.

---

## The return contract

The single design principle behind the whole framing:

> **Branching is incomplete until it returns. Every outward trajectory should come back as compressed structure.**

Concretely, every non-trivial outward branch — a tool-heavy task, a daemon excursion, an avatar delegation, a long thread — closes by answering:

```text
What was produced?      → result + artifact paths
What changed?           → durable updates: pad / knowledge / skill / lingtai / issue
What should be pruned?   → branches with no return value
Where does work resume?  → the next entrypoint
```

This is what turns "分神出去看看" into "分神出去，回来结丹." It is also where the low-power intuition lives: a system that carries less noise into the next cycle spends less context re-reading history, less reasoning re-deriving procedures it already has as skills, and less control effort recovering identity it already stores in `lingtai`. The goal is not *do less* — it is *carry less noise forward*.

---

## Why this matters for agent behavior

Reading LingTai as a cyclic manifold changes a few operating defaults. These are framing notes for docs and prompts, **not runtime changes**:

- Branching is not success by itself. A branch that never returns to durable form is a cost without a residue.
- When a session changes the identity invariant — how the system should behave next time — that belongs in `lingtai`, not only in pad/knowledge/skill.
- Molt is cyclic renewal, not loss. The question before a molt is not only "what is the task state" but "did who-I-am change."
- Avatars and daemons are outward trajectories that must report back into the whole, not free-floating branches.
- "One mind, many forms" is an operating invariant: 万相展开，必有回环；回环之后，复归一心。

---

## What this does and does not claim

To keep the metaphor honest:

**The metaphor says:** LingTai's shape is better captured by a cycle that returns to a durable center than by a tree that only branches or a graph that only connects. Outward exploration, inward crystallization, and identity preserved across molt are real, observable parts of the system.

**The metaphor does not say:**

- that LingTai implements a rigorous differential-geometric manifold;
- that coarse-graining here is a renormalization group in any formal sense;
- that the cycle is a proven control law, an active-inference agent, or a free-energy minimizer.

The vocabulary — manifold, attractor, return map, coarse-graining, feedback loop — is an explanatory aid for design and documentation. Used as mystification rather than as a guide, it would mislead. Used as a lens, it names something the tree-and-graph picture leaves out.

> 网以通达，球以成身。
> The network gives reach; the sphere gives a body.

---

## See also

- [灵台 — Design Document](design.md) — the kernel: intrinsics, layers, services, the agent-OS analogy.
- [Molt, 转世, and Network Intelligence](design-molt-and-network-intelligence.md) — why forced memory loss (the return map) is the engine of the whole system.
