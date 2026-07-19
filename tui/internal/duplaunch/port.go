// Package duplaunch is the DuplicateLaunchCheck policy interface: it decides
// allow/block/unknown for launching one agent into one working directory,
// judged solely by this package's private, non-creating probe of the kernel's
// .agent.lock lease. Process observation is never authority here, and Unknown
// blocks. See CONTRACT.md.
package duplaunch

import "path/filepath"

// kernelLockFile is the lease a running kernel holds inside its workdir.
const kernelLockFile = ".agent.lock"

// Verdict is the tri-state duplicate-launch decision.
type Verdict string

const (
	// Allow means the probe affirmatively found no live lock holder.
	Allow Verdict = "allow"
	// Block means a live holder of the workdir lock was observed.
	Block Verdict = "block"
	// Unknown means no usable lock evidence; callers must refuse the launch.
	Unknown Verdict = "unknown"
)

// Decision is a verdict plus the lock evidence it rests on.
type Decision struct {
	Verdict Verdict
	Reason  string
}

// Check reports whether launching an agent in agentDir would duplicate a live
// one. It never creates, truncates, or removes anything on disk and never
// consults process listings.
func Check(agentDir string) Decision {
	return probeKernelLock(filepath.Join(agentDir, kernelLockFile))
}
