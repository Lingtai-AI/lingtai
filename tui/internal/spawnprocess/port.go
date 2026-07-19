// Package spawnprocess owns a single authority: creating at most one child
// process from a typed specification. Success reports only that the OS
// accepted this one creation event — it implies no readiness, ownership,
// or duplicate-launch safety. See CONTRACT.md.
package spawnprocess

import (
	"errors"
	"io/fs"
	"os"
	"os/exec"
)

// Spec is the typed input for one creation event.
type Spec struct {
	Executable string   // program path; resolved via PATH if not absolute
	Args       []string // arguments, not including the program itself
	Env        []string // child environment; nil inherits the parent's
	Dir        string   // working directory; empty inherits the parent's
	Stdout     *os.File // child stdout; nil means the OS null device
	Stderr     *os.File // child stderr; nil means the OS null device
}

// Child identifies the created process. It carries no liveness, readiness,
// or exclusivity claim; the OS may reuse the PID once the child exits.
type Child struct {
	PID  int
	Proc *os.Process // handle from the creation call; identity only
}

// FailureKind classifies why creation did not happen.
type FailureKind string

const (
	FailureInvalidSpec FailureKind = "invalid_spec" // rejected before any OS call
	FailureNotFound    FailureKind = "not_found"    // executable does not exist
	FailureStart       FailureKind = "start"        // the OS refused the creation call
)

// Error is the typed failure of one creation event. When Error is returned,
// this call created no child.
type Error struct {
	Kind FailureKind
	Err  error
}

func (e *Error) Error() string { return string(e.Kind) + ": " + e.Err.Error() }
func (e *Error) Unwrap() error { return e.Err }

// Spawn creates at most one child process from spec. It performs exactly one
// OS creation call and returns the child's identity or a typed failure.
func Spawn(spec Spec) (*Child, error) {
	if spec.Executable == "" {
		return nil, &Error{Kind: FailureInvalidSpec, Err: errors.New("empty executable")}
	}
	return spawnOS(spec)
}

func classifyStart(err error) *Error {
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, exec.ErrNotFound) {
		return &Error{Kind: FailureNotFound, Err: err}
	}
	return &Error{Kind: FailureStart, Err: err}
}
