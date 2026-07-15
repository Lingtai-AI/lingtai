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
//	Session:  api 42  tok 181.6k (miss 1.4k)  cache 88%  tok/api 4.3k    ctx 186.5k/250.0k ▓▓▓▓░░ 73%
//
// All numbers are scoped to the current molt session (since the latest
// psyche_molt), NOT the whole-ledger lifetime total and NOT a single round:
//   - api:     LLM API calls this session
//   - tok:     total session tokens (input + output + thinking)
//   - (miss):  cache-miss tokens (input not served from cache), glued to tok
//   - cache:   cache-hit rate (cached / input)
//   - tok/api: average tokens per API call
// and, separately, the live context-window pressure with the gauge Jason liked:
//   - ctx used/limit ▓▓░░ N%: tokens in use over the model's limit,
//     the gauge, then the fill percentage on the right of the bar.
//
// It is scalar-only — never the noisy `_meta` block hidden by PR #440.
//
// Why this differs from the original #441 row ("数据有点怪", msg 3198): that row
// showed `tok <global> / <limit>` where <global> was fs.SumTokenLedger over the
// ENTIRE ledger — every molt the agent has ever lived (163 molts / 20k+ rows for
// a long-lived agent) — so the number only grew and bore no relation to the
// session in view. And <limit> was read from manifest["llm"]["context_limit"],
// a key that does not exist (context_limit sits at the manifest TOP LEVEL), so
// the "/ limit" half silently never rendered. This version reads current-session
// stats from the same source the molt-session stats panel uses
// (fs.SumMoltSessionTokenLedger().Current, props.go) and reads context usage +
// window from the SAME live `.status.json` snapshot /kanban's context section
// uses (fs.ReadStatus().Tokens.Context, props.go:518-535) so the two never
// disagree. When no data is available the row is omitted entirely.

// homeTelemetry holds the already-resolved scalars for the home row. Keeping the
// data plain (no rendering) makes formatHomeTelemetry trivially testable.
type homeTelemetry struct {
	apiCalls      int64   // current-session LLM API calls; 0 = none/unknown
	sessionTokens int64   // current-session tokens (input+output+thinking); 0 = unknown
	cached        int64   // current-session cached input tokens
	inputTokens   int64   // current-session input tokens (cache-rate denominator)
	contextLimit  int64   // model context window; 0 = unknown
	contextUsed   int64   // context tokens in use (.status.json TotalTokens); 0 = unknown
	contextUsage  float64 // latest context-usage fraction 0..1; <0 = unknown
}

// --- Async scheduling ------------------------------------------------------
//
// gatherHomeTelemetry does real I/O: it reaches fs.SumMoltSessionTokenLedger
// (an incremental in-process read of canonical events.jsonl plus the token
// ledger; derived SQLite is only a compatibility fallback when events.jsonl is
// absent) and fs.ReadStatus/ReadInitManifest. On a slow volume that work can
// still take time. It therefore MUST NOT run on the Bubble Tea render
// (View) or input (Update/syncViewportHeight) paths — a stall there freezes the
// whole TUI.
//
// Instead the UI paths read a last-known snapshot cached on the model
// (m.homeTelemetry), and the I/O runs in the background as a tea.Cmd
// (fetchHomeTelemetry) that returns a homeTelemetryMsg to refresh the snapshot.
// The snapshot only moves when the kernel writes new ledger/status data (seconds
// to minutes apart), so a sub-second staleness on the boundary is invisible.
//
// Fetches are debounced by two model flags checked in maybeScheduleHomeTelemetry:
//   - homeTelemetryInFlight: at most one background fetch runs at a time, so a
//     burst of keypresses/renders cannot spawn a pile of sqlite subprocesses.
//   - homeTelemetryLastFetch + homeTelemetryTTL: after a completed fetch we skip
//     re-fetching until the TTL elapses, so the steady-state 1s poll doesn't
//     hammer the sidecar and rapid typing costs nothing.

