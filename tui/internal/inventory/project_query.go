package inventory

import (
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// Observation is the closed native-observation ABI, not a serialized Record.
// Bytes use JSON's base64 encoding. Labels, errors and UI-private fields never
// leave the observing process. Native Go case conversion is supplied as evidence
// so Core need not approximate Go's Unicode/invalid-byte collation.
type MemberObservation struct {
	Row                 int    `json:"row"`
	Directory           []byte `json:"directory"`
	Project             []byte `json:"project"`
	ProjectOrder        []byte `json:"project_order"`
	NameOrder           []byte `json:"name_order"`
	DirectoryOrder      []byte `json:"directory_order"`
	PID                 int    `json:"pid"`
	DuplicateRuntimePID int    `json:"duplicate_runtime_pid"`
	AdminNull           bool   `json:"admin_null"`
	AdminFlags          []bool `json:"admin_flags"`
	ManifestFailed      bool   `json:"manifest_failed"`
}

type ProjectObservation struct {
	Identity         []byte `json:"identity"`
	ContainerMissing bool   `json:"container_missing"`
}

type Observation struct {
	Members      []MemberObservation  `json:"members"`
	Projects     []ProjectObservation `json:"projects"`
	IncludeHuman bool                 `json:"include_human"`
}

type MemberDecision struct {
	DirectoryB64 string             `json:"directory"`
	PID          int                `json:"pid"`
	Row          int                `json:"row"`
	Role         Role               `json:"role"`
	Human        bool               `json:"human"`
	Orchestrator bool               `json:"orchestrator"`
	Phantom      bool               `json:"phantom"`
	Reason       EnterabilityReason `json:"reason"`
}

type Decision struct {
	Status          string           `json:"status"`
	Members         []MemberDecision `json:"members"`
	Groups          [][]int          `json:"groups"`
	PhantomProjects []int            `json:"phantom_projects"`
}

// Query is bound ONCE at the existing application composition edge to the
// concrete managed-kernel query. Absence is an error, never Go policy fallback.
// Tests may bind the same adapter to an explicit hermetic candidate executable.
var Query func(Observation) (Decision, error)

func query(observation Observation) (Decision, error) {
	if Query == nil {
		return Decision{}, fmt.Errorf("Project inventory query is not configured")
	}
	return Query(observation)
}

// Observe enrichment only once per normalized native directory. Duplicate
// runtime-PID observations are taken before enrichment, at exactly the old
// duplicate positions; choosing the winning PID is exclusively Core policy.
func observeRecords(records []Record, filterDir string, includeHuman bool) (Observation, []Record) {
	observation := Observation{Members: make([]MemberObservation, len(records)), Projects: []ProjectObservation{}, IncludeHuman: includeHuman}
	first := map[string]int{}
	unique := []int{}
	for i, r := range records {
		if _, seen := first[r.AgentDir]; seen && r.AgentDir != "" {
			observation.Members[i].DuplicateRuntimePID = fs.ReadStatus(r.AgentDir).Runtime.PID
		} else {
			first[r.AgentDir] = i
			unique = append(unique, i)
		}
	}
	flags := map[string][]bool{}
	for _, i := range unique {
		flags[records[i].AgentDir] = enrichRecord(&records[i])
	}
	for i := range records {
		pid, uptime := records[i].PID, records[i].Uptime
		if source := first[records[i].AgentDir]; source != i && records[i].AgentDir != "" {
			records[i] = records[source]
			records[i].PID, records[i].Uptime = pid, uptime
		}
		r := records[i]
		observation.Members[i] = MemberObservation{
			Row: i, Directory: []byte(r.AgentDir), Project: []byte(r.Project),
			ProjectOrder:   []byte(strings.ToLower(r.Project)),
			NameOrder:      []byte(strings.ToLower(firstNonEmpty(r.AgentName, r.Agent))),
			DirectoryOrder: []byte(strings.ToLower(r.AgentDir)), PID: r.PID,
			DuplicateRuntimePID: observation.Members[i].DuplicateRuntimePID,
			AdminNull:           r.IsHuman, AdminFlags: flags[r.AgentDir], ManifestFailed: r.ReadError != "",
		}
	}
	// Stat-error interpretation deliberately remains os.IsNotExist, not a new
	// directory/readability validation. FilterDir alone is observed when set.
	seen := map[string]bool{}
	observeProject := func(project string) {
		if seen[project] {
			return
		}
		seen[project] = true
		observation.Projects = append(observation.Projects, ProjectObservation{
			Identity: []byte(project), ContainerMissing: containerMissing(filepath.Join(project, ".lingtai")),
		})
	}
	if filterDir != "" {
		observeProject(filterDir)
	} else {
		for _, i := range unique {
			if records[i].Project != "" {
				observeProject(records[i].Project)
			}
		}
	}
	return observation, records
}

// projectSnapshot rebinds ONLY known native rows. No returned native string,
// path, synthesized errno or error text can replace the originals.
func projectSnapshot(filterDir string, records []Record, observation Observation, decision Decision) (Snapshot, error) {
	bad := func() (Snapshot, error) { return Snapshot{}, fmt.Errorf("invalid Project inventory query result") }
	if decision.Status != "inventory_queried" || decision.Members == nil || decision.Groups == nil || decision.PhantomProjects == nil {
		return bad()
	}
	snapshot := Snapshot{FilterDir: filterDir, Records: make([]Record, 0, len(decision.Members))}
	byRow := map[int]Record{}
	for _, d := range decision.Members {
		if d.Row < 0 || d.Row >= len(records) {
			return bad()
		}
		if _, exists := byRow[d.Row]; exists {
			return bad()
		}
		switch d.Role {
		case RoleMain, RoleAgent, RoleHuman, RoleUnknown:
		default:
			return bad()
		}
		switch d.Reason {
		case EnterReasonNone, EnterReasonAgentDirMissing, EnterReasonPathOutsideProject, EnterReasonPhantomProject, EnterReasonManifestUnreadable, EnterReasonHuman, EnterReasonNonAdmin:
		default:
			return bad()
		}
		r := records[d.Row]
		identity, err := base64.StdEncoding.Strict().DecodeString(d.DirectoryB64)
		if err != nil || base64.StdEncoding.EncodeToString(identity) != d.DirectoryB64 || string(identity) != r.AgentDir || d.PID != r.PID {
			return bad()
		}
		// Request row identity must still be the exact retained native identity.
		m := observation.Members[d.Row]
		if m.Row != d.Row || string(m.Directory) != r.AgentDir || string(m.Project) != r.Project || m.PID != r.PID {
			return bad()
		}
		r.Role, r.IsHuman, r.IsOrchestrator, r.Phantom = d.Role, d.Human, d.Orchestrator, d.Phantom
		r.EnterReason, r.Enterable = d.Reason, d.Reason == EnterReasonNone
		if d.Reason == EnterReasonManifestUnreadable {
			r.EnterDetail = r.ReadError
		}
		byRow[d.Row] = r
		snapshot.Records = append(snapshot.Records, r)
	}
	grouped := map[int]bool{}
	projects := map[string]bool{}
	for _, rows := range decision.Groups {
		if len(rows) == 0 {
			return bad()
		}
		group := Group{}
		for i, row := range rows {
			r, ok := byRow[row]
			if !ok || grouped[row] {
				return bad()
			}
			grouped[row] = true
			if i == 0 {
				group.Project = r.Project
			}
			if r.Project != group.Project {
				return bad()
			}
			group.Phantom = group.Phantom || r.Phantom
			group.Records = append(group.Records, r)
		}
		if projects[group.Project] {
			return bad()
		}
		projects[group.Project] = true
		snapshot.Groups = append(snapshot.Groups, group)
	}
	if len(grouped) != len(byRow) {
		return bad()
	}
	phantoms := map[int]bool{}
	for _, index := range decision.PhantomProjects {
		if index < 0 || index >= len(observation.Projects) || phantoms[index] {
			return bad()
		}
		phantoms[index] = true
		snapshot.PhantomDirs = append(snapshot.PhantomDirs, string(observation.Projects[index].Identity))
	}
	return snapshot, nil
}
