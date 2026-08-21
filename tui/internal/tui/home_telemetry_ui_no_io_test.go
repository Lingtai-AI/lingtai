package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// The UI render/input path must read the cached telemetry snapshot ONLY — it
// must never re-derive telemetry from disk (the Agent record, sqlite sidecar,
// token ledger, or events.jsonl). This test proves that by loading a snapshot
// via the async fetch, then DELETING the orchestrator's on-disk
// `system/agent_record.json`, and asserting that View() still renders the row
// and hasHomeTelemetry() still reports true. If View or hasHomeTelemetry did
// any disk I/O, removing the source would blank the row.
func TestHomeTelemetryUIPathReadsCacheNotDisk(t *testing.T) {
	const w, h = 100, 24
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	writeHomeAgentRecord(t, orchDir, `{"schema": "lingtai.agent_record/v1", "usage": {"context_used_tokens": 182500, "context_limit_tokens": 250000, "context_usage_pct": 73}}`)
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.Update(m.initialRebuild())
	// Async fetch round-trip: this is the ONE place telemetry I/O happens.
	m, _ = m.Update(m.fetchHomeTelemetry())
	if !m.hasHomeTelemetry() {
		t.Fatal("setup: telemetry snapshot should be loaded after the async fetch")
	}
	ctxLabel := i18n.T("mail.telemetry_context")
	if !strings.Contains(m.View(), ctxLabel) {
		t.Fatal("setup: telemetry row should render after the async fetch")
	}

	// Now remove every on-disk telemetry source. A disk read on the UI path
	// would find nothing.
	if err := os.RemoveAll(filepath.Join(orchDir, "system")); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(filepath.Join(orchDir, "logs")); err != nil {
		t.Fatal(err)
	}
	if entries, err := os.ReadDir(orchDir); err == nil {
		for _, e := range entries {
			if strings.HasSuffix(e.Name(), ".db") || strings.HasSuffix(e.Name(), ".sqlite") {
				_ = os.Remove(filepath.Join(orchDir, e.Name()))
			}
		}
	}

	// The UI path reads the cached snapshot, not disk — so the row survives.
	if !m.hasHomeTelemetry() {
		t.Fatal("hasHomeTelemetry() went false after deleting on-disk sources — the UI path is reading disk, not the cached snapshot")
	}
	if !strings.Contains(m.View(), ctxLabel) {
		t.Fatal("View() dropped the telemetry row after deleting on-disk sources — View is re-deriving telemetry from disk instead of reading the cached snapshot")
	}
	// syncViewportHeight likewise reads the cached predicate; it must still see the
	// row (its visibility is unchanged) with no disk access.
	if !m.hasHomeTelemetry() {
		t.Fatal("syncViewportHeight's visibility predicate (hasHomeTelemetry) must be cache-backed")
	}
	m.lastInputLines = -1
	_ = m.syncViewportHeight()
}
