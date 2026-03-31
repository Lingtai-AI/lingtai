//go:build !windows

package main

import "syscall"

// syscallExec replaces the current process with the given command.
func syscallExec(argv0 string, argv []string, envv []string) {
	syscall.Exec(argv0, argv, envv)
}
