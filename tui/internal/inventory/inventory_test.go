package inventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/processscan"
)

func writeAgentJSON(t *testing.T, agentDir, name, state, admin string) {
	t.Helper()
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if state == "" {
		state = "IDLE"
	}
	body := `{"address":"` + name + `","agent_name":"` + name + `","nickname":"` + name + ` nick","state":"` + state + `","admin":` + admin + `}`
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeHeartbeat(t *testing.T, agentDir string, age time.Duration) {
	t.Helper()
	ts := float64(time.Now().Add(-age).UnixNano()) / 1e9
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.heartbeat"), []byte(strconvFormat(ts)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func strconvFormat(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.6f", v), "0"), ".")
}

func TestFromProcessesEnrichesSortsGroupsAndFilters(t *testing.T) {
	root := t.TempDir()
	projectA := filepath.Join(root, "Project With Spaces")
	mainDir := filepath.Join(projectA, ".lingtai", "main agent")
	workerDir := filepath.Join(projectA, ".lingtai", "worker")
	humanDir := filepath.Join(projectA, ".lingtai", "human")
	projectB := filepath.Join(root, "B")
	otherDir := filepath.Join(projectB, ".lingtai", "other")
	writeAgentJSON(t, mainDir, "main", "ACTIVE", `{"karma":true,"nirvana":false}`)
	writeAgentJSON(t, workerDir, "worker", "IDLE", `{}`)
	writeAgentJSON(t, humanDir, "human", "IDLE", `null`)
	writeAgentJSON(t, otherDir, "other", "IDLE", `{}`)
	writeHeartbeat(t, mainDir, 100*time.Millisecond)
	writeHeartbeat(t, workerDir, 5*time.Second)

	identityDir := filepath.Join(workerDir, "system", "mcp_identities")
	if err := os.MkdirAll(identityDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(identityDir, "telegram.json"), []byte(`{"mcp":"telegram","accounts":[{"username":"workerbot"}]}`), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := FromProcesses([]processscan.AgentProcess{
		{PID: 20, Uptime: "00:02:00", AgentDir: workerDir},
		{PID: 10, Uptime: "00:01:00", AgentDir: mainDir},
		{PID: 30, Uptime: "00:03:00", AgentDir: humanDir},
		{PID: 40, Uptime: "00:04:00", AgentDir: otherDir},
	}, Options{FilterDir: projectA})

	if len(snap.Records) != 2 {
		t.Fatalf("records = %d, want 2 non-human records in filter: %+v", len(snap.Records), snap.Records)
	}
	if len(snap.Groups) != 1 || snap.Groups[0].Project != projectA {
		t.Fatalf("groups not deterministic/by project: %+v", snap.Groups)
	}
	main, worker := snap.Records[0], snap.Records[1]
	if main.AgentDir != mainDir || main.Role != RoleMain || main.AdminSummary != "karma=true,nirvana=false" {
		t.Fatalf("main enrichment/sort wrong: %+v", main)
	}
	if !main.Heartbeat.Fresh {
		t.Fatalf("main heartbeat should be fresh: %+v", main.Heartbeat)
	}
	if !main.Enterable || main.EnterReason != EnterReasonNone {
		t.Fatalf("admin/orchestrator should be enterable: %+v", main)
	}
	if worker.AgentDir != workerDir || worker.Role != RoleAgent || worker.IMHandles != "telegram:@workerbot" {
		t.Fatalf("worker enrichment wrong: %+v", worker)
	}
	if worker.Heartbeat.Fresh || !worker.Heartbeat.Exists {
		t.Fatalf("worker heartbeat should be stale and visible: %+v", worker.Heartbeat)
	}
	if worker.Enterable || worker.EnterReason != EnterReasonNonAdmin {
		t.Fatalf("non-admin member should remain visible but not enterable: %+v", worker)
	}
}

func TestFromProcessesEnrichesLifecycleAndLiveContext(t *testing.T) {
	project := t.TempDir()
	agentDir := filepath.Join(project, ".lingtai", "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := `{"address":"agent","agent_name":"agent","state":"ACTIVE","admin":{"karma":true},"created_at":"2026-07-01T10:30:00Z","started_at":"2026-07-10T08:00:00Z","molt_count":7}`
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	status := `{"tokens":{"context":{"total_tokens":12345,"window_size":250000,"usage_pct":4.938}},"runtime":{"pid":77,"running":true}}`
	if err := os.WriteFile(filepath.Join(agentDir, ".status.json"), []byte(status), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := FromProcesses([]processscan.AgentProcess{{PID: 77, Uptime: "01:02:03", AgentDir: agentDir}}, Options{})
	if len(snap.Records) != 1 {
		t.Fatalf("records = %+v", snap.Records)
	}
	r := snap.Records[0]
	if r.CreatedAt != "2026-07-01T10:30:00Z" {
		t.Fatalf("creation timestamp not enriched: %+v", r)
	}
	if !r.MoltCountAvailable || r.MoltCount != 7 {
		t.Fatalf("molt count not enriched: %+v", r)
	}
	if !r.ContextAvailable || r.ContextTotalTokens != 12345 || r.ContextWindowSize != 250000 || r.ContextUsagePct != 4.938 {
		t.Fatalf("live context not enriched from status: %+v", r)
	}
}

func TestFromProcessesLeavesMissingKanbanDetailsUnavailable(t *testing.T) {
	project := t.TempDir()
	agentDir := filepath.Join(project, ".lingtai", "agent")
	writeAgentJSON(t, agentDir, "agent", "IDLE", `{}`)

	snap := FromProcesses([]processscan.AgentProcess{{PID: 7, AgentDir: agentDir}}, Options{})
	if len(snap.Records) != 1 {
		t.Fatalf("records = %+v", snap.Records)
	}
	r := snap.Records[0]
	if r.CreatedAt != "" || r.MoltCountAvailable || r.ContextAvailable {
		t.Fatalf("missing details must remain unavailable, not fabricated: %+v", r)
	}
}

func TestDuplicateProcessPrefersRuntimePID(t *testing.T) {
	project := t.TempDir()
	agentDir := filepath.Join(project, ".lingtai", "agent")
	writeAgentJSON(t, agentDir, "agent", "IDLE", `{}`)
	if err := os.WriteFile(filepath.Join(agentDir, ".status.json"), []byte(`{"runtime":{"pid":222,"running":true}}`), 0o644); err != nil {
		t.Fatal(err)
	}

	snap := FromProcesses([]processscan.AgentProcess{
		{PID: 111, AgentDir: agentDir},
		{PID: 222, AgentDir: agentDir},
	}, Options{})
	if len(snap.Records) != 1 || snap.Records[0].PID != 222 {
		t.Fatalf("duplicate collapse did not prefer .status runtime PID: %+v", snap.Records)
	}
}

func TestUnreadableAndPhantomRecordsRenderAsDisabled(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	badDir := filepath.Join(project, ".lingtai", "bad")
	if err := os.MkdirAll(badDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(badDir, ".agent.json"), []byte(`{bad json`), 0o644); err != nil {
		t.Fatal(err)
	}
	phantomDir := filepath.Join(root, "gone", ".lingtai", "ghost")

	snap := FromProcesses([]processscan.AgentProcess{
		{PID: 1, AgentDir: badDir},
		{PID: 2, AgentDir: phantomDir},
	}, Options{})

	if len(snap.Records) != 2 {
		t.Fatalf("records = %+v", snap.Records)
	}
	var bad, phantom Record
	for _, r := range snap.Records {
		if r.PID == 1 {
			bad = r
		}
		if r.PID == 2 {
			phantom = r
		}
	}
	if bad.ReadError == "" || bad.Enterable {
		t.Fatalf("bad manifest should render disabled with read error: %+v", bad)
	}
	if bad.EnterReason != EnterReasonManifestUnreadable {
		t.Fatalf("bad manifest reason = %q, want %q", bad.EnterReason, EnterReasonManifestUnreadable)
	}
	if !phantom.Phantom || phantom.Enterable {
		t.Fatalf("phantom should render disabled: %+v", phantom)
	}
	if phantom.EnterReason != EnterReasonPhantomProject {
		t.Fatalf("phantom reason = %q, want %q", phantom.EnterReason, EnterReasonPhantomProject)
	}
	if len(snap.PhantomDirs) != 1 || !strings.Contains(snap.PhantomDirs[0], "gone") {
		t.Fatalf("phantom dirs = %+v", snap.PhantomDirs)
	}
}

func TestFromProcessesCleansAgentAndFilterPaths(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	agentDir := filepath.Join(project, ".lingtai", "agent")
	writeAgentJSON(t, agentDir, "agent", "IDLE", `{}`)

	dirtyAgent := filepath.Join(project, ".", ".lingtai", "nested", "..", "agent")
	dirtyFilter := filepath.Join(project, ".")
	snap := FromProcesses([]processscan.AgentProcess{{PID: 7, AgentDir: dirtyAgent}}, Options{FilterDir: dirtyFilter})

	if len(snap.Records) != 1 {
		t.Fatalf("records = %+v, want cleaned path inside filter", snap.Records)
	}
	if snap.Records[0].AgentDir != agentDir {
		t.Fatalf("AgentDir = %q, want clean %q", snap.Records[0].AgentDir, agentDir)
	}
	if snap.Records[0].Project != project {
		t.Fatalf("Project = %q, want clean %q", snap.Records[0].Project, project)
	}
	if snap.FilterDir != project {
		t.Fatalf("FilterDir = %q, want clean %q", snap.FilterDir, project)
	}
}

func TestSnapshotJSONFixtureParity(t *testing.T) {
	project := t.TempDir()
	agentDir := filepath.Join(project, ".lingtai", "agent")
	writeAgentJSON(t, agentDir, "agent", "ASLEEP", `{}`)
	snap := FromProcesses([]processscan.AgentProcess{{PID: 7, Uptime: "00:00:07", AgentDir: agentDir}}, Options{})
	data, err := json.Marshal(snap.Records)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"PID":7`) || !strings.Contains(string(data), `"AgentName":"agent"`) {
		t.Fatalf("record JSON parity lost: %s", data)
	}
}
