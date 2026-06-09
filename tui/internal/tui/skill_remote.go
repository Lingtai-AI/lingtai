package tui

import (
	"os/exec"
	"sort"
	"strings"
)

// ── Repo-backed skill detection (issue #172) ────────────────────────────────
//
// A skill directory may live inside a git repository that has a configured
// remote. When it does, the catalog exposes a lightweight `remote` pointer so
// repo-backed skills are visible as such. This is catalog metadata only — it
// does not affect skill loading. See ANATOMY for the non-goals (no provenance
// or evaluation schema).

// gitRemoteForDir returns the configured remote URL for the git repository that
// contains dir, or "" when dir is not inside a git worktree or no remote is
// configured.
//
// Remote selection (mirrors issue #172):
//   - prefer the "origin" remote URL when present;
//   - otherwise fall back to the first remote in deterministic (sorted) order;
//   - otherwise return "".
func gitRemoteForDir(dir string) string {
	if dir == "" {
		return ""
	}
	return resolveGitRemote(dir)
}

func resolveGitRemote(dir string) string {
	// Prefer origin. `git remote get-url` exits non-zero when the remote is
	// absent or dir is not a repo, so a clean exit means a usable URL.
	if url := gitRemoteURL(dir, "origin"); url != "" {
		return url
	}
	// No origin — pick the first configured remote deterministically.
	out, err := gitOutput(dir, "remote")
	if err != nil {
		return ""
	}
	var names []string
	for _, line := range strings.Split(out, "\n") {
		if name := strings.TrimSpace(line); name != "" {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return gitRemoteURL(dir, names[0])
}

func gitRemoteURL(dir, name string) string {
	out, err := gitOutput(dir, "remote", "get-url", name)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(out)
}

// gitOutput runs `git -C dir <args...>` and returns trimmed stdout. The -C flag
// makes git treat dir as the working directory for repo discovery.
func gitOutput(dir string, args ...string) (string, error) {
	full := append([]string{"-C", dir}, args...)
	cmd := exec.Command("git", full...)
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
