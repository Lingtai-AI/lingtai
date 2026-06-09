package tui

import (
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/charmbracelet/x/ansi"
)

func TestPropsRenderRightShowsRunningDaemons(t *testing.T) {
	m := PropsModel{
		network: fs.Network{
			Activity: fs.NetworkActivity{
				Status:         fs.NetworkStatusDaemonActive,
				RunningDaemons: 2,
			},
		},
	}

	right := ansi.Strip(m.renderRight(80))
	if !strings.Contains(right, "Daemons: 2 running") {
		t.Fatalf("renderRight missing running daemon count:\n%s", right)
	}
}

func TestPropsRenderDetailShowsDaemonCounts(t *testing.T) {
	m := PropsModel{
		detailDaemonCounts: fs.DaemonCounts{
			Running: 1,
			Total:   3,
		},
	}

	detail := ansi.Strip(m.renderDetail())
	if !strings.Contains(detail, "running: 1") {
		t.Fatalf("renderDetail missing running daemon count:\n%s", detail)
	}
	if !strings.Contains(detail, "total: 3") {
		t.Fatalf("renderDetail missing total daemon count:\n%s", detail)
	}
}
