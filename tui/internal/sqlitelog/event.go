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

const sessionEventFilterSQL = "(type IN ('thinking','diary','text_input','text_output','tool_call','tool_result','llm_call','llm_response','insight','consultation_fire','notification_pair_injected','apriori_summary_generated','apriori_summary_cap_refused','apriori_summary_failed','apriori_summary_empty','apriori_summary_no_summarizer') OR type IN ('aed_attempt','aed_exhausted','aed_timeout'))"

// sessionEventFieldsSQL avoids sending tool-result fields that the session parser
// deliberately ignores or hides. On large histories these fields account for tens
// of megabytes of SQLite transport and substantially more Go JSON allocations.
// json_remove preserves every visible result field; formatToolResultEvent still
// emits its standard metadata hint from the synthesized tool identity block.
const sessionEventFieldsSQL = `CASE WHEN type = 'tool_result' THEN json_remove(
	fields_json,
	'$.tool_args',
	'$.kernel_runtime',
	'$.kernel_runtime_stamp',
	'$.kernel_version',
	'$.result._runtime_pending',
	'$.result._runtime',
	'$.result._runtime_guidance',
	'$.result.notifications',
	'$.result._notification_guidance',
	'$.result._tool'
) ELSE fields_json END`

type EventsIndexCoverage struct {
	SourceFile string
	FileSize   int64
	MinOffset  int64
	MaxOffset  int64
	// Count is a row-presence sentinel: 1 means at least one canonical
	// indexed row exists (HasRows()==true); 0 means none exist. It is NOT
	// an exact row count — the endpoint-lookup query avoids a full-table
	// COUNT(*) aggregate that would scan all rows. Callers that need an
	// exact count must issue their own query.
	Count int64
}

