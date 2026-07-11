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
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

type listOptions struct {
	FilterDir string
	Detailed  bool
	Admin     bool
	JSON      bool
}

type listJSONOutput struct {
	Status      string         `json:"status"`
	Count       int            `json:"count"`
	FilterDir   string         `json:"filter_dir,omitempty"`
	Processes   []listJSONProc `json:"processes"`
	PhantomDirs []string       `json:"phantom_dirs,omitempty"`
}

type listJSONProc struct {
	PID        string             `json:"pid"`
	Uptime     string             `json:"uptime,omitempty"`
	Role       string             `json:"role"`
	Agent      string             `json:"agent"`
	Project    string             `json:"project"`
	AgentDir   string             `json:"agent_dir"`
	Address    string             `json:"address"`
	AgentName  string             `json:"agent_name"`
	Nickname   string             `json:"nickname,omitempty"`
	State      string             `json:"state"`
	ReadError  string             `json:"read_error,omitempty"`
	Heartbeat  fs.HeartbeatStatus `json:"heartbeat"`
	LockExists bool               `json:"lock_exists"`
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
		case "--json":
			opts.JSON = true
		case "--help", "-h":
			return opts, fmt.Errorf("usage: lingtai-tui list [--detailed|-d] [--admin] [--json] [dir]")
		default:
			if strings.HasPrefix(arg, "-") {
				return opts, fmt.Errorf("unknown list flag %q\nusage: lingtai-tui list [--detailed|-d] [--admin] [--json] [dir]", arg)
			}
			if opts.FilterDir != "" {
				return opts, fmt.Errorf("list accepts at most one directory filter\nusage: lingtai-tui list [--detailed|-d] [--admin] [--json] [dir]")
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

func printList(w io.Writer, records []inventory.Record, opts listOptions, showUptime bool) {
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

	for _, r := range records {
		project := r.Project
		if r.Phantom {
			project += " [PHANTOM]"
		}
		imHandles := firstNonEmpty(r.IMHandles, "-")
		name := firstNonEmpty(r.AgentName, r.Agent)
		address := firstNonEmpty(r.Address, r.Agent)
		state := firstNonEmpty(r.State, "unknown")
		role := string(r.Role)
		pid := fmt.Sprint(r.PID)
		if opts.Admin {
			admin := r.AdminSummary
			if r.ReadError != "" {
				admin = "manifest unreadable"
			}
			if showUptime {
				fmt.Fprintf(w, "%-8s %-12s %-6s %-24s %-24s %-24s %-10s %-28s %-40s %s\n", pid, r.Uptime, role, admin, address, name, state, imHandles, project, r.AgentDir)
			} else {
				fmt.Fprintf(w, "%-8s %-6s %-24s %-24s %-24s %-10s %-28s %-40s %s\n", pid, role, admin, address, name, state, imHandles, project, r.AgentDir)
			}
		} else if opts.Detailed {
			if showUptime {
				fmt.Fprintf(w, "%-8s %-12s %-6s %-10s %-24s %-24s %-18s %-28s %-40s %s\n", pid, r.Uptime, role, state, address, name, r.Nickname, imHandles, project, r.AgentDir)
			} else {
				fmt.Fprintf(w, "%-8s %-6s %-10s %-24s %-24s %-18s %-28s %-40s %s\n", pid, role, state, address, name, r.Nickname, imHandles, project, r.AgentDir)
			}
		} else {
			if showUptime {
				fmt.Fprintf(w, "%-8s %-12s %-6s %-30s %s\n", pid, r.Uptime, role, r.Agent, project)
			} else {
				fmt.Fprintf(w, "%-8s %-6s %-30s %s\n", pid, role, r.Agent, project)
			}
		}
	}
}

func printListJSON(w io.Writer, snap inventory.Snapshot, opts listOptions) {
	phantoms := append([]string(nil), snap.PhantomDirs...)
	sort.Strings(phantoms)

	out := listJSONOutput{
		Status:      "ok",
		Count:       len(snap.Records),
		FilterDir:   opts.FilterDir,
		Processes:   make([]listJSONProc, 0, len(snap.Records)),
		PhantomDirs: phantoms,
	}
	for _, r := range snap.Records {
		out.Processes = append(out.Processes, listJSONProc{
			PID:        fmt.Sprint(r.PID),
			Uptime:     r.Uptime,
			Role:       string(r.Role),
			Agent:      r.Agent,
			Project:    r.Project,
			AgentDir:   r.AgentDir,
			Address:    firstNonEmpty(r.Address, r.Agent),
			AgentName:  firstNonEmpty(r.AgentName, r.Agent),
			Nickname:   r.Nickname,
			State:      firstNonEmpty(r.State, "unknown"),
			ReadError:  r.ReadError,
			Heartbeat:  r.Heartbeat,
			LockExists: r.LockExists,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(out)
}

func printListWarnings(w io.Writer, phantomDirs []string, filterDir string) {
	if len(phantomDirs) == 0 {
		return
	}
	fmt.Fprintln(w)
	for _, dir := range phantomDirs {
		fmt.Fprintf(w, "WARNING: %s/.lingtai/ no longer exists — processes are phantoms.\n", dir)
	}
	if filterDir != "" {
		fmt.Fprintf(w, "Run 'lingtai-tui purge %s' to kill them.\n", filterDir)
	} else {
		fmt.Fprintln(w, "Run 'lingtai-tui purge <dir>' to kill phantoms in a specific directory.")
	}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func listUsageError(err error) {
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(2)
}
