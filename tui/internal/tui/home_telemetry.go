package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// Home telemetry row — a single muted line shown BELOW the input box and ABOVE
// the bottom path/shortcut status bar. It condenses the CURRENT SESSION's token
// economy and the live context-window pressure into one high-density line:
//
//	api 42  tok 181.6k  cache 88%  tok/api 4.3k    Current Context 186.5k/250.0k ▓▓▓▓░░ 73%
//
// All numbers are scoped to the current molt session (since the latest
// psyche_molt), NOT the whole-ledger lifetime total and NOT a single round:
//   - api:     LLM API calls this session
//   - tok:     total session tokens (input + output + thinking)
//   - cache:   cache-hit rate (cached / input)
//   - tok/api: average tokens per API call
// and, separately, the live context-window pressure with the gauge Jason liked:
//   - Current Context used/limit ▓▓░░ N%: tokens in use over the model's limit,
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
func (m MailModel) hasHomeTelemetry() bool {
	return m.gatherHomeTelemetry().hasData()
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
		segs = append(segs, i18n.T("mail.telemetry_tok")+" "+humanizeTokenCount(t.sessionTokens))
	}
	if t.inputTokens > 0 {
		segs = append(segs, i18n.T("mail.telemetry_cache")+" "+formatCacheRate(t.cached, t.inputTokens))
	}
	if t.apiCalls > 0 && t.sessionTokens > 0 {
		segs = append(segs, i18n.T("mail.telemetry_tok_per_api")+" "+humanizeTokenCount(avgPerCall(t.sessionTokens, t.apiCalls)))
	}

	// Current Context  186.5k/250.0k  ▓▓▓░░ 73%  — live context-window pressure
	// with the gauge Jason liked (msg 3195/3196). Jason's layout follow-up
	// (msg 3251): the scope reads as an explicit "Current Context" label, then
	// used/limit, then the bar, then the percentage on the RIGHT of the bar — so
	// the eye reads "what / how much / how full" in order, never the confusing
	// "73% / 250k" the percentage-first form produced. The used/limit + bar +
	// percentage are the core; the bar is dropped on narrow terminals (the
	// numbers stay), and "Current Context" + percentage always frame the metric.
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
	// i18n (mail.telemetry_session), never hard-coded. A middle-dot sets it off
	// from the metrics; the whole row is muted by StyleFaint below.
	segs = append([]string{i18n.T("mail.telemetry_session") + "  " + RuneBullet}, segs...)
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

// appendKanbanHint right-aligns the "/kanban for details" affordance against the
// terminal width, padding between the already-rendered left segment and the hint.
// It returns left unchanged when there isn't room for at least two spaces of gap,
// so the metrics never get clipped on narrow terminals.
func appendKanbanHint(left string, width int) string {
	hint := StyleFaint.Render(i18n.T("mail.telemetry_kanban_hint"))
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
