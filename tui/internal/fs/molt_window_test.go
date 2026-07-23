package fs

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCanonicalMoltSessionWindowsFindsLatestBoundariesAfterAppends(t *testing.T) {
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
	// move the session boundary.
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

	// Log rotation/truncation discards old boundaries and reads the replacement
	// history as authoritative.
	if err := os.WriteFile(eventsPath, []byte(`{"type":"psyche_molt","ts":4000}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, last, before, ok = canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 4000, 0)
}

func TestCanonicalMoltSessionWindowsScansReverseAcrossChunks(t *testing.T) {
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	body := `{"type":"psyche_molt","ts":1000}` + "\n" +
		`{"type":"psyche_molt","ts":2000}` + "\n" +
		`{"type":"tool_call","ts":2001,"text":"` + strings.Repeat("x", int(canonicalMoltWindowChunkSize)+1024) + `"}` + "\n" +
		"not-json\n" +
		`{"type":"psyche_molt","ts":3000}`
	if err := os.WriteFile(eventsPath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// The latest valid boundary has no final newline. Finding the previous one
	// requires crossing a multi-chunk row and skipping a complete malformed row.
	current, last, before, ok := canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 3000, 2000)
}

func TestReverseScanCanonicalMoltWindowStopsAfterTwoTailBoundaries(t *testing.T) {
	prefix := `{"type":"tool_call","ts":1000,"text":"` + strings.Repeat("x", int(3*canonicalMoltWindowChunkSize)) + `"}` + "\n"
	tail := `{"type":"psyche_molt","ts":2000}` + "\n" +
		`{"type":"tool_call","ts":2001}` + "\n" +
		`{"type":"psyche_molt","ts":3000}` + "\n"
	body := []byte(prefix + tail)
	reader := &countingReaderAt{ReaderAt: bytes.NewReader(body)}

	current, last, err := reverseScanCanonicalMoltWindow(reader, int64(len(body)))
	if err != nil {
		t.Fatal(err)
	}
	if got := unixOrZero(current); got != 3000 {
		t.Fatalf("current = %d, want 3000", got)
	}
	if got := unixOrZero(last); got != 2000 {
		t.Fatalf("last = %d, want 2000", got)
	}
	if reader.calls != 1 || reader.bytesRead != int(canonicalMoltWindowChunkSize) {
		t.Fatalf("tail scan reads = %d calls/%d bytes, want 1 call/%d bytes", reader.calls, reader.bytesRead, canonicalMoltWindowChunkSize)
	}
}

func TestCanonicalMoltSessionWindowsRecoversSameInodeTruncateAndRegrow(t *testing.T) {
	eventsPath := filepath.Join(t.TempDir(), "events.jsonl")
	const preservedTailSize = 4096
	stableTail := `{"type":"tool_call","ts":2001,"text":"` + strings.Repeat("x", preservedTailSize+1024) + `"}` + "\n"
	initial := `{"type":"psyche_molt","ts":1000}` + "\n" +
		`{"type":"psyche_molt","ts":2000}` + "\n" +
		stableTail
	if err := os.WriteFile(eventsPath, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	beforeInfo, err := os.Stat(eventsPath)
	if err != nil {
		t.Fatal(err)
	}

	current, last, before, ok := canonicalMoltSessionWindows(eventsPath)
	assertMoltWindow(t, current, last, before, ok, 2000, 1000)

	// A writer can truncate and regrow the same inode past the prior read horizon
	// between telemetry polls while preserving every byte in a bounded tail sample.
	// Only the earlier, equal-length molt rows change; the long tail through the
	// old horizon remains identical and one ordinary event grows the new file.
	replacement := `{"type":"psyche_molt","ts":4000}` + "\n" +
		`{"type":"psyche_molt","ts":5000}` + "\n" +
		stableTail +
		`{"type":"tool_call","ts":5001}` + "\n"
	tailStart := len(initial) - preservedTailSize
	if got, want := replacement[tailStart:len(initial)], initial[tailStart:]; got != want {
		t.Fatal("fixture changed the bounded tail sample; want identical bytes through the old horizon")
	}
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
	if _, _, _, ok := canonicalMoltSessionWindows(t.TempDir()); ok {
		t.Fatal("non-regular canonical event log reported available")
	}

	empty := filepath.Join(t.TempDir(), "events.jsonl")
	if err := os.WriteFile(empty, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	current, last, before, ok := canonicalMoltSessionWindows(empty)
	if !ok || !current.IsZero() || !last.IsZero() || !before.IsZero() {
		t.Fatalf("empty canonical log = (%v, %v, %v, ok=%v), want zero window with ok=true", current, last, before, ok)
	}

	if err := os.WriteFile(empty, []byte(`{"type":"tool_call","ts":1000}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	current, last, before, ok = canonicalMoltSessionWindows(empty)
	if !ok || !current.IsZero() || !last.IsZero() || !before.IsZero() {
		t.Fatalf("no-molt canonical log = (%v, %v, %v, ok=%v), want zero window with ok=true", current, last, before, ok)
	}
}

type countingReaderAt struct {
	io.ReaderAt
	calls     int
	bytesRead int
}

func (r *countingReaderAt) ReadAt(p []byte, off int64) (int, error) {
	r.calls++
	n, err := r.ReaderAt.ReadAt(p, off)
	r.bytesRead += n
	return n, err
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
