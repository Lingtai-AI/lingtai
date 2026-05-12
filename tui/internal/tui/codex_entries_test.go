package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultCommandsIncludesKnowledge(t *testing.T) {
	for _, cmd := range DefaultCommands() {
		if cmd.Name == "knowledge" {
			if cmd.Description != "palette.knowledge" || cmd.Detail != "cmd.knowledge" {
				t.Fatalf("knowledge command keys = (%q, %q), want (palette.knowledge, cmd.knowledge)", cmd.Description, cmd.Detail)
			}
			return
		}
	}
	t.Fatal("DefaultCommands() missing knowledge command")
}

func TestBuildAgentCodexEntries_ReadsFilesystemKnowledge(t *testing.T) {
	agentDir := t.TempDir()
	knowledgePath := filepath.Join(agentDir, "knowledge", "research", "mimo", "KNOWLEDGE.md")
	if err := os.MkdirAll(filepath.Dir(knowledgePath), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: \"MiMo provider notes\"\ndescription: >\n  How to choose MiMo endpoints\n  from the live docs.\nversion: 1.0.0\n---\n# MiMo provider notes\n\nUse the live docs.\n"
	if err := os.WriteFile(knowledgePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildAgentCodexEntries(agentDir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.Label != "MiMo provider notes" {
		t.Errorf("label = %q, want %q", got.Label, "MiMo provider notes")
	}
	if got.Group != filepath.Join("research", "mimo") {
		t.Errorf("group = %q, want %q", got.Group, filepath.Join("research", "mimo"))
	}
	if got.Path != knowledgePath {
		t.Errorf("path = %q, want %q", got.Path, knowledgePath)
	}
	if got.Content != "" {
		t.Errorf("filesystem knowledge entries should be path-backed, got content %q", got.Content)
	}
}

func TestBuildAgentCodexEntries_FallsBackToLegacyCodex(t *testing.T) {
	agentDir := t.TempDir()
	codexDir := filepath.Join(agentDir, "codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(codexFile{Entries: []codexEntry{{
		ID:        "codex_test",
		Title:     "Legacy entry",
		Summary:   "old store",
		Content:   "Legacy body.",
		CreatedAt: "2026-05-12T21:00:00Z",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "codex.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildAgentCodexEntries(agentDir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.Label != "Legacy entry" {
		t.Errorf("label = %q, want %q", got.Label, "Legacy entry")
	}
	if got.Group != "Legacy codex" {
		t.Errorf("group = %q, want %q", got.Group, "Legacy codex")
	}
	if got.Path != "" {
		t.Errorf("legacy entries should be content-backed, got path %q", got.Path)
	}
	if got.Content == "" {
		t.Fatal("legacy entry content is empty")
	}
}
