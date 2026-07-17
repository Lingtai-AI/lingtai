package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

func TestMaybeCheckAndPromptAlwaysCallsInspectKernel(t *testing.T) {
	for _, interactive := range []bool{true, false} {
		inspectCalls := 0
		maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
			Input:       strings.NewReader("n\n"),
			Output:      &bytes.Buffer{},
			Interactive: func() bool { return interactive },
			InspectKernelFunc: func(string) config.KernelStatus {
				inspectCalls++
				return config.KernelStatus{Installed: "0.9.7", Latest: "0.9.7"}
			},
		})
		if inspectCalls != 1 {
			t.Fatalf("interactive=%v: InspectKernel calls = %d, want 1", interactive, inspectCalls)
		}
	}
}

type kernelPromptCase struct {
	name           string
	input          string
	status         config.KernelStatus
	nonInteractive bool
	wantUpdated    bool
	wantCalls      int
	wantPrompt     []string
	forbidPrompt   []string
}

func TestMaybeCheckAndPromptStateMachine(t *testing.T) {
	stale := config.KernelStatus{Installed: "0.9.6", Latest: "0.9.7", NeedsUpdate: true}
	missing := config.KernelStatus{NeedsUpdate: true}
	tests := []kernelPromptCase{
		{name: "current", input: "y\n", status: config.KernelStatus{Installed: "0.9.7", Latest: "0.9.7"}, wantPrompt: []string{}},
		{name: "editable", input: "y\n", status: config.KernelStatus{Installed: "0.9.6", Editable: true}, wantPrompt: []string{}},
		{name: "inspect failure", input: "y\n", status: config.KernelStatus{}, wantPrompt: []string{}},
		{name: "stale decline", input: "n\n", status: stale, wantPrompt: []string{"0.9.6", "0.9.7", "[y/N]"}},
		{name: "missing decline", input: "n\n", status: missing, wantPrompt: []string{"not installed", "[y/N]"}, forbidPrompt: []string{"update available", "→"}},
		{name: "missing EOF", status: missing},
		{name: "stale EOF", status: stale},
		{name: "stale default", input: "\n", status: stale},
		{name: "missing non-TTY", input: "y\n", status: missing, nonInteractive: true},
		{name: "stale non-TTY", input: "y\n", status: stale, nonInteractive: true},
		{name: "missing consent", input: "y\n", status: missing, wantUpdated: true, wantCalls: 1},
		{name: "stale consent", input: "y\n", status: stale, wantUpdated: true, wantCalls: 1},
		{name: "update failure", input: "y\n", status: stale, wantCalls: 1},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var out bytes.Buffer
			inspectCalls, updateCalls := 0, 0
			updated := maybeCheckAndPromptKernelUpgradeWithOptions("/tmp/lingtai-test", kernelUpgradePromptOptions{
				Input:       strings.NewReader(tc.input),
				Output:      &out,
				Interactive: func() bool { return !tc.nonInteractive },
				InspectKernelFunc: func(string) config.KernelStatus {
					inspectCalls++
					return tc.status
				},
				RunKernelUpdate: func(globalDir string, force bool) config.DoctorReport {
					updateCalls++
					if globalDir != "/tmp/lingtai-test" || force {
						t.Fatalf("RunKernelUpdate args = (%q, %v), want (%q, false)", globalDir, force, "/tmp/lingtai-test")
					}
					if tc.name == "update failure" {
						return config.DoctorReport{}
					}
					return config.DoctorReport{Healthy: true}
				},
			})
			if inspectCalls != 1 {
				t.Fatalf("InspectKernel calls = %d, want 1", inspectCalls)
			}
			if updated != tc.wantUpdated {
				t.Fatalf("updated = %v, want %v; output=%q", updated, tc.wantUpdated, out.String())
			}
			if updateCalls != tc.wantCalls {
				t.Fatalf("RunKernelUpdate calls = %d, want %d; output=%q", updateCalls, tc.wantCalls, out.String())
			}
			for _, text := range tc.wantPrompt {
				if !strings.Contains(out.String(), text) {
					t.Errorf("prompt %q missing from %q", text, out.String())
				}
			}
			for _, text := range tc.forbidPrompt {
				if strings.Contains(out.String(), text) {
					t.Errorf("prompt %q unexpectedly present in %q", text, out.String())
				}
			}
			if tc.nonInteractive && out.Len() != 0 {
				t.Fatalf("non-interactive launch printed a prompt: %q", out.String())
			}
		})
	}
}

func TestInspectKernelIssuesNoMutatingCommands(t *testing.T) {
	status := config.InspectKernel(t.TempDir())
	if !status.NeedsUpdate || status.Installed != "" {
		t.Fatalf("missing venv status = %+v, want NeedsUpdate=true and no installed version", status)
	}
	if len(status.Lines) == 0 || !strings.Contains(status.Lines[0].Text, "not found") {
		t.Fatalf("expected a 'venv not found' diagnostic line, got %+v", status.Lines)
	}
}
