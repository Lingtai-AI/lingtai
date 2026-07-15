package tui

import (
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/inventory"
)

type pr5RailInventoryInstaller interface {
	installInventory(asyncOwner, inventory.Snapshot)
}

func TestPR5Stage3RailInstallsEligibleOrdinaryRowsAndMovesCursor(t *testing.T) {
	a, _, _ := installationNewApp(t, 0)
	projectRoot := filepath.Dir(a.projectDir)
	owner := a.asyncCurrent().binding.owner
	if !validAsyncOwner(owner) {
		t.Fatalf("fixture owner = %#v, want valid root store owner", owner)
	}

	record := func(name, nickname string, pid int) inventory.Record {
		return inventory.Record{
			PID:                     pid,
			Agent:                   name,
			Project:                 projectRoot,
			AgentDir:                filepath.Join(a.projectDir, name),
			Address:                 name,
			AgentName:               "Agent " + name,
			Nickname:                nickname,
			ManifestAddressVerified: true,
			Role:                    inventory.RoleAgent,
			// Ordinary home-Agent admission is deliberately independent of the
			// cross-project Enterable policy.
			Enterable: false,
		}
	}

	snapshot := inventory.Snapshot{
		FilterDir: projectRoot,
		Records: []inventory.Record{
			record("agent-b", "Bee", 4201),
			record("agent-a", "", 4101),
			func() inventory.Record {
				r := record("orchestrator", "", 4301)
				r.IsOrchestrator = true
				return r
			}(),
			func() inventory.Record {
				r := record("phantom", "", 4401)
				r.Phantom = true
				return r
			}(),
			func() inventory.Record {
				r := record("unreadable", "", 4501)
				r.ReadError = "permission denied"
				return r
			}(),
		},
	}

	installer, ok := any(&a.agentRail).(pr5RailInventoryInstaller)
	if !ok {
		t.Fatal("AgentRailState has no typed inventory installation boundary")
	}
	installer.installInventory(owner, snapshot)

	gotLabels := make([]string, 0, len(a.agentRail.rows))
	for _, row := range a.agentRail.rows {
		gotLabels = append(gotLabels, row.label)
	}
	wantLabels := []string{a.mail.orchDisplayName(), "Bee", "Agent agent-a"}
	if !reflect.DeepEqual(gotLabels, wantLabels) {
		t.Fatalf("installed rail labels = %v, want synthetic Main plus only eligible ordinary rows %v", gotLabels, wantLabels)
	}
	if !a.agentRail.rows[0].originalMain || a.agentRail.rows[1].originalMain || a.agentRail.rows[2].originalMain {
		t.Fatalf("original-Main flags = [%v %v %v], want only row 0 synthetic Main", a.agentRail.rows[0].originalMain, a.agentRail.rows[1].originalMain, a.agentRail.rows[2].originalMain)
	}

	a = pr5UpdateRailFocusApp(t, a, tea.WindowSizeMsg{Width: 84, Height: 24})
	a = pr5UpdateRailFocusApp(t, a, tea.KeyPressMsg{Code: tea.KeyTab})
	if a.mailFocus != mailFocusRail {
		t.Fatal("precondition: Tab must focus the visible rail")
	}
	budget := a.layoutBudget()
	before := a.agentRail.View(budget.RailWidth, budget.ChildHeight, a.mail.orchDisplayName())
	a = pr5UpdateRailFocusApp(t, a, tea.KeyPressMsg{Code: tea.KeyDown})
	afterDown := a.agentRail.View(budget.RailWidth, budget.ChildHeight, a.mail.orchDisplayName())
	if afterDown == before {
		t.Fatal("Down on a focused multi-row rail did not move the visible selection from Main to the first ordinary Agent")
	}
	a = pr5UpdateRailFocusApp(t, a, tea.KeyPressMsg{Code: tea.KeyUp})
	afterUp := a.agentRail.View(budget.RailWidth, budget.ChildHeight, a.mail.orchDisplayName())
	if afterUp != before {
		t.Fatal("Up did not return the visible rail selection to synthetic Main")
	}
}
