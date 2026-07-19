//go:build !windows

package spawnprocess

import "os/exec"

// spawnOS performs the single POSIX creation event via fork/exec (through
// os/exec). The child joins the parent's process group and session; the
// parent does not wait, so reaping the child is the caller's concern.
func spawnOS(spec Spec) (*Child, error) {
	cmd := exec.Command(spec.Executable, spec.Args...)
	cmd.Dir = spec.Dir
	cmd.Env = spec.Env
	cmd.Stdout = spec.Stdout
	cmd.Stderr = spec.Stderr
	if err := cmd.Start(); err != nil {
		return nil, classifyStart(err)
	}
	return &Child{PID: cmd.Process.Pid, Proc: cmd.Process}, nil
}
