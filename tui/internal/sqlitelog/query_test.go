package sqlitelog

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// makeTestDB creates a minimal sqlite3 database under a temp agent dir
// using the system sqlite3 binary and the given extra SQL statements.
// It skips the test if sqlite3 is unavailable.
func makeTestDB(t *testing.T, extraSQL ...string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires sqlite3 binary (POSIX only)")
	}
	bin, err := findSQLite3()
	if err != nil {
		t.Skip("sqlite3 not available:", err)
	}

	agentDir := filepath.Join(t.TempDir(), "agent")
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(logsDir, "log.sqlite")

	sql := `CREATE TABLE events (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		ts REAL NOT NULL,
		type TEXT NOT NULL,
		agent_address TEXT,
		fields_json TEXT NOT NULL DEFAULT '{}',
		source_file TEXT,
		source_offset INTEGER,
		source_line INTEGER,
		source_kind TEXT,
		scope TEXT,
		run_id TEXT,
		inserted_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
	);`
	for _, s := range extraSQL {
		sql += "\n" + s
	}

	out, err := exec.Command(bin, db, sql).CombinedOutput()
	if err != nil {
		t.Fatalf("makeTestDB: %v\n%s", err, out)
	}
	return agentDir
}

func TestQueryNotificationsEmpty(t *testing.T) {
	agentDir := makeTestDB(t)
	events, err := QueryNotifications(agentDir, 0)
	if err != nil {
		t.Fatalf("QueryNotifications: %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestQueryNotificationsReturnsMatchingRows(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'email_notification_published','{"count":1}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'notification_pair_injected','{"sources":["email"],"summary":"hello"}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.0,'tool_call','{"name":"read"}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1003.0,'large_tool_result_notification_published','{"size":1024}');`,
	)
	events, err := QueryNotifications(agentDir, 0)
	if err != nil {
		t.Fatalf("QueryNotifications: %v", err)
	}
	// 3 rows match %notification% (id 1, 2, 4); id 3 is tool_call.
	if len(events) != 3 {
		t.Fatalf("expected 3 notification events, got %d", len(events))
	}
	// ORDER BY id DESC → newest first.
	if events[0].Type != "large_tool_result_notification_published" {
		t.Fatalf("expected newest first, got %s", events[0].Type)
	}
	if events[2].Type != "email_notification_published" {
		t.Fatalf("expected oldest last, got %s", events[2].Type)
	}
}

func TestQueryNotificationByID(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'notification_pair_injected','{"sources":["email"]}');`,
	)
	all, err := QueryNotifications(agentDir, 0)
	if err != nil || len(all) == 0 {
		t.Fatalf("setup: %v, events=%d", err, len(all))
	}
	id := all[0].ID
	ev, err := QueryNotificationByID(agentDir, id)
	if err != nil {
		t.Fatalf("QueryNotificationByID: %v", err)
	}
	if ev == nil {
		t.Fatal("expected event, got nil")
	}
	if ev.ID != id {
		t.Fatalf("id mismatch: got %d, want %d", ev.ID, id)
	}
	if !strings.Contains(ev.FieldsJSON, "email") {
		t.Fatalf("unexpected fields_json: %s", ev.FieldsJSON)
	}
}

func TestQueryNotificationBeforeAfter(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'email_notification_published','{"count":1}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'notification_pair_injected','{"sources":["email"]}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.0,'large_tool_result_notification_published','{"size":1024}');`,
	)
	all, err := QueryNotifications(agentDir, 0)
	if err != nil || len(all) != 3 {
		t.Fatalf("setup: %v, events=%d", err, len(all))
	}
	// ORDER BY id DESC: all[0]=newest(id=3), all[1]=middle(id=2), all[2]=oldest(id=1)
	newest, middle, oldest := all[0], all[1], all[2]

	prev, err := QueryNotificationBefore(agentDir, newest.ID)
	if err != nil {
		t.Fatalf("QueryNotificationBefore: %v", err)
	}
	if prev == nil || prev.ID != middle.ID {
		t.Fatalf("before newest: got %v, want id=%d", prev, middle.ID)
	}

	next, err := QueryNotificationAfter(agentDir, middle.ID)
	if err != nil {
		t.Fatalf("QueryNotificationAfter: %v", err)
	}
	if next == nil || next.ID != newest.ID {
		t.Fatalf("after middle: got %v, want id=%d", next, newest.ID)
	}

	prevOfOldest, err := QueryNotificationBefore(agentDir, oldest.ID)
	if err != nil {
		t.Fatalf("QueryNotificationBefore oldest: %v", err)
	}
	if prevOfOldest != nil {
		t.Fatalf("expected nil before oldest, got id=%d", prevOfOldest.ID)
	}

	nextOfNewest, err := QueryNotificationAfter(agentDir, newest.ID)
	if err != nil {
		t.Fatalf("QueryNotificationAfter newest: %v", err)
	}
	if nextOfNewest != nil {
		t.Fatalf("expected nil after newest, got id=%d", nextOfNewest.ID)
	}
}

