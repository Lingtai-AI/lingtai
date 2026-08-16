package tui

import (
	"fmt"
	"os"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// useMessageLimitEnv puts fs.MessageLimitEnv into the state a case describes for
// the duration of the test. "unset" is expressed by removing the variable after
// t.Setenv has registered its restore, so a value inherited from the developer's
// shell cannot make the default cases pass for the wrong reason.
func useMessageLimitEnv(t *testing.T, set bool, value string) {
	t.Helper()
	t.Setenv(fs.MessageLimitEnv, value)
	if !set {
		if err := os.Unsetenv(fs.MessageLimitEnv); err != nil {
			t.Fatalf("unset %s: %v", fs.MessageLimitEnv, err)
		}
	}
}

// TestMailboxPageLoadsTheResolvedWindow is the /mailbox caller: the page asks
// the resolver for its window rather than owning a number, so the environment
// decides how many message bodies it reads — and the zero case is proven
// against a mailbox that actually exceeds the default window, since that is the
// only way to tell "unlimited" from "the default is simply larger".
func TestMailboxPageLoadsTheResolvedWindow(t *testing.T) {
	const small = 12
	for _, tc := range []struct {
		name  string
		set   bool
		value string
		seed  int
		want  int
	}{
		{name: "unset environment loads the default window", seed: small, want: small},
		{name: "invalid value loads the default window", set: true, value: "twelve", seed: small, want: small},
		{name: "positive value bounds the page", set: true, value: "3", seed: small, want: 3},
		{
			name:  "zero loads past the default window",
			set:   true,
			value: "0",
			seed:  fs.DefaultRecentMessageLimit + 5,
			want:  fs.DefaultRecentMessageLimit + 5,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMessageLimitEnv(t, tc.set, tc.value)
			agentDir := t.TempDir()
			for i := range tc.seed {
				seedMailboxWindowLeaf(t, agentDir, "inbox", i, fmt.Sprintf("body-%04d", i))
			}

			entries := buildMailboxEntries(agentDir)

			if len(entries) != tc.want {
				t.Fatalf("loaded %d entries, want %d", len(entries), tc.want)
			}
			// The window is always the newest end of the mailbox, and it bounds
			// search because search only ever sees what was loaded.
			newest := fmt.Sprintf("body-%04d", tc.seed-1)
			if got := filterMailboxEntries(entries, newest); len(got) != 1 {
				t.Fatalf("newest message %q matched %d entries, want 1", newest, len(got))
			}
			if tc.want < tc.seed {
				if got := filterMailboxEntries(entries, "body-0000"); len(got) != 0 {
					t.Errorf("the oldest message is outside the window but still loaded (%d matches)", len(got))
				}
			}
		})
	}
}

// TestMainRenderWindowFollowsTheResolvedWindow is the Main caller: the positive
// cap bounds both the canonical event window a rebuild ingests and the render
// window it draws. The zero opt-in instead selects the full-history rebuild
// sentinel; mail_page_size still controls the initial visible batch.
func TestMainRenderWindowFollowsTheResolvedWindow(t *testing.T) {
	const pageSize, loaded = 2000, 1500
	for _, tc := range []struct {
		name        string
		set         bool
		value       string
		wantContent int
		wantVisible int
	}{
		{
			name:        "unset environment caps both halves at the default window",
			wantContent: fs.DefaultRecentMessageLimit,
			wantVisible: fs.DefaultRecentMessageLimit,
		},
		{
			name:        "invalid value caps both halves at the default window",
			set:         true,
			value:       "many",
			wantContent: fs.DefaultRecentMessageLimit,
			wantVisible: fs.DefaultRecentMessageLimit,
		},
		{
			name:        "positive value caps both halves below the configured page size",
			set:         true,
			value:       "50",
			wantContent: 50,
			wantVisible: 50,
		},
		{
			name:        "zero selects the full-history rebuild sentinel",
			set:         true,
			value:       "0",
			wantContent: 0,
			wantVisible: loaded,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMessageLimitEnv(t, tc.set, tc.value)
			m := NewMailModel(t.TempDir(), "human", t.TempDir(), "", "agent", pageSize, "", "en", false, 0)
			if m.pageSize != pageSize {
				t.Fatalf("fixture page size = %d, want the configured %d", m.pageSize, pageSize)
			}

			if got := m.contentWindow(); got != tc.wantContent {
				t.Fatalf("content window = %d, want %d", got, tc.wantContent)
			}
			m.messages = make([]ChatMessage, loaded)
			if got := len(m.visibleMessages()); got != tc.wantVisible {
				t.Fatalf("visible window = %d, want %d", got, tc.wantVisible)
			}
		})
	}
}

