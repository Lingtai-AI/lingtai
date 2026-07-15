package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

func TestPR5Stage5VisitReturnPreservesOrdinaryRailTargetWithoutMainEscalation(t *testing.T) {
	app, targetA := pr5OrdinarySendApp(t, "agent-a", "Agent A", 7101, 7)
	beforeBinding := app.mailStore.binding
	if beforeBinding.target.policy != asyncTargetHomeAgentRail ||
		beforeBinding.target.directory != filepath.Clean(targetA) {
		t.Fatalf("precondition ordinary binding = %#v, want exact Agent A rail target", beforeBinding)
	}
	if len(app.agentRail.rows) == 0 || !app.agentRail.rows[0].originalMain {
		t.Fatalf("precondition Main row missing: %#v", app.agentRail.rows)
	}
	beforeMain := app.agentRail.rows[0].directTarget
	app.mail.pendingMessage = "ordinary A draft"
	app.mail.input.SetValue("ordinary A draft")

	visited, _ := app.enterVisitedAgent(ProjectsAgentSelectedMsg{
		Record: visitRecord(t.TempDir(), "worker", "Worker"),
	})
	if !visited.visiting || visited.visitReturn == nil || visited.suspendedHomeMailStore == nil {
		t.Fatalf(
			"visit did not suspend exact ordinary home state: visiting=%v return=%v store=%v",
			visited.visiting, visited.visitReturn != nil, visited.suspendedHomeMailStore != nil,
		)
	}

	restored, resumeCmd := visited.returnFromVisit()
	if resumeCmd == nil || restored.visiting {
		t.Fatalf("ordinary visit return: cmd=%v visiting=%v, want resume/non-visiting", resumeCmd != nil, restored.visiting)
	}
	binding := restored.mailStore.binding
	if binding.target != beforeBinding.target || binding.target.policy != asyncTargetHomeAgentRail {
		t.Fatalf("ordinary A returned as %#v, want preserved rail target %#v", binding.target, beforeBinding.target)
	}
	if restored.mail.asyncBinding != binding || restored.currentThread.target != binding.target ||
		restored.currentThread.generation != restored.mail.generation {
		t.Fatalf(
			"ordinary visit return coordinates diverged: mail=%#v store=%#v thread=%#v mailGen=%d threadGen=%d",
			restored.mail.asyncBinding, binding, restored.currentThread.target,
			restored.mail.generation, restored.currentThread.generation,
		)
	}
	if restored.mail.orchestrator != targetA || restored.mail.orchAddr != "agent-a" {
		t.Fatalf(
			"ordinary visit return identity = (%q,%q), want (%q,%q)",
			restored.mail.orchestrator, restored.mail.orchAddr, targetA, "agent-a",
		)
	}
	if len(restored.agentRail.rows) == 0 || !restored.agentRail.rows[0].originalMain ||
		restored.agentRail.rows[0].directTarget != beforeMain {
		t.Fatalf(
			"ordinary visit return overwrote synthetic Main: got=%#v want=%#v",
			restored.agentRail.rows, beforeMain,
		)
	}
	if restored.mail.pendingMessage != "ordinary A draft" || restored.mail.input.Value() != "ordinary A draft" {
		t.Fatalf(
			"ordinary visit return lost bound draft: pending=%q input=%q",
			restored.mail.pendingMessage, restored.mail.input.Value(),
		)
	}
}

