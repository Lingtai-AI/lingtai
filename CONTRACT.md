---
name: component-contract-convention
contract_version: 1
related_files:
  - ANATOMY.md
  - README.md
  - dev-guide-skill/SKILL.md
  - tui/architecture_documents_test.go
  - tui/internal/process/CONTRACT.md
  - tui/internal/processscan/CONTRACT.md
maintenance: |
  This file is the normative root of the distributed code interface definition
  system and the contract-of-contract for the LingTai Go repository (the two
  binaries lingtai-tui and lingtai-portal plus the install pipeline). Keep the
  root ANATOMY.md reciprocal. Keep each governed child CONTRACT.md linked here
  exactly once and require every child to point back with root_contract:
  CONTRACT.md and pair with its co-located ANATOMY.md. Change architecture
  rules, schemas, templates, maintenance contracts, and validation together.
  Revalidate all linked pairs whenever this convention changes; bump
  contract_version for a breaking convention change.
---
# Component Contract Convention

## Purpose

**CONTRACT is the distributed code interface definition system.** Each
architectural layer keeps a `CONTRACT.md` beside the code whose interface it
owns: Core/use cases, inbound and outbound Ports, Adapters, expected agent
behavior, errors, ordering, state semantics, and conformance tests. Local
contracts link into a graph that an agent can descend from this repository root
to the exact interface promise relevant to a change.

This file is the repository's Ports & Adapters foundation and the
**contract of contract**: the normative meaning, child template, link rules,
versioning, and maintenance contract for that distributed system. Existing
specialized contracts are governed only when this file lists them as children.

[`ANATOMY.md`](ANATOMY.md) is the paired distributed code navigation system. It
describes where code is and how it is composed; this contract defines how a
layer may be used and what it promises. They cross-link instead of duplicating
each other's content.

This repository ships two independent binaries — `lingtai-tui` and
`lingtai-portal` — as two Go modules with no shared module root, plus the
`install.sh` pipeline. Neither binary imports the other; both read the same
per-project `.lingtai/` filesystem state and exchange running-agent state with
the Python kernel through files. Only the TUI launches kernel agents (as
`python -m lingtai run` subprocesses); the portal observes but does not launch
them. That two-binary, shared-state topology — where the durable cross-process
interface is the on-disk `.lingtai/` schema rather than a socket or RPC channel
— is the concrete shape this convention governs. It is not a Python-package
convention; it is a Go-repository convention that keeps the two-binary symmetry
the paired Anatomy already documents.

## Architecture foundation

These rules are the **target** architecture for newly governed or migrated
components — the direction a component moves toward when it enters the governed
system. They are not a description of the current repository. Today the two
binaries are largely concrete/mixed: `internal/fs`, the Bubble Tea presentation
models, the portal HTTP handlers, and the large `main.go` entrypoints hold
domain, presentation, filesystem, and policy responsibilities together, import
concrete packages directly, and expose no Core-owned Port with an injected
adapter. That is the honest starting point. A component earns the Ports &
Adapters roles below only through a real migration (rule 9); until then it is
unmigrated code the graph still maps and this contract still governs
behaviorally.

Normative rules:

1. LingTai components MUST be reasoned about as **Core / Use Cases**,
   **Ports / Contracts**, and **Adapters**. Core owns domain decisions,
   orchestration, and policy. Ports are technology-neutral boundaries owned by
   Core. Adapters translate concrete operating systems, providers, protocols,
   SDKs, processes, filesystems, terminals, or browsers into Ports.
2. The allowed conceptual dependency is:

   ```text
   Adapter -> Port <- Core
   ```

   Core and adapters may depend on the Port. Core MUST NOT depend on, import,
   construct, branch on, or name a concrete adapter.
3. The target direction is exactly: **Core owns Ports; adapters live outside**.
   A Port is placed with the Core boundary it protects; production adapters are
   placed outside that Core package and depend inward.
4. Core technology ignorance is mandatory. Core MUST NOT know POSIX vs Windows,
   Bubble Tea vs another terminal toolkit, one HTTP router vs another, or a
   particular model provider. Platform/provider/protocol types, exceptions,
   configuration keys, payloads, and branch conditions belong in adapters
   unless translated into technology-neutral Port vocabulary. When a component
   in this repository migrates, its platform-specific concerns — the
   `*_unix.go` / `*_windows.go` build-tagged files, the Bubble Tea screen
   models, the portal HTTP handlers, and the embedded React frontend — are the
   surfaces that become adapter-side. The `.lingtai/` filesystem schema those
   layers read and write is not a Core-owned Port; it is the shared external
   on-disk protocol described below, which a component's filesystem adapter
   realizes.
