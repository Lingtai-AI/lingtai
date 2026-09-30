package tui

import (
	"os"
	"testing"
)

// TestMain keeps the package hermetic: no test may exec the developer
// machine's `claude` (which can also write ~/.claude.json under a temp HOME
// and trip the draft zero-write checks). Tests that need a login state stub
// claudeLoginProbe themselves via stubClaudeLogin.
func TestMain(m *testing.M) {
	claudeLoginProbe = func() claudeCodeAuthInfo { return claudeCodeAuthInfo{} }
	os.Exit(m.Run())
}

// stubClaudeLogin replaces the local-login probe for one test.
func stubClaudeLogin(t *testing.T, info claudeCodeAuthInfo) *int {
	t.Helper()
	calls := 0
	old := claudeLoginProbe
	claudeLoginProbe = func() claudeCodeAuthInfo {
		calls++
		return info
	}
	t.Cleanup(func() { claudeLoginProbe = old })
	return &calls
}

// TestParseClaudeAuthStatus locks in the tolerant parsing of
// `claude auth status` output. The CLI defaults to JSON with a
// "loggedIn" boolean; we also tolerate the --text form and never treat
// ambiguous/garbage output as logged-in.
func TestParseClaudeAuthStatus(t *testing.T) {
	cases := []struct {
		name string
		out  string
		want bool
	}{
		{
			name: "json logged in",
			out: `{
  "loggedIn": true,
  "authMethod": "claude.ai",
  "email": "user@example.com",
  "subscriptionType": "max"
}`,
			want: true,
		},
		{
			name: "json logged out",
			out:  `{"loggedIn": false}`,
			want: false,
		},
		{
			name: "json logged in with leading log lines",
			out:  "fetching status...\n{\"loggedIn\":true,\"authMethod\":\"claude.ai\"}\n",
			want: true,
		},
		{
			name: "text logged in",
			out:  "Logged in as user@example.com (claude.ai, max)",
			want: true,
		},
		{
			name: "text not logged in",
			out:  "Not logged in. Run `claude auth login` to sign in.",
			want: false,
		},
		{
			name: "empty output",
			out:  "",
			want: false,
		},
		{
			name: "unrelated output",
			out:  "command not found",
			want: false,
		},
		{
			name: "json wins over a stray logged-in word in a not-logged-in body",
			out:  `{"loggedIn": false, "hint": "previously logged in"}`,
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := parseClaudeAuthStatus([]byte(tc.out)); got != tc.want {
				t.Errorf("parseClaudeAuthStatus(%q) = %v, want %v", tc.out, got, tc.want)
			}
		})
	}
}

// TestReadClaudeCodeAuthInfo_MissingBinary verifies the real probe returns
// "not logged in" (never panics or hangs) when the claude CLI is not on PATH.
func TestReadClaudeCodeAuthInfo_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a dir with no `claude` binary
	if info := readClaudeCodeAuthInfo(); info.LoggedIn {
		t.Errorf("readClaudeCodeAuthInfo() = %#v with no claude on PATH, want logged out", info)
	}
}

// TestResolveClaudeAuthOrder pins the credential order: a stored
// setup-token wins without even asking the CLI; otherwise the local login
// decides; neither is "none".
func TestResolveClaudeAuthOrder(t *testing.T) {
	calls := 0
	loggedIn := func() claudeCodeAuthInfo {
		calls++
		return claudeCodeAuthInfo{LoggedIn: true, Email: "user@example.com"}
	}
	st := resolveClaudeAuth("CLAUDE_CODE_OAUTH_TOKEN", true, loggedIn)
	if st.Source != claudeAuthToken || calls != 0 {
		t.Fatalf("token present: source=%v probe calls=%d, want token without probing", st.Source, calls)
	}
	if got := st.Label(); got != "✓ setup-token stored (CLAUDE_CODE_OAUTH_TOKEN)" {
		t.Fatalf("token label = %q", got)
	}
	st = resolveClaudeAuth("CLAUDE_CODE_OAUTH_TOKEN", false, loggedIn)
	if st.Source != claudeAuthLogin || st.Email != "user@example.com" {
		t.Fatalf("login: %#v, want local login with account", st)
	}
	if got := st.Label(); got != "✓ using local Claude login (user@example.com)" {
		t.Fatalf("login label = %q", got)
	}
	st = resolveClaudeAuth("CLAUDE_CODE_OAUTH_TOKEN", false, func() claudeCodeAuthInfo { return claudeCodeAuthInfo{} })
	if st.Source != claudeAuthNone {
		t.Fatalf("neither: source=%v, want none", st.Source)
	}
	if got := st.Label(); got != "✗ no local Claude login and no setup-token" {
		t.Fatalf("none label = %q", got)
	}
}

func TestParseClaudeAuthInfoReturnsCurrentAccount(t *testing.T) {
	info := parseClaudeAuthInfo([]byte(`{
	  "loggedIn": true,
	  "authMethod": "claude.ai",
	  "email": "user@example.com",
	  "subscriptionType": "max"
	}`))
	if !info.LoggedIn {
		t.Fatal("LoggedIn = false, want true")
	}
	if info.Email != "user@example.com" {
		t.Fatalf("Email = %q, want user@example.com", info.Email)
	}

	loggedOut := parseClaudeAuthInfo([]byte(`{"loggedIn":false,"email":"stale@example.com"}`))
	if loggedOut.LoggedIn || loggedOut.Email != "" {
		t.Fatalf("logged-out info = %#v, want no account", loggedOut)
	}
}
