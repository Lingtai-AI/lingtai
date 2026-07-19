package agentcounter

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeAgent creates <project>/.lingtai/<name> with a manifest and an
// optional heartbeat body.
func writeAgent(t *testing.T, project, name, manifest, heartbeat string) {
	t.Helper()
	dir := filepath.Join(project, ".lingtai", name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{".agent.json": manifest, ".agent.heartbeat": heartbeat}
	for base, body := range files {
		if body == "" {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, base), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func writeRegistry(t *testing.T, globalDir string, rows ...string) {
	t.Helper()
	data := []byte(strings.Join(rows, "\n") + "\n")
	if err := os.WriteFile(filepath.Join(globalDir, "registry.jsonl"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func freshHeartbeat() string {
	return fmt.Sprintf("%.3f", float64(time.Now().UnixNano())/1e9)
}

func TestCountsOnlyFreshNonHumanAgents(t *testing.T) {
	globalDir, project := t.TempDir(), t.TempDir()
	writeAgent(t, project, "runner", `{"admin":{}}`, freshHeartbeat())
	writeAgent(t, project, "human", `{"admin":null}`, freshHeartbeat())
	writeAgent(t, project, "stale", `{"admin":{}}`, "12345.0")
	writeAgent(t, project, "stopped", `{"admin":{}}`, "")
	stale := fmt.Sprintf(`{"path":%q}`, filepath.Join(t.TempDir(), "gone"))
	writeRegistry(t, globalDir, fmt.Sprintf(`{"path":%q}`, project), stale)

	got := CountRunningAgents(globalDir)
	if got.State != StateKnown || got.Agents != 1 || len(got.Issues) != 0 {
		t.Fatalf("want Known/1/no issues, got %+v", got)
	}
}

func TestMalformedEvidenceBecomesIssuesNotSilence(t *testing.T) {
	globalDir, project := t.TempDir(), t.TempDir()
	writeAgent(t, project, "runner", `{"admin":{}}`, freshHeartbeat())
	writeAgent(t, project, "badmanifest", `{not json`, freshHeartbeat())
	writeAgent(t, project, "badheartbeat", `{"admin":{}}`, "not-a-number")
	writeRegistry(t, globalDir, fmt.Sprintf(`{"path":%q}`, project), "{broken")

	got := CountRunningAgents(globalDir)
	if got.State != StateKnown || got.Agents != 1 || len(got.Issues) != 3 {
		t.Fatalf("want Known/1/3 issues, got %+v", got)
	}
}

func TestMissingRegistryIsRealZero(t *testing.T) {
	got := CountRunningAgents(t.TempDir())
	if got.State != StateKnown || got.Agents != 0 || len(got.Issues) != 0 {
		t.Fatalf("want Known/0/no issues, got %+v", got)
	}
}

func TestUnreadableRegistryIsUnknownNotZero(t *testing.T) {
	globalDir := t.TempDir()
	// A directory named registry.jsonl fails the read without ENOENT.
	if err := os.MkdirAll(filepath.Join(globalDir, "registry.jsonl"), 0o755); err != nil {
		t.Fatal(err)
	}
	got := CountRunningAgents(globalDir)
	if got.State != StateUnknown || len(got.Issues) == 0 {
		t.Fatalf("want Unknown with issue, got %+v", got)
	}
}
