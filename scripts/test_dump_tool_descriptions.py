#!/usr/bin/env python3
"""Stdlib smoke tests for dump_tool_descriptions.py."""
from __future__ import annotations

import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parent.parent
SCRIPT = ROOT / "scripts" / "dump_tool_descriptions.py"


def write_json(path: Path, payload: dict[str, str]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(payload, ensure_ascii=False), encoding="utf-8")


def create_kernel_repo(root: Path, marker: str = "default") -> Path:
    kernel_i18n = root / "src" / "lingtai_kernel" / "i18n"
    capability_i18n = root / "src" / "lingtai" / "i18n"
    for lang, label in (("en", "English"), ("zh", "Chinese"), ("wen", "Classical")):
        write_json(
            kernel_i18n / f"{lang}.json",
            {
                "intrinsic.description": f"{label} intrinsic {marker}",
                "intrinsic.name": f"ignored kernel {marker}",
            },
        )
        write_json(
            capability_i18n / f"{lang}.json",
            {
                "read.description": f"{label} capability {marker}",
                "read.name": f"ignored capability {marker}",
            },
        )
    return root


class DumpToolDescriptionsTest(unittest.TestCase):
    def run_script(self, *args: str, env_updates=None):
        env = os.environ.copy()
        env.pop("LINGTAI_KERNEL_REPO", None)
        if env_updates:
            env.update(env_updates)
        return subprocess.run(
            [sys.executable, str(SCRIPT), *args],
            cwd=ROOT,
            text=True,
            capture_output=True,
            timeout=10,
            env=env,
            check=False,
        )

    def test_happy_path_includes_both_sections_and_locale_labels(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            kernel_repo = create_kernel_repo(Path(tmp) / "lingtai-kernel")
            wen_path = kernel_repo / "src" / "lingtai" / "i18n" / "wen.json"
            write_json(wen_path, {"other.description": "Classical capability fallback"})

            result = self.run_script("--kernel-repo", str(kernel_repo))

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(result.stderr, "")
        self.assertIn("# Tool Descriptions", result.stdout)
        self.assertIn("## Kernel Intrinsics", result.stdout)
        self.assertIn("## Capabilities", result.stdout)
        self.assertIn("**English:**", result.stdout)
        self.assertIn("**中文:**", result.stdout)
        self.assertIn("**文言:**", result.stdout)
        self.assertIn("### intrinsic", result.stdout)
        self.assertIn("### read", result.stdout)
        self.assertIn("—", result.stdout)

    def test_missing_capability_dir_fails_loudly(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            kernel_repo = Path(tmp) / "lingtai-kernel"
            kernel_i18n = kernel_repo / "src" / "lingtai_kernel" / "i18n"
            for lang in ("en", "zh", "wen"):
                write_json(kernel_i18n / f"{lang}.json", {"intrinsic.description": lang})

            result = self.run_script("--kernel-repo", str(kernel_repo))

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn("src/lingtai/i18n", result.stderr)
        self.assertIn("--kernel-repo", result.stderr)
        self.assertIn("LINGTAI_KERNEL_REPO", result.stderr)

    def test_nonexistent_kernel_repo_fails_with_override_hint(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            missing = Path(tmp) / "missing-kernel"
            result = self.run_script("--kernel-repo", str(missing))

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn(str(missing.resolve()), result.stderr)
        self.assertIn("--kernel-repo", result.stderr)
        self.assertIn("LINGTAI_KERNEL_REPO", result.stderr)

    def test_env_var_works_and_cli_arg_takes_precedence(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            env_repo = create_kernel_repo(Path(tmp) / "env-kernel", "env")
            cli_repo = create_kernel_repo(Path(tmp) / "cli-kernel", "cli")

            env_result = self.run_script(
                env_updates={"LINGTAI_KERNEL_REPO": str(env_repo)}
            )
            cli_result = self.run_script(
                "--kernel-repo",
                str(cli_repo),
                env_updates={"LINGTAI_KERNEL_REPO": str(env_repo)},
            )

        self.assertEqual(env_result.returncode, 0, env_result.stderr)
        self.assertIn("English capability env", env_result.stdout)
        self.assertNotIn("English capability cli", env_result.stdout)
        self.assertEqual(cli_result.returncode, 0, cli_result.stderr)
        self.assertIn("English capability cli", cli_result.stdout)
        self.assertNotIn("English capability env", cli_result.stdout)

    def test_missing_locale_file_fails_and_names_file(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            kernel_repo = create_kernel_repo(Path(tmp) / "lingtai-kernel")
            missing = kernel_repo / "src" / "lingtai" / "i18n" / "zh.json"
            missing.unlink()

            result = self.run_script("--kernel-repo", str(kernel_repo))

        self.assertNotEqual(result.returncode, 0)
        self.assertEqual(result.stdout, "")
        self.assertIn(str(missing.resolve()), result.stderr)

    def test_non_description_keys_are_ignored(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            kernel_repo = create_kernel_repo(Path(tmp) / "lingtai-kernel", "visible")

            result = self.run_script("--kernel-repo", str(kernel_repo))

        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn("English capability visible", result.stdout)
        self.assertNotIn("ignored capability visible", result.stdout)
        self.assertNotIn("ignored kernel visible", result.stdout)


if __name__ == "__main__":
    unittest.main()
