package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"charm.land/lipgloss/v2"
)

func TestFormatHomeAsyncStats(t *testing.T) {
	t.Run("daemon fallback all-zero remains visible and honest", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{}, 120)
		if got == "" {
			t.Fatal("all-zero stats must still render the row")
		}
		for _, want := range []string{
			"Daemons:", "running 0", "queued 0", "done 0", "failed 0",
			"in 0", "out 0", "cache 0%", "calls 0",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("row missing %q: %q", want, got)
			}
		}
		if strings.Contains(got, "Async:") || strings.Contains(got, "Shell") {
			t.Fatalf("daemon-only fallback implied unified coverage: %q", got)
		}
	})

	t.Run("fallback counts render in order with daemon label", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 3, queued: 2, done: 12, failed: 1}, 160)
		for _, want := range []string{"Daemons:", "running 3", "queued 2", "done 12", "failed 1"} {
			if !strings.Contains(got, want) {
				t.Errorf("row missing %q: %q", want, got)
			}
		}
		if !strings.HasPrefix(got, "  ") {
			t.Errorf("row should be left-padded, got %q", got)
		}
	})

	t.Run("fallback token and api-call stats retain legacy portions", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{
			running: 3, queued: 2, done: 12, failed: 1,
			input: 1234, output: 567, cached: 300, calls: 9,
		}, 200)
		for _, want := range []string{
			"running 3", "queued 2", "done 12", "failed 1",
			"in 1.2k", "out 567", "cache 24%", "calls 9",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("row missing %q: %q", want, got)
			}
		}
	})

	t.Run("unified renders aggregate and both visibly distinct lanes", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{
			unified: true,
			running: 3, queued: 2, done: 12, failed: 1,
			daemon: homeAsyncCounts{running: 1, done: 4},
			shell:  homeAsyncCounts{running: 2, queued: 2, done: 8, failed: 1},
			input:  1200, output: 300, cached: 600, calls: 7,
			models: map[string]int64{"glm-5.2": 3},
		}, 240)
		for _, want := range []string{
			"Async:", "r3 q2 d12 f1", "Daemon r1 q0 d4 f0", "Shell r2 q2 d8 f1",
			"(glm-5.2 × 3)", "in 1.2k", "out 300", "cache 50%", "calls 7",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("unified row missing %q: %q", want, got)
			}
		}
		if strings.Contains(got, "Daemons:") {
			t.Fatalf("unified row used fallback label: %q", got)
		}
	})

	t.Run("lingtai models render directly or as stable model counts", func(t *testing.T) {
		single := formatHomeAsyncStats(homeAsyncStats{running: 1, models: map[string]int64{"gpt-5.6-terra": 1}}, 220)
		if want := "(gpt-5.6-terra)"; !strings.Contains(single, want) {
			t.Fatalf("single-model row missing %q: %q", want, single)
		}

		multiple := formatHomeAsyncStats(homeAsyncStats{running: 3, models: map[string]int64{"beta": 2, "alpha": 1}}, 220)
		if want := "(alpha × 1 · beta × 2)"; !strings.Contains(multiple, want) {
			t.Fatalf("multi-model row missing stable counts %q: %q", want, multiple)
		}
	})

	t.Run("model tail is bounded and control-free", func(t *testing.T) {
		got := formatHomeAsyncModelStats(map[string]int64{" alpha\n\t": 1, strings.Repeat("b", 200): 1, "\x00ignored": 1})
		if strings.ContainsAny(got, "\n\r\t\x00") {
			t.Fatalf("model tail retained a control character: %q", got)
		}
		if len([]rune(got)) > homeAsyncModelStatsMaxWidth+2 {
			t.Fatalf("model tail exceeded its hard display budget: %q", got)
		}
	})

	t.Run("fallback zero count buckets are omitted", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 1}, 160)
		if strings.Contains(got, "queued") || strings.Contains(got, "done") || strings.Contains(got, "failed") {
			t.Errorf("zero count buckets should be omitted: %q", got)
		}
	})

	t.Run("wide terminal retains daemons detail hint", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 1}, 240)
		if !strings.Contains(got, "/daemons for details") {
			t.Fatalf("wide row lost the existing detail affordance: %q", got)
		}
	})

	t.Run("narrow terminal remains one width-safe row", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{
			unified: true, running: 9223372036854775807,
			daemon: homeAsyncCounts{running: 9223372036854775807},
		}, 30)
		if got == "" {
			t.Fatal("expected a row on narrow terminal")
		}
		if gotWidth := lipgloss.Width(got); gotWidth > 30 {
			t.Fatalf("narrow row width = %d, want <= 30: %q", gotWidth, got)
		}
	})
}

