// Package migrationguard decides whether one offline migration/destructive
// action on an agent working directory may proceed. Its only admissible
// evidence is a non-creating probe of the kernel's working-directory lock;
// process observation never affects the verdict. See CONTRACT.md.
package migrationguard

import "path/filepath"

// kernelLockFile is the lock the agent kernel holds while an agent runs
// (POSIX flock; Windows one-byte range lock at offset 0).
const kernelLockFile = ".agent.lock"

// Verdict classifies one migration decision. The zero value is Unknown so
// an uninitialized verdict can never read as permission.
type Verdict int

const (
	// Unknown means no usable lock evidence was obtained. Unknown blocks.
	Unknown Verdict = iota
	// Allow means the kernel lock was observed free at probe time.
	Allow
	// Block means the kernel lock is held.
	Block
)

// Decision is the guard's answer for one migration/destructive action.
type Decision struct {
	Verdict Verdict
	// Reason is a short human-readable justification for the verdict.
	Reason string
}

// Permitted reports whether the action may proceed. Only an explicit Allow
// permits; Block and Unknown both refuse.
func (d Decision) Permitted() bool { return d.Verdict == Allow }

// CheckAgentDir probes the kernel working-directory lock for agentDir and
// returns the verdict for one migration/destructive action there. The probe
// never creates, truncates, or deletes the lock file; a briefly acquired
// probe lock is released before returning. A verdict is evidence for this
// call only — recheck immediately before acting.
func CheckAgentDir(agentDir string) Decision {
	return probeKernelLock(filepath.Join(agentDir, kernelLockFile))
}
