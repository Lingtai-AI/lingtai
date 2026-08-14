package tui

// Home async-work stats row.
//
// One muted line BELOW the input box and ABOVE the bottom path/shortcut status
// bar that shows the orchestrator agent's async job ledger at a glance, mirroring
// the Telegram Task Card's daemon counts ("N running · M queued · K done · J
// failed"). It is a sibling of the home telemetry row (home_telemetry.go) and
// follows the same contract: background tea.Cmd I/O, snapshot cached on the
// model, never touched on View/Update hot paths, and the row's height is reserved
// through the same mailFooterHeight budget so the status bar can never be
// clipped by the additive line.
//
// Data source: <agentDir>/daemons/<run-id>/daemon.json state files — the async
// job ledger. fs.CountDaemons() reads those state files cheaply (one JSON per
// run directory, no ledger/event/chat/result I/O), which is the same source the
// /daemons view and props.go detail counts use.

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// homeAsyncStatsTTL is how long a fetched async-stats snapshot is considered
// fresh, matching the home telemetry TTL so the two additive rows refresh on the
// same cadence without doubling disk reads.
const homeAsyncStatsTTL = 1 * time.Second

// homeAsyncStats is the resolved, cached snapshot of the orchestrator agent's
// async job ledger counts. A zero value means "no data yet / nothing to show".
type homeAsyncStats struct {
	running int
	queued  int
	done    int
	failed  int
}

// hasData reports whether any count is present. With nothing to show the caller
// omits the whole row.
func (s homeAsyncStats) hasData() bool {
	return s.running > 0 || s.queued > 0 || s.done > 0 || s.failed > 0
}

// hasHomeAsyncStats reports whether View() will render the additive async row.
// It is the single predicate shared by the height budget (syncViewportHeight)
// and the renderer (View), mirroring hasHomeTelemetry so they can never disagree
// about whether the row occupies a line.
func (m MailModel) hasHomeAsyncStats() bool {
	return m.homeAsyncStatsLoaded && m.homeAsyncStats.hasData()
}

// fetchHomeAsyncStats returns the background command that reads the async job
// ledger snapshot. It runs off the render/input path: os.ReadDir over the
// daemons directory can stall on a slow volume, so like fetchHomeTelemetry it is
// never invoked synchronously from View()/Update().
func (m MailModel) fetchHomeAsyncStats() tea.Cmd {
	return func() tea.Msg {
		counts := fs.CountDaemons(m.orchestrator)
		return homeAsyncStatsMsg{
			generation: m.generation,
			t: homeAsyncStats{
				running: counts.Running,
				queued:  counts.Queued,
				done:    counts.Done,
				failed:  counts.Failed,
			},
		}
	}
}

// maybeScheduleHomeAsyncStats gates a background fetch by in-flight + TTL so the
// poll loop does not spawn a new read every tick. The first fetch is always
// allowed; subsequent fetches wait homeAsyncStatsTTL since the last completion.
// Returns nil when no fetch should start.
func (m MailModel) maybeScheduleHomeAsyncStats(now time.Time) tea.Cmd {
	if m.homeAsyncStatsInFlight {
		return nil
	}
	if !m.homeAsyncStatsLastFetch.IsZero() && now.Sub(m.homeAsyncStatsLastFetch) < homeAsyncStatsTTL {
		return nil
	}
	return m.fetchHomeAsyncStats()
}

// applyHomeAsyncStats stores a completed snapshot and returns whether the row's
// visibility flipped (data ⇄ no-data), so the viewport height is re-synced only
// when the layout actually changes.
func (m *MailModel) applyHomeAsyncStats(t homeAsyncStats, now time.Time) bool {
	wasVisible := m.homeAsyncStatsLoaded && m.homeAsyncStats.hasData()
	m.homeAsyncStats = t
	m.homeAsyncStatsLoaded = true
	m.homeAsyncStatsLastFetch = now
	return m.homeAsyncStats.hasData() != wasVisible
}

// formatHomeAsyncStats renders the async-work row for the given terminal width,
// or "" when there is nothing to show. The returned string is styled and
// left-padded to align with the status-bar path label ("  " indent), matching
// the home telemetry row.
func formatHomeAsyncStats(s homeAsyncStats, width int) string {
	if !s.hasData() {
		return ""
	}

	var segs []string
	if s.running > 0 {
		segs = append(segs, fmt.Sprintf("%s %d", i18n.T("mail.async_running"), s.running))
	}
	if s.queued > 0 {
		segs = append(segs, fmt.Sprintf("%s %d", i18n.T("mail.async_queued"), s.queued))
	}
	if s.done > 0 {
		segs = append(segs, fmt.Sprintf("%s %d", i18n.T("mail.async_done"), s.done))
	}
	if s.failed > 0 {
		segs = append(segs, fmt.Sprintf("%s %d", i18n.T("mail.async_failed"), s.failed))
	}
	if len(segs) == 0 {
		return ""
	}

	// Lead with a localized scope label (mirroring "Session:") so the user reads
	// "these are the orchestrator agent's async jobs". The whole row is muted.
	segs = append([]string{i18n.T("mail.async_label")}, segs...)
	left := "  " + StyleFaint.Render(strings.Join(segs, "  "))

	// Right-side affordance pointing at the full run browser (/daemons), dropped
	// on terminals too narrow to right-align it without colliding.
	return appendAsyncDaemonsHint(left, width)
}

// appendAsyncDaemonsHint right-aligns a "/daemons for details" affordance
// against the terminal width, mirroring appendKanbanHint. It returns left
// unchanged when there isn't room for at least two spaces of gap.
func appendAsyncDaemonsHint(left string, width int) string {
	hint := StyleFaint.Render(i18n.T("mail.async_daemons_hint"))
	pad := width - lipgloss.Width(left) - lipgloss.Width(hint) - 1
	if pad < 2 {
		return left
	}
	return left + strings.Repeat(" ", pad) + hint
}
