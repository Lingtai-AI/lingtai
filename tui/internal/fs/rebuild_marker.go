package fs

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/lingtai-tui/internal/sqlitelog"
)

// tailReadChunk is the size of each backward read when tailing events.jsonl.
const tailReadChunk = 64 * 1024

// tailScanLines bounds how many trailing lines of events.jsonl the JSONL
// fallback inspects. Full-file scans of events.jsonl can be slow on
// long-lived agents, so the fallback only tails the most recent lines.
const tailScanLines = 1000

// RecentRebuildTimes returns the timestamps of recent context-rebuild
// (psyche_molt) events for an agent, newest first, capped at limit. It is
// best-effort and never errors: the primary source is the agent's
// logs/log.sqlite sidecar via a targeted LIMIT query (no full scan); if that
// is missing or fails, it falls back to tailing the last tailScanLines lines
// of logs/events.jsonl. Missing or malformed logs simply yield no markers,
// so callers can render no separator.
func RecentRebuildTimes(agentDir string, limit int) []time.Time {
	if limit <= 0 {
		limit = 10
	}
	if times, err := sqlitelog.QueryRecentMoltTimes(agentDir, limit); err == nil {
		return times
	}
	return tailMoltTimes(filepath.Join(agentDir, "logs", "events.jsonl"), tailScanLines, limit)
}

// tailMoltTimes inspects only the last maxLines lines of an events.jsonl file
// for psyche_molt rows and returns their timestamps newest-first, capped at
// limit. It reads backward from EOF in fixed chunks until it has gathered
// enough trailing lines (or hit the start of the file), so a huge line in the
// file prefix never affects it and the prefix is never scanned. Malformed
// lines are skipped; a missing file yields nil.
func tailMoltTimes(eventsPath string, maxLines, limit int) []time.Time {
	if maxLines <= 0 {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	f, err := os.Open(eventsPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil
	}
	size := info.Size()

	// Read backward from EOF until we have maxLines newlines (one more than
	// the number of trailing lines we want, to anchor the oldest line's start)
	// or reach the file start. We only keep a bounded suffix buffer.
	var suffix []byte
	pos := size
	newlines := 0
	for pos > 0 && newlines <= maxLines {
		readSize := int64(tailReadChunk)
		if readSize > pos {
			readSize = pos
		}
		pos -= readSize
		buf := make([]byte, readSize)
		if _, err := f.ReadAt(buf, pos); err != nil && err != io.EOF {
			return nil
		}
		suffix = append(buf, suffix...)
		newlines += bytes.Count(buf, []byte{'\n'})
	}

	// Keep only the last maxLines lines of the gathered suffix. Ignore
	// trailing newline terminators so a normally newline-ended file still
	// contributes exactly maxLines data lines instead of maxLines-1.
	suffix = bytes.TrimRight(suffix, "\n")
	if len(suffix) == 0 {
		return nil
	}
	lines := bytes.Split(suffix, []byte{'\n'})
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	var times []time.Time
	// Walk the tail newest-first so the returned slice is newest-first.
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var evt struct {
			Type string  `json:"type"`
			TS   float64 `json:"ts"`
		}
		if err := json.Unmarshal(line, &evt); err != nil {
			continue
		}
		if evt.Type != "psyche_molt" || evt.TS <= 0 {
			continue
		}
		times = append(times, unixFloatTime(evt.TS))
		if limit > 0 && len(times) >= limit {
			break
		}
	}
	return times
}
