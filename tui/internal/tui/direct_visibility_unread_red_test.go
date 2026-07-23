package tui

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const directUnreadRedHuman = "project/human"

type directUnreadRedProject struct {
	root     string
	lingtai  string
	humanDir string
	targetA  fs.DirectTarget
	targetB  fs.DirectTarget
}

type directUnreadRedPersistedThread struct {
	AgentID         string   `json:"agent_id"`
	ReceivedAt      string   `json:"received_at"`
	IDsAtReceivedAt []string `json:"ids_at_received_at"`
}

func newDirectUnreadRedProject(t *testing.T, includeB bool) directUnreadRedProject {
	t.Helper()
	root := t.TempDir()
	lingtai := filepath.Join(root, ".lingtai")
	humanDir := filepath.Join(lingtai, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatalf("create human directory: %v", err)
	}

	project := directUnreadRedProject{
		root:     root,
		lingtai:  lingtai,
		humanDir: humanDir,
		targetA: fs.DirectTarget{
			ProjectDirectory: root,
			Directory:        filepath.Join(lingtai, "agent-a"),
			AgentID:          "agent-a",
			Address:          "project/agent-a",
		},
		targetB: fs.DirectTarget{
			ProjectDirectory: root,
			Directory:        filepath.Join(lingtai, "agent-b"),
			AgentID:          "agent-b",
			Address:          "project/agent-b",
		},
	}
	directUnreadRedWriteManifest(t, project.targetA, "Alpha")
	if includeB {
		directUnreadRedWriteManifest(t, project.targetB, "Bravo")
	}
	return project
}

func directUnreadRedWriteManifest(t *testing.T, target fs.DirectTarget, nickname string) {
	t.Helper()
	if err := os.MkdirAll(target.Directory, 0o755); err != nil {
		t.Fatalf("create target directory %q: %v", target.Directory, err)
	}
	body := fmt.Sprintf(`{
		"agent_id":%q,
		"agent_name":%q,
		"nickname":%q,
		"address":%q,
		"state":"STOPPED",
		"admin":{}
	}`, target.AgentID, nickname, nickname, target.Address)
	if err := os.WriteFile(filepath.Join(target.Directory, ".agent.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write target manifest %q: %v", target.AgentID, err)
	}
}

func directUnreadRedIncoming(target fs.DirectTarget, id string, at time.Time) fs.MailMessage {
	return fs.MailMessage{
		MailboxID:  id,
		From:       target.Address,
		To:         directUnreadRedHuman,
		Message:    id,
		ReceivedAt: at.UTC().Format(time.RFC3339Nano),
		Identity:   map[string]interface{}{"agent_id": target.AgentID},
		Delivered:  true,
	}
}

func directUnreadRedNewMail(project directUnreadRedProject, pageSize int) MailModel {
	mail := NewMailModel(
		project.humanDir,
		directUnreadRedHuman,
		project.lingtai,
		"",
		"Main",
		pageSize,
		"",
		"en",
		false,
		0,
	)
	mail.generation = 19
	mail, _ = mail.Update(tea.WindowSizeMsg{Width: 85, Height: 24})
	return mail
}

func directUnreadRedPublish(mail MailModel, accepted []fs.MailMessage) (MailModel, tea.Cmd) {
	cache := fs.NewMailCache(mail.humanDir)
	cache.Messages = accepted
	return mail.Update(mailRefreshMsg{generation: mail.generation, cache: cache})
}

func directUnreadRedStatePath(project directUnreadRedProject) string {
	return filepath.Join(project.lingtai, ".tui-asset", "direct-unread.json")
}

func directUnreadRedReadThreads(t *testing.T, project directUnreadRedProject) map[string]directUnreadRedPersistedThread {
	t.Helper()
	path := directUnreadRedStatePath(project)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Errorf("read durable direct unread store %q: %v", path, err)
		return nil
	}
	var state struct {
		Version int                                       `json:"version"`
		Threads map[string]directUnreadRedPersistedThread `json:"threads"`
	}
	if err := json.Unmarshal(data, &state); err != nil {
		t.Errorf("decode durable direct unread store %q: %v", path, err)
		return nil
	}
	if state.Version != 1 {
		t.Errorf("durable direct unread store %q version = %d, want 1", path, state.Version)
	}
	return state.Threads
}

