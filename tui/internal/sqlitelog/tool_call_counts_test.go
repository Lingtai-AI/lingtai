package sqlitelog

import (
	"testing"
	"time"
)

// TestQueryMoltSessionToolCallCountsBucketsCurrentAndLast exercises the bounded
// sqlite COUNT over main-agent type='tool_call' rows in the current and previous
// molt windows. tool_result rows, rebuilt daemon-scope rows, and rows before the
// previous window must be ignored. Boundary inclusivity matches the
// token-ledger semantics: current is ts >= currentSince, last is
// lastSince <= ts < lastBefore.
func TestQueryMoltSessionToolCallCountsBucketsCurrentAndLast(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(500.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1500.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json,source_kind,scope,run_id) VALUES(1600.0,'tool_call','{}','daemon_events','daemon','em-last');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2000.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2200.0,'tool_result','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2500.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json,source_kind,scope,run_id) VALUES(2600.0,'tool_call','{}','daemon_events','daemon','em-current');`,
	)
	// Two psyche_molts (1000, 2000): currentSince=2000, lastSince=1000,
	// lastBefore=2000.
	current, last, ok, err := QueryMoltSessionToolCallCounts(agentDir,
		time.Unix(2000, 0).UTC(), time.Unix(1000, 0).UTC(), time.Unix(2000, 0).UTC())
	if err != nil || !ok {
		t.Fatalf("QueryMoltSessionToolCallCounts: ok=%v err=%v", ok, err)
	}
	if current != 2 {
		t.Fatalf("current tool_call count = %d, want 2 (ts>=2000: 2000 and 2500)", current)
	}
	if last != 2 {
		t.Fatalf("last tool_call count = %d, want 2 (1000<=ts<2000: 1000 and 1500)", last)
	}
}

// TestQueryMoltSessionToolCallCountsNoPreviousSession verifies the previous
// bucket stays zero (and the current bucket still counts) when lastBefore is
// zero, i.e. there is at most one molt so no previous session exists.
func TestQueryMoltSessionToolCallCountsNoPreviousSession(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1500.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2100.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2400.0,'tool_call','{}');`,
	)
	// Single molt at 2000: currentSince=2000, no previous session.
	current, last, ok, err := QueryMoltSessionToolCallCounts(agentDir,
		time.Unix(2000, 0).UTC(), time.Time{}, time.Time{})
	if err != nil || !ok {
		t.Fatalf("QueryMoltSessionToolCallCounts: ok=%v err=%v", ok, err)
	}
	if current != 2 {
		t.Fatalf("current tool_call count = %d, want 2 (ts>=2000: 2100 and 2400)", current)
	}
	if last != 0 {
		t.Fatalf("last tool_call count = %d, want 0 (no previous session)", last)
	}
}

// TestQueryMoltSessionToolCallCountsNoMoltCountsAll confirms that with no molt
// at all (currentSince zero) every tool_call event is counted as current,
// matching the token-ledger "current = all" semantics.
func TestQueryMoltSessionToolCallCountsNoMoltCountsAll(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(100.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(200.0,'tool_result','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(300.0,'tool_call','{}');`,
	)
	current, last, ok, err := QueryMoltSessionToolCallCounts(agentDir,
		time.Time{}, time.Time{}, time.Time{})
	if err != nil || !ok {
		t.Fatalf("QueryMoltSessionToolCallCounts: ok=%v err=%v", ok, err)
	}
	if current != 2 {
		t.Fatalf("current tool_call count = %d, want 2 (all tool_call rows)", current)
	}
	if last != 0 {
		t.Fatalf("last tool_call count = %d, want 0", last)
	}
}

// TestQueryMoltSessionToolCallCountsMissingDB verifies graceful degradation: a
// missing sidecar returns ok=false so the caller falls back to events.jsonl.
func TestQueryMoltSessionToolCallCountsMissingDB(t *testing.T) {
	_, _, ok, err := QueryMoltSessionToolCallCounts(t.TempDir(),
		time.Unix(2000, 0).UTC(), time.Unix(1000, 0).UTC(), time.Unix(2000, 0).UTC())
	if ok || err == nil {
		t.Fatalf("expected ok=false, err non-nil for missing sidecar, got ok=%v err=%v", ok, err)
	}
}
