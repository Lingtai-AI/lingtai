// Package sqlitelog provides just-in-time queries against an agent's
// logs/log.sqlite sidecar without importing a cgo sqlite driver.
// Queries are executed by shelling out to the system sqlite3 binary.
// All public functions degrade gracefully: if the database or binary is
// absent they return an empty result and a descriptive error.
package sqlitelog

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// NotificationBlockMeta mirrors the kernel's build_meta vital signs
// stored in the meta key of notification_pair_injected events.
type NotificationBlockMeta struct {
	CurrentTime        string  `json:"current_time,omitempty"`
	StaminaLeftSeconds float64 `json:"stamina_left_seconds,omitempty"`
	InjectionSeq       int     `json:"injection_seq,omitempty"`
	// Context sub-fields (may be absent in older events)
	ContextSystemTokens  int     `json:"context_system_tokens,omitempty"`
	ContextHistoryTokens int     `json:"context_history_tokens,omitempty"`
	ContextUsage         float64 `json:"context_usage,omitempty"`
}

// NotificationBlock is a parsed notification_pair_injected event row.
// It exposes the structured fields from fields_json for richer display
// than the raw NotificationEvent. Existing NotificationEvent APIs are
// unchanged; this is an additive layer.
type NotificationBlock struct {
	// Raw event identity
	ID     int64
	Ts     float64
	Source string // source_file basename, empty when absent

	// Parsed from fields_json
	CallID  string   // call_id field if present
	Summary string   // summary field (the body text injected into the LLM)
	Sources []string // sources list (email, soul, system, ...)
	Meta    *NotificationBlockMeta
}

