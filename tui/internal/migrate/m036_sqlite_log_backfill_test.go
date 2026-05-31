package migrate

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSQLiteDiscoverBackfillCandidatesMissingSidecar(t *testing.T) {
	lingtaiDir := t.TempDir()
	agentDir := makeSQLiteBackfillAgentWithEvents(t, lingtaiDir, "agent-a")

	candidates, skipped, err := sqliteDiscoverBackfillCandidates("/definitely/not/used", lingtaiDir)
	if err != nil {
		t.Fatalf("sqliteDiscoverBackfillCandidates: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped running = %d, want 0", skipped)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if candidates[0].AgentDir != agentDir {
		t.Fatalf("candidate agent dir = %q, want %q", candidates[0].AgentDir, agentDir)
	}
	if !strings.Contains(candidates[0].Reason, "missing") {
		t.Fatalf("reason = %q, want missing sidecar", candidates[0].Reason)
	}
}

func TestSQLiteDiscoverBackfillCandidatesExistingSidecarWithoutCursor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake python shell script is POSIX-only")
	}
	lingtaiDir := t.TempDir()
	agentDir := makeSQLiteBackfillAgentWithEvents(t, lingtaiDir, "agent-a")
	if err := os.WriteFile(filepath.Join(agentDir, "logs", "log.sqlite"), []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	python := fakeSQLitePython(t, "printf '[]\\n'")

	candidates, skipped, err := sqliteDiscoverBackfillCandidates(python, lingtaiDir)
	if err != nil {
		t.Fatalf("sqliteDiscoverBackfillCandidates: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped running = %d, want 0", skipped)
	}
	if len(candidates) != 1 {
		t.Fatalf("candidates = %d, want 1", len(candidates))
	}
	if !strings.Contains(candidates[0].Reason, "not been backfilled") {
		t.Fatalf("reason = %q, want missing backfill cursor", candidates[0].Reason)
	}
}

func TestSQLiteDiscoverBackfillCandidatesExistingSidecarWithCursor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake python shell script is POSIX-only")
	}
	lingtaiDir := t.TempDir()
	agentDir := makeSQLiteBackfillAgentWithEvents(t, lingtaiDir, "agent-a")
	if err := os.WriteFile(filepath.Join(agentDir, "logs", "log.sqlite"), []byte("placeholder"), 0o644); err != nil {
		t.Fatal(err)
	}
	python := fakeSQLitePython(t, "printf '[{\"byte_offset\":12,\"line_no\":1}]\\n'")

	candidates, skipped, err := sqliteDiscoverBackfillCandidates(python, lingtaiDir)
	if err != nil {
		t.Fatalf("sqliteDiscoverBackfillCandidates: %v", err)
	}
	if skipped != 0 {
		t.Fatalf("skipped running = %d, want 0", skipped)
	}
	if len(candidates) != 0 {
		t.Fatalf("candidates = %d, want 0", len(candidates))
	}
}

func TestSQLiteBackfillPromptCopyEmphasizesCostProgressAndSafeSkip(t *testing.T) {
	var b strings.Builder
	sqlitePrintBackfillPrompt(&b, []sqliteBackfillCandidate{{Name: "agent-a", EventsBytes: 2048, Reason: "SQLite sidecar is missing"}}, 1)
	out := b.String()
	for _, want := range []string{
		"can take a long time",
		"Skipping is safe",
		"does not affect normal LingTai use",
		"Backfill historical logs now? [y/N]",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("prompt missing %q:\n%s", want, out)
		}
	}
}

func makeSQLiteBackfillAgentWithEvents(t *testing.T, lingtaiDir, name string) string {
	t.Helper()
	agentDir := filepath.Join(lingtaiDir, name)
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte(`{"manifest":{"agent_name":"`+name+`"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(`{"type":"test","ts":1}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return agentDir
}

func fakeSQLitePython(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "python")
	content := "#!/bin/sh\nset -eu\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}
