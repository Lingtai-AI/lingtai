package inventory_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/process"
	"github.com/anthropics/lingtai-tui/internal/processscan"
)

// The acceptance owner supplies an ABSOLUTE candidate-runtime interpreter.
// All original native inventory fixtures run through the real Core query, not
// a second Go policy implementation. Missing binding is a failure, not a skip.
func TestMain(m *testing.M) {
	python := os.Getenv("LINGTAI_PROJECT_INVENTORY_TEST_PYTHON")
	if python != "" {
		inventory.Query = func(observation inventory.Observation) (inventory.Decision, error) {
			return process.QueryProjectInventory(python, observation)
		}
	}
	os.Exit(m.Run())
}

func TestCandidateInventoryNativeDecodeAndProcessOnlyRows(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, ".lingtai", "live")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	// Typed ReadAgent rejects the identity field, but native raw decoding still
	// observes the literal true privilege. This row is MAIN yet disabled; Python
	// must not impose a stricter replacement manifest reader.
	if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(`{"agent_name":123,"admin":{"karma":true},"unknown":{"anything":[1,null,"x"]}}`), 0644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(root, ".lingtai", "process-only")
	snapshot := inventory.FromProcesses([]processscan.AgentProcess{{PID: 10, AgentDir: dir}, {PID: 11, AgentDir: missing}}, inventory.Options{})
	if snapshot.Err != nil {
		t.Fatal(snapshot.Err)
	}
	if len(snapshot.Records) != 2 || snapshot.Records[0].Role != inventory.RoleMain {
		t.Fatalf("native classification changed: %+v", snapshot)
	}
	for _, record := range snapshot.Records {
		if record.Enterable || record.EnterReason != inventory.EnterReasonManifestUnreadable || record.ReadError == "" {
			t.Fatalf("native read error not retained: %+v", record)
		}
	}
}
