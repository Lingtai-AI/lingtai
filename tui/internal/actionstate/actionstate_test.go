package actionstate

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeHeartbeat(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluate(t *testing.T) {
	now := float64(time.Now().UnixNano()) / 1e9
	cases := []struct {
		name      string
		heartbeat string // "" means no file
		want      Observation
		canStart  bool
		canRefr   bool
	}{
		{"absent file is stopped", "", ObservationStopped, true, false},
		{"fresh is running", fmt.Sprintf("%f", now), ObservationRunning, false, true},
		{"stale is stopped", fmt.Sprintf("%f", now-100), ObservationStopped, true, false},
		{"malformed is unknown", "not-a-timestamp", ObservationUnknown, false, false},
		{"future is unknown", fmt.Sprintf("%f", now+3600), ObservationUnknown, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			if tc.heartbeat != "" {
				writeHeartbeat(t, dir, tc.heartbeat)
			}
			d := Evaluate(dir, 3.0)
			if d.Observation != tc.want {
				t.Fatalf("observation = %v, want %v (reason %q)", d.Observation, tc.want, d.Reason)
			}
			if d.CanStart != tc.canStart || d.CanRestart != tc.canStart || d.CanRefresh != tc.canRefr {
				t.Fatalf("enablement start=%v restart=%v refresh=%v, want start/restart=%v refresh=%v",
					d.CanStart, d.CanRestart, d.CanRefresh, tc.canStart, tc.canRefr)
			}
			if d.Reason == "" {
				t.Fatal("reason must not be empty")
			}
		})
	}
}

// A heartbeat path that exists but cannot be read as a file must be
// Unknown, never Stopped: enabling restart here would be a false verdict.
func TestEvaluateUnreadableIsUnknownNotStopped(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ".agent.heartbeat"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := Evaluate(dir, 3.0)
	if d.Observation != ObservationUnknown {
		t.Fatalf("observation = %v, want Unknown (reason %q)", d.Observation, d.Reason)
	}
	if d.CanStart || d.CanRestart || d.CanRefresh {
		t.Fatalf("unknown must disable everything, got %+v", d)
	}
}