func directUnreadRedSeedStore(t *testing.T, project directUnreadRedProject, targets []fs.DirectTarget) {
	t.Helper()
	t.Logf("durable DirectUnreadStore path: %s", directUnreadRedStatePath(project))
	if _, err := fs.OpenDirectUnreadStore(project.root, directUnreadRedHuman, targets, nil); err != nil {
		t.Fatalf("seed DirectUnreadStore at %q: %v", directUnreadRedStatePath(project), err)
	}
}

func directUnreadRedCount(t *testing.T, project directUnreadRedProject, targets []fs.DirectTarget, target fs.DirectTarget, accepted []fs.MailMessage) int {
	t.Helper()
	store, err := fs.OpenDirectUnreadStore(project.root, directUnreadRedHuman, targets, accepted)
	if err != nil {
		t.Errorf("reopen DirectUnreadStore at %q: %v", directUnreadRedStatePath(project), err)
		return -1
	}
	count, err := store.UnreadCount(target, accepted)
	if err != nil {
		t.Errorf("UnreadCount(%q): %v", target.AgentID, err)
		return -1
	}
	return count
}

func directUnreadRedVisibilityFromCmd(t *testing.T, cmd tea.Cmd, context string) (directVisibilityMsg, bool) {
	t.Helper()
	if cmd == nil {
		t.Errorf("%s: no deferred direct visibility command", context)
		return directVisibilityMsg{}, false
	}
	msg := runCmd(cmd)
	if visibility, ok := msg.(directVisibilityMsg); ok {
		return visibility, true
	}
	if batch, ok := msg.(tea.BatchMsg); ok {
		for _, child := range batch {
			if child == nil {
				continue
			}
			if visibility, ok := runCmd(child).(directVisibilityMsg); ok {
				return visibility, true
			}
		}
	}
	t.Errorf("%s: deferred command produced %T without directVisibilityMsg", context, msg)
	return directVisibilityMsg{}, false
}

func directUnreadRedActivate(t *testing.T, mail MailModel, agentID string) (MailModel, directVisibilityMsg, bool) {
	t.Helper()
	mail = mail.openAgentSelector()
	index := -1
	for candidate, row := range mail.agentSelector.rows {
		if agentID == "" && row.Main || !row.Main && row.Target.AgentID == agentID {
			index = candidate
			break
		}
	}
	if index < 0 {
		t.Fatalf("/agents has no row for %q", agentID)
	}
	mail, _ = mail.Update(tea.KeyPressMsg{Code: tea.KeyHome})
	for range index {
		mail, _ = mail.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	}
	var cmd tea.Cmd
	mail, cmd = mail.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if agentID == "" {
		if cmd != nil {
			t.Errorf("returning to Main produced unexpected deferred command")
		}
		return mail, directVisibilityMsg{}, false
	}
	visibility, ok := directUnreadRedVisibilityFromCmd(t, cmd, "/agents activation of "+agentID)
	return mail, visibility, ok
}

func TestDirectUnreadAcceptedPublicationSync(t *testing.T) {
	i18n.SetLang("en")
	project := newDirectUnreadRedProject(t, false)
	mail := directUnreadRedNewMail(project, 200)

	// The first accepted publication must open exactly this project's durable
	// store and baseline the safe row before any direct selection exists.
	mail, _ = directUnreadRedPublish(mail, nil)
	statePath := directUnreadRedStatePath(project)
	t.Logf("durable DirectUnreadStore path: %s", statePath)
	if _, err := os.Stat(statePath); err != nil {
		t.Errorf("first accepted publication did not open durable DirectUnreadStore %q: %v", statePath, err)
		// Seed only after recording the missing production behavior so the
		// independent full-snapshot SyncTargets assertions below still run.
		directUnreadRedSeedStore(t, project, []fs.DirectTarget{project.targetA})
	}
	firstThreads := directUnreadRedReadThreads(t, project)
	keyA := fs.DirectThreadKey(project.targetA)
	if _, ok := firstThreads[keyA]; !ok {
		t.Errorf("first accepted publication durable threads = %#v, want safe A key %q", firstThreads, keyA)
	}

	// B is discovered together with an already-accepted B message. SyncTargets
	// must receive the full snapshot and baseline B from it, while the new A mail
	// remains unread before any target has ever been selected.
	directUnreadRedWriteManifest(t, project.targetB, "Bravo")
	at := time.Date(2026, 7, 23, 12, 0, 0, 0, time.UTC)
	accepted := []fs.MailMessage{
		directUnreadRedIncoming(project.targetA, "a-new-before-selection", at),
		directUnreadRedIncoming(project.targetB, "b-present-at-discovery", at.Add(time.Second)),
	}
	mail, _ = directUnreadRedPublish(mail, accepted)
	secondThreads := directUnreadRedReadThreads(t, project)
	keyB := fs.DirectThreadKey(project.targetB)
	if _, ok := secondThreads[keyB]; !ok {
		t.Errorf("accepted publication did not SyncTargets into %q: threads=%#v, missing B key %q", statePath, secondThreads, keyB)
	} else {
		wantAt := accepted[1].ReceivedAt
		if got := secondThreads[keyB].ReceivedAt; got != wantAt {
			t.Errorf("B durable baseline received_at = %q, want full accepted snapshot boundary %q", got, wantAt)
		}
		if got, want := secondThreads[keyB].IDsAtReceivedAt, []string{accepted[1].MailboxID}; !reflect.DeepEqual(got, want) {
			t.Errorf("B durable baseline IDs = %#v, want full accepted snapshot IDs %#v", got, want)
		}
	}
	if got := directUnreadRedCount(t, project, []fs.DirectTarget{project.targetA}, project.targetA, accepted); got != 1 {
		t.Errorf("new strict A message before any selection unread = %d, want 1", got)
	}
}

