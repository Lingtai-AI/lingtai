package tui

// Home async-work stats row.
//
// One muted line BELOW the input box and ABOVE the bottom path/shortcut status
// bar shows the orchestrator agent's recent asynchronous work at a glance. A
// valid fresh kernel-published Agent Record child covers both daemon and Shell
// lanes. Older kernels and unavailable children retain the bounded daemon-ledger
// compatibility fallback, labeled honestly as daemon-only. The row remains a
// sibling of Home telemetry with background tea.Cmd I/O, one cached snapshot,
// no View/Update filesystem reads, and the existing mailFooterHeight budget.

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const (
	homeAsyncStatsTTL           = 1 * time.Second
	homeAsyncModelNameMaxRunes  = 64
	homeAsyncModelStatsMaxWidth = 96
)

type homeAsyncCounts struct {
	running int64
	queued  int64
	done    int64
	failed  int64
}

// homeAsyncStats is one resolved cached snapshot. unified is true only for a
// validated fresh `lingtai.async_work/v1` child; false means all values came
// from the bounded daemon-ledger compatibility reader. Token/model values are
// daemon-scoped in either mode. The loaded zero value remains visible.
type homeAsyncStats struct {
	unified bool
	running int64
	queued  int64
	done    int64
	failed  int64
	daemon  homeAsyncCounts
	shell   homeAsyncCounts
	input   int64
	output  int64
	cached  int64
	calls   int64
	models  map[string]int64
}

func (m MailModel) hasHomeAsyncStats() bool {
	return m.homeAsyncStatsLoaded
}

// fetchHomeAsyncStats preserves the existing one-command scheduler. The worker
// first consumes the published Agent Record snapshot; only when that child is
// unavailable does it invoke the pre-existing bounded daemon-ledger reader.
func (m MailModel) fetchHomeAsyncStats() tea.Cmd {
	return func() tea.Msg {
		return homeAsyncStatsMsg{
			generation: m.generation,
			t:          gatherHomeAsyncStats(m.orchestrator, time.Now()),
		}
	}
}

func gatherHomeAsyncStats(agentDir string, now time.Time) homeAsyncStats {
	if record, ok := fs.ReadAgentRecord(agentDir); ok {
		if snapshot, ok := record.FreshAsyncWork(now); ok {
			return homeAsyncStats{
				unified: true,
				running: snapshot.Counts.Running,
				queued:  snapshot.Counts.Queued,
				done:    snapshot.Counts.Done,
				failed:  snapshot.Counts.Failed,
				daemon:  homeAsyncCountsFromPublished(snapshot.Daemon.Counts),
				shell:   homeAsyncCountsFromPublished(snapshot.Shell.Counts),
				input:   snapshot.Daemon.Usage.InputTokens,
				output:  snapshot.Daemon.Usage.OutputTokens,
				cached:  snapshot.Daemon.Usage.CachedTokens,
				calls:   snapshot.Daemon.Usage.APICalls,
				models:  snapshot.Daemon.ModelCounts,
			}
		}
	}

	fallback := fs.ReadRecentDaemonActivity(agentDir)
	models := make(map[string]int64, len(fallback.Models))
	for _, model := range fallback.Models {
		models[model] = saturatingCountAdd(models[model], 1)
	}
	return homeAsyncStats{
		running: int64(fallback.Counts.Running),
		queued:  int64(fallback.Counts.Queued),
		done:    int64(fallback.Counts.Done),
		failed:  int64(fallback.Counts.Failed),
		input:   fallback.Tokens.Input,
		output:  fallback.Tokens.Output,
		cached:  fallback.Tokens.Cached,
		calls:   fallback.Tokens.APICalls,
		models:  models,
	}
}

func (s homeAsyncStats) aggregateCounts() homeAsyncCounts {
	return homeAsyncCounts{running: s.running, queued: s.queued, done: s.done, failed: s.failed}
}

func homeAsyncCountsFromPublished(counts fs.AsyncWorkCounts) homeAsyncCounts {
	return homeAsyncCounts{
		running: counts.Running,
		queued:  counts.Queued,
		done:    counts.Done,
		failed:  counts.Failed,
	}
}

