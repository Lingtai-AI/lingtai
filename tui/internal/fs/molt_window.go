package fs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// canonicalMoltWindowCache tracks each events.jsonl through its last parsed byte.
// The event log is append-only in normal operation, so a telemetry refresh only
// parses newly appended records instead of rescanning the full history. Entries
// carry their own lock so unrelated agents never serialize behind a large first
// read on a slow volume.
var canonicalMoltWindowCache sync.Map // map[clean events path]*canonicalMoltWindowCacheEntry

type canonicalMoltWindowCacheEntry struct {
	mu sync.Mutex

	info     os.FileInfo
	fileSize int64
	offset   int64

	currentSince time.Time
	lastSince    time.Time
	lastBefore   time.Time
}

// canonicalMoltSessionWindows resolves the latest two valid psyche_molt
// boundaries directly from the canonical events.jsonl. A successful read is
// authoritative even when the file contains no molt rows. ok=false is reserved
// for a missing/unreadable file so callers may use the derived SQLite sidecar as
// a compatibility fallback.
func canonicalMoltSessionWindows(eventsPath string) (currentSince, lastSince, lastBefore time.Time, ok bool) {
	path := filepath.Clean(eventsPath)
	value, _ := canonicalMoltWindowCache.LoadOrStore(path, &canonicalMoltWindowCacheEntry{})
	entry := value.(*canonicalMoltWindowCacheEntry)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return time.Time{}, time.Time{}, time.Time{}, false
	}

	restart := entry.info == nil ||
		!os.SameFile(entry.info, info) ||
		info.Size() < entry.fileSize ||
		(info.Size() == entry.fileSize && !info.ModTime().Equal(entry.info.ModTime()))
	if restart {
		entry.info = nil
		entry.fileSize = 0
		entry.offset = 0
		entry.currentSince = time.Time{}
		entry.lastSince = time.Time{}
		entry.lastBefore = time.Time{}
	}

	// Exact same completed horizon: no file I/O at all. If offset trails the
	// horizon, the prior read ended on an incomplete JSON record and must retry it.
	if entry.info != nil && entry.offset == info.Size() && entry.fileSize == info.Size() && entry.info.ModTime().Equal(info.ModTime()) {
		return entry.currentSince, entry.lastSince, entry.lastBefore, true
	}

	current, last, before := entry.currentSince, entry.lastSince, entry.lastBefore
	next, current, last, before, err := scanCanonicalMoltWindow(path, entry.offset, info.Size(), current, last, before)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false
	}

	entry.info = info
	entry.fileSize = info.Size()
	entry.offset = next
	entry.currentSince = current
	entry.lastSince = last
	entry.lastBefore = before
	return current, last, before, true
}

// scanCanonicalMoltWindow reads exactly [start,end). Complete malformed JSONL
// records are skipped like the other streaming readers. An invalid unterminated
// final record is left unread so the next refresh retries it after the writer
// finishes appending.
func scanCanonicalMoltWindow(path string, start, end int64, currentSince, lastSince, lastBefore time.Time) (next int64, current, last, before time.Time, err error) {
	f, err := os.Open(path)
	if err != nil {
		return start, currentSince, lastSince, lastBefore, err
	}
	defer f.Close()

	if start < 0 || start > end {
		start = 0
		currentSince, lastSince, lastBefore = time.Time{}, time.Time{}, time.Time{}
	}
	r := bufio.NewReaderSize(io.NewSectionReader(f, start, end-start), jsonlReaderBufferSize)
	pos := start
	current, last, before = currentSince, lastSince, lastBefore
	for {
		lineStart := pos
		line, readErr := r.ReadBytes('\n')
		if readErr == nil {
			pos += int64(len(line))
			current, last, before, _ = consumeMoltWindowLine(line, current, last, before)
			continue
		}
		if readErr != io.EOF {
			return start, currentSince, lastSince, lastBefore, readErr
		}
		if len(line) == 0 {
			return pos, current, last, before, nil
		}

		// A valid EOF record is complete even without a final newline. If it is
		// malformed, retain its starting offset and retry after the next append.
		var valid bool
		current, last, before, valid = consumeMoltWindowLine(line, current, last, before)
		if valid {
			pos += int64(len(line))
		} else {
			pos = lineStart
		}
		return pos, current, last, before, nil
	}
}

func consumeMoltWindowLine(line []byte, currentSince, lastSince, lastBefore time.Time) (current, last, before time.Time, valid bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return currentSince, lastSince, lastBefore, true
	}
	var evt struct {
		Type string  `json:"type"`
		TS   float64 `json:"ts"`
	}
	if err := json.Unmarshal(line, &evt); err != nil {
		return currentSince, lastSince, lastBefore, false
	}
	if evt.Type != "psyche_molt" || evt.TS <= 0 {
		return currentSince, lastSince, lastBefore, true
	}
	lastSince = currentSince
	lastBefore = unixFloatTime(evt.TS)
	currentSince = lastBefore
	return currentSince, lastSince, lastBefore, true
}
