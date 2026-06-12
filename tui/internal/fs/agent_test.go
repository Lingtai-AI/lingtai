// internal/fs/agent_test.go
package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReadAgent_ValidManifest(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "alice")
	os.MkdirAll(agentDir, 0o755)

	manifest := map[string]interface{}{
		"agent_name":   "alice",
		"address":      "alice",
		"state":        "ACTIVE",
		"admin":        map[string]interface{}{"karma": true},
		"capabilities": []string{"file", "vision"},
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644)

	node, err := ReadAgent(agentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if node.AgentName != "alice" {
		t.Errorf("agent_name = %q, want %q", node.AgentName, "alice")
	}
	if node.State != "ACTIVE" {
		t.Errorf("state = %q, want %q", node.State, "ACTIVE")
	}
	if node.IsHuman {
		t.Error("is_human = true, want false")
	}
	if len(node.Capabilities) != 2 {
		t.Errorf("capabilities len = %d, want 2", len(node.Capabilities))
	}
}

func TestReadAgent_HumanAgent(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "human")
	os.MkdirAll(agentDir, 0o755)

	// admin: null → is_human = true
	manifest := map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644)

	node, err := ReadAgent(agentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !node.IsHuman {
		t.Error("is_human = false, want true (admin: null)")
	}
}

func TestReadAgent_MissingAdminKey(t *testing.T) {
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "human2")
	os.MkdirAll(agentDir, 0o755)

	// admin key absent → is_human = true
	manifest := map[string]interface{}{
		"agent_name": "human2",
		"address":    "human2",
	}
	data, _ := json.Marshal(manifest)
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644)

	node, err := ReadAgent(agentDir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !node.IsHuman {
		t.Error("is_human = false, want true (admin key absent)")
	}
}

func TestReadAgent_NoManifest(t *testing.T) {
	dir := t.TempDir()
	_, err := ReadAgent(dir)
	if err == nil {
		t.Error("expected error for missing .agent.json")
	}
}

func TestCapabilitiesForDisplay_AugmentsIntrinsics(t *testing.T) {
	// .agent.json manifest capabilities, as the kanban/props view sees them.
	manifest := []string{
		"knowledge", "skills", "bash", "avatar", "daemon", "mcp",
		"read", "write", "edit", "glob", "grep", "vision", "web_search",
	}

	got := CapabilitiesForDisplay(manifest)

	// The four intrinsic agent capabilities must be present.
	for _, want := range []string{"system", "soul", "email", "psyche"} {
		if !contains(got, want) {
			t.Errorf("CapabilitiesForDisplay() missing intrinsic %q; got %v", want, got)
		}
	}

	// Intrinsics lead, manifest capabilities follow in their original order.
	want := []string{
		"system", "soul", "email", "psyche",
		"knowledge", "skills", "bash", "avatar", "daemon", "mcp",
		"read", "write", "edit", "glob", "grep", "vision", "web_search",
	}
	if !equalSlices(got, want) {
		t.Errorf("CapabilitiesForDisplay() = %v, want %v", got, want)
	}
}

func TestCapabilitiesForDisplay_NoDuplicates(t *testing.T) {
	// A manifest that already lists some intrinsics must not get them twice.
	manifest := []string{"email", "bash", "soul", "read"}

	got := CapabilitiesForDisplay(manifest)

	seen := map[string]int{}
	for _, c := range got {
		seen[c]++
	}
	for c, n := range seen {
		if n > 1 {
			t.Errorf("capability %q appears %d times, want 1; got %v", c, n, got)
		}
	}

	// Intrinsics still lead (deduped against the manifest), then the
	// remaining manifest entries keep their original order.
	want := []string{"system", "soul", "email", "psyche", "bash", "read"}
	if !equalSlices(got, want) {
		t.Errorf("CapabilitiesForDisplay() = %v, want %v", got, want)
	}
}

func TestCapabilitiesForDisplay_EmptyManifest(t *testing.T) {
	got := CapabilitiesForDisplay(nil)
	want := []string{"system", "soul", "email", "psyche"}
	if !equalSlices(got, want) {
		t.Errorf("CapabilitiesForDisplay(nil) = %v, want %v", got, want)
	}
}

func contains(xs []string, target string) bool {
	for _, x := range xs {
		if x == target {
			return true
		}
	}
	return false
}

