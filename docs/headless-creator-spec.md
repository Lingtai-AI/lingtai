# Standalone Headless Creator Specification

- **Status:** proposed implementation contract
- **Spec version:** 1
- **Owner:** a standalone `Lingtai-AI/lingtai-create` distribution
- **Tracked decision:** [TUI issue #921](https://github.com/Lingtai-AI/lingtai/issues/921)
- **Current compatibility contract:** [Headless Runtime Contract](headless-runtime-contract.md)

## Decision

New bot creation becomes a standalone product named **LingTai Create**. Its
Python distribution, import package, and executable are respectively
`lingtai-create`, `lingtai_create`, and `lingtai-create`.

The implementation lives outside both `Lingtai-AI/lingtai` and
`Lingtai-AI/lingtai-kernel`. It orchestrates public kernel commands and public
runtime state; it does not import TUI `internal` packages or kernel private
modules.

The ownership split is:

- the kernel owns reusable project and runtime primitives;
- LingTai Create owns non-interactive project/bot creation policy and the
  create/configure/launch transaction;
- channel adapters own channel-specific configuration and verification;
- the TUI owns interactive presentation and optional TUI-local registration;
- skills and manuals teach the workflow but are never its implementation.

`lingtai-tui spawn` becomes a compatibility wrapper around LingTai Create. It
must not retain a second creation implementation.

## Why this boundary

The current headless path has already moved project seeding into
`lingtai-agent project create` and process start into `lingtai-agent run`, but
`lingtai-tui spawn` still owns preset/covenant selection, compatibility writes,
recipe state, registration, launch, readiness, and recovery. The bundled
Telegram helper then configures the already-running agent and requests a
refresh.

That order has three structural problems:

1. a headless creator still requires the TUI binary and TUI-managed runtime;
2. the agent can run before its channel secret and MCP configuration exist;
3. a failure after project creation has no portable resume contract.

Moving channel or TUI policy into the kernel would replace one ownership leak
with another. A separate orchestrator preserves the kernel's reusable boundary
and lets the TUI consume the same creation contract as every other controller.

## Goals

Version 1 must:

1. create a fresh one-agent LingTai project without `lingtai-tui` being
   installed or present on `PATH`;
2. configure the project completely before starting the agent;
3. support Telegram as the first channel through a versioned adapter contract;
4. emit one machine-readable result or one machine-readable error;
5. make every non-terminal failure inspectable and resumable;
6. verify process liveness independently of model-generated text;
7. keep credentials out of argv, output, logs, receipts, and tracked files;
8. work on Darwin, Linux, native Windows, and WSL2;
9. let the TUI wrap the exact same implementation without changing the
   creator's success semantics; and
10. remove recipe and TUI-registry policy from the portable creation path.

## Non-goals

Version 1 does not:

- install, repair, or upgrade Python, LingTai, an addon, or the TUI;
- manage saved/template preset catalogs;
- create more than one initial agent;
- add an agent to an existing project;
- adopt an arbitrary partial `.lingtai` tree;
- delete an existing project or transaction;
- merge, publish, or deploy anything;
- prove that a model answered correctly;
- provide an interactive setup wizard;
- replace general runtime lifecycle administration; or
- make TUI project registration part of portable creation success.

A missing prerequisite fails before target publication with an actionable code.
The creator never downloads a missing prerequisite as a side effect.

## Authoritative dependencies

LingTai Create consumes only public surfaces:

- `lingtai-agent project create --json` for a fresh data-only seed;
- `lingtai-agent run <agent-dir>` for runtime start;
- `.agent.lock`, `.agent.heartbeat`, and `.status.json` for liveness;
- the filesystem mailbox probe/ack contract for runtime reachability; and
- documented addon configuration and verification contracts.

The current kernel project-create contract remains data-only. It does not gain
Telegram, TUI registry, recipe, prompt-selection, installer, or launch policy.
The kernel is the sole authority for the canonical seed layout, initial
manifests, mailbox directories, init/Psyche serialization, and seed validation.
The creator may add only its documented post-seed files and configuration; it
must not maintain a second copy of those kernel invariants.

LingTai Create must execute the kernel CLI as a child process. It must not rely
on internal Python imports that can change without a CLI contract revision.

## Public CLI

Version 1 exposes three commands:

```text
lingtai-create create --request REQUEST.json [--json]
lingtai-create resume --project-dir ROOT --transaction ID [--json]
lingtai-create inspect --project-dir ROOT [--json]
```

`--json` is mandatory for machine callers and recommended for all automation.
In JSON mode:

- success writes exactly one JSON document to stdout and nothing else;
- failure writes exactly one JSON document to stderr and exits nonzero;
- warnings go inside the document, never beside it; and
- child stdout/stderr are captured and never forwarded verbatim.

The CLI accepts no credential value as a flag. A future interactive frontend
may generate the same request document and invoke this CLI; it must not call a
separate library path with different semantics.

## Request document

The request is a versioned JSON or JSONC object:

```json
{
  "schema_version": 1,
  "kind": "bot",
  "project_dir": "/path/to/project",
  "agent_name": "example-bot",
  "language": "en",
  "preset": {
    "ref": "/path/to/preset.json"
  },
  "covenant": {
    "source": "bundled"
  },
  "initial_prompt": null,
  "runtime": {
    "python": "/path/to/runtime/python"
  },
  "start": true,
  "channel": {
    "type": "telegram",
    "schema_version": 1,
    "account_alias": "main",
    "token": {
      "env": "TELEGRAM_BOT_TOKEN"
    },
    "allowed_users": [123456789],
    "poll_interval_seconds": 1.0
  },
  "verification": {
    "runtime_probe": true,
    "channel_identity": true
  }
}
```

Rules:

- `schema_version` is exactly `1`; unknown versions fail closed.
- `kind` is exactly `bot` in version 1.
- `project_dir` names a directory without `.lingtai`, or an absent directory
  whose existing parent can be written. The creator records whether it created
  the root and never deletes caller content as compensation.
- `agent_name` is one safe, non-reserved path segment.
- relative file paths are resolved relative to the request document, not the
  caller's current directory.
- `preset.ref` is an explicit readable JSON/JSONC file. Friendly TUI preset
  names are not portable input.
- `language` selects the creator-owned bundled covenant when no override is
  supplied. Version 1 supports `en`, `zh`, and `wen`.
- `runtime.python` is explicit. If omitted, the creator may use its own
  interpreter only after proving that the required `lingtai-agent` entry point
  and requested channel addon resolve in that same environment.
- `start` defaults to `true`. `false` stops after publication with status
  `configured`.
- every remote chat adapter requires a nonempty allowlist unless its adapter
  contract defines an equally restrictive admission policy.
- unknown top-level or adapter fields fail closed. Silent field ignoring is not
  forward compatibility.

`inspect` and `resume` operate only on creator-authored state. They do not adopt
an unrelated `.lingtai` directory.

## Credential references

A credential reference identifies a source without containing the credential.
Version 1 accepts:

```json
{"env": "TELEGRAM_BOT_TOKEN"}
```

and may later add an OS keychain or inherited file-descriptor source. Raw token
values and token-bearing file paths are not part of the portable request schema.

The adapter reads the credential only while writing its protected sidecar and
performing explicitly requested verification. It must not persist the source
environment-variable name into the agent configuration unless the addon needs
that name at runtime.

## Initial prompt decision

This specification resolves TUI issue #921 for the portable creation path:

- the default initial prompt is **absent**;
- absence is represented by `"initial_prompt": null`;
- the creator does not publish an empty `.prompt` signal file;
- the new bot starts silently and waits for an admitted inbound message;
- the creator never synthesizes greeting or product prose; and
- no `.recipe` tree or TUI recipe-state document is produced.

A caller may provide an explicit prompt:

```json
{"initial_prompt": {"file": "first-message.md"}}
```

The file must be nonempty UTF-8 and at most 256 KiB. Its exact bytes are staged
with secret-equivalent path safety and `0600` semantics as the agent's `.prompt`
before publication and before runtime start. An empty file is rejected as
`initial_prompt_empty`; callers that want no prompt use `null`.

`.prompt` is a transient one-shot runtime signal, not canonical project
configuration or provenance. The transaction records only its size and digest,
never its body. A pre-launch resume may retain the staged file; once process
start is observed or ambiguous, resume must not recreate it because the runtime
may already have consumed it. Prompt input is file/request based and never a
literal argv value.

The interactive TUI may choose to provide a localized initial prompt, but it
must do so as explicit request input. Headless and interactive creation share
one creator contract; neither receives an implicit recipe-derived prompt.

## Covenant decision

The creator distribution owns versioned default covenant assets for `en`, `zh`,
and `wen`. `{"source":"bundled"}` selects the asset matching `language`.

An advanced caller may instead use:

```json
{"covenant": {"file": "covenant.md"}}
```

The selected covenant must be nonempty UTF-8. The result reports its source kind,
creator asset version when bundled, and content digest; it does not echo the
text. The kernel receives the materialized file through its existing public CLI.

The TUI covenant directory is not a fallback source.

## Preset decision

The portable contract accepts one explicit preset file and does not call TUI
preset bootstrap or TUI global state. The creator resolves the path, validates
that the kernel can load it, and records the canonical reference returned by
`lingtai-agent project create`.

The initial manifest policy is least privilege:

- `active`, `default`, and `allowed` contain only the selected preset;
- a caller may provide an explicit versioned preset-policy object to widen
  `allowed`; and
- copying policy from another agent's `init.json` is not a first-class creator
  operation.

A future preset-catalog product may generate request documents. It does not
change this file-level creator contract.

## Runtime discovery and readiness

The creator never assumes `~/.lingtai-tui/runtime/venv`.

Runtime resolution order is:

1. the explicit `runtime.python` request value;
2. the interpreter running LingTai Create, if it owns a matching
   `lingtai-agent` entry point and imports all requested addon modules; or
3. failure with `runtime_unavailable`.

Before any target write, preflight verifies:

- the interpreter exists and is executable;
- the adjacent/platform-appropriate `lingtai-agent` entry point exists;
- the kernel version satisfies the creator's declared compatibility range;
- the project-create JSON protocol version is supported; and
- every requested addon module is importable.

Readiness after launch requires both:

1. an inspectable runtime process for the exact agent directory; and
2. a fresh `.agent.heartbeat` with compatible `.status.json` runtime metadata.

`cmd.Start()`, PID existence alone, or model text is never readiness.

When `verification.runtime_probe` is true, readiness is followed by the public
mailbox probe and a correlated runtime-managed ack. Channel verification occurs
only after runtime reachability succeeds.

## Channel adapter contract

Channel support is a versioned adapter, not a branch inside the transaction
engine. An adapter must provide these operations:

```text
validate(request) -> normalized channel request
stage(agent_dir, normalized request, secret resolver) -> channel manifest
preflight_runtime(runtime) -> verification plan
verify(runtime state, channel manifest) -> verification result
redact(value or error) -> safe text
```

Rules:

- `stage` writes only beneath the staged agent directory.
- It must not launch a process, install software, or contact the network.
- It returns every changed relative path and its non-secret purpose.
- It declares which values are secret and which receipt fields are safe.
- It must merge with unrelated addon/MCP entries rather than replace them.
- It must reject a symlink, reparse point, or special file at a secret target.
- It creates secret directories/files with `0700`/`0600` semantics where the
  platform supports POSIX modes and applies the platform's strongest available
  equivalent elsewhere.
- Verification failures never print credentials or unredacted provider bodies.

### Telegram adapter v1

The first adapter:

- requires one bot token credential reference;
- requires at least one numeric `allowed_users` entry;
- writes `.secrets/telegram.json`;
- adds the `telegram` addon and `mcp.telegram` configuration;
- uses the resolved runtime interpreter, not a TUI-managed path;
- validates the configuration without starting the runtime;
- optionally verifies Bot API identity with a bounded request; and
- verifies one admitted inbound message only as a separate, explicitly
  requested live acceptance step.

Bot API identity is useful channel evidence but does not replace runtime
heartbeat or mailbox-probe evidence.

## Transaction model

Creation is a two-commit state machine. Filesystem publication and process
start cannot be one atomic operation, so the result must never pretend they are.

```text
RECEIVED
  -> PREFLIGHTED
  -> LOCKED
  -> SEEDED
  -> CONFIGURED
  -> VALIDATED
  -> PUBLISHED          # atomic/no-replace filesystem commit
  -> LAUNCHING
  -> READY              # process + fresh heartbeat
  -> RUNTIME_VERIFIED   # optional mailbox probe
  -> CHANNEL_VERIFIED   # optional adapter verification
  -> COMPLETE
```

`start: false` terminates successfully at `PUBLISHED` with status `configured`.

Every transition is written atomically to a versioned transaction document.
The document contains:

- transaction ID and schema version;
- normalized non-secret request fingerprint;
- current and previous phase;
- timestamps;
- project and agent identity;
- created relative paths;
- kernel/creator/adapter protocol versions;
- safe diagnostics;
- whether publication and process start occurred; and
- the next legal resume action.

It contains no credential value, model prompt body, covenant body, child raw
stdout/stderr, or environment snapshot.

## Staging and publication

The creator configures a hidden, same-filesystem staging root. It invokes
`lingtai-agent project create` against that staging root, then applies all
creator and channel configuration there.

Before publication, the canonical transaction document lives in a protected
creator-owned control directory beside the staging root. At `VALIDATED`, a safe
copy is written into the staged `.lingtai/.creator/transaction.json`; after the
no-replace move, that published copy becomes canonical. The control-directory
copy is retained until completion or successful creator-owned staging cleanup.

Before `PUBLISHED`:

- the requested project root has no `.lingtai`;
- the runtime has not started;
- a failed transaction cannot expose a half-configured target project; and
- protected staging state is retained for `resume`.

Publication moves the completed staged `.lingtai` into the requested root with a
same-filesystem, no-replace operation. Before that move, every staged file and
required parent-directory entry is flushed through the platform's durability
primitive. After the move, the target parent directory is flushed where the OS
provides that guarantee. If the platform adapter cannot prove no-replace and
crash-durable publication, the creator fails closed; it does not emulate safety
with check-then-rename.

After publication, the transaction document lives under the published
`.lingtai` tree. A launch, readiness, probe, or channel-verification failure
leaves the project in place and records a resumable state. The creator does not
delete a published project as compensation.

Successful completion may remove only empty creator-owned staging containers.
Failed or ambiguous staging is retained. Explicit abort/deletion is outside
version 1.

## Concurrency

The creator acquires the current stable per-resolved-root TUI spawn lock before
examining the target. It holds that lock through no-replace publication and
releases it before launch/readiness polling.

This transitional choice makes the new creator coordinate with existing
`lingtai-tui spawn` callers. The final TUI wrapper calls LingTai Create, so only
one implementation remains.

Required behavior:

- real path and symlink aliases resolve to one lock identity;
- another cooperating creator waits and then observes the published target;
- owner crash releases the OS lock;
- lock files may remain as stable sidecars;
- a non-cooperating writer cannot be overwritten because publication is
  no-replace; and
- a lost/ambiguous child outcome is `outcome_unknown`, never inferred success.

## Failure and resume contract

A failure document has this minimum shape:

```json
{
  "schema_version": 1,
  "status": "error",
  "code": "channel_validation_failed",
  "phase": "VALIDATED",
  "message": "Telegram configuration could not be validated",
  "transaction_id": "tx-...",
  "recovery": {
    "state": "staged",
    "published": false,
    "process_started": false,
    "resume_supported": true,
    "next_action": "resume"
  }
}
```

Stable error families are:

- `invalid_request`
- `invalid_project_root`
- `already_initialized`
- `runtime_unavailable`
- `kernel_incompatible`
- `preset_invalid`
- `covenant_invalid`
- `initial_prompt_empty`
- `lock_failed`
- `seed_failed`
- `outcome_unknown`
- `channel_validation_failed`
- `publication_failed`
- `launch_failed`
- `process_exited_before_ready`
- `readiness_timeout`
- `runtime_probe_failed`
- `channel_verification_failed`
- `resume_conflict`

`resume` verifies the request fingerprint, transaction phase, target identity,
created-path manifest, and process state before doing work. It never repeats a
completed phase merely because a previous CLI process exited. If staging exists
and the target does not, it resumes from the last durable staging phase. If the
target contains a matching creator transaction and content manifest, it treats
publication as complete even when the control-directory checkpoint still says
`VALIDATED`. If both locations exist, identities differ, or the published
manifest cannot be verified, it returns `outcome_unknown` without mutation.
A process-start boundary is reconciled from the exact agent lock, heartbeat,
status, and process identity; PID existence alone is insufficient.

A rotated credential is permitted during resume. Credential bytes are excluded
from the immutable fingerprint, while the adapter's non-secret policy and
credential-source kind remain included.

## Success result

A completed result has this minimum shape:

```json
{
  "schema_version": 1,
  "status": "ready",
  "phase": "COMPLETE",
  "transaction_id": "tx-...",
  "project_dir": "/path/to/project",
  "agent_name": "example-bot",
  "agent_dir": "/path/to/project/.lingtai/example-bot",
  "preset_ref": "/path/to/preset.json",
  "runtime": {
    "started": true,
    "pid": 12345,
    "inspectable_process_confirmed": true,
    "heartbeat_confirmed": true,
    "probe": "passed"
  },
  "channel": {
    "type": "telegram",
    "configured": true,
    "identity_verification": "passed"
  },
  "warnings": []
}
```

`status: "configured"` is the only other successful creation status and means
`start: false`; it must not include a PID or readiness claim.

A result path is local machine data. Public logs, issue comments, and examples
must replace it with a placeholder.

## TUI compatibility and migration

The TUI remains a consumer, not the owner, of the portable transaction.

### Headless wrapper

`lingtai-tui spawn` becomes a thin compatibility adapter:

1. resolve its friendly preset selection into an explicit preset file;
2. construct a version-1 creator request;
3. invoke `lingtai-create create --json`;
4. map the creator result into the existing TUI JSON schema;
5. register the project in TUI-local global state only after creator success;
6. report registration failure as TUI-local recovery, never as creator failure.

It must not seed files, apply recipes, start a second process, or independently
poll readiness.

### Interactive creator

The interactive TUI builds the same request and displays creator phases. It may
supply an explicit localized initial prompt. It may maintain drafts before
invocation, but final project publication belongs to LingTai Create.

### Removed portable dependencies

The creator path does not run or write:

- TUI global migrations;
- TUI preset bootstrap;
- the TUI covenant directory;
- `.recipe` bundles;
- `.lingtai/.tui-asset/recipe-state.json`;
- TUI utility-library population; or
- TUI project registry entries.

If the TUI needs those for its own UI, it performs them outside the portable
creator result and cannot make them requirements for a healthy bot.

### Existing helper

The bundled `headless-bot` helper becomes a thin request generator for
`lingtai-create`. Its direct `init.json`, secret, `.refresh`, and
`lingtai-tui spawn/list` implementation is removed after parity acceptance.

The skill continues to own safety guidance and examples, but the executable is
the single behavior source.

## Security invariants

1. No credential value appears in argv, stdout, stderr, logs, receipts, process
   titles, exception strings, or request fingerprints.
2. Secret targets are contained beneath the staged agent directory and are
   never followed through symlinks/reparse points.
3. A remote chat channel fails closed without an explicit admission policy.
4. The creator never broadens preset access implicitly.
5. The creator never installs or upgrades code as part of create/resume.
6. All child output is bounded and redacted before inclusion in diagnostics.
7. Static validation finishes before filesystem publication.
8. Runtime start happens only after publication.
9. Readiness requires process plus fresh heartbeat; model output is irrelevant.
10. The creator never compensates for an uncertain outcome by deleting the
    target.
11. Dry-run/preflight performs no target write, process start, network request,
    or credential read.
12. Public docs and test fixtures use placeholders, not realistic token-shaped
    dummy values.

## Acceptance matrix

The implementation is not ready for TUI cutover until all of these pass.

### Contract

- request version, required fields, unknown fields, and relative-path base;
- one stdout success document / one stderr error document;
- exact stable status, phase, error, and recovery fields;
- no raw child stream leakage;
- `start: false` returns `configured` without launch.

### Filesystem and recovery

- no target `.lingtai` before publication;
- same-filesystem no-replace publication;
- failure injection at every phase;
- pre-publication resume from retained staging;
- post-publication resume without repeating creation;
- ambiguous child outcome remains `outcome_unknown`;
- no adoption or deletion of unrelated files;
- real-path/symlink contention and owner-crash lock release.

### Security

- token absent from argv, output, logs, receipt, exception, and process listing;
- secret mode/ACL checks on every supported OS;
- symlink/reparse/special-file rejection;
- required allowlist;
- preset least privilege;
- redaction under malformed provider responses and child failures.

### Runtime

- no `lingtai-tui` executable or `~/.lingtai-tui` directory present;
- explicit runtime selection;
- missing/incompatible kernel and missing addon fail before publication;
- successful long-lived child after creator exit;
- duplicate-process prevention;
- heartbeat readiness, early exit, and timeout;
- correlated mailbox probe;
- native Darwin, Linux, Windows, and WSL2 coverage.

The WSL2 acceptance must explicitly cover the long-lived-child scenario tracked
by [TUI issue #904](https://github.com/Lingtai-AI/lingtai/issues/904); it must not
be closed from source inspection alone.

### Telegram

- valid request produces the intended addon/MCP/sidecar configuration;
- unrelated addon/MCP entries survive merging;
- missing/empty allowlist fails;
- missing credential source fails without disclosure;
- optional Bot API identity verification is bounded and redacted;
- one real admitted-user round trip is documented as a manual release gate;
- one non-admitted-user rejection is documented as a manual release gate.

### Migration

- TUI headless and interactive paths call the creator rather than duplicate it;
- `lingtai-tui spawn` preserves its documented JSON compatibility fields;
- TUI registration failure cannot alter creator success state;
- no portable path writes recipe state or applies a recipe;
- the old helper contains no `lingtai-tui spawn/list`, direct config patch, or
  `.refresh` implementation;
- documentation and Anatomy links identify one creator behavior source.

## Implementation order

One implementation program may contain multiple reviewable PRs, but it follows
this order:

1. **Standalone contract engine and transaction** in `Lingtai-AI/lingtai-create`:
   request/result schemas, preflight, staging, no-replace publication, state,
   inspect/resume, and fake adapters.
2. **Telegram vertical slice:** secure sidecar/config merge, runtime discovery,
   launch/readiness, mailbox probe, identity verification, and failure matrix.
3. **TUI cutover:** interactive and headless wrappers, explicit initial-prompt
   input, TUI-local registration, recipe/state removal from creation, and JSON
   compatibility tests.
4. **Helper and documentation cutover:** thin request generator, deprecation
   notes, cross-platform acceptance, and release gates.

The first creator PR must establish a reviewable public CLI and transaction
schema before channel behavior grows. The first TUI PR must depend on a released
or exact-commit creator contract; it must not vendor a private copy.

## Versioning and compatibility

- Request, result, transaction, and adapter schemas each version independently.
- A new reader may accept older versions explicitly; it never assumes unknown
  fields are harmless.
- A breaking public CLI or schema change increments its version and ships a
  migration note.
- The TUI wrapper pins a compatible creator range and reports mismatch before
  target writes.
- Kernel CLI protocol compatibility is checked during preflight.
- Existing projects are not rewritten by this migration.
- Existing `lingtai-tui spawn` callers retain their JSON surface through the
  compatibility window.

The compatibility window ends only after one released TUI version has used
LingTai Create for both interactive and headless creation and the acceptance
matrix is green on all required platforms.

## Deferred decisions

These are intentionally deferred because they are not required for a safe
version-1 creator:

- adding agents to an existing project;
- arbitrary project adoption or repair;
- a remote preset catalog;
- an interactive standalone UI;
- automatic dependency installation;
- non-bot project templates;
- generalized stop/suspend/CPR administration; and
- automatic deletion/abort of retained transactions.

They must extend the versioned contract rather than bypass it.

## Definition of done

The migration is complete when:

1. a Telegram bot can be created, configured, launched, and verified on every
   required platform with no TUI installation or TUI state;
2. failures at every phase produce a truthful resumable receipt;
3. the TUI's interactive and headless creation paths invoke that same creator;
4. kernel code still contains only reusable project/runtime primitives;
5. the bundled helper contains no independent creation logic;
6. recipe/state behavior is absent from project creation;
7. the initial-prompt default is silent and explicit;
8. public JSON compatibility and security gates pass; and
9. the old duplicated paths are removed or reduced to documented wrappers.

Until those conditions hold, the current
[`headless-runtime-contract.md`](headless-runtime-contract.md) remains the
implemented TUI controller contract, while this document remains the target
specification.