func writeAsyncTestAgentRecord(t *testing.T, dir, child string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "system"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := fmt.Sprintf(`{
  "schema":"lingtai.agent_record/v1","schema_version":1,
  "model":{"provider":"zhipu","model":"session-model"},
  "usage":{"api_calls":42},"async_work":%s
}`, child)
	if err := os.WriteFile(filepath.Join(dir, "system", "agent_record.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func validAsyncTestChild(generatedAt time.Time) string {
	return fmt.Sprintf(`{
  "schema":"lingtai.async_work/v1","schema_version":1,
  "generated_at":%q,"window_seconds":600,
  "running":3,"queued":2,"done":12,"failed":1,
  "daemon":{"running":1,"queued":0,"done":4,"failed":0,
    "model_counts":{"record-model":4},
    "usage":{"input_tokens":1200,"output_tokens":300,"thinking_tokens":40,"cached_tokens":600,"api_calls":7}},
  "shell":{"running":2,"queued":2,"done":8,"failed":1}
}`, generatedAt.UTC().Format(time.RFC3339Nano))
}

func writeAsyncTestFallbackRun(t *testing.T, dir string) {
	t.Helper()
	runDir := filepath.Join(dir, "daemons", "fallback-run")
	if err := os.MkdirAll(runDir, 0o755); err != nil {
		t.Fatal(err)
	}
	card := `{"state":"running","backend":"lingtai","model":"fallback-model","tokens":{"input":99,"output":10,"cached":9,"calls":2}}`
	if err := os.WriteFile(filepath.Join(runDir, "daemon.json"), []byte(card), 0o644); err != nil {
		t.Fatal(err)
	}
	ledger := fmt.Sprintf(`{"schema":"lingtai.daemon_dispatch/v1","sequence":1,"run_id":"fallback-run","created_at":%q}`+"\n", time.Now().UTC().Format(time.RFC3339Nano))
	if err := os.WriteFile(filepath.Join(dir, "daemons", ".dispatch-ledger.jsonl"), []byte(ledger), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGatherHomeAsyncStatsAgentRecordWinsOverDivergentFallback(t *testing.T) {
	now := time.Now().UTC()
	dir := t.TempDir()
	writeAsyncTestFallbackRun(t, dir) // divergent fallback: running=1, fallback-model
	writeAsyncTestAgentRecord(t, dir, validAsyncTestChild(now))

	got := gatherHomeAsyncStats(dir, now)
	if !got.unified || got.running != 3 || got.daemon.running != 1 || got.shell.running != 2 {
		t.Fatalf("published snapshot did not win: %+v", got)
	}
	if got.models["record-model"] != 4 || got.models["fallback-model"] != 0 {
		t.Fatalf("daemon-scoped published models not preserved: %v", got.models)
	}
	row := formatHomeAsyncStats(got, 240)
	for _, want := range []string{"Async:", "r3 q2 d12 f1", "Daemon r1 q0 d4 f0", "Shell r2 q2 d8 f1"} {
		if !strings.Contains(row, want) {
			t.Errorf("published row missing %q: %q", want, row)
		}
	}
}

func TestGatherHomeAsyncStatsUnavailableChildFallsBackHonestly(t *testing.T) {
	now := time.Now().UTC()
	valid := validAsyncTestChild(now)
	baseTimestamp := now.Format(time.RFC3339Nano)
	commaTimestamp := now.Format("2006-01-02T15:04:05") + ",000Z"
	invalidOffsetTimestamp := now.Add(24*time.Hour).Format("2006-01-02T15:04:05") + "+24:00"
	duplicateEmptyShell := strings.TrimSuffix(valid, "\n}") + ",\n  \"shell\":{}\n}"
	for _, tc := range []struct {
		name  string
		child string
	}{
		{"missing", "__MISSING__"},
		{"malformed", `{"schema":17,"daemon":"bad"}`},
		{"stale", validAsyncTestChild(now.Add(-601 * time.Second))},
		{"capitalized required count", strings.Replace(valid, `"running":3`, `"Running":3`, 1)},
		{"duplicate empty shell lane", duplicateEmptyShell},
		{"null zero-valued daemon count", strings.Replace(valid, `"queued":0`, `"queued":null`, 1)},
		{"null daemon usage", strings.Replace(valid, `"api_calls":7`, `"api_calls":null`, 1)},
		{"comma fractional timestamp", strings.Replace(valid, baseTimestamp, commaTimestamp, 1)},
		{"invalid 24-hour offset", strings.Replace(valid, baseTimestamp, invalidOffsetTimestamp, 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			writeAsyncTestFallbackRun(t, dir)
			if tc.child == "__MISSING__" {
				if err := os.MkdirAll(filepath.Join(dir, "system"), 0o755); err != nil {
					t.Fatal(err)
				}
				body := `{"schema":"lingtai.agent_record/v1","schema_version":1,"model":{"model":"still-valid"},"usage":{"api_calls":8}}`
				if err := os.WriteFile(filepath.Join(dir, "system", "agent_record.json"), []byte(body), 0o644); err != nil {
					t.Fatal(err)
				}
			} else {
				writeAsyncTestAgentRecord(t, dir, tc.child)
			}

			telemetry := (MailModel{orchestrator: dir}).gatherHomeTelemetry()
			wantModel, wantCalls := "session-model", int64(42)
			if tc.child == "__MISSING__" {
				wantModel, wantCalls = "still-valid", 8
			}
			if telemetry.model != wantModel || telemetry.apiCalls != wantCalls {
				t.Fatalf("optional async child hid top-level telemetry: model=%q calls=%d", telemetry.model, telemetry.apiCalls)
			}

			got := gatherHomeAsyncStats(dir, now)
			if got.unified || got.running != 1 || got.models["fallback-model"] != 1 {
				t.Fatalf("fallback snapshot = %+v", got)
			}
			row := formatHomeAsyncStats(got, 240)
			if !strings.Contains(row, "Daemons:") || strings.Contains(row, "Async:") || strings.Contains(row, "Shell") {
				t.Fatalf("fallback scope mislabeled: %q", row)
			}
		})
	}
}

// TestHomeAsyncStatsRowVisibilityAfterLoad pins the always-visible contract:
// the row is hidden only before the first snapshot lands and stays visible even
// when the loaded snapshot contains all-zero counts.
func TestHomeAsyncStatsRowVisibilityAfterLoad(t *testing.T) {
	var m MailModel
	if m.hasHomeAsyncStats() {
		t.Fatal("row must be hidden before the first snapshot lands")
	}
	if !m.applyHomeAsyncStats(homeAsyncStats{}, time.Now()) {
		t.Fatal("first snapshot landing must flip the row to visible")
	}
	if !m.hasHomeAsyncStats() {
		t.Fatal("row must be visible after load even when all counts are zero")
	}
	if m.applyHomeAsyncStats(homeAsyncStats{running: 1}, time.Now()) {
		t.Fatal("later snapshots must not flip visibility")
	}
	m.applyHomeAsyncStats(homeAsyncStats{}, time.Now())
	if !m.hasHomeAsyncStats() {
		t.Fatal("row must stay visible when counts drop back to zero")
	}
}

func TestHomeAsyncStatsSchedulingStateTransitions(t *testing.T) {
	var m MailModel
	if cmd := m.maybeScheduleHomeAsyncStats(time.Unix(1000, 0)); cmd == nil {
		t.Fatal("first async schedule must return a fetch command")
	}
	if !m.homeAsyncStatsInFlight {
		t.Fatal("scheduling must mark async fetch in-flight")
	}
	if cmd := m.maybeScheduleHomeAsyncStats(time.Unix(1001, 0)); cmd != nil {
		t.Fatal("in-flight async fetch must suppress overlap")
	}
	m.applyHomeAsyncStats(homeAsyncStats{}, time.Unix(1002, 0))
	if m.homeAsyncStatsInFlight {
		t.Fatal("accepted async completion must clear in-flight")
	}
}
