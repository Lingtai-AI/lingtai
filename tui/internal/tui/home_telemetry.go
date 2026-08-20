package tui

import (
	"fmt"
	"image/color"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// Home telemetry row — a single muted line shown BELOW the input box and ABOVE
// the bottom path/shortcut status bar. It condenses the CURRENT SESSION's token
// economy and the live context-window pressure into one high-density line:
//
//	Session:  llm zhipu/glm-5.2  api 42  tok 181.6k (miss 1.4k)  cache 88%    ctx 186.5k/250.0k ▓▓▓▓░░ 73%
//
// All numbers are scoped to the current molt session (since the latest
// psyche_molt), NOT the whole-ledger lifetime total and NOT a single round:
//   - llm:     active configured provider/model, when both manifest fields exist
//   - api:     LLM API calls this session
//   - tok:     total session tokens (input + output + thinking)
//   - (miss):  cache-miss tokens (input not served from cache), glued to tok
//   - cache:   cache-hit rate (cached / input)
// and, separately, the live context-window pressure with the gauge Jason liked:
//   - ctx used/limit ▓▓░░ N%: tokens in use over the model's limit,
//     the gauge, then the fill percentage on the right of the bar.
//
// It is scalar-only — never the noisy `_meta` block hidden by PR #440.
//
// Direct migration to the kernel's Agent Record (kernel #1423,
// lingtai-kernel src/lingtai/kernel/session_stats): the row now reads the
// ONE versioned, redacted `system/agent_record.json` (fs.ReadAgentRecord)
// instead of `.status.json` + `init.json`/`manifest.resolved.json`. That
// record is the kernel's sole source for normal live status — this consumer
// curates and renders it, it does not independently re-collect or reconstruct
// it from any other file. There is no old-consumer fallback: a missing,
// malformed, or unrecognized-schema record is treated as an explicit bounded
// unavailable state (the row hides, exactly like the pre-existing "no data"
// case), never reconstructed from `.status.json`, heartbeat files, raw
// `logs/events.jsonl` notifications, or the manifest. The ledger/SQLite path
// remains the detail path in props.go; Home does not reconstruct its economy
// from history.

// homeTelemetry holds the already-resolved scalars for the home row. Keeping the
// data plain (no rendering) makes formatHomeTelemetry trivially testable.
type homeTelemetry struct {
	provider      string  // active configured provider from the Agent record's model block
	model         string  // active configured model from the Agent record's model block
	apiCalls      int64   // current-session LLM API calls; 0 = none/unknown
	sessionTokens int64   // current-session tokens (input+output+thinking); 0 = unknown
	cached        int64   // current-session cached input tokens
	inputTokens   int64   // current-session input tokens (cache-rate denominator)
	contextLimit  int64   // model context window; 0 = unknown
	contextUsed   int64   // context tokens in use (Agent record usage.context_used_tokens); 0 = unknown
	contextUsage  float64 // latest context-usage fraction 0..1; <0 = unknown
}

// --- Async scheduling ------------------------------------------------------
//
// gatherHomeTelemetry reads one `system/agent_record.json` snapshot (fs.ReadAgentRecord)
// and nothing else. It deliberately does not invoke the ledger/SQLite/history
// path: Home must use the kernel-owned current-session counters and leave
// `/kanban` detail as the ledger consumer. It therefore still MUST NOT run on
// the Bubble Tea render (View) or input (Update/syncViewportHeight) paths —
// even a small filesystem read can stall the whole TUI.
//
// Instead the UI paths read a last-known snapshot cached on the model
// (m.homeTelemetry), and the I/O runs in the background as a tea.Cmd
// (fetchHomeTelemetry) that returns a homeTelemetryMsg to refresh the snapshot.
// The snapshot only moves when the kernel rewrites `system/agent_record.json`
// (throttled to session_stats_refresh_seconds(), default 5s), so a sub-second
// staleness on the boundary is invisible.
//
// Fetches are debounced by two model flags checked in maybeScheduleHomeTelemetry:
//   - homeTelemetryInFlight: at most one background fetch runs at a time, so a
//     burst of keypresses/renders cannot pile up workers.
//   - homeTelemetryLastFetch + homeTelemetryTTL: after a completed fetch we skip
//     re-reading until the TTL elapses, so the steady-state 1s poll costs little.

// homeTelemetryTTL is the minimum wall-clock interval between two completed
// background telemetry fetches. It is deliberately close to the mail poll cadence
// (m.pollRate, ~1s): the underlying agent_record.json snapshot is refreshed on a
// similar cadence, so fetching faster only burns I/O without showing newer
// numbers. Repeated render/keypress within the TTL reuses the cached snapshot
// with no I/O at all.
const homeTelemetryTTL = 1 * time.Second

// homeTelemetryMsg carries a freshly-gathered telemetry snapshot from the
// background fetch back into the model on the Update path.
type homeTelemetryMsg struct {
	generation uint64
	t          homeTelemetry
}

type homeAsyncStatsMsg struct {
	generation uint64
	t          homeAsyncStats
}

// fetchHomeTelemetry is the background worker: it performs the Agent record
// read off the UI thread and returns a homeTelemetryMsg.
// It is a value-receiver tea.Cmd, so it captures a snapshot of the model (orchestrator
// path + session cache) exactly like refreshMail/initialRebuild — the running
// command never touches live model state.
func (m MailModel) fetchHomeTelemetry() tea.Msg {
	return homeTelemetryMsg{generation: m.generation, t: m.gatherHomeTelemetry()}
}

// maybeScheduleHomeTelemetry returns a fetchHomeTelemetry command when a new
// background fetch is warranted, or nil to reuse the cached snapshot. It is the
// single debounce/TTL/in-flight gate: callers (the poll tick and post-refresh)
// funnel through it so no other path spawns telemetry I/O. It mutates only the
// in-flight/last-fetch bookkeeping, never the snapshot itself (that lands via
// homeTelemetryMsg), so it is safe to call from the Update path.
func (m *MailModel) maybeScheduleHomeTelemetry(now time.Time) tea.Cmd {
	if m.homeTelemetryInFlight {
		return nil
	}
	// Honor the TTL only once we have a snapshot to fall back on; the very first
	// fetch (nothing cached yet) is always allowed so the row can appear promptly.
	if m.homeTelemetryLoaded && now.Sub(m.homeTelemetryLastFetch) < homeTelemetryTTL {
		return nil
	}
	m.homeTelemetryInFlight = true
	return m.fetchHomeTelemetry
}

// applyHomeTelemetry lands a background fetch result: it stores the snapshot,
// clears the in-flight flag, and stamps the completion time for the TTL. It
// returns true when the telemetry row's VISIBILITY flipped (row ⇄ no-row), which
// is the only telemetry change that affects layout height — the caller re-syncs
// the viewport only then, avoiding layout thrash on every numeric tick.
//
// "was visible" uses hasHomeTelemetry() (the loaded-aware predicate), NOT a bare
// hasData() on the old snapshot: before the first fetch the row is not visible
// even though the zero snapshot's hasData() reports true (the contextUsage==0
// sentinel trap). So the first real landing correctly reports false→true.
func (m *MailModel) applyHomeTelemetry(t homeTelemetry, now time.Time) (visibilityChanged bool) {
	was := m.hasHomeTelemetry()
	m.homeTelemetry = t
	m.homeTelemetryLoaded = true
	m.homeTelemetryInFlight = false
	m.homeTelemetryLastFetch = now
	return m.hasHomeTelemetry() != was
}

// gatherHomeTelemetry resolves Home from one canonical Agent record read
// (fs.ReadAgentRecord, `system/agent_record.json`):
//   - the four since-molt economy counters come from record.Usage;
//   - context usage, used tokens, and window come from the same record's
//     Usage block;
//   - the active configured provider/model pair comes from record.Model.
//
// The ContextLimitTokens > 0 gate mirrors the previous `.status.json`
// WindowSize > 0 gate: the kernel publishes a null/zero context_limit_tokens
// when no context window is known yet, and Home must not fabricate a used/limit
// pair from that. A missing, malformed, or unrecognized-schema record (ok ==
// false) leaves every field at its zero/-1 sentinel — an explicit bounded
// unavailable state, never a fallback to `.status.json`, heartbeat files, the
// manifest, or raw notification logs.
func (m MailModel) gatherHomeTelemetry() homeTelemetry {
	t := homeTelemetry{contextUsage: -1}
	if m.orchestrator == "" {
		return t
	}
	record, ok := fs.ReadAgentRecord(m.orchestrator)
	if !ok {
		return t
	}

	t.provider = record.Model.Provider
	t.model = record.Model.Model

	usage := record.Usage
	t.apiCalls = usage.APICalls
	// Cached tokens are a subset of input and are reported separately.
	t.sessionTokens = usage.InputTokens + usage.OutputTokens + usage.ThinkingTokens
	t.cached = usage.CachedTokens
	t.inputTokens = usage.InputTokens

	if usage.ContextLimitTokens > 0 {
		t.contextUsage = usage.ContextUsagePct / 100
		t.contextLimit = usage.ContextLimitTokens
		t.contextUsed = usage.ContextUsedTokens
	}
	return t
}

// hasData reports whether any fragment is renderable. With nothing to show the
// caller omits the whole row.
func (t homeTelemetry) hasData() bool {
	return t.apiCalls > 0 || t.sessionTokens > 0 || t.contextUsage >= 0 || (t.provider != "" && t.model != "")
}

// hasHomeTelemetry reports whether View() will render the additive telemetry row.
// It is the single predicate shared by the height budget (syncViewportHeight)
// and the renderer (View) so they can never disagree about whether the row
// occupies a line — a disagreement is exactly what clipped the status bar.
//
// It reads the last-known cached snapshot (m.homeTelemetry) ONLY — never
// gatherHomeTelemetry — so it stays on the UI hot path without touching
// disk or history. The snapshot is refreshed asynchronously by the
// fetchHomeTelemetry background command (see the Async scheduling note above).
//
// The homeTelemetryLoaded gate matters: a zero-value homeTelemetry has
// contextUsage == 0, and hasData() treats contextUsage >= 0 as "present" (the
// unknown sentinel is -1, set only inside gatherHomeTelemetry). So before the
// first fetch lands we must NOT trust the zero snapshot — we render without the
// row rather than showing a spurious "ctx 0%". Once a fetch has completed, the
// snapshot carries the real -1/≥0 sentinel and hasData() is authoritative.
func (m MailModel) hasHomeTelemetry() bool {
	return m.homeTelemetryLoaded && m.homeTelemetry.hasData()
}

// mailFooterHeight returns how many terminal rows the mail-view footer block
// occupies: sep(1) + palette(N) + input(N) + optional telemetry(1) + status(1),
// plus the layout's trailing border(1). It is the single source of truth for
// footer height shared by syncViewportHeight (the viewport budget) and View()
// (the actual render), so the additive telemetry row is reserved consistently
// and never overflows the frame.
func mailFooterHeight(paletteLines, inputLines, telemetryRows int) int {
	h := 1 + paletteLines + inputLines + 1 + 1 // sep + palette + input + border + status
	return h + telemetryRows
}

// formatHomeTelemetry renders the telemetry row for the given terminal width, or
// "" when there is nothing to show. The returned string is already styled and
// left-padded to align with the status-bar path label ("  " indent). width is
// the full terminal width; the bar adapts to it and is hidden entirely below
// homeTelemetryBarMinWidth so narrow terminals keep the numbers.
func formatHomeTelemetry(t homeTelemetry, width int) string {
	if !t.hasData() {
		return ""
	}

	var segs []string

	// Active configured LLM and current-session token economy. The Agent
	// record's model identity is omitted as a pair when either field is absent;
	// every token fragment is likewise dropped when its source is unknown so
	// Home never shows a fabricated zero.
	if t.provider != "" && t.model != "" {
		segs = append(segs, i18n.T("mail.telemetry_model")+" "+t.provider+"/"+t.model)
	}
	if t.apiCalls > 0 {
		segs = append(segs, fmt.Sprintf("%s %d", i18n.T("mail.telemetry_api"), t.apiCalls))
	}
	if t.sessionTokens > 0 {
		tok := i18n.T("mail.telemetry_tok") + " " + humanizeTokenCount(t.sessionTokens)
		// Cache-miss (input NOT served from cache) glued to the token total in
		// parens, exactly where Jason asked for it — right after `tok …`, NOT after
		// the cache percentage. Reuses cacheMiss() (input - cached, clamped ≥0) and
		// humanizeTokenCount so it reads "tok 1.1M (miss 8.6k)" in the same units as
		// the token total. Gated on inputTokens > 0 so a session with no recorded
		// input never shows a bare "(miss 0)".
		if t.inputTokens > 0 {
			tok += " (" + i18n.T("mail.telemetry_miss") + " " + humanizeTokenCount(cacheMiss(t.cached, t.inputTokens)) + ")"
		}
		segs = append(segs, tok)
	}
	if t.inputTokens > 0 {
		segs = append(segs, i18n.T("mail.telemetry_cache")+" "+formatCacheRate(t.cached, t.inputTokens))
	}

	// ctx  186.5k/250.0k  ▓▓▓░░ 73%  — live context-window pressure
	// with the gauge Jason liked (msg 3195/3196). Jason's layout follow-up
	// (msg 3251): the scope reads as an explicit label, then used/limit, then the
	// bar, then the percentage on the RIGHT of the bar — so the eye reads
	// "what / how much / how full" in order, never the confusing "73% / 250k" the
	// percentage-first form produced. Jason's final follow-up trimmed the verbose
	// "Current Context" label to the technical abbreviation "ctx" (same in every
	// locale). The used/limit + bar + percentage are the core; the bar is dropped
	// on narrow terminals (the numbers stay), and the "ctx" label + percentage
	// always frame the metric.
	if t.contextUsage >= 0 {
		pct := t.contextUsage * 100
		ctx := i18n.T("mail.telemetry_context")
		if t.contextUsed > 0 && t.contextLimit > 0 {
			ctx += " " + humanizeTokenCount(t.contextUsed) + "/" + humanizeTokenCount(t.contextLimit)
		} else if t.contextLimit > 0 {
			// No absolute "used" recorded yet (e.g. a fresh session with a known
			// window but zero context tokens so far): derive it from the usage
			// fraction so used/limit still renders rather than vanishing.
			ctx += " " + humanizeTokenCount(int64(t.contextUsage*float64(t.contextLimit))) + "/" + humanizeTokenCount(t.contextLimit)
		}
		if barW := homeTelemetryBarWidth(width); barW > 0 {
			ctx += "  " + renderContextBar(pct, barW)
		}
		// Percentage to the RIGHT of the bar (or right of used/limit when the bar
		// is hidden), never before used/limit.
		ctx += fmt.Sprintf(" %.0f%%", pct)
		segs = append(segs, ctx)
	}

	if len(segs) == 0 {
		return ""
	}
	// Lead with a localized scope label (Jason, msg 3217) so the user reads
	// "these numbers are the CURRENT SESSION" before the metrics. Localized via
	// i18n (mail.telemetry_session), never hard-coded. Jason's final follow-up
	// trimmed the verbose "Current Session" to the compact "Session:" label (same
	// in every locale); the trailing colon now carries the set-off the middle-dot
	// used to, so the bullet is dropped and the label reads "Session:  api 42 …".
	// The whole row is muted by StyleFaint below.
	segs = append([]string{i18n.T("mail.telemetry_session")}, segs...)
	// Two spaces between segments for a calm, low-density-feeling separation; the
	// label words themselves are muted by the caller's style.
	left := "  " + StyleFaint.Render(strings.Join(segs, "  "))

	// Right-side affordance pointing at the full breakdown (Jason's follow-up):
	// the home row is a glance; "/kanban for details" tells the user where the
	// system/tools/history split, window math, and per-tool counts live. Localized
	// via i18n (mail.telemetry_kanban_hint). It is dropped on terminals too narrow
	// to right-align it without colliding with the metrics, so the numbers always
	// win the space. Mirrors the status bar's left/pad/right layout (mail.go).
	return appendKanbanHint(left, width)
}

// appendKanbanHint right-aligns the first-line affordance against the terminal
// width, padding between the already-rendered left segment and the hint. The hint
// leads with the copy-mode reminder ("ctrl+y to select text") that Jason asked to
// surface on this upper line (PR #402), then the "/kanban for details" pointer,
// joined by the shared hints.sep so it reads like the status bar's lower hint
// line ("ctrl+o to expand, / for commands"). It returns left unchanged when there
// isn't room for at least two spaces of gap, so the metrics never get clipped on
// narrow terminals — the whole affordance is dropped together, copy reminder and
// all.
func appendKanbanHint(left string, width int) string {
	hintText := i18n.T("mail.telemetry_copy_hint") +
		i18n.T("hints.sep") + i18n.T("mail.telemetry_kanban_hint")
	hint := StyleFaint.Render(hintText)
	// -1 trailing margin mirrors the status bar's right edge (mail.go statusPad).
	pad := width - lipgloss.Width(left) - lipgloss.Width(hint) - 1
	if pad < 2 {
		return left
	}
	return left + strings.Repeat(" ", pad) + hint
}

const (
	// homeTelemetryBarMinWidth is the narrowest terminal that still shows the
	// context bar; below it we keep "tok …" and "ctx N%" but drop the bar so the
	// row never wraps. Jason asked for width<40 → numbers only.
	homeTelemetryBarMinWidth = 40
	// homeTelemetryBarMax caps the bar so it stays a compact gauge, not a ruler.
	homeTelemetryBarMax = 14
)

// homeTelemetryBarWidth picks an adaptive bar cell count for the terminal width,
// or 0 to hide the bar on narrow terminals.
func homeTelemetryBarWidth(width int) int {
	if width < homeTelemetryBarMinWidth {
		return 0
	}
	// Scale gently with width; clamp to a compact range so it reads as a gauge.
	w := width / 8
	if w < 6 {
		w = 6
	}
	if w > homeTelemetryBarMax {
		w = homeTelemetryBarMax
	}
	return w
}

// renderContextBar returns a small filled/empty bar proportional to pct (0..100)
// with width cells, colored by pressure: <70% muted green/teal, 70–89% amber,
// >=90% muted red. Empty cells stay dim gray. Uses ▓ (filled) and ░ (empty) to
// match Jason's mock; both are box-drawing glyphs the TUI already relies on.
func renderContextBar(pct float64, width int) string {
	if width < 1 {
		width = 1
	}
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	filled := int((pct / 100.0) * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	full := lipgloss.NewStyle().Foreground(contextBarColor(pct)).Render(strings.Repeat("▓", filled))
	empty := lipgloss.NewStyle().Foreground(ColorTextFaint).Render(strings.Repeat("░", width-filled))
	return full + empty
}

// contextBarColor maps context pressure to a muted theme color. Thresholds match
// Jason's spec: calm below 70%, caution to 89%, alarm at 90%+. All three are the
// theme's existing muted state colors — no bright red, consistent with the
// beige/dim footer palette.
func contextBarColor(pct float64) color.Color {
	switch {
	case pct >= 90:
		return ColorSuspended // 朱砂 — muted red
	case pct >= 70:
		return ColorAccent // 琥珀 — amber
	default:
		return ColorActive // 竹青 — muted green/teal
	}
}
