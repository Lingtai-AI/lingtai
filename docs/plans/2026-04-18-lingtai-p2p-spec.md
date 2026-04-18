# lingtai-p2p: Peer-to-Peer Agent Communication

**Date:** 2026-04-18
**Status:** Design Spec

## Summary

A lightweight Python package (`lingtai-p2p`) that gives LingTai agents the ability to communicate directly over the internet via QUIC. Each agent has an Ed25519 keypair identity, generates invite strings for peer discovery, and authenticates connections via a simple handshake. No central server, no registration, no provider — just two agents with each other's invite string.

## Motivation

LingTai agents communicate via filesystem-based email. This works on a single machine but cannot span multiple machines. The kernel already has a `mode` parameter on the email tool (`rel`/`abs`/`net`). This package implements the `net` transport: authenticated, encrypted, direct agent-to-agent messaging over the internet.

Design philosophy: ephemeral connections, no persistence infrastructure, free to use, decentralized. Connections break when IPs shift; re-invite to reconnect. This is intentional — persistence requires infrastructure, and infrastructure centralizes. Ephemeral is the honest trade-off for zero-cost decentralization.

## Architecture

```
┌──────────────────────────────────────────────┐
│                lingtai-kernel                 │
│  mail(action="send", mode="net", ...)        │
│       │                                      │
│       ▼                                      │
│  ┌──────────────────────────────────────┐    │
│  │           lingtai-p2p                │    │
│  │                                      │    │
│  │  identity.py   — Ed25519 keypairs    │    │
│  │  invite.py     — invite encode/decode│    │
│  │  handshake.py  — HELLO/WELCOME       │    │
│  │  node.py       — QUIC listener/dialer│    │
│  │  wire.py       — message framing     │    │
│  └──────────────────────────────────────┘    │
│       │                                      │
│       ▼                                      │
│    aioquic (QUIC + TLS 1.3)                  │
│       │                                      │
│       ▼                                      │
│    UDP socket ←→ internet ←→ peer agent      │
└──────────────────────────────────────────────┘
```

## 1. Identity

### Keypair Generation

Each agent gets an Ed25519 keypair. Generation is triggered by a `.gen_key` signal file (same pattern as `.sleep`, `.suspend`):

1. Human or system drops `.gen_key` in the agent's working directory
2. Agent detects signal, generates Ed25519 keypair
3. Private key → `.agent.key` (PEM format, never shared, never leaves machine)
4. Public key → `public_key` field added to `.agent.json` (base58-encoded)
5. `.gen_key` removed

Re-dropping `.gen_key` regenerates — old identity is gone. This enables ephemeral keys for one-off interactions.

Agents without keys work exactly as today. Keys are opt-in.

### File Layout

```
.lingtai/agent_name/
├── .agent.json          # existing manifest, gains "public_key" field
├── .agent.key           # NEW: Ed25519 private key (PEM)
├── .gen_key             # signal: triggers keypair generation
└── mailbox/
    └── contacts.json    # existing contacts, gains freeform fields
```

### `.agent.json` Extension

```json
{
  "agent_name": "agent_auditor",
  "address": "agent_auditor",
  "state": "active",
  "admin": {},
  "public_key": "ed25519:<base58-encoded-public-key>"
}
```

The `public_key` travels with every email in the `identity` field, so recipients learn the sender's key automatically.

## 2. Contacts & Trust

### Freeform Contact Entries

`mailbox/contacts.json` entries become freeform dicts. Required fields: `address`, `name`. Everything else is optional, agent-defined.

```json
[
  {
    "address": "agent_b",
    "name": "Agent B",
    "note": "Bob's research agent",
    "public_key": "ed25519:<base58>",
    "met_at": "2026-04-18",
    "trust_level": "friend"
  }
]
```

A contact with a `public_key` is a trusted peer for P2P connections. The kernel and TUI read fields they recognize, ignore the rest. Agents can store arbitrary metadata — the contact list is their personal CRM.

### Whitelist Semantics

- Only contacts with a `public_key` can establish P2P connections
- Incoming connections from unknown keys are rejected
- The handshake flow (Section 4) adds contacts automatically on successful nonce verification

## 3. Invite Format

### Invite String

