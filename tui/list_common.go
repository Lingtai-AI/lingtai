package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/fs"
	uitui "github.com/anthropics/lingtai-tui/internal/tui"
)

type listOptions struct {
	FilterDir string
	Detailed  bool
	Admin     bool
}

type listProc struct {
	PID     string
	Uptime  string
	Agent   string
	Project string
	Dir     string
	Info    listAgentInfo
}

type listAgentInfo struct {
	Address        string
	AgentName      string
	Nickname       string
	State          string
	IsHuman        bool
	IsOrchestrator bool
	AdminSummary   string
	IMHandles      string
	ReadError      string
}

func parseListArgs(args []string) (listOptions, error) {
	var opts listOptions
	for _, arg := range args {
		switch arg {
		case "--detailed", "-d":
			opts.Detailed = true
		case "--admin":
			opts.Admin = true
			opts.Detailed = true
		case "--help", "-h":
			return opts, fmt.Errorf("usage: lingtai-tui list [--detailed|-d] [--admin] [dir]")
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown list flag %q\nusage: lingtai-tui list [--detailed|-d] [--admin] [dir]", arg)
			}
			if opts.FilterDir != "" {
				return opts, fmt.Errorf("list accepts at most one directory filter\nusage: lingtai-tui list [--detailed|-d] [--admin] [dir]")
			}
			abs, err := filepath.Abs(arg)
			if err != nil {
				return opts, err
			}
			opts.FilterDir = abs
		}
	}
	return opts, nil
}

func loadListAgentInfo(agentDir, fallbackAgent string) listAgentInfo {
	info := listAgentInfo{Address: fallbackAgent, AgentName: fallbackAgent, AdminSummary: "unknown"}

	node, err := fs.ReadAgent(agentDir)
	if err == nil {
		info.Address = firstNonEmpty(node.Address, fallbackAgent)
		info.AgentName = firstNonEmpty(node.AgentName, fallbackAgent)
		info.Nickname = node.Nickname
		info.State = node.State
		info.IsHuman = node.IsHuman
	} else {
		info.ReadError = err.Error()
	}

	raw, err := fs.ReadAgentRaw(agentDir)
	if err != nil {
		if info.ReadError == "" {
			info.ReadError = err.Error()
		}
		return info
	}
	info.IsOrchestrator = uitui.IsOrchestrator(raw)
	info.AdminSummary = summarizeAdmin(raw["admin"])
	if info.IsOrchestrator {
		info.IsHuman = false
	}
	info.IMHandles = summarizeIMIdentities(agentDir)
	return info
}

func summarizeIMIdentities(agentDir string) string {
	identityDir := filepath.Join(agentDir, "system", "mcp_identities")
	entries, err := os.ReadDir(identityDir)
	if err != nil {
		return ""
	}
	var parts []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		mcp := strings.TrimSuffix(entry.Name(), ".json")
		data, err := os.ReadFile(filepath.Join(identityDir, entry.Name()))
		if err != nil {
			continue
		}
		var doc map[string]interface{}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		if docMCP, ok := doc["mcp"].(string); ok && docMCP != "" {
			mcp = docMCP
		}
		accounts, _ := doc["accounts"].([]interface{})
		if len(accounts) == 0 {
			continue
		}
		var handles []string
		for _, account := range accounts {
			accountMap, ok := account.(map[string]interface{})
			if !ok {
				continue
			}
			if handle := publicHandle(accountMap); handle != "" {
				handles = append(handles, handle)
			}
		}
		if len(handles) > 0 {
			parts = append(parts, fmt.Sprintf("%s:%s", mcp, strings.Join(handles, "/")))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, ";")
}

func publicHandle(account map[string]interface{}) string {
	preferred := []string{"bot_username", "username", "handle", "open_id", "user_id", "union_id", "chat_id", "email", "bot_id", "alias"}
	for _, key := range preferred {
		if value, ok := account[key]; ok {
			if s := publicScalar(value); s != "" {
				if key == "bot_username" || key == "username" {
					return "@" + strings.TrimPrefix(s, "@")
				}
				return s
			}
		}
	}
	return ""
}

func publicScalar(value interface{}) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return fmt.Sprintf("%.0f", v)
	}
	return ""
}

func summarizeAdmin(adminRaw interface{}) string {
	if adminRaw == nil {
		return "admin=null"
	}
	adminMap, ok := adminRaw.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("admin=%T", adminRaw)
	}
	if len(adminMap) == 0 {
		return "admin={}"
	}
	keys := make([]string, 0, len(adminMap))
	for k := range adminMap {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%v", k, adminMap[k]))
	}
	return strings.Join(parts, ",")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func roleLabel(info listAgentInfo) string {
	switch {
	case info.IsOrchestrator:
		return "MAIN"
	case info.IsHuman:
		return "HUMAN"
	case info.ReadError != "":
		return "?"
	default:
		return "AGENT"
	}
}

