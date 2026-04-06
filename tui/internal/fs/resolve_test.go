package fs

import (
	"path/filepath"
	"testing"
)

func TestResolveAddress(t *testing.T) {
	baseDir := "/Users/alice/project/.lingtai"

	tests := []struct {
		addr string
		want string
	}{
		{"本我", filepath.Join(baseDir, "本我")},
		{"human", filepath.Join(baseDir, "human")},
		{"/Users/bob/other/.lingtai/agent", "/Users/bob/other/.lingtai/agent"},
		{baseDir + "/guide", baseDir + "/guide"},
	}

	for _, tt := range tests {
		got := ResolveAddress(tt.addr, baseDir)
		if got != tt.want {
			t.Errorf("ResolveAddress(%q, %q) = %q, want %q", tt.addr, baseDir, got, tt.want)
		}
	}
}
