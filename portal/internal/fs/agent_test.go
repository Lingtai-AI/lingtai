package fs

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func touchAgentFile(t *testing.T, dir, rel string, mod time.Time) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.Chtimes(path, mod, mod); err != nil {
		t.Fatal(err)
	}
}

func writeAgentFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	path := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadInitManifest_PrefersResolvedArtifact(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "stale", "llm": {"model": "stale-model", "provider": "stale"}}}`)
	writeAgentFile(t, dir, "system/manifest.resolved.json",
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
	// Flattening must keep working on the artifact path.
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
	writeAgentFile(t, dir, "init.json",
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
	writeAgentFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "from-init"}}`)

	cases := map[string]string{
		"truncated JSON":      `{"schema": "lingtai.manifest.resolved/v1", "manifest": {`,
		"manifest not object": `{"schema": "lingtai.manifest.resolved/v1", "manifest": []}`,
		"missing manifest":    `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1}`,
	}
	for name, artifact := range cases {
		writeAgentFile(t, dir, "system/manifest.resolved.json", artifact)
		m, err := ReadInitManifest(dir)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if got := m["agent_name"]; got != "from-init" {
			t.Errorf("%s: agent_name = %v, want from-init", name, got)
		}
	}
}

func TestReadInitManifest_ErrorsWhenBothMissing(t *testing.T) {
	dir := t.TempDir()
	if _, err := ReadInitManifest(dir); err == nil {
		t.Error("expected error when neither artifact nor init.json exists")
	}
}

func TestReadInitManifest_FallsBackToInitWhenArtifactSchemaInvalid(t *testing.T) {
	dir := t.TempDir()
	writeAgentFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "from-init"}}`)
	cases := map[string]string{
		"wrong schema":  `{"schema": "other/v1", "schema_version": 1, "source": "kernel", "manifest": {"agent_name": "bad"}}`,
		"wrong version": `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 2, "source": "kernel", "manifest": {"agent_name": "bad"}}`,
		"wrong source":  `{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "user", "manifest": {"agent_name": "bad"}}`,
	}
	for name, artifact := range cases {
		writeAgentFile(t, dir, "system/manifest.resolved.json", artifact)
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
	writeAgentFile(t, dir, "system/manifest.resolved.json",
		`{"schema": "lingtai.manifest.resolved/v1", "schema_version": 1, "source": "kernel", "manifest": {"agent_name": "stale-artifact"}}`)
	writeAgentFile(t, dir, "init.json",
		`{"manifest": {"agent_name": "fresh-init"}}`)
	base := time.Now().Add(-time.Hour)
	touchAgentFile(t, dir, "system/manifest.resolved.json", base)
	touchAgentFile(t, dir, "init.json", base.Add(time.Minute))
	m, err := ReadInitManifest(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := m["agent_name"]; got != "fresh-init" {
		t.Errorf("agent_name = %v, want fresh-init", got)
	}
}
