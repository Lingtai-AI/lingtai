// Package tuiinstance is the TUIInstanceGuard: it acquires and releases
// the one TUI singleton ownership token for a scope directory. Only an
// Acquired token permits protected TUI startup; Contended and Unknown
// are distinct refusals. See CONTRACT.md.
package tuiinstance

import "path/filepath"

const lockFileName = "tui-instance.lock"

// State classifies one acquisition attempt.
type State uint8

const (
	// Acquired: this process now holds the singleton ownership token.
	Acquired State = iota
	// Contended: a live holder already owns the token; refuse startup.
	Contended
	// Unknown: the attempt proved neither outcome; refuse startup.
	Unknown
)

// Ownership is the singleton token. Only a non-nil Ownership permits
// protected TUI startup.
type Ownership struct {
	h        lockHandle
	released bool
}

// Release drops the kernel lock and closes the handle. It never deletes
// the lock file. Safe to call once; further calls are no-ops.
func (o *Ownership) Release() {
	if o == nil || o.released {
		return
	}
	o.released = true
	releaseLock(o.h)
}

// Acquisition reports one Acquire attempt. Token is non-nil iff State
// is Acquired. Detail explains Contended/Unknown for the refusal message.
type Acquisition struct {
	State  State
	Token  *Ownership
	Detail string
}

// Acquire attempts to take the TUI singleton token scoped to dir. It
// never waits, retries, steals, or consults processes or file content.
func Acquire(dir string) Acquisition {
	h, state, detail := acquireLock(filepath.Join(dir, lockFileName))
	if state != Acquired {
		return Acquisition{State: state, Detail: detail}
	}
	return Acquisition{State: Acquired, Token: &Ownership{h: h}}
}
