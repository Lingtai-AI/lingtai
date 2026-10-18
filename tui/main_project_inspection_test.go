package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/process"
)

func TestAppInspectionSelectionPreservesLegacyAndDirectoryIdentity(t *testing.T) {
	observations := process.ProjectInspection{Members: []process.ProjectMember{
		{DirectoryName: ".helper", ManifestKind: "valid", Role: "orchestrator"},
		{DirectoryName: "broken", ManifestKind: "malformed", Role: "unknown"},
		{DirectoryName: "legacy-null", ManifestKind: "valid", Role: "human"},
		{DirectoryName: "main-dir", ManifestKind: "valid", Role: "orchestrator", AgentName: "Display name"},
		{DirectoryName: "worker", ManifestKind: "valid", Role: "worker"},
	}}
	agents, orchestrators := appInspectionMembers(observations)
	if !reflect.DeepEqual(agents, []string{"main-dir", "worker"}) || !reflect.DeepEqual(orchestrators, []string{"main-dir"}) {
		t.Fatalf("legacy/identity filtering changed: %v %v", agents, orchestrators)
	}
	if err := checkObservedOrchestratorInvariant(agents, orchestrators); err != nil {
		t.Fatal(err)
	}
	for _, count := range []int{0, 2} {
		dirs := []string{}
		for i := 0; i < count; i++ {
			dirs = append(dirs, "fixture")
		}
		if err := checkObservedOrchestratorInvariant(agents, dirs); err == nil {
			t.Fatalf("app exact-one policy lost for %d", count)
		}
	}
}

func TestProjectReadinessRendererPreservesOriginalRefusalProse(t *testing.T) {
	for _, tc := range []struct {
		reason                string
		agents, orchestrators []string
	}{
		{"no_agents", nil, nil}, {"no_orchestrator", []string{"worker"}, nil}, {"multiple_orchestrators", []string{"a", "b"}, []string{"a", "b"}},
	} {
		old := checkObservedOrchestratorInvariant(tc.agents, tc.orchestrators)
		got := renderProjectStartupReadiness(process.ProjectStartupReadiness{Reason: tc.reason, Agents: tc.agents, Orchestrators: tc.orchestrators})
		if old == nil || got == nil || old.Error() != got.Error() {
			t.Fatalf("%s prose/advice changed: %v / %v", tc.reason, old, got)
		}
	}
	root := t.TempDir()
	for _, name := range []string{"a", "b"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(`{"admin":{}}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(root, "a", "init.json"), 0755); err != nil {
		t.Fatal(err)
	}
	_, old := checkInitJSONInvariant(root)
	got := renderProjectStartupReadiness(process.ProjectStartupReadiness{Reason: "mixed_init", InitEvidence: []process.ProjectInitEvidence{{DirectoryName: "a", Presence: "present"}, {DirectoryName: "b", Presence: "absent"}}})
	if old == nil || got == nil || old.Error() != got.Error() {
		t.Fatalf("mixed prose/advice changed: %v / %v", old, got)
	}
}