func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func writeInitManifestTestFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func touchInitManifestTestFile(t *testing.T, dir, rel string, mod time.Time) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func TestReadInitManifest_PrefersResolvedArtifact(t *testing.T) {
	dir := t.TempDir()
	writeInitManifestTestFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "stale", "llm": {"model": "stale-model", "provider": "stale"}}}`)
	writeInitManifestTestFile(t, dir, "system/manifest.resolved.json",
		`{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "kernel",
		  "manifest": {"agent_name": "resolved", "llm": {"model": "resolved-model", "provider": "minimax", "base_url": "https://api.example"},
		               "soul": {"delay": 7}}}`)

	m, err := ReadInitManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m["agent_name"]; got != "resolved" {
		t.Errorf("agent_name = %v, want resolved", got)
	}
	if got := m["model"]; got != "resolved-model" {
		t.Errorf("model = %v, want resolved-model", got)
	}
	if got := m["provider"]; got != "minimax" {
		t.Errorf("provider = %v, want minimax", got)
	}
	if got := m["base_url"]; got != "https://api.example" {
		t.Errorf("base_url = %v, want https://api.example", got)
	}
	if got, ok := m["soul_delay"].(float64); !ok || got != 7 {
		t.Errorf("soul_delay = %v, want 7", m["soul_delay"])
	}
}

func TestReadInitManifest_FallsBackToInitWhenArtifactAbsent(t *testing.T) {
	dir := t.TempDir()
	writeInitManifestTestFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "from-init", "llm": {"model": "init-model"}, "soul": {"delay": 3}}}`)

	m, err := ReadInitManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m["agent_name"]; got != "from-init" {
		t.Errorf("agent_name = %v, want from-init", got)
	}
	if got := m["model"]; got != "init-model" {
		t.Errorf("model = %v, want init-model", got)
	}
	if got, ok := m["soul_delay"].(float64); !ok || got != 3 {
		t.Errorf("soul_delay = %v, want 3", m["soul_delay"])
	}
}

func TestReadInitManifest_FallsBackToInitWhenArtifactMalformed(t *testing.T) {
	dir := t.TempDir()
	writeInitManifestTestFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "from-init"}}`)

	cases := map[string]string{
		"truncated JSON":      `{"schema": "lingtai.manifest.resolved/v1", "manifest": {`,
		"manifest not object": `{"schema": "lingtai.manifest.resolved/v1", "manifest": []}`,
		"missing manifest":    `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1}`,
	}
	for name, artifact := range cases {
		writeInitManifestTestFile(t, dir, "system/manifest.resolved.json", artifact)
		m, err := ReadInitManifest(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := m["agent_name"]; got != "from-init" {
			t.Errorf("%s: agent_name = %v, want from-init", name, got)
		}
	}
}

func TestReadInitManifest_FallsBackToInitWhenArtifactSchemaInvalid(t *testing.T) {
	dir := t.TempDir()
	writeInitManifestTestFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "from-init"}}`)
	cases := map[string]string{
		"wrong schema":  `{"schema": "other/v1", "schema_version": 1, "source": "kernel", "manifest": {"agent_name": "bad"}}`,
		"wrong version": `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 2, "source": "kernel", "manifest": {"agent_name": "bad"}}`,
		"wrong source":  `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "user", "manifest": {"agent_name": "bad"}}`,
	}
	for name, artifact := range cases {
		writeInitManifestTestFile(t, dir, "system/manifest.resolved.json", artifact)
		m, err := ReadInitManifest(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := m["agent_name"]; got != "from-init" {
			t.Errorf("%s: agent_name = %v, want from-init", name, got)
		}
	}
}

func TestReadInitManifest_FallsBackToInitWhenArtifactStale(t *testing.T) {
	dir := t.TempDir()
	writeInitManifestTestFile(t, dir, "system/manifest.resolved.json",
		`{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "kernel", "manifest": {"agent_name": "stale-artifact"}}`)
	writeInitManifestTestFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "fresh-init"}}`)
	base := time.Now().Add(-time.Hour)
	touchInitManifestTestFile(t, dir, "system/manifest.resolved.json", base)
	touchInitManifestTestFile(t, dir, "init.json", base.Add(time.Minute))
	m, err := ReadInitManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m["agent_name"]; got != "fresh-init" {
		t.Errorf("agent_name = %v, want fresh-init", got)
	}
}

func TestReadInitManifest_ErrorsWhenBothMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadInitManifest(dir); err == nil {
		t.Error("expected error when neither artifact nor init.json exists")
	}
}
