package inventory

import (
	"encoding/base64"
	"errors"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/processscan"
)

func TestFailedScanAndQueryPreserveOriginalError(t *testing.T) {
	oldScan, oldQuery := scanProcesses, Query
	t.Cleanup(func() { scanProcesses, Query = oldScan, oldQuery })
	original := errors.New("original native scan error")
	scanProcesses = func() ([]processscan.AgentProcess, error) { return nil, original }
	Query = func(Observation) (Decision, error) { t.Fatal("query after failed scan"); return Decision{}, nil }
	if _, err := Scan(Options{}); err != original {
		t.Fatalf("scan error identity lost: %v", err)
	}
	Query = func(Observation) (Decision, error) { return Decision{}, original }
	if snapshot := FromProcesses(nil, Options{}); snapshot.Err != original {
		t.Fatalf("query error identity lost: %v", snapshot.Err)
	}
}

func TestReceiptBindsNativeIdentityAndPID(t *testing.T) {
	// No filesystem dependency, including on Windows: invalid bytes are opaque.
	dir := "/project/.lingtai/\xff"
	records := []Record{{AgentDir: dir, Project: "/project", PID: 123, ReadError: "original native text\xff"}}
	observation := Observation{Members: []MemberObservation{{Row: 0, Directory: []byte(dir), Project: []byte("/project"), PID: 123}}}
	decision := Decision{Status: "inventory_queried", Members: []MemberDecision{{Row: 0, DirectoryB64: base64.StdEncoding.EncodeToString([]byte(dir)), PID: 123, Role: RoleUnknown, Reason: EnterReasonManifestUnreadable}}, Groups: [][]int{{0}}, PhantomProjects: []int{}}
	snapshot, err := projectSnapshot("", records, observation, decision)
	if err != nil || snapshot.Records[0].EnterDetail != records[0].ReadError || snapshot.Records[0].AgentDir != dir {
		t.Fatalf("native identity/error lost: %+v %v", snapshot, err)
	}
	decision.Members[0].PID++
	if _, err := projectSnapshot("", records, observation, decision); err == nil {
		t.Fatal("accepted replaced PID receipt")
	}
	decision.Members[0].PID--
	decision.Members[0].DirectoryB64 = base64.StdEncoding.EncodeToString([]byte("/different"))
	if _, err := projectSnapshot("", records, observation, decision); err == nil {
		t.Fatal("accepted replaced native directory receipt")
	}
}
