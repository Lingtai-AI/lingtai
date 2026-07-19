//go:build darwin

package termobs

import (
	"syscall"
	"unsafe"
)

// readStartToken reads the start time (microseconds since the epoch) of
// the process holding pid via sysctl kern.proc.pid; no signal is
// involved. found is false when no process holds pid (empty result).
func readStartToken(pid int) (token uint64, found bool, err error) {
	// mib: CTL_KERN, KERN_PROC, KERN_PROC_PID, pid. The kinfo_proc
	// result (648 bytes on darwin/amd64+arm64) places p_starttime
	// (timeval: tv_sec int64, tv_usec int32) at offset 0.
	mib := [4]int32{1, 14, 1, int32(pid)}
	var buf [648]byte
	n := uintptr(len(buf))
	_, _, errno := syscall.Syscall6(syscall.SYS___SYSCTL,
		uintptr(unsafe.Pointer(&mib[0])), uintptr(len(mib)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)),
		0, 0)
	if errno != 0 {
		return 0, false, errno
	}
	if n < 16 {
		return 0, false, nil
	}
	sec := *(*int64)(unsafe.Pointer(&buf[0]))
	usec := *(*int32)(unsafe.Pointer(&buf[8]))
	return uint64(sec)*1_000_000 + uint64(uint32(usec)), true, nil
}
