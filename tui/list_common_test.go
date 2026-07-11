package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/processscan"
)

func writeListAgentManifest(t *testing.T, agentDir, name string, admin string) {
	t.Helper()
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if admin == "" {
		admin = `{}`
	}
	body := fmt.Sprintf(`{"address":%q,"agent_name":%q,"nickname":"","state":"IDLE","admin":%s}`, name, name, admin)
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParseListArgsModesAndDir(t *testing.T) {
	opts, err := parseListArgs([]string{"--detailed", "--admin", "--json", "./project"})
	if err != nil {
		t.Fatalf("parseListArgs returned error: %v", err)
	}
	if !opts.Detailed || !opts.Admin || !opts.JSON {
		t.Fatalf("expected detailed/admin/json true, got detailed=%v admin=%v json=%v", opts.Detailed, opts.Admin, opts.JSON)
	}
	want, _ := filepath.Abs("./project")
	if opts.FilterDir != want {
		t.Fatalf("FilterDir=%q, want %q", opts.FilterDir, want)
	}
}

func TestInventoryFromProcessesPreservesSpacesAndFilter(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "Project With Spaces")
	agentDir := filepath.Join(project, ".lingtai", "agent A")
	otherProject := filepath.Join(root, "Other Project")
	otherAgentDir := filepath.Join(otherProject, ".lingtai", "agent B")
	writeListAgentManifest(t, agentDir, "agent A", `{}`)

	snap := inventory.FromProcesses([]processscan.AgentProcess{
		{PID: 111, Uptime: "00:01:02", AgentDir: agentDir},
		{PID: 222, Uptime: "00:02:03", AgentDir: otherAgentDir},
		{PID: 333, Uptime: "00:03:04", AgentDir: agentDir},
	}, inventory.Options{FilterDir: project, SelfPID: 333})
	got := snap.Records

	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	proc := got[0]
	if proc.PID != 111 || proc.Uptime != "00:01:02" {
		t.Fatalf("unexpected pid/uptime: %+v", proc)
	}
	if proc.Agent != "agent A" || proc.Project != project || proc.AgentDir != agentDir {
		t.Fatalf("space-containing path was not preserved: %+v", proc)
	}
	if len(snap.PhantomDirs) != 0 {
		t.Fatalf("existing project with spaces should not be phantom: %+v", snap.PhantomDirs)
	}
}

func TestInventoryKeepsDistinctSpacePaths(t *testing.T) {
	root := t.TempDir()
	dirA := filepath.Join(root, "Project With Spaces", ".lingtai", "agent A")
	dirB := filepath.Join(root, "Project With Spaces", ".lingtai", "agent B")
	writeListAgentManifest(t, dirA, "agent A", `{}`)
	writeListAgentManifest(t, dirB, "agent B", `{}`)

	got := inventory.FromProcesses([]processscan.AgentProcess{
		{PID: 111, AgentDir: dirA},
		{PID: 222, AgentDir: dirB},
	}, inventory.Options{}).Records
	if len(got) != 2 {
		t.Fatalf("distinct full dirs should not collapse, got %+v", got)
	}
}

func TestPrintListJSONIncludesHeartbeatAndLock(t *testing.T) {
	project := t.TempDir()
	agentDir := filepath.Join(project, ".lingtai", "agent-a")
	writeListAgentManifest(t, agentDir, "agent-a", `{}`)
	heartbeat := fmt.Sprintf("%.6f", float64(time.Now().UnixNano())/1e9)
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.heartbeat"), []byte(heartbeat), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.lock"), []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := inventory.FromProcesses([]processscan.AgentProcess{{PID: 123, Uptime: "1s", AgentDir: agentDir}}, inventory.Options{})

	var buf bytes.Buffer
	printListJSON(&buf, snap, listOptions{JSON: true})
	var parsed listJSONOutput
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	if parsed.Count != 1 || len(parsed.Processes) != 1 {
		t.Fatalf("unexpected process count: %+v", parsed)
	}
	got := parsed.Processes[0]
	if !got.Heartbeat.Fresh {
		t.Fatalf("heartbeat should be fresh: %+v", got.Heartbeat)
	}
	if !got.LockExists {
		t.Fatal("lock_exists should be true")
	}
}

func TestPrintListJSONKeepsBaselineProcessKeyShape(t *testing.T) {
	record := inventory.Record{
		PID:        123,
		Uptime:     "1s",
		Role:       inventory.RoleAgent,
		Agent:      "agent",
		Project:    "/tmp/project",
		AgentDir:   "/tmp/project/.lingtai/agent",
		Address:    "agent-address",
		AgentName:  "agent-name",
		Nickname:   "nick",
		State:      "IDLE",
		ReadError:  "read manifest: denied",
		LockExists: true,
		Enterable:  true,
	}
	snap := inventory.Snapshot{Records: []inventory.Record{record}}

	var buf bytes.Buffer
	printListJSON(&buf, snap, listOptions{JSON: true})
	var raw map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &raw); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, buf.String())
	}
	processes, ok := raw["processes"].([]interface{})
	if !ok || len(processes) != 1 {
		t.Fatalf("processes shape = %#v", raw["processes"])
	}
	proc, ok := processes[0].(map[string]interface{})
	if !ok {
		t.Fatalf("process shape = %#v", processes[0])
	}
	wantKeys := map[string]bool{
		"pid": true, "uptime": true, "role": true, "agent": true, "project": true,
		"agent_dir": true, "address": true, "agent_name": true, "nickname": true,
		"state": true, "read_error": true, "heartbeat": true, "lock_exists": true,
	}
	if len(proc) != len(wantKeys) {
		t.Fatalf("process keys = %v, want exactly %v", sortedMapKeys(proc), sortedBoolKeys(wantKeys))
	}
	for key := range wantKeys {
		if _, ok := proc[key]; !ok {
			t.Fatalf("process missing key %q; keys=%v", key, sortedMapKeys(proc))
		}
	}
	for _, forbidden := range []string{"admin_summary", "im_handles", "phantom", "enterable", "enter_error"} {
		if _, ok := proc[forbidden]; ok {
			t.Fatalf("process JSON leaked %q: %s", forbidden, buf.String())
		}
	}
}

