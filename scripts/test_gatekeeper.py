#!/usr/bin/env python3
"""Contract tests for scripts/gatekeeper.py (lingtai #864 first vertical slice).

Run:  python3 scripts/test_gatekeeper.py
"""
from __future__ import annotations

import json
import os
import tempfile
import unittest
from datetime import datetime, timedelta, timezone
from pathlib import Path

import gatekeeper

NOW = datetime(2026, 8, 16, 12, 0, 0, tzinfo=timezone.utc)


def make_workdir(tmp: str, own: dict | None, shared: dict | None = None) -> Path:
    """Lay out an agent workdir with optional per-agent and shared configs."""
    workdir = Path(tmp) / "agent"
    if own is not None:
        sec = workdir / ".security"
        sec.mkdir(parents=True, exist_ok=True)
        (sec / "gate_config.json").write_text(json.dumps(own), encoding="utf-8")
    if shared is not None:
        shared_dir = Path(tmp) / "shared" / ".security"
        shared_dir.mkdir(parents=True, exist_ok=True)
        (shared_dir / "gate_config.json").write_text(json.dumps(shared), encoding="utf-8")
    return workdir


class LoadConfigTest(unittest.TestCase):
    def test_missing_config_disables_gating(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own=None)
            self.assertIsNone(gatekeeper.load_gate_config(workdir))

    def test_own_config_loads_and_scalars_stay_agent_local(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            own = {"local_write_roots": ["/a"], "pending_dir": ".security/pending", "ttl_seconds": 3600}
            workdir = make_workdir(tmp, own=own)
            config = gatekeeper.load_gate_config(workdir)
            self.assertEqual(config["local_write_roots"], ["/a"])
            self.assertEqual(config["pending_dir"], ".security/pending")
            self.assertEqual(config["ttl_seconds"], 3600)

    def test_shared_config_unions_list_keys(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            own = {
                "local_write_roots": ["/own/a", "/shared-base"],
                "ssh_hosts": ["own-host"],
                "pending_dir": ".security/pending",
            }
            shared = {
                "local_write_roots": ["/shared-base", "/network/b"],
                "ssh_hosts": ["network-host"],
            }
            workdir = make_workdir(tmp, own=own, shared=shared)
            config = gatekeeper.load_gate_config(workdir)
            self.assertEqual(config["local_write_roots"], ["/shared-base", "/network/b", "/own/a"])
            self.assertCountEqual(config["ssh_hosts"], ["own-host", "network-host"])
            # scalar stays agent-local; absent scalars are NOT invented by the
            # loader (defaults apply at use sites, e.g. write_pending_record)
            self.assertEqual(config["pending_dir"], ".security/pending")
            self.assertNotIn("ttl_seconds", config)

    def test_shared_config_absent_leaves_own_untouched(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            own = {"local_write_roots": ["/own/a"]}
            workdir = make_workdir(tmp, own=own)
            config = gatekeeper.load_gate_config(workdir)
            self.assertEqual(config["local_write_roots"], ["/own/a"])


class ClassifyFileTargetTest(unittest.TestCase):
    def test_exact_root_is_safe(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "approved"
            root.mkdir()
            config = {"local_write_roots": [str(root)]}
            risky, reason = gatekeeper.classify_file_target(str(root), config)
            self.assertFalse(risky, reason)

    def test_descendant_is_safe(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "approved"
            (root / "sub").mkdir(parents=True)
            config = {"local_write_roots": [str(root)]}
            risky, reason = gatekeeper.classify_file_target(str(root / "sub" / "f.txt"), config)
            self.assertFalse(risky, reason)

    def test_sibling_prefix_is_rejected(self) -> None:
        # The classic soundness failure: '/approved' must not allow '/approved-evil'.
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "approved"
            root.mkdir()
            sibling = Path(tmp) / "approved-evil"
            config = {"local_write_roots": [str(root)]}
            risky, reason = gatekeeper.classify_file_target(str(sibling), config)
            self.assertTrue(risky, reason)

    def test_unrelated_target_is_risky(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "approved"
            root.mkdir()
            outside = Path(tmp) / "elsewhere" / "f.txt"
            config = {"local_write_roots": [str(root)]}
            risky, reason = gatekeeper.classify_file_target(str(outside), config)
            self.assertTrue(risky, reason)

    def test_symlink_escape_is_rejected(self) -> None:
        # Resolution happens BEFORE the comparison: a symlink inside the root
        # pointing at an outside path must classify by the real target.
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "approved"
            root.mkdir()
            outside_dir = Path(tmp) / "outside"
            outside_dir.mkdir()
            try:
                (root / "escape").symlink_to(outside_dir, target_is_directory=True)
            except OSError as exc:  # pragma: no cover - platform without symlinks
                self.skipTest(f"symlinks unavailable: {exc}")
            config = {"local_write_roots": [str(root)]}
            risky, reason = gatekeeper.classify_file_target(str(root / "escape") + "/f.txt", config)
            self.assertTrue(risky, reason)

    def test_no_roots_fails_closed(self) -> None:
        config = {}
        risky, reason = gatekeeper.classify_file_target("/anywhere", config)
        self.assertTrue(risky, reason)
        self.assertIn("fail closed", reason)

    def test_relative_target_resolves_against_workdir(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp) / "approved"
            root.mkdir()
            config = {"local_write_roots": [str(root)]}
            risky, reason = gatekeeper.classify_file_target("sub/x.txt", config, base=str(root))
            self.assertFalse(risky, reason)


class PendingRecordTest(unittest.TestCase):
    def _config(self, workdir: Path) -> dict:
        return gatekeeper.load_gate_config(workdir)

    def test_record_written_durably_with_issue_shape(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"], "ttl_seconds": 1234})
            config = self._config(workdir)
            operation = {
                "kind": "file_write",
                "command": "/etc/passwd",
                "cwd": str(workdir),
                "reason": "target outside local_write_roots: /etc/passwd",
            }
            record, path = gatekeeper.write_pending_record(operation, config, workdir, now=NOW)
            self.assertTrue(path.is_file())
            self.assertEqual(record["status"], "pending")
            self.assertEqual(record["id"], path.stem)
            self.assertEqual(record["created_at"], NOW.isoformat())
            self.assertEqual(record["expires_at"], (NOW + timedelta(seconds=1234)).isoformat())
            self.assertEqual(record["operation"], operation)  # exact, replay-safe
            self.assertEqual(record["approvals"], {"telegram": None, "wechat": None})
            on_disk = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(on_disk, record)

    def test_default_ttl_is_24h(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"]})
            config = self._config(workdir)
            _, path = gatekeeper.write_pending_record(
                {"kind": "shell_command", "command": "rm -rf build", "cwd": str(workdir), "reason": "test"},
                config, workdir, now=NOW,
            )
            record = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(record["expires_at"], (NOW + timedelta(seconds=86400)).isoformat())


class ApprovalLegTest(unittest.TestCase):
    def test_both_channels_required_for_approval(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"]})
            config = gatekeeper.load_gate_config(workdir)
            operation = {"kind": "file_write", "command": "/etc/passwd", "cwd": str(workdir), "reason": "test"}
            record, path = gatekeeper.write_pending_record(operation, config, workdir, now=NOW)
            rid = record["id"]

            after_one = gatekeeper.mark_approval(rid, "telegram", "approve", config, workdir, now=NOW)
            self.assertEqual(after_one["status"], "pending")
            self.assertFalse(gatekeeper.dual_approved(after_one))

            after_two = gatekeeper.mark_approval(rid, "wechat", "approve", config, workdir, now=NOW)
            self.assertEqual(after_two["status"], "approved")
            self.assertTrue(gatekeeper.dual_approved(after_two))
            self.assertEqual(after_two["approvals"], {"telegram": "approve", "wechat": "approve"})

    def test_deny_blocks_approval_permanently(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"]})
            config = gatekeeper.load_gate_config(workdir)
            operation = {"kind": "file_write", "command": "/etc/passwd", "cwd": str(workdir), "reason": "test"}
            record, path = gatekeeper.write_pending_record(operation, config, workdir, now=NOW)
            rid = record["id"]

            gatekeeper.mark_approval(rid, "telegram", "approve", config, workdir, now=NOW)
            after_deny = gatekeeper.mark_approval(rid, "wechat", "deny", config, workdir, now=NOW)
            self.assertEqual(after_deny["status"], "denied")
            self.assertFalse(gatekeeper.dual_approved(after_deny))
            # a later approve on the already-denied leg cannot reopen it
            frozen = gatekeeper.mark_approval(rid, "wechat", "approve", config, workdir, now=NOW)
            self.assertEqual(frozen["status"], "denied")

    def test_terminal_records_are_frozen(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"]})
            config = gatekeeper.load_gate_config(workdir)
            operation = {"kind": "file_write", "command": "/etc/passwd", "cwd": str(workdir), "reason": "test"}
            record, path = gatekeeper.write_pending_record(operation, config, workdir, now=NOW)
            rid = record["id"]
            gatekeeper.mark_approval(rid, "telegram", "approve", config, workdir, now=NOW)
            gatekeeper.mark_approval(rid, "wechat", "approve", config, workdir, now=NOW)
            reopened = gatekeeper.mark_approval(rid, "telegram", "deny", config, workdir, now=NOW)
            self.assertEqual(reopened["status"], "approved")

    def test_unknown_channel_and_decision_rejected(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"]})
            config = gatekeeper.load_gate_config(workdir)
            operation = {"kind": "file_write", "command": "/etc/passwd", "cwd": str(workdir), "reason": "test"}
            record, _ = gatekeeper.write_pending_record(operation, config, workdir, now=NOW)
            with self.assertRaises(ValueError):
                gatekeeper.mark_approval(record["id"], "email", "approve", config, workdir, now=NOW)
            with self.assertRaises(ValueError):
                gatekeeper.mark_approval(record["id"], "telegram", "maybe", config, workdir, now=NOW)


class ExpiryTest(unittest.TestCase):
    def test_stale_pending_record_expires_and_never_runs(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"]})
            config = gatekeeper.load_gate_config(workdir)
            operation = {"kind": "file_write", "command": "/etc/passwd", "cwd": str(workdir), "reason": "test"}
            stale, _ = gatekeeper.write_pending_record(operation, config, workdir, now=NOW)

            later = NOW + timedelta(seconds=86401)
            expired = gatekeeper.expire_stale(workdir, config, now=later)
            self.assertEqual(expired, [stale["id"]])

            path = gatekeeper.record_path_for(stale["id"], config, workdir)
            on_disk = json.loads(path.read_text(encoding="utf-8"))
            self.assertEqual(on_disk["status"], "expired")
            self.assertFalse(gatekeeper.dual_approved(on_disk))

    def test_recent_record_survives(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            workdir = make_workdir(tmp, own={"local_write_roots": ["/approved"]})
            config = gatekeeper.load_gate_config(workdir)
            operation = {"kind": "file_write", "command": "/etc/passwd", "cwd": str(workdir), "reason": "test"}
            fresh, _ = gatekeeper.write_pending_record(operation, config, workdir, now=NOW)
            expired = gatekeeper.expire_stale(workdir, config, now=NOW + timedelta(seconds=60))
            self.assertEqual(expired, [])
            on_disk = json.loads(gatekeeper.record_path_for(fresh["id"], config, workdir).read_text(encoding="utf-8"))
            self.assertEqual(on_disk["status"], "pending")


class ExampleConfigTest(unittest.TestCase):
    def test_example_configs_parse_and_carry_interface_keys(self) -> None:
        examples = Path(__file__).resolve().parent.parent / "examples"
        own = json.loads((examples / "gate_config.json").read_text(encoding="utf-8"))
        shared = json.loads((examples / "gate_config.shared.json").read_text(encoding="utf-8"))
        for key in gatekeeper.MERGED_LIST_KEYS + ("pending_dir",):
            self.assertIn(key, own)
        for key in gatekeeper.MERGED_LIST_KEYS:
            self.assertIn(key, shared)

    def test_example_shared_config_loads_through_loader(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            examples = Path(__file__).resolve().parent.parent / "examples"
            own = json.loads((examples / "gate_config.json").read_text(encoding="utf-8"))
            shared = json.loads((examples / "gate_config.shared.json").read_text(encoding="utf-8"))
            workdir = make_workdir(tmp, own=own, shared=shared)
            config = gatekeeper.load_gate_config(workdir)
            self.assertIn("/path/to/approved-work", config["local_write_roots"])
            self.assertIn("/path/to/shared-approved", config["local_write_roots"])
            self.assertIn("cluster", config["ssh_hosts"])
            self.assertEqual(config["pending_dir"], ".security/pending")
            self.assertEqual(config["ttl_seconds"], 86400)


if __name__ == "__main__":
    unittest.main()
