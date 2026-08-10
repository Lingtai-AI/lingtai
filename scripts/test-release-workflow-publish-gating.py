#!/usr/bin/env python3
"""Static assertions for the tag release workflow.

The tag workflow must create a GitHub source release as a DRAFT, update the
Homebrew source-build formula only after the Windows publication boundary,
and publish exactly one prebuilt asset: a Windows AMD64 TUI/portal archive
plus its checksum sidecar and bundle manifest. Only the windows-release job
may upload release assets or flip the draft to public; source-release must
remain upload-free and draft-only, and Homebrew must remain
source-tarball-based. The Windows archive must fail closed unless both
binaries are built and packaged, the Homebrew checksum must fail closed on
HTTP error status, and exact-tag smoke must verify both installed binaries
through the maintained bounded version-probe helper.
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
SMOKE_WORKFLOW_PATH = REPO_ROOT / ".github" / "workflows" / "windows-installer-smoke.yml"
FAILURES: list[str] = []


def check(condition: bool, message: str) -> None:
    if not condition:
        FAILURES.append(message)


def find_step(job: dict, name_substring: str) -> dict | None:
    for step in job.get("steps", []):
        if name_substring.lower() in step.get("name", "").lower():
            return step
    return None


def main() -> int:
    data = yaml.safe_load(WORKFLOW_PATH.read_text())
    on = data.get("on") or data.get(True)  # YAML 1.1 bareword quirk guard
    check("push" in on and "tags" in on["push"], "release.yml must trigger on tag pushes")
    check(any("v*" in tag for tag in on["push"]["tags"]), "release.yml must trigger on v* tags")

    jobs = data.get("jobs", {})
    check(set(jobs) == {"source-release", "update-homebrew", "windows-release"},
          f"release jobs must be source-release + update-homebrew + windows-release only, got {sorted(jobs)}")

    source = jobs.get("source-release", {})
    create = find_step(source, "create github source release")
    check(create is not None, "source-release must have a GitHub source release step")
    if create:
        script = create.get("run", "")
        check("gh release create" in script, "source-release must create the GitHub release")
        check("--verify-tag" in script, "source-release must verify the pushed tag")
        check("--draft" in script, "source-release must create the release as a draft (draft-first)")
        check("--draft=false" not in script, "source-release must never publish the draft")
        check("gh release upload" not in script, "source-release must not upload binary assets")

    homebrew = jobs.get("update-homebrew", {})
    check(homebrew.get("needs") == "windows-release",
          "update-homebrew must depend on windows-release so it waits for the same publication boundary")
    checksum = find_step(homebrew, "compute source tarball checksum")
    formula = find_step(homebrew, "write formula")
    check(checksum is not None, "update-homebrew must checksum the GitHub source tarball")
    if checksum:
        script = checksum.get("run", "")
        check("curl -fsSL" in script or "curl -fSL" in script or "curl --fail" in script,
              "update-homebrew checksum must fail closed on HTTP error status (curl -f)")
        check("curl -sL" not in script,
              "update-homebrew checksum must not use bare curl -sL (an HTTP error body must not be hashed)")
    check(formula is not None, "update-homebrew must write the source-build formula")
    if formula:
        script = formula.get("run", "")
        check("archive/refs/tags/${TAG}.tar.gz" in script,
              "Homebrew formula must build from the GitHub tag source archive")
        check('depends_on "go" => :build' in script,
              "Homebrew formula must retain source-build Go dependency")
    homebrew_text = "\n".join(step.get("run", "") for step in homebrew.get("steps", []))
    check("gh release upload" not in homebrew_text,
          "update-homebrew must not upload release assets")

    windows = jobs.get("windows-release", {})
    check(windows.get("needs") == "source-release",
          "windows-release must depend on source-release so upload cannot race release creation")

    windows_text = "\n".join(step.get("run", "") for step in windows.get("steps", []))
    check("kernel-release.json" in windows_text,
          "windows-release must read the repo-owned kernel-release.json pin")
    check("kernel_tag" in windows_text and 'gh release view "$kernel_tag"' in windows_text,
          "windows-release must fail closed unless the pinned kernel release exists")
    check("win_amd64" in windows_text,
          "windows-release must require a win_amd64 kernel wheel before building")
    check("GOOS=windows GOARCH=amd64" in windows_text,
          "windows-release must cross-compile for windows/amd64")
    check("lingtai-bundle-manifest.json" in windows_text,
          "windows-release must generate the strict bundle manifest")
    upload_step = find_step(windows, "upload windows release assets")
    check(upload_step is not None, "windows-release must have an asset upload step")
    if upload_step:
        script = upload_step.get("run", "")
        check("gh release upload" in script, "windows-release must upload the Windows assets")
        check("$zip_name" in script and "$zip_name.sha256" in script and "lingtai-bundle-manifest.json" in script,
              "windows-release must upload the ZIP, its sha256 sidecar, and the bundle manifest")

    publish_step = find_step(windows, "publish github release after windows assets")
    check(publish_step is not None,
          "windows-release must publish the draft after the Windows assets are verified and uploaded")
    if publish_step:
        script = publish_step.get("run", "")
        check("gh release edit" in script and "--draft=false" in script,
              "windows-release must publish the draft via gh release edit --draft=false")
        check("gh release upload" not in script, "publish step must not upload assets")
        check("grep -qx" in script and "missing required Windows asset" in script,
              "publish step must verify the required Windows assets exist on the release before publishing")
    if upload_step and publish_step:
        steps = windows.get("steps", [])
        check(steps.index(upload_step) < steps.index(publish_step),
              "draft publish must come after the Windows asset upload step (publish-last)")

    portal_build = find_step(windows, "build lingtai-portal.exe")
    check(portal_build is not None, "windows-release must have a portal build step")
    if portal_build:
        check("continue-on-error" not in portal_build,
              "portal build must fail loud; continue-on-error is forbidden")
        check("id" not in portal_build,
              "portal build must not retain outcome plumbing after fail-loud gating")

    package = find_step(windows, "package windows archive")
    check(package is not None, "windows-release must have a Windows package step")
    if package:
        package_script = package.get("run", "")
        check("test -f dist/lingtai-tui.exe" in package_script,
              "Windows package must require lingtai-tui.exe")
        check("test -f dist/lingtai-portal.exe" in package_script,
              "Windows package must require lingtai-portal.exe")
        check('zip -X "../$zip_name" lingtai-tui.exe lingtai-portal.exe' in package_script,
              "Windows package must include both binaries unconditionally")
        for forbidden in (
            "continue-on-error", "PORTAL_BUILD_OUTCOME", "rm -f dist/lingtai-portal.exe",
            "publishing the Windows archive with lingtai-tui.exe only",
            "-f lingtai-portal.exe ]] &&",
        ):
            check(forbidden not in package_script,
                  f"Windows package must not contain fallback logic {forbidden!r}")

    text = WORKFLOW_PATH.read_text()
    forbidden = (
        "publish-bundle", "publish_bundle_to_gitee.sh",
        "sync_gitee_mirror.sh", "GITEE_ACCESS_TOKEN", "resolve latest kernel",
    )
    for needle in forbidden:
        check(needle not in text, f"release workflow must not contain {needle!r}")

    check(text.count("gh release upload") == 1,
          "gh release upload must appear exactly once, in windows-release")
    check(text.count("gh release edit") == 1,
          "gh release edit must appear exactly once, in windows-release")
    check(text.count("--draft=false") == 1,
          "the draft must be published exactly once (--draft=false), in windows-release")

    smoke = yaml.safe_load(SMOKE_WORKFLOW_PATH.read_text())
    exact_smoke = smoke.get("jobs", {}).get("windows-release-asset-smoke", {})
    exact_steps = "\n".join(step.get("run", "") for step in exact_smoke.get("steps", []))
    check("-SkipVenv" in exact_steps,
          "exact-tag smoke must install with -SkipVenv")
    check("lingtai-portal.exe" in exact_steps and "Test-Path" in exact_steps,
          "exact-tag smoke must require the installed portal executable")
    check("function Invoke-BoundedVersion" in exact_steps,
          "exact-tag smoke must define the bounded version-probe helper Invoke-BoundedVersion")
    check("WaitForExit" in exact_steps and "$p.ExitCode -ne 0" in exact_steps,
          "Invoke-BoundedVersion must bound the probe timeout and fail closed on a non-zero exit")
    check("$reported -ne $Expected" in exact_steps,
          "Invoke-BoundedVersion must assert the probed version output against the expected string")
    check('Invoke-BoundedVersion $tuiPath "lingtai-tui $env:LINGTAI_VERSION"' in exact_steps,
          "exact-tag smoke must run the TUI version surface through Invoke-BoundedVersion and assert its output")
    check('Invoke-BoundedVersion $portalPath "lingtai-portal $env:LINGTAI_VERSION"' in exact_steps,
          "exact-tag smoke must run the portal version surface through Invoke-BoundedVersion and assert its output")

    if FAILURES:
        print("FAILED release workflow checks:", file=sys.stderr)
        for failure in FAILURES:
            print(f"  - {failure}", file=sys.stderr)
        return 1
    print("OK: source GitHub release + Homebrew + gated Windows asset/bundle publication")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
