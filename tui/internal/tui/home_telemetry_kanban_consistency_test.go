package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
)

// Regression guard for Jason's "home context bar disagrees with /kanban" report
// (follow-up to PR #443).
//
// Root cause: gatherHomeTelemetry sourced context usage from the freshest
// notification Meta.Context.Usage in the session cache, which the kernel only
// refreshes when a notification is injected (per molt round). /kanban
// (props.go:518-535) reads the live `.status.json` Tokens.Context snapshot, which
// the kernel rewrites on a tight cadence. Between notifications the two diverge —
// observed live as 80% (stale notification) vs 74.6% (fresh .status.json).
//
// The fix makes the home row read the SAME `.status.json` snapshot /kanban does
// (gated identically on WindowSize > 0), with the notification value kept only as
// a fallback for agents that have no live status.

// writeStatusJSON writes a minimal .status.json into an agent dir with the given
// context window snapshot, matching the shape fs.ReadStatus parses.
func writeStatusJSON(t *testing.T, dir string, usagePct float64, total, window int) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	doc := fmt.Sprintf(`{"tokens":{"context":{"system_tokens":1000,"tools_tokens":2000,`+
		`"history_tokens":3000,"total_tokens":%d,"window_size":%d,"usage_pct":%g}}}`, total, window, usagePct)
	if err := os.WriteFile(filepath.Join(dir, ".status.json"), []byte(doc), 0o644); err != nil {
		t.Fatal(err)
	}
}

// newTelemetryModel builds a MailModel rooted at dir/orch and drives the deferred
// initial rebuild so the session cache is populated — the normal launch path.
func newTelemetryModel(t *testing.T, dir, orchDir string) MailModel {
	t.Helper()
	humanDir := filepath.Join(dir, "human")
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatal(err)
	}
	m := NewMailModel(humanDir, "human@local", "~", orchDir, "TestOrch", 50, dir, "en", false, 0)
	m, _ = m.Update(m.initialRebuild())
	return m
}

// When a live `.status.json` is present, the home row must read its usage_pct and
// window_size — exactly what /kanban shows — and must NOT use the stale
// notification value even when one is newer in the log.
func TestHomeTelemetryPrefersStatusJSONOverNotification(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// A notification carrying a STALE usage (80%) — the value the old code used.
	event := `{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.80}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}
	// The FRESH live snapshot /kanban reads: 74.6% of a 250k window.
	writeStatusJSON(t, orchDir, 74.6, 186500, 250000)

	m := newTelemetryModel(t, dir, orchDir)
	tel := m.gatherHomeTelemetry()

	// 74.6% from .status.json, not 80% from the stale notification.
	if got := tel.contextUsage * 100; got < 74.5 || got > 74.7 {
		t.Fatalf("contextUsage = %.2f%%, want 74.6%% (the live .status.json value /kanban shows, not the stale notification's 80%%)", got)
	}
	// The window must match /kanban's WindowSize, not the manifest.
	if tel.contextLimit != 250000 {
		t.Fatalf("contextLimit = %d, want 250000 (the .status.json window_size /kanban shows)", tel.contextLimit)
	}
	// "used" must come straight from .status.json TotalTokens — the same field
	// /kanban renders as the numerator — so "used/limit" matches /kanban exactly.
	if tel.contextUsed != 186500 {
		t.Fatalf("contextUsed = %d, want 186500 (the .status.json total_tokens /kanban shows)", tel.contextUsed)
	}
}

// With no `.status.json` (stopped / never-booted — /kanban shows no context
// section either), the home row falls back to the notification value so the bar
// degrades gracefully instead of vanishing, and stays independent of Ctrl+O.
func TestHomeTelemetryFallsBackToNotificationWithoutStatus(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.55}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}
	// No .status.json written.

	m := newTelemetryModel(t, dir, orchDir)
	tel := m.gatherHomeTelemetry()

	if got := tel.contextUsage * 100; got < 54.9 || got > 55.1 {
		t.Fatalf("contextUsage = %.2f%%, want 55%% (notification fallback when no .status.json)", got)
	}
}

// A `.status.json` with WindowSize == 0 is treated as "no live snapshot" (matching
// /kanban's WindowSize > 0 gate) and must fall back to the notification value.
func TestHomeTelemetryStatusZeroWindowFallsBack(t *testing.T) {
	dir := t.TempDir()
	orchDir := filepath.Join(dir, "orch")
	logsDir := filepath.Join(orchDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	event := `{"type":"notification","ts":1782000000,"summary":"sync","meta":{"context":{"usage":0.33}}}` + "\n"
	if err := os.WriteFile(filepath.Join(logsDir, "events.jsonl"), []byte(event), 0o644); err != nil {
		t.Fatal(err)
	}
	writeStatusJSON(t, orchDir, 0, 0, 0) // window_size 0 → not a usable snapshot

	m := newTelemetryModel(t, dir, orchDir)
	tel := m.gatherHomeTelemetry()

	if got := tel.contextUsage * 100; got < 32.9 || got > 33.1 {
		t.Fatalf("contextUsage = %.2f%%, want 33%% (fallback when .status.json has no window)", got)
	}
}

// Jason's layout follow-up (msg 3251): the context segment must read as
//
//	Current Context 186.5k/250.0k ▓▓▓░░ 75%
//
// — an explicit scope label, then used/limit, then the bar, then the percentage
// on the RIGHT of the bar. It must NOT render the confusing "75% / 250.0k"
// percentage-first form.
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
