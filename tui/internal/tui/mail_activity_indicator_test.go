package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// TestStateGlyph verifies each agent state maps to its expected badge glyph,
// case-insensitively. ACTIVE uses the animated spinner frame; the rest are
// distinct static glyphs (with color carrying IDLE vs STUCK).
func TestStateGlyph(t *testing.T) {
	cases := []struct {
		state string
		want  string
	}{
		{"ACTIVE", spinnerFrames[0]},
		{"active", spinnerFrames[0]}, // case-insensitive
		{"IDLE", "◉"},
		{"idle", "◉"},
		{"STUCK", "◉"}, // color (ColorStuck) carries the distinction
		{"ASLEEP", "◌"},
		{"SUSPENDED", "○"},
		{"STALE", "◉"},
		{"REFRESHING", "⟳"},
		{"refreshing", "⟳"},
		{"", "◉"},
		{"bogus", "◉"},
	}
	for _, c := range cases {
		m := MailModel{orchState: c.state, pulseTick: 0}
		if got := m.stateGlyph(); got != c.want {
			t.Errorf("stateGlyph(%q) = %q, want %q", c.state, got, c.want)
		}
	}
}

// TestStateGlyphActiveAnimates verifies the ACTIVE spinner advances with
// pulseTick (modulo the frame count) so the badge visibly animates.
func TestStateGlyphActiveAnimates(t *testing.T) {
	for i := 0; i < len(spinnerFrames)*2+1; i++ {
		m := MailModel{orchState: "ACTIVE", pulseTick: i}
		want := spinnerFrames[i%len(spinnerFrames)]
		if got := m.stateGlyph(); got != want {
			t.Errorf("pulseTick=%d: stateGlyph() = %q, want %q", i, got, want)
		}
	}
}

// TestActiveElapsed verifies the elapsed suffix renders only while ACTIVE with
// a non-zero start time, always formatted as whole seconds so the value matches
// the Telegram Task Card footer's “active (N s)“. Offsets are mid-interval to
// avoid second/minute boundary flake.
func TestActiveElapsed(t *testing.T) {
	now := time.Now()
	cases := []struct {
		name  string
		state string
		since time.Time
		want  string
	}{
		{"not active drops timer", "idle", now.Add(-30 * time.Second), ""},
		{"active but zero start", "active", time.Time{}, ""},
		{"active seconds", "active", now.Add(-12500 * time.Millisecond), " 12s"},
		{"active minutes still seconds", "active", now.Add(-210 * time.Second), " 210s"},
	}
	for _, c := range cases {
		m := MailModel{orchState: c.state, activeSince: c.since}
		if got := m.activeElapsed(); got != c.want {
			t.Errorf("%s: activeElapsed() = %q, want %q", c.name, got, c.want)
		}
	}
}

// TestActiveSinceLifecycle verifies the activeSince timestamp is set on entry to
// ACTIVE, preserved while staying ACTIVE, and cleared on any non-ACTIVE refresh
// (including the synthesized suspended/refreshing states, which arrive as
// non-ACTIVE through the same mailRefreshMsg path). When the refresh carries a
// kernel last_api_call_at it is used as the baseline (so "active N sec" measures
// seconds since the last API call), falling back to wall-clock when absent.
func TestActiveSinceLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := NewMailModel(dir, "human", dir, dir, "orch", 20, dir, "en", false, 0)
	m = sizeMail(t, m)

	// Enter ACTIVE → timer starts (wall-clock fallback when no progress field).
	m, _ = m.Update(mailRefreshMsg{state: "active", alive: true})
	if m.activeSince.IsZero() {
		t.Fatal("activeSince should be set on entering ACTIVE")
	}
	first := m.activeSince

	// Stay ACTIVE → timestamp preserved (not reset each refresh).
	m, _ = m.Update(mailRefreshMsg{state: "active", alive: true})
	if !m.activeSince.Equal(first) {
		t.Errorf("activeSince should be preserved while staying ACTIVE; was %v, now %v", first, m.activeSince)
	}

	// Stay ACTIVE with fresh kernel progress → baseline advances to it.
	progress := time.Now().Add(-4 * time.Second)
	m, _ = m.Update(mailRefreshMsg{state: "active", alive: true, lastApiCallAt: progress})
	if !m.activeSince.Equal(progress) {
		t.Errorf("activeSince should advance to last_api_call_at; was %v, now %v", first, m.activeSince)
	}

	// Leave ACTIVE → timer cleared so the badge drops the elapsed suffix.
	m, _ = m.Update(mailRefreshMsg{state: "idle", alive: true})
	if !m.activeSince.IsZero() {
		t.Error("activeSince should be cleared when leaving ACTIVE")
	}
}

