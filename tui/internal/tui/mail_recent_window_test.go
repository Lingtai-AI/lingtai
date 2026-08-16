package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const recentWindowExtra = 40

// seedRecentWindowMailbox writes n inbox messages with kernel-shaped sortable
// mailbox ids (so directory order is chronological order, as on disk) and
// returns their ids oldest-first.
func seedRecentWindowMailbox(t *testing.T, humanDir string, orchAddr string, n int) []string {
	t.Helper()
	parent := filepath.Join(humanDir, "mailbox", "inbox")
	if err := os.MkdirAll(parent, 0o755); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, 0, n)
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range n {
		stamp := base.Add(time.Duration(i) * time.Minute)
		id := fmt.Sprintf("%s-%04x", stamp.Format("20060102T150405"), i%0x10000)
		msg := fs.MailMessage{
			ID:         id,
			MailboxID:  id,
			From:       orchAddr,
			To:         "human",
			Message:    fmt.Sprintf("mail body %d", i),
			ReceivedAt: stamp.Format(time.RFC3339Nano),
			Delivered:  true,
		}
		data, err := json.Marshal(msg)
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(parent, id)
		if err := os.Mkdir(dir, 0o755); err != nil && !os.IsExist(err) {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "message.json"), data, 0o644); err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestMailRecentMessageWindowContract is the human contract for the Main page,
// exercised against one mailbox holding more than the cap. Seeding that mailbox
// is the expensive part, so every clause shares it.
func TestMailRecentMessageWindowContract(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	ids := seedRecentWindowMailbox(t, humanDir, orchDir, fs.RecentMessageLimit()+recentWindowExtra)
	newModel := func() MailModel {
		return NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", 200, "", "en", false, 0)
	}

	// The page loads exactly the newest RecentMessageLimit() entries, and the cap
	// is observable in the loaded snapshot — not applied when rendering over a
	// fully loaded mailbox.
	t.Run("loads only the newest window", func(t *testing.T) {
		m := newModel()
		m, _ = m.Update(m.initialRebuild())

		if got := len(m.messages); got != fs.RecentMessageLimit() {
			t.Fatalf("loaded %d entries, want exactly the newest %d", got, fs.RecentMessageLimit())
		}
		if got := len(m.acceptedSnapshot.cacheCopy(humanDir).Messages); got != fs.RecentMessageLimit() {
			t.Fatalf("accepted mailbox snapshot holds %d messages, want the cap %d", got, fs.RecentMessageLimit())
		}
		dropped := map[string]bool{}
		for _, id := range ids[:recentWindowExtra] {
			dropped[id] = true
		}
		for _, msg := range m.messages {
			if dropped[msg.Timestamp] {
				t.Fatalf("entry %q older than the window survived into the loaded page", msg.Timestamp)
			}
		}
		wantNewest := fmt.Sprintf("mail body %d", fs.RecentMessageLimit()+recentWindowExtra-1)
		if !strings.Contains(m.messages[len(m.messages)-1].Body, wantNewest) {
			t.Fatalf("newest message missing from the loaded page; last body = %q", m.messages[len(m.messages)-1].Body)
		}
	})

	// Trimming to the newest window must not reorder what survives: newest-first
	// selection, chronological display, newest entry last.
	t.Run("window stays chronological after trimming", func(t *testing.T) {
		m := newModel()
		m, _ = m.Update(m.initialRebuild())

		if len(m.messages) != fs.RecentMessageLimit() {
			t.Fatalf("loaded %d entries, want %d", len(m.messages), fs.RecentMessageLimit())
		}
		for i := 1; i < len(m.messages); i++ {
			if chatMessageBefore(m.messages[i], m.messages[i-1]) {
				t.Fatalf("trimmed window is out of order at index %d: %q precedes %q",
					i, m.messages[i].Timestamp, m.messages[i-1].Timestamp)
			}
		}
		visible := m.visibleMessages()
		if len(visible) != 200 {
			t.Fatalf("render window = %d, want the configured page size 200", len(visible))
		}
		if visible[len(visible)-1].Timestamp != m.messages[len(m.messages)-1].Timestamp {
			t.Fatal("render window is not anchored at the newest loaded entry")
		}
	})

	// The older/total banner and its string are gone, so the page cannot display
	// a number it would have to scan the whole mailbox to compute.
	t.Run("never announces a total message count", func(t *testing.T) {
		if got := i18n.T("mail.load_more"); got != "mail.load_more" {
			t.Fatalf("stale total-count banner string is still defined: %q", got)
		}
		m := sizeMail(t, newModel())
		m, _ = m.Update(m.initialRebuild())

		if m.initialLoading {
			t.Fatal("the accepted initial rebuild must clear the one-time loading state outright")
		}
		if m.bannerLineCount() != 0 {
			t.Fatalf("banner rows reserved after load = %d, want 0 (no older-history banner exists)", m.bannerLineCount())
		}
		view := m.View()
		// "older" alone would match the temp-directory path in the status bar, so
		// match the banner's own distinctive tail plus the loading line it replaced.
		for _, forbidden := range []string{"ctrl+u to load", "加载", loadingBannerFragment} {
			if strings.Contains(view, forbidden) {
				t.Fatalf("loaded view still shows history-count chrome %q:\n%s", forbidden, view)
			}
		}
	})

	// Live conversation still works: an ordinary refresh delivers a new message
	// and the page stays capped. This clause writes to the shared mailbox, so it
	// runs last.
	t.Run("new message still arrives under the window", func(t *testing.T) {
		m := sizeMail(t, newModel())
		m, _ = m.Update(m.initialRebuild())

		arrival := time.Date(2027, 3, 4, 5, 6, 7, 0, time.UTC)
		seedRecentWindowNewArrival(t, humanDir, orchDir, arrival)

		var cmd tea.Cmd
		m, cmd = m.issueRefreshRequest()
		if cmd == nil {
			t.Fatal("refresh request produced no command")
		}
		m, _ = m.Update(cmd())

		if len(m.messages) != fs.RecentMessageLimit() {
			t.Fatalf("after refresh the page holds %d entries, want the cap %d", len(m.messages), fs.RecentMessageLimit())
		}
		if !strings.Contains(m.messages[len(m.messages)-1].Body, "brand new message") {
			t.Fatalf("new message did not land at the tail of the page; last body = %q", m.messages[len(m.messages)-1].Body)
		}
	})
}

func seedRecentWindowNewArrival(t *testing.T, humanDir, orchAddr string, at time.Time) {
	t.Helper()
	writeMailboxProjectionMessage(t, humanDir, "inbox", at.Format("20060102T150405")+"-ffff", fs.MailMessage{
		From:       orchAddr,
		To:         "human",
		Message:    "brand new message",
		ReceivedAt: at.Format(time.RFC3339Nano),
		Delivered:  true,
	})
}
