package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestNoProjectGate_PrecedesGlobalDirAndMigrations is a source-order smoke
// test (design doc Gate 1: "prove the launcher truly sits before
// config.GlobalDir(), not merely before InitProject"). It reads main.go's
// own source and asserts the no-project probe (tui.ProbeNoProjectPure)
// appears strictly before every listed eager-write call in main()'s body.
// This is a coarse but meaningful regression guard: if a future edit moves
// config.GlobalDir() (or globalmigrate.Run, or process.InitProject, ...)
// above the gate, this test fails immediately instead of silently
// reintroducing the exact defect the design doc identified in the
// pre-existing code.
func TestNoProjectGate_PrecedesGlobalDirAndMigrations(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)

	// Every one of these must appear strictly AFTER the gate call within
	// main(). We search only within the func main() body to avoid false
	// positives from runNoProjectLauncher/runCreatedProject's own bodies
	// (which correctly call some of these — e.g. runCreatedProject calls
	// config.GlobalDir for the resolved project, which is fine, that's
	// POST-decision).
	mainBody := extractFuncBody(t, text, "func main() {")

	// Strip line comments before searching so a doc comment that merely
	// NAMES one of these calls (as this very gate's explanatory comment
	// does, to describe what it precedes) can never produce a false
	// "appears before the gate" positive. This keeps the test honest about
	// finding real call sites, not prose mentions.
	codeOnly := stripLineComments(mainBody)
	gateIdxCodeOnly := strings.Index(codeOnly, "tui.ProbeNoProjectPure(projectDir)")
	if gateIdxCodeOnly < 0 {
		t.Fatal("gate call not found in comment-stripped func main() body")
	}

	eagerWrites := []string{
		"config.GlobalDir()",
		"globalmigrate.Run(",
		"config.MigrateLegacyLanguage(",
		"config.LoadTUIConfig(",
		"tui.ValidateCodexAuthOnStartup(",
		"showWelcome(",
		"maybeShowAgentCount(",
		"process.InitProject(",
		"config.Register(",
		"preset.PopulateBundledLibrary(",
		"config.EnsureRuntime(",
		"preset.Bootstrap(",
	}
	for _, call := range eagerWrites {
		idx := strings.Index(codeOnly, call)
		if idx < 0 {
			// Some calls (e.g. config.EnsureRuntime) only appear inside the
			// "!needsFirstRun" branch further down — still fine, just
			// confirm it's after the gate if present at all.
			continue
		}
		if idx < gateIdxCodeOnly {
			t.Fatalf("%s appears BEFORE the no-project gate in func main() (offset %d < gate offset %d) — this reintroduces the eager-write defect the launcher exists to fix", call, idx, gateIdxCodeOnly)
		}
	}
}

// TestNoProjectGate_FailsClosedOnProbeError is a source-order companion
// proving the gate handles ProbeNoProjectPure's error return by exiting
// BEFORE any eager-write call, rather than falling through into the normal
// startup pipeline on an unexpected error (the exact fail-open defect a
// parent review found in the original single-return-value probe: any
// non-ENOENT Lstat error was silently treated as "project exists", which let
// startup proceed to config.GlobalDir()/migrations without the gate ever
// making a real decision). This asserts the SOURCE shape: `probeErr != nil`
// is checked, and that check's block appears before every eager-write call
// listed above — not merely that error handling exists somewhere.
func TestNoProjectGate_FailsClosedOnProbeError(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	mainBody := extractFuncBody(t, string(src), "func main() {")
	codeOnly := stripLineComments(mainBody)

	if !strings.Contains(codeOnly, "noProject, probeErr := tui.ProbeNoProjectPure(projectDir)") {
		t.Fatal("expected func main() to capture both the bool and error return of tui.ProbeNoProjectPure")
	}
	errCheckIdx := strings.Index(codeOnly, "if probeErr != nil {")
	if errCheckIdx < 0 {
		t.Fatal("expected an explicit `if probeErr != nil` check immediately after calling ProbeNoProjectPure")
	}
	exitBlock := codeOnly[errCheckIdx:]
	if !strings.Contains(exitBlock[:min(len(exitBlock), 200)], "os.Exit(1)") {
		t.Fatal("expected the probeErr != nil branch to os.Exit(1) — fail closed, never fall through to normal startup on an unexpected probe error")
	}

	gateIdxCodeOnly := strings.Index(codeOnly, "tui.ProbeNoProjectPure(projectDir)")
	if errCheckIdx < gateIdxCodeOnly {
		t.Fatal("probeErr check must appear after the ProbeNoProjectPure call")
	}

	eagerWrites := []string{
		"config.GlobalDir()",
		"globalmigrate.Run(",
		"process.InitProject(",
		"config.Register(",
	}
	for _, call := range eagerWrites {
		idx := strings.Index(codeOnly, call)
		if idx < 0 {
			continue
		}
		if idx < errCheckIdx {
			t.Fatalf("%s appears before the probeErr fail-closed check (offset %d < %d)", call, idx, errCheckIdx)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// stripLineComments removes everything from "//" to end-of-line on every
// line. Deliberately crude (does not understand string literals containing
// "//" or block comments) — sufficient for this file's own well-formatted
// Go source where no string literal contains that sequence.
func stripLineComments(src string) string {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		if idx := strings.Index(line, "//"); idx >= 0 {
			lines[i] = line[:idx]
		}
	}
	return strings.Join(lines, "\n")
}

// extractFuncBody returns the text of the named function's body (from its
// signature to the matching closing brace at column 0), using simple brace
// counting. Good enough for a single well-formatted Go source file in a
// source-order smoke test; not a general Go parser.
func extractFuncBody(t *testing.T, src, signature string) string {
	t.Helper()
	start := strings.Index(src, signature)
	if start < 0 {
		t.Fatalf("signature %q not found", signature)
	}
	depth := 0
	i := start
	bodyStart := -1
	for ; i < len(src); i++ {
		switch src[i] {
		case '{':
			if depth == 0 {
				bodyStart = i + 1
			}
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[bodyStart:i]
			}
		}
	}
	t.Fatalf("unbalanced braces scanning for %q", signature)
	return ""
}

// TestNoProjectGate_SubcommandsNeverCallLauncher proves every CLI
// subcommand (list, clean, doctor, spawn, ...) keeps its existing
// early-return behavior — none of them reference the launcher. Source-scan
// companion to Gate 5 ("final diff doesn't rewrite clean/portal/setup").
func TestNoProjectGate_SubcommandsNeverCallLauncher(t *testing.T) {
	src, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}
	text := string(src)

	subcommandFns := regexp.MustCompile(`func (purgeMain|listMain|cleanMain|suspendMain|postmanMain|bootstrapMain|presetsMain|spawnMain|selfUpdateMain|doctorMain)\(\)`).FindAllStringSubmatch(text, -1)
	if len(subcommandFns) == 0 {
		t.Fatal("no subcommand *Main functions found — has main.go been restructured?")
	}
	for _, m := range subcommandFns {
		name := m[1]
		body := extractFuncBody(t, text, "func "+name+"()")
		if strings.Contains(body, "runNoProjectLauncher") || strings.Contains(body, "ProbeNoProjectPure") || strings.Contains(body, "NewLauncherRootModel") {
			t.Errorf("subcommand %s references the no-project launcher — subcommands must keep their existing early-return behavior unchanged", name)
		}
	}
}