func TestPR5Stage5VisitReturnStartsOrdinaryRefreshAndKeepsTickBeforeInventoryResult(t *testing.T) {
	app, targetA := pr5OrdinarySendApp(t, "agent-a", "Agent A", 7101, 7)
	binding := app.mailStore.binding
	lifecycle := app.ensureAgentRailInventoryLifecycle()
	_, requestSequence := lifecycle.schedule()
	record := inventory.Record{
		PID:                     binding.target.pid,
		Project:                 filepath.Dir(binding.owner.projectID),
		AgentDir:                targetA,
		Address:                 "agent-a",
		AgentName:               "Agent A",
		Nickname:                "Agent A",
		ManifestAddressVerified: true,
		Role:                    inventory.RoleAgent,
	}
	if !lifecycle.acceptLatest(requestSequence, binding.owner, []inventory.Record{record}, nil) {
		t.Fatal("precondition did not accept exact ordinary home inventory")
	}
	if !lifecycle.revalidateTarget(binding.owner, binding.target) {
		t.Fatal("precondition exact ordinary target is not authorized by accepted inventory")
	}
	// The shared send fixture uses an explicit always-true revalidator. Release
	// that unrelated test bypass so visit install/return binds the sole accepted
	// inventory lifecycle exactly as production does.
	app.mailStore.setAsyncTargetRevalidator(nil)
	if app.mailStore.revalidateTargetExplicit {
		t.Fatal("precondition ordinary store retained an explicit test revalidator")
	}
	app.setAgentRailInventoryScanner(func(inventory.Options) (inventory.Snapshot, error) {
		return inventory.Snapshot{}, errors.New("delayed home inventory")
	})

	visited, _ := app.enterVisitedAgent(ProjectsAgentSelectedMsg{
		Record: visitRecord(t.TempDir(), "worker", "Worker"),
	})
	if visited.suspendedHomeMailStore == nil {
		t.Fatal("visit did not retain the exact ordinary home store")
	}
	visited.suspendedHomeMailStore.pollRate = time.Nanosecond

	restored, resumeCmd := visited.returnFromVisit()
	if resumeCmd == nil {
		t.Fatal("ordinary visit return produced no resume batch")
	}
	if !restored.mailStore.refreshInFlight || !restored.mailStore.refreshInitial {
		t.Fatalf(
			"ordinary visit return did not start its initial refresh: inFlight=%v initial=%v",
			restored.mailStore.refreshInFlight, restored.mailStore.refreshInitial,
		)
	}

	var (
		refreshCount int
		tick         projectMailTickMsg
		haveTick     bool
		inventoryErr error
	)
	for _, msg := range installationRunBatch(t, resumeCmd) {
		switch msg := msg.(type) {
		case projectMailRefreshMsg:
			refreshCount++
		case projectMailTickMsg:
			tick = msg
			haveTick = true
		case agentRailInventoryResultMsg:
			inventoryErr = msg.err
		}
	}
	if refreshCount != 1 {
		t.Fatalf("ordinary visit return refresh messages=%d, want one initial refresh", refreshCount)
	}
	if !haveTick {
		t.Fatal("ordinary visit return produced no refresh tick")
	}
	if inventoryErr == nil || inventoryErr.Error() != "delayed home inventory" {
		t.Fatalf("inventory result precondition error=%v, want delayed home inventory", inventoryErr)
	}
	_, tickCmd := installationDeliverApp(t, restored, tick)
	if tickCmd == nil {
		t.Fatal("ordinary refresh tick stopped before the delayed inventory result could authorize a new owner")
	}
}

