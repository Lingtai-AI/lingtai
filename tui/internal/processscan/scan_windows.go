//go:build windows

package processscan

import "github.com/yusufpapurcu/wmi"

const windowsProcessQuery = "SELECT ProcessId, CommandLine FROM Win32_Process"

type windowsProcessRow struct {
	ProcessId   uint32
	CommandLine *string
}

type wmiProcessQuery func(query string, dst interface{}, connectServerArgs ...interface{}) error

func scanAgentProcesses(abs string) ([]AgentProcess, error) {
	return scanWindowsAgentProcesses(abs)
}

func scanAllAgentProcesses() ([]AgentProcess, error) {
	return scanWindowsAgentProcesses("")
}

func scanWindowsAgentProcesses(abs string) ([]AgentProcess, error) {
	client := &wmi.Client{PtrNil: true}
	return scanWindowsAgentProcessesWithQuery(abs, client.Query)
}

func scanWindowsAgentProcessesWithQuery(abs string, query wmiProcessQuery) ([]AgentProcess, error) {
	var rows []windowsProcessRow
	if err := query(windowsProcessQuery, &rows); err != nil {
		return nil, err
	}
	records := make([]processRecord, 0, len(rows))
	for _, row := range rows {
		records = append(records, processRecord{
			PID:         int(row.ProcessId),
			CommandLine: row.CommandLine,
		})
	}
	return agentProcessesFromRecords(records, abs), nil
}
