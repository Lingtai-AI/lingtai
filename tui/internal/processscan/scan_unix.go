//go:build !windows

package processscan

import (
	"errors"
	"os/exec"
)

func scanAgentProcesses(abs string) ([]AgentProcess, error) {
	out, err := exec.Command("ps", "-eo", "pid=,command=").Output()
	if err != nil {
		return nil, err
	}
	return ParsePSOutput(string(out), abs), nil
}

func scanAllAgentProcesses() ([]AgentProcess, error) {
	out, err := exec.Command("ps", "-eo", "pid=,etime=,command=").Output()
	if err != nil {
		return nil, err
	}
	return ParsePSListOutput(string(out)), nil
}

func scanWindowsAgentProcesses(string) ([]AgentProcess, error) {
	return nil, errors.New("windows process observation unavailable on this platform")
}