func TestQueryNotificationsLimit(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'notification_pair_injected','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'notification_pair_injected','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.0,'notification_pair_injected','{}');`,
	)
	events, err := QueryNotifications(agentDir, 2)
	if err != nil {
		t.Fatalf("QueryNotifications with limit: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events with limit=2, got %d", len(events))
	}
}

func TestParseRows(t *testing.T) {
	raw := "1\x1f1000.5\x1fnotification_pair_injected\x1f{\"sources\":[\"email\"]}\x1fevents.jsonl\n" +
		"2\x1f1001.0\x1femail_notification_published\x1f{\"count\":1}\x1f"
	events, err := parseRows(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2, got %d", len(events))
	}
	if events[0].ID != 1 || events[0].Ts != 1000.5 {
		t.Fatalf("row0 mismatch: %+v", events[0])
	}
	if events[0].Source != "events.jsonl" {
		t.Fatalf("source mismatch: %q", events[0].Source)
	}
	if events[0].Type != "notification_pair_injected" {
		t.Fatalf("type mismatch: %q", events[0].Type)
	}
}

func TestParseRowsEmpty(t *testing.T) {
	events, err := parseRows("")
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("expected 0 events, got %d", len(events))
	}
}

func TestExistsReturnsFalseWhenMissing(t *testing.T) {
	if Exists(t.TempDir()) {
		t.Fatal("Exists should return false for dir without log.sqlite")
	}
}

func TestPrettyFields(t *testing.T) {
	ev := NotificationEvent{FieldsJSON: `{"sources":["email"],"summary":"hello"}`}
	pretty := PrettyFields(ev)
	if !strings.Contains(pretty, "\n") {
		t.Fatalf("expected indented JSON, got: %s", pretty)
	}
	if !strings.Contains(pretty, "email") {
		t.Fatalf("expected email in output: %s", pretty)
	}
}

func TestPrettyFieldsInvalidJSON(t *testing.T) {
	ev := NotificationEvent{FieldsJSON: "not-json"}
	pretty := PrettyFields(ev)
	if pretty != "not-json" {
		t.Fatalf("expected passthrough for invalid JSON, got: %s", pretty)
	}
}

func TestNotificationEventTime(t *testing.T) {
	ev := NotificationEvent{Ts: 1781577055.46409}
	tt := ev.Time()
	if tt.Year() != 2026 {
		t.Fatalf("unexpected year: %d", tt.Year())
	}
}

func TestMissingDB(t *testing.T) {
	_, err := QueryNotifications(t.TempDir(), 0)
	if err == nil {
		t.Fatal("expected error for missing sqlite sidecar")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

// ── NotificationBlock tests ───────────────────────────────────────────────────

func TestQueryNotificationBlocksEmpty(t *testing.T) {
	agentDir := makeTestDB(t)
	blocks, err := QueryNotificationBlocks(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryNotificationBlocks: %v", err)
	}
	if len(blocks) != 0 {
		t.Fatalf("expected 0 blocks, got %d", len(blocks))
	}
}

func TestQueryNotificationBlocksFiltersType(t *testing.T) {
	// Only notification_pair_injected rows should be returned.
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'email_notification_published','{"count":1}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'notification_pair_injected','{"sources":["email"],"summary":"hello world"}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.0,'tool_call','{"name":"read"}');`,
	)
	blocks, err := QueryNotificationBlocks(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryNotificationBlocks: %v", err)
	}
	if len(blocks) != 1 {
		t.Fatalf("expected 1 block (notification_pair_injected only), got %d", len(blocks))
	}
	if blocks[0].Summary != "hello world" {
		t.Fatalf("Summary = %q, want %q", blocks[0].Summary, "hello world")
	}
	if len(blocks[0].Sources) != 1 || blocks[0].Sources[0] != "email" {
		t.Fatalf("Sources = %v, want [email]", blocks[0].Sources)
	}
}

