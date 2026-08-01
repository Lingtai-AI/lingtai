package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/process"
)

func newNirvanaRetirementTestApp(t *testing.T) App {
	t.Helper()
	a := visitTestApp(t)
	if err := process.InitProject(a.projectDir); err != nil {
		t.Fatalf("initialize Nirvana test project: %v", err)
	}
	a.currentView = appViewNirvana
	a.nirvana = NewNirvanaModel(a.projectDir)
	a.nirvana.cursor = 0
	return a
}

// startNirvanaRetirement executes the real confirmation command and returns its
// first message to the real App root. Recreating only the project directory makes
// the old implementation's eager RemoveAll observable without letting that first
// defect mask the independent queued-work retirement contracts.
func startNirvanaRetirement(t *testing.T, a App) (App, tea.Cmd) {
	t.Helper()
	updated, confirmCmd := a.nirvana.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	a.nirvana = updated
	if confirmCmd == nil {
		t.Fatal("Nirvana confirmation returned no root handoff command")
	}
	firstMsg := runCmd(confirmCmd)
	if firstMsg == nil {
		t.Fatal("Nirvana confirmation command returned no message")
	}
	if err := os.MkdirAll(a.projectDir, 0o755); err != nil {
		t.Fatalf("recreate project directory after confirmation: %v", err)
	}
	model, cleanupCmd := a.Update(firstMsg)
	return model.(App), cleanupCmd
}

func TestNirvanaConfirmHandoffLeavesProjectIntact(t *testing.T) {
	a := newNirvanaRetirementTestApp(t)
	marker := filepath.Join(a.projectDir, "handoff-marker")
	if err := os.WriteFile(marker, []byte("root must retire Mail first"), 0o644); err != nil {
		t.Fatalf("write handoff marker: %v", err)
	}

	updated, cmd := a.nirvana.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if !updated.cleaning {
		t.Fatal("confirmation did not enter the cleaning progress state")
	}
	if cmd == nil || runCmd(cmd) == nil {
		t.Fatal("confirmation did not emit a root-visible handoff message")
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("first confirmation command changed .lingtai before root retirement: %v", err)
	}
}

func TestNirvanaRetiresQueuedPersistBeforeCleanup(t *testing.T) {
	a := newNirvanaRetirementTestApp(t)
	oldGeneration := a.mail.generation
	queued := mailPersistMsg{generation: oldGeneration, sessionCache: a.mail.sessionCache}

	retired, _ := startNirvanaRetirement(t, a)
	if retired.mail.generation == oldGeneration {
		t.Error("root handoff did not synchronously retire the installed Mail generation")
	}

	sessionPath := filepath.Join(a.mail.humanDir, "logs", "session.jsonl")
	model, cmd := retired.Update(queued)
	retired = model.(App)
	if cmd != nil {
		t.Errorf("queued old-generation persist returned a command after retirement: %T", runCmd(cmd))
	}
	if retired.mail.sessionCache != a.mail.sessionCache {
		t.Fatal("retirement replaced the installed cache instead of rejecting its queued old-generation message")
	}
	if _, err := os.Stat(sessionPath); !os.IsNotExist(err) {
		t.Fatalf("queued persist resurrected %q after retirement: %v", sessionPath, err)
	}
}

func TestNirvanaDrainsDirectUnreadBeforeCleanup(t *testing.T) {
	a := newNirvanaRetirementTestApp(t)
	oldGeneration := a.mail.generation
	a.mail.directUnreadOpInFlight = true
	a.mail.directUnreadSyncPending = true
	a.mail.directUnreadOpSerial = 41

	terminal := directUnreadResultMsg{
		operation:              directUnreadSyncOperation,
		opSerial:               a.mail.directUnreadOpSerial,
		mailGeneration:         oldGeneration,
		acceptedSnapshotSerial: a.mail.acceptedSnapshotSerial,
		projectRoot:            filepath.Dir(filepath.Clean(a.mail.baseDir)),
	}

	retired, prematureCleanup := startNirvanaRetirement(t, a)
	if prematureCleanup != nil {
		t.Errorf("cleanup started while the direct-unread writer was still in flight: %T", runCmd(prematureCleanup))
	}
	if retired.mail.generation == oldGeneration {
		t.Error("root handoff did not retire the Mail generation")
	}
	if retired.mail.directUnreadSyncPending {
		t.Error("root handoff preserved a queued direct-unread continuation")
	}
	if !retired.mail.directUnreadOpInFlight || retired.mail.directUnreadOpSerial != terminal.opSerial {
		t.Errorf("root handoff invalidated the in-flight direct-unread lane: inFlight=%v serial=%d want serial=%d",
			retired.mail.directUnreadOpInFlight, retired.mail.directUnreadOpSerial, terminal.opSerial)
	}

	model, cleanupCmd := retired.Update(terminal)
	retired = model.(App)
	if retired.mail.directUnreadOpInFlight {
		t.Error("exact terminal direct-unread result did not clear the durable lane")
	}
	if retired.mail.directUnreadSyncPending {
		t.Error("exact terminal direct-unread result restarted a retired continuation")
	}
	if cleanupCmd == nil {
		t.Fatal("cleanup did not start after the exact terminal result drained the lane")
	}
	cleanupResult := runCmd(cleanupCmd)
	if _, ok := cleanupResult.(nirvanaCleanDoneMsg); !ok {
		t.Fatalf("post-drain cleanup returned %T, want nirvanaCleanDoneMsg", cleanupResult)
	}

	_, duplicateCmd := retired.Update(terminal)
	if duplicateCmd != nil {
		t.Fatalf("duplicate terminal result started cleanup twice: %T", runCmd(duplicateCmd))
	}
}

