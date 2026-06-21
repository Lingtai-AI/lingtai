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

// NotificationBlockMeta mirrors the kernel's build_meta vital signs stored in
// the meta key of notification_pair_injected and notification_block_injected events.
type NotificationBlockMeta struct {
	CurrentTime        string  `json:"current_time,omitempty"`
	StaminaLeftSeconds float64 `json:"stamina_left_seconds,omitempty"`
	InjectionSeq       int     `json:"injection_seq,omitempty"`
	// Context sub-fields (may be absent in older events)
	ContextSystemTokens  int     `json:"context_system_tokens,omitempty"`
	ContextHistoryTokens int     `json:"context_history_tokens,omitempty"`
	ContextUsage         float64 `json:"context_usage,omitempty"`
}

// NotificationSummaryEntry is a parsed notification_pair_injected event row.
// It exposes the compact summary/sources/meta logged by the kernel's
// _inject_notification_pair path. Use NotificationBlockSnapshot for the full
// canonical payload from notification_block_injected events.
type NotificationSummaryEntry struct {
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

// Time returns the wall-clock time for the entry.
func (b NotificationSummaryEntry) Time() time.Time {
	sec := int64(b.Ts)
	nsec := int64((b.Ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).Local()
}

// NotificationBlock is a legacy alias for NotificationSummaryEntry retained
// for callers that have not yet migrated to the renamed type.
//
// Deprecated: use NotificationSummaryEntry or NotificationBlockSnapshot.
type NotificationBlock = NotificationSummaryEntry

// summaryEntryFields holds the raw fields_json structure for notification_pair_injected.
type summaryEntryFields struct {
	CallID  string                 `json:"call_id"`
	Summary string                 `json:"summary"`
	Sources []string               `json:"sources"`
	Meta    map[string]interface{} `json:"meta"`
}

// parseNotificationBlockFields parses a fields_json string into a
// NotificationSummaryEntry, populating CallID, Summary, Sources, and Meta.
// Returns a zero-value entry (no fields set) on parse failure.
func parseNotificationBlockFields(fieldsJSON string, b *NotificationSummaryEntry) {
	var f summaryEntryFields
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
// row's fields_json into a NotificationSummaryEntry. Existing QueryNotifications
// is unchanged and still returns raw NotificationEvent rows.
func QueryNotificationBlocks(agentDir string, limit int) ([]NotificationSummaryEntry, error) {
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
	blocks := make([]NotificationSummaryEntry, 0, len(lines))
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
		b := NotificationSummaryEntry{
			ID:     id,
			Ts:     ts,
			Source: sourceBase(parts[3]),
		}
		parseNotificationBlockFields(parts[2], &b)
		blocks = append(blocks, b)
	}
	return blocks, nil
}

// NotificationBlockSnapshot is a parsed notification_block_injected event row.
// It carries the actual canonical block that was injected into the model's
// context.
//
// Modern kernel events (post notification-meta-envelope) persist a single
// top-level `_meta` envelope with four blocks: `tool_meta`, `agent_meta`,
// `guidance`, plus `notifications`/`notification_guidance`. Those populate
// ToolMeta/AgentMeta/Guidance/Notifications/NotificationGuidance below.
//
// Legacy events are still parsed for back-compat: older rows carried a
// top-level `payload` (with `_tool`, `_runtime.state`/`_runtime.guidance`,
// `notification_guidance`/`_notification_guidance`, `notifications`) and a
// separate `meta` dict. Those map onto the same display fields so the
// /notification view renders old and new rows uniformly.
type NotificationBlockSnapshot struct {
	// Raw event identity
	ID     int64
	Ts     float64
	Source string // source_file basename, empty when absent

	// Parsed from fields_json
	Mode    string   // "synthetic_notification_pair" or "active_tool_result"
	CallID  string   // call_id when available
	Sources []string // sorted channel names from notifications.keys()
	Meta    *NotificationBlockMeta
	RawMeta map[string]interface{} // full _meta.agent_meta (modern) or meta (legacy) dict

	// Canonical `_meta` envelope blocks as the agent saw them.
	ToolMeta             map[string]interface{} // _meta.tool_meta  (legacy: payload._tool)
	AgentMeta            map[string]interface{} // _meta.agent_meta (legacy: payload._runtime.state)
	Guidance             map[string]interface{} // _meta.guidance   (legacy: payload._runtime.guidance)
	NotificationGuidance string                 // _meta.notification_guidance (legacy: payload.notification_guidance)
	Notifications        map[string]string      // channel → JSON-encoded channel dict
}

// Time returns the wall-clock time for the snapshot.
func (s NotificationBlockSnapshot) Time() time.Time {
	sec := int64(s.Ts)
	nsec := int64((s.Ts - float64(sec)) * 1e9)
	return time.Unix(sec, nsec).Local()
}

// snapshotFields holds the raw fields_json structure for notification_block_injected.
// `_meta` is the modern envelope; `payload`/`meta` are the legacy shape.
type snapshotFields struct {
	Mode    string                 `json:"mode"`
	CallID  string                 `json:"call_id"`
	Sources []string               `json:"sources"`
	MetaEnv map[string]interface{} `json:"_meta"`
	Payload map[string]interface{} `json:"payload"`
	Meta    map[string]interface{} `json:"meta"`
}

func notificationMap(v interface{}) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	return m, ok
}

// encodeNotificationChannels JSON-encodes each channel payload to a pretty
// string for display, matching the previous per-channel rendering.
func encodeNotificationChannels(notifs map[string]interface{}) map[string]string {
	out := make(map[string]string, len(notifs))
	for ch, v := range notifs {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			out[ch] = fmt.Sprintf("%v", v)
		} else {
			out[ch] = string(b)
		}
	}
	return out
}