func TestQueryNotificationBlocksLatest10(t *testing.T) {
	// Insert 12 notification_pair_injected rows; we should get 10 newest.
	sqls := make([]string, 12)
	for i := 0; i < 12; i++ {
		sqls[i] = fmt.Sprintf(
			`INSERT INTO events(ts,type,fields_json) VALUES(%d.0,'notification_pair_injected','{"summary":"msg%d"}');`,
			1000+i, i,
		)
	}
	agentDir := makeTestDB(t, sqls...)
	blocks, err := QueryNotificationBlocks(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryNotificationBlocks: %v", err)
	}
	if len(blocks) != 10 {
		t.Fatalf("expected 10 blocks with default limit, got %d", len(blocks))
	}
	// newest first → summary msg11 at index 0
	if blocks[0].Summary != "msg11" {
		t.Fatalf("expected newest first (msg11), got %q", blocks[0].Summary)
	}
}

func TestParseNotificationBlockFieldsMeta(t *testing.T) {
	fieldsJSON := `{
		"call_id": "abc123",
		"summary": "You have 1 new email.",
		"sources": ["email", "soul"],
		"meta": {
			"current_time": "2026-06-20T10:00:00-07:00",
			"stamina_left_seconds": 35884.5,
			"injection_seq": 3,
			"context": {
				"system_tokens": 38398,
				"history_tokens": 109121,
				"usage": 0.147519
			}
		}
	}`
	b := NotificationBlock{}
	parseNotificationBlockFields(fieldsJSON, &b)

	if b.CallID != "abc123" {
		t.Errorf("CallID = %q, want abc123", b.CallID)
	}
	if b.Summary != "You have 1 new email." {
		t.Errorf("Summary = %q", b.Summary)
	}
	if len(b.Sources) != 2 || b.Sources[0] != "email" {
		t.Errorf("Sources = %v", b.Sources)
	}
	if b.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if b.Meta.InjectionSeq != 3 {
		t.Errorf("InjectionSeq = %d, want 3", b.Meta.InjectionSeq)
	}
	if b.Meta.StaminaLeftSeconds != 35884.5 {
		t.Errorf("StaminaLeftSeconds = %v", b.Meta.StaminaLeftSeconds)
	}
	if b.Meta.ContextSystemTokens != 38398 {
		t.Errorf("ContextSystemTokens = %d", b.Meta.ContextSystemTokens)
	}
	if b.Meta.ContextUsage != 0.147519 {
		t.Errorf("ContextUsage = %v", b.Meta.ContextUsage)
	}
}

func TestParseNotificationBlockFieldsInvalidJSON(t *testing.T) {
	b := NotificationBlock{ID: 42}
	parseNotificationBlockFields("not-json", &b)
	// Should not panic; identity fields unaffected
	if b.ID != 42 {
		t.Errorf("ID changed unexpectedly")
	}
	if b.Summary != "" || b.CallID != "" {
		t.Errorf("unexpected fields set on parse failure")
	}
}