// TestMainInitialRebuildLoadsFullHistoryForUnlimitedWindow proves the zero opt-in
// reaches the Main session rebuild, not just the mailbox body loader. The fixture
// exceeds mail_page_size, so default and zero are observably different.
func TestMainInitialRebuildLoadsFullHistoryForUnlimitedWindow(t *testing.T) {
	const pageSize, total = 200, 405
	for _, tc := range []struct {
		name         string
		set          bool
		value        string
		wantEntries  int
		wantComplete bool
	}{
		{name: "default remains a partial page window", wantEntries: pageSize},
		{name: "zero loads the complete historical event stream", set: true, value: "0", wantEntries: total, wantComplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMessageLimitEnv(t, tc.set, tc.value)
			m := NewMailModel(t.TempDir(), "human", t.TempDir(), buildWindowedAgentDir(t, total), "agent", pageSize, "", "en", false, 0)
			rm, ok := m.initialRebuild().(mailRefreshMsg)
			if !ok || rm.sessionCache == nil {
				t.Fatalf("initialRebuild returned %#v, want a populated mailRefreshMsg", rm)
			}
			if got := rm.sessionCache.Len(); got != tc.wantEntries {
				t.Fatalf("initial session history = %d, want %d", got, tc.wantEntries)
			}
			if got := rm.sessionCache.Complete(); got != tc.wantComplete {
				t.Fatalf("initial session completeness = %v, want %v", got, tc.wantComplete)
			}
		})
	}
}

// TestDirectRevealStopsAtTheResolvedWindow is the direct caller: Ctrl+U grows
// the direct reveal horizon one page at a time and stops at the same resolved
// window, so no direct thread can page past what Main is allowed to hold. The
// starting horizon sits just under the default window, which is what makes the
// cap rather than the page size decide each outcome.
func TestDirectRevealStopsAtTheResolvedWindow(t *testing.T) {
	const pageSize, startHorizon, published = 200, 900, 1200
	target := fs.DirectTarget{
		ProjectDirectory: "/projects/window",
		Directory:        "/projects/window/.lingtai/agent-window",
		AgentID:          "agent-window",
		Address:          "window/agent-window",
	}

	for _, tc := range []struct {
		name         string
		set          bool
		value        string
		wantRevealed bool
		wantHorizon  int
	}{
		{
			name:         "unset environment stops the reveal at the default window",
			wantRevealed: true,
			wantHorizon:  fs.DefaultRecentMessageLimit,
		},
		{
			name:         "invalid value stops the reveal at the default window",
			set:          true,
			value:        "-5",
			wantRevealed: true,
			wantHorizon:  fs.DefaultRecentMessageLimit,
		},
		{
			name:         "positive value stops the reveal at that window",
			set:          true,
			value:        "950",
			wantRevealed: true,
			wantHorizon:  950,
		},
		{
			name:         "a window smaller than the current reveal refuses to grow it",
			set:          true,
			value:        "500",
			wantRevealed: false,
			wantHorizon:  startHorizon,
		},
		{
			name:         "zero lets the reveal grow by a full page",
			set:          true,
			value:        "0",
			wantRevealed: true,
			wantHorizon:  startHorizon + pageSize,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMessageLimitEnv(t, tc.set, tc.value)
			m := NewMailModel(t.TempDir(), directPerformanceHuman, t.TempDir(), "", "agent", pageSize, "", "en", false, 0)
			m.width = 80
			m.directPublication = fs.NewDirectMailPublication(
				directPerformanceHuman,
				[]fs.DirectTarget{target},
				directPagingMessages(target, published),
			)
			m.directChat.revealHorizon = startHorizon
			m.directChat.hasOlder = true
			m.directChat.viewport.GotoTop()

			revealed, ok := m.revealOlderDirectPage(target)

			if ok != tc.wantRevealed {
				t.Fatalf("reveal reported %v, want %v (horizon %d)", ok, tc.wantRevealed, revealed.directChat.revealHorizon)
			}
			if revealed.directChat.revealHorizon != tc.wantHorizon {
				t.Fatalf("reveal horizon = %d, want %d", revealed.directChat.revealHorizon, tc.wantHorizon)
			}
			if tc.wantRevealed && len(revealed.directChat.projection) != tc.wantHorizon {
				t.Fatalf("revealed projection holds %d messages, want the horizon %d",
					len(revealed.directChat.projection), tc.wantHorizon)
			}
		})
	}
}