func TestListInventoryCanIncludeHumanRecords(t *testing.T) {
	project := t.TempDir()
	humanDir := filepath.Join(project, ".lingtai", "human")
	writeListAgentManifest(t, humanDir, "human", `null`)

	snap := inventory.FromProcesses([]processscan.AgentProcess{{PID: 44, AgentDir: humanDir}}, inventory.Options{IncludeHuman: true})
	if len(snap.Records) != 1 {
		t.Fatalf("records = %+v, want human record included", snap.Records)
	}
	if snap.Records[0].Role != inventory.RoleHuman {
		t.Fatalf("role = %q, want HUMAN", snap.Records[0].Role)
	}
	var buf bytes.Buffer
	printList(&buf, snap.Records, listOptions{}, false)
	if !strings.Contains(buf.String(), "HUMAN") {
		t.Fatalf("list output missing HUMAN role:\n%s", buf.String())
	}
}

func TestInventoryPrefersRuntimeStatusPID(t *testing.T) {
	project := t.TempDir()
	agentDir := filepath.Join(project, ".lingtai", "agent-a")
	writeListAgentManifest(t, agentDir, "agent-a", `{}`)
	status := []byte(`{"runtime":{"pid":222,"running":true}}`)
	if err := os.WriteFile(filepath.Join(agentDir, ".status.json"), status, 0o644); err != nil {
		t.Fatal(err)
	}

	got := inventory.FromProcesses([]processscan.AgentProcess{
		{PID: 111, AgentDir: agentDir},
		{PID: 222, AgentDir: agentDir},
	}, inventory.Options{}).Records
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1: %+v", len(got), got)
	}
	if got[0].PID != 222 {
		t.Fatalf("PID = %d, want status pid 222", got[0].PID)
	}
}

func TestParseListArgsRejectsUnknownFlag(t *testing.T) {
	if _, err := parseListArgs([]string{"--verbose"}); err == nil {
		t.Fatal("expected unknown flag error")
	}
}

func adminRawFromJSON(raw string) interface{} {
	var manifest map[string]interface{}
	_ = json.Unmarshal([]byte(raw), &manifest)
	return manifest["admin"]
}

func TestSummarizeAdmin(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"null", `{"admin": null}`, "admin=null"},
		{"empty", `{"admin": {}}`, "admin={}"},
		{"sorted", `{"admin": {"nirvana": false, "karma": true}}`, "karma=true,nirvana=false"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := inventory.SummarizeAdmin(adminRawFromJSON(tc.raw)); got != tc.want {
				t.Fatalf("summarizeAdmin=%q, want %q", got, tc.want)
			}
		})
	}
}

func TestRoleLabel(t *testing.T) {
	if got := inventory.RoleFor(inventory.Record{IsOrchestrator: true}); got != inventory.RoleMain {
		t.Fatalf("orchestrator role=%q", got)
	}
	if got := inventory.RoleFor(inventory.Record{IsHuman: true}); got != inventory.RoleHuman {
		t.Fatalf("human role=%q", got)
	}
	if got := inventory.RoleFor(inventory.Record{}); got != inventory.RoleAgent {
		t.Fatalf("agent role=%q", got)
	}
}

func TestPrintListDetailedAndAdmin(t *testing.T) {
	procs := []inventory.Record{
		{
			PID:            123,
			Uptime:         "1m 0s",
			Role:           inventory.RoleMain,
			Agent:          "mimo-1",
			Project:        "/tmp/project",
			AgentDir:       "/tmp/project/.lingtai/mimo-1",
			Address:        "mimo-1",
			AgentName:      "mimo-1",
			Nickname:       "Mimo",
			State:          "IDLE",
			IsOrchestrator: true,
			AdminSummary:   "karma=true",
			IMHandles:      "telegram:@Lingtaidev1bot",
		},
	}

	var detailed bytes.Buffer
	printList(&detailed, procs, listOptions{Detailed: true}, true)
	detailedOut := detailed.String()
	for _, want := range []string{"ROLE", "MAIN", "IDLE", "mimo-1", "Mimo", "telegram:@Lingtaidev1bot"} {
		if !strings.Contains(detailedOut, want) {
			t.Fatalf("detailed output missing %q:\n%s", want, detailedOut)
		}
	}

	var admin bytes.Buffer
	printList(&admin, procs, listOptions{Admin: true, Detailed: true}, true)
	adminOut := admin.String()
	for _, want := range []string{"ADMIN", "karma=true", "MAIN"} {
		if !strings.Contains(adminOut, want) {
			t.Fatalf("admin output missing %q:\n%s", want, adminOut)
		}
	}
	if strings.Contains(adminOut, "HEARTBEAT") {
		t.Fatalf("admin output added HEARTBEAT column:\n%s", adminOut)
	}
}

func sortedMapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedBoolKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