// homeTelemetryTTL is the minimum wall-clock interval between two completed
// background telemetry fetches. It is deliberately close to the mail poll cadence
// (m.pollRate, ~1s): the underlying data (token ledger, .status.json) is rewritten
// by the kernel on a similar cadence, so fetching faster only burns I/O without
// showing newer numbers. Repeated render/keypress within the TTL reuses the cached
// snapshot with no I/O at all.
const homeTelemetryTTL = 1 * time.Second

// homeTelemetryMsg carries a freshly-gathered telemetry snapshot from the
// background fetch back into the model on the Update path.
type homeTelemetryMsg struct {
	generation uint64
	t          homeTelemetry
}

// fetchHomeTelemetry is the background worker: it performs all telemetry I/O
// (event log/ledger/status/manifest) off the UI thread and returns a homeTelemetryMsg.
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

// gatherHomeTelemetry resolves the telemetry scalars for the orchestrator agent
// from data the TUI already reads elsewhere:
//   - current-session token/cache/api stats from logs/token_ledger.jsonl bounded
//     to the current molt window (fs.SumMoltSessionTokenLedger().Current) — the
//     SAME source and scope as the molt-session stats panel in props.go
//   - contextUsage + contextLimit from the live `.status.json` snapshot
//     (fs.ReadStatus().Tokens.Context) — the SAME source, scope, and gate
//     (WindowSize > 0) that /kanban's context section uses (props.go:518-535).
//     This is the fix for Jason's "home ctx bar disagrees with /kanban" report:
//     `.status.json` is rewritten by the kernel on a tight cadence and re-read
//     here every 1s poll, whereas the notification Meta.Context.Usage below is
//     only refreshed when a notification is injected (per molt round) and so can
//     lag the live value by many minutes. Reading `.status.json` makes the home
//     row show the exact same percentage and window /kanban does.
//   - notification Meta.Context.Usage from the UNFILTERED session cache is kept
//     ONLY as a fallback for agents with no live `.status.json` (stopped /
//     never-booted — /kanban shows no context section for them either, but the
//     home row degrades to the last notification value so the bar doesn't vanish
//     mid-session). Reading the session cache — not the verbose-filtered
//     m.messages — keeps that fallback independent of Ctrl+O/verbose state (the
//     #442 regression: shouldShow() hides notifications below verboseThinking).
//   - contextLimit also falls back to manifest TOP-LEVEL `context_limit`
//     (fs.ReadInitManifest) when `.status.json` carries no WindowSize.
//
// Every source degrades to its "unknown" sentinel independently, so a missing
// ledger / status / manifest / notification just drops that fragment rather than
// the row.
func (m MailModel) gatherHomeTelemetry() homeTelemetry {
	t := homeTelemetry{contextUsage: -1}
	if m.orchestrator != "" {
		cur := fs.SumMoltSessionTokenLedger(m.orchestrator).Current
		t.apiCalls = cur.APICalls
		t.sessionTokens = cur.Input + cur.Output + cur.Thinking
		t.cached = cur.Cached
		t.inputTokens = cur.Input

		// Primary, /kanban-identical source: the live `.status.json` context
		// snapshot. Gate on WindowSize > 0 exactly as props.go does so the home
		// row shows context whenever — and only when — /kanban would.
		ctx := fs.ReadStatus(m.orchestrator).Tokens.Context
		if ctx.WindowSize > 0 {
			t.contextUsage = ctx.UsagePct / 100
			t.contextLimit = int64(ctx.WindowSize)
			// Source the absolute "used" tokens straight from .status.json
			// TotalTokens — the SAME field /kanban renders as the numerator
			// (props.go:531) — so "used/limit" matches /kanban exactly rather
			// than a usage×limit re-derivation that could round differently.
			t.contextUsed = int64(ctx.TotalTokens)
		}

		// contextLimit fallback for agents with no live status snapshot.
		if t.contextLimit == 0 {
			if manifest, err := fs.ReadInitManifest(m.orchestrator); err == nil {
				t.contextLimit = manifestContextLimit(manifest)
			}
		}
	}
	// contextUsage fallback: when `.status.json` had no usable WindowSize, fall
	// back to the freshest notification Meta.Context.Usage in the UNFILTERED
	// session cache (NOT verbose-filtered m.messages — see the #442 note above).
	if t.contextUsage < 0 && m.sessionCache != nil {
		t.contextUsage = latestContextUsage(m.sessionCache.Entries())
	}
	return t
}

