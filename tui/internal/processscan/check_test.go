package processscan

import "testing"

func TestParseWMICOutputMatchesQuotedWindowsPath(t *testing.T) {
	abs := `C:\Users\rawle\AppData\Local\Temp\project\.lingtai\agent-a`
	out := `CommandLine=C:\Python\python.exe -m lingtai run "C:\Users\rawle\AppData\Local\Temp\project\.lingtai\agent-a"
ProcessId=1234

CommandLine=C:\Python\python.exe -m lingtai run "C:\Users\rawle\AppData\Local\Temp\project\.lingtai\agent-a-sibling"
ProcessId=5678
`
	got := ParseWMICOutput(out, abs)
	if len(got) != 1 {
		t.Fatalf("got %d procs, want 1: %+v", len(got), got)
	}
	if got[0].PID != 1234 {
		t.Fatalf("PID = %d, want 1234", got[0].PID)
	}
}

func TestParseWMICOutputListsAllWhenAbsEmpty(t *testing.T) {
	out := `CommandLine=C:\Python\python.exe -m lingtai run C:\tmp\a
ProcessId=1234

CommandLine=C:\Python\python.exe -m other run C:\tmp\a
ProcessId=5678
`
	got := ParseWMICOutput(out, "")
	if len(got) != 1 {
		t.Fatalf("got %d procs, want 1: %+v", len(got), got)
	}
	if got[0].PID != 1234 {
		t.Fatalf("PID = %d, want 1234", got[0].PID)
	}
}

func TestCommandMatchesAgentDirEOLAndArgBoundary(t *testing.T) {
	abs := `/work/foo`
	if !commandMatchesAgentDir(`python -m lingtai run /work/foo`, abs) {
		t.Fatal("expected exact EOL match")
	}
	if !commandMatchesAgentDir(`python -m lingtai run /work/foo --debug`, abs) {
		t.Fatal("expected arg-boundary match")
	}
	if commandMatchesAgentDir(`python -m lingtai run /work/foo-sibling`, abs) {
		t.Fatal("prefix sibling should not match")
	}
}
