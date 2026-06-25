package sqlitelog

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
)

const sqliteRowSeparator = "\x1f"

// SessionEventRow is the subset of the events table needed to rebuild the
// TUI session cache without scanning the full events.jsonl file.
type SessionEventRow struct {
	TS         float64
	Type       string
	FieldsJSON string
}

// ErrorEvent is a recent runtime error event read from the SQLite log index.
type ErrorEvent struct {
	TS    float64
	Type  string
	Error string
}

func streamSQLiteRows(agentDir, sql string, expectedColumns int, handle func([]string) error) error {
	dbPath := DBPath(agentDir)
	if _, err := os.Stat(dbPath); err != nil {
		return err
	}
	sqliteBin, err := findSQLite3()
	if err != nil {
		return err
	}

	cmd := exec.Command(sqliteBin, "-batch", "-noheader", "-separator", sqliteRowSeparator, dbPath, sql)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}

	reader := bufio.NewReader(stdout)
	for {
		line, readErr := reader.ReadString('\n')
		if len(line) > 0 {
			line = strings.TrimRight(line, "\r\n")
			if line != "" {
				cols := strings.SplitN(line, sqliteRowSeparator, expectedColumns)
				if expectedColumns > 0 && len(cols) != expectedColumns {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return fmt.Errorf("sqlite row: expected %d columns, got %d", expectedColumns, len(cols))
				}
				if err := handle(cols); err != nil {
					_ = cmd.Process.Kill()
					_ = cmd.Wait()
					return err
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return readErr
		}
	}
	if err := cmd.Wait(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("sqlite query failed: %w: %s", err, msg)
		}
		return err
	}
	return nil
}

func querySQLiteRows(agentDir, sql string, expectedColumns int) ([][]string, error) {
	var rows [][]string
	err := streamSQLiteRows(agentDir, sql, expectedColumns, func(cols []string) error {
		row := append([]string(nil), cols...)
		rows = append(rows, row)
		return nil
	})
	return rows, err
}

const sessionEventFilterSQL = "(type IN ('thinking','diary','text_input','text_output','tool_call','tool_result','llm_call','llm_response','insight','consultation_fire','notification_pair_injected') OR type IN ('aed_attempt','aed_exhausted','aed_timeout'))"

type EventsIndexCoverage struct {
	FileSize  int64
	MinOffset int64
	MaxOffset int64
	Count     int64
}

func (c EventsIndexCoverage) HasRows() bool {
	return c.Count > 0 && c.MinOffset >= 0 && c.MaxOffset >= 0
}

func (c EventsIndexCoverage) StartsAtBeginning() bool {
	return c.HasRows() && c.MinOffset <= 4096
}

func (c EventsIndexCoverage) TailNearEOF() bool {
	if !c.HasRows() {
		return false
	}
	tailSlack := int64(8 * 1024 * 1024)
	if c.FileSize < tailSlack {
		tailSlack = c.FileSize / 10
		if tailSlack < 64*1024 {
			tailSlack = 64 * 1024
		}
		if tailSlack > c.FileSize {
			tailSlack = c.FileSize
		}
	}
	return c.MaxOffset >= c.FileSize-tailSlack
}

// QueryEventsIndexCoverage reports the source_offset range currently represented
// in the SQLite events index for the agent's events.jsonl file.
func QueryEventsIndexCoverage(agentDir string) (EventsIndexCoverage, error) {
	coverage := EventsIndexCoverage{MinOffset: -1, MaxOffset: -1}
	info, err := os.Stat(filepath.Join(agentDir, "logs", "events.jsonl"))
	if err != nil {
		return coverage, err
	}
	coverage.FileSize = info.Size()
	if info.Size() == 0 {
		return coverage, nil
	}
	rows, err := querySQLiteRows(agentDir, "SELECT COALESCE(MIN(source_offset), -1), COALESCE(MAX(source_offset), -1), COUNT(source_offset) FROM events", 3)
	if err != nil {
		return coverage, err
	}
	if len(rows) == 0 {
		return coverage, nil
	}
	coverage.MinOffset, _ = strconv.ParseInt(rows[0][0], 10, 64)
	coverage.MaxOffset, _ = strconv.ParseInt(rows[0][1], 10, 64)
	coverage.Count, _ = strconv.ParseInt(rows[0][2], 10, 64)
	return coverage, nil
}

// StreamSessionEvents streams session-relevant events from logs/log.sqlite in
// event order. Callers should fall back to events.jsonl if this returns an
// error, because the SQLite index is additive and may be absent on older
// runtimes.
func StreamSessionEvents(agentDir string, handle func(SessionEventRow) error) error {
	sql := "SELECT ts, type, fields_json FROM events WHERE " + sessionEventFilterSQL + " ORDER BY id ASC"
	return streamSQLiteRows(agentDir, sql, 3, func(cols []string) error {
		ts, _ := strconv.ParseFloat(cols[0], 64)
		return handle(SessionEventRow{TS: ts, Type: cols[1], FieldsJSON: cols[2]})
	})
}

// QueryErrorEvents returns newest-first runtime error events used by /doctor.
func QueryErrorEvents(agentDir string) ([]ErrorEvent, error) {
	sql := "SELECT ts, type, fields_json FROM events WHERE type IN ('aed_attempt','aed_exhausted','refresh_init_error') ORDER BY id DESC"
	rows, err := querySQLiteRows(agentDir, sql, 3)
	if err != nil {
		return nil, err
	}
	events := make([]ErrorEvent, 0, len(rows))
	for _, row := range rows {
		var fields map[string]interface{}
		if err := json.Unmarshal([]byte(row[2]), &fields); err != nil {
			continue
		}
		errText, _ := fields["error"].(string)
		if strings.TrimSpace(errText) == "" {
			continue
		}
		ts, _ := strconv.ParseFloat(row[0], 64)
		events = append(events, ErrorEvent{TS: ts, Type: row[1], Error: errText})
	}
	return events, nil
}

// HasTUIClearCompletionEvent checks the SQLite events index for a TUI-originated
// clear completion event after the supplied events.jsonl byte offset. It returns
// false when none is found; callers may still stream-tail events.jsonl as a
// correctness fallback if they cannot trust the additive SQLite index.
func HasTUIClearCompletionEvent(agentDir string, offset int64) (bool, error) {
	offsetClause := ""
	if offset > 0 {
		offsetClause = fmt.Sprintf(" AND source_offset >= %d", offset)
	}
	sql := "SELECT type, fields_json FROM events WHERE type IN ('psyche_molt','clear_received')" + offsetClause + " ORDER BY id DESC"
	rows, err := querySQLiteRows(agentDir, sql, 2)
	if err != nil {
		return false, err
	}
	for _, row := range rows {
		var fields map[string]interface{}
		if err := json.Unmarshal([]byte(row[1]), &fields); err != nil {
			continue
		}
		source, _ := fields["source"].(string)
		if source == "tui" {
			return true, nil
		}
	}
	return false, nil
}