func TestQueryNotificationBlocksMissingDB(t *testing.T) {
	_, err := QueryNotificationBlocks(t.TempDir(), 10)
	if err == nil {
		t.Fatal("expected error for missing sqlite sidecar")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotificationBlockTime(t *testing.T) {
	b := NotificationBlock{Ts: 1781577055.46409}
	tt := b.Time()
	if tt.Year() != 2026 {
		t.Fatalf("unexpected year: %d", tt.Year())
	}
}

// ── NotificationBlockSnapshot tests ──────────────────────────────────────────

func TestQueryNotificationBlockSnapshotsEmpty(t *testing.T) {
	agentDir := makeTestDB(t)
	snaps, err := QueryNotificationBlockSnapshots(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryNotificationBlockSnapshots: %v", err)
	}
	if len(snaps) != 0 {
		t.Fatalf("expected 0 snapshots, got %d", len(snaps))
	}
}

func TestQueryNotificationBlockSnapshotsFiltersType(t *testing.T) {
	// Only notification_block_injected rows should be returned.
	fieldsJSON := `{"mode":"synthetic_notification_pair","sources":["email","system"],"payload":{"_notification_guidance":"kernel guidance","notifications":{"email":{"data":{"count":1}},"system":{"events":[]}}},"meta":{}}`
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'notification_pair_injected','{"sources":["email"]}');`,
		fmt.Sprintf(`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'notification_block_injected','%s');`, fieldsJSON),
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.0,'tool_call','{"name":"read"}');`,
	)
	snaps, err := QueryNotificationBlockSnapshots(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryNotificationBlockSnapshots: %v", err)
	}
	if len(snaps) != 1 {
		t.Fatalf("expected 1 snapshot (notification_block_injected only), got %d", len(snaps))
	}
	snap := snaps[0]
	if snap.NotificationGuidance != "kernel guidance" {
		t.Errorf("Guidance = %q, want 'kernel guidance'", snap.NotificationGuidance)
	}
	if len(snap.Sources) != 2 {
		t.Errorf("Sources = %v, want [email system]", snap.Sources)
	}
	if snap.Notifications == nil {
		t.Fatal("Notifications is nil")
	}
	if _, ok := snap.Notifications["email"]; !ok {
		t.Errorf("expected email channel in Notifications, got %v", snap.Notifications)
	}
	if _, ok := snap.Notifications["system"]; !ok {
		t.Errorf("expected system channel in Notifications, got %v", snap.Notifications)
	}
}

func TestQueryNotificationBlockSnapshotsLatest10(t *testing.T) {
	sqls := make([]string, 12)
	for i := 0; i < 12; i++ {
		fj := fmt.Sprintf(
			`{"mode":"synthetic_notification_pair","sources":["email"],"payload":{"_notification_guidance":"guidance%d","notifications":{"email":{}}},"meta":{}}`,
			i,
		)
		sqls[i] = fmt.Sprintf(
			`INSERT INTO events(ts,type,fields_json) VALUES(%d.0,'notification_block_injected','%s');`,
			1000+i, fj,
		)
	}
	agentDir := makeTestDB(t, sqls...)
	snaps, err := QueryNotificationBlockSnapshots(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryNotificationBlockSnapshots: %v", err)
	}
	if len(snaps) != 10 {
		t.Fatalf("expected 10 snapshots with limit=10, got %d", len(snaps))
	}
	// newest first → guidance11 at index 0
	if snaps[0].NotificationGuidance != "guidance11" {
		t.Fatalf("expected newest first (guidance11), got %q", snaps[0].NotificationGuidance)
	}
}

