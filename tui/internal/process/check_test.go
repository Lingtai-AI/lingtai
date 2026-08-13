package process

import (
	"testing"
)

func TestParsePSOutput(t *testing.T) {
	abs := "/home/user/.lingtai/orch"
	out := `  1234 /usr/bin/python -m lingtai run /home/user/.lingtai/orch
  5678 /usr/bin/python -m lingtai run /home/user/.lingtai/other
  9999 /usr/bin/python -m lingtai run /home/user/.lingtai/orch --debug
   111 grep lingtai run
`
	got := parsePSOutput(out, abs)
	if len(got) != 2 {
		t.Fatalf("got %d procs, want 2: %+v", len(got), got)
	}
	if got[0].PID != 1234 || got[1].PID != 9999 {
		t.Errorf("unexpected PIDs: %+v", got)
	}
}

func TestParsePSOutputPrefixMismatch(t *testing.T) {
	// Ensure `lingtai run /a/b` is not matched when ps shows `lingtai run /a/b-other`.
	abs := "/work/foo"
	out := "  1234 python -m lingtai run /work/foo-sibling\n"
	if got := parsePSOutput(out, abs); len(got) != 0 {
		t.Errorf("prefix should not match, got %+v", got)
	}
}

func TestParsePSOutputIgnoresUnrelated(t *testing.T) {
	abs := "/work/foo"
	out := `  100 sshd
  200 zsh
  300 python -m lingtai run /work/foo
`
	got := parsePSOutput(out, abs)
	if len(got) != 1 {
		t.Fatalf("want 1, got %d", len(got))
	}
}

func TestParsePSOutputMalformedPID(t *testing.T) {
	abs := "/work/foo"
	out := "abc python -m lingtai run /work/foo\n"
	if got := parsePSOutput(out, abs); len(got) != 0 {
		t.Errorf("malformed pid should be skipped, got %+v", got)
	}
}

func TestFindAgentProcessesEmpty(t *testing.T) {
	// Deeply nonexistent path — no real process can match.
	if got := FindAgentProcesses("/nonexistent/dir/that/should/never/be/an/agent/path-xyz-123"); len(got) != 0 {
		t.Errorf("expected no matches, got %+v", got)
	}
}
