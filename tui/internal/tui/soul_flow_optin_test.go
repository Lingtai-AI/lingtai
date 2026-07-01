package tui

import (
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

func TestResolveSoulDelay(t *testing.T) {
	cases := []struct {
		name    string
		raw     string
		enabled bool
		want    *float64 // nil means "expect nil"
	}{
		{"off blank → nil (kernel default, harmless while disabled)", "", false, nil},
		{"off explicit → honored", "300", false, f(300)},
		{"on blank → default cadence", "", true, f(preset.DefaultSoulFlowCadence)},
		{"on explicit → honored", "120", true, f(120)},
		{"on zero → default cadence (0 is not a real cadence)", "0", true, f(preset.DefaultSoulFlowCadence)},
		{"off garbage → nil", "abc", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveSoulDelay(tc.raw, tc.enabled)
			switch {
			case tc.want == nil && got != nil:
				t.Errorf("resolveSoulDelay(%q,%v) = %v, want nil", tc.raw, tc.enabled, *got)
			case tc.want != nil && got == nil:
				t.Errorf("resolveSoulDelay(%q,%v) = nil, want %v", tc.raw, tc.enabled, *tc.want)
			case tc.want != nil && got != nil && *got != *tc.want:
				t.Errorf("resolveSoulDelay(%q,%v) = %v, want %v", tc.raw, tc.enabled, *got, *tc.want)
			}
		})
	}
}

func f(v float64) *float64 { return &v }
