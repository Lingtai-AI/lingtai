package tui

import (
	"context"
	"encoding/json"
	"os/exec"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// claudeAuthStatusTimeout bounds how long the TUI waits on the Claude
// Code CLI before giving up. The check must never hang the UI; a slow or
// wedged `claude` is treated as "not logged in".
const claudeAuthStatusTimeout = 4 * time.Second

type claudeCodeAuthInfo struct {
	LoggedIn bool
	Email    string
}

// readClaudeCodeAuthInfo asks the installed Claude Code CLI for its login
// status (`claude auth status --json`). This is a local, non-billed check: it
// spends no model call. The TUI never reads Claude credential files.
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

// claudeLoginProbe is the local-login check every surface uses. Tests replace
// it so they never depend on the developer machine's `claude` login.
var claudeLoginProbe = readClaudeCodeAuthInfo

// claudeCodeAuthConfigured reports whether the local Claude Code CLI is logged
// in. Feed this into preset.AuthState.ClaudeCodeAuthConfigured.
func claudeCodeAuthConfigured() bool {
	return claudeLoginProbe().LoggedIn
}

// claudeAuthSource names the credential a claude-code preset runs on.
type claudeAuthSource int

const (
	// claudeAuthNone: no stored setup-token and no local login.
	claudeAuthNone claudeAuthSource = iota
	// claudeAuthToken: a `claude setup-token` token is stored under the
	// preset's api_key_env. It takes precedence over any local login (the
	// kernel then isolates ~/.claude).
	claudeAuthToken
	// claudeAuthLogin: no token, but the local `claude` CLI is logged in.
	claudeAuthLogin
)

// claudeAuthStatus is the resolved Claude credential for one preset.
type claudeAuthStatus struct {
	Source claudeAuthSource
	// Env is the api_key_env slot the token lives in (or would be stored in).
	Env string
	// Email is the local login's account, when the CLI reports one.
	Email string
}

// resolveClaudeAuth applies the Claude credential order: a stored token wins
// without consulting the CLI; otherwise the local login (via login) decides.
// login may be nil when the caller has no probe result.
func resolveClaudeAuth(env string, tokenPresent bool, login func() claudeCodeAuthInfo) claudeAuthStatus {
	st := claudeAuthStatus{Env: env}
	if tokenPresent {
		st.Source = claudeAuthToken
		return st
	}
	if login != nil {
		if info := login(); info.LoggedIn {
			st.Source = claudeAuthLogin
			st.Email = info.Email
		}
	}
	return st
}

// Label is the one-line status shown on the editor auth row, the preset
// library preview, and Setup → Credentials. It never contains a secret.
func (s claudeAuthStatus) Label() string {
	switch s.Source {
	case claudeAuthToken:
		return i18n.TF("claude.auth_token", s.Env)
	case claudeAuthLogin:
		if s.Email != "" {
			return i18n.TF("claude.auth_login_account", s.Email)
		}
		return i18n.T("claude.auth_login")
	}
	return i18n.T("claude.auth_none")
}

// claudeLoginStatusMsg carries an asynchronous local-login probe result to
// whichever model asked for it.
type claudeLoginStatusMsg struct {
	Info claudeCodeAuthInfo
}

// probeClaudeLoginCmd runs claudeLoginProbe off the UI goroutine.
func probeClaudeLoginCmd() tea.Cmd {
	return func() tea.Msg {
		return claudeLoginStatusMsg{Info: claudeLoginProbe()}
	}
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