func TestDirectUnreadVisibilityAcknowledgement(t *testing.T) {
	at := time.Date(2026, 7, 23, 13, 0, 0, 0, time.UTC)
	project := newDirectUnreadRedProject(t, true)
	accepted := []fs.MailMessage{directUnreadRedIncoming(project.targetA, "a-visible", at)}
	targets := []fs.DirectTarget{project.targetA, project.targetB}
	directUnreadRedSeedStore(t, project, targets)
	mail := directUnreadRedNewMail(project, 200)
	mail, _ = directUnreadRedPublish(mail, accepted)

	var visibility directVisibilityMsg
	var ok bool
	mail, visibility, ok = directUnreadRedActivate(t, mail, project.targetA.AgentID)
	if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 1 {
		t.Fatalf("/agents selection alone unread = %d, want retained 1", got)
	}
	if !ok {
		return
	}
	if !mail.ready || mail.agentSelector.selectorOpen || mail.showEditorWarn || mail.input.IsPaletteActive() {
		t.Fatalf("visibility fixture is not ready and unobscured: ready=%v selector=%v editorWarn=%v palette=%v",
			mail.ready, mail.agentSelector.selectorOpen, mail.showEditorWarn, mail.input.IsPaletteActive())
	}
	mail, _ = mail.Update(visibility)
	if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 0 {
		t.Errorf("exact current directVisibilityMsg through MailModel.Update unread = %d, want 0 after durable MarkSeen", got)
	}
}

