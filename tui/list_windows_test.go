//go:build windows

package main

import "testing"

func TestAgentDirFromWindowsCommandLine(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want string
	}{
		{
			name: "quoted path with spaces",
			cmd:  `C:\Python\python.exe -m lingtai run "C:\Users\Raw Lee\project\.lingtai\contract-agent"`,
			want: `C:\Users\Raw Lee\project\.lingtai\contract-agent`,
		},
		{
			name: "unquoted path",
			cmd:  `C:\Python\python.exe -m lingtai run C:\Users\rawle\project\.lingtai\contract-agent`,
			want: `C:\Users\rawle\project\.lingtai\contract-agent`,
		},
		{
			name: "non lingtai command",
			cmd:  `powershell.exe -NoProfile`,
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := agentDirFromWindowsCommandLine(tt.cmd); got != tt.want {
				t.Fatalf("agentDirFromWindowsCommandLine() = %q, want %q", got, tt.want)
			}
		})
	}
}
