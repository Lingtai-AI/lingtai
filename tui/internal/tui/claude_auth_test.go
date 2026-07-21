package tui

import "testing"

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

// TestClaudeCodeAuthConfigured_MissingBinary verifies the wrapper returns
// false (never panics or hangs) when the claude CLI is not on PATH.
func TestClaudeCodeAuthConfigured_MissingBinary(t *testing.T) {
	t.Setenv("PATH", t.TempDir()) // a dir with no `claude` binary
	if claudeCodeAuthConfigured() {
		t.Errorf("claudeCodeAuthConfigured() = true with no claude on PATH, want false")
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