func TestParseNotificationBlockSnapshotFields(t *testing.T) {
	fieldsJSON := `{
		"mode": "synthetic_notification_pair",
		"call_id": "notif_abc",
		"sources": ["email", "system"],
		"payload": {
			"notification_guidance": "kernel-level guidance",
			"notifications": {
				"email": {"data": {"count": 3}, "notification_guidance": "email guidance"},
				"system": {"events": [{"body": "test"}]}
			}
		},
		"meta": {
			"current_time": "2026-06-20T10:00:00-07:00",
			"stamina_left_seconds": 3600.0,
			"injection_seq": 5,
			"context": {
				"system_tokens": 1000,
				"history_tokens": 5000,
				"usage": 0.06
			}
		}
	}`
	s := NotificationBlockSnapshot{}
	parseNotificationBlockSnapshotFields(fieldsJSON, &s)

	if s.Mode != "synthetic_notification_pair" {
		t.Errorf("Mode = %q, want synthetic_notification_pair", s.Mode)
	}
	if s.CallID != "notif_abc" {
		t.Errorf("CallID = %q, want notif_abc", s.CallID)
	}
	if len(s.Sources) != 2 || s.Sources[0] != "email" {
		t.Errorf("Sources = %v", s.Sources)
	}
	if s.NotificationGuidance != "kernel-level guidance" {
		t.Errorf("NotificationGuidance = %q", s.NotificationGuidance)
	}
	if s.Notifications == nil {
		t.Fatal("Notifications is nil")
	}
	if _, ok := s.Notifications["email"]; !ok {
		t.Errorf("email missing from Notifications: %v", s.Notifications)
	}
	if _, ok := s.Notifications["system"]; !ok {
		t.Errorf("system missing from Notifications: %v", s.Notifications)
	}
	if s.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if s.RawMeta == nil {
		t.Fatal("RawMeta is nil")
	}
	if s.AgentMeta == nil || s.AgentMeta["current_time"] != "2026-06-20T10:00:00-07:00" {
		t.Errorf("AgentMeta fallback = %v", s.AgentMeta)
	}
	if got := s.RawMeta["current_time"]; got != "2026-06-20T10:00:00-07:00" {
		t.Errorf("RawMeta current_time = %v", got)
	}
	if ctx, ok := s.RawMeta["context"].(map[string]interface{}); !ok || ctx["history_tokens"] != float64(5000) {
		t.Errorf("RawMeta context = %v", s.RawMeta["context"])
	}
	if s.Meta.InjectionSeq != 5 {
		t.Errorf("InjectionSeq = %d, want 5", s.Meta.InjectionSeq)
	}
	if s.Meta.StaminaLeftSeconds != 3600.0 {
		t.Errorf("StaminaLeftSeconds = %v", s.Meta.StaminaLeftSeconds)
	}
	if s.Meta.ContextSystemTokens != 1000 {
		t.Errorf("ContextSystemTokens = %d", s.Meta.ContextSystemTokens)
	}
}

// TestParseNotificationBlockSnapshotFieldsMetaEnvelope covers the modern
// top-level `_meta` envelope shape (tool_meta/agent_meta/guidance/
// notifications/notification_guidance).
func TestParseNotificationBlockSnapshotFieldsMetaEnvelope(t *testing.T) {
	fieldsJSON := `{
		"mode": "active_tool_result",
		"call_id": "call_abc",
		"sources": ["email", "system"],
		"_meta": {
			"tool_meta": {"id": "call_abc", "char_count": 1200, "elapsed_ms": 42, "synthetic": false},
			"agent_meta": {
				"current_time": "2026-06-21T10:00:00-07:00",
				"stamina_left_seconds": 3600.0,
				"context": {"system_tokens": 1000, "history_tokens": 5000, "usage": 0.06}
			},
			"guidance": {"guidance_version": "1", "meta_readme": {"tool_meta": "per-result"}},
			"notification_guidance": "kernel-level guidance",
			"notifications": {
				"email": {"data": {"count": 3}, "notification_guidance": "email guidance"},
				"system": {"events": [{"body": "test"}]}
			}
		}
	}`
	s := NotificationBlockSnapshot{}
	parseNotificationBlockSnapshotFields(fieldsJSON, &s)

	if s.Mode != "active_tool_result" {
		t.Errorf("Mode = %q", s.Mode)
	}
	if s.ToolMeta == nil || s.ToolMeta["id"] != "call_abc" {
		t.Errorf("ToolMeta = %v", s.ToolMeta)
	}
	if s.AgentMeta == nil || s.AgentMeta["current_time"] != "2026-06-21T10:00:00-07:00" {
		t.Errorf("AgentMeta = %v", s.AgentMeta)
	}
	if s.Guidance == nil || s.Guidance["guidance_version"] != "1" {
		t.Errorf("Guidance = %v", s.Guidance)
	}
	if s.NotificationGuidance != "kernel-level guidance" {
		t.Errorf("NotificationGuidance = %q", s.NotificationGuidance)
	}
	if _, ok := s.Notifications["email"]; !ok {
		t.Errorf("email missing from Notifications: %v", s.Notifications)
	}
	if _, ok := s.Notifications["system"]; !ok {
		t.Errorf("system missing from Notifications: %v", s.Notifications)
	}
	// Vital-signs Meta derived from _meta.agent_meta.
	if s.Meta == nil {
		t.Fatal("Meta is nil")
	}
	if s.Meta.StaminaLeftSeconds != 3600.0 {
		t.Errorf("StaminaLeftSeconds = %v", s.Meta.StaminaLeftSeconds)
	}
	if s.Meta.ContextSystemTokens != 1000 {
		t.Errorf("ContextSystemTokens = %d", s.Meta.ContextSystemTokens)
	}
	// RawMeta points at the agent_meta block for the modern shape.
	if s.RawMeta == nil || s.RawMeta["current_time"] != "2026-06-21T10:00:00-07:00" {
		t.Errorf("RawMeta = %v", s.RawMeta)
	}
}

