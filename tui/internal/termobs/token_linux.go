//go:build linux

package termobs

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"syscall"
)

// readStartToken reads the start time (clock ticks since boot, field 22
// of /proc/<pid>/stat) of the process holding pid; reading procfs
// involves no signal. found is false when no process holds pid.
func readStartToken(pid int) (token uint64, found bool, err error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			return 0, false, nil
		}
		return 0, false, err
	}
	// comm may contain spaces or parens; fields resume after the last
	// ')', so starttime (field 22) is index 19 there.
	i := bytes.LastIndexByte(data, ')')
	if i < 0 {
		return 0, false, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	fields := strings.Fields(string(data[i+1:]))
	if len(fields) < 20 {
		return 0, false, fmt.Errorf("malformed /proc/%d/stat", pid)
	}
	v, perr := strconv.ParseUint(fields[19], 10, 64)
	if perr != nil {
		return 0, false, fmt.Errorf("malformed starttime in /proc/%d/stat", pid)
	}
	return v, true, nil
}
