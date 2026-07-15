package tui

import (
	"strings"
	"testing"

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
