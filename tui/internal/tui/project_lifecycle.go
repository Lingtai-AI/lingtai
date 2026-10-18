package tui

import (
	"fmt"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/process"
)

// A narrow composition seam, also usable by caller fixtures. Never a runtime
// fallback: production always calls the one bound kernel exchange.
var externalProjectLifecycle = process.RunProjectLifecycle

func projectLifecycleNative() process.ProjectLifecycleNative {
	return process.ProjectLifecycleNative{
		Puffo:       hasPuffoManagedAgent,
		ProbeLock:   tryLock,
		ResetPreset: resetActivePresetToDefault,
		SetPreset:   setActivePreset,
		Heartbeat:   waitForLaunchHeartbeat,
	}
}

func projectLifecycleFailures(result process.ProjectLifecycleResult) []string {
	var failures []string
	for _, failure := range result.Failures {
		failures = append(failures, fmt.Sprintf("%s (%s)", filepath.Base(failure.Directory), firstLine(failure.Err)))
	}
	return failures
}

func clearLifecycleNative(command string, cfg clearWaitConfig, deps clearContextDeps) process.ProjectLifecycleNative {
	native := projectLifecycleNative()
	var before int
	var hasBefore bool
	var offset int64
	native.ClearAlive = func(dir string) bool { return deps.isAlive(dir, 3.0) }
	// Production deps.revive is reviveDir, now the Core-owned revive action.
	// Retain the existing explicit dependency seam and exact clear wait helpers.
	native.ReviveForClear = func(dir string) error { return deps.revive(command, dir) }
	native.ClearAliveWait = func(dir string) error { return waitForAgentAlive(dir, cfg.aliveTimeout, cfg.pollInterval, deps) }
	native.ClearCapture = func(dir string) {
		before, hasBefore = readMoltCount(dir)
		offset = eventLogOffset(dir)
	}
	native.ClearCompletionWait = func(dir string) error { return waitForClearCompletion(dir, before, hasBefore, offset, cfg, deps) }
	native.ClearSuspend = func(dir string) error { return suspendTemporaryClearAgent(dir, cfg, deps) }
	return native
}

// Unknown cleanup never emits the success message that permits root re-init.
type nirvanaCleanFailedMsg struct{ err error }
