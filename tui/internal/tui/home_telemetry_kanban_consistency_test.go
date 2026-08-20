package tui

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
)

// Regression guard for Jason's "home context bar disagrees with /kanban" report
// (follow-up to PR #443), now re-anchored on the kernel Agent Record migration
// (kernel #1423): gatherHomeTelemetry's context usage/window/used tokens come
// from ONE canonical `system/agent_record.json` snapshot — the SAME
// lingtai.agent_record/v1 record schema the kernel's session_stats module
// throttles and rewrites live (default every 5s, session_stats_refresh_seconds).
// There is no independent notification/events.jsonl fallback any more: a
// missing or context-less record means the row has no context segment,
// exactly like /kanban's own WindowSize > 0 gate degrades to no context
// section for a stopped/never-booted agent.

// When a live Agent record is present, the home row must read its context
// usage/window/used-tokens exactly as published.
func TestHomeTelemetryReadsContextFromAgentRecord(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	writeHomeAgentRecord(t, orchDir, `{
  "schema": "lingtai.agent_record/v1",
  "usage": {"context_used_tokens": 186500, "context_limit_tokens": 250000, "context_usage_pct": 74.6}
}`)

	m := newTelemetryModel(t, dir, orchDir)
	tel := m.gatherHomeTelemetry()

	if got := tel.contextUsage * 100; got < 74.5 || got > 74.7 {
		t.Fatalf("contextUsage = %.2f%%, want 74.6%% (the live Agent record value)", got)
	}
	if tel.contextLimit != 250000 {
		t.Fatalf("contextLimit = %d, want 250000 (the Agent record context_limit_tokens)", tel.contextLimit)
	}
	if tel.contextUsed != 186500 {
		t.Fatalf("contextUsed = %d, want 186500 (the Agent record context_used_tokens)", tel.contextUsed)
	}
}

// With no Agent record at all (stopped / never-booted — /kanban shows no
// context section either), the home row has no context segment. There is no
// notification-log fallback: the direct-migration contract treats an absent
// record as a bounded unavailable state, not a second source of truth.
func TestHomeTelemetryNoContextWithoutAgentRecord(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")

	m := newTelemetryModel(t, dir, orchDir)
	tel := m.gatherHomeTelemetry()

	if tel.contextUsage != -1 || tel.contextUsed != 0 || tel.contextLimit != 0 {
		t.Fatalf("context = %+v, want the unavailable sentinel with no Agent record present", tel)
	}
}

// An Agent record with context_limit_tokens == 0 (no context window known yet)
// is treated as "no live context snapshot", mirroring /kanban's WindowSize > 0
// gate — never a fabricated used/limit pair.
func TestHomeTelemetryZeroContextLimitOmitsContext(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	writeHomeAgentRecord(t, orchDir, `{"schema": "lingtai.agent_record/v1", "usage": {"context_used_tokens": 0, "context_limit_tokens": 0, "context_usage_pct": 0}}`)

	m := newTelemetryModel(t, dir, orchDir)
	tel := m.gatherHomeTelemetry()

	if tel.contextUsage != -1 || tel.contextUsed != 0 || tel.contextLimit != 0 {
		t.Fatalf("context = %+v, want the unavailable sentinel when context_limit_tokens is 0", tel)
	}
}

