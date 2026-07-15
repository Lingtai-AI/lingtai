#!/usr/bin/env python3
"""Static assertions over .github/workflows/release.yml's publish-bundle job:
proves the Gitee publish step is actually reachable with --execute on the
real tag-push release trigger (Blocker 3 repair), that a missing
GITEE_ACCESS_TOKEN skips rather than fakes success, that the sync step never
force-pushes, and that the token is never echoed. This is a workflow-shape
test — it parses the committed YAML/shell text, no live GitHub Actions run.

Run directly: python3 scripts/test-release-workflow-publish-gating.py
"""
from __future__ import annotations

import sys
from pathlib import Path

try:
    import yaml
except ModuleNotFoundError:
    print("SKIP: PyYAML not available in this environment", file=sys.stderr)
    raise SystemExit(0)

REPO_ROOT = Path(__file__).resolve().parents[1]
WORKFLOW_PATH = REPO_ROOT / ".github" / "workflows" / "release.yml"

FAILURES: list[str] = []


def check(condition: bool, message: str) -> None:
    if not condition:
        FAILURES.append(message)


def load_workflow() -> dict:
    return yaml.safe_load(WORKFLOW_PATH.read_text())


def publish_bundle_job(data: dict) -> dict:
    return data["jobs"]["publish-bundle"]


def find_step(job: dict, name_substring: str) -> dict | None:
    for step in job["steps"]:
        if name_substring.lower() in step.get("name", "").lower():
            return step
    return None


def main() -> int:
    data = load_workflow()
    on = data.get("on") or data.get(True)  # YAML 1.1 bareword quirk guard
    check("push" in on and "tags" in on["push"], "release.yml must trigger on tag pushes")
    check(any("v*" in t for t in on["push"]["tags"]), "release.yml must trigger on v* tags")

    job = publish_bundle_job(data)
    check(job.get("permissions", {}).get("contents") == "write",
          "publish-bundle needs contents:write to push to Gitee / upload release assets")

    checkout = find_step(job, "checkout")
    check(checkout is not None, "publish-bundle must have a checkout step")
    if checkout:
        check(checkout.get("with", {}).get("fetch-depth") == 0,
              "checkout must use fetch-depth: 0 so sync_gitee_mirror.sh can push full history")

    sync_step = find_step(job, "synchronize the exact commit/tag to gitee")
    check(sync_step is not None, "publish-bundle must have a Gitee mirror sync step")
    if sync_step:
        script = sync_step["run"]
        check("sync_gitee_mirror.sh" in script, "sync step must call sync_gitee_mirror.sh")
        check("--execute" in script, "sync step must pass --execute (this job IS the real release action)")
        check("--force" not in script, "sync step must never pass --force")

    publish_step = find_step(job, "publish bundle to gitee")
    check(publish_step is not None, "publish-bundle must have a Gitee publish step")
    if publish_step:
        script = publish_step["run"]
        check("publish_bundle_to_gitee.sh" in script, "publish step must call publish_bundle_to_gitee.sh")
        check("--execute" in script, "publish step must be capable of passing --execute on a real tag push")
        check("GITEE_ACCESS_TOKEN" in script,
              "publish step must condition --execute on GITEE_ACCESS_TOKEN being configured")
        # The step must NOT unconditionally omit --execute (the prior defect):
        # there must be a branch that runs the script WITH --execute.
        check("--bundle-dir gitee-bundle --execute" in script,
              "publish step must have a code path that actually invokes --execute")
        check("is not set; running the publish plan in dry-run" in script,
              "publish step must explicitly explain the dry-run fallback when the token is missing")

    # Token-leak guard: no `echo`/`print`/`cat` of the raw token anywhere in the file.
    text = WORKFLOW_PATH.read_text()
    for i, line in enumerate(text.splitlines(), start=1):
        stripped = line.strip()
        if "$GITEE_ACCESS_TOKEN" in stripped or "${GITEE_ACCESS_TOKEN" in stripped:
            check(
                not any(stripped.startswith(cmd) for cmd in ("echo", "print", "cat")),
                f"line {i} appears to print the raw Gitee token: {line}",
            )

    if FAILURES:
        print("FAILED release.yml publish-gating checks:", file=sys.stderr)
        for f in FAILURES:
            print(f"  - {f}", file=sys.stderr)
        return 1
    print(f"OK: {len(list(publish_bundle_job(data)['steps']))} publish-bundle steps checked, all gating assertions pass")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
