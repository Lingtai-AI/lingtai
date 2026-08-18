#!/usr/bin/env python3
"""Gatekeeper reference contract for lingtai #864 (first vertical slice).

Implements the repo-ownable core of the kernel risky-action gate feature
requested in https://github.com/Lingtai-AI/lingtai/issues/864, exactly as the
issue's design reference specifies:

  1. Per-agent opt-in config: gating is DISABLED unless
     `<agent_workdir>/.security/gate_config.json` exists (acceptance
     criterion 5: an agent without the config file sees zero behavior
     change).
  2. Shared network grants: list-valued keys from
     `<agent_workdir>/../shared/.security/gate_config.json` are unioned in,
     so one shared file governs "everyone gets this" changes without
     per-agent copy-paste drift (acceptance criterion 4).
  3. File-target classification: resolve-then-compare against the
     `local_write_roots` allowlist, with symlink resolution BEFORE the
     comparison and prefix-safe sibling rejection (acceptance criterion 1,
     file side).
  4. Durable pending-request records: a blocked operation is written to
     `<pending_dir>/<id>.json` with status `pending`, both approval channels
     null, and a default-deny TTL; unapproved records expire and are never
     performed (acceptance criterion 3).
  5. Dual-channel approval legs: `approve`/`deny` per channel; a record only
     becomes `approved` when BOTH configured channels approve it (dual-channel
     human approval, acceptance criterion 2). The gate module itself never
     sends notifications and never executes anything - it only classifies and
     records; approval replay is a separate auditable component (kernel-side).

Honest limits (from issue #864 section 6, stated here rather than hidden):

  - This module is the CONFIG + FILE-CLASSIFICATION + PENDING-RECORD slice.
    Shell-command classification, the inline-Python AST scan, notification
    dispatch, and the exact-replay watchdog belong in the kernel dispatch
    path (see pull request #896's design note) and are intentionally OUT of
    scope here.
  - Path classification is sound ONLY for the `file` tool surface, because
    every file call carries an explicit target path. Shell commands are
    free-form text and need a separate fail-closed pattern classifier in the
    kernel.
  - No approval channel is contacted by this module; it only records the
    human's approve/deny decisions as durable JSON legs.

Usage:
    python scripts/gatekeeper.py check-file PATH [--workdir DIR]
        # SAFE -> exit 0 | RISKY <reason> -> exit 1 | gating disabled -> exit 2
    python scripts/gatekeeper.py record --kind file_write --op PATH \
        [--cwd DIR] [--reason TEXT] [--workdir DIR]
        # writes a pending record, prints its JSON + path
    python scripts/gatekeeper.py approve <id> <telegram|wechat> [--workdir DIR]
    python scripts/gatekeeper.py deny <id> <telegram|wechat> [--workdir DIR]
    python scripts/gatekeeper.py status <id> [--workdir DIR]
    python scripts/gatekeeper.py expire [--workdir DIR]

Reference configs live in examples/gate_config.json (per-agent) and
examples/gate_config.shared.json (shared network grants).
"""
from __future__ import annotations

import argparse
import json
import os
import sys
import uuid
from datetime import datetime, timedelta, timezone
from pathlib import Path

MERGED_LIST_KEYS = ("local_write_roots", "remote_write_roots", "ssh_hosts", "trusted_scripts")
APPROVAL_CHANNELS = ("telegram", "wechat")
APPROVED_LEG = "approve"
DENIED_LEG = "deny"
DEFAULT_PENDING_DIR = ".security/pending"
DEFAULT_TTL_SECONDS = 86400  # 24h: no dual approval -> expired -> never performed
SHARED_CONFIG_PATH = Path("..") / "shared" / ".security" / "gate_config.json"
RECORD_KINDS = ("file_write", "file_edit", "shell_command")


def _utcnow() -> datetime:
    return datetime.now(timezone.utc)


