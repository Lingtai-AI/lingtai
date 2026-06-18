package processscan

import (
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// AgentProcess is a single `lingtai run <agentDir>` process discovered by
// scanning `ps`. The command field holds the trimmed ps line; pid is parsed
// from the leading column. Used by FindAgentProcesses so callers can both
// detect and act on lingering interpreters.
type AgentProcess struct {
	PID     int
	Command string
}

// ParsePSOutput extracts AgentProcess records from `ps -eo pid=,command=`
// output that match `lingtai run <abs>`. Split out from FindAgentProcesses so
// the parsing logic is unit-testable without shelling out to ps.
//
// The ps output format is: leading whitespace, PID, single space, command
// line (which itself may contain spaces). We split on the first whitespace
// run to separate pid from command.
func ParsePSOutput(out, abs string) []AgentProcess {
	var results []AgentProcess
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "lingtai run") {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if !commandMatchesAgentDir(trimmed, abs) {
			continue
		}
		// Split off the leading pid column. ps emits "  1234 python ..." so
		// Fields collapses leading whitespace; we take the first token.
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		results = append(results, AgentProcess{PID: pid, Command: trimmed})
	}
	return results
}

func ParseWMICOutput(out, abs string) []AgentProcess {
	var results []AgentProcess
	var cmdline string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "CommandLine=") {
			cmdline = strings.TrimPrefix(line, "CommandLine=")
			continue
		}
		if !strings.HasPrefix(line, "ProcessId=") {
			continue
		}
		pidText := strings.TrimPrefix(line, "ProcessId=")
		pid, err := strconv.Atoi(strings.TrimSpace(pidText))
		if err == nil && commandMatchesProcess(cmdline, abs) {
			results = append(results, AgentProcess{
				PID:     pid,
				Command: strings.TrimSpace(cmdline),
			})
		}
		cmdline = ""
	}
	return results
}

func commandMatchesProcess(command, abs string) bool {
	if abs == "" {
		lower := strings.ToLower(command)
		return strings.Contains(lower, "-m lingtai run ") ||
			strings.Contains(lower, "lingtai-agent run ")
	}
	return commandMatchesAgentDir(command, abs)
}

func commandMatchesAgentDir(command, abs string) bool {
	if !strings.Contains(strings.ToLower(command), "lingtai run") {
		return false
	}
	candidates := []string{abs, filepath.ToSlash(abs)}
	for _, candidate := range candidates {
		for _, arg := range []string{candidate, `"` + candidate + `"`} {
			needle := "lingtai run " + arg
			if commandContainsArg(command, needle) {
				return true
			}
		}
	}
	return false
}

func commandContainsArg(command, needle string) bool {
	cmd := strings.ToLower(command)
	n := strings.ToLower(needle)
	return strings.Contains(cmd, n+" ") || strings.Contains(cmd, n+"\t") || strings.HasSuffix(cmd, n)
}

// FindAgentProcesses returns all running `lingtai run <agentDir>` processes
// visible to the current user via process listing. Empty slice on
// error or no match. Use IsAgentRunning if you only need a boolean.
func FindAgentProcesses(agentDir string) []AgentProcess {
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		abs = agentDir
	}
	if runtime.GOOS == "windows" {
		return findAgentProcessesWindows(abs)
	}
	out, err := exec.Command("ps", "-eo", "pid=,command=").Output()
	if err != nil {
		return nil
	}
	return ParsePSOutput(string(out), abs)
}

func findAgentProcessesWindows(abs string) []AgentProcess {
	out, err := windowsAgentProcessOutput()
	if err != nil {
		return nil
	}
	return ParseWMICOutput(string(out), abs)
}

func FindWindowsAgentProcesses(abs string) []AgentProcess {
	return findAgentProcessesWindows(abs)
}

func windowsAgentProcessOutput() ([]byte, error) {
	out, err := exec.Command(
		"wmic",
		"process",
		"where",
		"commandline like '%lingtai run%'",
		"get",
		"processid,commandline",
		"/format:list",
	).Output()
	if err == nil {
		return out, nil
	}
	script := `Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*lingtai run*' } | ForEach-Object { "CommandLine=$($_.CommandLine)"; "ProcessId=$($_.ProcessId)"; "" }`
	return exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		script,
	).Output()
}

// IsAgentRunning returns true if any `python -m lingtai run <agentDir>`
// (or `lingtai-agent run <agentDir>`) process exists on this machine.
func IsAgentRunning(agentDir string) bool {
	return len(FindAgentProcesses(agentDir)) > 0
}
