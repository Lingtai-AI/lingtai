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

// Pin the footer i18n keys directly so future copy edits cannot regress to
// "soul" in English or to the old 灵台 wording in localized UI. These are the
// exact lower-layer values used by MailModel.View() to render the bottom prompt.
func TestHomeHintI18nKeysUseLocalizedExpandCopy(t *testing.T) {
	cases := []struct {
		lang      string
		want      map[string]string
		forbidden []string
	}{
		{
			lang: "en",
			want: map[string]string{
				"hints.verbose":    "ctrl+o to expand",
				"hints.verbose_on": "ctrl+o to expand",
				"hints.commands":   "/ for commands",
				"hints.sep":        ", ",
			},
			forbidden: []string{"soul"},
		},
		{
			lang: "zh",
			want: map[string]string{
				"hints.verbose":    "ctrl+o 展开",
				"hints.verbose_on": "ctrl+o 展开",
				"hints.commands":   "/ 命令",
				"hints.sep":        " • ",
			},
			forbidden: []string{"soul", "灵台"},
		},
		{
			lang: "wen",
			want: map[string]string{
				"hints.verbose":    "ctrl+o 展",
				"hints.verbose_on": "ctrl+o 展",
				"hints.commands":   "/ 令",
				"hints.sep":        " • ",
			},
			forbidden: []string{"soul", "灵台"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.lang, func(t *testing.T) {
			i18n.SetLang(tc.lang)
			t.Cleanup(func() { i18n.SetLang("en") })

			for key, want := range tc.want {
				got := i18n.T(key)
				if got != want {
					t.Errorf("%s i18n[%q] = %q, want %q", tc.lang, key, got, want)
				}
				for _, bad := range tc.forbidden {
					if strings.Contains(got, bad) {
						t.Errorf("%s i18n[%q] must not contain %q; got %q", tc.lang, key, bad, got)
					}
				}
			}
		})
	}
}
