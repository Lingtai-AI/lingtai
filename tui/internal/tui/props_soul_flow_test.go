package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/charmbracelet/x/ansi"
)

// writeAgentWithEnv sets up an agent dir with init.json (env_file → envPath)
// and an optional .env, returning a PropsModel pointed at it.
func propsWithEnv(t *testing.T, soulDelay string, envContent *string) (PropsModel, string) {
	t.Helper()
	dir := t.TempDir()
	globalDir := filepath.Join(dir, "global")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	envPath := config.EnvFilePath(globalDir)
	if envContent != nil {
		if err := os.WriteFile(envPath, []byte(*envContent), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	soulBlock := ""
	if soulDelay != "" {
		soulBlock = fmt.Sprintf(`,"soul":{"delay":%s}`, soulDelay)
	}
	initJSON := fmt.Sprintf(`{"manifest":{"agent_name":"mimo"%s},"env_file":%q}`, soulBlock, envPath)
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte(initJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), []byte(`{"agent_name":"mimo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	return PropsModel{selectedDir: agentDir, globalDir: globalDir}, agentDir
}

func TestPropsSoulFlowDisabledByDefault(t *testing.T) {
	// No .env / no opt-in → /kanban shows "disabled" + opt-in hint, never a
	// raw delay sentinel as if it were the off switch.
	m, _ := propsWithEnv(t, "99999", nil)
	left := ansi.Strip(m.renderLeft(120))

	if !strings.Contains(left, i18n.T("props.soul_flow_disabled")) {
		t.Fatalf("expected soul flow shown as disabled:\n%s", left)
	}
	if !strings.Contains(left, config.SoulFlowEnabledEnvVar) {
		t.Fatalf("disabled hint should name the opt-in env var:\n%s", left)
	}
	if !strings.Contains(left, "soul-manual") {
		t.Fatalf("disabled hint should point at soul-manual:\n%s", left)
	}
	// The raw sentinel must NOT be presented as the status value.
	if strings.Contains(left, "99999") {
		t.Fatalf("disabled soul flow must not surface the raw delay sentinel:\n%s", left)
	}
}

func TestPropsSoulFlowEnabledShowsCadence(t *testing.T) {
	env := "LINGTAI_SOUL_FLOW_ENABLED=1\n"
	m, _ := propsWithEnv(t, "7200", &env)
	left := ansi.Strip(m.renderLeft(120))

	if !strings.Contains(left, i18n.T("props.soul_flow_enabled")) {
		t.Fatalf("expected soul flow shown as enabled:\n%s", left)
	}
	if !strings.Contains(left, "7200") {
		t.Fatalf("enabled soul flow should surface cadence 7200:\n%s", left)
	}
	if !strings.Contains(left, "soul-manual") {
		t.Fatalf("enabled soul flow should still point at soul-manual:\n%s", left)
	}
}

func TestPropsSoulFlowEnabledDefaultCadenceWhenOmitted(t *testing.T) {
	env := "LINGTAI_SOUL_FLOW_ENABLED=true\n"
	m, _ := propsWithEnv(t, "", &env) // no soul.delay in manifest
	left := ansi.Strip(m.renderLeft(120))

	if !strings.Contains(left, i18n.T("props.soul_flow_enabled")) {
		t.Fatalf("expected soul flow enabled:\n%s", left)
	}
	if !strings.Contains(left, i18n.T("props.soul_flow_cadence_default")) {
		t.Fatalf("enabled with no cadence should show default-cadence label:\n%s", left)
	}
}