```
lingtai://<base58-pubkey>?addr=<ip:port>&nonce=<hex>&name=<display-name>&exp=<unix-timestamp>
```

Example:
```
lingtai://7Hs9xK3mP2...?addr=203.0.113.5:4001&nonce=a1b2c3d4e5f6&name=Alice%27s+Research+Agent&exp=1713571200
```

Fields:
- `<base58-pubkey>` — the inviting agent's Ed25519 public key (path component)
- `addr` — reachable address (ip:port). Multiple `addr` params allowed for fallback
- `nonce` — random hex string, single-use authorization token
- `name` — human-readable display name (URL-encoded)
- `exp` — expiry as unix timestamp (optional, default: 7 days from generation)

### Invite Generation

The agent generates an invite via a new mail action:

```
mail(action="invite")
```

Returns:
```json
{
  "invite": "lingtai://7Hs9xK3...",
  "expires_in": "7 days",
  "note": "Share this string with the peer you want to connect to"
}
```

The agent stores the nonce locally in a pending-invites list (in-memory or `mailbox/pending_invites.json`). The nonce is consumed on first successful handshake.

### Invite Properties

- **Single-use**: first peer to complete the handshake claims the nonce
- **Expiring**: default 7 days, configurable
- **Channel-agnostic**: shared via Signal, email, QR code, spoken aloud, pasted in TUI

## 4. Handshake Protocol

### Wire Handshake (over QUIC)

After QUIC connection is established (TLS 1.3 encrypts the channel), the two agents exchange identity:

```
Dialer (Bob)  →  HELLO  →  Listener (Alice)
Dialer (Bob)  ←  WELCOME  ←  Listener (Alice)
```

**HELLO message** (Bob → Alice):
```json
{
  "type": "hello",
  "public_key": "ed25519:<bob's-base58-pubkey>",
  "nonce": "a1b2c3d4e5f6",
  "nonce_signature": "<bob-signs-nonce-with-his-private-key>",
  "name": "Bob's Agent"
}
```

**WELCOME message** (Alice → Bob):
```json
{
  "type": "welcome",
  "public_key": "ed25519:<alice's-base58-pubkey>",
  "name": "Alice's Research Agent"
}
```

### Verification Flow (Alice's side — listener)

1. Receive HELLO from Bob
2. Verify `nonce` is in pending invites, not expired, not consumed
3. Verify `nonce_signature` was signed by Bob's `public_key` (proves Bob holds the private key)
4. Add Bob to `contacts.json` with his `public_key`
5. Mark nonce as consumed
6. Send WELCOME back with own public key
7. Connection ready for messages

### Verification Flow (Bob's side — dialer)

1. Parse invite string → extract Alice's public key, address, nonce
2. Dial Alice's address via QUIC
3. Send HELLO with own public key + nonce + signature
4. Receive WELCOME from Alice
5. Verify Alice's `public_key` matches what was in the invite string
6. Add Alice to `contacts.json` with her `public_key`
7. Connection ready for messages

### Subsequent Connections

After the first handshake, both agents have each other in contacts. Future connections:

1. Dial using the address in contacts (or a fresh invite if address changed)
2. HELLO without nonce — just public key exchange
3. Verify public key matches contacts entry
4. Connected

No nonce needed for known peers. The nonce is only for first introductions.

## 5. Transport

### QUIC via `aioquic`

Each agent with keys runs a QUIC listener on a configurable port (default: auto-assigned by OS). The QUIC layer provides:

- Encrypted channel (TLS 1.3 built-in)
- Reliable ordered delivery
- UDP underneath (better NAT traversal than TCP)
- Stream multiplexing
- Fast connection establishment

### Message Framing (over QUIC stream)

Messages are length-prefixed JSON over a QUIC stream:

```
┌────────────┬──────────────────────────┐
│ length (4) │ JSON payload             │
│ big-endian │ (message.json content)   │
└────────────┴──────────────────────────┘
```

The JSON payload is a standard LingTai mail message — same schema as `mailbox/inbox/{uuid}/message.json`.

### Node Lifecycle

- Agent starts → if `.agent.key` exists, start QUIC listener
- Agent receives `.gen_key` → generate keys, start listener
- Agent sends `mode="net"` mail → open QUIC connection to peer, handshake if needed, send message
- Agent shuts down → close all connections, stop listener

