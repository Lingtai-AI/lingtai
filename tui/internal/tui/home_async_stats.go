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
	"math"
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
// async job ledger counts plus recent-window token usage (in/out/cache/calls,
// mirroring the Telegram Task Card). A zero value means "all counts are zero"
// — the row is still rendered once loaded, showing every segment as 0.
type homeAsyncStats struct {
	running int
	queued  int
	done    int
	failed  int
	input   int64
	output  int64
	cached  int64
	calls   int64
}

// hasHomeAsyncStats reports whether View() will render the additive async row.
// It is the single predicate shared by the height budget (syncViewportHeight)
// and the renderer (View), mirroring hasHomeTelemetry so they can never disagree
// about whether the row occupies a line. The row appears once the first
// background snapshot has landed and stays visible afterwards, even when all
// counts are zero (idle daemons inside the 10-minute window) — mirroring the
// Telegram Task Card's always-present daemon row.
func (m MailModel) hasHomeAsyncStats() bool {
	return m.homeAsyncStatsLoaded
}

// fetchHomeAsyncStats returns the background command that reads the async job
// ledger snapshot. It runs off the render/input path: os.ReadDir over the
// daemons directory can stall on a slow volume, so like fetchHomeTelemetry it is
// never invoked synchronously from View()/Update(). Both reads are bounded to
// the 10-minute daemon window (fs.CountDaemons + fs.DaemonRecentLedgerSummary),
// so stale historical runs never get their state or token ledger opened on the
// per-second tick.
func (m MailModel) fetchHomeAsyncStats() tea.Cmd {
	return func() tea.Msg {
		counts := fs.CountDaemons(m.orchestrator)
		tokens := fs.DaemonRecentLedgerSummary(m.orchestrator)
		return homeAsyncStatsMsg{
			generation: m.generation,
			t: homeAsyncStats{
				running: counts.Running,
				queued:  counts.Queued,
				done:    counts.Done,
				failed:  counts.Failed,
				input:   tokens.Input,
				output:  tokens.Output,
				cached:  tokens.Cached,
				calls:   tokens.APICalls,
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
// visibility flipped, so the viewport height is re-synced only when the layout
// actually changes. Because the row becomes visible on the first snapshot and
// never hides afterwards (counts may be zero), the only flip is the first load.
func (m *MailModel) applyHomeAsyncStats(t homeAsyncStats, now time.Time) bool {
	was := m.hasHomeAsyncStats()
	m.homeAsyncStats = t
	m.homeAsyncStatsLoaded = true
	m.homeAsyncStatsLastFetch = now
	return m.hasHomeAsyncStats() != was
}

// formatHomeAsyncStats renders the async-work row for the given terminal width.
// The returned string is styled and left-padded to align with the status-bar
// path label ("  " indent), matching the home telemetry row. Callers gate on
// hasHomeAsyncStats, so this always renders: when every count is zero the row
// shows "running 0 · queued 0 · done 0 · failed 0" instead of disappearing, and
// the recent-window token line ("in <n> · out <n> · cache <pct> · calls <n>",
// mirroring the Telegram Task Card) is always appended.
func formatHomeAsyncStats(s homeAsyncStats, width int) string {
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
		// All-zero snapshot (idle daemons in the 10-minute window): render every
		// bucket as 0 so the row never vanishes, mirroring the Telegram Task Card.
		segs = []string{
			fmt.Sprintf("%s 0", i18n.T("mail.async_running")),
			fmt.Sprintf("%s 0", i18n.T("mail.async_queued")),
			fmt.Sprintf("%s 0", i18n.T("mail.async_done")),
			fmt.Sprintf("%s 0", i18n.T("mail.async_failed")),
		}
	}

	// Recent-window token usage, mirroring the Telegram Task Card's daemon-stats
	// line. Always rendered (zeros while idle). Cache pct follows the TUI
	// convention from props.go: cached/input.
	cachePct := 0
	if s.input > 0 {
		cachePct = int(math.Round(100.0 * float64(s.cached) / float64(s.input)))
	}
	segs = append(segs,
		fmt.Sprintf("%s %s", i18n.T("mail.async_in"), humanizeTokenCount(s.input)),
		fmt.Sprintf("%s %s", i18n.T("mail.async_out"), humanizeTokenCount(s.output)),
		fmt.Sprintf("%s %d%%", i18n.T("mail.async_cache"), cachePct),
		fmt.Sprintf("%s %d", i18n.T("mail.async_calls"), s.calls),
	)

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
