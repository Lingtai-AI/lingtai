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

Applying this ruleset is a real workflow change: `main` currently receives direct
pushes, including from `github-actions[bot]`. Expect that to start failing.

### `release-tags.json` — tag ruleset for `refs/tags/v*`

New. Makes release tags immutable once pushed:

- `deletion` — a published release tag cannot be deleted.
- `update` and `non_fast_forward` — a published release tag cannot be moved to a
  different commit.

It deliberately does **not** include `creation`. `creation` restricts tag creation to
bypass actors only, so with `bypass_actors: []` it would block every release. To
restrict who may cut a release, add it together with an explicit bypass list:

```json
{ "type": "creation" }
```
```json
"bypass_actors": [
  { "actor_type": "OrganizationAdmin", "bypass_mode": "always" }
]
```

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

There is no per-ref rules endpoint for tags, so verify the tag ruleset by reading it
back and then by behavior:

```bash
gh api "repos/Lingtai-AI/lingtai/rulesets?targets=tag" \
  --jq '[.[] | {name, enforcement}]'          # -> one active entry

git push origin :refs/tags/<some-old-tag>     # -> must be rejected by the ruleset
```

Read back what GitHub actually stored, not what was sent — the API silently accepts
some shapes it then normalizes:

```bash
gh api repos/Lingtai-AI/lingtai/rulesets/<id> --jq '{conditions, rules: [.rules[].type]}'
```

## Notes

- `Lingtai-AI/homebrew-lingtai` — the tap that `release.yml` pushes to — has no
  rulesets either. The same treatment applies there.
- `bypass_actors` on the two existing rulesets is not readable without admin. Audit it
  when applying: a bypass list survives a `conditions` repair and would quietly undo it.
- A CODEOWNERS file is only load-bearing once `require_code_owner_review` is `true` in
  the `pull_request` rule. Adding one before that changes nothing.
