#!/usr/bin/env python3
"""Focused assertions for the source-only tag release workflow."""
from __future__ import annotations

import sys
from pathlib import Path

try:
    import yaml
except ModuleNotFoundError:
    print("SKIP: PyYAML not available in this environment", file=sys.stderr)
    raise SystemExit(0)

ROOT = Path(__file__).resolve().parents[1]
WORKFLOW = ROOT / ".github" / "workflows" / "release.yml"
FAILURES: list[str] = []


def check(condition: bool, message: str) -> None:
    if not condition:
        FAILURES.append(message)


def step(job: dict, name: str) -> dict | None:
    for candidate in job.get("steps", []):
        if name.lower() in candidate.get("name", "").lower():
            return candidate
    return None


def main() -> int:
    text = WORKFLOW.read_text()
    data = yaml.safe_load(text)
    trigger = data.get("on") or data.get(True)
    check("push" in trigger and "tags" in trigger["push"], "release.yml must trigger on tag pushes")
    check(any("v*" in tag for tag in trigger["push"]["tags"]), "release.yml must trigger on v* tags")

    jobs = data.get("jobs", {})
    check(set(jobs) == {"source-release", "update-homebrew"},
          f"release must contain only source-release and update-homebrew, got {sorted(jobs)}")

    source = jobs.get("source-release", {})
    checkout = step(source, "checkout peeled release commit")
    check(checkout is not None, "source-release must check out the pushed release commit")
    if checkout:
        check(checkout.get("with", {}).get("fetch-depth") == 0,
              "source checkout must have full history for annotated-tag peeling")
        check(checkout.get("with", {}).get("ref") == "${{ github.ref_name }}",
              "source checkout must use the pushed tag ref")

    archive = step(source, "create deterministic tui source archive")
    check(archive is not None, "source-release must create a deterministic TUI source archive")
    if archive:
        script = archive.get("run", "")
        for needle, message in (
            ('git rev-parse "$TAG^{commit}"', "archive step must resolve the peeled commit"),
            ('git rev-parse "$TAG^{}"', "archive step must compare the peeled tag object target"),
            ('git rev-parse HEAD', "archive step must verify checkout provenance"),
            ('git archive --format=tar --prefix="lingtai-${TAG}/" "$commit"', "archive must be made from the peeled commit"),
            ('gzip -n', "source gzip must be deterministic"),
            ('sha256sum "$source_asset" > "$checksum_asset"', "archive step must create the checksum sidecar"),
            ('test "$(sha256sum "$source_asset"', "archive step must verify the declared checksum"),
        ):
            check(needle in script, message)
        check("lingtai-${TAG}-source.tar.gz" in script, "archive name must be the producer-owned source name")
        check('checksum_asset="${source_asset}.sha256"' in script, "checksum name must derive from the source archive")

    create = step(source, "create draft github release")
    check(create is not None, "source-release must create a draft GitHub release")
    if create:
        script = create.get("run", "")
        check("gh release create" in script and "--verify-tag" in script and "--draft" in script,
              "draft release creation must verify the tag and start as a draft")
        check("gh release upload" not in script and "--draft=false" not in script,
              "draft creation must not publish or upload assets")

    publish = step(source, "publish producer-owned source assets")
    check(publish is not None, "source-release must upload its source archive and checksum")
    if publish:
        script = publish.get("run", "")
        check("gh release upload" in script and "$SOURCE_ASSET" in script and "$CHECKSUM_ASSET" in script,
              "source publication must upload source and checksum assets")
        check("--clobber" in script and "gh release edit" in script and "--draft=false" in script,
              "source publication must verify and publish the draft after upload")

    notify = step(source, "notify lingtai-web")
    check(notify is not None, "source-release must dispatch source-only mirror metadata")
    if notify:
        script = notify.get("run", "")
        check("gh api repos/Lingtai-AI/lingtai-web/dispatches" in script,
              "mirror notification must use the lingtai-web dispatch endpoint")
        check('"source_repo": "Lingtai-AI/lingtai"' in script,
              "mirror notification must identify the TUI source repository")
        check('"assets": assets' in script and "source_path" in script and "checksum_path" in script,
              "mirror notification must contain only source and checksum assets")
        check("windows" not in script.lower() and "portal" not in script.lower() and "bundle" not in script.lower(),
              "mirror notification must be source-only")

    homebrew = jobs.get("update-homebrew", {})
    check(homebrew.get("needs") == "source-release", "Homebrew update must wait for source publication")
    checksum = step(homebrew, "read producer source checksum")
    check(checksum is not None, "Homebrew job must consume the producer source checksum")
    if checksum:
        script = checksum.get("run", "")
        check("SOURCE_ASSET" in script and "SOURCE_SHA" in script,
              "Homebrew checksum step must validate source asset and digest outputs")
    formula = step(homebrew, "write tui-only formula")
    check(formula is not None, "Homebrew job must write the TUI-only formula")
    if formula:
        script = formula.get("run", "")
        check("lingtai-tui.rb" in script and "lingtai-tui" in script,
              "Homebrew formula must be named and built for lingtai-tui")
        check("${SOURCE_ASSET}" in script and 'sha256 "${SOURCE_SHA}"' in script,
              "Homebrew formula must use the producer source asset and checksum")
        check('depends_on "go" => :build' in script, "TUI formula must retain its Go build dependency")

    for forbidden in (
        "windows-release", "lingtai-portal", "lingtai-bundle-manifest", "kernel-release.json",
        "publish_bundle_to_gitee", "sync_gitee_mirror", "GITEE_ACCESS_TOKEN",
    ):
        check(forbidden not in text, f"release workflow must not contain retired producer surface {forbidden!r}")
    check(text.count("gh release upload") == 1, "source+checksum upload must be the only release upload")
    check(text.count("gh release edit") == 1 and text.count("--draft=false") == 1,
          "the published draft transition must occur exactly once")

    if FAILURES:
        print("FAILED release workflow checks:", file=sys.stderr)
        for failure in FAILURES:
            print(f"  - {failure}", file=sys.stderr)
        return 1
    print("OK: deterministic TUI source archive + checksum publication and source-only mirror/Homebrew flow")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
