package migrate

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/migrationguard"
)

// sqliteBackfillCandidate is a stopped agent whose historical JSONL event log
// has not yet been explicitly backfilled into logs/log.sqlite.
type sqliteBackfillCandidate struct {
	AgentDir    string
	Name        string
	EventsPath  string
	EventsBytes int64
	Reason      string
}

// migrateSQLiteLogBackfill is a one-time, optional command-line migration for
// the kernel's derived SQLite event-log sidecar. JSONL remains the source of
// truth, so declining or failing this migration must not prevent normal use.
// Returning nil intentionally stamps the project migration version even when the
// user declines, stdin is non-interactive, or the existing Python runtime cannot
// inspect/rebuild: this is an offer-at-most-once startup prompt, not a recurring
// health check. The manual rebuild hint below keeps the skipped path recoverable.
func migrateSQLiteLogBackfill(lingtaiDir string) error {
	if !sqliteHasAgentEvents(lingtaiDir) {
		return nil
	}
	globalDir := globalTUIDir()
	if globalDir == "" {
		return nil
	}
	if config.NeedsVenv(globalDir) {
		fmt.Println("SQLite log backfill migration: Python runtime is not ready yet; skipping optional historical backfill for this one-time migration.")
		sqlitePrintManualBackfillHint()
		return nil
	}
	python := config.LingtaiCmd(globalDir)
	if !sqliteRuntimeSupportsLogCLI(python) {
		fmt.Println("SQLite log backfill migration: installed Python runtime does not expose `lingtai log`; skipping optional historical backfill.")
		sqlitePrintManualBackfillHint()
		return nil
	}

	candidates, skippedRunning, err := sqliteDiscoverBackfillCandidates(python, lingtaiDir)
	if err != nil {
		fmt.Printf("warning: failed to inspect SQLite log backfill state: %v\n", err)
		return nil
	}
	if len(candidates) == 0 {
		if skippedRunning > 0 {
			fmt.Printf("SQLite log backfill: %d running agent(s) skipped; stop them and run `lingtai-agent log rebuild <agent_dir>` if you want historical SQLite queries.\n", skippedRunning)
		}
		return nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Printf("SQLite log backfill available for %d stopped agent(s), but stdin is not interactive; continuing without backfill.\n", len(candidates))
		sqlitePrintManualBackfillHint()
		return nil
	}

	sqlitePrintBackfillPrompt(os.Stdout, candidates, skippedRunning)
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	answer := strings.TrimSpace(strings.ToLower(line))
	if answer != "y" && answer != "yes" {
		fmt.Println("Skipping SQLite backfill. LingTai will start normally; new events still write to SQLite automatically.")
		fmt.Println("You can backfill later with: lingtai-agent log rebuild <agent_dir>")
		return nil
	}

	fmt.Println()
	fmt.Println("Starting SQLite log backfill migration...")
	for i, c := range candidates {
		fmt.Printf("\n[%d/%d] %s\n", i+1, len(candidates), c.Name)
		if d := migrationguard.CheckAgentDir(c.AgentDir); !d.Permitted() {
			fmt.Fprintf(os.Stderr, "warning: skipping SQLite backfill for %s: %s\n", c.Name, d.Reason)
			continue
		}
		if err := sqliteRunRebuildWithProgress(python, c, os.Stdout, os.Stderr); err != nil {
			fmt.Fprintf(os.Stderr, "\nwarning: SQLite backfill failed for %s: %v\n", c.Name, err)
			fmt.Fprintln(os.Stderr, "LingTai will continue to start normally; JSONL logs remain intact.")
		}
	}
	fmt.Println("\nSQLite backfill migration complete. Continuing startup...")
	return nil
}

func sqliteRuntimeSupportsLogCLI(python string) bool {
	return exec.Command(python, "-m", "lingtai", "log", "--help").Run() == nil
}

func sqlitePrintManualBackfillHint() {
	fmt.Println("Skipping is safe and does not affect normal LingTai use: JSONL logs remain the source of truth, and new events still write to SQLite automatically.")
	fmt.Println("You can backfill historical logs later with: lingtai-agent log rebuild <agent_dir>")
}

func sqliteHasAgentEvents(lingtaiDir string) bool {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, entry.Name())
		if _, err := os.Stat(filepath.Join(agentDir, "init.json")); err != nil {
			continue
		}
		info, err := os.Stat(filepath.Join(agentDir, "logs", "events.jsonl"))
		if err != nil || info.Size() == 0 {
			continue
		}
		return true
	}
	return false
}

