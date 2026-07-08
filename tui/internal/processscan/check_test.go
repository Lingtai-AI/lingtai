package processscan

import "testing"

func TestParsePSListOutputPreservesAgentDirsWithSpaces(t *testing.T) {
	out := `  1234 00:01:02 /usr/bin/python -m lingtai run /tmp/Project With Spaces/.lingtai/agent A
  2345 01:02:03 /opt/bin/lingtai run /tmp/Project With Spaces/.lingtai/agent B
  3456 1-02:03:04 /opt/bin/lingtai-agent run /tmp/Project With Spaces/.lingtai/agent C
`
	got := ParsePSListOutput(out)
	if len(got) != 3 {
		t.Fatalf("got %d procs, want 3: %+v", len(got), got)
	}
	wants := []struct {
		pid    int
		uptime string
		dir    string
	}{
		{1234, "00:01:02", "/tmp/Project With Spaces/.lingtai/agent A"},
		{2345, "01:02:03", "/tmp/Project With Spaces/.lingtai/agent B"},
		{3456, "1-02:03:04", "/tmp/Project With Spaces/.lingtai/agent C"},
	}
	for i, want := range wants {
		if got[i].PID != want.pid || got[i].Uptime != want.uptime || got[i].AgentDir != want.dir {
			t.Fatalf("proc[%d] = %+v, want pid=%d uptime=%q dir=%q", i, got[i], want.pid, want.uptime, want.dir)
		}
	}
}

func TestParsePSListOutputRejectsMalformedRows(t *testing.T) {
	out := `abc 00:01:02 /usr/bin/python -m lingtai run /tmp/project/.lingtai/a
  1234 00:01:02 /usr/bin/python -m lingtai run
  2345 00:01:02 /usr/bin/python -m other run /tmp/project/.lingtai/a
  3456 00:01:02 /usr/bin/python -m lingtai
`
	if got := ParsePSListOutput(out); len(got) != 0 {
		t.Fatalf("malformed rows should be skipped, got %+v", got)
	}
}

func TestParsePSOutputMatchesAgentDirWithSpaces(t *testing.T) {
	abs := "/tmp/Project With Spaces/.lingtai/agent A"
	out := `  1234 /usr/bin/python -m lingtai run /tmp/Project With Spaces/.lingtai/agent A
  2345 /usr/bin/python -m lingtai run /tmp/Project With Spaces/.lingtai/agent B
`
	got := ParsePSOutput(out, abs)
	if len(got) != 1 {
		t.Fatalf("got %d procs, want 1: %+v", len(got), got)
	}
	if got[0].PID != 1234 || got[0].AgentDir != abs {
		t.Fatalf("unexpected match: %+v", got[0])
	}
}

func TestParsePSOutputRejectsUnquotedSpacedPrefixSibling(t *testing.T) {
	abs := "/tmp/Project With Spaces/.lingtai/agent"
	out := `  1234 /usr/bin/python -m lingtai run /tmp/Project With Spaces/.lingtai/agent beta
  2345 /usr/bin/python -m lingtai run /tmp/Project With Spaces/.lingtai/agent
`
	got := ParsePSOutput(out, abs)
	if len(got) != 1 {
		t.Fatalf("got %d procs, want exact match only: %+v", len(got), got)
	}
	if got[0].PID != 2345 || got[0].AgentDir != abs {
		t.Fatalf("unexpected match: %+v", got[0])
	}
}

