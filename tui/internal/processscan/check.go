package processscan

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"
)

// AgentProcess is a LingTai agent process discovered by scanning the process
// table. Puffo ACP processes resolve their workdir through the local registry.
type AgentProcess struct {
	PID      int
	Uptime   string
	AgentDir string
	Command  string
	PuffoACP bool
}

// ParsePSOutput extracts AgentProcess records from `ps -eo pid=,command=`
// output that match a LingTai agent in the given workdir. Split out from
// FindAgentProcesses so parsing is testable without shelling out to ps.
//
// The ps output format is: leading whitespace, PID, single space, command
// line (which itself may contain spaces). We split on the first whitespace
// run to separate pid from command.
func ParsePSOutput(out, abs string) []AgentProcess {
	var results []AgentProcess
	for _, line := range strings.Split(out, "\n") {
		fields, command, ok := splitLeadingFields(line, 1)
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		agentDir, puffoACP, ok := agentDirForCommand(command, abs)
		if !ok {
			continue
		}
		results = append(results, AgentProcess{
			PID:      pid,
			AgentDir: agentDir,
			Command:  strings.TrimSpace(command),
			PuffoACP: puffoACP,
		})
	}
	return results
}

// ParsePSListOutput extracts all LingTai agent processes from
// `ps -eo pid=,etime=,command=` output. The command column may contain spaces,
// so only the leading pid and etime fields are split.
func ParsePSListOutput(out string) []AgentProcess {
	var results []AgentProcess
	for _, line := range strings.Split(out, "\n") {
		fields, command, ok := splitLeadingFields(line, 2)
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		agentDir, puffoACP, ok := agentDirForCommand(command, "")
		if !ok {
			continue
		}
		results = append(results, AgentProcess{
			PID:      pid,
			Uptime:   fields[1],
			AgentDir: agentDir,
			Command:  strings.TrimSpace(command),
			PuffoACP: puffoACP,
		})
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
		agentDir, puffoACP, ok := agentDirForCommand(cmdline, abs)
		if err == nil && ok {
			results = append(results, AgentProcess{
				PID:      pid,
				AgentDir: agentDir,
				Command:  strings.TrimSpace(cmdline),
				PuffoACP: puffoACP,
			})
		}
		cmdline = ""
	}
	return results
}

func agentDirForCommand(command, abs string) (string, bool, bool) {
	if dir, ok := puffoACPAgentDir(command); ok {
		if abs == "" {
			return dir, true, true
		}
		return abs, true, filepath.Clean(dir) == filepath.Clean(abs)
	}
	if abs == "" {
		dir, ok := extractRunAgentDir(command)
		return dir, false, ok
	}
	if commandMatchesAgentDir(command, abs) {
		return abs, false, true
	}
	return "", false, false
}

// ExtractAgentDir resolves the workdir of a supported LingTai launch. For run
// commands the directory is the final argument; for Puffo ACP it is bound in
// the local registry. This remains an advisory process-table projection.
func ExtractAgentDir(command string) (string, bool) {
	dir, _, ok := agentDirForCommand(command, "")
	return dir, ok
}

func extractRunAgentDir(command string) (string, bool) {
	rest, ok := agentDirRestAfterMarker(command)
	if !ok {
		return "", false
	}
	agentDir := extractAgentDirFromRest(rest)
	if strings.TrimSpace(agentDir) == "" {
		return "", false
	}
	return agentDir, true
}