// TestResolveAgentLifecycleHeartbeatPresentation keeps the Mail badge's
// presentation boundary honest: ordinary liveness loss is explicitly stale
// with elapsed seconds, only a 120-second observed gap displays suspended, and
// a manifest-declared SUSPENDED lifecycle is never overwritten by heartbeat age.
func TestResolveAgentLifecycleHeartbeatPresentation(t *testing.T) {
	previousLang := i18n.Lang()
	if err := i18n.SetLang("en"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = i18n.SetLang(previousLang) })
	t.Setenv(fs.AgentAliveThresholdEnvVar, "")

	cases := []struct {
		name             string
		manifestState    string
		age              time.Duration
		wantAlive        bool
		wantState        string
		wantStaleSeconds int
	}{
		{
			name:          "fresh heartbeat keeps manifest lifecycle",
			manifestState: "ACTIVE",
			age:           2*time.Second + 250*time.Millisecond,
			wantAlive:     true,
			wantState:     "ACTIVE",
		},
		{
			name:             "ordinary post-threshold heartbeat is stale with seconds",
			manifestState:    "ACTIVE",
			age:              11*time.Second + 250*time.Millisecond,
			wantAlive:        false,
			wantState:        "STALE",
			wantStaleSeconds: 11,
		},
		{
			name:          "120 second gap is suspended",
			manifestState: "ACTIVE",
			age:           120*time.Second + 250*time.Millisecond,
			wantAlive:     false,
			wantState:     "SUSPENDED",
		},
		{
			name:          "manifest suspended remains suspended during ordinary gap",
			manifestState: "SUSPENDED",
			age:           11*time.Second + 250*time.Millisecond,
			wantAlive:     false,
			wantState:     "SUSPENDED",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			manifest := fmt.Sprintf(`{"agent_name":"orch","state":%q}`, tc.manifestState)
			if err := os.WriteFile(filepath.Join(dir, ".agent.json"), []byte(manifest), 0o644); err != nil {
				t.Fatalf("write manifest: %v", err)
			}
			timestamp := float64(time.Now().Add(-tc.age).UnixNano()) / 1e9
			if err := os.WriteFile(filepath.Join(dir, ".agent.heartbeat"), []byte(fmt.Sprintf("%.9f", timestamp)), 0o644); err != nil {
				t.Fatalf("write heartbeat: %v", err)
			}

			alive, lifecycle, node := resolveAgentLifecycle(dir)
			if alive != tc.wantAlive {
				t.Errorf("alive = %v, want %v", alive, tc.wantAlive)
			}
			if !strings.EqualFold(lifecycle.state, tc.wantState) {
				t.Errorf("presentation state = %q, want %q", lifecycle.state, tc.wantState)
			}
			if lifecycle.staleSeconds != tc.wantStaleSeconds {
				t.Errorf("stale seconds = %d, want %d", lifecycle.staleSeconds, tc.wantStaleSeconds)
			}
			if !strings.EqualFold(node.State, tc.manifestState) {
				t.Errorf("manifest state = %q, want %q", node.State, tc.manifestState)
			}

			m := NewMailModel(dir, "human", dir, dir, "orch", 20, dir, "en", false, 0)
			preparedMsg := m.refreshMail()
			prepared, ok := preparedMsg.(mailRefreshMsg)
			if !ok {
				t.Fatalf("refreshMail() returned %T, want mailRefreshMsg", preparedMsg)
			}
			if prepared.alive != tc.wantAlive || !strings.EqualFold(prepared.state, tc.wantState) || prepared.staleSeconds != tc.wantStaleSeconds {
				t.Errorf("prepared Mail presentation = alive:%v state:%q staleSeconds:%d, want alive:%v state:%q staleSeconds:%d", prepared.alive, prepared.state, prepared.staleSeconds, tc.wantAlive, tc.wantState, tc.wantStaleSeconds)
			}

			m = sizeMail(t, m)
			// This status-only message avoids the unrelated prepared-refresh side
			// effects while exercising the exact footer rendering payload above.
			m, _ = m.Update(mailRefreshMsg{
				alive:        prepared.alive,
				state:        prepared.state,
				staleSeconds: prepared.staleSeconds,
			})
			footer := emailToLine(m.View())
			if footer == "" {
				t.Fatal("no Email To: footer line in view")
			}
			if label := i18n.T("state." + strings.ToLower(tc.wantState)); !strings.Contains(footer, label) {
				t.Errorf("footer missing presentation label %q: %q", label, footer)
			}
			stalePresentation := fmt.Sprintf("%s %ds", i18n.T("state.stale"), tc.wantStaleSeconds)
			if tc.wantStaleSeconds > 0 {
				if !strings.Contains(footer, stalePresentation) {
					t.Errorf("footer missing stale elapsed presentation %q: %q", stalePresentation, footer)
				}
			} else if strings.Contains(footer, i18n.T("state.stale")) {
				t.Errorf("footer unexpectedly rendered ordinary stale state: %q", footer)
			}
		})
	}
}