func annotateListProcs(procs []listProc) {
	for i := range procs {
		procs[i].Info = loadListAgentInfo(procs[i].Dir, procs[i].Agent)
	}
}

func printList(w io.Writer, procs []listProc, phantomDirs map[string]bool, opts listOptions, showUptime bool) {
	if opts.Admin {
		if showUptime {
			fmt.Fprintf(w, "%-8s %-12s %-6s %-24s %-24s %-24s %-10s %-28s %-40s %s\n", "PID", "UPTIME", "ROLE", "ADMIN", "ADDRESS", "NAME", "STATE", "IM_HANDLES", "PROJECT", "AGENT_DIR")
		} else {
			fmt.Fprintf(w, "%-8s %-6s %-24s %-24s %-24s %-10s %-28s %-40s %s\n", "PID", "ROLE", "ADMIN", "ADDRESS", "NAME", "STATE", "IM_HANDLES", "PROJECT", "AGENT_DIR")
		}
	} else if opts.Detailed {
		if showUptime {
			fmt.Fprintf(w, "%-8s %-12s %-6s %-10s %-24s %-24s %-18s %-28s %-40s %s\n", "PID", "UPTIME", "ROLE", "STATE", "ADDRESS", "NAME", "NICKNAME", "IM_HANDLES", "PROJECT", "AGENT_DIR")
		} else {
			fmt.Fprintf(w, "%-8s %-6s %-10s %-24s %-24s %-18s %-28s %-40s %s\n", "PID", "ROLE", "STATE", "ADDRESS", "NAME", "NICKNAME", "IM_HANDLES", "PROJECT", "AGENT_DIR")
		}
	} else {
		if showUptime {
			fmt.Fprintf(w, "%-8s %-12s %-6s %-30s %s\n", "PID", "UPTIME", "ROLE", "AGENT", "PROJECT")
		} else {
			fmt.Fprintf(w, "%-8s %-6s %-30s %s\n", "PID", "ROLE", "AGENT", "PROJECT")
		}
	}

	for _, p := range procs {
		project := p.Project
		imHandles := firstNonEmpty(p.Info.IMHandles, "-")
		if phantomDirs[p.Project] {
			project += " [PHANTOM]"
		}
		role := roleLabel(p.Info)
		name := firstNonEmpty(p.Info.AgentName, p.Agent)
		address := firstNonEmpty(p.Info.Address, p.Agent)
		state := firstNonEmpty(p.Info.State, "unknown")
		if opts.Admin {
			admin := p.Info.AdminSummary
			if p.Info.ReadError != "" {
				admin = "manifest unreadable"
			}
			if showUptime {
				fmt.Fprintf(w, "%-8s %-12s %-6s %-24s %-24s %-24s %-10s %-28s %-40s %s\n", p.PID, p.Uptime, role, admin, address, name, state, imHandles, project, p.Dir)
			} else {
				fmt.Fprintf(w, "%-8s %-6s %-24s %-24s %-24s %-10s %-28s %-40s %s\n", p.PID, role, admin, address, name, state, imHandles, project, p.Dir)
			}
		} else if opts.Detailed {
			if showUptime {
				fmt.Fprintf(w, "%-8s %-12s %-6s %-10s %-24s %-24s %-18s %-28s %-40s %s\n", p.PID, p.Uptime, role, state, address, name, p.Info.Nickname, imHandles, project, p.Dir)
			} else {
				fmt.Fprintf(w, "%-8s %-6s %-10s %-24s %-24s %-18s %-28s %-40s %s\n", p.PID, role, state, address, name, p.Info.Nickname, imHandles, project, p.Dir)
			}
		} else {
			if showUptime {
				fmt.Fprintf(w, "%-8s %-12s %-6s %-30s %s\n", p.PID, p.Uptime, role, p.Agent, project)
			} else {
				fmt.Fprintf(w, "%-8s %-6s %-30s %s\n", p.PID, role, p.Agent, project)
			}
		}
	}
}

func printListWarnings(w io.Writer, phantomDirs map[string]bool, filterDir string) {
	if len(phantomDirs) == 0 {
		return
	}
	fmt.Fprintln(w)
	dirs := make([]string, 0, len(phantomDirs))
	for dir := range phantomDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	for _, dir := range dirs {
		fmt.Fprintf(w, "WARNING: %s/.lingtai/ no longer exists — processes are phantoms.\n", dir)
	}
	if filterDir != "" {
		fmt.Fprintf(w, "Run 'lingtai-tui purge %s' to kill them.\n", filterDir)
	} else {
		fmt.Fprintln(w, "Run 'lingtai-tui purge <dir>' to kill phantoms in a specific directory.")
	}
}

func listUsageError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(2)
}