def load_gate_config(working_dir: str | Path) -> dict | None:
    """Load the per-agent gatekeeper config; None means gating is DISABLED.

    Per-agent file: <working_dir>/.security/gate_config.json (presence is the
    opt-in signal). Shared network file: <working_dir>/../shared/.security/
    gate_config.json; list-valued keys (MERGED_LIST_KEYS) are unioned in so a
    shared file can grant network-wide permissions without per-agent drift.
    Scalar values (pending_dir, ttl_seconds) stay agent-local.
    """
    path = Path(working_dir) / ".security" / "gate_config.json"
    if not path.is_file():
        return None  # gating NOT enabled -> zero behavior change
    own = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(own, dict):
        raise ValueError(f"{path} must contain a JSON object")

    shared_path = Path(working_dir) / SHARED_CONFIG_PATH
    if not shared_path.is_file():
        return own
    shared = json.loads(shared_path.read_text(encoding="utf-8"))
    if not isinstance(shared, dict):
        raise ValueError(f"{shared_path} must contain a JSON object")

    merged = dict(own)
    for key in MERGED_LIST_KEYS:
        combined = list(shared.get(key) or []) + list(own.get(key) or [])
        deduped = list(dict.fromkeys(combined))
        if deduped:
            merged[key] = deduped
    return merged


def resolve_path(path: str | Path, base: str | Path | None = None) -> str:
    """Resolve a target path for allowlist comparison.

    Relative paths resolve against `base` (default: the calling process cwd),
    then expanduser, then resolve symlinks BEFORE the comparison so a symlink
    inside an allowed root that points outside is classified by its real
    target. strict=False so write targets that do not exist yet still resolve.
    """
    p = Path(path)
    if not p.is_absolute():
        p = Path(base or Path.cwd()) / p
    return str(p.expanduser().resolve(strict=False))


def is_within_roots(path: str, roots: list[str], base: str | Path | None = None) -> bool:
    """True iff the resolved path equals an allowed root or descends from it.

    Prefix-safe: `/approved` does NOT allow `/approved-evil`; only an exact
    root match or a child under `root + "/"` qualifies.
    """
    resolved = resolve_path(path, base=base)
    for root in roots:
        root_resolved = resolve_path(root, base=base).rstrip("/")
        if resolved == root_resolved or resolved.startswith(root_resolved + "/"):
            return True
    return False


def classify_file_target(path: str | Path, config: dict, base: str | Path | None = None) -> tuple[bool, str]:
    """Classify a file write/edit target. Returns (risky, reason).

    True => OUTSIDE every allowed root => the operation must be gated.
    An opted-in agent with no roots configured fails closed: everything is
    risky, nothing writes without approval.
    """
    roots = config.get("local_write_roots") or []
    if not roots:
        return True, "no local_write_roots configured - fail closed"
    if is_within_roots(str(path), roots, base=base):
        return False, ""
    resolved = resolve_path(path, base=base)
    return True, f"target outside local_write_roots: {resolved}"


def pending_dir_for(config: dict, working_dir: str | Path) -> Path:
    pending = config.get("pending_dir") or DEFAULT_PENDING_DIR
    return Path(working_dir) / pending


def _write_record_atomic(record_path: Path, record: dict) -> None:
    tmp = record_path.with_name(record_path.name + ".tmp")
    tmp.write_text(json.dumps(record, indent=2) + "\n", encoding="utf-8")
    os.replace(tmp, record_path)  # durable: readers never see a partial record


def write_pending_record(
    operation: dict,
    config: dict,
    working_dir: str | Path,
    now: datetime | None = None,
) -> tuple[dict, Path]:
    """Persist a blocked operation as a durable pending request.

    `operation` carries the EXACT original operation so a later approval
    watchdog can replay it verbatim (never re-derived by an LLM):
      {"kind": "file_write"|"file_edit"|"shell_command",
       "command": "<exact original command or resolved target path>",
       "cwd": "<exact working dir>", "reason": "<gate classification reason>"}
    The record starts status=pending, both approval channels null, and
    expires_at = created_at + ttl_seconds. Default is deny: no dual approval
    within the TTL means the operation is never performed.
    """
    now = now or _utcnow()
    ttl = int(config.get("ttl_seconds") or DEFAULT_TTL_SECONDS)
    record = {
        "id": uuid.uuid4().hex,
        "created_at": now.isoformat(),
        "expires_at": (now + timedelta(seconds=ttl)).isoformat(),
        "status": "pending",
        "operation": operation,
        "approvals": {channel: None for channel in APPROVAL_CHANNELS},
    }
    pending_dir = pending_dir_for(config, working_dir)
    pending_dir.mkdir(parents=True, exist_ok=True)
    record_path = pending_dir / f"{record['id']}.json"
    _write_record_atomic(record_path, record)
    return record, record_path


