package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
)

// Regression guard for Jason's "没改干净，依然是soul，只改了一层" report
// (human msg #3254). PR #445 removed only the `ctrl+e editor` footer hint but
// left the lower-layer English `hints.verbose` copy reading `ctrl+o soul`. This
// test renders the real home/mail footer at the default verbosity (verboseOff,
// no Ctrl+O) in the English UI and pins the exact requested visible prompt:
//
//	ctrl+o to expand, / for commands
//
// and asserts the stale wording (`ctrl+o soul`, bare `soul`, `ctrl+e editor`) is
// gone from the visible prompt. A test like this — asserting the *positive* final
// string rather than just "the hint exists" — would have caught the half-fix.
func newEnglishHomeModel(t *testing.T, w, h int) MailModel {
	t.Helper()
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	m, _ = m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m, _ = m.Update(m.initialRebuild())
	return m
}

func TestHomeStatusHintShowsExpandCommandsCopy(t *testing.T) {
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("en") })

	m := newEnglishHomeModel(t, 100, 24)
	// Strip ANSI styling: each hint segment is rendered in its own style, so the
	// raw frame interleaves escape codes between "ctrl+o to expand" and ", / for
	// commands". The user sees one continuous string; assert against that.
	out := ansi.Strip(m.View())

	const want = "ctrl+o to expand, / for commands"
	if !strings.Contains(out, want) {
		t.Errorf("home footer must render the exact prompt %q; got:\n%s", want, out)
	}

	for _, bad := range []string{"ctrl+o soul", "ctrl+e editor"} {
		if strings.Contains(out, bad) {
			t.Errorf("home footer must not contain stale hint %q; got:\n%s", bad, out)
		}
	}
	// The bare word "soul" must not leak into the English home prompt. Guard only
	// the rendered footer line, not the whole frame, so unrelated content can't
	// false-positive — though at verboseOff the stream is empty anyway.
	if strings.Contains(out, "soul") {
		t.Errorf("home view must not contain the word \"soul\" at verboseOff; got:\n%s", out)
	}
}

// Pin the i18n keys directly so a future copy edit that reintroduces "soul" into
// the English home prompt fails loudly at the source, independent of layout.
func TestHomeHintI18nKeysHaveNoSoul(t *testing.T) {
	i18n.SetLang("en")
	t.Cleanup(func() { i18n.SetLang("en") })

	checks := map[string]string{
		"hints.verbose":    "ctrl+o to expand",
		"hints.verbose_on": "ctrl+o to expand",
		"hints.commands":   "/ for commands",
		"hints.sep":        ", ",
	}
	for key, want := range checks {
		if got := i18n.T(key); got != want {
			t.Errorf("i18n[%q] = %q, want %q", key, got, want)
		}
	}
}
