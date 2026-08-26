package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/charmbracelet/x/ansi"
)

func propsWithConfiguredSoulDelay(t *testing.T, soulDelay string) PropsModel {
	t.Helper()
	dir := t.TempDir()
	agentDir := filepath.Join(dir, "agent")
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	soulBlock := ""
	if soulDelay != "" {
		soulBlock = fmt.Sprintf(`,"soul":{"delay":%s}`, soulDelay)
	}
	initJSON := fmt.Sprintf(`{"manifest":{"agent_name":"mimo"%s}}`, soulBlock)
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte(initJSON), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), []byte(`{"agent_name":"mimo"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	m := PropsModel{selectedDir: agentDir, orchDir: agentDir, globalDir: filepath.Join(dir, "global")}
	m.applySnapshot(m.loadSnapshot().(propsLoadMsg))
	return m
}

func TestPropsSoulFlowShowsConfiguredValueWithoutStateProbe(t *testing.T) {
	m := propsWithConfiguredSoulDelay(t, "7200")
	out := ansi.Strip(m.renderDetail())
	if !strings.Contains(out, i18n.T("props.soul_flow")+": 7200") {
		t.Fatalf("configured soul delay missing:\n%s", out)
	}
	for _, healthClaim := range []string{
		i18n.T("props.soul_flow_enabled"),
		i18n.T("props.soul_flow_disabled"),
		"LINGTAI_SOUL_FLOW_ENABLED",
		"soul-manual",
	} {
		if healthClaim != "" && strings.Contains(out, healthClaim) {
			t.Fatalf("Kanban probed or claimed soul-flow state %q:\n%s", healthClaim, out)
		}
	}
}

func TestPropsSoulFlowOmitsUnconfiguredValue(t *testing.T) {
	m := propsWithConfiguredSoulDelay(t, "")
	out := ansi.Strip(m.renderDetail())
	if strings.Contains(out, i18n.T("props.soul_flow")+":") {
		t.Fatalf("unconfigured soul flow should be omitted, not probed:\n%s", out)
	}
}