### Port Management

- Default: OS auto-assigns port (0 → random available port)
- Agent records its listening port (for invite generation)
- Configurable via `init.json` if the user wants a fixed port

## 6. Kernel Integration

### `mode="net"` Delivery Path

```python
# In mail intrinsic _send()
if mode == "net":
    # Send directly via lingtai-p2p, no outbox queue
    result = agent._p2p_node.send(address, payload)
```

The three delivery paths in the kernel:

| Mode | Path | Mechanism |
|------|------|-----------|
| `rel` | Resolve name → write to local inbox | Filesystem |
| `abs` | Use path as-is → write to inbox | Filesystem |
| `net` | Dial peer → QUIC stream → send | Network |

### Signal Handling

`.gen_key` is handled in the agent's signal-check loop alongside `.sleep`, `.suspend`, `.prompt`, `.inquiry`:

```python
gen_key_path = working_dir / ".gen_key"
if gen_key_path.exists():
    generate_keypair(working_dir)
    gen_key_path.unlink()
    start_p2p_node()
```

### New Mail Actions

Two new actions added to the mail tool:

- `mail(action="invite")` — generate invite string for this agent
- `mail(action="connect", invite="lingtai://...")` — connect to a peer using their invite

## 7. Package Structure

```
lingtai-p2p/
├── pyproject.toml              # package metadata, dependency: aioquic
├── README.md
├── src/lingtai_p2p/
│   ├── __init__.py             # public API: P2PNode, Identity, Invite
│   ├── identity.py             # Ed25519 keypair generation, load, store
│   ├── invite.py               # invite string encode/decode, nonce management
│   ├── handshake.py            # HELLO/WELCOME protocol, peer verification
│   ├── node.py                 # QUIC listener/dialer, connection management
│   └── wire.py                 # length-prefixed JSON framing
└── tests/
    ├── test_identity.py        # keypair gen, load, store, regeneration
    ├── test_invite.py          # encode/decode, nonce lifecycle, expiry
    ├── test_handshake.py       # HELLO/WELCOME, nonce verification, rejection
    ├── test_node.py            # QUIC connect/listen, message send/receive
    └── test_wire.py            # framing encode/decode
```

### Dependencies

```
aioquic (BSD license)
└── cryptography (transitive, Apache 2.0 + BSD) — provides Ed25519
```

One declared dependency.

## 8. What Changes Where

| Component | Change |
|-----------|--------|
| **NEW: `lingtai-p2p` package** | Standalone package, own repo, own PyPI release |
| Kernel `mail.py` | `mode="net"` sends via `lingtai-p2p` directly (no outbox) |
| Kernel `mail.py` | New actions: `invite`, `connect` |
| Kernel agent init | Handle `.gen_key` signal, manage P2P node lifecycle |
| Kernel `base_agent.py` | Hold `_p2p_node` instance, start/stop with agent |
| i18n files | Add descriptions for `invite`/`connect` actions (en/zh/wen) |
| TUI `internal/postman/` | Obsolete — keep for reference |
| TUI address parsing | Already done (Phase 1 — keep as-is for display) |
| `contacts.json` schema | Becomes freeform (backward compatible) |

## 9. What's Out of Scope (v1)

- **Relay/fallback**: direct connections only. If you can't reach the peer, retry or re-invite.
- **NAT hole punching**: rely on QUIC's UDP-based traversal where it works. No STUN/TURN.
- **Persistent connections**: connections are ephemeral. IP shifts = reconnect.
- **Discovery**: manual invite sharing only. No DHT, no registry.
- **Multi-recipient**: one peer per connection. Broadcast is not supported.
- **Attachments over P2P**: text messages only. Files stay local.
- **Key backup/recovery**: lose your key, lose your identity. Generate a new one.

## 10. Success Criteria

1. Two agents on two machines can exchange messages via `mode="net"`
2. Invite string shared out-of-band enables one-click connection
3. Unauthorized peers (no invite, no contact entry) are rejected
4. Connection is encrypted (QUIC TLS 1.3)
5. Agent identity is cryptographically verifiable (Ed25519)
6. Zero registration, zero infrastructure, zero cost
7. Existing `mode="rel"` and `mode="abs"` behavior unchanged
