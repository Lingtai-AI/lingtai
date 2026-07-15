package fs

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"time"
)

const canonicalMoltWindowChunkSize int64 = 64 * 1024

// canonicalMoltSessionWindows resolves the latest two valid psyche_molt
// boundaries directly from the canonical events.jsonl. It scans fixed-size
// chunks from the tail and stops as soon as both boundaries are known, so normal
// telemetry refreshes remain process-free and usually read only the relevant
// session tail. Histories with fewer than two boundaries are scanned to byte zero.
// A successful read is authoritative even when the file contains no molt rows.
// ok=false is reserved for a missing, non-regular, unreadable, or concurrently
// changed canonical file so callers may use the derived SQLite sidecar as a
// compatibility fallback.
func canonicalMoltSessionWindows(eventsPath string) (currentSince, lastSince, lastBefore time.Time, ok bool) {
	pathInfo, err := os.Stat(eventsPath)
	if err != nil || !pathInfo.Mode().IsRegular() {
		return time.Time{}, time.Time{}, time.Time{}, false
	}

	f, err := os.Open(eventsPath)
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false
	}
	defer f.Close()

	startInfo, err := f.Stat()
	if err != nil || !startInfo.Mode().IsRegular() || !os.SameFile(pathInfo, startInfo) {
		return time.Time{}, time.Time{}, time.Time{}, false
	}

	current, last, err := reverseScanCanonicalMoltWindow(f, startInfo.Size())
	if err != nil {
		return time.Time{}, time.Time{}, time.Time{}, false
	}

	// Reject a mixed snapshot if the opened file changed while it was scanned, or
	// if the canonical path was replaced before the result could be published.
	endInfo, err := f.Stat()
	if err != nil || !sameCanonicalMoltWindowSnapshot(startInfo, endInfo) {
		return time.Time{}, time.Time{}, time.Time{}, false
	}
	pathInfo, err = os.Stat(eventsPath)
	if err != nil || !pathInfo.Mode().IsRegular() || !sameCanonicalMoltWindowSnapshot(endInfo, pathInfo) {
		return time.Time{}, time.Time{}, time.Time{}, false
	}

	if current.IsZero() {
		return time.Time{}, time.Time{}, time.Time{}, true
	}
	return current, last, current, true
}

func sameCanonicalMoltWindowSnapshot(a, b os.FileInfo) bool {
	return os.SameFile(a, b) && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}

// reverseScanCanonicalMoltWindow reads [0,size) newest-to-oldest. Pending parts
// retain only the one JSONL record crossing a chunk boundary, so memory is bounded
// by a fixed chunk plus the longest encountered record. Complete malformed rows
// are skipped. A valid unterminated EOF row is accepted; an invalid partial row is
// skipped for this call and naturally retried after a later append completes it.
func reverseScanCanonicalMoltWindow(f io.ReaderAt, size int64) (current, last time.Time, err error) {
	if size < 0 {
		return time.Time{}, time.Time{}, io.ErrUnexpectedEOF
	}

	var pending [][]byte
	pendingLen := 0
	end := size
	for end > 0 {
		start := end - canonicalMoltWindowChunkSize
		if start < 0 {
			start = 0
		}
		chunk := make([]byte, int(end-start))
		n, readErr := f.ReadAt(chunk, start)
		if n != len(chunk) {
			if readErr == nil {
				readErr = io.ErrUnexpectedEOF
			}
			return time.Time{}, time.Time{}, readErr
		}
		if readErr != nil && readErr != io.EOF {
			return time.Time{}, time.Time{}, readErr
		}

		cursor := len(chunk)
		for cursor > 0 {
			newline := bytes.LastIndexByte(chunk[:cursor], '\n')
			if newline < 0 {
				part := append([]byte(nil), chunk[:cursor]...)
				pending = append(pending, part)
				pendingLen += len(part)
				break
			}

			line := assembleReverseJSONLLine(chunk[newline+1:cursor], pending, pendingLen)
			pending = nil
			pendingLen = 0
			if boundary, isMolt := canonicalMoltBoundary(line); isMolt {
				if current.IsZero() {
					current = boundary
				} else {
					return current, boundary, nil
				}
			}
			cursor = newline
		}
		end = start
	}

	if len(pending) > 0 {
		line := assembleReverseJSONLLine(nil, pending, pendingLen)
		if boundary, isMolt := canonicalMoltBoundary(line); isMolt {
			if current.IsZero() {
				current = boundary
			} else {
				last = boundary
			}
		}
	}
	return current, last, nil
}

// assembleReverseJSONLLine joins a prefix from the current chunk to parts found
// in later chunks. Pending parts are stored in reverse discovery order.
func assembleReverseJSONLLine(prefix []byte, pending [][]byte, pendingLen int) []byte {
	if len(pending) == 0 {
		return prefix
	}
	line := make([]byte, len(prefix)+pendingLen)
	offset := copy(line, prefix)
	for i := len(pending) - 1; i >= 0; i-- {
		offset += copy(line[offset:], pending[i])
	}
	return line
}

func canonicalMoltBoundary(line []byte) (time.Time, bool) {
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return time.Time{}, false
	}
	var evt struct {
		Type string  `json:"type"`
		TS   float64 `json:"ts"`
	}
	if err := json.Unmarshal(line, &evt); err != nil || evt.Type != "psyche_molt" || evt.TS <= 0 {
		return time.Time{}, false
	}
	return unixFloatTime(evt.TS), true
}
