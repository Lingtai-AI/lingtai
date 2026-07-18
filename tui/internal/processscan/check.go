package processscan

import (
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

// AgentProcess is a single `lingtai run <agentDir>` process discovered by
// scanning the process table. AgentDir is parsed from the final run argument
// so paths containing spaces remain intact.
type AgentProcess struct {
	PID      int
	Uptime   string
	AgentDir string
	Command  string
}

// processRecord is the technology-neutral result supplied by a platform
// process-table adapter before LingTai launch markers are matched. A nil or
// empty CommandLine represents a process that cannot be identified and is
// skipped.
type processRecord struct {
	PID         int
	CommandLine *string
}

type processQuery func(abs string) ([]AgentProcess, error)
type allProcessQuery func() ([]AgentProcess, error)

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
		fields, command, ok := splitLeadingFields(line, 1)
		if !ok {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		agentDir, ok := agentDirForCommand(command, abs)
		if !ok {
			continue
		}
		results = append(results, AgentProcess{
			PID:      pid,
			AgentDir: agentDir,
			Command:  strings.TrimSpace(command),
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
		agentDir, ok := ExtractAgentDir(command)
		if !ok {
			continue
		}
		results = append(results, AgentProcess{
			PID:      pid,
			Uptime:   fields[1],
			AgentDir: agentDir,
			Command:  strings.TrimSpace(command),
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
		agentDir, ok := agentDirForCommand(cmdline, abs)
		if err == nil && ok {
			results = append(results, AgentProcess{
				PID:      pid,
				AgentDir: agentDir,
				Command:  strings.TrimSpace(cmdline),
			})
		}
		cmdline = ""
	}
	return results
}

func agentProcessesFromRecords(records []processRecord, abs string) []AgentProcess {
	var results []AgentProcess
	for _, record := range records {
		if record.CommandLine == nil {
			continue
		}
		command := strings.TrimSpace(*record.CommandLine)
		if command == "" {
			continue
		}
		agentDir, ok := agentDirForCommand(command, abs)
		if !ok {
			continue
		}
		results = append(results, AgentProcess{
			PID:      record.PID,
			AgentDir: agentDir,
			Command:  command,
		})
	}
	return results
}

func agentDirForCommand(command, abs string) (string, bool) {
	if abs == "" {
		return ExtractAgentDir(command)
	}
	if commandMatchesAgentDir(command, abs) {
		return abs, true
	}
	return "", false
}

// ExtractAgentDir returns the argument after a supported LingTai launch marker.
// The launcher passes the agent directory as the final argv element; when a
// platform adapter presents argv as command text, taking the rest after the
// marker preserves spaces inside that directory.
func ExtractAgentDir(command string) (string, bool) {
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

// FindAgentProcesses returns all running `lingtai run <agentDir>` processes
// visible to the current user via the platform process-table adapter. Empty
// slice on error or no match. Use IsAgentRunning for a boolean.
func FindAgentProcesses(agentDir string) []AgentProcess {
	return findAgentProcesses(agentDir, scanAgentProcesses)
}

func findAgentProcesses(agentDir string, query processQuery) []AgentProcess {
	abs, err := filepath.Abs(agentDir)
	if err != nil {
		abs = agentDir
	}
	processes, err := query(abs)
	if err != nil {
		return nil
	}
	return processes
}

// FindAllAgentProcesses returns every visible LingTai agent process. On Unix it
// uses `etime` as a display-only uptime string. A host-query failure is returned
// as an error, never as an empty result, so callers can distinguish "nothing
// running" from "scan failed".
func FindAllAgentProcesses() ([]AgentProcess, error) {
	return findAllAgentProcesses(scanAllAgentProcesses)
}

func findAllAgentProcesses(query allProcessQuery) ([]AgentProcess, error) {
	return query()
}

// FindWindowsAgentProcesses preserves the legacy Windows-specific parser entry
// point while delegating observation to the platform adapter.
func FindWindowsAgentProcesses(abs string) []AgentProcess {
	processes, err := scanWindowsAgentProcesses(abs)
	if err != nil {
		return nil
	}
	return processes
}

// IsAgentRunning returns true if any supported `lingtai run <agentDir>` launch
// form is visible on this machine.
func IsAgentRunning(agentDir string) bool {
	return len(FindAgentProcesses(agentDir)) > 0
}