func TestExtractAgentDirFromWindowsCommandLinesWithSpaces(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{
			name: "quoted python module",
			cmd:  `C:\Python\python.exe -m lingtai run "C:\Users\Raw Lee\project\.lingtai\agent A"`,
			want: `C:\Users\Raw Lee\project\.lingtai\agent A`,
		},
		{
			name: "unquoted final arg",
			cmd:  `C:\Python\python.exe -m lingtai run C:\Users\Raw Lee\project\.lingtai\agent A`,
			want: `C:\Users\Raw Lee\project\.lingtai\agent A`,
		},
		{
			name: "direct console script",
			cmd:  `C:\Users\Raw Lee\AppData\Local\Programs\Python\Scripts\lingtai.exe run "C:\Users\Raw Lee\project\.lingtai\agent B"`,
			want: `C:\Users\Raw Lee\project\.lingtai\agent B`,
		},
		{
			name: "agent console script",
			cmd:  `C:\Users\Raw Lee\AppData\Local\Programs\Python\Scripts\lingtai-agent.exe run "C:\Users\Raw Lee\project\.lingtai\agent C"`,
			want: `C:\Users\Raw Lee\project\.lingtai\agent C`,
		},
		{
			name: "non agent",
			cmd:  `powershell.exe -NoProfile`,
			want: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ExtractAgentDir(tt.cmd)
			if tt.want == "" {
				if ok || got != "" {
					t.Fatalf("ExtractAgentDir() = %q, %v; want no match", got, ok)
				}
				return
			}
			if !ok || got != tt.want {
				t.Fatalf("ExtractAgentDir() = %q, %v; want %q, true", got, ok, tt.want)
			}
		})
	}
}

func TestParseWMICOutputMatchesQuotedWindowsPath(t *testing.T) {
	abs := `C:\Users\Raw Lee\AppData\Local\Temp\project\.lingtai\agent-a`
	out := `CommandLine=C:\Python\python.exe -m lingtai run "C:\Users\Raw Lee\AppData\Local\Temp\project\.lingtai\agent-a"
ProcessId=1234

CommandLine=C:\Python\python.exe -m lingtai run "C:\Users\Raw Lee\AppData\Local\Temp\project\.lingtai\agent-a-sibling"
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

func TestParseWMICOutputRejectsUnquotedSpacedPrefixSibling(t *testing.T) {
	abs := `C:\Users\Raw Lee\AppData\Local\Temp\project\.lingtai\agent`
	out := `CommandLine=C:\Python\python.exe -m lingtai run C:\Users\Raw Lee\AppData\Local\Temp\project\.lingtai\agent beta
ProcessId=1234

CommandLine=C:\Python\python.exe -m lingtai run C:\Users\Raw Lee\AppData\Local\Temp\project\.lingtai\agent
ProcessId=2345
`
	got := ParseWMICOutput(out, abs)
	if len(got) != 1 {
		t.Fatalf("got %d procs, want exact match only: %+v", len(got), got)
	}
	if got[0].PID != 2345 || got[0].AgentDir != abs {
		t.Fatalf("unexpected match: %+v", got[0])
	}
}

func TestParseWMICOutputListsAllWhenAbsEmpty(t *testing.T) {
	out := `CommandLine=C:\Python\python.exe -m lingtai run C:\tmp\a
ProcessId=1234

CommandLine=C:\Python\Scripts\lingtai.exe run C:\tmp\Project With Spaces\.lingtai\agent A
ProcessId=2345

CommandLine=C:\Python\python.exe -m other run C:\tmp\a
ProcessId=5678
`
	got := ParseWMICOutput(out, "")
	if len(got) != 2 {
		t.Fatalf("got %d procs, want 2: %+v", len(got), got)
	}
	if got[0].PID != 1234 || got[0].AgentDir != `C:\tmp\a` {
		t.Fatalf("first proc = %+v, want PID 1234 dir C:\\tmp\\a", got[0])
	}
	if got[1].PID != 2345 || got[1].AgentDir != `C:\tmp\Project With Spaces\.lingtai\agent A` {
		t.Fatalf("second proc = %+v", got[1])
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

	spacedAbs := `/work/Project With Spaces/.lingtai/agent`
	if !commandMatchesAgentDir(`python -m lingtai run /work/Project With Spaces/.lingtai/agent`, spacedAbs) {
		t.Fatal("expected exact unquoted spaced match")
	}
	if commandMatchesAgentDir(`python -m lingtai run /work/Project With Spaces/.lingtai/agent beta`, spacedAbs) {
		t.Fatal("unquoted spaced prefix sibling should not match")
	}
	if commandMatchesAgentDir(`python -m lingtai run /work/Project With Spaces/.lingtai/agent --debug`, spacedAbs) {
		t.Fatal("unquoted spaced trailing args are ambiguous and should not match")
	}
	if !commandMatchesAgentDir(`python -m lingtai run "/work/Project With Spaces/.lingtai/agent" --debug`, spacedAbs) {
		t.Fatal("quoted spaced arg-boundary match should remain supported")
	}
}
