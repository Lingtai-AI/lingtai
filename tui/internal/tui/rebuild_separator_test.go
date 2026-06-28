package tui

import (
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func ts(unix int64) string {
	return time.Unix(unix, 0).UTC().Format(time.RFC3339)
}

// Ledger rows are newest-first. A rebuild between two adjacent calls means a
// separator is drawn after the newer (earlier-in-slice) row.

func TestLedgerSeparatorLabelKeysDistinguishesReconstructAndMolt(t *testing.T) {
	entries := []fs.LedgerEntry{
		{TS: ts(3000), CodexWSDeltaReason: "epoch_reset"},
		{TS: ts(2000)},
		{TS: ts(1000)},
	}
	labels := ledgerSeparatorLabelKeys(entries, []time.Time{time.Unix(1500, 0).UTC()})
	if got := labels[0]; len(got) != 1 || got[0] != ledgerSeparatorReconstructLabel {
		t.Fatalf("expected reconstruct label after index 0, got %v", got)
	}
	if got := labels[1]; len(got) != 1 || got[0] != ledgerSeparatorMoltLabel {
		t.Fatalf("expected molt label after index 1, got %v", got)
	}
}

func TestRenderMainCallRowsInsertsMoltSeparatorLabel(t *testing.T) {
	m := PropsModel{
		detailRecent: []fs.LedgerEntry{
			{TS: ts(3000), Model: "m", Endpoint: "e"},
			{TS: ts(1000), Model: "m", Endpoint: "e"},
		},
		detailRebuilds: []time.Time{time.Unix(2000, 0).UTC()},
	}
	rows := m.renderMainCallRows()
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows (header, row, molt separator, row), got %d:\n%s",
			len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[2], "┈") || !strings.Contains(rows[2], "molt") {
		t.Fatalf("expected dotted molt separator, got %q", rows[2])
	}
}

func TestRebuildSeparatorIndexesUsesCodexEpochResetRow(t *testing.T) {
	entries := []fs.LedgerEntry{
		{TS: ts(3000), CodexWSDeltaReason: "ok"},
		{TS: ts(2000), CodexWSDeltaReason: "epoch_reset"},
		{TS: ts(1000), CodexWSDeltaReason: "ok"},
	}
	got := rebuildSeparatorIndexes(entries, nil)
	if len(got) != 1 || !got[1] {
		t.Fatalf("expected separator after epoch_reset row index 1, got %v", got)
	}
}

