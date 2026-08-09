# Repository rulesets

GitHub rulesets are per-repository *settings*, not repository *content* — nothing in
this tree applies them. `.github/rulesets/*.json` therefore holds the intended state
as reviewable, diffable JSON, and this document holds the commands an admin runs to
put that state into effect and to prove it took.

Keep the JSON and the live rulesets in sync: change the file in a PR, then re-apply.

## Why these files exist

`Lingtai-AI/lingtai` has two active branch rulesets. Both have an **empty**
`conditions.ref_name.include`, which matches no ref, so neither has ever applied to
anything:

| id | name | rules | `ref_name.include` |
|---|---|---|---|
| 14453254 | `Zesen Huang` | `deletion`, `non_fast_forward` | `[]` |
| 14481209 | `main rule` | `pull_request` | `[]` |

`main` is consequently unprotected. Reproduce:

```bash
gh api repos/Lingtai-AI/lingtai/rules/branches/main      # -> []
gh api repos/Lingtai-AI/lingtai/branches/main --jq .protected   # -> false
```

`/rules/branches/{branch}` is the authoritative "which rules actually apply to this
ref" endpoint — it resolves repository-, organization-, and enterprise-level rulesets
together. Two controls make its empty answer meaningful:

```bash
# Positive control: the same read on a repo that IS protected returns rules,
# including an Organization-sourced one, so the endpoint is not being silenced
# by a low-privilege token.
gh api repos/github/docs/rules/branches/main \
  --jq '[.[] | {type, ruleset_source_type}] | .[0:3]'

# Negative control: [] is not self-validating — a nonexistent ref returns [] too,
# so this result must always be read alongside the positive control.
gh api repos/Lingtai-AI/lingtai/rules/branches/no-such-branch    # -> []

# Organization-level parents ruled out: both entries are source_type Repository.
gh api "repos/Lingtai-AI/lingtai/rulesets?includes_parents=true" \
  --jq '[.[] | {name, source_type, target}]'
```

There is also **no tag ruleset at all** (`gh api
"repos/Lingtai-AI/lingtai/rulesets?targets=tag" --jq length` returns `0`). Tag pushes
are what trigger `.github/workflows/release.yml`, and a branch ruleset never covers
tags — so nothing currently constrains the ref that drives the release pipeline.

## Intended state

### `main.json` — branch ruleset for the default branch

This is the *existing* configuration with the one broken field repaired. It merges
what the two current rulesets already declare into a single ruleset that actually
targets a ref:

- `conditions.ref_name.include: ["~DEFAULT_BRANCH"]` — the fix. `~DEFAULT_BRANCH` and
  `~ALL` are the two special targets; `~ALL` would gate every feature branch, so it is
  the wrong choice here.
- `deletion` and `non_fast_forward` — carried over from `Zesen Huang`. Blocks
  force-push and deletion of `main`.
- `pull_request` — carried over from `main rule`, **parameters unchanged**, including
  `required_approving_review_count: 0`.

> **`required_approving_review_count: 0` means a PR is required but no approval is.**
> The author can open and merge their own PR unreviewed. That is what is configured
> today; this file preserves it rather than quietly changing policy. Raising it is a
> one-line edit, but it is a team-size decision — on a repo where one person does
> nearly all the pushing, `1` blocks all work until a second reviewer exists.

Applying this ruleset codifies the workflow already in use rather than changing it.
The only workflow that pushed to `main` as `github-actions[bot]` was
`.github/workflows/star-tracker.yml`, removed on 2026-07-23 in `71cf16c6`
(`chore(actions): remove daily star tracker (#716)`). No workflow pushes to this
repository any more: `release.yml` pushes to the `Lingtai-AI/homebrew-lingtai` tap and
`sync-hf.yml` pushes to Hugging Face — neither touches `Lingtai-AI/lingtai`. Recent
history on `main` is already entirely PR-based.

The one observable change is that with `bypass_actors: []` admins lose the ability to
push straight to `main` and must go through a PR like everyone else. There is no
pending migration cost gating this fix.

### `release-tags.json` — tag ruleset for `refs/tags/v*`

New. Makes release tags immutable once pushed:

- `deletion` — a published release tag cannot be deleted.
- `update` and `non_fast_forward` — a published release tag cannot be moved to a
  different commit.

With `bypass_actors: []` these bind admins too: once applied, nobody can delete or move
a `v*` tag. That is the intended property, not an oversight — but read the Recovery
section below before applying, because it is the only documented way back and the
improvised alternative is deleting the ruleset.

It deliberately does **not** include `creation`. `creation` restricts tag creation to
bypass actors only, so with `bypass_actors: []` it would block every release. To
restrict who may cut a release, add it together with an explicit bypass list. Both
fields below go into `release-tags.json` — `creation` appended to the existing `rules`
array, `bypass_actors` replacing the empty one:

```json
{
  "rules": [
    { "type": "creation" }
  ],
  "bypass_actors": [
    { "actor_type": "OrganizationAdmin", "actor_id": 1, "bypass_mode": "always" }
  ]
}
```

`actor_id` is required alongside `actor_type`; the API returns 422 without it.
`OrganizationAdmin` is `actor_id: 1`.

Decide the bypass list deliberately — it is the only thing standing between the rule
and a broken release pipeline.

## Apply

Requires admin on the repository. Create the new rulesets first, verify, then retire
the two inert ones:

```bash
gh api -X POST repos/Lingtai-AI/lingtai/rulesets --input .github/rulesets/main.json
gh api -X POST repos/Lingtai-AI/lingtai/rulesets --input .github/rulesets/release-tags.json
```

