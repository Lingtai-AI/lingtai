// Package termobs answers exactly one question: has one exact,
// previously identified process incarnation exited? It observes only —
// it never signals, stops, or escalates, and it never converts an
// unknown or failed observation into "exited". See CONTRACT.md.
package termobs

import "errors"

// Status classifies one observation of one incarnation.
type Status string

const (
	StatusRunning Status = "running" // same PID + start token still held
	StatusExited  Status = "exited"  // the exact incarnation certainly exited
	StatusUnknown Status = "unknown" // OS refused the observation; no answer
	StatusError   Status = "error"   // observation failed; no conclusion
)

// Incarnation pins one exact process incarnation: a PID plus the OS
// start token captured by Identify while the process held the PID.
// The zero Incarnation is invalid and always observes as StatusError.
type Incarnation struct {
	PID   int
	token uint64
}

// Observation is the result of a single Observe call.
type Observation struct {
	Status Status
	Err    error // detail for StatusUnknown / StatusError, else nil
}

// ErrNotFound reports that Identify found no process holding the PID.
var ErrNotFound = errors.New("termobs: no process holds this PID")

// Identify captures the incarnation of the process currently holding
// pid. It never searches for or selects processes.
func Identify(pid int) (Incarnation, error) { return identify(pid) }

// Observe reports whether the exact incarnation is exited, running,
// unknown, or unobservable. It has no side effects on the target.
func Observe(inc Incarnation) Observation { return observe(inc) }