// parseNotificationBlockSnapshotFields parses a fields_json string into a
// NotificationBlockSnapshot. Returns a zero-value snapshot on parse failure.
//
// The modern `_meta` envelope is parsed first; when absent, the legacy
// `payload`/`meta` shape is parsed as a fallback.
func parseNotificationBlockSnapshotFields(fieldsJSON string, s *NotificationBlockSnapshot) {
	var f snapshotFields
	if err := json.Unmarshal([]byte(fieldsJSON), &f); err != nil {
		return
	}
	s.Mode = f.Mode
	s.CallID = f.CallID
	s.Sources = f.Sources

	if f.MetaEnv != nil {
		parseMetaEnvelope(f.MetaEnv, s)
	} else if f.Payload != nil {
		parseLegacyPayload(f.Payload, f.Meta, s)
	}

	// Vital-signs Meta: modern rows carry the build_meta snapshot under
	// _meta.agent_meta; legacy rows carry it under the separate `meta` dict.
	rawMeta := s.RawMeta
	if rawMeta == nil && f.Meta != nil {
		rawMeta = f.Meta
		s.RawMeta = f.Meta
	}
	if rawMeta != nil {
		// Actual legacy kernel rows persisted build_meta only as the separate
		// top-level `meta` dict, not under payload._runtime.state. Surface it as
		// the agent_meta display block when no richer legacy runtime state exists.
		if s.AgentMeta == nil {
			s.AgentMeta = rawMeta
		}
		s.Meta = parseBlockMeta(rawMeta)
	}
}

// parseMetaEnvelope reads the modern top-level `_meta` envelope.
func parseMetaEnvelope(env map[string]interface{}, s *NotificationBlockSnapshot) {
	if tool, ok := notificationMap(env["tool_meta"]); ok {
		s.ToolMeta = tool
	}
	if agent, ok := notificationMap(env["agent_meta"]); ok {
		s.AgentMeta = agent
		// agent_meta carries the build_meta vital signs (current_time,
		// context, stamina) used by the Meta footer.
		s.RawMeta = agent
	}
	if guidance, ok := notificationMap(env["guidance"]); ok {
		s.Guidance = guidance
	}
	if g, ok := env["notification_guidance"].(string); ok {
		s.NotificationGuidance = g
	}
	if notifs, ok := notificationMap(env["notifications"]); ok {
		s.Notifications = encodeNotificationChannels(notifs)
	}
}

