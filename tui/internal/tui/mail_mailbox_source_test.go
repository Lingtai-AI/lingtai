package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func writeMailboxProjectionMessage(t *testing.T, humanDir, folder, id string, msg fs.MailMessage) {
	t.Helper()
	msg.ID = id
	msg.MailboxID = id
	data, err := json.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(humanDir, "mailbox", folder, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "message.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestMailProjectionUsesMailboxExactlyOnceBeyondEventWindow(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := buildWindowedAgentDir(t, 405)
	writeMailboxProjectionMessage(t, humanDir, "inbox", "mailbox-only-1", fs.MailMessage{
		From:       orchDir,
		To:         "human",
		Message:    "mailbox source sentinel",
		ReceivedAt: "1960-01-01T00:00:00Z", // older than the bounded 1970 event window
	})

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", 200, "", "en", false, 0)
	m, _ = m.Update(m.initialRebuild())

	matches := 0
	for _, message := range m.messages {
		if message.Type == "mail" && message.Body == "mailbox source sentinel" {
			matches++
		}
	}
	if matches != 1 {
		t.Fatalf("mailbox-backed mail appeared %d times, want exactly once; messages=%d", matches, len(m.messages))
	}

	received, err := time.Parse(time.RFC3339, "1960-01-01T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	wantTimestamp := received.Local().Format("2006-01-02 15:04 MST")
	if rendered := m.renderMessages(m.messages); !strings.Contains(rendered, wantTimestamp) {
		t.Fatalf("default mailbox projection omitted full local timestamp %q; rendered=%q", wantTimestamp, rendered)
	}
}

func TestMailProjectionKeepsExpandedEventHistoryWhileMailStaysSingleSource(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := buildWindowedAgentDir(t, 3)
	writeMailboxProjectionMessage(t, humanDir, "sent", "mailbox-only-2", fs.MailMessage{
		From:       "human",
		To:         []string{orchDir},
		Message:    "single mailbox copy",
		ReceivedAt: "2026-07-12T00:00:00Z",
	})

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", 100, "", "en", false, 0)
	m, _ = m.Update(m.initialRebuild())
	m.verbose = verboseThinking
	m.buildMessages()

	mailCount, eventCount := 0, 0
	for _, message := range m.messages {
		switch {
		case message.Type == "mail" && message.Body == "single mailbox copy":
			mailCount++
		case message.Type == "text_output":
			eventCount++
		}
	}
	if mailCount != 1 {
		t.Fatalf("expanded projection rendered mailbox mail %d times, want exactly once", mailCount)
	}
	if eventCount != 3 {
		t.Fatalf("expanded projection kept %d event entries, want 3", eventCount)
	}

	received, err := time.Parse(time.RFC3339, "2026-07-12T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	wantTimestamp := received.Local().Format("2006-01-02 15:04 MST")
	rendered := m.renderMessages(m.messages)
	if !strings.Contains(rendered, wantTimestamp) {
		t.Fatalf("expanded mailbox projection omitted default full local timestamp %q; rendered=%q", wantTimestamp, rendered)
	}
	if strings.Contains(rendered, "2026-07-12T00:00:00Z") {
		t.Fatalf("expanded mailbox projection leaked its raw RFC3339 header timestamp; rendered=%q", rendered)
	}
}

func TestMailModelOnlyUsesAcceptedMailboxSnapshotForRenderAndOlderPage(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	writeMailboxProjectionMessage(t, humanDir, "inbox", "accepted-mail", fs.MailMessage{
		From:       orchDir,
		To:         "human",
		Message:    "accepted snapshot message",
		ReceivedAt: "2026-07-22T12:00:00Z",
	})

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", 200, "", "en", false, 0)
	m, _ = m.Update(m.initialRebuild())

	writeMailboxProjectionMessage(t, humanDir, "inbox", "producer-only-mail", fs.MailMessage{
		From:       orchDir,
		To:         "human",
		Message:    "unaccepted producer message",
		ReceivedAt: "2026-07-22T12:01:00Z",
	})
	m.cache = m.cache.Refresh()
	for i := range m.cache.Messages {
		if m.cache.Messages[i].Message == "accepted snapshot message" {
			m.cache.Messages[i].Message = "mutated producer message"
		}
	}

	m.buildMessages()
	renderBodies := map[string]int{}
	for _, message := range m.messages {
		if message.Type == "mail" {
			renderBodies[message.Body]++
		}
	}
	if renderBodies["accepted snapshot message"] != 1 ||
		renderBodies["mutated producer message"] != 0 ||
		renderBodies["unaccepted producer message"] != 0 {
		t.Errorf("render projection used live producer state: bodies=%v", renderBodies)
	}

	older := m.olderPageCmd(200, m.generation).(mailOlderPageMsg)
	olderBodies := map[string]int{}
	for _, entry := range older.sessionCache.Entries() {
		if entry.Type == "mail" {
			olderBodies[entry.Body]++
		}
	}
	if olderBodies["accepted snapshot message"] != 1 ||
		olderBodies["mutated producer message"] != 0 ||
		olderBodies["unaccepted producer message"] != 0 {
		t.Errorf("older-page reconstruction used live producer state: bodies=%v", olderBodies)
	}
}
