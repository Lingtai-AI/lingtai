package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helper: write lines to a file, each terminated by \n.
func writeLines(t *testing.T, path string, lines []string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		f.WriteString(l + "\n")
	}
	f.Close()
}

// helper: append raw bytes (no trailing \n) to a file.
func appendRaw(t *testing.T, path string, data string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(data)
	f.Close()
}

// makeEntry creates a raw JSON event line matching the format agents write to
// events.jsonl: ts is a Unix float, text is the content field.
func makeEntry(ts float64, typ, text string) string {
	raw := map[string]interface{}{
		"ts":   ts,
		"type": typ,
		"text": text,
	}
	b, _ := json.Marshal(raw)
	return string(b)
}

func TestTailJSONLBasic(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{
		makeEntry(1781258400, "thinking", "thought 1"),
		makeEntry(1781258460, "thinking", "thought 2"),
		makeEntry(1781258520, "thinking", "thought 3"),
	}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	info, _ := os.Stat(p)
	if off != info.Size() {
		t.Fatalf("offset = %d, want %d (file size)", off, info.Size())
	}
}

func TestTailJSONLPartialLine(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	// Write one complete line, then a partial line (no \n).
	lines := []string{
		makeEntry(1781258400, "thinking", "complete"),
	}
	writeLines(t, p, lines)
	appendRaw(t, p, makeEntry(1781258460, "thinking", "partial"))

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)

	// Should only get the complete line.
	if len(entries) != 1 {
		t.Fatalf("got %d entries, want 1 (partial should be skipped)", len(entries))
	}
	if entries[0].Body != "complete" {
		t.Fatalf("got body %q, want %q", entries[0].Body, "complete")
	}

	// Now complete the partial line.
	appendRaw(t, p, "\n")

	entries2, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries2) != 1 {
		t.Fatalf("got %d entries on retry, want 1", len(entries2))
	}
	if entries2[0].Body != "partial" {
		t.Fatalf("got body %q, want %q", entries2[0].Body, "partial")
	}

	info, _ := os.Stat(p)
	if off2 != info.Size() {
		t.Fatalf("final offset = %d, want %d", off2, info.Size())
	}
}

func TestTailJSONLIncremental(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{
		makeEntry(1781258400, "thinking", "first"),
		makeEntry(1781258460, "thinking", "second"),
	}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	// Append 3 more lines.
	f, _ := os.OpenFile(p, os.O_APPEND|os.O_WRONLY, 0o644)
	for _, body := range []string{"third", "fourth", "fifth"} {
		f.WriteString(makeEntry(1781258520, "thinking", body) + "\n")
	}
	f.Close()

	entries2, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries2) != 3 {
		t.Fatalf("got %d entries on second read, want 3", len(entries2))
	}

	info, _ := os.Stat(p)
	if off2 != info.Size() {
		t.Fatalf("offset = %d, want %d", off2, info.Size())
	}
}

func TestTailJSONLTruncation(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{
		makeEntry(1781258400, "thinking", "before truncation"),
	}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	_, off := sc.tailJSONL(p, 0, parseEvent)

	// Truncate the file (simulates molt).
	os.WriteFile(p, []byte{}, 0o644)

	// Write new content.
	writeLines(t, p, []string{
		makeEntry(1781262000, "thinking", "after truncation"),
	})

	entries, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries) != 1 {
		t.Fatalf("got %d entries after truncation, want 1", len(entries))
	}
	if entries[0].Body != "after truncation" {
		t.Fatalf("got body %q, want %q", entries[0].Body, "after truncation")
	}

	info, _ := os.Stat(p)
	if off2 != info.Size() {
		t.Fatalf("offset = %d, want %d", off2, info.Size())
	}
}

func TestTailJSONLEmptyLines(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	// Write with empty lines interspersed.
	f, _ := os.Create(p)
	f.WriteString(makeEntry(1781258400, "thinking", "one") + "\n")
	f.WriteString("\n")
	f.WriteString(makeEntry(1781258460, "thinking", "two") + "\n")
	f.WriteString("\n")
	f.Close()

	sc := &SessionCache{}
	entries, _ := sc.tailJSONL(p, 0, parseEvent)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2 (empty lines should be skipped)", len(entries))
	}
}

func TestTailJSONLInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	f, _ := os.Create(p)
	f.WriteString(makeEntry(1781258400, "thinking", "valid") + "\n")
	f.WriteString("not json at all\n")
	f.WriteString(makeEntry(1781258460, "thinking", "also valid") + "\n")
	f.Close()

	sc := &SessionCache{}
	entries, off := sc.tailJSONL(p, 0, parseEvent)

	// Should get the 2 valid entries; invalid line is skipped but offset advances past it.
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}

	info, _ := os.Stat(p)
	if off != info.Size() {
		t.Fatalf("offset = %d, want %d (should advance past invalid line)", off, info.Size())
	}
}

func TestTailJSONLNothingNew(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "events.jsonl")

	lines := []string{makeEntry(1781258400, "thinking", "one")}
	writeLines(t, p, lines)

	sc := &SessionCache{}
	_, off := sc.tailJSONL(p, 0, parseEvent)

	// Poll again with nothing new.
	entries, off2 := sc.tailJSONL(p, off, parseEvent)
	if len(entries) != 0 {
		t.Fatalf("got %d entries, want 0", len(entries))
	}
	if off2 != off {
		t.Fatalf("offset changed from %d to %d, should be unchanged", off, off2)
	}
}