def _load_record(record_path: Path) -> dict:
    data = json.loads(record_path.read_text(encoding="utf-8"))
    if not isinstance(data, dict):
        raise ValueError(f"{record_path} must contain a JSON object")
    return data


def _save_record(record_path: Path, record: dict) -> None:
    _write_record_atomic(record_path, record)


def record_path_for(record_id_or_path: str, config: dict, working_dir: str | Path) -> Path:
    p = Path(record_id_or_path)
    if p.is_absolute() or p.suffix == ".json":
        return p if p.is_absolute() else Path(working_dir) / p
    return pending_dir_for(config, working_dir) / f"{p}.json"


def mark_approval(
    record_id_or_path: str,
    channel: str,
    decision: str,
    config: dict,
    working_dir: str | Path,
    now: datetime | None = None,
) -> dict:
    """Record one human approve/deny leg for one approval channel.

    A record becomes `approved` only when BOTH configured channels show
    `approve`. A deny on any channel permanently blocks it; terminal records
    (approved/denied/expired/executed/failed) are never reopened.
    """
    if channel not in APPROVAL_CHANNELS:
        raise ValueError(f"unknown approval channel {channel!r}; expected one of {APPROVAL_CHANNELS}")
    if decision not in (APPROVED_LEG, DENIED_LEG):
        raise ValueError(f"decision must be 'approve' or 'deny', got {decision!r}")

    path = record_path_for(record_id_or_path, config, working_dir)
    if not path.is_file():
        raise FileNotFoundError(f"no pending record at {path}")
    record = _load_record(path)
    if record.get("status") != "pending":
        return record  # terminal: can't approve/deny an already-settled record

    now = now or _utcnow()
    record["approvals"][channel] = decision
    legs = record["approvals"]
    if all(legs.get(ch) == APPROVED_LEG for ch in APPROVAL_CHANNELS):
        record["status"] = "approved"
        record["approved_at"] = now.isoformat()
    elif DENIED_LEG in legs.values():
        record["status"] = "denied"
    _save_record(path, record)
    return record


def dual_approved(record: dict) -> bool:
    """True iff both configured approval channels approved the record."""
    legs = record.get("approvals") or {}
    return all(legs.get(ch) == APPROVED_LEG for ch in APPROVAL_CHANNELS)


def expire_stale(working_dir: str | Path, config: dict, now: datetime | None = None) -> list[str]:
    """Expire pending records past their TTL (default deny: they never run)."""
    now = now or _utcnow()
    expired: list[str] = []
    pending_dir = pending_dir_for(config, working_dir)
    if not pending_dir.is_dir():
        return expired
    for record_path in sorted(pending_dir.glob("*.json")):
        record = _load_record(record_path)
        if record.get("status") != "pending":
            continue
        expires_at = datetime.fromisoformat(record["expires_at"])
        if now >= expires_at:
            record["status"] = "expired"
            record["expired_at"] = now.isoformat()
            _save_record(record_path, record)
            expired.append(record["id"])
    return expired


def _cmd_check_file(args: argparse.Namespace) -> int:
    config = load_gate_config(args.workdir)
    if config is None:
        print(f"DISABLED: no {Path(args.workdir) / '.security' / 'gate_config.json'}")
        return 2
    risky, reason = classify_file_target(args.path, config, base=args.workdir)
    if risky:
        print(f"RISKY {reason}")
        return 1
    print(f"SAFE {resolve_path(args.path, base=args.workdir)}")
    return 0