5. A Port is more than a Go interface. Its component `CONTRACT.md` owns units,
   ordering, errors, state/time domains, and observable guarantees; adapters and
   Core use cases are tested against those same rules.
6. One small outer **Composition Root** MAY read deployment configuration,
   select concrete adapters, construct them, and inject them into Core. It MUST
   own wiring only. It MUST NOT contain business decisions, use-case policy,
   provider-specific behavior that belongs in an adapter, or a service-locator
   mechanism that lets Core fetch implementations implicitly. In this
   repository each binary's flat `main.go` (`tui/main.go`, `portal/main.go`) is
   today's startup/composition edge — it selects subcommands and drives startup
   — but it is not yet a small wiring-only Composition Root: both currently own
   startup decisions, migration/consistency policy, and recovery logic mixed in
   with wiring. Shrinking a `main.go` toward a wiring-only root is migration
   work, not an accomplished fact.
7. Components MAY be nested. A component can present one capability to its
   parent while internally owning smaller Core/use-case, Port, and Adapter
   boundaries. A parent Core MUST depend on a child component through the
   child's Port, not reach through it to its internal implementation. Each
   component contract states the boundary and viewpoint it governs.
8. Concrete technology belongs only in the Adapter at the boundary where that
   technology actually varies. POSIX or Windows belongs at operating-system
   boundaries; a specific terminal or browser toolkit at the presentation
   boundary; a specific model or transport at the provider boundary. These
   identities MUST NOT leak up through otherwise technology-neutral parent
   Ports.
9. A component migration is complete only when its existing responsibility is
   actually separated into a Core-owned Port and one or more outside Adapters,
   Core no longer imports or constructs the concrete mechanism, the Composition
   Root wires the chosen Adapter, and shared contract tests prove conformance.
   New directory names or an unused interface alone do not satisfy this rule.
10. Migration MUST proceed one real boundary/vertical slice at a time: one use
    case, its Port and contract, one real production adapter, composition
    wiring, and contract tests. Do not perform a one-shot repository
    rearrangement, create speculative empty Port/adapter taxonomies, or claim
    unmigrated code already obeys the target architecture.
11. Ports are earned by architectural boundaries, not by file count. Pure
    algorithms, value objects, and ordinary internal helpers SHOULD remain
    ordinary code unless they own an independently meaningful promise, isolate
    a concrete mechanism or side effect, or require substitutable
    implementations.

Inbound ports versus outbound ports:

An **inbound port** is how an external driver asks Core to execute a use case.
It points into Core. The use-case implementation is in Core; a driving adapter
translates an external event or request into the inbound Port. When a component
here migrates, a terminal keypress routed through a Bubble Tea model, a portal
HTTP request, or a subcommand invocation from `main.go` is the kind of external
event a driving adapter would translate into an inbound Port. Today those events
reach mixed presentation/use-case code directly; the inbound Port is the target,
not a boundary the current code already has.

An **outbound port** is how Core asks the outside world for a capability. It
points out of Core conceptually, while the source dependency still points inward
because an outer adapter implements the Core-owned Port. Reading an agent's
heartbeat, writing a signal file, or launching `python -m lingtai run` (TUI
only) are the kind of side effects a migrated Core would reach through an
outbound Port — realized today by concrete code in `internal/fs/` and
`internal/process/` that Core-equivalent logic calls directly. Serving embedded
web assets (`embed.go`) is presentation delivery, not by itself a Core outbound
port.

Two-binary, shared-state note (platform-neutral promise): the durable interface
among `lingtai-tui`, `lingtai-portal`, and the Python kernel is the `.lingtai/`
filesystem schema — manifests, heartbeats, mailbox folders, signal files,
`meta.json`, and the read-only event log. That schema is the **shared external
on-disk interoperability protocol** these programs agree on: a compatibility
contract between separately built processes, not a technology-neutral Core-owned
Port. It is concrete (paths, folders, file formats) even though it is portable
across operating systems. A migrated component MAY own a technology-neutral
semantic Port whose filesystem adapter realizes this protocol; the protocol
itself is not that Port, and each binary's `fs` layer is today concrete code
that reads and writes it directly. A promise stated in terms of this schema (a
field, a folder layout, an ordering, a migration version) MUST hold identically
for whichever binary or OS reads it. Neither binary may open a socket or RPC
channel to a running agent as a hidden second interface.

