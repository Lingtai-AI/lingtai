// Package inventory turns the visible LingTai process table into typed running
// agent records shared by the CLI list command and the /projects TUI switcher.
package inventory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/processscan"
)

type Role string

const (
	RoleMain    Role = "MAIN"
	RoleAgent   Role = "AGENT"
	RoleHuman   Role = "HUMAN"
	RoleUnknown Role = "?"
)

// Options controls inventory conversion. FilterDir is a project root, not a
// .lingtai directory. SelfPID is excluded so list never reports itself when a
// test or wrapper command happens to match the process pattern.
type Options struct {
	FilterDir    string
	SelfPID      int
	IncludeHuman bool
}

type Snapshot struct {
	// Err preserves the historical Snapshot-only FromProcesses signature.
	// Production callers MUST check it; Scan returns this exact error.
	Err         error `json:"-"`
	FilterDir   string
	Records     []Record
	Groups      []Group
	PhantomDirs []string
}

type EnterabilityReason string

const (
	EnterReasonNone               EnterabilityReason = ""
	EnterReasonPathOutsideProject EnterabilityReason = "path_not_under_project"
	EnterReasonPhantomProject     EnterabilityReason = "phantom_project"
	EnterReasonManifestUnreadable EnterabilityReason = "manifest_unreadable"
	EnterReasonHuman              EnterabilityReason = "human_target"
	EnterReasonNonAdmin           EnterabilityReason = "non_admin_target"
	EnterReasonAgentDirMissing    EnterabilityReason = "agent_dir_missing"
)

type AgentIdentity struct {
	AgentDir string
	PID      int
}

type Group struct {
	Project string
	Phantom bool
	Records []Record
}

type Record struct {
	PID       int
	Uptime    string
	Agent     string
	Project   string
	AgentDir  string
	Address   string
	AgentName string
	Nickname  string
	State     string

	IsHuman        bool
	IsOrchestrator bool
	Role           Role
	AdminSummary   string
	IMHandles      string
	ReadError      string

	CreatedAt          string
	MoltCount          int
	MoltCountAvailable bool
	ContextTotalTokens int
	ContextWindowSize  int
	ContextUsagePct    float64
	ContextAvailable   bool

	Heartbeat  fs.HeartbeatStatus
	LockExists bool
	Phantom    bool

	Enterable   bool
	EnterReason EnterabilityReason
	EnterDetail string
}

// Scan reads the process table via processscan and converts visible processes
// into a typed inventory snapshot. Scan command failures are returned as
// errors so callers can distinguish "no running agents" from "scan failed".
var scanProcesses = processscan.FindAllAgentProcesses

func Scan(opts Options) (Snapshot, error) {
	found, err := scanProcesses()
	if err != nil {
		return Snapshot{}, err
	}
	snapshot := FromProcesses(found, opts)
	return snapshot, snapshot.Err
}

// FromProcesses converts already-discovered process rows into an enriched,
// duplicate-collapsed, deterministically sorted snapshot. It is the testable
// core behind Scan.
func FromProcesses(found []processscan.AgentProcess, opts Options) Snapshot {
	filterDir := NormalizePath(opts.FilterDir)
	records := make([]Record, 0, len(found))
	for _, proc := range found {
		if opts.SelfPID != 0 && proc.PID == opts.SelfPID {
			continue
		}
		agentDir := proc.AgentDir
		if agentDir == "" {
			var ok bool
			agentDir, ok = processscan.ExtractAgentDir(proc.Command)
			if !ok {
				continue
			}
		}
		agentDir = NormalizePath(agentDir)
		if !AgentDirInFilter(agentDir, filterDir) {
			continue
		}
		records = append(records, Record{
			PID:      proc.PID,
			Uptime:   proc.Uptime,
			Agent:    filepath.Base(agentDir),
			Project:  ProjectFromAgentDir(agentDir),
			AgentDir: agentDir,
		})
	}

	observation, records := observeRecords(records, filterDir, opts.IncludeHuman)
	decision, err := query(observation)
	if err != nil {
		return Snapshot{Err: err}
	}
	snapshot, err := projectSnapshot(filterDir, records, observation, decision)
	if err != nil {
		return Snapshot{Err: err}
	}
	return snapshot
}

func NormalizePath(path string) string {
	if path == "" {
		return ""
	}
	clean := filepath.Clean(path)
	abs, err := filepath.Abs(clean)
	if err == nil {
		clean = abs
	}
	return filepath.Clean(clean)
}

func IdentityFor(agentDir string, pid int) AgentIdentity {
	return AgentIdentity{AgentDir: NormalizePath(agentDir), PID: pid}
}

func (r Record) Identity() AgentIdentity {
	return IdentityFor(r.AgentDir, r.PID)
}

func AgentDirInFilter(agentDir, filterDir string) bool {
	if filterDir == "" {
		return true
	}
	agentDir = NormalizePath(agentDir)
	filterDir = NormalizePath(filterDir)
	lingtaiDir := filepath.Join(filterDir, ".lingtai")
	if rel, err := filepath.Rel(lingtaiDir, agentDir); err == nil {
		if rel != "." && rel != ".." && !filepath.IsAbs(rel) && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return true
		}
	}
	return strings.HasPrefix(filepath.ToSlash(agentDir), filepath.ToSlash(lingtaiDir)+"/")
}