func sqlitePrintBackfillPrompt(w io.Writer, candidates []sqliteBackfillCandidate, skippedRunning int) {
	totalBytes := int64(0)
	for _, c := range candidates {
		totalBytes += c.EventsBytes
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "SQLite log backfill migration is available for historical agent events.")
	fmt.Fprintf(w, "Found %d stopped agent(s) with %s of events.jsonl history that has not been backfilled.\n", len(candidates), sqliteHumanBytes(totalBytes))
	if skippedRunning > 0 {
		fmt.Fprintf(w, "%d running agent(s) were skipped; backfill requires stopped/offline agents.\n", skippedRunning)
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Warning: this migration can take a long time for large histories. It builds a derived logs/log.sqlite query index from existing logs/events.jsonl files.")
	fmt.Fprintln(w, "If you confirm, LingTai will show a clear progress bar for each agent while it backfills.")
	fmt.Fprintln(w, "Skipping is safe and does not affect normal LingTai use: JSONL remains the source of truth, and new events will still be written to SQLite automatically.")
	fmt.Fprintln(w, "If you skip now, old events simply will not be available through SQLite queries until you backfill manually later.")
	fmt.Fprintln(w)
	for _, c := range candidates {
		fmt.Fprintf(w, "  - %s (%s): %s\n", c.Name, sqliteHumanBytes(c.EventsBytes), c.Reason)
	}
	fmt.Fprint(w, "\nBackfill historical logs now? [y/N] ")
}

func sqliteDiscoverBackfillCandidates(python, lingtaiDir string) ([]sqliteBackfillCandidate, int, error) {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, 0, nil
		}
		return nil, 0, err
	}
	var candidates []sqliteBackfillCandidate
	skippedRunning := 0
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, entry.Name())
		if _, err := os.Stat(filepath.Join(agentDir, "init.json")); err != nil {
			continue
		}
		eventsPath := filepath.Join(agentDir, "logs", "events.jsonl")
		info, err := os.Stat(eventsPath)
		if err != nil || info.Size() == 0 {
			continue
		}
		if !migrationguard.CheckAgentDir(agentDir).Permitted() {
			skippedRunning++
			continue
		}
		needs, reason := sqliteNeedsBackfill(python, agentDir, eventsPath)
		if needs {
			candidates = append(candidates, sqliteBackfillCandidate{
				AgentDir:    agentDir,
				Name:        entry.Name(),
				EventsPath:  eventsPath,
				EventsBytes: info.Size(),
				Reason:      reason,
			})
		}
	}
	return candidates, skippedRunning, nil
}

func sqliteNeedsBackfill(python, agentDir, eventsPath string) (bool, string) {
	sqlitePath := filepath.Join(agentDir, "logs", "log.sqlite")
	if _, err := os.Stat(sqlitePath); err != nil {
		return true, "SQLite sidecar is missing"
	}
	absEvents, err := filepath.Abs(eventsPath)
	if err == nil {
		if resolved, evalErr := filepath.EvalSymlinks(absEvents); evalErr == nil {
			absEvents = resolved
		}
	} else {
		absEvents = eventsPath
	}

	// The `lingtai log query` CLI accepts a SQL string (not bound parameters), so
	// quote the local resolved source path using SQLite's single-quote doubling.
	sql := fmt.Sprintf("SELECT byte_offset, line_no FROM import_cursors WHERE source_file = '%s'", strings.ReplaceAll(absEvents, "'", "''"))
	cmd := exec.Command(python, "-m", "lingtai", "log", "query", agentDir, sql)
	out, err := cmd.Output()
	if err != nil {
		return true, "SQLite sidecar could not be inspected"
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		return true, "SQLite sidecar query returned unreadable output"
	}
	if len(rows) == 0 {
		return true, "historical JSONL has not been backfilled yet"
	}
	if sqliteAsInt64(rows[0]["byte_offset"]) <= 0 {
		return true, "backfill cursor is empty"
	}
	return false, ""
}