func TestDirectUnreadRejectsStaleVisibility(t *testing.T) {
	at := time.Date(2026, 7, 23, 14, 0, 0, 0, time.UTC)

	t.Run("wrong coordinates", func(t *testing.T) {
		for _, tc := range []struct {
			name   string
			mutate func(directVisibilityMsg, directUnreadRedProject) directVisibilityMsg
		}{
			{
				name: "project",
				mutate: func(msg directVisibilityMsg, project directUnreadRedProject) directVisibilityMsg {
					msg.projectRoot = project.root + "-wrong"
					return msg
				},
			},
			{
				name: "thread",
				mutate: func(msg directVisibilityMsg, project directUnreadRedProject) directVisibilityMsg {
					msg.threadKey = fs.DirectThreadKey(project.targetB)
					return msg
				},
			},
			{
				name: "generation",
				mutate: func(msg directVisibilityMsg, _ directUnreadRedProject) directVisibilityMsg {
					msg.directGeneration++
					return msg
				},
			},
			{
				name: "accepted serial",
				mutate: func(msg directVisibilityMsg, _ directUnreadRedProject) directVisibilityMsg {
					msg.acceptedSnapshotSerial++
					return msg
				},
			},
		} {
			t.Run(tc.name, func(t *testing.T) {
				project := newDirectUnreadRedProject(t, true)
				accepted := []fs.MailMessage{directUnreadRedIncoming(project.targetA, "a-stale-"+tc.name, at)}
				targets := []fs.DirectTarget{project.targetA, project.targetB}
				directUnreadRedSeedStore(t, project, targets)
				mail := directUnreadRedNewMail(project, 200)
				mail, _ = directUnreadRedPublish(mail, accepted)
				var current directVisibilityMsg
				var ok bool
				mail, current, ok = directUnreadRedActivate(t, mail, project.targetA.AgentID)
				if !ok {
					return
				}
				before, err := os.ReadFile(directUnreadRedStatePath(project))
				if err != nil {
					t.Fatal(err)
				}
				mail, _ = mail.Update(tc.mutate(current, project))
				if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 1 {
					t.Errorf("wrong %s coordinate unread = %d, want retained 1", tc.name, got)
				}
				after, err := os.ReadFile(directUnreadRedStatePath(project))
				if err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(after, before) {
					t.Errorf("wrong %s coordinate changed durable bytes", tc.name)
				}
			})
		}
	})

	t.Run("target switch", func(t *testing.T) {
		project := newDirectUnreadRedProject(t, true)
		accepted := []fs.MailMessage{
			directUnreadRedIncoming(project.targetA, "a-before-switch", at),
			directUnreadRedIncoming(project.targetB, "b-after-switch", at.Add(time.Second)),
		}
		targets := []fs.DirectTarget{project.targetA, project.targetB}
		directUnreadRedSeedStore(t, project, targets)
		mail := directUnreadRedNewMail(project, 200)
		mail, _ = directUnreadRedPublish(mail, accepted)
		mail, staleA, ok := directUnreadRedActivate(t, mail, project.targetA.AgentID)
		if !ok {
			return
		}
		mail, _, _ = directUnreadRedActivate(t, mail, project.targetB.AgentID)
		mail, _ = mail.Update(staleA)
		if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 1 {
			t.Errorf("A coordinate made stale by target switch unread = %d, want retained 1", got)
		}
		if got := directUnreadRedCount(t, project, targets, project.targetB, accepted); got != 1 {
			t.Errorf("target switch plus stale A coordinate changed B unread to %d, want 1", got)
		}
	})

	t.Run("Main return", func(t *testing.T) {
		project := newDirectUnreadRedProject(t, true)
		accepted := []fs.MailMessage{directUnreadRedIncoming(project.targetA, "a-before-main", at)}
		targets := []fs.DirectTarget{project.targetA, project.targetB}
		directUnreadRedSeedStore(t, project, targets)
		mail := directUnreadRedNewMail(project, 200)
		mail, _ = directUnreadRedPublish(mail, accepted)
		mail, staleA, ok := directUnreadRedActivate(t, mail, project.targetA.AgentID)
		if !ok {
			return
		}
		mail, _, _ = directUnreadRedActivate(t, mail, "")
		mail, _ = mail.Update(staleA)
		if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 1 {
			t.Errorf("A coordinate made stale by Main return unread = %d, want retained 1", got)
		}
	})
}

func TestDirectUnreadObstructionRetriesVisibility(t *testing.T) {
	at := time.Date(2026, 7, 23, 15, 0, 0, 0, time.UTC)

	for _, tc := range []struct {
		name  string
		open  func(MailModel) MailModel
		close func(MailModel) (MailModel, tea.Cmd)
	}{
		{
			name: "selector overlay",
			open: func(mail MailModel) MailModel {
				return mail.openAgentSelector()
			},
			close: func(mail MailModel) (MailModel, tea.Cmd) {
				return mail.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			},
		},
		{
			name: "editor warning",
			open: func(mail MailModel) MailModel {
				mail, _ = mail.Update(OpenEditorMsg{Text: "obscured draft"})
				return mail
			},
			close: func(mail MailModel) (MailModel, tea.Cmd) {
				return mail.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			project := newDirectUnreadRedProject(t, true)
			accepted := []fs.MailMessage{directUnreadRedIncoming(project.targetA, "a-obscured-"+tc.name, at)}
			targets := []fs.DirectTarget{project.targetA, project.targetB}
			directUnreadRedSeedStore(t, project, targets)
			mail := directUnreadRedNewMail(project, 200)
			mail, _ = directUnreadRedPublish(mail, accepted)
			var current directVisibilityMsg
			var ok bool
			mail, current, ok = directUnreadRedActivate(t, mail, project.targetA.AgentID)
			if !ok {
				return
			}

			mail = tc.open(mail)
			mail, _ = mail.Update(current)
			if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 1 {
				t.Errorf("otherwise-current coordinate while %s is open unread = %d, want retained 1", tc.name, got)
			}

			var retryCmd tea.Cmd
			mail, retryCmd = tc.close(mail)
			retry, retryOK := directUnreadRedVisibilityFromCmd(t, retryCmd, "closing "+tc.name)
			if retryOK {
				if retry.projectRoot != current.projectRoot ||
					retry.threadKey != current.threadKey ||
					retry.directGeneration != current.directGeneration ||
					retry.acceptedSnapshotSerial != current.acceptedSnapshotSerial {
					t.Errorf("closing %s retry = %#v, want fresh current coordinate matching %#v", tc.name, retry, current)
				}
				mail, _ = mail.Update(retry)
			}
			if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 0 {
				t.Errorf("current visible retry after closing %s unread = %d, want 0", tc.name, got)
			}
		})
	}
}

