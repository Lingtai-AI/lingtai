package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDetachedSessionCacheSteadyRefreshRemainsMemoryOnly(t *testing.T) {
	root := t.TempDir()
	humanDir := filepath.Join(root, ".lingtai", "human")
	sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")
	cache := MailCache{Messages: []MailMessage{{
		ID:         "first",
		MailboxID:  "first",
		From:       "human",
		To:         "agent-b",
		Message:    "first",
		ReceivedAt: "2026-07-13T10:00:00Z",
	}}}

	sc := NewSessionCache(humanDir, root, NoPersist)
	sc.RebuildFromSourcesInMemory(cache, "human", "", "agent-b")
	cache.Messages = append(cache.Messages, MailMessage{
		ID:         "second",
		MailboxID:  "second",
		From:       "agent-b",
		To:         "human",
		Message:    "second",
		ReceivedAt: "2026-07-13T10:01:00Z",
	})
	sc.Refresh(cache, "human", "", "agent-b")

	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("memory-only cache steady refresh wrote %s; stat err = %v", sessionPath, err)
	}
	if got := sc.Len(); got != 2 {
		t.Fatalf("memory-only cache length = %d, want 2", got)
	}
}

func TestNoPersistSessionCacheNeverChangesAggregateFile(t *testing.T) {
	root := t.TempDir()
	humanDir := filepath.Join(root, ".lingtai", "human")
	sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const original = "existing-main-aggregate\n"
	if err := os.WriteFile(sessionPath, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	cache := MailCache{Messages: []MailMessage{{
		ID: "only", MailboxID: "only", From: "agent-b", To: "human",
		Message: "short complete history", ReceivedAt: "2026-07-13T10:00:00Z",
	}}}

	sc := NewSessionCache(humanDir, root, NoPersist)
	sc.RebuildFromSources(cache, "human", "", "agent-b")
	if !sc.Complete() {
		t.Fatal("short full rebuild should be complete")
	}
	sc.Persist()
	cache.Messages = append(cache.Messages, MailMessage{
		ID: "next", MailboxID: "next", From: "agent-b", To: "human",
		Message: "steady append", ReceivedAt: "2026-07-13T10:01:00Z",
	})
	sc.Refresh(cache, "human", "", "agent-b")

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != original {
		t.Fatalf("NoPersist changed aggregate file: %q", data)
	}
}

func TestMainAggregateWriterRebuildAndRefreshPersist(t *testing.T) {
	root := t.TempDir()
	humanDir := filepath.Join(root, ".lingtai", "human")
	sessionPath := filepath.Join(humanDir, "logs", "session.jsonl")
	cache := MailCache{Messages: []MailMessage{{
		ID: "first", MailboxID: "first", From: "human", To: "main",
		Message: "first", ReceivedAt: "2026-07-13T10:00:00Z",
	}}}
	sc := NewSessionCache(humanDir, root, MainAggregateWriter)
	sc.RebuildFromSources(cache, "human", "", "main")
	cache.Messages = append(cache.Messages, MailMessage{
		ID: "second", MailboxID: "second", From: "main", To: "human",
		Message: "second", ReceivedAt: "2026-07-13T10:01:00Z",
	})
	sc.Refresh(cache, "human", "", "main")

	data, err := os.ReadFile(sessionPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := len(sc.Entries()); got != 2 {
		t.Fatalf("writer entries = %d, want 2", got)
	}
	if string(data) == "" || !containsAll(string(data), "first", "second") {
		t.Fatalf("writer aggregate file missing rebuild/append entries: %q", data)
	}
}

func containsAll(s string, values ...string) bool {
	for _, value := range values {
		if !strings.Contains(s, value) {
			return false
		}
	}
	return true
}
