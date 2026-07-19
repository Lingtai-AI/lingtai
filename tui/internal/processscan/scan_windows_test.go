//go:build windows

package processscan

import (
	"errors"
	"testing"
)

func TestScanWindowsAgentProcessesWithQuery(t *testing.T) {
	abs := `C:\Users\Raw Lee\Project With Spaces\.lingtai\agent-a`
	valid := `C:\Python\python.exe -m lingtai run "C:\Users\Raw Lee\Project With Spaces\.lingtai\agent-a"`
	empty := ""

	got, err := scanWindowsAgentProcessesWithQuery(abs, func(query string, dst interface{}, args ...interface{}) error {
		if query != windowsProcessQuery {
			t.Fatalf("query = %q, want %q", query, windowsProcessQuery)
		}
		if len(args) != 0 {
			t.Fatalf("unexpected WMI connect args: %v", args)
		}
		rows, ok := dst.(*[]windowsProcessRow)
		if !ok {
			t.Fatalf("dst type = %T", dst)
		}
		*rows = []windowsProcessRow{
			{ProcessId: 1, CommandLine: nil},
			{ProcessId: 2, CommandLine: &empty},
			{ProcessId: 3, CommandLine: &valid},
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].PID != 3 || got[0].AgentDir != abs {
		t.Fatalf("got %+v, want one exact PID 3 match", got)
	}
}

func TestScanWindowsAgentProcessesReturnsQueryError(t *testing.T) {
	queryErr := errors.New("WMI unavailable")
	procs, err := scanWindowsAgentProcessesWithQuery("", func(string, interface{}, ...interface{}) error {
		return queryErr
	})
	if !errors.Is(err, queryErr) {
		t.Fatalf("error = %v, want %v", err, queryErr)
	}
	if len(procs) != 0 {
		t.Fatalf("got processes alongside query error: %+v", procs)
	}
}