func ProjectFromAgentDir(agentDir string) string {
	agentDir = NormalizePath(agentDir)
	slashDir := filepath.ToSlash(agentDir)
	idx := strings.Index(slashDir, "/.lingtai/")
	if idx < 0 {
		return ""
	}
	return filepath.Clean(filepath.FromSlash(slashDir[:idx]))
}

func enrichRecord(r *Record) []bool {
	adminFlags := []bool{}
	fallback := r.Agent
	r.Address = fallback
	r.AgentName = fallback
	r.AdminSummary = "unknown"

	node, err := fs.ReadAgent(r.AgentDir)
	if err == nil {
		r.Address = firstNonEmpty(node.Address, fallback)
		r.AgentName = firstNonEmpty(node.AgentName, fallback)
		r.Nickname = node.Nickname
		r.State = node.State
		r.IsHuman = node.IsHuman
	} else {
		r.ReadError = err.Error()
	}

	raw, err := fs.ReadAgentRaw(r.AgentDir)
	if err != nil {
		if r.ReadError == "" {
			r.ReadError = err.Error()
		}
	} else {
		// Keep the native map/boolean decoding; Core evaluates privilege policy.
		if admin, ok := raw["admin"].(map[string]interface{}); ok {
			for _, value := range admin {
				if flag, ok := value.(bool); ok {
					adminFlags = append(adminFlags, flag)
				}
			}
		}
		r.AdminSummary = SummarizeAdmin(raw["admin"])
		r.IMHandles = SummarizeIMIdentities(r.AgentDir)
		r.CreatedAt = rawString(raw, "created_at")
		r.MoltCount, r.MoltCountAvailable = rawInt(raw, "molt_count")
	}
	status := fs.ReadStatus(r.AgentDir)
	ctx := status.Tokens.Context
	if ctx.WindowSize > 0 {
		r.ContextTotalTokens = ctx.TotalTokens
		r.ContextWindowSize = ctx.WindowSize
		r.ContextUsagePct = ctx.UsagePct
		r.ContextAvailable = true
	}
	r.Heartbeat = fs.ReadHeartbeat(r.AgentDir, fs.AgentAliveThresholdSec())
	r.LockExists = fileExists(filepath.Join(r.AgentDir, ".agent.lock"))
	return adminFlags
}

// RoleFor is retained for callers projecting an already observed record.
// New production inventory gets its role with the complete bound query.
func RoleFor(r Record) Role {
	if r.Role != "" {
		return r.Role
	}
	observation := Observation{Members: []MemberObservation{{Row: 0,
		Directory: []byte(r.AgentDir), Project: []byte(r.Project),
		ProjectOrder: []byte{}, NameOrder: []byte{}, DirectoryOrder: []byte{},
		AdminNull: r.IsHuman, AdminFlags: []bool{r.IsOrchestrator}, ManifestFailed: r.ReadError != "",
	}}, Projects: []ProjectObservation{}, IncludeHuman: true}
	decision, err := query(observation)
	if err != nil || len(decision.Members) != 1 {
		return RoleUnknown
	}
	return decision.Members[0].Role
}

func containerMissing(path string) bool {
	_, err := os.Stat(path)
	return os.IsNotExist(err)
}

func rawString(raw map[string]interface{}, key string) string {
	value, _ := raw[key].(string)
	return strings.TrimSpace(value)
}

func rawInt(raw map[string]interface{}, key string) (int, bool) {
	value, ok := raw[key]
	if !ok {
		return 0, false
	}
	switch n := value.(type) {
	case float64:
		whole := int(n)
		if n < 0 || n != float64(whole) {
			return 0, false
		}
		return whole, true
	case int:
		return n, n >= 0
	default:
		return 0, false
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

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func SummarizeIMIdentities(agentDir string) string {
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
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return ""
}

func SummarizeAdmin(adminRaw interface{}) string {
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

// HumanUptimeFromEtime converts a ps etime value ([[dd-]hh:]mm:ss) into the
// human-readable uptime used by the CLI and TUI ("2d 3h", "1h 2m", "4m 9s").
// Unparseable values are returned unchanged.
func HumanUptimeFromEtime(etime string) string {
	secs, ok := parseEtimeSeconds(etime)
	if !ok {
		return etime
	}
	d := time.Duration(secs) * time.Second
	switch {
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
	case d >= time.Hour:
		return fmt.Sprintf("%dh %dm", int(d.Hours()), int(d.Minutes())%60)
	default:
		return fmt.Sprintf("%dm %ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

func parseEtimeSeconds(etime string) (int, bool) {
	etime = strings.TrimSpace(etime)
	days := 0
	if day, rest, ok := strings.Cut(etime, "-"); ok {
		d, err := strconv.Atoi(day)
		if err != nil || d < 0 {
			return 0, false
		}
		days = d
		etime = rest
	}
	parts := strings.Split(etime, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	secs := 0
	for _, part := range parts {
		v, err := strconv.Atoi(part)
		if err != nil || v < 0 {
			return 0, false
		}
		secs = secs*60 + v
	}
	return days*24*60*60 + secs, true
}

func HeartbeatLabel(h fs.HeartbeatStatus) string {
	switch {
	case h.Fresh:
		return "fresh"
	case h.Exists:
		return "stale"
	default:
		return "missing"
	}
}
