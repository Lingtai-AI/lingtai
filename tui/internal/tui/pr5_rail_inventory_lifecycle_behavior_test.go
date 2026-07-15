package tui

import (
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/inventory"
)

type pr5AgentRailInventoryScannerSetter interface {
	setAgentRailInventoryScanner(func(inventory.Options) (inventory.Snapshot, error))
}

type pr5RailInventoryScanStep struct {
	snapshot inventory.Snapshot
	err      error
}

type pr5RailInventoryScanScript struct {
	steps   []pr5RailInventoryScanStep
	calls   int
	options []inventory.Options
}

func (s *pr5RailInventoryScanScript) Scan(opts inventory.Options) (inventory.Snapshot, error) {
	s.options = append(s.options, opts)
	if s.calls >= len(s.steps) {
		panic(fmt.Sprintf("unexpected agent-rail inventory scan %d", s.calls+1))
	}
	step := s.steps[s.calls]
	s.calls++
	return step.snapshot, step.err
}

func TestPR5Stage3RailInventoryLifecycleIsRootOwnedAndDropsStaleResults(t *testing.T) {
	app, _, _ := installationNewApp(t, 0)
	app, _ = installationAcceptInitial(t, app)
	setter, ok := any(&app).(pr5AgentRailInventoryScannerSetter)
	if !ok {
		t.Fatal("App has no root-owned agent-rail inventory lifecycle boundary")
	}

	root := filepath.Dir(app.projectDir)
	initial := pr5RailLifecycleSnapshot(app, "agent-a", "Agent A", 5101)
	newer := pr5RailLifecycleSnapshot(app, "agent-new", "Agent New", 5102)
	older := pr5RailLifecycleSnapshot(app, "agent-old", "Agent Old", 5103)
	visitPending := pr5RailLifecycleSnapshot(app, "agent-visit", "Agent Visit", 5104)
	home := pr5RailLifecycleSnapshot(app, "agent-home", "Agent Home", 5105)
	script := &pr5RailInventoryScanScript{steps: []pr5RailInventoryScanStep{
		{snapshot: initial},
		{snapshot: newer},
		{snapshot: older},
		{snapshot: visitPending},
		{snapshot: home},
		{err: errors.New("inventory unavailable")},
	}}
	setter.setAgentRailInventoryScanner(script.Scan)

	beforeViewRows := pr5RailLifecycleRows(app)
	for range 3 {
		_ = app.View()
	}
	if script.calls != 0 || !reflect.DeepEqual(pr5RailLifecycleRows(app), beforeViewRows) {
		t.Fatalf("View performed inventory lifecycle work: scans=%d rows=%v want scans=0 rows=%v", script.calls, pr5RailLifecycleRows(app), beforeViewRows)
	}

	initResult := pr5RunTrailingRailInventoryScan(t, app.Init(), script)
	if script.calls != 1 {
		t.Fatalf("App.Init inventory scans = %d, want exactly 1", script.calls)
	}
	if got := script.options[0].FilterDir; got != root {
		t.Fatalf("App.Init inventory FilterDir = %q, want current project root %q", got, root)
	}

	foreign, _, _ := installationNewApp(t, 0)
	foreignBefore := pr5RailLifecycleRows(foreign)
	foreign, _ = installationDeliverApp(t, foreign, initResult)
	if !reflect.DeepEqual(pr5RailLifecycleRows(foreign), foreignBefore) {
		t.Fatalf("foreign-owner result installed rows = %v, want unchanged %v", pr5RailLifecycleRows(foreign), foreignBefore)
	}

	app, _ = installationDeliverApp(t, app, initResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent A"})

	olderCmd := app.resumeProjectMail(false)
	newerCmd := app.resumeProjectMail(false)
	newerResult := pr5RunTrailingRailInventoryScan(t, newerCmd, script)
	app, _ = installationDeliverApp(t, app, newerResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent New"})

	olderResult := pr5RunTrailingRailInventoryScan(t, olderCmd, script)
	app, _ = installationDeliverApp(t, app, olderResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent New"})

	pendingBeforeVisit := app.resumeProjectMail(false)
	app.visiting = true
	visitResult := pr5RunTrailingRailInventoryScan(t, pendingBeforeVisit, script)
	app, _ = installationDeliverApp(t, app, visitResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent New"})

	beforeHiddenCalls := script.calls
	hiddenResume := app.resumeProjectMail(false)
	pr5RunTrailingNonRailCommand(t, hiddenResume, script)
	if script.calls != beforeHiddenCalls {
		t.Fatalf("visit-active home resume ran inventory scan: calls=%d want %d", script.calls, beforeHiddenCalls)
	}
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent New"})

	app.visiting = false
	homeResult := pr5RunTrailingRailInventoryScan(t, app.resumeProjectMail(false), script)
	app, _ = installationDeliverApp(t, app, homeResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent Home"})

	errorResult := pr5RunTrailingRailInventoryScan(t, app.resumeProjectMail(false), script)
	app, _ = installationDeliverApp(t, app, errorResult)
	pr5RequireRailLifecycleRows(t, app, []string{"Main", "Agent Home"})

	for i, opts := range script.options {
		if opts.FilterDir != root {
			t.Fatalf("inventory scan %d FilterDir = %q, want current project root %q", i+1, opts.FilterDir, root)
		}
	}
	if script.calls != 6 {
		t.Fatalf("total root-owned inventory scans = %d, want 6 scheduled lifecycle requests", script.calls)
	}
	beforeFinalViewCalls := script.calls
	for range 3 {
		_ = app.View()
	}
	if script.calls != beforeFinalViewCalls {
		t.Fatalf("View scans after lifecycle install = %d, want unchanged %d", script.calls, beforeFinalViewCalls)
	}
}

func pr5RailLifecycleSnapshot(app App, address, label string, pid int) inventory.Snapshot {
	root := filepath.Dir(app.projectDir)
	agentDir := filepath.Join(app.projectDir, address)
	return inventory.Snapshot{
		FilterDir: root,
		Records: []inventory.Record{{
			PID:                     pid,
			Agent:                   address,
			Project:                 root,
			AgentDir:                agentDir,
			Address:                 address,
			AgentName:               label,
			Nickname:                label,
			ManifestAddressVerified: true,
			Role:                    inventory.RoleAgent,
			Enterable:               false,
		}},
	}
}

func pr5RunTrailingRailInventoryScan(t *testing.T, cmd tea.Cmd, script *pr5RailInventoryScanScript) tea.Msg {
	t.Helper()
	raw := runCmd(cmd)
	batch, ok := raw.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("lifecycle command produced no tea.BatchMsg: %T", raw)
	}
	before := script.calls
	msg := runCmd(batch[len(batch)-1])
	if script.calls != before+1 {
		t.Fatalf("trailing lifecycle command inventory scans = %d, want %d", script.calls, before+1)
	}
	if msg == nil {
		t.Fatal("trailing inventory scan returned nil result")
	}
	return msg
}

func pr5RunTrailingNonRailCommand(t *testing.T, cmd tea.Cmd, script *pr5RailInventoryScanScript) {
	t.Helper()
	raw := runCmd(cmd)
	batch, ok := raw.(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("hidden-resume command produced no tea.BatchMsg: %T", raw)
	}
	before := script.calls
	_ = runCmd(batch[len(batch)-1])
	if script.calls != before {
		t.Fatalf("hidden-resume trailing command ran inventory scan: calls=%d want %d", script.calls, before)
	}
}

func pr5RailLifecycleRows(app App) []string {
	rows := make([]string, len(app.agentRail.rows))
	for i, row := range app.agentRail.rows {
		rows[i] = row.label
	}
	return rows
}

func pr5RequireRailLifecycleRows(t *testing.T, app App, want []string) {
	t.Helper()
	if got := pr5RailLifecycleRows(app); !reflect.DeepEqual(got, want) {
		t.Fatalf("rail rows = %v, want %v", got, want)
	}
}
