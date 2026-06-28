package sqlitelog

import "testing"

func TestQueryRecentMoltTimesNewestFirst(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1001.0,'tool_call','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1002.5,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(1500.25,'psyche_molt','{}');`,
	)
	times, err := QueryRecentMoltTimes(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryRecentMoltTimes: %v", err)
	}
	if len(times) != 3 {
		t.Fatalf("expected 3 molt times, got %d", len(times))
	}
	wantUnix := []int64{1500, 1002, 1000}
	for i, w := range wantUnix {
		if got := times[i].Unix(); got != w {
			t.Fatalf("times[%d].Unix() = %d, want %d", i, got, w)
		}
	}
}

func TestQueryRecentMoltTimesRespectsLimit(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(2000.0,'psyche_molt','{}');`,
		`INSERT INTO events(ts,type,fields_json) VALUES(3000.0,'psyche_molt','{}');`,
	)
	times, err := QueryRecentMoltTimes(agentDir, 2)
	if err != nil {
		t.Fatalf("QueryRecentMoltTimes: %v", err)
	}
	if len(times) != 2 {
		t.Fatalf("expected limit of 2, got %d", len(times))
	}
	if times[0].Unix() != 3000 || times[1].Unix() != 2000 {
		t.Fatalf("expected newest two (3000,2000), got %d,%d", times[0].Unix(), times[1].Unix())
	}
}

func TestQueryRecentMoltTimesEmpty(t *testing.T) {
	agentDir := makeTestDB(t,
		`INSERT INTO events(ts,type,fields_json) VALUES(1000.0,'tool_call','{}');`,
	)
	times, err := QueryRecentMoltTimes(agentDir, 10)
	if err != nil {
		t.Fatalf("QueryRecentMoltTimes empty: %v", err)
	}
	if len(times) != 0 {
		t.Fatalf("expected no molt times, got %d", len(times))
	}
}

func TestQueryRecentMoltTimesMissingDB(t *testing.T) {
	_, err := QueryRecentMoltTimes(t.TempDir(), 10)
	if err == nil {
		t.Fatal("expected error for missing sqlite sidecar")
	}
}
