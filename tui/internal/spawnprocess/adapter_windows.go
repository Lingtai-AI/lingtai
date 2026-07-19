//go:build windows

package spawnprocess

import "os/exec"

// spawnOS performs the single Windows creation event via CreateProcess
// (through os/exec). No creation flags are set, so the child shares the
// parent's console; the returned handle stays open until the caller
// releases it.
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
