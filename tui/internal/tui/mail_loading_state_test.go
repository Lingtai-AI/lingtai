package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// loadingBannerFragment is a locale-resolved substring of the initial-loading
// banner. The TUI's default locale (set in i18n.init) is "en", so under test
// this resolves to "loading...". Deriving it from i18n rather than hardcoding a
// language keeps these tests correct after the banner was split into
// locale-specific strings (English shows English, Chinese shows Chinese).
var loadingBannerFragment = i18n.T("mail.initial_loading")

// sizeMail brings a freshly constructed MailModel to the ready state by feeding
// it a window size, the same way the Bubble Tea runtime does before the first
// real frame. View() short-circuits to "app.loading" until ready, so tests that
// inspect the stream layout must size the model first.
func sizeMail(t *testing.T, m MailModel) MailModel {
	t.Helper()
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	if !updated.ready {
		t.Fatal("MailModel did not become ready after WindowSizeMsg")
	}
	return updated
}

// TestMailShowsInitialLoadingBanner verifies the loading/intermediate state:
// after construction but before the deferred initial rebuild's refresh has been
// applied, the top of the mail view shows the bilingual loading banner.
func TestMailShowsInitialLoadingBanner(t *testing.T) {
	// Under the default "en" locale the loading banner must be English-only —
	// no leftover Chinese from the old bilingual "loading... / 加载中..." string.
	for _, r := range i18n.T("mail.initial_loading") {
		if r >= 0x4e00 && r <= 0x9fff {
			t.Fatalf("en mail.initial_loading must not contain Chinese; got %q", i18n.T("mail.initial_loading"))
		}
	}

	dir := t.TempDir()
	m := NewMailModel(dir, "human@local", dir, dir, "orch", 20, dir, "en", false, 0)
	if !m.initialLoading {
		t.Fatal("NewMailModel should start with initialLoading = true")
	}

	m = sizeMail(t, m)
	if !strings.Contains(m.View(), loadingBannerFragment) {
		t.Fatalf("expected initial loading banner in first render; view did not contain %q", loadingBannerFragment)
	}
}

// TestMailLoadingBannerClearsAfterInitialRebuild verifies the banner is a
// one-time intermediate state: once the deferred initial rebuild's mailRefreshMsg
// is applied, the loading banner disappears and the rebuilt history is shown.
func TestMailLoadingBannerClearsAfterInitialRebuild(t *testing.T) {
	humanDir := t.TempDir()
	orchDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	events := strings.Join([]string{
		`{"ts":1781300000,"type":"llm_response","api_call_id":"api_one"}`,
		`{"ts":1781300001,"type":"text_output","text":"loaded history line"}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(orchDir, "logs", "events.jsonl"), []byte(events), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewMailModel(humanDir, "human", t.TempDir(), orchDir, "agent", unlimitedPageSize, "", "en", false, 0)
	m.verbose = verboseThinking
	m = sizeMail(t, m)

	// Precondition: loading banner shows before the rebuild lands.
	if !strings.Contains(m.View(), loadingBannerFragment) {
		t.Fatal("expected loading banner before initial rebuild was applied")
	}

	// Run the deferred rebuild and apply its (initial-tagged) message.
	msg := m.initialRebuild()
	rm, ok := msg.(mailRefreshMsg)
	if !ok {
		t.Fatalf("initialRebuild returned %T; expected mailRefreshMsg", msg)
	}
	if !rm.initial {
		t.Fatal("initialRebuild's mailRefreshMsg must be tagged initial=true")
	}

	m, _ = m.Update(msg)

	if m.initialLoading {
		t.Fatal("initialLoading should be false after the initial rebuild message is applied")
	}
	view := m.View()
	if strings.Contains(view, loadingBannerFragment) {
		t.Fatal("loading banner should be gone after the initial rebuild was applied")
	}

	found := false
	for _, cm := range m.messages {
		if cm.Type == "text_output" && strings.Contains(cm.Body, "loaded history line") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected rebuilt history after initial rebuild; got %d messages", len(m.messages))
	}
}

// TestMailPeriodicRefreshDoesNotReshowLoading guards against stale/re-shown
// loading state: a periodic (untagged) mailRefreshMsg must not turn the loading
// banner back on after the initial rebuild has already cleared it.
func TestMailPeriodicRefreshDoesNotReshowLoading(t *testing.T) {
	dir := t.TempDir()
	m := NewMailModel(dir, "human@local", dir, dir, "orch", 20, dir, "en", false, 0)
	m = sizeMail(t, m)

	// Apply the initial rebuild to clear loading.
	m, _ = m.Update(m.initialRebuild())
	if m.initialLoading {
		t.Fatal("loading should be cleared by the initial rebuild")
	}

	// A periodic refresh (untagged) must leave loading cleared.
	periodic := m.refreshMail()
	if rm, ok := periodic.(mailRefreshMsg); ok && rm.initial {
		t.Fatal("periodic refreshMail must not produce an initial-tagged message")
	}
	m, _ = m.Update(periodic)
	if m.initialLoading {
		t.Fatal("periodic refresh must not re-show the loading banner")
	}
	if strings.Contains(m.View(), loadingBannerFragment) {
		t.Fatal("loading banner must not reappear after a periodic refresh")
	}
}