func TestPR5Stage5VisitReturnInitialRootRefreshStagesWithoutAggregatePersist(t *testing.T) {
	app, _, _ := installationNewApp(t, 0)
	targetA := filepath.Join(app.projectDir, "agent-a")
	installationWriteAgent(t, targetA, "agent-a", "Agent A", "Agent A")
	installationWriteEvents(t, targetA, 1, "event-a")

	initial := installationRefreshResult(t, &app, true)
	app, _ = installationDeliverApp(t, app, initial)
	rootSnapshot := app.mailStore.snapshot
	if rootSnapshot == nil {
		t.Fatal("precondition root refresh did not install a snapshot")
	}
	app.mailStore.setAsyncTargetRevalidator(func(asyncOwner, asyncTarget) bool { return true })
	app.threadLoads = newThreadLoadCoordinator(directThreadLoadWorker{})
	app = pr5ProjectColdDirectTarget(t, app, rootSnapshot, targetA, "Agent A", 7101, 7, "A1")
	ordinaryCache := app.mail.sessionCache
	if ordinaryCache == nil || !ordinaryCache.Complete() {
		t.Fatalf("precondition ordinary cold cache=%p complete=%v, want one complete NoPersist cache", ordinaryCache, ordinaryCache != nil && ordinaryCache.Complete())
	}

	binding := app.mailStore.binding
	lifecycle := app.ensureAgentRailInventoryLifecycle()
	_, requestSequence := lifecycle.schedule()
	record := inventory.Record{
		PID:                     binding.target.pid,
		Project:                 filepath.Dir(binding.owner.projectID),
		AgentDir:                targetA,
		Address:                 "agent-a",
		AgentName:               "Agent A",
		Nickname:                "Agent A",
		ManifestAddressVerified: true,
		Role:                    inventory.RoleAgent,
	}
	if !lifecycle.acceptLatest(requestSequence, binding.owner, []inventory.Record{record}, nil) ||
		!lifecycle.revalidateTarget(binding.owner, binding.target) {
		t.Fatal("precondition did not authorize the exact ordinary home target")
	}
	app.mailStore.setAsyncTargetRevalidator(nil)
	if app.mailStore.revalidateTargetExplicit {
		t.Fatal("precondition ordinary store retained an explicit test revalidator")
	}

	sentinelPath := filepath.Join(app.mail.humanDir, "logs", "session.jsonl")
	if err := os.MkdirAll(filepath.Dir(sentinelPath), 0o755); err != nil {
		t.Fatal(err)
	}
	const sentinel = "shared Main session sentinel\n"
	if err := os.WriteFile(sentinelPath, []byte(sentinel), 0o644); err != nil {
		t.Fatal(err)
	}
	ordinaryCache.Persist()
	if got, err := os.ReadFile(sentinelPath); err != nil || string(got) != sentinel {
		t.Fatalf("precondition ordinary cache was not NoPersist: body=%q err=%v", got, err)
	}

	visited, _ := app.enterVisitedAgent(ProjectsAgentSelectedMsg{
		Record: visitRecord(t.TempDir(), "worker", "Worker"),
	})
	restored, resumeCmd := visited.returnFromVisit()
	if resumeCmd == nil || restored.visiting || restored.mail.sessionCache != ordinaryCache {
		t.Fatalf(
			"ordinary visit return state: cmd=%v visiting=%v cache=%p/%p",
			resumeCmd != nil, restored.visiting, restored.mail.sessionCache, ordinaryCache,
		)
	}
	beforeRootVersion := restored.mailStore.version
	refresh, ok := findProjectMailRefresh(resumeCmd)
	if !ok || !refresh.mail.initial {
		t.Fatalf("ordinary visit return refresh: found=%v initial=%v", ok, refresh.mail.initial)
	}

	staged, postFrame := installationDeliverApp(t, restored, refresh)
	if staged.mailStore.version != beforeRootVersion+1 || staged.mailStore.snapshot == nil {
		t.Fatalf(
			"staged root version/snapshot = %d/%p, want %d/nonnil",
			staged.mailStore.version, staged.mailStore.snapshot, beforeRootVersion+1,
		)
	}
	if staged.mail.acceptedSnapshot != nil {
		t.Fatalf("staged root refresh published aggregate snapshot %p into ordinary projection", staged.mail.acceptedSnapshot)
	}

	var (
		persist     mailPersistMsg
		havePersist bool
		cold        threadLoadResultMsg
		haveCold    bool
	)
	for _, msg := range installationRunBatch(t, postFrame) {
		switch msg := msg.(type) {
		case mailPersistMsg:
			persist = msg
			havePersist = true
		case threadLoadResultMsg:
			cold = msg
			haveCold = true
		}
	}
	if !haveCold {
		t.Fatal("staged root refresh did not preserve the ordinary direct cold-load request")
	}
	if cold.envelope.storeVersion != staged.mailStore.version ||
		cold.envelope.target != staged.mailStore.binding.target ||
		cold.envelope.generation.thread != staged.mail.generation {
		t.Fatalf(
			"cold completion coordinates store=%d/%d target=%#v/%#v generation=%d/%d",
			cold.envelope.storeVersion, staged.mailStore.version,
			cold.envelope.target, staged.mailStore.binding.target,
			cold.envelope.generation.thread, staged.mail.generation,
		)
	}

	if havePersist {
		staged, _ = installationDeliverApp(t, staged, persist)
	}
	if got, err := os.ReadFile(sentinelPath); err != nil || string(got) != sentinel {
		t.Fatalf(
			"staged aggregate persist ran before ordinary cold completion: body=%q err=%v",
			got, err,
		)
	}
	if havePersist {
		t.Fatal("staged ordinary root refresh scheduled a MainAggregateWriter persist continuation")
	}
	if staged.mail.sessionCache != ordinaryCache {
		t.Fatalf("staged ordinary refresh installed aggregate cache %p over NoPersist cache %p", staged.mail.sessionCache, ordinaryCache)
	}

	accepted, followup := installationDeliverApp(t, staged, cold)
	if followup != nil {
		t.Fatalf("accepted ordinary cold completion returned unexpected follow-up %T", runCmd(followup))
	}
	if accepted.mail.sessionCache != cold.sessionCache ||
		accepted.currentThread.sessionCache != cold.sessionCache ||
		accepted.currentThread.acceptedSnapshotVersion != staged.mailStore.snapshot.Version() {
		t.Fatalf(
			"accepted cold state mail=%p thread=%p result=%p snapshot=%d/%d",
			accepted.mail.sessionCache, accepted.currentThread.sessionCache, cold.sessionCache,
			accepted.currentThread.acceptedSnapshotVersion, staged.mailStore.snapshot.Version(),
		)
	}
	accepted.mail.sessionCache.Persist()
	if got, err := os.ReadFile(sentinelPath); err != nil || string(got) != sentinel {
		t.Fatalf("accepted ordinary NoPersist cache changed Main session: body=%q err=%v", got, err)
	}
}

func TestPR5Stage5RenderedSyntheticMainUsesLocalizedLabel(t *testing.T) {
	t.Cleanup(func() { _ = i18n.SetLang("en") })
	if err := i18n.SetLang("zh"); err != nil {
		t.Fatal(err)
	}
	app, _, _ := installationNewApp(t, 0)
	app.mail.orchName = "Coordinator"
	if len(app.agentRail.rows) == 0 || !app.agentRail.rows[0].originalMain {
		t.Fatalf("precondition synthetic Main missing: %#v", app.agentRail.rows)
	}
	app.agentRail.installMain(app.agentRail.rows[0].directTarget)

	rendered := app.agentRail.View(24, 6)
	if !strings.Contains(rendered, i18n.T("rail.main")) {
		t.Fatalf("rendered synthetic Main=%q, want localized label %q", rendered, i18n.T("rail.main"))
	}
	if strings.Contains(rendered, "Coordinator") {
		t.Fatalf("rendered synthetic Main leaked orchestrator display name: %q", rendered)
	}
}