// latestContextUsage scans session entries from newest to oldest and returns the
// freshest notification's context-usage fraction (0..1), or -1 when no
// notification carries a usable context block. Entries are assumed
// chronologically ordered (the session cache sorts by timestamp), so the last
// matching entry is the freshest.
func latestContextUsage(entries []fs.SessionEntry) float64 {
	for i := len(entries) - 1; i >= 0; i-- {
		e := entries[i]
		if e.Type == "notification" && e.Meta != nil && e.Meta.Context != nil && e.Meta.Context.Usage >= 0 {
			return e.Meta.Context.Usage
		}
	}
	return -1
}

// manifestContextLimit resolves the model context window from a manifest read by
// fs.ReadInitManifest, returning 0 when unknown. It checks BOTH nestings because
// the two artifacts ReadInitManifest can return disagree on where the value sits
// (the em-1 "do not assume wrong nesting" trap):
//   - the kernel-resolved system/manifest.resolved.json carries it at the
//     TOP LEVEL (`manifest.context_limit`); `llm.context_limit` is absent there —
//     verified empirically across every live agent on this machine. The original
//     PR #441 read only `llm.context_limit` and so always missed it.
//   - the raw init.json fallback (stopped / never-booted agents) keeps the
//     saved-preset canonical shape `llm.context_limit` (see
//     internal/preset/preset.go NormalizeLegacyContextLimit). flattenInitManifest
//     does NOT hoist context_limit to top level, so that case needs the llm path.
//
// Top level wins when both are present.
func manifestContextLimit(manifest map[string]interface{}) int64 {
	if cl, ok := manifest["context_limit"].(float64); ok && cl > 0 {
		return int64(cl)
	}
	if llm, ok := manifest["llm"].(map[string]interface{}); ok {
		if cl, ok := llm["context_limit"].(float64); ok && cl > 0 {
			return int64(cl)
		}
	}
	return 0
}

// hasData reports whether any fragment is renderable. With nothing to show the
// caller omits the whole row.
func (t homeTelemetry) hasData() bool {
	return t.apiCalls > 0 || t.sessionTokens > 0 || t.contextUsage >= 0
}

// hasHomeTelemetry reports whether View() will render the additive telemetry row.
// It is the single predicate shared by the height budget (syncViewportHeight)
// and the renderer (View) so they can never disagree about whether the row
// occupies a line — a disagreement is exactly what clipped the status bar.
//
// It reads the last-known cached snapshot (m.homeTelemetry) ONLY — never
// gatherHomeTelemetry — so it stays on the UI hot path without touching
// sqlite/filesystem/JSONL. The snapshot is refreshed asynchronously by the
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
func mailFooterHeight(paletteLines, inputLines int, telemetryVisible bool) int {
	h := 1 + paletteLines + inputLines + 1 + 1 // sep + palette + input + border + status
	if telemetryVisible {
		h++
	}
	return h
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

	// Current-session token economy: api · tok · cache · tok/api. Each fragment
	// is dropped when its source is unknown so a missing piece never shows a 0.
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
	if t.apiCalls > 0 && t.sessionTokens > 0 {
		segs = append(segs, i18n.T("mail.telemetry_tok_per_api")+" "+humanizeTokenCount(avgPerCall(t.sessionTokens, t.apiCalls)))
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
			// No absolute "used" (notification fallback path): derive it from the
			// usage fraction so used/limit still renders rather than vanishing.
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