func (c EventsIndexCoverage) HasRows() bool {
	return c.SourceFile != "" && c.Count > 0 && c.MinOffset >= 0 && c.MaxOffset >= 0
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

func canonicalEventsSource(agentDir string) (string, error) {
	source, err := filepath.Abs(filepath.Join(agentDir, "logs", "events.jsonl"))
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(source)
	if err != nil {
		return "", err
	}
	return filepath.Clean(resolved), nil
}

func sqliteStringLiteral(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func rootEventsPredicate(sourceFile string) string {
	return "source_file = " + sqliteStringLiteral(sourceFile) + " AND source_kind = 'agent_events' AND scope = 'agent'"
}

// QueryEventsIndexCoverage reports the source_offset range currently represented
// for the canonical root logs/events.jsonl coordinate. Missing identity columns,
// unclassified rows, and path mismatches return no usable coverage so callers can
// fall back to authoritative JSONL.
//
// The query uses indexed endpoint lookups (ORDER BY source_offset ASC/DESC
// LIMIT 1) instead of a full-table MIN/MAX/COUNT aggregate. The extra
// `source_offset IS NOT NULL` predicate allows SQLite to use the partial unique
// index idx_events_source_offset (source_file, source_offset), turning each
// endpoint into an O(1) index seek rather than a full scan of all canonical
// rows. Count is a row-presence sentinel (1 or 0), not an exact row count —
// see the EventsIndexCoverage.Count doc comment.
func QueryEventsIndexCoverage(agentDir string) (EventsIndexCoverage, error) {
	coverage := EventsIndexCoverage{MinOffset: -1, MaxOffset: -1}
	sourceFile, err := canonicalEventsSource(agentDir)
	if err != nil {
		return coverage, err
	}
	info, err := os.Stat(sourceFile)
	if err != nil {
		return coverage, err
	}
	coverage.SourceFile = sourceFile
	coverage.FileSize = info.Size()
	if info.Size() == 0 {
		return coverage, nil
	}
	pred := rootEventsPredicate(sourceFile) + " AND source_offset IS NOT NULL"
	sql := "SELECT " +
		"COALESCE((SELECT source_offset FROM events WHERE " + pred + " ORDER BY source_offset ASC LIMIT 1), -1), " +
		"COALESCE((SELECT source_offset FROM events WHERE " + pred + " ORDER BY source_offset DESC LIMIT 1), -1), " +
		"CASE WHEN EXISTS(SELECT 1 FROM events WHERE " + pred + " LIMIT 1) THEN 1 ELSE 0 END"
	rows, err := querySQLiteRows(agentDir, sql, 3)
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

// StreamSessionEvents streams session-relevant root events from logs/log.sqlite
// in event order, bounded by the MaxOffset captured with coverage. Callers should
// fall back to events.jsonl if this returns an error, because the SQLite index is
// additive and may be absent or lack provable source identity on older runtimes.
func StreamSessionEvents(agentDir string, coverage EventsIndexCoverage, handle func(SessionEventRow) error) error {
	sourceFile, err := canonicalEventsSource(agentDir)
	if err != nil {
		return err
	}
	if !coverage.HasRows() || sourceFile != coverage.SourceFile {
		return fmt.Errorf("sqlite events coverage does not identify canonical root source")
	}
	sql := fmt.Sprintf(
		"SELECT ts, type, %s FROM events WHERE %s AND source_offset <= %d AND %s ORDER BY id ASC",
		sessionEventFieldsSQL, rootEventsPredicate(sourceFile), coverage.MaxOffset, sessionEventFilterSQL,
	)
	return streamSQLiteRows(agentDir, sql, 3, func(cols []string) error {
		ts, _ := strconv.ParseFloat(cols[0], 64)
		return handle(SessionEventRow{TS: ts, Type: cols[1], FieldsJSON: cols[2]})
	})
}

// StreamSessionEventsWindow streams only the newest `limit` session-relevant
// root events (by id) in ASCENDING id order — the same order and projection as
// StreamSessionEvents, but bounded to a window so the first Mail frame does not
// scan the entire history. A non-positive limit means "no window" and behaves
// exactly like StreamSessionEvents. Callers fall back to events.jsonl on error.
//
// The window is the newest rows. The inner subquery selects them with
// `ORDER BY source_offset DESC LIMIT limit` and the outer query re-sorts them
// ascending for stream-order ingestion. Ordering on source_offset (not id) is
// what makes this O(limit): the unique index idx_events_source_offset
// (source_file, source_offset) lets SQLite walk backward from the horizon and
// stop after `limit` matching rows, instead of materializing and sorting every
// row for this source into a temp b-tree (which an id-ordered window forces,
// because row selection is already driven by the source_offset index). For a
// single canonical source file source_offset is monotonic with append/id order,
// so "newest by source_offset" equals "newest by id" and ascending source_offset
// is the same chronological stream order StreamSessionEvents produces with id.
// Every row is bounded by the captured MaxOffset horizon, so a window never
// reaches past the coverage snapshot into rows the JSONL Refresh boundary owns.
func StreamSessionEventsWindow(agentDir string, coverage EventsIndexCoverage, limit int, handle func(SessionEventRow) error) error {
	if limit <= 0 {
		return StreamSessionEvents(agentDir, coverage, handle)
	}
	sourceFile, err := canonicalEventsSource(agentDir)
	if err != nil {
		return err
	}
	if !coverage.HasRows() || sourceFile != coverage.SourceFile {
		return fmt.Errorf("sqlite events coverage does not identify canonical root source")
	}
	sql := fmt.Sprintf(
		"SELECT ts, type, %s FROM (SELECT * FROM events WHERE %s AND source_offset <= %d AND %s ORDER BY source_offset DESC LIMIT %d) ORDER BY source_offset ASC",
		sessionEventFieldsSQL, rootEventsPredicate(sourceFile), coverage.MaxOffset, sessionEventFilterSQL, limit,
	)
	return streamSQLiteRows(agentDir, sql, 3, func(cols []string) error {
		ts, _ := strconv.ParseFloat(cols[0], 64)
		return handle(SessionEventRow{TS: ts, Type: cols[1], FieldsJSON: cols[2]})
	})
}

// legacyGroupBoundaryTypes are the hidden markers buildMessages uses to derive
// api-call grouping for legacy events that carry no explicit api_call_id: an
// llm_response opens a group, an llm_call resets (no active group).
const legacyGroupBoundaryTypes = "('llm_response','llm_call')"

// WindowGroupBoundary describes what a newest-`limit` session window needs in
// order to preserve legacy api-call grouping across its lower cut. WindowLower is
// the smallest source_offset among the windowed rows. BoundaryOffset/BoundaryType
// identify the nearest grouping marker strictly OLDER than the window; HasBoundary
// is false when none precedes the window.
type WindowGroupBoundary struct {
	WindowLower    int64
	HasBoundary    bool
	BoundaryOffset int64
	BoundaryType   string
}

// QueryWindowGroupBoundary reports the lower edge of the newest-`limit` session
// window and the nearest legacy grouping marker (llm_response / llm_call) that
// sits just below it. It is used to decide whether a windowed first frame must
// reach back to an llm_response header so the window's leading legacy tool
// entries derive the same group id the full-history render would give them (a
// preceding llm_call means the group was reset, so no extension is warranted).
// A non-positive limit has no lower cut, so it returns HasBoundary=false.
func QueryWindowGroupBoundary(agentDir string, coverage EventsIndexCoverage, limit int) (WindowGroupBoundary, error) {
	var out WindowGroupBoundary
	if limit <= 0 {
		return out, nil
	}
	sourceFile, err := canonicalEventsSource(agentDir)
	if err != nil {
		return out, err
	}
	if !coverage.HasRows() || sourceFile != coverage.SourceFile {
		return out, fmt.Errorf("sqlite events coverage does not identify canonical root source")
	}
	// Lower edge of the newest-`limit` session window (same selection as
	// StreamSessionEventsWindow, reduced to MIN(source_offset)).
	lowerSQL := fmt.Sprintf(
		"SELECT COALESCE(MIN(source_offset), -1) FROM (SELECT source_offset FROM events WHERE %s AND source_offset <= %d AND %s ORDER BY source_offset DESC LIMIT %d)",
		rootEventsPredicate(sourceFile), coverage.MaxOffset, sessionEventFilterSQL, limit,
	)
	lowerRows, err := querySQLiteRows(agentDir, lowerSQL, 1)
	if err != nil {
		return out, err
	}
	if len(lowerRows) == 0 {
		return out, nil
	}
	out.WindowLower, _ = strconv.ParseInt(lowerRows[0][0], 10, 64)
	if out.WindowLower < 0 {
		return out, nil // empty window
	}
	// Nearest grouping marker strictly below the window's lower edge.
	boundarySQL := fmt.Sprintf(
		"SELECT source_offset, type FROM events WHERE %s AND source_offset < %d AND type IN %s ORDER BY source_offset DESC LIMIT 1",
		rootEventsPredicate(sourceFile), out.WindowLower, legacyGroupBoundaryTypes,
	)
	boundaryRows, err := querySQLiteRows(agentDir, boundarySQL, 2)
	if err != nil {
		return out, err
	}
	if len(boundaryRows) == 0 {
		return out, nil
	}
	out.HasBoundary = true
	out.BoundaryOffset, _ = strconv.ParseInt(boundaryRows[0][0], 10, 64)
	out.BoundaryType = boundaryRows[0][1]
	return out, nil
}

// StreamSessionEventsOffsetRange streams session-relevant root events whose
// source_offset lies in [lower, upper) in ascending source_offset order, using
// the same projection and predicates as StreamSessionEventsWindow. It is used to
// prepend the small back-extension that carries a legacy group's llm_response
// header into a windowed first frame.
func StreamSessionEventsOffsetRange(agentDir string, coverage EventsIndexCoverage, lower, upper int64, handle func(SessionEventRow) error) error {
	sourceFile, err := canonicalEventsSource(agentDir)
	if err != nil {
		return err
	}
	if !coverage.HasRows() || sourceFile != coverage.SourceFile {
		return fmt.Errorf("sqlite events coverage does not identify canonical root source")
	}
	if lower >= upper {
		return nil
	}
	sql := fmt.Sprintf(
		"SELECT ts, type, %s FROM events WHERE %s AND source_offset >= %d AND source_offset < %d AND %s ORDER BY source_offset ASC",
		sessionEventFieldsSQL, rootEventsPredicate(sourceFile), lower, upper, sessionEventFilterSQL,
	)
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
