package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/tui"
)

// TestStartupDecision covers the R1/R2/R3 launch-mode decision table
// (tui/CONTRACT.md). Content-based (fable F7): a present-but-keyless
// config.json degrades exactly like an absent one.
func TestStartupDecision(t *testing.T) {
	cases := []struct {
		name            string
		orchestrators   int
		resolvedKeys    map[string]string
		configOK        bool
		mirrorKeys      map[string]string
		login           bool
		declaredKeyEnvs []string
		wantFirstRun    bool
		wantRecovery    bool
		wantDegraded    bool
	}{
		{
			name: "no agents → first-run",
			// R1 fail even when keys/config are healthy.
			orchestrators: 0, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: map[string]string{"DEEPSEEK_API_KEY": "x"},
			wantFirstRun: true,
		},
		{
			name: "config missing + env has keys → degraded",
			// The 2026-08-08 incident: agents keep running, mirror lost.
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: false, mirrorKeys: nil,
			wantDegraded: true,
		},
		{
			name: "config missing + no keys → recovery",
			// R2 fail: real setup.
			orchestrators: 1, resolvedKeys: map[string]string{}, configOK: false, mirrorKeys: nil,
			wantRecovery: true,
		},
		{
			name: "config present but keys mirror empty + env has keys → degraded (F7)",
			// Only legacy `language` in config.json: present-but-keyless mirror.
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: nil,
			wantDegraded: true,
		},
		{
			name:          "everything present → normal",
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: map[string]string{"DEEPSEEK_API_KEY": "x"},
		},
		{
			name:          "wizard custom declared key → normal",
			orchestrators: 1, resolvedKeys: map[string]string{"PUFFO_ATTACH_QA_DEEPSEEK_KEY": "x"}, configOK: true, mirrorKeys: map[string]string{"PUFFO_ATTACH_QA_DEEPSEEK_KEY": "x"},
			declaredKeyEnvs: []string{"PUFFO_ATTACH_QA_DEEPSEEK_KEY"},
		},
		{
			name:          "unrelated nonstandard env var → recovery",
			orchestrators: 1, resolvedKeys: map[string]string{"UNRELATED_SETTING": "x"}, configOK: true, mirrorKeys: map[string]string{"UNRELATED_SETTING": "x"},
			declaredKeyEnvs: []string{"PUFFO_ATTACH_QA_DEEPSEEK_KEY"}, wantRecovery: true,
		},
		{
			name: "env empty but mirror has keys → normal (legacy config-only)",
			// ResolveKeys fills from the mirror, so R2 is satisfied and the
			// mirror is healthy — legacy setups without .env still launch.
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: map[string]string{"DEEPSEEK_API_KEY": "x"},
		},
		{
			name: "claude-code orchestrator + no keys + keyless config.json → normal",
			// Keyless provider authenticates via the Claude Code CLI login:
			// R2 holds without any API key and there is nothing to mirror.
			orchestrators: 1, resolvedKeys: map[string]string{}, configOK: true, mirrorKeys: nil, login: true,
		},
		{
			name:          "codex OAuth orchestrator + no keys + no config.json → normal",
			orchestrators: 1, resolvedKeys: map[string]string{}, configOK: false, mirrorKeys: nil, login: true,
		},
		{
			name: "login orchestrator + env keys + keyless config.json → degraded",
			// A real key in .env still needs its R3.1 mirror.
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: nil, login: true,
			wantDegraded: true,
		},
		{
			name:          "api-key provider + no keys + keyless config.json → recovery",
			orchestrators: 1, resolvedKeys: map[string]string{}, configOK: true, mirrorKeys: nil,
			declaredKeyEnvs: []string{"DEEPSEEK_API_KEY"}, wantRecovery: true,
		},
		{
			name:          "api-key provider + env keys + keyless config.json → degraded",
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: nil,
			declaredKeyEnvs: []string{"DEEPSEEK_API_KEY"}, wantDegraded: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			firstRun, recovery, degraded := startupDecision(tc.orchestrators, tc.resolvedKeys, tc.configOK, tc.mirrorKeys, tc.login, tc.declaredKeyEnvs...)
			if firstRun != tc.wantFirstRun || recovery != tc.wantRecovery || degraded != tc.wantDegraded {
				t.Fatalf("startupDecision(%d, %v, %v, %v) = (firstRun=%v, recovery=%v, degraded=%v), want (%v, %v, %v)",
					tc.orchestrators, tc.resolvedKeys, tc.configOK, tc.mirrorKeys,
					firstRun, recovery, degraded, tc.wantFirstRun, tc.wantRecovery, tc.wantDegraded)
			}
		})
	}
}