type sqliteProgressLine struct {
	Kind       string         `json:"kind"`
	ByteOffset int64          `json:"byte_offset,omitempty"`
	TotalBytes int64          `json:"total_bytes,omitempty"`
	LineNo     int64          `json:"line_no,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
}

func sqliteRunRebuildWithProgress(python string, c sqliteBackfillCandidate, stdout, stderr io.Writer) error {
	cmd := exec.Command(python, "-c", sqliteRebuildProgressScript, c.AgentDir)
	pipe, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var errBuf bytes.Buffer
	cmd.Stderr = &errBuf
	if err := cmd.Start(); err != nil {
		return err
	}

	updates := make(chan sqliteProgressLine, 16)
	scanDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(pipe)
		scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for scanner.Scan() {
			var line sqliteProgressLine
			if err := json.Unmarshal(scanner.Bytes(), &line); err == nil && line.Kind != "" {
				updates <- line
			}
		}
		close(updates)
		scanDone <- scanner.Err()
	}()

	start := time.Now()
	current := int64(0)
	total := c.EventsBytes
	lineNo := int64(0)
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()

	var waitErr error
	waiting := true
	for waiting {
		select {
		case update, ok := <-updates:
			if !ok {
				updates = nil
				continue
			}
			if update.TotalBytes > 0 {
				total = update.TotalBytes
			}
			if update.ByteOffset > current {
				current = update.ByteOffset
			}
			if update.LineNo > 0 {
				lineNo = update.LineNo
			}
			sqliteRenderProgress(stdout, current, total, lineNo, time.Since(start))
		case <-ticker.C:
			sqliteRenderProgress(stdout, current, total, lineNo, time.Since(start))
		case waitErr = <-waitDone:
			waiting = false
		}
	}
	if scanErr := <-scanDone; scanErr != nil && waitErr == nil {
		waitErr = scanErr
	}
	if waitErr != nil {
		msg := strings.TrimSpace(errBuf.String())
		if msg != "" {
			return fmt.Errorf("%w: %s", waitErr, msg)
		}
		return waitErr
	}
	sqliteRenderProgress(stdout, total, total, lineNo, time.Since(start))
	fmt.Fprintln(stdout)
	return nil
}

func sqliteRenderProgress(w io.Writer, current, total, lineNo int64, elapsed time.Duration) {
	if total <= 0 {
		total = 1
	}
	if current > total {
		current = total
	}
	width := 32
	filled := int((current * int64(width)) / total)
	if filled > width {
		filled = width
	}
	bar := strings.Repeat("=", filled) + strings.Repeat("-", width-filled)
	pct := float64(current) * 100 / float64(total)
	fmt.Fprintf(w, "\r  [%s] %5.1f%%  %s/%s  lines:%d  elapsed:%s", bar, pct, sqliteHumanBytes(current), sqliteHumanBytes(total), lineNo, elapsed.Truncate(time.Second))
}

func sqliteHumanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for n >= div*unit && exp < 4 {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func sqliteAsInt64(v any) int64 {
	switch x := v.(type) {
	case float64:
		return int64(x)
	case int64:
		return x
	case int:
		return int64(x)
	case json.Number:
		n, _ := x.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	default:
		return 0
	}
}

const sqliteRebuildProgressScript = `
import json
import sys
from pathlib import Path

# Cross-version compatibility: the kernel namespace moved from
# lingtai_kernel to lingtai.kernel in the pre-1.0 layout redesign.
# Prefer the new path, but fall back to the old one so the same TUI
# binary can backfill projects using either runtime version.
# Fallback only when the new umbrella namespace itself is absent; a
# broken new runtime (missing internal child) must surface loudly.
try:
    from lingtai.kernel.services import logging as logmod
except ModuleNotFoundError as exc:
    if exc.name not in ("lingtai", "lingtai.kernel"):
        raise
    from lingtai_kernel.services import logging as logmod

agent_dir = Path(sys.argv[1]).resolve()
source = agent_dir / "logs" / "events.jsonl"
total = source.stat().st_size if source.exists() else 0
orig_iter = logmod._iter_jsonl_events_with_offsets
threshold = max(total // 200, 1024 * 1024)
last_emit = 0

def emit(byte_offset=0, line_no=0, kind="progress", result=None):
    payload = {"kind": kind, "byte_offset": int(byte_offset), "total_bytes": int(total), "line_no": int(line_no)}
    if result is not None:
        payload["result"] = result
    print(json.dumps(payload, ensure_ascii=False), flush=True)

def iter_with_progress(path):
    global last_emit
    last_line = 0
    for event, offset, next_offset, line_no in orig_iter(path):
        last_line = line_no
        if next_offset - last_emit >= threshold or next_offset >= total:
            emit(next_offset, line_no)
            last_emit = next_offset
        yield event, offset, next_offset, line_no
    if total and last_emit < total:
        emit(total, last_line)

logmod._iter_jsonl_events_with_offsets = iter_with_progress
emit(0, 0)
result = logmod.rebuild_sqlite_event_index(agent_dir)
emit(total, result.get("line_no", 0), kind="result", result=result)
`