func TestRenderMainCallRowsInsertsSeparatorFromEpochReset(t *testing.T) {
	m := PropsModel{
		detailRecent: []fs.LedgerEntry{
			{TS: ts(3000), Model: "m", Endpoint: "e"},
			{TS: ts(2000), Model: "m", Endpoint: "e", CodexWSDeltaReason: "epoch_reset"},
			{TS: ts(1000), Model: "m", Endpoint: "e"},
		},
	}
	rows := m.renderMainCallRows()
	// header + 3 data rows + 1 separator after the epoch_reset row.
	if len(rows) != 5 {
		t.Fatalf("expected 5 rows (header, row, epoch row, separator, row), got %d:\n%s",
			len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[3], "┈") {
		t.Fatalf("expected dotted separator after epoch_reset row, got %q", rows[3])
	}
	if strings.Contains(rows[1], "┈") || strings.Contains(rows[2], "┈") || strings.Contains(rows[4], "┈") {
		t.Fatalf("separator leaked into data rows:\n%s", strings.Join(rows, "\n"))
	}
}

func TestRebuildSeparatorIndexesBetweenCalls(t *testing.T) {
	entries := []fs.LedgerEntry{
		{TS: ts(3000)}, // index 0 (newest)
		{TS: ts(2000)}, // index 1
		{TS: ts(1000)}, // index 2 (oldest)
	}
	// A rebuild at 2500 sits between row 0 (3000) and row 1 (2000).
	rebuilds := []time.Time{time.Unix(2500, 0).UTC()}

	got := rebuildSeparatorIndexes(entries, rebuilds)
	if len(got) != 1 || !got[0] {
		t.Fatalf("expected separator after index 0, got %v", got)
	}
}

func TestRebuildSeparatorIndexesMultiple(t *testing.T) {
	entries := []fs.LedgerEntry{
		{TS: ts(4000)},
		{TS: ts(3000)},
		{TS: ts(2000)},
		{TS: ts(1000)},
	}
	rebuilds := []time.Time{
		time.Unix(3500, 0).UTC(), // between idx 0 and 1
		time.Unix(1500, 0).UTC(), // between idx 2 and 3
	}
	got := rebuildSeparatorIndexes(entries, rebuilds)
	if !got[0] || !got[2] {
		t.Fatalf("expected separators after index 0 and 2, got %v", got)
	}
	if got[1] || got[3] {
		t.Fatalf("unexpected separators at 1 or 3: %v", got)
	}
}

func TestRebuildSeparatorIndexesNoneWhenOutOfRange(t *testing.T) {
	entries := []fs.LedgerEntry{
		{TS: ts(3000)},
		{TS: ts(2000)},
	}
	// Rebuilds older than all rows or newer than all rows produce no
	// in-list separator (best effort: no marker).
	rebuilds := []time.Time{
		time.Unix(500, 0).UTC(),  // older than oldest
		time.Unix(9000, 0).UTC(), // newer than newest
	}
	got := rebuildSeparatorIndexes(entries, rebuilds)
	if len(got) != 0 {
		t.Fatalf("expected no separators, got %v", got)
	}
}

func TestRebuildSeparatorIndexesEmptyInputs(t *testing.T) {
	if got := rebuildSeparatorIndexes(nil, []time.Time{time.Unix(1, 0)}); len(got) != 0 {
		t.Fatalf("expected none for no entries, got %v", got)
	}
	entries := []fs.LedgerEntry{{TS: ts(1000)}}
	if got := rebuildSeparatorIndexes(entries, nil); len(got) != 0 {
		t.Fatalf("expected none for no rebuilds, got %v", got)
	}
}

func TestRenderMainCallRowsInsertsSeparator(t *testing.T) {
	m := PropsModel{
		detailRecent: []fs.LedgerEntry{
			{TS: ts(3000), Model: "m", Endpoint: "e"},
			{TS: ts(1000), Model: "m", Endpoint: "e"},
		},
		detailRebuilds: []time.Time{time.Unix(2000, 0).UTC()},
	}
	rows := m.renderMainCallRows()
	// header + 2 data rows + 1 separator
	if len(rows) != 4 {
		t.Fatalf("expected 4 rows (header, row, separator, row), got %d:\n%s",
			len(rows), strings.Join(rows, "\n"))
	}
	if !strings.Contains(rows[2], "┈") {
		t.Fatalf("expected dotted separator on row index 2, got %q", rows[2])
	}
	if strings.Contains(rows[1], "┈") || strings.Contains(rows[3], "┈") {
		t.Fatalf("separator leaked into data rows:\n%s", strings.Join(rows, "\n"))
	}
}

func TestRenderMainCallRowsNoSeparatorWithoutRebuild(t *testing.T) {
	m := PropsModel{
		detailRecent: []fs.LedgerEntry{
			{TS: ts(3000), Model: "m", Endpoint: "e"},
			{TS: ts(1000), Model: "m", Endpoint: "e"},
		},
		detailRebuilds: nil,
	}
	rows := m.renderMainCallRows()
	if len(rows) != 3 {
		t.Fatalf("expected 3 rows with no separator, got %d", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r, "┈") {
			t.Fatalf("unexpected separator without rebuild marker: %q", r)
		}
	}
}

func TestRebuildSeparatorIndexesSkipsMalformedTS(t *testing.T) {
	entries := []fs.LedgerEntry{
		{TS: "not-a-time"},
		{TS: ts(2000)},
		{TS: ts(1000)},
	}
	rebuilds := []time.Time{time.Unix(1500, 0).UTC()} // between idx 1 and 2
	got := rebuildSeparatorIndexes(entries, rebuilds)
	if len(got) != 1 || !got[1] {
		t.Fatalf("expected separator after index 1 only, got %v", got)
	}
}