func TestDirectUnreadMarksFullAcceptedSnapshot(t *testing.T) {
	project := newDirectUnreadRedProject(t, true)
	targets := []fs.DirectTarget{project.targetA, project.targetB}
	directUnreadRedSeedStore(t, project, targets)

	const pageSize = 200
	accepted := make([]fs.MailMessage, 0, 300)
	start := time.Date(2026, 7, 23, 16, 0, 0, 0, time.UTC)
	for index := range 300 {
		accepted = append(accepted, directUnreadRedIncoming(
			project.targetA,
			fmt.Sprintf("a-full-%03d", index),
			start.Add(time.Duration(index)*time.Second),
		))
	}
	mail := directUnreadRedNewMail(project, pageSize)
	if mail.pageSize != pageSize {
		t.Fatalf("configured page size = %d, want %d", mail.pageSize, pageSize)
	}
	mail, _ = directUnreadRedPublish(mail, accepted)
	if got := len(mail.acceptedSnapshot.messagesForUnread(mail.humanDir)); got != len(accepted) {
		t.Fatalf("accepted unread snapshot has %d messages, want full %d", got, len(accepted))
	}
	if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != len(accepted) {
		t.Fatalf("before visibility unread = %d, want all %d accepted messages", got, len(accepted))
	}

	var visibility directVisibilityMsg
	var ok bool
	mail, visibility, ok = directUnreadRedActivate(t, mail, project.targetA.AgentID)
	if !ok {
		return
	}
	mail, _ = mail.Update(visibility)
	if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 0 {
		t.Errorf("visible MarkSeen with page size %d and %d accepted messages left %d unread; want full accepted snapshot cleared",
			pageSize, len(accepted), got)
	}
}

func TestDirectUnreadMarkSeenFailureFailsClosed(t *testing.T) {
	i18n.SetLang("en")
	project := newDirectUnreadRedProject(t, true)
	targets := []fs.DirectTarget{project.targetA, project.targetB}
	directUnreadRedSeedStore(t, project, targets)
	accepted := []fs.MailMessage{directUnreadRedIncoming(
		project.targetA,
		"a-persistence-failure",
		time.Date(2026, 7, 23, 17, 0, 0, 0, time.UTC),
	)}
	mail := directUnreadRedNewMail(project, 200)
	mail, _ = directUnreadRedPublish(mail, accepted)
	var visibility directVisibilityMsg
	var ok bool
	mail, visibility, ok = directUnreadRedActivate(t, mail, project.targetA.AgentID)
	if !ok {
		return
	}
	currentBefore, currentBeforeOK := mail.currentDirectTarget()
	if !currentBeforeOK {
		t.Fatal("failure fixture has no current direct target")
	}

	statePath := directUnreadRedStatePath(project)
	before, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	// writeJSONAtomic uses statePath+".tmp"; a directory there deterministically
	// makes persistence fail without changing the durable file.
	if err := os.Mkdir(statePath+".tmp", 0o755); err != nil {
		t.Fatalf("create deterministic MarkSeen blocker: %v", err)
	}
	mail, _ = mail.Update(visibility)

	if got := directUnreadRedCount(t, project, targets, project.targetA, accepted); got != 1 {
		t.Errorf("failed visible MarkSeen unread = %d, want retained 1", got)
	}
	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Errorf("failed visible MarkSeen changed durable bytes")
	}
	if got, want := mail.statusFlash, i18n.T("agent_selector.unread_failed"); got != want {
		t.Errorf("failed visible MarkSeen status = %q, want owner-neutral %q", got, want)
	}
	currentAfter, currentAfterOK := mail.currentDirectTarget()
	if !currentAfterOK || !reflect.DeepEqual(currentAfter, currentBefore) {
		t.Errorf("failed visible MarkSeen changed current target from %#v to %#v (current=%v)", currentBefore, currentAfter, currentAfterOK)
	}
}
