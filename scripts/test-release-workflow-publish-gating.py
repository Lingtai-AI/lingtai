#!/usr/bin/env python3
"""Static gates for exact, tag-only Windows preview publication."""
from __future__ import annotations

import sys
from pathlib import Path

try:
    import yaml
except ModuleNotFoundError:
    print("SKIP: PyYAML not available", file=sys.stderr)
    raise SystemExit(0)

WORKFLOW = Path(__file__).resolve().parents[1] / ".github/workflows/release.yml"
FAILURES: list[str] = []


def check(condition: bool, message: str) -> None:
    if not condition:
        FAILURES.append(message)


def find_step(job: dict, name: str) -> dict | None:
    return next((step for step in job.get("steps", []) if name in step.get("name", "").lower()), None)


def scripts(job: dict) -> str:
    return "\n".join(str(step.get("run", "")) for step in job.get("steps", []))


def main() -> int:
    data = yaml.safe_load(WORKFLOW.read_text())
    on = data.get("on") or data.get(True)
    check("pull_request" in on and "workflow_dispatch" in on, "PR/manual validation triggers required")
    check(any("v*" in tag for tag in on["push"]["tags"]), "v* tag trigger required")
    check(data.get("permissions", {}).get("contents") == "read", "default contents permission must be read")

    jobs = data.get("jobs", {})
    expected = {"windows-amd64-preview", "source-release", "publish-windows-amd64-preview", "update-homebrew"}
    check(set(jobs) == expected, f"unexpected release jobs: {sorted(jobs)}")

    preview = jobs.get("windows-amd64-preview", {})
    preview_text = scripts(preview)
    check(preview.get("runs-on") == "windows-latest", "preview must run natively on Windows")
    for needle in ("go test ./...", "go vet ./...", "lingtai-tui-windows-amd64.exe", "lingtai-portal-windows-amd64.exe", "install-windows-preview.ps1"):
        check(needle in preview_text, f"preview validation missing {needle!r}")
    upload = next((step for step in preview.get("steps", []) if step.get("uses") == "actions/upload-artifact@v4"), None)
    check(upload is not None, "preview must upload a PR-safe Actions artifact")
    if upload:
        paths = {line.strip() for line in str(upload.get("with", {}).get("path", "")).splitlines() if line.strip()}
        check(paths == {"dist/lingtai-*-windows-amd64-preview.zip", "dist/lingtai-*-windows-amd64-preview.zip.sha256", "dist/install-windows-preview.ps1"},
              "all artifact paths must share the dist root used by publication")
    check("gh release upload" not in preview_text, "preview validation must not publish")

    source = jobs.get("source-release", {})
    check("github.event_name == 'push'" in str(source.get("if", "")), "source release must be tag-push gated")
    check(source.get("permissions", {}).get("contents") == "write", "source release must request contents: write")
    create = find_step(source, "create github source release")
    check(create is not None and "--verify-tag" in create.get("run", ""), "source release must verify the tag")

    publish = jobs.get("publish-windows-amd64-preview", {})
    publish_text = scripts(publish)
    check("github.event_name == 'push'" in str(publish.get("if", "")), "preview publication must be tag-push gated")
    check(set(publish.get("needs", [])) == {"source-release", "windows-amd64-preview"},
          "preview publication must need source release and validated artifact")
    check(publish.get("permissions", {}).get("contents") == "write", "preview publication must request contents: write")
    download = next((step for step in publish.get("steps", []) if step.get("uses") == "actions/download-artifact@v4"), None)
    check(download is not None and download.get("with", {}).get("path") == "dist",
          "validated artifact must download into dist")
    for needle in ("lingtai-${TAG}-windows-amd64-preview.zip", 'bootstrap="dist/install-windows-preview.ps1"', "gh release upload"):
        check(needle in publish_text, f"preview publication missing {needle!r}")
    check("dist/*" not in publish_text and "*.zip" not in publish_text, "preview publication must not use wildcard assets")

    homebrew = jobs.get("update-homebrew", {})
    check("github.event_name == 'push'" in str(homebrew.get("if", "")), "Homebrew update must be tag-push gated")
    check(homebrew.get("needs") == "source-release", "Homebrew update must wait for source release")
    formula = find_step(homebrew, "write formula")
    check(formula is not None and 'depends_on "go" => :build' in formula.get("run", ""),
          "Homebrew must retain its source-build formula")

    text = WORKFLOW.read_text()
    for forbidden in ("lingtai-bundle-manifest.json", "publish_bundle_to_gitee.sh", "sync_gitee_mirror.sh", "GITEE_ACCESS_TOKEN", "windows-arm64"):
        check(forbidden not in text, f"release workflow must not contain {forbidden!r}")

    if FAILURES:
        print("FAILED release workflow checks:", file=sys.stderr)
        for failure in FAILURES:
            print(f"  - {failure}", file=sys.stderr)
        return 1
    print("OK: native preview validation, exact tag-only assets, source release, and Homebrew gates")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
