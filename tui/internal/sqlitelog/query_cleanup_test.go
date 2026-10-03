package sqlitelog

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

func TestNotificationMetaParserParity(t *testing.T) {
	cases := []struct {
		name, fields string
		want         *NotificationBlockMeta
	}{
		{name: "missing", fields: `{}`},
		{name: "null", fields: `{"meta":null}`},
		{name: "empty object", fields: `{"meta":{}}`, want: &NotificationBlockMeta{}},
		{name: "wrong types", fields: `{"meta":{"current_time":4,"injection_seq":"3","context":[]}}`, want: &NotificationBlockMeta{}},
		{name: "values", fields: `{"meta":{"current_time":"now","injection_seq":3.9,"context":{"system_tokens":2.9,"history_tokens":4.1,"usage":0.125}}}`, want: &NotificationBlockMeta{CurrentTime: "now", InjectionSeq: 3, ContextSystemTokens: 2, ContextHistoryTokens: 4, ContextUsage: 0.125}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var summary NotificationSummaryEntry
			var snapshot NotificationBlockSnapshot
			parseNotificationBlockFields(tc.fields, &summary)
			parseNotificationBlockSnapshotFields(tc.fields, &snapshot)
			if !reflect.DeepEqual(summary.Meta, tc.want) {
				t.Errorf("summary Meta = %#v, want %#v", summary.Meta, tc.want)
			}
			if !reflect.DeepEqual(snapshot.Meta, tc.want) {
				t.Errorf("snapshot Meta = %#v, want %#v", snapshot.Meta, tc.want)
			}
		})
	}
}

func TestNotificationQueriesShareSQLiteExecutor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("fake sqlite3 uses a POSIX shell")
	}
	routes := []struct {
		name, sql string
		query     func(string) (int, error)
	}{
		{"blocks", `SELECT id, ts, fields_json, COALESCE(source_file,'') FROM events WHERE type = 'notification_pair_injected' ORDER BY id DESC LIMIT 10`, func(dir string) (int, error) { rows, err := QueryNotificationBlocks(dir, 0); return len(rows), err }},
		{"snapshots", `SELECT id, ts, fields_json, COALESCE(source_file,'') FROM events WHERE type = 'notification_block_injected' ORDER BY id DESC LIMIT 10`, func(dir string) (int, error) {
			rows, err := QueryNotificationBlockSnapshots(dir, 0)
			return len(rows), err
		}},
		{"notifications", `SELECT id, ts, type, fields_json, COALESCE(source_file,'') FROM events WHERE type LIKE '%notification%' ORDER BY id DESC`, func(dir string) (int, error) { rows, err := QueryNotifications(dir, 0); return len(rows), err }},
	}
	for _, route := range routes {
		t.Run(route.name, func(t *testing.T) {
			t.Run("missing database", func(t *testing.T) {
				dir := filepath.Join(t.TempDir(), "agent")
				_, err := route.query(dir)
				if want := "sqlite sidecar not found: " + DBPath(dir); err == nil || err.Error() != want {
					t.Fatalf("error = %v, want %q", err, want)
				}
			})
			for _, failure := range []struct{ name, stderr, want string }{
				{"stderr", "fake sqlite failure\n", "sqlite3: fake sqlite failure"},
				{"no stderr", "", "sqlite3 query failed: exit status 7"},
			} {
				t.Run(failure.name, func(t *testing.T) {
					dir := tempSQLiteAgent(t)
					installFakeSQLite(t, failure.stderr, 7)
					_, err := route.query(dir)
					if err == nil || err.Error() != failure.want {
						t.Fatalf("error = %v, want %q", err, failure.want)
					}
				})
			}
			t.Run("empty output argv", func(t *testing.T) {
				dir := tempSQLiteAgent(t)
				argsLog := installFakeSQLite(t, "", 0)
				if rows, err := route.query(dir); err != nil || rows != 0 {
					t.Fatalf("query = (%d rows, %v), want (0, nil)", rows, err)
				}
				got, err := os.ReadFile(argsLog)
				if err != nil {
					t.Fatal(err)
				}
				args := strings.Split(strings.TrimSuffix(string(got), "\n"), "\n")
				want := []string{"-separator", "\x1f", DBPath(dir), route.sql}
				if !reflect.DeepEqual(args, want) {
					t.Errorf("sqlite3 argv = %#v, want %#v", args, want)
				}
			})
		})
	}
}

func tempSQLiteAgent(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "agent")
	if err := os.MkdirAll(filepath.Dir(DBPath(dir)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(DBPath(dir), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

func installFakeSQLite(t *testing.T, stderr string, exit int) string {
	t.Helper()
	binDir := t.TempDir()
	bin := filepath.Join(binDir, "sqlite3")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SQLITE_ARGS_LOG\"\nprintf '%s' \"$SQLITE_STDOUT\"\nprintf '%s' \"$SQLITE_STDERR\" >&2\nexit \"$SQLITE_EXIT\"\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	argsLog := filepath.Join(binDir, "args")
	t.Setenv("SQLITE_ARGS_LOG", argsLog)
	t.Setenv("SQLITE_STDOUT", "")
	t.Setenv("SQLITE_STDERR", stderr)
	t.Setenv("SQLITE_EXIT", strconv.Itoa(exit))
	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return argsLog
}
