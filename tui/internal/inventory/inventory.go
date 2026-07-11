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
func Scan(opts Options) (Snapshot, error) {
	found, err := processscan.FindAllAgentProcesses()
	if err != nil {
		return Snapshot{}, err
	}
	return FromProcesses(found, opts), nil
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

	records = collapseByAgentDir(records)
	enrichRecords(records)
	phantoms := detectPhantomDirs(records, filterDir)
	for i := range records {
		records[i].Phantom = containsString(phantoms, records[i].Project)
		records[i].Enterable, records[i].EnterReason, records[i].EnterDetail = enterability(records[i])
	}
	if !opts.IncludeHuman {
		out := records[:0]
		for _, r := range records {
			if !r.IsHuman {
				out = append(out, r)
			}
		}
		records = out
	}
	sortRecords(records)
	return Snapshot{
		FilterDir:   filterDir,
		Records:     records,
		Groups:      groupRecords(records),
		PhantomDirs: phantoms,
	}
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

func enrichRecords(records []Record) {
	for i := range records {
		enrichRecord(&records[i])
	}
}

func enrichRecord(r *Record) {
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
		r.IsOrchestrator = fs.IsOrchestratorManifest(raw)
		r.AdminSummary = SummarizeAdmin(raw["admin"])
		if r.IsOrchestrator {
			r.IsHuman = false
		}
		r.IMHandles = SummarizeIMIdentities(r.AgentDir)
	}
	r.Role = RoleFor(*r)
	r.Heartbeat = fs.ReadHeartbeat(r.AgentDir, fs.AgentAliveThresholdSec)
	r.LockExists = fileExists(filepath.Join(r.AgentDir, ".agent.lock"))
}

func RoleFor(r Record) Role {
	switch {
	case r.IsOrchestrator:
		return RoleMain
	case r.IsHuman:
		return RoleHuman
	case r.ReadError != "":
		return RoleUnknown
	default:
		return RoleAgent
	}
}

func enterability(r Record) (bool, EnterabilityReason, string) {
	switch {
	case r.AgentDir == "":
		return false, EnterReasonAgentDirMissing, ""
	case r.Project == "":
		return false, EnterReasonPathOutsideProject, ""
	case r.Phantom:
		return false, EnterReasonPhantomProject, ""
	case r.ReadError != "":
		return false, EnterReasonManifestUnreadable, r.ReadError
	case r.IsHuman:
		return false, EnterReasonHuman, ""
	case !r.IsOrchestrator:
		return false, EnterReasonNonAdmin, ""
	default:
		return true, EnterReasonNone, ""
	}
}

func collapseByAgentDir(records []Record) []Record {
	if len(records) < 2 {
		return records
	}
	out := make([]Record, 0, len(records))
	indexByDir := map[string]int{}
	for _, r := range records {
		if r.AgentDir == "" {
			out = append(out, r)
			continue
		}
		idx, ok := indexByDir[r.AgentDir]
		if !ok {
			indexByDir[r.AgentDir] = len(out)
			out = append(out, r)
			continue
		}
		status := fs.ReadStatus(r.AgentDir)
		if status.Runtime.PID != 0 && r.PID == status.Runtime.PID {
			out[idx] = r
		}
	}
	return out
}

func detectPhantomDirs(records []Record, filterDir string) []string {
	phantoms := map[string]bool{}
	if filterDir != "" {
		lingtaiDir := filepath.Join(filterDir, ".lingtai")
		if _, err := os.Stat(lingtaiDir); os.IsNotExist(err) {
			phantoms[filterDir] = true
		}
		return sortedKeys(phantoms)
	}

	seen := map[string]bool{}
	for _, r := range records {
		if r.Project == "" || seen[r.Project] {
			continue
		}
		seen[r.Project] = true
		lingtaiDir := filepath.Join(r.Project, ".lingtai")
		if _, err := os.Stat(lingtaiDir); os.IsNotExist(err) {
			phantoms[r.Project] = true
		}
	}
	return sortedKeys(phantoms)
}

func sortRecords(records []Record) {
	sort.SliceStable(records, func(i, j int) bool {
		a, b := records[i], records[j]
		for _, cmp := range []int{
			strings.Compare(strings.ToLower(a.Project), strings.ToLower(b.Project)),
			strings.Compare(roleSortKey(a.Role), roleSortKey(b.Role)),
			strings.Compare(strings.ToLower(firstNonEmpty(a.AgentName, a.Agent)), strings.ToLower(firstNonEmpty(b.AgentName, b.Agent))),
			strings.Compare(strings.ToLower(a.AgentDir), strings.ToLower(b.AgentDir)),
		} {
			if cmp < 0 {
				return true
			}
			if cmp > 0 {
				return false
			}
		}
		return a.PID < b.PID
	})
}

func roleSortKey(role Role) string {
	switch role {
	case RoleMain:
		return "0"
	case RoleAgent:
		return "1"
	case RoleUnknown:
		return "2"
	case RoleHuman:
		return "3"
	default:
		return "4"
	}
}

func groupRecords(records []Record) []Group {
	var groups []Group
	groupByProject := map[string]int{}
	for _, r := range records {
		idx, ok := groupByProject[r.Project]
		if !ok {
			idx = len(groups)
			groupByProject[r.Project] = idx
			groups = append(groups, Group{Project: r.Project, Phantom: r.Phantom})
		}
		groups[idx].Records = append(groups[idx].Records, r)
		if r.Phantom {
			groups[idx].Phantom = true
		}
	}
	return groups
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func containsString(values []string, value string) bool {
	for _, v := range values {
		if v == value {
			return true
		}
	}
	return false
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
