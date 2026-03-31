//go:build windows

package main

import (
	"os"
	"os/exec"
)

// syscallExec on Windows: can't replace process, so launch new and exit.
func syscallExec(argv0 string, argv []string, envv []string) {
	cmd := exec.Command(argv0, argv[1:]...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	cmd.Start()
	os.Exit(0)
}