The Composition Root belongs at the outer application/startup edge:

```text
read deployment config -> construct selected adapters -> inject Ports -> start Core
```

Choosing which adapter is configured is wiring. Deciding what an agent should
do, when a signal is honored, how a use case interprets time, or what fallback
policy applies is Core/use-case policy and MUST remain outside the Composition
Root.

Non-normative wall-socket analogy:

As explanatory prose only, Core is like a house whose rooms rely on wall
sockets without knowing the power station or appliance manufacturer. The
Port/Contract is the socket shape, voltage, and safety agreement; an adapter is
the plug/transformer that connects a particular external technology; the
Composition Root decides what is plugged in. The analogy is not normative, does
not define Go placement, and must not replace the inbound/outbound or
dependency rules above.

## Behavior

Every contract includes an expected-agent-behavior agreement. It states
observable obligations and prohibitions for LingTai agents and coding agents
that use, inspect, or modify the governed component. It does not duplicate a
manual's commands or troubleshooting recipes: **Behavior defines what agents
must do; manuals and skills explain how to do it.**

Root behavior rules:

1. Before development, agents MUST find and read the repository-local dev guide
   (the repository-root `dev-guide-skill/SKILL.md`); before reasoning about or
   changing a governed component, they MUST read the nearest `ANATOMY.md` to
   navigate its code and the paired `CONTRACT.md` to learn its interface and
   behavior promises.
2. LingTai agents that observe runtime behavior MUST compare evidence with the
   contract, surface mismatches, and preserve uncertainty. They MUST NOT hide an
   implementation defect by weakening the written promise. They may report or
   propose a contract change, but changing the product promise requires explicit
   authorization.
3. Coding agents MUST keep implementation, the Anatomy/Contract pair, Ports,
   affected Adapters, and shared contract tests synchronized in the same PR
   whenever their governed facts or promises change.
4. Agents MUST traverse YAML `related_files` as the distributed graph and repair
   missing, stale, duplicate, one-way, or orphaned edges they touch. They MUST
   NOT invent a second registry or copy the same normative rule into multiple
   layers.
5. Agents MUST keep concrete technology outside Core, wire implementations only
   at the Composition Root, and reject unused interfaces or directory-only
   reshuffles as evidence of a completed migration.
6. Because both binaries read one on-disk schema, a change to **shared** state
   that both consume (a `.lingtai/` field, folder layout, or signal semantics)
   MUST keep `lingtai-tui` and `lingtai-portal` compatible. Legacy
   `.lingtai/meta.json` migration fields may remain on disk, but production does
   not consult, advance, or gate on them; the retained TUI/Portal migration
   registries are historical/test APIs rather than a live lockstep contract.
   This is compatibility, not mirrored code: portal-only state (`.portal/` port
   and recordings), TUI-only or kernel-facing capabilities, and each binary's
   distinct feature set are legitimate and need not acquire a symmetric
   implementation in the other.
7. A component's local `Behavior` section MAY add stricter obligations specific
   to that boundary, including safe handling of retries, cancellation, unknown
   side effects, ordering, recovery, or sensitive data. It MUST NOT contradict
   this root behavior contract.

The behavior agreement is jointly maintained: LingTai agents contribute runtime
observations and drift evidence; coding agents update code and architecture
documents; shared tests and review supply conformance evidence.

## Frontmatter contract

The root contract has exactly `name`, `contract_version`, `related_files`, and
`maintenance` frontmatter keys. It omits `root_contract` because it is the root.

The governed-child rules in this section (and in `## Body contract`,
`## Link semantics`, and `## Maintenance contract`) are the normative schema for
all root-governed children. The first governed vertical slice is the TUI agent
process lifecycle plus its neighboring process-observation boundary. The focused
validation in `## Validation` enforces that concrete graph without pretending to
be a general YAML or architecture parser.

Every governed child contract has exactly these frontmatter keys, in this
order:

1. `name`: non-empty kebab-case identity, unique among root-linked children.
2. `contract_version`: positive YAML integer.
3. `root_contract`: literal repo-relative path `CONTRACT.md`.
4. `related_files`: non-empty duplicate-free list of repo-relative regular
   files. It includes the co-located paired `ANATOMY.md`, the Port, every
   production Adapter, contract tests, public exports, and directly relevant
   component contracts.
5. `maintenance`: the canonical generic text from the template below. The
   focused validator requires the canonical opening sentence and a non-empty
   block; review remains responsible for detecting drift in the rest of the
   maintenance wording rather than introducing a second YAML parser.

## Body contract

