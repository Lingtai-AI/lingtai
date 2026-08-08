package main

import (
	"testing"
)

// TestStartupDecision covers the R1/R2/R3 launch-mode decision table
// (tui/CONTRACT.md). Content-based (fable F7): a present-but-keyless
// config.json degrades exactly like an absent one.
func TestStartupDecision(t *testing.T) {
	cases := []struct {
		name          string
		orchestrators int
		resolvedKeys  map[string]string
		configOK      bool
		mirrorKeys    map[string]string
		wantFirstRun  bool
		wantRecovery  bool
		wantDegraded  bool
	}{
		{
			name: "no agents → first-run",
			// R1 fail even when keys/config are healthy.
			orchestrators: 0, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: map[string]string{"DEEPSEEK_API_KEY": "x"},
			wantFirstRun: true,
		},
		{
			name: "config missing + env has keys → degraded",
			// The 2026-08-08 incident: agents keep running, mirror lost.
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: false, mirrorKeys: nil,
			wantDegraded: true,
		},
		{
			name: "config missing + no keys → recovery",
			// R2 fail: real setup.
			orchestrators: 1, resolvedKeys: map[string]string{}, configOK: false, mirrorKeys: nil,
			wantRecovery: true,
		},
		{
			name: "config present but keys mirror empty + env has keys → degraded (F7)",
			// Only legacy `language` in config.json: present-but-keyless mirror.
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: nil,
			wantDegraded: true,
		},
		{
			name: "everything present → normal",
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: map[string]string{"DEEPSEEK_API_KEY": "x"},
		},
		{
			name: "env empty but mirror has keys → normal (legacy config-only)",
			// ResolveKeys fills from the mirror, so R2 is satisfied and the
			// mirror is healthy — legacy setups without .env still launch.
			orchestrators: 1, resolvedKeys: map[string]string{"DEEPSEEK_API_KEY": "x"}, configOK: true, mirrorKeys: map[string]string{"DEEPSEEK_API_KEY": "x"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			firstRun, recovery, degraded := startupDecision(tc.orchestrators, tc.resolvedKeys, tc.configOK, tc.mirrorKeys)
			if firstRun != tc.wantFirstRun || recovery != tc.wantRecovery || degraded != tc.wantDegraded {
				t.Fatalf("startupDecision(%d, %v, %v, %v) = (firstRun=%v, recovery=%v, degraded=%v), want (%v, %v, %v)",
					tc.orchestrators, tc.resolvedKeys, tc.configOK, tc.mirrorKeys,
					firstRun, recovery, degraded, tc.wantFirstRun, tc.wantRecovery, tc.wantDegraded)
			}
		})
	}
}