func (m *MailModel) maybeScheduleHomeAsyncStats(now time.Time) tea.Cmd {
	if m.homeAsyncStatsInFlight {
		return nil
	}
	if !m.homeAsyncStatsLastFetch.IsZero() && now.Sub(m.homeAsyncStatsLastFetch) < homeAsyncStatsTTL {
		return nil
	}
	m.homeAsyncStatsInFlight = true
	return m.fetchHomeAsyncStats()
}

func (m *MailModel) applyHomeAsyncStats(t homeAsyncStats, now time.Time) bool {
	was := m.hasHomeAsyncStats()
	m.homeAsyncStats = t
	m.homeAsyncStatsInFlight = false
	m.homeAsyncStatsLoaded = true
	m.homeAsyncStatsLastFetch = now
	return m.hasHomeAsyncStats() != was
}

// formatHomeAsyncStats renders exactly one width-safe row. Unified snapshots
// show aggregate counts plus explicitly named daemon and Shell lane counts.
// Daemon model/token detail stays inside the daemon segment. Compatibility
// snapshots use the long-form legacy row under the truthful `Daemons:` label.
func formatHomeAsyncStats(s homeAsyncStats, width int) string {
	var candidates []string
	if s.unified {
		candidates = unifiedHomeAsyncCandidates(s)
	} else {
		candidates = fallbackHomeAsyncCandidates(s)
	}
	left := renderHomeAsyncCandidate(candidates, width)
	return appendAsyncDaemonsHint(left, width)
}

func unifiedHomeAsyncCandidates(s homeAsyncStats) []string {
	aggregate := compactHomeAsyncCounts(s.aggregateCounts())
	daemonCore := i18n.T("mail.async_daemon_lane") + " " + compactHomeAsyncCounts(s.daemon)
	shell := i18n.T("mail.async_shell_lane") + " " + compactHomeAsyncCounts(s.shell)
	core := i18n.T("mail.async_label") + " " + aggregate + " · " + daemonCore + " · " + shell

	tokens := homeAsyncTokenSegments(s)
	withTokens := i18n.T("mail.async_label") + " " + aggregate + " · " +
		daemonCore + " " + strings.Join(tokens, "  ") + " · " + shell
	models := formatHomeAsyncModelStats(s.models)
	if models == "" {
		return uniqueHomeAsyncCandidates(withTokens, core)
	}
	full := i18n.T("mail.async_label") + " " + aggregate + " · " +
		daemonCore + " " + models + "  " + strings.Join(tokens, "  ") + " · " + shell
	return uniqueHomeAsyncCandidates(full, withTokens, core)
}

func fallbackHomeAsyncCandidates(s homeAsyncStats) []string {
	counts := longHomeAsyncCountSegments(s.aggregateCounts())
	tokens := homeAsyncTokenSegments(s)
	core := i18n.T("mail.daemons_label") + "  " + strings.Join(counts, "  ")
	withTokens := core + "  " + strings.Join(tokens, "  ")
	models := formatHomeAsyncModelStats(s.models)
	if models == "" {
		return uniqueHomeAsyncCandidates(withTokens, core)
	}
	full := core + "  " + models + "  " + strings.Join(tokens, "  ")
	return uniqueHomeAsyncCandidates(full, withTokens, core)
}

func compactHomeAsyncCounts(s homeAsyncCounts) string {
	return fmt.Sprintf("%s%d %s%d %s%d %s%d",
		i18n.T("mail.async_running_short"), s.running,
		i18n.T("mail.async_queued_short"), s.queued,
		i18n.T("mail.async_done_short"), s.done,
		i18n.T("mail.async_failed_short"), s.failed,
	)
}

func longHomeAsyncCountSegments(s homeAsyncCounts) []string {
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
		return []string{
			fmt.Sprintf("%s 0", i18n.T("mail.async_running")),
			fmt.Sprintf("%s 0", i18n.T("mail.async_queued")),
			fmt.Sprintf("%s 0", i18n.T("mail.async_done")),
			fmt.Sprintf("%s 0", i18n.T("mail.async_failed")),
		}
	}
	return segs
}