// puffoACPAgentDir recognizes only the fixed Puffo-owned argv shape. ACP does
// not receive an agent directory on the command line; its runtime id is bound
// to one in the local registry. This is advisory discovery, not authentication
// of the process or the registry. The kernel's workdir lease remains the gate.
func puffoACPAgentDir(command string) (string, bool) {
	const maxRegistryBytes = 1 << 20
	markers := []string{
		"-m lingtai acp --profile puffo-v1 --runtime-id ",
		"lingtai-agent.exe acp --profile puffo-v1 --runtime-id ",
		"lingtai-agent acp --profile puffo-v1 --runtime-id ",
		"lingtai.exe acp --profile puffo-v1 --runtime-id ",
		"lingtai acp --profile puffo-v1 --runtime-id ",
	}
	lower := strings.ToLower(command)
	for _, marker := range markers {
		idx := strings.Index(lower, marker)
		if idx < 0 || !hasLaunchMarkerBoundary(command, idx) {
			continue
		}
		rest := strings.TrimSpace(command[idx+len(marker):])
		id, registryArg, ok := strings.Cut(rest, " --registry ")
		id = strings.TrimSpace(id)
		registryArg = strings.TrimSpace(registryArg)
		if !ok || id == "" || strings.ContainsAny(id, " \t\r\n\"'") || registryArg == "" {
			return "", false
		}
		if value, tail, quoted := splitQuoted(registryArg); quoted {
			if strings.TrimSpace(tail) != "" {
				return "", false
			}
			registryArg = value
		}
		if !filepath.IsAbs(registryArg) {
			return "", false
		}
		file, err := os.Open(registryArg)
		if err != nil {
			return "", false
		}
		var registry struct {
			Runtimes map[string]struct {
				AgentDir string `json:"agent_dir"`
			} `json:"runtimes"`
		}
		info, statErr := file.Stat()
		if statErr != nil || !info.Mode().IsRegular() || info.Size() > maxRegistryBytes {
			file.Close()
			return "", false
		}
		decoder := json.NewDecoder(io.LimitReader(file, maxRegistryBytes+1))
		decodeErr := decoder.Decode(&registry)
		var trailing any
		if decodeErr == nil && decoder.Decode(&trailing) != io.EOF {
			decodeErr = fmt.Errorf("registry has trailing data")
		}
		file.Close()
		if decodeErr != nil {
			return "", false
		}
		dir := registry.Runtimes[id].AgentDir
		if filepath.IsAbs(dir) {
			return filepath.Clean(dir), true
		}
		return "", false
	}
	return "", false
}

func commandMatchesAgentDir(command, abs string) bool {
	rest, ok := agentDirRestAfterMarker(command)
	if !ok {
		return false
	}
	candidates := []string{abs, filepath.ToSlash(abs)}
	for _, candidate := range candidates {
		if restMatchesAgentCandidate(rest, candidate) {
			return true
		}
	}
	return false
}

var launchMarkers = []string{
	"-m lingtai run ",
	"lingtai-agent.exe run ",
	"lingtai-agent run ",
	"lingtai.exe run ",
	"lingtai run ",
}

func agentDirRestAfterMarker(command string) (string, bool) {
	lower := strings.ToLower(command)
	for _, marker := range launchMarkers {
		start := 0
		for {
			idx := strings.Index(lower[start:], marker)
			if idx < 0 {
				break
			}
			idx += start
			if hasLaunchMarkerBoundary(command, idx) {
				return strings.TrimSpace(command[idx+len(marker):]), true
			}
			start = idx + 1
		}
	}
	return "", false
}

func hasLaunchMarkerBoundary(command string, idx int) bool {
	if idx == 0 {
		return true
	}
	prev := rune(command[idx-1])
	return unicode.IsSpace(prev) || prev == '/' || prev == '\\' || prev == '"' || prev == '\''
}

func extractAgentDirFromRest(rest string) string {
	rest = strings.TrimSpace(rest)
	if value, _, ok := splitQuoted(rest); ok {
		return value
	}
	return rest
}

func restMatchesAgentCandidate(rest, candidate string) bool {
	rest = strings.TrimSpace(rest)
	candidate = strings.TrimSpace(candidate)
	if rest == "" || candidate == "" {
		return false
	}
	if value, tail, ok := splitQuoted(rest); ok {
		if !strings.EqualFold(value, candidate) {
			return false
		}
		return strings.TrimSpace(tail) == "" || startsWithWhitespace(tail)
	}
	if strings.EqualFold(rest, candidate) {
		return true
	}
	if containsWhitespace(candidate) {
		return false
	}
	if len(rest) <= len(candidate) {
		return false
	}
	if !strings.EqualFold(rest[:len(candidate)], candidate) {
		return false
	}
	return unicode.IsSpace(rune(rest[len(candidate)]))
}