```bash
gh api -X DELETE repos/Lingtai-AI/lingtai/rulesets/14453254   # "Zesen Huang"  (inert)
gh api -X DELETE repos/Lingtai-AI/lingtai/rulesets/14481209   # "main rule"    (inert)
```

To update an already-applied ruleset instead of creating a duplicate, use its id:

```bash
gh api -X PUT repos/Lingtai-AI/lingtai/rulesets/<id> --input .github/rulesets/main.json
```

## Verify

The branch ruleset has a direct check — this must go from `[]` to a non-empty array:

```bash
gh api repos/Lingtai-AI/lingtai/rules/branches/main \
  --jq '[.[] | {type, ruleset_source_type}]'
```

There is no per-ref rules endpoint for tags, so start with a read-only check:

```bash
gh api "repos/Lingtai-AI/lingtai/rulesets?targets=tag" \
  --jq '[.[] | {name, enforcement}]'          # -> one active entry
```

Read back what GitHub actually stored, not what was sent — the API silently accepts
some shapes it then normalizes, and a ruleset whose `conditions` match no ref looks
identical to a working one in the settings UI. That failure is the entire subject of
this document:

```bash
gh api repos/Lingtai-AI/lingtai/rulesets/<id> \
  --jq '{conditions, rules: [.rules[].type]}'
```

Confirm specifically that `conditions.ref_name.include` came back as
`["refs/tags/v*"]` and not `[]`, and that `non_fast_forward` survived — it is a
branch-oriented rule, so it is the field most likely to have been rejected or
normalized away on a tag target. (`update` already blocks moving a tag, so losing
`non_fast_forward` is not itself a problem; a POST that failed as a whole is.)

> **Never verify a tag rule by attempting to delete a real release tag.** A command
> like `git push origin :refs/tags/v1.2.3` proves the rule works only when it is
> *rejected* — and the case where it is accepted is exactly the case above, a ruleset
> that looks applied but matches no ref. This repository has 117 `v*` tags, and
> `release.yml` builds the Homebrew formula from
> `archive/refs/tags/${TAG}.tar.gz` with a pinned sha256; deleting a published tag
> 404s that URL and breaks the already-shipped formula for every user of that version.

Probe with a throwaway tag instead, never a release tag:

```bash
git tag v0.0.0-ruleset-probe
git push origin v0.0.0-ruleset-probe          # allowed: no `creation` rule
git push origin :refs/tags/v0.0.0-ruleset-probe   # -> must be rejected by the ruleset
```

The rejection is the proof. Note that the probe tag is now itself undeletable — that
is the same property working as intended — so clearing it requires the recovery path
below.

## Recovery — undoing a tag once `release-tags.json` is active

`release-tags.json` ships `bypass_actors: []` with `deletion`, `update` and
`non_fast_forward`. That makes a pushed `v*` tag immutable **for everyone**, including
repository and organization admins. Immutability is the point, but it means a mis-cut
release has no in-place fix, and the improvised one — deleting the ruleset — is the
exact drift this document exists to prevent.

**Prefer not to recover.** For a bad release, cut `vX.Y.Z+1` and yank the bad version
downstream. Moving or deleting a published ref breaks anything that already resolved
it: the Homebrew formula pins a sha256 against the tag tarball, and consumers may have
the old tag fetched. A superseding tag is cheaper and leaves history honest.

**When a tag genuinely must go** (a secret was committed, the tag points at the wrong
repository state entirely), flip enforcement off, act, flip it back — and read back
after each flip, because a silently-failed re-enable leaves the repository unprotected
while looking fine:

```bash
# 1. Find the ruleset id.
gh api "repos/Lingtai-AI/lingtai/rulesets?targets=tag" --jq '.[] | {id, name}'

# 2. Disable, and confirm the flip took.
gh api -X PUT repos/Lingtai-AI/lingtai/rulesets/<id> -f enforcement=disabled
gh api repos/Lingtai-AI/lingtai/rulesets/<id> --jq .enforcement    # -> "disabled"

# 3. Delete or re-cut the tag.
git push origin :refs/tags/vX.Y.Z
git tag -f vX.Y.Z <sha> && git push origin vX.Y.Z

# 4. Re-enable, and confirm again. Do not skip this read-back.
gh api -X PUT repos/Lingtai-AI/lingtai/rulesets/<id> -f enforcement=active
gh api repos/Lingtai-AI/lingtai/rulesets/<id> \
  --jq '{enforcement, conditions, rules: [.rules[].type]}'
```

Step 4's read-back must show `enforcement: "active"` *and* a non-empty
`conditions.ref_name.include`. Checking only `enforcement` reproduces the original bug:
an active ruleset that matches nothing.

Do not use `-X DELETE` on the ruleset as a shortcut. Deleting and recreating loses the
id, and a recreate is one typo away from the empty-`include` state documented above.

The same procedure clears the `v0.0.0-ruleset-probe` tag from the Verify section.

If this becomes routine rather than exceptional, that is the signal to ship a real
bypass list — `{ "actor_type": "OrganizationAdmin", "actor_id": 1, "bypass_mode":
"always" }` — instead of toggling enforcement by hand each time.

## Notes

- `Lingtai-AI/homebrew-lingtai` — the tap that `release.yml` pushes to — has no
  rulesets either. The same treatment applies there.
- `bypass_actors` on the two existing rulesets is not readable without admin. Audit it
  when applying: a bypass list survives a `conditions` repair and would quietly undo it.
- A CODEOWNERS file is only load-bearing once `require_code_owner_review` is `true` in
  the `pull_request` rule. Adding one before that changes nothing.