// parseLegacyPayload reads the pre-envelope `payload`/`meta` shape so older
// rows still render. payload._tool → ToolMeta, payload._runtime.state →
// AgentMeta, payload._runtime.guidance → Guidance,
// payload.notification_guidance → NotificationGuidance.
func parseLegacyPayload(payload, meta map[string]interface{}, s *NotificationBlockSnapshot) {
	if tool, ok := notificationMap(payload["_tool"]); ok {
		s.ToolMeta = tool
	}
	if state, ok := notificationMap(payload["_runtime.state"]); ok {
		s.AgentMeta = state
	}
	if guidance, ok := notificationMap(payload["_runtime.guidance"]); ok {
		s.Guidance = guidance
	}
	if runtimeBlock, ok := notificationMap(payload["_runtime"]); ok {
		if state, ok := notificationMap(runtimeBlock["state"]); ok {
			s.AgentMeta = state
		}
		if guidance, ok := notificationMap(runtimeBlock["guidance"]); ok {
			s.Guidance = guidance
		}
	}
	if g, ok := payload["notification_guidance"].(string); ok {
		s.NotificationGuidance = g
	} else if g, ok := payload["_notification_guidance"].(string); ok {
		s.NotificationGuidance = g
	}
	if notifs, ok := notificationMap(payload["notifications"]); ok {
		s.Notifications = encodeNotificationChannels(notifs)
	}
	if meta != nil {
		s.RawMeta = meta
	}
}

// parseBlockMeta extracts the build_meta vital signs from a meta-bearing dict
// (modern: _meta.agent_meta; legacy: fields_json.meta).
func parseBlockMeta(m map[string]interface{}) *NotificationBlockMeta {
	bm := &NotificationBlockMeta{}
	if v, ok := m["current_time"].(string); ok {
		bm.CurrentTime = v
	}
	if v, ok := m["stamina_left_seconds"].(float64); ok {
		bm.StaminaLeftSeconds = v
	}
	if v, ok := m["injection_seq"].(float64); ok {
		bm.InjectionSeq = int(v)
	}
	if ctx, ok := m["context"].(map[string]interface{}); ok {
		if v, ok := ctx["system_tokens"].(float64); ok {
			bm.ContextSystemTokens = int(v)
		}
		if v, ok := ctx["history_tokens"].(float64); ok {
			bm.ContextHistoryTokens = int(v)
		}
		if v, ok := ctx["usage"].(float64); ok {
			bm.ContextUsage = v
		}
	}
	return bm
}

// QueryNotificationBlockSnapshots fetches the latest notification_block_injected
// events (up to limit, default 10) ordered newest-first and parses each row's
// fields_json into a NotificationBlockSnapshot carrying the actual canonical
// payload the agent saw. Returns nil when no rows exist (not an error).
func QueryNotificationBlockSnapshots(agentDir string, limit int) ([]NotificationBlockSnapshot, error) {
	if limit <= 0 {
		limit = 10
	}
	sql := fmt.Sprintf(
		`SELECT id, ts, fields_json, COALESCE(source_file,'') FROM events WHERE type = 'notification_block_injected' ORDER BY id DESC LIMIT %d`,
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
	snaps := make([]NotificationBlockSnapshot, 0, len(lines))
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
		s := NotificationBlockSnapshot{
			ID:     id,
			Ts:     ts,
			Source: sourceBase(parts[3]),
		}
		parseNotificationBlockSnapshotFields(parts[2], &s)
		snaps = append(snaps, s)
	}
	return snaps, nil
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
