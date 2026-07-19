// Package agentcounter counts running semantic agents from its own
// registry/manifest/heartbeat evidence. Process counts are not authority
// here. See CONTRACT.md for the full contract.
package agentcounter

// State reports whether a Count is trustworthy.
type State string

const (
	// StateKnown: Agents is a real count (Issues may still note degraded evidence).
	StateKnown State = "known"
	// StateUnknown: no trustworthy count; must not pass silently as zero.
	StateUnknown State = "unknown"
)

// Count is the result of one counting pass.
type Count struct {
	State  State
	Agents int
	Issues []string
}

// The port is CountRunningAgents(globalDir) Count, implemented privately
// per OS in adapter_posix.go and adapter_windows.go.