// Exercises the disk-to-decision boundary used after a wizard commits and on
// later reopening the same project: the agent can use a valid custom key name
// even though the global suffix-only heuristic cannot recognize it alone.
func TestStartupDecisionFromWizardManifestCustomKey(t *testing.T) {
	root := t.TempDir()
	orchDir := filepath.Join(root, ".lingtai", "qa")
	globalDir := filepath.Join(root, "global")
	if err := os.MkdirAll(orchDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	const keyName = "PUFFO_ATTACH_QA_DEEPSEEK_KEY"
	if err := os.WriteFile(filepath.Join(orchDir, "init.json"), []byte(`{"manifest":{"llm":{"provider":"deepseek","api_key_env":"PUFFO_ATTACH_QA_DEEPSEEK_KEY"}}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(orchDir, ".agent.json"), []byte(`{"admin":{"karma":true}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, ".env"), []byte(keyName+"=test-only\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(`{"keys":{"PUFFO_ATTACH_QA_DEEPSEEK_KEY":"test-only"}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	orchestrators := tui.DetectOrchestrators(filepath.Join(root, ".lingtai"))
	if len(orchestrators) != 1 || orchestrators[0] != "qa" {
		t.Fatalf("detected orchestrators = %v, want [qa]", orchestrators)
	}
	declared := config.ReadAgentAPIKeyEnv(filepath.Join(root, ".lingtai", orchestrators[0]))
	if declared != keyName {
		t.Fatalf("declared key = %q, want %q", declared, keyName)
	}
	resolved, configOK := config.ResolveKeys(globalDir)
	mirror, err := config.LoadConfigReadOnly(globalDir)
	if err != nil {
		t.Fatal(err)
	}
	firstRun, recovery, degraded := startupDecision(len(orchestrators), resolved, configOK, mirror.Keys, false, declared)
	if firstRun || recovery || degraded {
		t.Fatalf("wizard handoff/reopen classified as firstRun=%v recovery=%v degraded=%v", firstRun, recovery, degraded)
	}
}

// Disk-to-decision boundary for the keyless-provider bug: a lone orchestrator
// whose manifest.llm.provider is a CLI/OAuth login family, with no .env and a
// `{}` config.json, must launch normally instead of opening the setup wizard.
// The same layout with an API-key provider still goes to recovery.
func TestStartupDecisionFromManifestLoginProvider(t *testing.T) {
	cases := []struct {
		provider     string
		wantRecovery bool
	}{
		{provider: "claude-code"},
		{provider: "claude_agent_sdk"},
		{provider: "codex"},
		{provider: "codex_oauth"},
		{provider: "deepseek", wantRecovery: true},
	}
	for _, tc := range cases {
		t.Run(tc.provider, func(t *testing.T) {
			root := t.TempDir()
			lingtaiDir := filepath.Join(root, ".lingtai")
			orchDir := filepath.Join(lingtaiDir, "orch")
			globalDir := filepath.Join(root, "global")
			if err := os.MkdirAll(orchDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(globalDir, 0o755); err != nil {
				t.Fatal(err)
			}
			initJSON := `{"manifest":{"llm":{"provider":"` + tc.provider + `","api_key_env":""}}}`
			if err := os.WriteFile(filepath.Join(orchDir, "init.json"), []byte(initJSON), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(orchDir, ".agent.json"), []byte(`{"admin":{"karma":true}}`), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(globalDir, "config.json"), []byte(`{}`), 0o600); err != nil {
				t.Fatal(err)
			}
			orchestrators := tui.DetectOrchestrators(lingtaiDir)
			declared, login := tui.OrchestratorCredentials(lingtaiDir, orchestrators)
			resolved, configOK := config.ResolveKeys(globalDir)
			mirror, err := config.LoadConfigReadOnly(globalDir)
			if err != nil {
				t.Fatal(err)
			}
			firstRun, recovery, degraded := startupDecision(len(orchestrators), resolved, configOK, mirror.Keys, login, declared...)
			if firstRun || recovery != tc.wantRecovery || degraded {
				t.Fatalf("provider %q: firstRun=%v recovery=%v degraded=%v, want recovery=%v only",
					tc.provider, firstRun, recovery, degraded, tc.wantRecovery)
			}
		})
	}
}