func TestParseNotificationBlockSnapshotFieldsInvalidJSON(t *testing.T) {
	s := NotificationBlockSnapshot{ID: 99}
	parseNotificationBlockSnapshotFields("not-json", &s)
	// Should not panic; identity fields unaffected
	if s.ID != 99 {
		t.Errorf("ID changed unexpectedly")
	}
	if s.NotificationGuidance != "" || s.Mode != "" {
		t.Errorf("unexpected fields set on parse failure")
	}
}

func TestQueryNotificationBlockSnapshotsMissingDB(t *testing.T) {
	_, err := QueryNotificationBlockSnapshots(t.TempDir(), 10)
	if err == nil {
		t.Fatal("expected error for missing sqlite sidecar")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNotificationBlockSnapshotTime(t *testing.T) {
	s := NotificationBlockSnapshot{Ts: 1781577055.46409}
	tt := s.Time()
	if tt.Year() != 2026 {
		t.Fatalf("unexpected year: %d", tt.Year())
	}
}

func TestQueryMoltSessionWindowsLatestTwo(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.5,'psyche_molt','{}');`,
	)
	current, lastSince, lastBefore, ok, err := QueryMoltSessionWindows(agentDir)
	if err != nil {
		t.Fatalf("QueryMoltSessionWindows: %v", err)
	}
	if !ok {
		t.Fatal("expected sqlite query to be ok")
	}
	if got, want := current.Unix(), int64(1002); got != want {
		t.Fatalf("current unix = %d, want %d", got, want)
	}
	if got, want := lastSince.Unix(), int64(1000); got != want {
		t.Fatalf("lastSince unix = %d, want %d", got, want)
	}
	if !lastBefore.Equal(current) {
		t.Fatalf("lastBefore = %v, want current %v", lastBefore, current)
	}
}

func TestQueryMoltSessionWindowsEmpty(t *testing.T) {
	agentDir := makeTestDB(t)
	current, lastSince, lastBefore, ok, err := QueryMoltSessionWindows(agentDir)
	if err != nil {
		t.Fatalf("QueryMoltSessionWindows empty: %v", err)
	}
	if !ok {
		t.Fatal("expected sqlite query to be ok")
	}
	if !current.IsZero() || !lastSince.IsZero() || !lastBefore.IsZero() {
		t.Fatalf("expected zero windows, got current=%v lastSince=%v lastBefore=%v", current, lastSince, lastBefore)
	}
}

func TestStreamSessionEventsFiltersRelevantRows(t *testing.T) {
	agentDir := makeTestDB(t, `
		INSERT INTO events (ts, type, fields_json) VALUES
			(1.0, 'heartbeat', '{}'),
			(2.0, 'text_input', '{"message":"hello"}'),
			(3.0, 'notification_pair_injected', '{"message":"note"}'),
			(4.0, 'aed_attempt', '{"error":"boom"}');
	`)
	var rows []SessionEventRow
	err := StreamSessionEvents(agentDir, func(row SessionEventRow) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		t.Fatalf("StreamSessionEvents error: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("expected 3 relevant rows, got %d", len(rows))
	}
	if rows[0].Type != "text_input" || rows[1].Type != "notification_pair_injected" || rows[2].Type != "aed_attempt" {
		t.Fatalf("unexpected row order/types: %#v", rows)
	}
}

func TestQueryErrorEventsNewestFirst(t *testing.T) {
	agentDir := makeTestDB(t, `
		INSERT INTO events (ts, type, fields_json) VALUES
			(1.0, 'aed_attempt', '{"error":"old"}'),
			(2.0, 'aed_attempt', '{"error":""}'),
			(3.0, 'refresh_init_error', '{"error":"new"}'),
			(4.0, 'text_output', '{"error":"ignored"}');
	`)
	rows, err := QueryErrorEvents(agentDir)
	if err != nil {
		t.Fatalf("QueryErrorEvents error: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 non-empty error rows, got %d", len(rows))
	}
	if rows[0].Error != "new" || rows[1].Error != "old" {
		t.Fatalf("unexpected errors: %#v", rows)
	}
}

func TestHasTUIClearCompletionEventUsesOffsetAndSource(t *testing.T) {
	agentDir := makeTestDB(t, `
		INSERT INTO events (ts, type, fields_json, source_offset) VALUES
			(1.0, 'psyche_molt', '{"source":"human"}', 10),
			(2.0, 'text_output', '{"source":"tui"}', 20),
			(3.0, 'clear_received', '{"source":"tui"}', 30);
	`)
	ok, err := HasTUIClearCompletionEvent(agentDir, 25)
	if err != nil {
		t.Fatalf("HasTUIClearCompletionEvent error: %v", err)
	}
	if !ok {
		t.Fatalf("expected TUI clear completion after offset")
	}
	ok, err = HasTUIClearCompletionEvent(agentDir, 35)
	if err != nil {
		t.Fatalf("HasTUIClearCompletionEvent error: %v", err)
	}
	if ok {
		t.Fatalf("did not expect completion before offset")
	}
}

func TestEventsIndexCoversJSONLRequiresFullCoverage(t *testing.T) {
	agentDir := makeTestDB(t, `
		INSERT INTO events (ts, type, fields_json, source_offset) VALUES
			(1.0, 'text_input', '{"message":"early"}', 0),
			(2.0, 'text_output', '{"message":"late"}', 20);
	`)
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(strings.Repeat("x", 64)), 0o644); err != nil {
		t.Fatal(err)
	}
	coverage, err := QueryEventsIndexCoverage(agentDir)
	if err != nil {
		t.Fatalf("QueryEventsIndexCoverage error: %v", err)
	}
	if !coverage.StartsAtBeginning() || !coverage.TailNearEOF() {
		t.Fatalf("expected complete index coverage: %#v", coverage)
	}

	agentDir = makeTestDB(t, `
		INSERT INTO events (ts, type, fields_json, source_offset) VALUES
			(1.0, 'text_input', '{"message":"late only"}', 8192);
	`)
	logsDir = filepath.Join(agentDir, "logs")
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(strings.Repeat("x", 16384)), 0o644); err != nil {
		t.Fatal(err)
	}
	coverage, err = QueryEventsIndexCoverage(agentDir)
	if err != nil {
		t.Fatalf("QueryEventsIndexCoverage error: %v", err)
	}
	if coverage.StartsAtBeginning() && coverage.TailNearEOF() {
		t.Fatalf("did not expect partial tail-only coverage: %#v", coverage)
	}
}

func TestEventsIndexCoversJSONLDetectsSmallFileTailGap(t *testing.T) {
	agentDir := makeTestDB(t, `
		INSERT INTO events (ts, type, fields_json, source_offset) VALUES
			(1.0, 'text_input', '{"message":"early only"}', 0);
	`)
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(strings.Repeat("x", 1024*1024)), 0o644); err != nil {
		t.Fatal(err)
	}
	coverage, err := QueryEventsIndexCoverage(agentDir)
	if err != nil {
		t.Fatalf("QueryEventsIndexCoverage error: %v", err)
	}
	if coverage.StartsAtBeginning() && coverage.TailNearEOF() {
		t.Fatalf("did not expect coverage when a small file has a large unindexed tail: %#v", coverage)
	}
}
