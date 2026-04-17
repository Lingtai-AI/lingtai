package fs

import (
	"path/filepath"
	"testing"
)

func TestParseAddress(t *testing.T) {
	tests := []struct {
		addr     string
		wantHost string
		wantPath string
		wantOK   bool
	}{
		{"human", "", "", false},
		{"agent_auditor", "", "", false},
		{"本我", "", "", false},
		{"../other/agent", "", "", false},
		{"/Users/bob/.lingtai/agent", "", "", false},
		{"localhost:/Users/alice/.lingtai/agent_a", "localhost", "/Users/alice/.lingtai/agent_a", true},
		{"[2001:db8::1]:/home/user/.lingtai/agent_b", "2001:db8::1", "/home/user/.lingtai/agent_b", true},
		{"[fe80::1%25en0]:/home/user/.lingtai/agent_c", "fe80::1%25en0", "/home/user/.lingtai/agent_c", true},
		{"[2001:db8::1]:7777:/home/user/.lingtai/agent_d", "2001:db8::1", "/home/user/.lingtai/agent_d", true},
		{"not-a-host:relative", "", "", false},
		{"localhost:", "", "", false},
		{"localhost:relative/path", "", "", false},
	}

	for _, tt := range tests {
		host, path, ok := ParseAddress(tt.addr)
		if ok != tt.wantOK || host != tt.wantHost || path != tt.wantPath {
			t.Errorf("ParseAddress(%q) = (%q, %q, %v), want (%q, %q, %v)",
				tt.addr, host, path, ok, tt.wantHost, tt.wantPath, tt.wantOK)
		}
	}
}

func TestIsRemoteAddress(t *testing.T) {
	tests := []struct {
		addr string
		want bool
	}{
		{"human", false},
		{"/Users/bob/.lingtai/agent", false},
		{"localhost:/Users/alice/.lingtai/agent_a", false},
		{"[2001:db8::1]:/home/user/.lingtai/agent_b", true},
		{"[fe80::1%25en0]:/home/user/.lingtai/agent_c", true},
	}

	for _, tt := range tests {
		got := IsRemoteAddress(tt.addr)
		if got != tt.want {
			t.Errorf("IsRemoteAddress(%q) = %v, want %v", tt.addr, got, tt.want)
		}
	}
}

func TestFormatAbsoluteAddress(t *testing.T) {
	tests := []struct {
		host string
		path string
		want string
	}{
		{"localhost", "/path", "localhost:/path"},
		{"2001:db8::1", "/path", "[2001:db8::1]:/path"},
	}

	for _, tt := range tests {
		got := FormatAbsoluteAddress(tt.host, tt.path)
		if got != tt.want {
			t.Errorf("FormatAbsoluteAddress(%q, %q) = %q, want %q",
				tt.host, tt.path, got, tt.want)
		}
	}
}

func TestResolveAddress(t *testing.T) {
	baseDir := "/Users/alice/project/.lingtai"

	tests := []struct {
		addr string
		want string
	}{
		{"本我", filepath.Join(baseDir, "本我")},
		{"human", filepath.Join(baseDir, "human")},
		{"/Users/bob/other/.lingtai/agent", "/Users/bob/other/.lingtai/agent"},
		{"localhost:/Users/bob/.lingtai/agent", "localhost:/Users/bob/.lingtai/agent"},
		{"[2001:db8::1]:/home/user/.lingtai/agent", "[2001:db8::1]:/home/user/.lingtai/agent"},
	}

	for _, tt := range tests {
		got := ResolveAddress(tt.addr, baseDir)
		if got != tt.want {
			t.Errorf("ResolveAddress(%q, %q) = %q, want %q", tt.addr, baseDir, got, tt.want)
		}
	}
}
