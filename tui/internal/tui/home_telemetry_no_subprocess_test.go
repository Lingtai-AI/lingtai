package tui

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// statusFixture is the shape of the `.status.json` token block the kernel
// publishes on every turn (lingtai-kernel base_agent/identity.py `_status`).
// The five scalar fields are the CURRENT SESSION (since the latest psyche_molt)
// counters: SessionManager.reset_session_token_usage zeroes them on a successful
// molt and restore_token_state preserves them across a refresh. They share the
// boundary fs.SumMoltSessionTokenLedger(...).Current re-derives, not necessarily
// its totals: the ledger additionally counts source=soul consultation rows.
//
// `total_tokens` is deliberately set to a value INCONSISTENT with
// input+output+thinking. The kernel always writes the true sum there, so no real
// snapshot looks like this — the inconsistency exists purely so that decoding
// `tokens.total_tokens` and assigning it to sessionTokens (instead of summing the
// three components) fails this test loudly. That sibling field must stay
// undecoded: it duplicates the sum, and a `Tokens.TotalTokens` sitting next to
// the context block's own TotalTokens is exactly how a double count gets in.
const statusFixture = `{
  "runtime": {"pid": 4242, "running": true, "uptime_seconds": 12.5},
  "tokens": {
    "input_tokens": 1200,
    "output_tokens": 340,
    "thinking_tokens": 60,
    "cached_tokens": 900,
    "total_tokens": 999999,
    "api_calls": 7,
    "estimated": false,
    "context": {
      "system_tokens": 100,
      "tools_tokens": 200,
      "history_tokens": 47700,
      "total_tokens": 48000,
      "window_size": 200000,
      "usage_pct": 24.0
    }
  }
}`

// homeTelemetrySQLiteFixtureGzipBase64 is a deterministic 1,536-byte SQLite 3
// database (512-byte pages) created from:
//
//	CREATE TABLE events (id INTEGER PRIMARY KEY AUTOINCREMENT, ts REAL NOT NULL,
//	  type TEXT NOT NULL, fields_json TEXT NOT NULL DEFAULT '{}');
//	INSERT INTO events(ts,type) VALUES
//	  (1784998800.0,'psyche_molt'), (1785002400.0,'psyche_molt');
//
// Embedding the compressed bytes keeps this contract independent of an installed
// sqlite3 binary while still giving the legacy path a real sidecar it could query.
const homeTelemetrySQLiteFixtureGzipBase64 = "H4sIAAAAAAAC/wsO9MksSVVIyy/KTSxRMGZgYmBkZHBQUGBgYGCGYhhgAmIWJD4jA2HAzKA39TQvSDMjFwNjDJAYtCCAiU1cW5sxsiQxKSe1uDAHGCzxxamFpal5yehcZucgV8cQV4UQRycfVwU0SY28xNxUHSBPs1GRkV1cUpKxSRZsZmpZal5JMYRkQjEBIqagkZmi4OkX4uruGqQQEOTp6xgUqeDtGqngGBri7+kH1OHr6heiowBUCdTso+DnH6LgF+rjAxSpLEhVCHGNCEESS8tMzUkpjs8qzs9DlVJwcXVzDPUJUVCvrlXXhMTNBQbGF0BiFAx5IMbEysCiLJiV8nNBQXFlckZqfG5+Tkl1rRgjVPz1BBRxUPwzMn5hAKJRMEIAFyOzJCO0HAIAYOJhlQAGAAA="

