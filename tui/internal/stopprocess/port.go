// Package stopprocess delivers one native stop request to an
// identity-verified process. Delivery is not exit proof; see CONTRACT.md.
package stopprocess

import "errors"

// Kind selects the native stop request: Graceful (POSIX SIGTERM; unsupported
// on Windows) or Forceful (SIGKILL / TerminateProcess).
type Kind int

const (
	Graceful Kind = iota
	Forceful
)

// Target names the process to stop; Executable is its expected base name.
type Target struct {
	PID        int
	Executable string
}

// Outcome classifies a single delivery attempt.
type Outcome int

const (
	Delivered     Outcome = iota
	StaleIdentity         // PID alive but not Target.Executable
	AccessDenied
	AlreadyAbsent
	Unsupported // platform cannot deliver the requested Kind
	Unknown     // undiagnosed verification or delivery failure
)

var outcomeNames = [...]string{"delivered", "stale-identity", "access-denied", "already-absent", "unsupported", "unknown"}

func (o Outcome) String() string {
	if o < 0 || int(o) >= len(outcomeNames) {
		return "invalid-outcome"
	}
	return outcomeNames[o]
}

// Result reports one delivery attempt; Err details AccessDenied and Unknown.
type Result struct {
	Outcome Outcome
	Err     error
}

// Deliver verifies Target identity and, on match, delivers one native stop
// request without waiting for exit and without escalation.
func Deliver(t Target, k Kind) Result {
	if k != Graceful && k != Forceful {
		return Result{Outcome: Unsupported}
	}
	if t.PID <= 0 || t.Executable == "" {
		return Result{Outcome: Unknown, Err: errors.New("stopprocess: need positive PID and executable name")}
	}
	return deliver(t, k)
}