// Time returns the wall-clock time for the block.
func (b NotificationBlock) Time() time.Time {
	sec := int64(b.Ts)
	nsec := int64((b.Ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).Local()
}

// blockFields holds the raw fields_json structure for notification_pair_injected.
type blockFields struct {
	CallID  string                 `json:"call_id"`
	Summary string                 `json:"summary"`
	Sources []string               `json:"sources"`
	Meta    map[string]interface{} `json:"meta"`
}

// parseNotificationBlockFields parses a fields_json string into a
// NotificationBlock, populating CallID, Summary, Sources, and Meta.
// Returns a zero-value block (no fields set) on parse failure.
func parseNotificationBlockFields(fieldsJSON string, b *NotificationBlock) {
	var f blockFields
	if err := json.Unmarshal([]byte(fieldsJSON), &f); err != nil {
		return
	}
	b.CallID = f.CallID
	b.Summary = f.Summary
	b.Sources = f.Sources
	if f.Meta != nil {
		m := &NotificationBlockMeta{}
		if v, ok := f.Meta["current_time"].(string); ok {
			m.CurrentTime = v
		}
		if v, ok := f.Meta["stamina_left_seconds"].(float64); ok {
			m.StaminaLeftSeconds = v
		}
		if v, ok := f.Meta["injection_seq"].(float64); ok {
			m.InjectionSeq = int(v)
		}
		if ctx, ok := f.Meta["context"].(map[string]interface{}); ok {
			if v, ok := ctx["system_tokens"].(float64); ok {
				m.ContextSystemTokens = int(v)
			}
			if v, ok := ctx["history_tokens"].(float64); ok {
				m.ContextHistoryTokens = int(v)
			}
			if v, ok := ctx["usage"].(float64); ok {
				m.ContextUsage = v
			}
		}
		b.Meta = m
	}
}

// QueryNotificationBlocks fetches the latest notification_pair_injected
// events (up to limit, default 10) ordered newest-first and parses each
// row's fields_json into a NotificationBlock. Existing QueryNotifications
// is unchanged and still returns raw NotificationEvent rows.
func QueryNotificationBlocks(agentDir string, limit int) ([]NotificationBlock, error) {
	if limit <= 0 {
		limit = 10
	}
	sql := fmt.Sprintf(
		`SELECT id, ts, fields_json, COALESCE(source_file,'') FROM events WHERE type = 'notification_pair_injected' ORDER BY id DESC LIMIT %d`,
		limit,
	)
	db := DBPath(agentDir)
	if _, err := os.Stat(db); err != nil {
		return nil, fmt.Errorf("sqlite sidecar not found: %s", db)
	}
	bin, err := findSQLite3()
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(bin, "-separator", "\x1f", db, sql).Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		if msg != "" {
			return nil, fmt.Errorf("sqlite3: %s", msg)
		}
		return nil, fmt.Errorf("sqlite3 query failed: %w", err)
	}
	raw := strings.TrimRight(string(out), "\n")
	if raw == "" {
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	blocks := make([]NotificationBlock, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 4)
		if len(parts) != 4 {
			continue
		}
		id, _ := strconv.ParseInt(parts[0], 10, 64)
		ts, _ := strconv.ParseFloat(parts[1], 64)
		b := NotificationBlock{
			ID:     id,
			Ts:     ts,
			Source: sourceBase(parts[3]),
		}
		parseNotificationBlockFields(parts[2], &b)
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// NotificationEvent is a single notification-related row from the events table.
type NotificationEvent struct {
	ID         int64
	Ts         float64
	Type       string
	FieldsJSON string
	Source     string // source_file basename, empty when absent
}

// Time returns the wall-clock time for the event.
func (e NotificationEvent) Time() time.Time {
	sec := int64(e.Ts)
	nsec := int64((e.Ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).Local()
}

// DBPath returns the canonical sqlite sidecar path for agentDir.
func DBPath(agentDir string) string {
	return filepath.Join(agentDir, "logs", "log.sqlite")
}

func sourceBase(path string) string {
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}

// Exists reports whether the sqlite sidecar is present for agentDir.
func Exists(agentDir string) bool {
	_, err := os.Stat(DBPath(agentDir))
	return err == nil
}

// QueryNotifications fetches all notification-related events ordered by id DESC
// (newest first). limit caps how many rows are returned (0 = no limit).
func QueryNotifications(agentDir string, limit int) ([]NotificationEvent, error) {
	sql := `SELECT id, ts, type, fields_json, COALESCE(source_file,'') FROM events WHERE type LIKE '%notification%' ORDER BY id DESC`
	if limit > 0 {
		sql += fmt.Sprintf(" LIMIT %d", limit)
	}
	return runQuery(agentDir, sql)
}

// QueryNotificationByID fetches the single event with the given id.
func QueryNotificationByID(agentDir string, id int64) (*NotificationEvent, error) {
	sql := fmt.Sprintf(`SELECT id, ts, type, fields_json, COALESCE(source_file,'') FROM events WHERE id = %d`, id)
	rows, err := runQuery(agentDir, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	return &r, nil
}

// QueryNotificationBefore fetches the nearest notification event with id < pivot
// (i.e., one step older). Returns nil when there is no older event.
func QueryNotificationBefore(agentDir string, pivot int64) (*NotificationEvent, error) {
	sql := fmt.Sprintf(
		`SELECT id, ts, type, fields_json, COALESCE(source_file,'') FROM events WHERE type LIKE '%%notification%%' AND id < %d ORDER BY id DESC LIMIT 1`,
		pivot,
	)
	rows, err := runQuery(agentDir, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	return &r, nil
}

// QueryNotificationAfter fetches the nearest notification event with id > pivot
// (i.e., one step newer). Returns nil when there is no newer event.
func QueryNotificationAfter(agentDir string, pivot int64) (*NotificationEvent, error) {
	sql := fmt.Sprintf(
		`SELECT id, ts, type, fields_json, COALESCE(source_file,'') FROM events WHERE type LIKE '%%notification%%' AND id > %d ORDER BY id ASC LIMIT 1`,
		pivot,
	)
	rows, err := runQuery(agentDir, sql)
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	r := rows[0]
	return &r, nil
}

// PrettyFields returns the fields_json of ev formatted with indentation.
func PrettyFields(ev NotificationEvent) string {
	var v any
	if err := json.Unmarshal([]byte(ev.FieldsJSON), &v); err != nil {
		return ev.FieldsJSON
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return ev.FieldsJSON
	}
	return string(b)
}

// runQuery executes sql against the agent's sqlite sidecar using the system
// sqlite3 binary. Rows are returned as tab-separated values (4 columns:
// id, ts, type, fields_json, source_file).
func runQuery(agentDir, sql string) ([]NotificationEvent, error) {
	db := DBPath(agentDir)
	if _, err := os.Stat(db); err != nil {
		return nil, fmt.Errorf("sqlite sidecar not found: %s", db)
	}
	bin, err := findSQLite3()
	if err != nil {
		return nil, err
	}
	out, err := exec.Command(bin, "-separator", "\x1f", db, sql).Output()
	if err != nil {
		msg := ""
		if ee, ok := err.(*exec.ExitError); ok {
			msg = strings.TrimSpace(string(ee.Stderr))
		}
		if msg != "" {
			return nil, fmt.Errorf("sqlite3: %s", msg)
		}
		return nil, fmt.Errorf("sqlite3 query failed: %w", err)
	}
	return parseRows(strings.TrimRight(string(out), "\n"))
}

// parseRows parses the unit-separator (0x1f) delimited output produced by
// sqlite3 -separator '\x1f'. Each line is one row with 5 fields:
// id, ts, type, fields_json, source_file.
func parseRows(raw string) ([]NotificationEvent, error) {
	if raw == "" {
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	events := make([]NotificationEvent, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "\x1f", 5)
		if len(parts) != 5 {
			continue
		}
		id, _ := strconv.ParseInt(parts[0], 10, 64)
		ts, _ := strconv.ParseFloat(parts[1], 64)
		events = append(events, NotificationEvent{
			ID:         id,
			Ts:         ts,
			Type:       parts[2],
			FieldsJSON: parts[3],
			Source:     sourceBase(parts[4]),
		})
	}
	return events, nil
}

// findSQLite3 locates the sqlite3 binary. Checks PATH first, then common
// Homebrew and anaconda paths.
func findSQLite3() (string, error) {
	if p, err := exec.LookPath("sqlite3"); err == nil {
		return p, nil
	}
	candidates := []string{
		"/opt/homebrew/bin/sqlite3",
		"/usr/local/bin/sqlite3",
		"/usr/bin/sqlite3",
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("sqlite3 binary not found in PATH or common locations; install sqlite3 to use notification history")
}
