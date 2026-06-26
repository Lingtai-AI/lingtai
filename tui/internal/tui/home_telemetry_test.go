package tui

import (
	"strings"
	"testing"
)

// The home telemetry row condenses the CURRENT SESSION's token economy
// (api · tok · cache · tok/api) plus the live context pressure gauge into one
// muted line: "api 42  tok 181.6k  cache 88%  tok/api 4.3k  Current Context 146.0k/200.0k ▓▓▓░░ 73%". It
// is scalar-only (never the _meta block hidden by PR #440), scoped to the
// current molt session (never the global lifetime total), and hides gracefully
// when no data is available.
func TestFormatHomeTelemetry(t *testing.T) {
	tests := []struct {
		name    string
		tel     homeTelemetry
		width   int
		exact   string   // when set, output must equal this exactly ("" = hidden)
		want    []string // substrings that must appear
		notWant []string // substrings that must NOT appear
	}{
		{
			name:  "no data — row hidden",
			tel:   homeTelemetry{contextUsage: -1},
			width: 120,
			exact: "",
		},
		{
			name: "full current-session economy + context + bar (wide)",
			// 42 calls, 181,585 tokens, 180,224 cached of 181,585 input (99.3%),
			// avg 4,323 tok/call. Context 146.0k/200.0k, 73% → amber bar.
			tel: homeTelemetry{
				apiCalls: 42, sessionTokens: 181585, inputTokens: 181585,
				cached: 180224, contextUsed: 146000, contextLimit: 200000, contextUsage: 0.73,
			},
			width: 120,
			// api count, humanized token total, cache rate, tok/api, then the
			// Current Context scope label, used/limit, bar, and percentage.
			want: []string{"api", "42", "tok", "181.6k", "cache", "%", "tok/api", "Current Context", "146.0k/200.0k", "▓", "░", "73%"},
		},
		{
			name: "no api calls yet — drop api & tok/api, keep tokens",
			tel: homeTelemetry{
				apiCalls: 0, sessionTokens: 5000, inputTokens: 5000, cached: 0,
				contextUsage: -1,
			},
			width: 120,
			want:  []string{"tok", "5.0k"},
			// no api count, no per-call average, no context segment, no bar
			notWant: []string{"api", "tok/api", "Current Context", "▓"},
		},
		{
			name: "context only — no session ledger yet",
			tel: homeTelemetry{
				apiCalls: 0, sessionTokens: 0, inputTokens: 0,
				contextUsed: 64000, contextLimit: 128000, contextUsage: 0.5,
			},
			width: 120,
			want:  []string{"Current Context", "64.0k/128.0k", "50%", "▓"},
			// nothing to show for the token economy
			notWant: []string{"tok", "api", "cache"},
		},
		{
			name: "narrow terminal (<40) keeps numbers, hides bar",
			tel: homeTelemetry{
				apiCalls: 42, sessionTokens: 18432, inputTokens: 18432,
				cached: 9000, contextUsage: 0.14,
			},
			width:   30,
			want:    []string{"api", "42", "tok", "18.4k", "cache", "Current Context", "14%"},
			notWant: []string{"▓", "░"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatHomeTelemetry(tt.tel, tt.width)
			if tt.exact != "" || !tt.tel.hasData() {
				if got != tt.exact {
					t.Fatalf("got %q, want exact %q", got, tt.exact)
				}
				return
			}
			for _, w := range tt.want {
				if !strings.Contains(got, w) {
					t.Errorf("telemetry %q missing %q", got, w)
				}
			}
			for _, nw := range tt.notWant {
				if strings.Contains(got, nw) {
					t.Errorf("telemetry %q should not contain %q", got, nw)
				}
			}
			// The label must resolve through i18n, not leak the raw key — guards
			// against the localized `mail.telemetry_*` keys going missing.
			if strings.Contains(got, "mail.telemetry") {
				t.Errorf("telemetry %q leaked a raw i18n key (missing translation)", got)
			}
		})
	}
}

func TestRenderContextBar(t *testing.T) {
	// 30% of 10 cells = 3 filled, 7 empty.
	bar := renderContextBar(30, 10)
	if got := strings.Count(bar, "▓"); got != 3 {
		t.Errorf("filled cells = %d, want 3 (bar=%q)", got, bar)
	}
	if got := strings.Count(bar, "░"); got != 7 {
		t.Errorf("empty cells = %d, want 7 (bar=%q)", got, bar)
	}

	// Clamping: over 100 and under 0 must not panic or overflow the width.
	if got := strings.Count(renderContextBar(250, 8), "▓"); got != 8 {
		t.Errorf("pct>100 should fill all 8 cells, got %d", got)
	}
	if got := strings.Count(renderContextBar(-5, 8), "▓"); got != 0 {
		t.Errorf("pct<0 should fill 0 cells, got %d", got)
	}
}

func TestHomeTelemetryBarWidth(t *testing.T) {
	if w := homeTelemetryBarWidth(30); w != 0 {
		t.Errorf("width 30 (<40) should hide bar (0), got %d", w)
	}
	if w := homeTelemetryBarWidth(40); w <= 0 {
		t.Errorf("width 40 should show a bar, got %d", w)
	}
	// Adaptive but capped: a very wide terminal must not produce a ruler.
	if w := homeTelemetryBarWidth(4000); w != homeTelemetryBarMax {
		t.Errorf("very wide terminal should cap bar at %d, got %d", homeTelemetryBarMax, w)
	}
}

func TestContextBarColorThresholds(t *testing.T) {
	// The three muted theme colors must be distinct across the thresholds so the
	// bar visibly escalates calm → caution → alarm. We can't assert exact RGB
	// without the theme initialized, but we can assert the mapping picks the
	// expected named color variables.
	if contextBarColor(50) != ColorActive {
		t.Error("50% should map to ColorActive (calm)")
	}
	if contextBarColor(75) != ColorAccent {
		t.Error("75% should map to ColorAccent (caution)")
	}
	if contextBarColor(95) != ColorSuspended {
		t.Error("95% should map to ColorSuspended (alarm)")
	}
}