func homeAsyncTokenSegments(s homeAsyncStats) []string {
	return []string{
		fmt.Sprintf("%s %s", i18n.T("mail.async_in"), humanizeTokenCount(s.input)),
		fmt.Sprintf("%s %s", i18n.T("mail.async_out"), humanizeTokenCount(s.output)),
		fmt.Sprintf("%s %s", i18n.T("mail.async_cache"), homeAsyncCachePercent(s.cached, s.input)),
		fmt.Sprintf("%s %d", i18n.T("mail.async_calls"), s.calls),
	}
}

func homeAsyncCachePercent(cached, input int64) string {
	if input <= 0 {
		return "0%"
	}
	percent := math.Round(100 * float64(cached) / float64(input))
	return fmt.Sprintf("%.0f%%", percent)
}

func uniqueHomeAsyncCandidates(values ...string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		if len(result) == 0 || value != result[len(result)-1] {
			result = append(result, value)
		}
	}
	return result
}

func renderHomeAsyncCandidate(candidates []string, width int) string {
	indent := "  "
	if width > 0 && width < len(indent) {
		indent = indent[:width]
	}
	available := width - lipgloss.Width(indent)
	chosen := candidates[len(candidates)-1]
	if width <= 0 {
		chosen = candidates[0]
	} else {
		for _, candidate := range candidates {
			if lipgloss.Width(candidate) <= available {
				chosen = candidate
				break
			}
		}
		if available <= 0 {
			chosen = ""
		} else if lipgloss.Width(chosen) > available {
			chosen = ansi.Truncate(chosen, available, "…")
		}
	}
	return indent + StyleFaint.Render(chosen)
}

// formatHomeAsyncModelStats returns a bounded, control-free parenthetical
// model-count segment. Published child identifiers are already strict; the
// sanitizing merge also keeps legacy daemon-ledger display strings harmless.
func formatHomeAsyncModelStats(models map[string]int64) string {
	counts := make(map[string]int64, len(models))
	for raw, count := range models {
		if model := safeHomeAsyncModelName(raw); model != "" && count > 0 {
			counts[model] = saturatingCountAdd(counts[model], count)
		}
	}
	if len(counts) == 0 {
		return ""
	}

	keys := make([]string, 0, len(counts))
	for model := range counts {
		keys = append(keys, model)
	}
	sort.Strings(keys)
	if len(keys) == 1 && counts[keys[0]] == 1 {
		return "(" + keys[0] + ")"
	}

	parts := make([]string, 0, len(keys))
	for index, model := range keys {
		part := fmt.Sprintf("%s × %d", model, counts[model])
		candidate := "(" + strings.Join(append(append([]string(nil), parts...), part), " · ") + ")"
		if lipgloss.Width(candidate) > homeAsyncModelStatsMaxWidth {
			remaining := len(keys) - index
			if len(parts) > 0 {
				parts = append(parts, fmt.Sprintf("+%d", remaining))
			}
			break
		}
		parts = append(parts, part)
	}
	if len(parts) == 0 {
		return ""
	}
	return "(" + strings.Join(parts, " · ") + ")"
}

func saturatingCountAdd(left, right int64) int64 {
	if left >= math.MaxInt64-right {
		return math.MaxInt64
	}
	return left + right
}

func safeHomeAsyncModelName(raw string) string {
	clean := strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, raw)
	clean = strings.TrimSpace(clean)
	runes := []rune(clean)
	if len(runes) > homeAsyncModelNameMaxRunes {
		clean = string(runes[:homeAsyncModelNameMaxRunes-1]) + "…"
	}
	return clean
}

// appendAsyncDaemonsHint retains the existing right-side /daemons affordance,
// dropping it when there is not room for at least two columns of gap.
func appendAsyncDaemonsHint(left string, width int) string {
	hint := StyleFaint.Render(i18n.T("mail.async_daemons_hint"))
	pad := width - lipgloss.Width(left) - lipgloss.Width(hint) - 1
	if pad < 2 {
		return left
	}
	return left + strings.Repeat(" ", pad) + hint
}
