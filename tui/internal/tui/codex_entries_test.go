package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
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
	knowledgePath := filepath.Join(agentDir, "knowledge", "mimo", "KNOWLEDGE.md")
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
	if got.Group != "Knowledge" {
		t.Errorf("group = %q, want %q", got.Group, "Knowledge")
	}
	if got.Path != knowledgePath {
		t.Errorf("path = %q, want %q", got.Path, knowledgePath)
	}
	if got.Content != "" {
		t.Errorf("filesystem knowledge entries should be path-backed, got content %q", got.Content)
	}
}

func TestBuildAgentCodexEntries_HidesNestedSubKnowledgeFromTopLayer(t *testing.T) {
	agentDir := t.TempDir()
	parentPath := filepath.Join(agentDir, "knowledge", "session-journal", "KNOWLEDGE.md")
	childPath := filepath.Join(agentDir, "knowledge", "session-journal", "2026-06-09-molt-1-child", "KNOWLEDGE.md")
	if err := os.MkdirAll(filepath.Dir(childPath), 0o755); err != nil {
		t.Fatal(err)
	}
	parentBody := "---\nname: session-journal\ndescription: Routing index for session journals.\n---\n# Session Journal Index\n"
	childBody := "---\nname: child-entry\ndescription: Second layer detail that should not appear in the top-level catalog.\n---\n# Child Entry\n"
	if err := os.WriteFile(parentPath, []byte(parentBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(childPath, []byte(childBody), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildAgentCodexEntries(agentDir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 top-level parent only: %+v", len(entries), entries)
	}
	if entries[0].Label != "session-journal" {
		t.Fatalf("top-level label = %q, want session-journal", entries[0].Label)
	}
	if entries[0].Path != parentPath {
		t.Fatalf("top-level path = %q, want %q", entries[0].Path, parentPath)
	}

	drillInEntries := buildKnowledgeFolderEntries(filepath.Dir(entries[0].Path))
	foundChild := false
	for _, entry := range drillInEntries {
		if entry.Path == childPath {
			foundChild = true
			break
		}
	}
	if !foundChild {
		t.Fatalf("nested sub-knowledge should remain reachable after drilling into the parent; entries=%+v", drillInEntries)
	}
}

func TestBuildAgentCodexEntries_MigratesLegacyCodexJSON(t *testing.T) {
	agentDir := t.TempDir()
	codexDir := filepath.Join(agentDir, "codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	codexPath := filepath.Join(codexDir, "codex.json")
	raw, err := json.Marshal(codexFile{Entries: []codexEntry{{
		ID:            "codex_test",
		Title:         "Legacy entry",
		Summary:       "old store",
		Content:       "Legacy body.",
		Supplementary: "extra material",
		CreatedAt:     "2026-05-12T21:00:00Z",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codexPath, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildAgentCodexEntries(agentDir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	got := entries[0]
	if got.Group == "Legacy codex" {
		t.Errorf("group should not be 'Legacy codex', got %q", got.Group)
	}
	if got.Path == "" {
		t.Errorf("migrated entries should be filesystem-backed, got empty Path")
	}
	if got.Content != "" {
		t.Errorf("migrated entries should not carry inline content, got %q", got.Content)
	}
	if got.Label != "legacy-entry" {
		t.Errorf("label = %q, want slugged %q", got.Label, "legacy-entry")
	}

	if _, err := os.Stat(codexPath); !os.IsNotExist(err) {
		t.Errorf("expected legacy codex.json removed; err=%v", err)
	}
	if _, err := os.Stat(codexPath + ".migrated"); err != nil {
		t.Errorf("expected codex.json.migrated to exist: %v", err)
	}

	body, err := os.ReadFile(got.Path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "origin: \"migrated-codex-json\"") {
		t.Errorf("KNOWLEDGE.md missing origin marker: %s", body)
	}
	if !strings.Contains(string(body), "Legacy body.") {
		t.Errorf("KNOWLEDGE.md missing content: %s", body)
	}

	suppPath := filepath.Join(filepath.Dir(got.Path), "references", "supplementary.md")
	if data, err := os.ReadFile(suppPath); err != nil {
		t.Errorf("expected supplementary.md: %v", err)
	} else if !strings.Contains(string(data), "extra material") {
		t.Errorf("supplementary.md missing content: %s", data)
	}
}

func TestBuildAgentCodexEntries_MigrationIsIdempotent(t *testing.T) {
	agentDir := t.TempDir()
	codexDir := filepath.Join(agentDir, "codex")
	if err := os.MkdirAll(codexDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(codexFile{Entries: []codexEntry{
		{ID: "a", Title: "First", Content: "one", CreatedAt: "2026-05-12T21:00:00Z"},
		{ID: "b", Title: "Second", Content: "two", CreatedAt: "2026-05-12T22:00:00Z"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(codexDir, "codex.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	first := buildAgentCodexEntries(agentDir)
	second := buildAgentCodexEntries(agentDir)
	if len(first) != 2 || len(second) != 2 {
		t.Fatalf("expected 2 entries on both runs, got %d then %d", len(first), len(second))
	}
	for i := range first {
		if first[i].Path != second[i].Path {
			t.Errorf("entry %d path drifted: %q vs %q", i, first[i].Path, second[i].Path)
		}
	}
}

func TestBuildAgentCodexEntries_MigratesLegacyKnowledgeJSON(t *testing.T) {
	agentDir := t.TempDir()
	knowledgeDir := filepath.Join(agentDir, "knowledge")
	if err := os.MkdirAll(knowledgeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(codexFile{Entries: []codexEntry{{
		ID: "k1", Title: "From knowledge.json", Summary: "summary", Content: "body",
	}}})
	if err != nil {
		t.Fatal(err)
	}
	legacy := filepath.Join(knowledgeDir, "knowledge.json")
	if err := os.WriteFile(legacy, raw, 0o644); err != nil {
		t.Fatal(err)
	}

	entries := buildAgentCodexEntries(agentDir)
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1", len(entries))
	}
	if entries[0].Group == "Legacy codex" {
		t.Errorf("group should not be legacy fallback, got %q", entries[0].Group)
	}
	if _, err := os.Stat(legacy + ".migrated"); err != nil {
		t.Errorf("expected knowledge.json.migrated: %v", err)
	}
}