// Jason's layout follow-up (msg 3251): the context segment must read as
//
//	ctx 186.5k/250.0k ▓▓▓░░ 75%
//
// — an explicit scope label, then used/limit, then the bar, then the percentage
// on the RIGHT of the bar. It must NOT render the confusing "75% / 250.0k"
// percentage-first form. This is a pure format test over the already-resolved
// homeTelemetry struct, independent of the Agent Record migration.
func TestFormatHomeTelemetryContextLayout(t *testing.T) {
	tel := homeTelemetry{
		apiCalls: 42, sessionTokens: 181585, inputTokens: 181585,
		cached: 180224, contextUsed: 186500, contextLimit: 250000, contextUsage: 0.746,
	}
	got := formatHomeTelemetry(tel, 160)

	label := i18n.T("mail.telemetry_context")
	if label == "mail.telemetry_context" {
		t.Fatal("i18n key mail.telemetry_context is missing a translation")
	}
	// Explicit localized scope label present.
	if !strings.Contains(got, label) {
		t.Errorf("context row %q is missing the %q scope label", got, label)
	}
	// used/limit present in human form.
	if !strings.Contains(got, "186.5k/250.0k") {
		t.Errorf("context row %q must show used/limit as 186.5k/250.0k", got)
	}
	// Percentage present and AFTER the used/limit + bar, never before used/limit.
	usedLimit := strings.Index(got, "186.5k/250.0k")
	pct := strings.Index(got, "75%")
	if pct < 0 {
		t.Fatalf("context row %q is missing the 75%% percentage", got)
	}
	if pct < usedLimit {
		t.Errorf("percentage must follow used/limit (and the bar), not precede it, in %q (pct@%d used/limit@%d)", got, pct, usedLimit)
	}
	bar := strings.IndexRune(got, '▓')
	if bar >= 0 && pct < bar {
		t.Errorf("percentage must sit to the RIGHT of the bar in %q (pct@%d bar@%d)", got, pct, bar)
	}
	// The confusing "% / limit" percentage-first form must never appear.
	if strings.Contains(got, "75% / 250.0k") {
		t.Errorf("context row %q renders the confusing percentage-first form", got)
	}
}

// On a terminal too narrow for the bar, used/limit and the percentage must still
// render (the bar is the only droppable core element) — the layout never clips
// the numbers.
func TestFormatHomeTelemetryContextLayoutNarrow(t *testing.T) {
	tel := homeTelemetry{
		apiCalls: 42, sessionTokens: 181585, inputTokens: 181585,
		cached: 180224, contextUsed: 186500, contextLimit: 250000, contextUsage: 0.746,
	}
	got := formatHomeTelemetry(tel, 30) // below homeTelemetryBarMinWidth → bar hidden

	if strings.ContainsRune(got, '▓') || strings.ContainsRune(got, '░') {
		t.Errorf("narrow row %q must drop the bar", got)
	}
	if !strings.Contains(got, "186.5k/250.0k") {
		t.Errorf("narrow row %q must keep used/limit", got)
	}
	if !strings.Contains(got, "75%") {
		t.Errorf("narrow row %q must keep the percentage", got)
	}
}

// The "/kanban for details" hint is right-aligned on a wide terminal and present
// in the rendered row.
func TestFormatHomeTelemetryShowsKanbanHint(t *testing.T) {
	tel := homeTelemetry{
		apiCalls: 42, sessionTokens: 181585, inputTokens: 181585,
		cached: 180224, contextUsed: 182500, contextLimit: 250000, contextUsage: 0.73,
	}
	got := formatHomeTelemetry(tel, 160)

	hint := i18n.T("mail.telemetry_kanban_hint")
	if hint == "mail.telemetry_kanban_hint" {
		t.Fatal("i18n key mail.telemetry_kanban_hint is missing a translation")
	}
	if !strings.Contains(got, hint) {
		t.Errorf("telemetry row %q is missing the %q hint", got, hint)
	}
	// The hint must sit on the RIGHT: after the metrics, with padding before it.
	hi, api := strings.Index(got, hint), strings.Index(got, i18n.T("mail.telemetry_api"))
	if hi < 0 || api < 0 || hi < api {
		t.Errorf("/kanban hint must follow the metrics on the right in %q (hint@%d api@%d)", got, hi, api)
	}
}

// On a terminal too narrow to right-align the hint without colliding with the
// metrics, the hint is dropped so the numbers keep the space — the row never
// wraps or clips.
func TestFormatHomeTelemetryDropsKanbanHintWhenNarrow(t *testing.T) {
	tel := homeTelemetry{
		apiCalls: 42, sessionTokens: 181585, inputTokens: 181585,
		cached: 180224, contextUsed: 182500, contextLimit: 250000, contextUsage: 0.73,
	}
	got := formatHomeTelemetry(tel, 30) // narrow

	if strings.Contains(got, i18n.T("mail.telemetry_kanban_hint")) {
		t.Errorf("narrow row %q must drop the /kanban hint rather than collide with the metrics", got)
	}
	// The session metrics themselves must still render.
	if !strings.Contains(got, i18n.T("mail.telemetry_session")) {
		t.Errorf("narrow row %q dropped the session label — the hint drop must not affect the metrics", got)
	}
}
