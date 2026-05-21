//go:build windows

package main

func findOtherTUIProcesses() []runningTUIProcess {
	return nil
}

func stopTUIProcess(pid int) error {
	return nil
}
