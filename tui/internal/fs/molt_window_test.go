package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanonicalMoltSessionWindowsTracksOnlyAppendedEvents(t *testing.T) {
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	write := func(flags int, body string) {
		t.Helper()
		f, err := os.OpenFile(eventsPath, flags, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.WriteString(body); err != nil {
			f.Close()
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	}

	write(os.O_CREATE|os.O_WRONLY|os.O_TRUNC,
		`{"type":"psyche_molt","ts":1000}`+"\n"+
			`{"type":"tool_call","ts":1001}`+"\n"+
			`{"type":"psyche_molt","ts":2000}`+"\n")

	current, last, before, ok := canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 2000, 1000)

	// Ordinary tool traffic grows events.jsonl every telemetry tick. It must not
	// move the session boundary or force a full-history rescan.
	write(os.O_WRONLY|os.O_APPEND, `{"type":"tool_call","ts":2001}`+"\n")
	current, last, before, ok = canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 2000, 1000)

	write(os.O_WRONLY|os.O_APPEND, `{"type":"psyche_molt","ts":3000}`+"\n")
	current, last, before, ok = canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 3000, 2000)
}

func TestCanonicalMoltSessionWindowsRetriesPartialTailAndRecoversTruncation(t *testing.T) {
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(eventsPath, []byte(`{"type":"psyche_molt","ts":1000}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	current, last, before, ok := canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 1000, 0)

	f, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"psyche_molt","ts":2000`); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	current, last, before, ok = canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 1000, 0)

	f, err = os.OpenFile(eventsPath, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("}\n"); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	current, last, before, ok = canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 2000, 1000)

	// Log rotation/truncation discards old boundaries instead of retaining stale
	// cache state from the previous file horizon.
	if err := os.WriteFile(eventsPath, []byte(`{"type":"psyche_molt","ts":4000}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, last, before, ok = canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 4000, 0)
}

func TestCanonicalMoltSessionWindowsRecoversSameInodeTruncateAndRegrow(t *testing.T) {
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	initial := `{"type":"psyche_molt","ts":1000}` + "\n" +
		`{"type":"tool_call","ts":1001,"text":"` + strings.Repeat("x", 512) + `"}` + "\n" +
		`{"type":"psyche_molt","ts":2000}` + "\n"
	if err := os.WriteFile(eventsPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(eventsPath)
	if err != nil {
		t.Fatal(err)
	}

	current, last, before, ok := canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 2000, 1000)

	// A writer can truncate and regrow the same inode past the cached horizon
	// between telemetry polls. The cache must recognize that the previous prefix
	// changed rather than starting in the middle of the replacement history.
	replacement := `{"type":"psyche_molt","ts":4000}` + "\n" +
		`{"type":"psyche_molt","ts":5000}` + "\n" +
		`{"type":"tool_call","ts":5001,"text":"` + strings.Repeat("y", len(initial)*2) + `"}` + "\n"
	f, err := os.OpenFile(eventsPath, os.O_WRONLY|os.O_TRUNC, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(replacement); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	afterInfo, err := os.Stat(eventsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(beforeInfo, afterInfo) {
		t.Fatal("fixture replaced the inode; want same-inode truncate and regrow")
	}
	if afterInfo.Size() <= beforeInfo.Size() {
		t.Fatalf("replacement size = %d, want larger than cached size %d", afterInfo.Size(), beforeInfo.Size())
	}

	current, last, before, ok = canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 5000, 4000)
}

func TestCanonicalMoltSessionWindowsAvailability(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing.jsonl")
	if _, _, _, ok := canonicalMoltSessionWindows(missing); ok {
		t.Fatal("missing canonical event log reported available")
	}

	empty := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	current, last, before, ok := canonicalMoltSessionWindows(empty)
	if !ok || !current.IsZero() || !last.IsZero() || !before.IsZero() {
		t.Fatalf("empty canonical log = (%v, %v, %v, ok=%v), want zero window with ok=true", current, last, before, ok)
	}
}

func assertMoltWindow(t *testing.T, current, last, before time.Time, ok bool, wantCurrent, wantLast int64) {
	t.Helper()
	if !ok {
		t.Fatal("canonical molt window unavailable")
	}
	if got := unixOrZero(current); got != wantCurrent {
		t.Fatalf("current = %d, want %d", got, wantCurrent)
	}
	if got := unixOrZero(last); got != wantLast {
		t.Fatalf("last = %d, want %d", got, wantLast)
	}
	if wantCurrent == 0 {
		if !before.IsZero() {
			t.Fatalf("lastBefore = %v, want zero without a molt boundary", before)
		}
	} else if !before.Equal(current) {
		t.Fatalf("lastBefore = %v, want current %v (the session before the first molt starts at the beginning)", before, current)
	}
}

func unixOrZero(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}