func writeValidHomeTelemetrySQLiteFixture(t *testing.T, path string) {
	t.Helper()
	packed, err := base64.StdEncoding.DecodeString(homeTelemetrySQLiteFixtureGzipBase64)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := gzip.NewReader(bytes.NewReader(packed))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if err := zr.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func validateHomeTelemetrySQLiteFixture(data []byte) error {
	const expectedSHA256 = "4426ba7085f66722720a2bfa1c03b2d009a904a9a22fe4a97cfefc65e031f3df"
	if got := fmt.Sprintf("%x", sha256.Sum256(data)); got != expectedSHA256 {
		return fmt.Errorf("SHA-256 %s, want %s", got, expectedSHA256)
	}
	if len(data) != 1536 || !bytes.HasPrefix(data, []byte("SQLite format 3\x00")) {
		return fmt.Errorf("not the expected SQLite 3 database (size=%d)", len(data))
	}
	if !bytes.Contains(data, []byte("CREATE TABLE events")) {
		return fmt.Errorf("does not contain the events schema")
	}
	if got := bytes.Count(data, []byte("psyche_molt")); got != 2 {
		return fmt.Errorf("contains %d psyche_molt rows, want 2", got)
	}
	return nil
}

func assertValidHomeTelemetrySQLiteFixture(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateHomeTelemetrySQLiteFixture(data); err != nil {
		t.Fatalf("sqlite fixture %s %v", path, err)
	}
}

func TestHomeTelemetrySQLiteFixtureGuardRejectsStructuredJunk(t *testing.T) {
	junk := bytes.Repeat([]byte{'X'}, 1536)
	copy(junk, []byte("SQLite format 3\x00"))
	copy(junk[128:], []byte("CREATE TABLE events"))
	copy(junk[256:], []byte("psyche_molt"))
	copy(junk[512:], []byte("psyche_molt"))
	if err := validateHomeTelemetrySQLiteFixture(junk); err == nil {
		t.Fatal("structured junk passed the fixture guard; bind the guard to the independently validated SQLite bytes")
	}
}

// writeHomeTelemetryFixture builds a real orchestrator directory carrying the
// legacy log/sidecar state the pre-#643 Home path consults — canonical
// events.jsonl with two psyche_molt boundaries, a token_ledger.jsonl row inside
// the current window, and a logs/log.sqlite sidecar so
// sqlitelog.QueryMoltSessionWindows does NOT short-circuit on a missing database
// — plus, when status != "", a `.status.json` snapshot.
//
// The ledger numbers are deliberately DIFFERENT from the `.status.json` numbers
// (api 1 / 26 tokens vs api 7 / 1600 tokens) so an economy assertion can never
// pass by coincidence when the wrong source is read.
//
// It also installs a recording `sqlite3` shim first on PATH. The sidecar is a
// real SQLite events database with those two molt rows; the shim returns those
// same timestamps while isolating whether Home forks at all. Any failure below is
// therefore the forbidden fork or wrong source, never a missing/invalid sidecar.
func writeHomeTelemetryFixture(t *testing.T, status string) (agentDir, marker string) {
	t.Helper()

	agentDir = filepath.Join(t.TempDir(), "agent")
	logsDir := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}

	previousMolt := time.Date(2026, 7, 25, 17, 0, 0, 0, time.UTC)
	latestMolt := previousMolt.Add(time.Hour)
	events := fmt.Sprintf(
		`{"type":"psyche_molt","ts":%d}`+"\n"+
			`{"type":"tool_call","ts":%d}`+"\n"+
			`{"type":"psyche_molt","ts":%d}`+"\n"+
			`{"type":"tool_call","ts":%d}`+"\n",
		previousMolt.Unix(), previousMolt.Add(time.Minute).Unix(),
		latestMolt.Unix(), latestMolt.Add(time.Minute).Unix(),
	)
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger := fmt.Sprintf(
		`{"ts":%q,"input":21,"output":3,"thinking":2,"cached":13}`+"\n",
		latestMolt.Add(time.Minute).Format(time.RFC3339),
	)
	if err := os.WriteFile(filepath.Join(logsDir, "token_ledger.jsonl"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
	sidecarPath := filepath.Join(logsDir, "log.sqlite")
	writeValidHomeTelemetrySQLiteFixture(t, sidecarPath)
	assertValidHomeTelemetrySQLiteFixture(t, sidecarPath)
	if status != "" {
		if err := os.WriteFile(filepath.Join(agentDir, ".status.json"), []byte(status), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	binDir := t.TempDir()
	marker = filepath.Join(t.TempDir(), "sqlite3-invoked")
	script := fmt.Sprintf(
		"#!/bin/sh\nprintf 'invoked\\n' >> \"$LINGTAI_SQLITE_MARKER\"\nprintf '%d\\n%d\\n'\n",
		latestMolt.Unix(), previousMolt.Unix(),
	)
	if err := os.WriteFile(filepath.Join(binDir, "sqlite3"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	t.Setenv("LINGTAI_SQLITE_MARKER", marker)

	return agentDir, marker
}

// assertNoSQLiteFork fails when the recording shim was executed at all.
func assertNoSQLiteFork(t *testing.T, marker string) {
	t.Helper()
	if _, err := os.Stat(marker); err == nil {
		t.Error("home telemetry forked sqlite3; the once-per-second Home gather must read the published .status.json snapshot instead")
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat sqlite3 marker: %v", err)
	}
}

// TestHomeTelemetryReadsStatusWithoutSQLiteSubprocess is the issue #643
// contract. The background Home telemetry worker runs once per second; on macOS
// each `sqlite3` fork it performs is visible process churn and can surface
// MallocStackLogging noise. The kernel already publishes its main-session
// since-last-molt economy — `.status.json` `tokens.{input,output,
// thinking,cached}_tokens` and `tokens.api_calls` — atomically beside the context
// block Home reads. It shares the ledger's molt boundary but omits `source=soul`
// rows, so Home intentionally shows the kernel's own counters and never touches
// the sqlite sidecar, ledger, or canonical events.
//
// The assertion runs through the real production entry (fetchHomeTelemetry →
// gatherHomeTelemetry), not a copied parser.
func TestHomeTelemetryReadsStatusWithoutSQLiteSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sqlite3 shadow executable fixture is POSIX-only")
	}

	agentDir, marker := writeHomeTelemetryFixture(t, statusFixture)

	msg, ok := (MailModel{orchestrator: agentDir}).fetchHomeTelemetry().(homeTelemetryMsg)
	if !ok {
		t.Fatalf("fetchHomeTelemetry returned %T, want homeTelemetryMsg", msg)
	}
	got := msg.t

	// Session economy — straight from `.status.json`, NOT the ledger's 1 call /
	// 26 tokens. sessionTokens counts input+output+thinking exactly once.
	if got.apiCalls != 7 {
		t.Errorf("apiCalls = %d, want 7 from .status.json tokens.api_calls", got.apiCalls)
	}
	// Each of output and thinking counted EXACTLY ONCE, and cached (a subset of
	// input) never added in. The fixture's inconsistent `total_tokens` (999999)
	// means reading that sibling field instead of summing the three components
	// fails right here.
	if got.sessionTokens != 1600 {
		t.Errorf("sessionTokens = %d, want 1600 (input 1200 + output 340 + thinking 60) from .status.json; "+
			"the sibling tokens.total_tokens must not be read", got.sessionTokens)
	}
	if got.cached != 900 {
		t.Errorf("cached = %d, want 900 from .status.json tokens.cached_tokens", got.cached)
	}
	if got.inputTokens != 1200 {
		t.Errorf("inputTokens = %d, want 1200 from .status.json tokens.input_tokens", got.inputTokens)
	}

	// Context — the same snapshot, unchanged semantics.
	if got.contextLimit != 200000 {
		t.Errorf("contextLimit = %d, want 200000", got.contextLimit)
	}
	if got.contextUsed != 48000 {
		t.Errorf("contextUsed = %d, want 48000", got.contextUsed)
	}
	if math.Abs(got.contextUsage-0.24) > 1e-9 {
		t.Errorf("contextUsage = %v, want 0.24", got.contextUsage)
	}

	assertNoSQLiteFork(t, marker)
}

// TestHomeTelemetryDegradesWithoutStatusAndWithoutSQLiteSubprocess pins the
// degradation contract: when `.status.json` is absent or malformed the token
// fragments must fall to zero (formatHomeTelemetry then omits them) — they must
// NOT revive the sqlite/ledger/events lookup as a fallback, because that is the
// exact once-per-second subprocess issue #643 is about. The context fallbacks
// stay independent: with no manifest and no session cache, contextUsage remains
// the -1 unknown sentinel.
//
// Each case gets its own fixture directory: sqlitelog's molt-window cache and
// fs's ledger cache are process-global, so a shared directory would let a later
// case skip the fork a fresh agent would perform.
func TestHomeTelemetryDegradesWithoutStatusAndWithoutSQLiteSubprocess(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sqlite3 shadow executable fixture is POSIX-only")
	}

	for _, tc := range []struct {
		name   string
		status string
	}{
		{name: "missing status", status: ""},
		{name: "malformed status", status: "{not valid json"},
		{name: "legacy status without token totals", status: `{"tokens":{"estimated":true,"context":{}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			agentDir, marker := writeHomeTelemetryFixture(t, tc.status)

			msg, ok := (MailModel{orchestrator: agentDir}).fetchHomeTelemetry().(homeTelemetryMsg)
			if !ok {
				t.Fatalf("fetchHomeTelemetry returned %T, want homeTelemetryMsg", msg)
			}
			got := msg.t

			if got.apiCalls != 0 || got.sessionTokens != 0 || got.cached != 0 || got.inputTokens != 0 {
				t.Errorf("token fragments = {api %d, tok %d, cached %d, input %d}, want all zero; "+
					"a missing/legacy status must degrade to no economy, never fall back to the ledger",
					got.apiCalls, got.sessionTokens, got.cached, got.inputTokens)
			}
			if got.contextLimit != 0 || got.contextUsed != 0 {
				t.Errorf("context = used %d / limit %d, want 0/0 with no status and no manifest", got.contextUsed, got.contextLimit)
			}
			if got.contextUsage != -1 {
				t.Errorf("contextUsage = %v, want the -1 unknown sentinel", got.contextUsage)
			}

			assertNoSQLiteFork(t, marker)
		})
	}
}
