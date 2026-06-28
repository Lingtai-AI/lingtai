package sqlitelog

import "testing"

func TestQueryRecentRefreshCompleteTimesNewestFirst(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'refresh_complete','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'refresh_start','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.5,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1500.25,'refresh_complete','{}');`,
	)
	times, err := QueryRecentRefreshCompleteTimes(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryRecentRefreshCompleteTimes: %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("expected 2 refresh_complete times, got %d", len(times))
	}
	if times[0].Unix() != 1500 || times[1].Unix() != 1000 {
		t.Fatalf("expected newest-first (1500,1000), got %d,%d", times[0].Unix(), times[1].Unix())
	}
}

func TestQueryRecentRefreshCompleteTimesRespectsLimit(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'refresh_complete','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2000.0,'refresh_complete','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(3000.0,'refresh_complete','{}');`,
	)
	times, err := QueryRecentRefreshCompleteTimes(agentDir, 2)
	if err != nil {
		t.Fatalf("QueryRecentRefreshCompleteTimes: %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(times))
	}
	if times[0].Unix() != 3000 || times[1].Unix() != 2000 {
		t.Fatalf("expected newest two (3000,2000), got %d,%d", times[0].Unix(), times[1].Unix())
	}
}

// refresh_start must NOT be treated as a completed refresh boundary.
func TestQueryRecentRefreshCompleteTimesIgnoresStart(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'refresh_start','{}');`,
	)
	times, err := QueryRecentRefreshCompleteTimes(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryRecentRefreshCompleteTimes: %v", err)
	}
	if len(times) != 0 {
		t.Fatalf("expected no refresh_complete times, got %d", len(times))
	}
}

func TestQueryRecentRefreshCompleteTimesMissingDB(t *testing.T) {
	_, err := QueryRecentRefreshCompleteTimes(t.TempDir(), 10)
	if err == nil {
		t.Fatal("expected error for missing sqlite sidecar")
	}
}
