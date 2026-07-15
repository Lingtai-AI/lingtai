package tui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

func TestStartupLoadingViewReusesCanonicalBodhiProgress(t *testing.T) {
	got := StartupLoadingView(80, 24)
	canonical := NirvanaModel{cleaning: true, width: 80, height: 24}.viewProgress()
	if strings.Contains(ansi.Strip(got), "Nirvana") || strings.Contains(ansi.Strip(got), "Cleaning") {
		t.Fatal("startup handoff loading view used Nirvana cleaning copy")
	}
	if !strings.Contains(ansi.Strip(got), "Loading...") {
		t.Fatal("startup handoff loading view did not use generic loading copy")
	}
	if !strings.Contains(ansi.Strip(got), "⢀⡴⠖⠚⠃") || !strings.Contains(ansi.Strip(canonical), "⢀⡴⠖⠚⠃") {
		t.Fatal("startup handoff loading view did not reuse the canonical Bodhi leaf")
	}
	if !strings.Contains(ansi.Strip(got), "⢀⡴⠖⠚⠃") {
		t.Fatal("startup handoff loading view did not render the Bodhi leaf")
	}
}

func TestStartupLoadingViewUsesActiveThemeAccent(t *testing.T) {
	original := ActiveTheme()
	t.Cleanup(func() { SetTheme(original) })
	SetThemeByName("xuan-paper")

	firstLeafLine := strings.SplitN(bodhiLeaf, "\n", 2)[0]
	accentLeaf := lipgloss.NewStyle().Foreground(ColorAccent).Render(firstLeafLine)
	agentLeaf := lipgloss.NewStyle().Foreground(ColorAgent).Render(firstLeafLine)
	if accentLeaf == agentLeaf {
		t.Fatal("test theme must distinguish accent from agent color")
	}

	loading := StartupLoadingView(80, 24)
	if !strings.Contains(loading, accentLeaf) {
		t.Fatal("startup loading Bodhi leaf did not use the active theme accent")
	}
	if strings.Contains(loading, agentLeaf) {
		t.Fatal("startup loading Bodhi leaf still used Nirvana's agent color")
	}

	nirvana := (NirvanaModel{cleaning: true, width: 80, height: 24}).viewProgress()
	if !strings.Contains(nirvana, agentLeaf) {
		t.Fatal("Nirvana Bodhi leaf no longer used its agent color")
	}
}
