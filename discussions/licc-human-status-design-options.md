# LICC human-status events — design options

Companion design doc for [Lingtai-AI/lingtai#146](https://github.com/Lingtai-AI/lingtai/issues/146).
Status: **design-options draft for review.** No code in this PR.

## TL;DR

We want the agent to say to a human, while it is about to start a long tool call, "I'm starting X, this will take a while" — without faking a final reply, without inventing per-tool messaging args, and without a hidden "same as last human" default.

This doc compares four options and recommends **B + D** for v1.

| Option | Surface                                       | Routing                                  | Cost     | Forward fit         |
|--------|-----------------------------------------------|------------------------------------------|----------|---------------------|
| **A**  | Side-channel metadata on tool calls           | Explicit `target` in envelope            | Med-high | Fits C later        |
| **B**  | Intrinsic `human_status.emit` tool            | Explicit `target` in args                | **Low**  | Fits C later        |
| **C**  | General ReAct/activity event stream           | Subscription + per-event audience        | High     | Native long-term    |
| **D**  | B/A scoped to `current_notification_source`   | Implicit — block the agent is reading    | **Lowest** | Constrained, safe |

## 1. Context

Restated so this reads cold:

- **Goal:** human-visible status, agent-emitted, around long tool work. Not a final reply. Not agent mail.
- **Default:** no emission. Each one is explicit.
- **No silent "last human" routing.** Convenience constants must be opt-in and visible.
- **Runtime/LICC owns** length cap, redaction, rate-limit, debounce, audit.
- **MCPs are delivery adapters** — they subscribe and translate; they do not each implement safety policy.

**What already exists** (kernel paths cited so the doc can be verified):
- `system(action="notification")` wire pair with per-source entries (`email`, `soul`, `system`, `mcp.<name>`) carrying `source`, `data{…}`, producer-side `instructions`, kernel-attached `_notification_guidance`. Plumbing: `lingtai-kernel/src/lingtai_kernel/meta_block.py`, `lingtai-kernel/src/lingtai_kernel/base_agent/__init__.py:_sync_notifications`.
- **MCP → agent** LICC v1 already ships: MCPs drop events into `.mcp_inbox/`; `lingtai-kernel/src/lingtai/core/mcp/inbox.py` writes `publish_notification(workdir, f"mcp.{name}", …, data={…, previews:[{chat_id, message_id, …}]})`. Reference impl: `lingtai-telegram/src/lingtai_telegram/licc.py`.
- **Agent → MCP** direction does **not** ship today. No "outbox" plumbing.

## 2. The four options

### Option A — tool-call metadata side-channel

**Shape.** Carry a `human_status` block at the tool-call envelope level alongside `args`:

```jsonc
{ "tool": "bash.run",
  "args": { "command": "..." },
  "human_status": { "target": {...}, "text": "...", "mode": "status" } }
```

Kernel strips it before forwarding `args`; broker routes it.

- **Routing.** Explicit `target` in metadata. Alias table resolves opaque conversation IDs (see §4 Open Q).
- **MCP delivery.** Broker writes `.mcp_outbox/<mcp>/<uuid>/event.json` (new convention, symmetric to `.mcp_inbox/`); MCPs poll and deliver.
- **Safety.** Single chokepoint in broker: length cap, secret/path redaction, rate-limit, dup-suppress, audit log.
- **Pros.** Status really emits *during* the same turn as the tool call — no extra round. Forward-compatible with C.
- **Cons.** Requires every LLM adapter (Anthropic, OpenAI, Codex, Gemini) to carry envelope-level metadata uniformly — we don't have that portably today. LLMs reliably emit tool calls; they less reliably attach side-channel JSON the prompt doesn't enforce.
- **Cost.** **Medium-high.** Driven by the portable-metadata problem, not routing.
- **Migration.** Envelope becomes one row in a future C stream. No rename.
- **Tests.** Redaction rules; dup-suppress keying; fake MCP subscriber asserts alias delivery; prompt-inject for raw IDs.

### Option B — intrinsic `human_status.emit` tool

**Shape.** A small intrinsic, sibling of `email`/`daemon`, called *before* the long tool call (or in parallel where the adapter supports it):

```jsonc
{ "tool": "human_status.emit",
  "args": { "target": {...}, "text": "...", "mode": "status", "ttl_seconds": 120 } }
```

Returns `{status, event_id, delivered_to, route_summary, delivered_at, redactions_applied}` synchronously (broker queues; no LLM round-trip on the MCP side).

- **Routing.** Explicit `target`, same as A. A convenience `target: "current_notification_source"` overlaps with D.
- **MCP delivery.** Same outbox-file convention as A.
- **Safety.** Identical to A — the intrinsic is a thin façade; all policy lives in the broker.
- **Pros.** **Lowest portable cost.** Tool calls work uniformly across every LLM adapter we ship. Intent is legible in the transcript ("I called `human_status.emit` then `bash.run`"). Discoverable through the normal tool schema. Pairs cleanly with D as a routing constraint.
- **Cons.** Costs one extra tool-call slot. On adapters that support parallel tool calls this is free; on serial ones it's one extra hop. The #146 phrasing "while calling the tool" weakens slightly — practically fine.
- **Cost.** **Low.** New intrinsic + broker module + outbox convention + one MCP subscriber (Telegram). Zero LLM-adapter changes.
- **Migration.** Becomes one producer of a future C stream. Same wire envelope.
- **Tests.** Schema validation; broker queue + redaction + rate-limit + dup-suppress; Telegram subscriber end-to-end; negative (no `target` → reject); default-off assertion.

### Option C — general ReAct activity-event stream

**Shape.** A broader event family (`agent.started_tool`, `agent.finished_tool`, `agent.human_status`, `agent.needs_attention`, `agent.checkpoint`, …) with a uniform envelope:

```jsonc
{ "type": "agent.<kind>", "agent": "...", "created_at": "...",
  "correlation": {...}, "payload": {...},
  "audience": { "human": true|false, "agent_peers": false, "log_only": false } }
```

Subscribers (MCPs, TUI, portal, telemetry) declare what they handle; broker routes by capability + audience.

- **Routing.** Mostly subscription-driven, plus per-event `target` overrides where appropriate.
- **MCP delivery.** MCPs declare richer subscription manifests. Telegram subscribes to `agent.human_status` with capabilities `[send, edit, typing, placeholder]`.
- **Safety.** Audience gate before dispatch. `audience.human=true` runs the full redaction pipeline.
- **Pros.** The architecturally right answer long-term. Future-proofs portal/TUI activity rendering, telemetry, replay, external observability. Human-status stops being a special case.
- **Cons.** Multi-week design + ship. Event taxonomy, subscription model, capability declarations, audience semantics, durability — all decisions we are not set up to make in days. Telegram demo delayed.
- **Cost.** **High.**
- **Migration.** Whatever ships as v1 should already speak `agent.human_status` in the wire format so C ingests it day one.
- **Tests.** Per-event-type units; subscription registration races; audience-gate end-to-end; backpressure.

### Option D — v1 scoped to `current_notification_source`

**Shape.** Same intrinsic surface as B, but `target` is a single sentinel:

```jsonc
{ "tool": "human_status.emit",
  "args": { "target": "current_notification_source", "text": "...", "mode": "status" } }
```

Kernel resolves the sentinel against the **active notification block** the agent is currently responding to. That block already carries `source` (e.g. `mcp.telegram`) and `data.previews[].{chat_id, message_id}` — the route is right there.

If the agent isn't currently responding to a notification (autonomous loop, scheduled fire, post-molt warm boot) → `{status: "no_route"}`. **No silent guess.**

- **Routing.** Implicit but auditable. The kernel records the resolved `(source, chat_id, message_id)` so a reviewer can verify it matched the block the agent was reading.
- **MCP delivery.** The route's `source` (e.g. `mcp.telegram`) names the producing MCP — natural delivery target. No subscription manifest required in v1 (added later for fan-out).
- **Safety.** **Strongest v1 story** of the four:
  - No agent-facing route identifiers — no alias table needed.
  - No arbitrary `target` to abuse via prompt injection.
  - No "default-to-last" magic — the only route is the block the agent is staring at.
  - Standard redaction/length-cap/rate-limit/dup-suppress all still apply.
- **Pros.** Smallest blast radius. Hardest to leak across conversations. No alias-table design work. Compatible with how MCP-bridged messages already arrive. Days-to-demo, not weeks.
- **Cons.** No autonomous status updates (no current notification → no route). No status while idle / mid-loop on background work. Single-source per emit (no fan-out).
- **Cost.** **Lowest.** Resolver helper + intrinsic + broker module + Telegram outbox subscriber. The outbox path is symmetric to existing `.mcp_inbox/` plumbing.
- **Migration.** **The wire-format event is identical to A/B/C.** Only the resolver differs. Phase v2 can add explicit `target` behind a per-preset capability with the sentinel as the default. Phase v3 can lift the broker into a C-shaped subscription bus. No phase invalidates earlier emissions or renames the schema.
- **Tests.** Resolver returns the right tuple for various notification shapes; returns `no_route` when none active; integration with Telegram MCP; negative for stale post-molt notifications; default-off; prompt-inject attempts at routing.

## 3. Hybrid / phase plan

| Phase | Surface | Ship gate |
|-------|---------|-----------|
| **v1** | **B + D**: intrinsic `human_status.emit`; only legal `target` is `"current_notification_source"`. Telegram subscriber. Wire event = `agent.human_status`. | One end-to-end Telegram demo; redaction tests; audit log. |
| v1.1  | Add `target: "self_email"` sentinel (status into agent's own notification email path). Covers self-paced autonomous work. | Works for a scheduled agent with no Telegram session. |
| v2    | Explicit `target: {channel, conversation}` behind per-preset capability `human_status.explicit_target`. Adds opaque alias table. | Alias-table review with ≥2 MCPs (Telegram + Feishu). |
| v3    | Promote broker into a C-style subscription bus. Existing emissions keep working. | Two non-producer MCPs subscribe to the same event. |

## 4. Recommendation

**Ship B + D as v1.**

Concretely:
1. Land intrinsic `human_status.emit` (B's surface).
2. In v1, the only legal `target` is `"current_notification_source"` (or omitted, treated as that). Any other value → reject with a clear error.
3. Define the wire event as `agent.human_status` with the envelope below — exactly the shape C would use.
4. Implement the broker as a single module (e.g. `lingtai_kernel/intrinsics/human_status/`) mirroring `intrinsics/email/`: redaction, length cap, rate-limit, dup-suppress, audit log.
5. Define agent → MCP outbox at `.mcp_outbox/<mcp_name>/<uuid>/event.json`, symmetric to existing `.mcp_inbox/`.
6. Wire Telegram MCP as the v1 subscriber: read the outbox in its existing poll loop, edit/send, dead-letter on failure.
7. Schema-design every artifact so a future C subscription bus is a pure superset.

Defer: arbitrary explicit `target` (v2, gated on capability + alias-table review); multi-MCP fan-out subscriptions (v3); cross-MCP placeholder/edit semantics (v3).

**Rationale.** D's closed route surface eliminates the riskiest design surface — arbitrary routing under prompt-injection adversaries — without losing ergonomics. B's intrinsic-tool shape is what every LLM adapter already supports portably. Together they ship in days and are forward-compatible with both an explicit-target v2 and a subscription-bus v3.

## 5. Wire and on-disk schemas

### LICC event envelope (broker → MCP outbox)

```jsonc
{ "licc_version": "1",
  "event_id":     "hs_<uuid>",
  "type":         "agent.human_status",
  "agent":        "mimo-1",
  "created_at":   "2026-05-21T10:40:00Z",
  "route": {
    "source":       "mcp.telegram",
    "account":      "default",
    "conversation": "<chat_id from notification preview>",
    "message_ref":  "<message_id from notification preview>"   // optional, enables edit
  },
  "payload": {
    "text":        "...",
    "mode":        "status",            // status | placeholder | final-status
    "ttl_seconds": 120                  // best-effort; MCP may ignore
  },
  "correlation": { "turn_id": "...", "tool_call_id": "...",
                   "notification_block_id": "..." },
  "audit": { "redactions_applied": [...], "rate_limit_state": {...} } }
```

### Intrinsic tool-call shape (LLM → kernel)

```jsonc
{ "tool": "human_status.emit",
  "args": { "target": "current_notification_source",
            "text":   "...", "mode": "status", "ttl_seconds": 120 } }
```

Returned tool result (lands in chat history):

```jsonc
{ "status": "delivered",          // delivered | queued | no_route | rate_limited | redacted
  "event_id": "hs_<uuid>", "delivered_to": "mcp.telegram",
  "route_summary": "telegram:<alias>",   // never raw chat_id
  "delivered_at": "...", "redactions_applied": [] }
```

### Audit log (`logs/human_status.jsonl`)

```jsonc
{ "ts": "...", "agent": "...", "event_id": "...",
  "route": { "source": "mcp.telegram", "alias": "<alias>" },
  "text_hash": "sha256:...", "text_len": 38,    // raw text NOT in audit
  "status": "delivered", "redactions_applied": [],
  "correlation": { "turn_id": "...", "tool_call_id": "..." } }
```

Audit logs the hash, not the text — emissions can contain user-visible content the user later wants forgotten; an immutable per-message log is a privacy footgun. Raw text lives in the outbox only until delivery completes.

## 6. Acceptance criteria (v1)

1. Agent can call `human_status.emit` to produce a human-visible status update before/during long-running work.
2. v1 only accepts `target: "current_notification_source"` (or omitted). Any other value → `{status: "no_route"}` with descriptive error.
3. Event routes through LICC to the MCP that produced the active notification.
4. Emission is **not** treated as the final reply — chat history shows tool call + tool result, not an assistant message.
5. No emission unless the agent calls the intrinsic.
6. Kernel applies: length cap (default 280 chars, configurable); secret-regex redaction; absolute-path redaction; rate limit (default `min_interval=2s` per route, `max_per_turn=5`); dup-suppress on `(agent, route, fingerprint(text))`.
7. Telegram demonstrates the flow end-to-end: notification arrives → agent calls emit → Telegram MCP edits the placeholder (or fresh send) in the same chat.
8. Audit log line per attempt (delivered, redacted, rate-limited, no_route).
9. Post-molt: stale notification block does **not** resolve to a valid route. Resolver re-checks against the current heartbeat's active notification.

## 7. Open questions

- **Route identifiers (v2+).** Opaque aliases stored at `system/route_aliases.json` keyed by `<channel>/<alias>` → `{account, conversation_id, last_used_at}`; raw chat IDs never enter LLM context. v1 (D): not applicable.
- **Recent routes in prompt.** v1: not surfaced (no agent-facing routing). v2: aliases — not raw IDs — appear in the notification block's `data` so the agent can pick.
- **Transcript visibility.** Emit's tool-result block lives in chat history (`{status, event_id, route_summary, delivered_at}`). Whether to render specially in TUI/portal is a follow-up.
- **Placeholder/edit lifecycle.** Per-platform primitives differ (Telegram `edit_message`, Feishu card-update, IMAP nothing useful). v1: Telegram uses existing MCP placeholder helper or plain `send`. v2+: MCPs declare capabilities; broker prefers richest available; `mode` hints the MCP.
- **Multi-update collapse.** Broker-level debounce keyed on `(agent, route, correlation.turn_id)`. One outbox entry per (turn, route); subsequent emits update it. Defaults: `min_interval=2s`, `max_per_turn=5`, per-preset overridable.
- **Per-MCP UX helpers (typing, seen/done).** Orthogonal — MCP-owned. MCPs may also flip their indicators on receiving a `human_status` event; that's MCP-implementation choice, not a runtime contract.
- **IMAP.** v1: no — email is too heavyweight for short pings; bounce/throttling cost is real. v3: IMAP can subscribe with high debounce thresholds if a use case lands.
- **Autonomous/scheduled emissions.** v1 (D): not supported. v1.1: `self_email` sentinel. v2: explicit targets for "ping the project channel" cases, gated on alias-table capability.
- **Replay/durability.** Audit log is append-only and survives molt. Outbox dead-letters on delivery failure. Replay-from-log: out of scope for v1; natural under v3 subscription bus.

## 8. Out of scope for this doc

Module layout (`intrinsics/human_status/` vs `core/human_status/`); whether `mode` is enum vs free-form string; Telegram-specific placeholder semantics (owned by `lingtai-telegram`); whether audit lives at `logs/` vs `.notification/`; portal/TUI rendering; i18n.

## 9. References

- Issue: [Lingtai-AI/lingtai#146](https://github.com/Lingtai-AI/lingtai/issues/146).
- LICC v1: `lingtai-kernel/src/lingtai/core/mcp/inbox.py`; `lingtai-telegram/src/lingtai_telegram/licc.py`.
- Notification plumbing: `lingtai_kernel/meta_block.py`; `base_agent/__init__.py:_sync_notifications`; `base_agent/messaging.py`.
- Producer/source pattern (`source=mcp.<name>`): `lingtai-kernel/src/lingtai/core/mcp/inbox.py:_publish_inbox_notification`; `lingtai-kernel/src/lingtai_kernel/intrinsics/email/ANATOMY.md`.
- Producer `instructions` + kernel `_notification_guidance` precedent: `lingtai_kernel/intrinsics/soul/ANATOMY.md`.