func TestNirvanaRetiresMailLoopCompanions(t *testing.T) {
	a := newNirvanaRetirementTestApp(t)
	oldGeneration := a.mail.generation
	retired, _ := startNirvanaRetirement(t, a)

	_, tickCmd := retired.mail.Update(tickMsg{generation: oldGeneration})
	if tickCmd != nil {
		t.Fatalf("old poll tick refreshed or rearmed after retirement: %T", runCmd(tickCmd))
	}
	_, pulseCmd := retired.mail.Update(pulseTickMsg{generation: oldGeneration})
	if pulseCmd != nil {
		t.Fatalf("old pulse tick rearmed after retirement: %T", runCmd(pulseCmd))
	}
}

type nirvanaLocationRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn nirvanaLocationRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func waitNirvanaSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitNirvanaCleanup(t *testing.T, result <-chan tea.Msg) tea.Msg {
	t.Helper()
	select {
	case msg := <-result:
		return msg
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for Nirvana cleanup")
		return nil
	}
}

func TestNirvanaCleanupHandoffWaitsForLocationWriter(t *testing.T) {
	root := filepath.Join(t.TempDir(), ".lingtai")
	if err := process.InitProject(root); err != nil {
		t.Fatalf("initialize project: %v", err)
	}
	humanDir := filepath.Join(root, "human")

	previousTransport := http.DefaultTransport
	requestStarted := make(chan struct{})
	releaseResolver := make(chan struct{})
	http.DefaultTransport = nirvanaLocationRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		close(requestStarted)
		<-releaseResolver
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body: io.NopCloser(strings.NewReader(`{
				"city":"Austin",
				"region":"Texas",
				"country":"US",
				"timezone":"America/Chicago",
				"loc":"30.2672,-97.7431"
			}`)),
		}, nil
	})
	t.Cleanup(func() { http.DefaultTransport = previousTransport })

	locationDone := make(chan struct{})
	go func() {
		fs.UpdateHumanLocation(humanDir)
		close(locationDone)
	}()
	waitNirvanaSignal(t, requestStarted, "blocked location resolver")

	cleanupDone := make(chan tea.Msg, 1)
	cleaner := NewNirvanaModel(root)
	go func() { cleanupDone <- runCmd(cleaner.doClean()) }()

	completedBeforeRelease := false
	var cleanupResult tea.Msg
	select {
	case cleanupResult = <-cleanupDone:
		completedBeforeRelease = true
	case <-time.After(150 * time.Millisecond):
	}

	close(releaseResolver)
	waitNirvanaSignal(t, locationDone, "location writer completion")
	if !completedBeforeRelease {
		cleanupResult = waitNirvanaCleanup(t, cleanupDone)
	}
	if _, ok := cleanupResult.(nirvanaCleanDoneMsg); !ok {
		t.Errorf("cleanup returned %T, want nirvanaCleanDoneMsg", cleanupResult)
	}
	if completedBeforeRelease {
		t.Error("Nirvana cleanup passed the destructive boundary while the location writer held the manifest mutex")
	}
	if _, err := os.Stat(root); !os.IsNotExist(err) {
		t.Fatalf("project tree survived or was resurrected after writer drain: %v", err)
	}

	if err := process.InitProject(root); err != nil {
		t.Fatalf("reinitialize project after cleanup: %v", err)
	}
	for _, relative := range []string{
		"human/.agent.json",
		"human/mailbox/inbox",
		"human/mailbox/sent",
		"human/mailbox/archive",
		"human/mailbox/contacts.json",
		".tui-asset",
		".library_shared",
	} {
		if _, err := os.Stat(filepath.Join(root, relative)); err != nil {
			t.Errorf("reinitialized project missing %s: %v", relative, err)
		}
	}
	manifestData, err := os.ReadFile(filepath.Join(humanDir, ".agent.json"))
	if err != nil {
		t.Fatalf("read reinitialized human manifest: %v", err)
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		t.Fatalf("reinitialized human manifest is invalid JSON: %v", err)
	}
	if manifest["agent_name"] != "human" || manifest["address"] != "human" {
		t.Fatalf("reinitialized human manifest = %#v", manifest)
	}
	if admin, exists := manifest["admin"]; !exists || admin != nil {
		t.Fatalf("reinitialized human admin = %#v (exists=%v), want explicit null", admin, exists)
	}
}