func splitQuoted(s string) (value, tail string, ok bool) {
	if s == "" || (s[0] != '"' && s[0] != '\'') {
		return "", "", false
	}
	quote := s[0]
	body := s[1:]
	end := strings.IndexByte(body, quote)
	if end < 0 {
		return strings.TrimSpace(body), "", true
	}
	return strings.TrimSpace(body[:end]), body[end+1:], true
}

func startsWithWhitespace(s string) bool {
	return s != "" && unicode.IsSpace(rune(s[0]))
}

func containsWhitespace(s string) bool {
	return strings.IndexFunc(s, unicode.IsSpace) >= 0
}

func splitLeadingFields(line string, count int) ([]string, string, bool) {
	rest := strings.TrimLeftFunc(line, unicode.IsSpace)
	fields := make([]string, 0, count)
	for len(fields) < count {
		if rest == "" {
			return nil, "", false
		}
		idx := strings.IndexFunc(rest, unicode.IsSpace)
		if idx < 0 {
			return nil, "", false
		}
		fields = append(fields, rest[:idx])
		rest = strings.TrimLeftFunc(rest[idx:], unicode.IsSpace)
	}
	if strings.TrimSpace(rest) == "" {
		return nil, "", false
	}
	return fields, rest, true
}

// FindAgentProcesses returns LingTai agent processes visible to the current
// user via process listing. Empty slice on
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

// FindAllAgentProcesses returns every visible LingTai agent process. On Unix it
// uses `etime` as a display-only uptime string. A process-scan command failure
// is returned as an error, never as an empty result, so callers can tell
// "nothing running" apart from "scan failed".
func FindAllAgentProcesses() ([]AgentProcess, error) {
	if runtime.GOOS == "windows" {
		out, err := WindowsAgentProcessOutput()
		if err != nil {
			return nil, err
		}
		return ParseWMICOutput(string(out), ""), nil
	}
	out, err := exec.Command("ps", "-eo", "pid=,etime=,command=").Output()
	if err != nil {
		return nil, err
	}
	return ParsePSListOutput(string(out)), nil
}

func findAgentProcessesWindows(abs string) []AgentProcess {
	out, err := WindowsAgentProcessOutput()
	if err != nil {
		return nil
	}
	return ParseWMICOutput(string(out), abs)
}

func FindWindowsAgentProcesses(abs string) []AgentProcess {
	return findAgentProcessesWindows(abs)
}

// WindowsAgentProcessOutput returns raw `CommandLine=` / `ProcessId=` records
// for candidate LingTai processes. wmic is tried first and PowerShell's
// Get-CimInstance is the fallback: wmic is no longer shipped on Windows 11
// 24H2+ and Server 2025, where a wmic-only scan silently reports zero
// processes. Exported so every Windows process count shares one fallback.
func WindowsAgentProcessOutput() ([]byte, error) {
	out, err := exec.Command(
		"wmic",
		"process",
		"where",
		"commandline like '%lingtai%'",
		"get",
		"processid,commandline",
		"/format:list",
	).Output()
	if err == nil {
		return out, nil
	}
	script := `Get-CimInstance Win32_Process | Where-Object { $_.CommandLine -like '*lingtai*' } | ForEach-Object { "CommandLine=$($_.CommandLine)"; "ProcessId=$($_.ProcessId)"; "" }`
	return exec.Command(
		"powershell.exe",
		"-NoProfile",
		"-NonInteractive",
		"-Command",
		script,
	).Output()
}

// IsAgentRunning returns true if any supported run or Puffo ACP launch is
// visible for this workdir on this machine.
func IsAgentRunning(agentDir string) bool {
	return len(FindAgentProcesses(agentDir)) > 0
}