The root body headings are exactly the nine `##` sections in this file, in
this order.

Every governed child body has these `##` headings, once and in this order:

1. `## Purpose`
2. `## Behavior`
3. `## Port`
4. `## Adapters`
5. `## Contract rules`
6. `## Contract tests`
7. `## Maintenance`

Child contracts describe behavior and maintenance obligations. They do not use
the `ANATOMY.md` structural section template and do not require line citations.

## Link semantics

YAML `related_files` is the single graph-wiring mechanism; do not introduce a
second registry. This root contract and root anatomy list each other exactly
once. The governed child `CONTRACT.md` entries in root `related_files` form the
canonical paired-component index.

Each governed child appears exactly once there, points back with
`root_contract: CONTRACT.md`, and lists its co-located `ANATOMY.md`. That anatomy
lists the child contract in return. Child `related_files` also lists the Port,
every production Adapter, contract tests, public exports, and related component
contracts that own the boundary. Contract-to-contract links are reciprocal when
either contract depends on the other's normative rules. Unrelated children do
not link to each other or copy each other's promises.

## Maintenance contract

Every code change MUST assess both distributed systems. If files, symbols,
connections, composition, or state ownership change, update Anatomy in the same
change. If a Port, Adapter, behavioral promise, error, ordering, or state
semantic changes, update Contract and contract tests in the same change. If
neither changes, review evidence may record that the pair was checked rather
than manufacture meaningless document churn.

The repair direction differs. Code is normally the structural source of truth
for Anatomy, so stale navigation follows verified code. Contract is normative
for behavior: if implementation and a governed contract disagree, treat that as
a defect and do not silently rewrite the promise to match accidental behavior.
Only an authorized contract change may deliberately change the promise.

A breaking Port-contract change is a change that makes a previously conforming
Port consumer or Adapter no longer conform: removed or renamed operation,
changed domain, units, ordering, error semantics, narrowed guarantee, or newly
required behavior. Breaking Port-contract changes bump `contract_version` and
update the Port, affected Adapters, shared contract tests, and paired Anatomy
when structure or composition also changes. A change to the shared `.lingtai/`
schema is breaking whenever it makes a previously conforming reader (either
binary, on any supported OS) no longer conform.

## Validation

`tui/architecture_documents_test.go` is a small real-repository smoke test in
the existing TUI module, run with `cd tui && go test ./...`. It checks the entry
path plus the first concrete governed vertical slice:

- the root `ANATOMY.md` and `CONTRACT.md` list each other and the repository-local
  `dev-guide-skill/SKILL.md`;
- the three READMEs and `CLAUDE.md` contain Markdown links to both roots and the
  dev guide;
- root `CONTRACT.md` lists the process-lifecycle and process-observation children
  exactly once;
- each child uses the governed frontmatter prefix, points to the root, uses the
  canonical maintenance opening, lists its paired Anatomy and decisive code/test
  files, and has the seven required body headings once in order;
- paired and parent/neighbor Anatomy links are reciprocal and every checked
  related-file target is a regular file.

It deliberately does not implement a second YAML parser or enforce scalar
styles, arbitrary future child schemas, symlink/case variants, or citation line
ranges. Those concerns remain review- and skill-owned until a recurring defect
earns narrower machinery. The test lives in the TUI module because the root
documents belong to neither binary and a third module is unnecessary. Citation
drift remains owned by the `lingtai-tui-anatomy` skill.

## Template

```markdown
---
name: <kebab-case-component-name>
contract_version: 1
root_contract: CONTRACT.md
related_files:
  - <repo-relative paired ANATOMY.md>
  - <repo-relative Port file>
  - <repo-relative production Adapter file>
  - <repo-relative contract-test file>
maintenance: |
  This component contract is governed by the root CONTRACT.md. Keep
  related_files complete and repo-relative: the paired ANATOMY.md, Port, every
  production Adapter, contract tests, and directly relevant component contracts
  belong here. Re-read this contract whenever a linked boundary changes. Update
  the Port, affected Adapters, contract tests, and this contract in the same
  change; update the paired Anatomy when structure or composition also changes;
  bump contract_version for a breaking Port-contract change. If code and contract
  disagree, treat the disagreement as a defect—do not silently rewrite the
  normative contract to match the implementation.
---
# <Component Name>

## Purpose

## Behavior

<State observable obligations and prohibitions for LingTai agents and coding
agents. Link to manuals/skills for procedures instead of duplicating them.>

## Port

## Adapters

## Contract rules

## Contract tests

## Maintenance
```
