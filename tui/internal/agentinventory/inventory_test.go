package agentinventory

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func writeAgent(t *testing.T, root, name, manifest, status string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for file, data := range map[string]string{".agent.json": manifest, ".status.json": status} {
		if data == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, file), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// exitedPID returns the PID of a child that has already been reaped.
func exitedPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func TestQueryFailureIsTypedNeverEmpty(t *testing.T) {
	records, err := Query(filepath.Join(t.TempDir(), "missing"))
	var qerr *QueryError
	if !errors.As(err, &qerr) || records != nil {
		t.Fatalf("want *QueryError and no records, got %v, %v", err, records)
	}
	if records, err = Query(t.TempDir()); err != nil || len(records) != 0 {
		t.Fatalf("empty root: want empty records and nil error, got %v, %v", records, err)
	}
}

func TestQueryStates(t *testing.T) {
	root := t.TempDir()
	runningStatus := fmt.Sprintf(`{"runtime":{"pid":%d,"running":true}}`, os.Getpid())
	goneStatus := fmt.Sprintf(`{"runtime":{"pid":%d,"running":true}}`, exitedPID(t))
	stoppedStatus := fmt.Sprintf(`{"runtime":{"pid":%d,"running":false}}`, exitedPID(t))
	writeAgent(t, root, "alive", `{"agent_name":"alice","address":"a@x","admin":{}}`, runningStatus)
	writeAgent(t, root, "gone", `{"agent_name":"bob","admin":{}}`, goneStatus)
	writeAgent(t, root, "stopped", `{"agent_name":"carol","admin":{}}`, stoppedStatus)
	writeAgent(t, root, "dormant", `{"agent_name":"dora","admin":{}}`, "")
	writeAgent(t, root, "broken", `{not json`, "")
	writeAgent(t, root, "human", `{"agent_name":"human","admin":null}`, "")
	writeAgent(t, root, "notagent", "", "")

	records, err := Query(root)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]Presence{
		"alice":  PresenceRunning,
		"bob":    PresenceConflict,
		"carol":  PresenceAbsent,
		"dora":   PresenceUnknown,
		"broken": PresenceUnknown,
	}
	if len(records) != len(want) {
		t.Fatalf("want %d records (notagent and human placeholder skipped), got %+v", len(want), records)
	}
	for _, r := range records {
		if want[r.Name] != r.Presence {
			t.Errorf("agent %s: want %s, got %s (%s)", r.Name, want[r.Name], r.Presence, r.Detail)
		}
		if (r.Presence == PresenceUnknown || r.Presence == PresenceConflict) && r.Detail == "" {
			t.Errorf("agent %s: %s record must carry a detail", r.Name, r.Presence)
		}
	}
	if records[0].Name != "alice" || records[0].Address != "a@x" {
		t.Errorf("want name-sorted records with manifest identity, got %+v", records[0])
	}
}
