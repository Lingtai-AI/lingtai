package tui

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"
)

// claudeAuthStatusTimeout bounds how long the TUI waits on the Claude
// Code CLI before giving up. The detection runs at render/health-check
// time, so it must never hang the UI; a slow or wedged `claude` is
// treated as "not configured".
const claudeAuthStatusTimeout = 4 * time.Second

type claudeCodeAuthInfo struct {
	LoggedIn bool
	Email    string
}

// readClaudeCodeAuthInfo asks the installed Claude Code CLI for the same OAuth
// status it owns. The TUI never reads Claude credential files or stores a token.
func readClaudeCodeAuthInfo() claudeCodeAuthInfo {
	if _, err := exec.LookPath("claude"); err != nil {
		return claudeCodeAuthInfo{}
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeAuthStatusTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "claude", "auth", "status", "--json")
	out, _ := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return claudeCodeAuthInfo{}
	}
	return parseClaudeAuthInfo(out)
}

// claudeCodeAuthConfigured reports whether the local Claude Code CLI is logged
// in. Feed this into preset.AuthState.ClaudeCodeAuthConfigured.
func claudeCodeAuthConfigured() bool {
	return readClaudeCodeAuthInfo().LoggedIn
}

// claudeCodeAccount returns the account email reported by the current Claude
// Code OAuth session. Empty means logged out, unavailable, or a CLI version that
// did not expose account metadata.
func claudeCodeAccount() string {
	info := readClaudeCodeAuthInfo()
	if !info.LoggedIn {
		return ""
	}
	return info.Email
}

// parseClaudeAuthInfo tolerantly parses `claude auth status`. Structured JSON
// is authoritative and carries the account email. Text output remains a
// conservative login fallback but does not invent an account identity.
func parseClaudeAuthInfo(out []byte) claudeCodeAuthInfo {
	s := strings.TrimSpace(string(out))
	if s == "" {
		return claudeCodeAuthInfo{}
	}

	if i := strings.IndexByte(s, '{'); i >= 0 {
		if j := strings.LastIndexByte(s, '}'); j > i {
			var doc struct {
				LoggedIn *bool  `json:"loggedIn"`
				Email    string `json:"email"`
			}
			if err := json.Unmarshal([]byte(s[i:j+1]), &doc); err == nil && doc.LoggedIn != nil {
				info := claudeCodeAuthInfo{LoggedIn: *doc.LoggedIn}
				if info.LoggedIn {
					info.Email = strings.TrimSpace(doc.Email)
				}
				return info
			}
		}
	}

	lower := strings.ToLower(s)
	if strings.Contains(lower, "not logged in") || strings.Contains(lower, "logged out") {
		return claudeCodeAuthInfo{}
	}
	return claudeCodeAuthInfo{LoggedIn: strings.Contains(lower, "logged in")}
}

// parseClaudeAuthStatus preserves the narrow boolean parser contract used by
// existing health checks and tests.
func parseClaudeAuthStatus(out []byte) bool {
	return parseClaudeAuthInfo(out).LoggedIn
}