def _cmd_record(args: argparse.Namespace) -> int:
    config = load_gate_config(args.workdir)
    if config is None:
        print(f"DISABLED: gatekeeper not configured for {args.workdir}", file=sys.stderr)
        return 2
    operation = {
        "kind": args.kind,
        "command": args.op,
        "cwd": str(args.cwd or Path(args.workdir).resolve()),
        "reason": args.reason or "manual record",
    }
    if args.tool_call_id:
        operation["tool_call_id"] = args.tool_call_id
    record, path = write_pending_record(operation, config, args.workdir)
    print(json.dumps(record, indent=2))
    print(f"record: {path}")
    return 0


def _cmd_leg(args: argparse.Namespace) -> int:
    config = load_gate_config(args.workdir)
    if config is None:
        print(f"DISABLED: gatekeeper not configured for {args.workdir}", file=sys.stderr)
        return 2
    decision = "approve" if args.command == "approve" else "deny"
    record = mark_approval(args.record_id, args.channel, decision, config, args.workdir)
    print(json.dumps(record, indent=2))
    return 0


def _cmd_status(args: argparse.Namespace) -> int:
    config = load_gate_config(args.workdir)
    if config is None:
        print(f"DISABLED: gatekeeper not configured for {args.workdir}", file=sys.stderr)
        return 2
    path = record_path_for(args.record_id, config, args.workdir)
    if not path.is_file():
        raise FileNotFoundError(f"no pending record at {path}")
    record = _load_record(path)
    print(json.dumps(record, indent=2))
    print(f"record: {path}")
    return 0


def _cmd_expire(args: argparse.Namespace) -> int:
    config = load_gate_config(args.workdir)
    if config is None:
        print(f"DISABLED: gatekeeper not configured for {args.workdir}", file=sys.stderr)
        return 2
    expired = expire_stale(args.workdir, config)
    if expired:
        print("expired: " + ", ".join(expired))
    else:
        print("nothing to expire")
    return 0


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(prog="gatekeeper", description=__doc__)
    parser.add_argument("--workdir", default=".", help="agent working directory (default: .)")
    sub = parser.add_subparsers(dest="command", required=True)

    def add_workdir(subparser: argparse.ArgumentParser) -> None:
        # Also accepted after the subcommand; SUPPRESS keeps a value given
        # before the subcommand from being clobbered by the default.
        subparser.add_argument("--workdir", default=argparse.SUPPRESS, help=argparse.SUPPRESS)

    p_check = sub.add_parser("check-file", help="classify a file write/edit target")
    add_workdir(p_check)
    p_check.add_argument("path", help="target path to classify")
    p_check.set_defaults(func=_cmd_check_file)

    p_record = sub.add_parser("record", help="write a durable pending request")
    add_workdir(p_record)
    p_record.add_argument("--kind", required=True, choices=RECORD_KINDS)
    p_record.add_argument("--op", required=True, help="exact original operation (command text or resolved target path)")
    p_record.add_argument("--cwd", default=None, help="exact working dir of the operation")
    p_record.add_argument("--reason", default=None, help="gate classification reason")
    p_record.add_argument("--tool-call-id", dest="tool_call_id", default=None)
    p_record.set_defaults(func=_cmd_record)

    for name in ("approve", "deny"):
        p_leg = sub.add_parser(name, help=f"record a {name} leg on one approval channel")
        add_workdir(p_leg)
        p_leg.add_argument("record_id")
        p_leg.add_argument("channel", choices=APPROVAL_CHANNELS)
        p_leg.set_defaults(func=_cmd_leg)

    p_status = sub.add_parser("status", help="show a pending record")
    add_workdir(p_status)
    p_status.add_argument("record_id")
    p_status.set_defaults(func=_cmd_status)

    p_expire = sub.add_parser("expire", help="expire pending records past their TTL")
    add_workdir(p_expire)
    p_expire.set_defaults(func=_cmd_expire)

    return parser


def main(argv: list[str] | None = None) -> int:
    args = build_parser().parse_args(argv)
    return args.func(args)


if __name__ == "__main__":
    sys.exit(main())
