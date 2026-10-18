package process

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// ProjectLifecycleOptions is bound before the child starts. Neither paths,
// preset references, wait durations nor launch commands are accepted from wire.
// Action has only the eight existing external actions below; no interrupt API.
type ProjectLifecycleOptions struct {
	Action      string
	Root        string
	Selected    string
	All         bool
	PresetPath  string
	Force       bool
	WaitTimeout time.Duration
}

// ProjectLifecycleNative supplies only mechanisms private to the TUI caller.
// Native process/FS implementations below retain the old error objects. The
// heartbeat callback receives the ORIGINAL launched *exec.Cmd, never a wire PID.
// Clear callbacks close over the original cfg/deps and captured evidence.
type ProjectLifecycleNative struct {
	Puffo               func(string) bool
	ProbeLock           func(string) bool
	ResetPreset         func(string)
	SetPreset           func(string, string) error
	Heartbeat           func(*exec.Cmd, string, time.Duration) error
	ClearAlive          func(string) bool
	ReviveForClear      func(string) error
	ClearAliveWait      func(string) error
	ClearCapture        func(string)
	ClearCompletionWait func(string) error
	ClearSuspend        func(string) error
}

type ProjectLifecycleFailure struct {
	Target    int    `json:"target"`
	Sequence  int    `json:"sequence"`
	Directory string `json:"-"`
	Err       error  `json:"-"`
}

type ProjectLifecycleResult struct {
	Code                string                    `json:"code"`
	ErrorSequence       int                       `json:"error_sequence"`
	Count               int                       `json:"count"`
	Completed           bool                      `json:"completed"`
	AlreadyAlive        bool                      `json:"already_alive"`
	Failures            []ProjectLifecycleFailure `json:"failures"`
	Survivors           []int                     `json:"survivors"`
	SurvivorDirectories []string                  `json:"-"`
}

type ProjectLifecycleUnknown struct{}

func (*ProjectLifecycleUnknown) Error() string {
	return "Project lifecycle outcome unknown; native effects may remain; inspect before another action (no automatic replay, fallback, rollback or repair)"
}

// Refusals retain the exact native discovery error but presentation remains
// with cleanProject. This is returned only after closed terminal validation.
type ProjectLifecycleRefusal struct {
	Code  string
	Cause error
}

func (e *ProjectLifecycleRefusal) Error() string { return "Project lifecycle refused: " + e.Code }
func (e *ProjectLifecycleRefusal) Unwrap() error { return e.Cause }

type lifecycleMember struct {
	Target int  `json:"target"`
	Human  bool `json:"human"`
}
type lifecycleFrame struct {
	Status    string `json:"status"`
	RequestID string `json:"request_id"`
	Sequence  int    `json:"sequence"`
	Operation string `json:"operation"`
	Target    int    `json:"target"`
	Argument  string `json:"argument"`
}
type lifecycleAck struct {
	Status           string            `json:"status"`
	RequestID        string            `json:"request_id"`
	Sequence         int               `json:"sequence"`
	Operation        string            `json:"operation"`
	Target           int               `json:"target"`
	Argument         string            `json:"argument"`
	Flag             bool              `json:"flag"`
	Failed           bool              `json:"failed"`
	OwnershipChanged bool              `json:"ownership_changed"`
	Members          []lifecycleMember `json:"members"`
}
type retainedLifecycle struct {
	frame lifecycleFrame
	ack   lifecycleAck
	err   error
}
type lifecycleSession struct {
	options     ProjectLifecycleOptions
	native      ProjectLifecycleNative
	requestID   string
	command     string
	paths       map[int]string
	identities  map[string]int
	commands    map[int]*exec.Cmd
	transcript  []lifecycleAck
	retained    map[int]retainedLifecycle
	calls       map[string]int
	discoveries int
	deadline    time.Time
	stopped     bool
}

var projectLifecycleSerial uint64

func newLifecycleSession(options ProjectLifecycleOptions, native ProjectLifecycleNative) *lifecycleSession {
	s := &lifecycleSession{options: options, native: native,
		requestID: strconv.FormatUint(atomic.AddUint64(&projectLifecycleSerial, 1), 10),
		paths:     map[int]string{}, identities: map[string]int{}, commands: map[int]*exec.Cmd{},
		transcript: []lifecycleAck{}, retained: map[int]retainedLifecycle{}, calls: map[string]int{}}
	if options.Selected != "" && !options.All && options.Action != "clean" && options.Action != "nirvana" {
		s.paths[0] = options.Selected
	}
	return s
}

func (s *lifecycleSession) allowed(f lifecycleFrame) bool {
	a := s.options.Action
	switch f.Operation {
	case "discover":
		return (s.options.All && (a == "sleep" || a == "suspend" || a == "cpr" || a == "refresh")) || a == "clean" || a == "nirvana"
	case "alive":
		return f.Argument == "standard" && (a == "sleep" || a == "suspend" || a == "cpr" || a == "clean" || a == "nirvana") || f.Argument == "clear" && a == "clear"
	case "write_signal":
		return (f.Argument == a && (a == "sleep" || a == "suspend")) || f.Argument == "suspend" && (a == "refresh" || a == "clean") || f.Argument == "clear" && a == "clear"
	case "external_owner", "probe_lock", "heartbeat":
		return a == "refresh" || a == "revive" || a == "cpr"
	case "sleep":
		return f.Argument == "500" && (a == "refresh" || a == "revive" || a == "cpr") || f.Argument == "250" && a == "clean"
	case "remove_handshake":
		return a == "refresh" && (f.Argument == "lock" || f.Argument == "refresh" || f.Argument == "refresh_taken" || f.Argument == "suspend") || (a == "revive" || a == "cpr") && f.Argument == "lock"
	case "running", "terminate":
		return a == "refresh"
	case "preset":
		return a == "refresh" && (f.Argument == "explicit") == (s.options.PresetPath != "") && (f.Argument == "default" || f.Argument == "explicit")
	case "launch":
		return a == "refresh" && f.Argument == "force" || (a == "revive" || a == "cpr") && f.Argument == "normal"
	case "revive_for_clear", "clear_alive_wait", "clear_capture", "clear_completion_wait", "clear_suspend":
		return a == "clear"
	case "clean_wait_start", "clean_before_deadline":
		return a == "clean"
	case "clean_survivor_notice":
		return a == "clean" && s.options.Force
	case "suspend_wait", "reset_barrier":
		return a == "nirvana"
	case "remove_tree":
		return a == "clean" || a == "nirvana"
	}
	return false
}

// cleanupReceipt checks the specialized clean/nirvana wire grammar against
// retained native receipts. It neither discovers eligibility independently nor
// dispatches a phase: Core still chooses every request. A nil next validates a
// complete receipt; a nonnil next must be exactly the next legal native frame.
func (s *lifecycleSession) cleanupReceipt(next *lifecycleFrame) (string, int, []int, bool) {
	phase, index, cursor := "discover", 0, 0
	members, alive, guarded, survivors := []int{}, []int{}, []int{}, []int{}
	code, errorSequence := "ok", 0
	for {
		// Empty lists advance without inventing observations. Polling uses the
		// Core's short-circuit any(), unlike the final full survivor pass.
		switch phase {
		case "signal", "nirvana_alive":
			if index == len(members) {
				index = 0
				if s.options.Action == "nirvana" {
					phase = "barrier"
				} else if len(alive) != 0 {
					phase = "wait"
				} else {
					phase = "rediscover"
				}
				continue
			}
		case "poll":
			if index == len(alive) {
				phase = "rediscover"
				continue
			}
		case "survivor":
			if index == len(guarded) {
				if len(survivors) == 0 {
					phase = "remove"
				} else if s.options.Force {
					phase = "notice"
				} else {
					phase, code = "done", "survivors"
				}
				continue
			}
		case "done":
			return code, errorSequence, survivors, next == nil && cursor == len(s.transcript)
		}
		op, target, argument := "", -1, ""
		switch phase {
		case "discover", "rediscover":
			op = "discover"
		case "signal":
			op, target, argument = "write_signal", members[index], "suspend"
		case "initial_alive", "nirvana_alive":
			op, target, argument = "alive", members[index], "standard"
		case "suspend":
			op, target = "suspend_wait", members[index]
		case "wait":
			op, argument = "clean_wait_start", strconv.Itoa(len(alive))
		case "deadline":
			op = "clean_before_deadline"
		case "poll":
			op, target, argument = "alive", alive[index], "standard"
		case "sleep":
			op, argument = "sleep", "250"
		case "survivor":
			op, target, argument = "alive", guarded[index], "standard"
		case "notice":
			op, argument = "clean_survivor_notice", strconv.Itoa(len(survivors))
		case "barrier":
			op = "reset_barrier"
		case "remove":
			op = "remove_tree"
		default:
			return "", 0, nil, false
		}
		if cursor == len(s.transcript) {
			return "", 0, nil, next != nil && next.Operation == op && next.Target == target && next.Argument == argument
		}
		ack := s.transcript[cursor]
		if ack.Operation != op || ack.Target != target || ack.Argument != argument {
			return "", 0, nil, false
		}
		cursor++
		switch phase {
		case "discover":
			if ack.Failed && s.options.Action == "clean" && !s.options.Force {
				phase, code, errorSequence = "done", "discovery", ack.Sequence
				continue
			}
			for _, member := range ack.Members {
				if !member.Human {
					members = append(members, member.Target)
				}
			}
			phase = "signal"
			if s.options.Action == "nirvana" {
				phase = "nirvana_alive"
			}
		case "signal":
			phase = "initial_alive" // suspend failures remain best effort
		case "initial_alive":
			if ack.Flag {
				alive = append(alive, ack.Target)
			}
			index++
			phase = "signal"
		case "nirvana_alive":
			if ack.Flag {
				phase = "suspend"
			} else {
				index++
			}
		case "suspend":
			index++
			phase = "nirvana_alive"
		case "wait", "sleep":
			phase = "deadline"
		case "deadline":
			phase, index = "poll", 0
			if !ack.Flag {
				phase = "rediscover"
			}
		case "poll":
			if ack.Flag {
				phase = "sleep"
			} else {
				index++
			}
		case "rediscover":
			if ack.Failed && !s.options.Force {
				phase, code, errorSequence = "done", "rediscovery", ack.Sequence
				continue
			}
			guarded = append(guarded, alive...)
			seen := map[int]bool{}
			for _, target := range alive {
				seen[target] = true
			}
			if !ack.Failed {
				for _, member := range ack.Members {
					if !member.Human && !seen[member.Target] {
						guarded = append(guarded, member.Target)
					}
				}
			}
			phase, index = "survivor", 0
		case "survivor":
			if ack.Flag {
				survivors = append(survivors, ack.Target)
			}
			index++
		case "notice", "barrier":
			phase = "remove"
		case "remove":
			phase = "done"
			if ack.Failed && s.options.Action == "clean" {
				code, errorSequence = "native", ack.Sequence
			}
		}
	}
}

// A terminal native refusal closes that target, not the whole all-target
// exchange. Only the original defensive refresh/temporary-clear tail remains
// legal; best-effort preset/signal/termination errors are not refusals.
func (s *lifecycleSession) afterNativeRefusal(f lifecycleFrame) bool {
	target := f.Target
	if target == -1 {
		for _, ack := range s.transcript {
			if ack.Target >= 0 {
				target = ack.Target
			}
		}
	}
	for _, r := range s.retained {
		if r.frame.Target != target || r.err == nil {
			continue
		}
		op := r.frame.Operation
		refused, cleanup := false, false
		switch s.options.Action {
		case "refresh", "revive", "cpr":
			refused = op == "external_owner" || op == "launch" || op == "heartbeat" || op == "terminate" && r.ack.OwnershipChanged
			cleanup = s.options.Action == "refresh" && op == "launch" && f.Operation == "remove_handshake" && f.Argument == "suspend"
		case "clear":
			refused = op == "revive_for_clear" || op == "clear_alive_wait" || op == "write_signal" || op == "clear_completion_wait" || op == "clear_suspend"
			cleanup = (op == "clear_alive_wait" || op == "write_signal" || op == "clear_completion_wait") && f.Operation == "clear_suspend"
			if op == "write_signal" && cleanup {
				cleanup = false
				for _, ack := range s.transcript {
					if ack.Operation == "clear_capture" && ack.Sequence < r.frame.Sequence {
						cleanup = true
					}
				}
			}
		}
		if refused {
			if !cleanup {
				return false
			}
			for _, ack := range s.transcript {
				if ack.Sequence > r.frame.Sequence && ack.Target == f.Target {
					return false
				}
			}
		}
	}
	return true
}

// observe validates capability binding BEFORE effects. Each requested operation
// is synchronous on the caller goroutine; once-only means no duplicate effect
// dispatch even if a child repeats a frame with a fresh sequence.
func (s *lifecycleSession) observe(f lifecycleFrame) (lifecycleAck, error) {
	bad := func() (lifecycleAck, error) { s.stopped = true; return lifecycleAck{}, &ProjectLifecycleUnknown{} }
	if s.stopped || f.Status != "project_lifecycle_native" || f.RequestID != s.requestID || f.Sequence != len(s.transcript)+1 || !s.allowed(f) {
		return bad()
	}
	if !s.afterNativeRefusal(f) {
		return bad()
	}
	if s.options.Action == "clean" || s.options.Action == "nirvana" {
		if _, _, _, valid := s.cleanupReceipt(&f); !valid {
			return bad()
		}
	}
	global := f.Operation == "discover" || f.Operation == "sleep" || f.Operation == "clean_wait_start" || f.Operation == "clean_before_deadline" || f.Operation == "clean_survivor_notice" || f.Operation == "reset_barrier" || f.Operation == "remove_tree"
	dir, exists := s.paths[f.Target]
	if global {
		if f.Target != -1 {
			return bad()
		}
	} else if !exists || (!s.options.All && s.options.Action != "clean" && s.options.Action != "nirvana" && f.Target != 0) {
		return bad()
	}
	switch f.Operation {
	case "alive", "write_signal", "remove_handshake", "preset", "launch", "sleep", "clean_wait_start", "clean_survivor_notice":
	default:
		if f.Argument != "" {
			return bad()
		}
	}
	key := f.Operation + ":" + strconv.Itoa(f.Target) + ":" + f.Argument
	if f.Operation == "clean_wait_start" || f.Operation == "clean_survivor_notice" {
		key = f.Operation
	}
	limit := 1
	switch f.Operation {
	case "alive", "probe_lock", "sleep", "clean_before_deadline":
		limit = 0 // actual native observations/pacing, not effects replay
	case "discover":
		if s.options.Action == "clean" {
			limit = 2
		}
	case "remove_handshake":
		// Timeout removal + refresh sweep, or prelaunch + defensive postlaunch.
		if s.options.Action == "refresh" && (f.Argument == "lock" || f.Argument == "suspend") {
			limit = 2
		}
	}
	if limit != 0 && s.calls[key] >= limit {
		return bad()
	}
	if f.Operation == "clean_wait_start" || f.Operation == "clean_survivor_notice" {
		count, err := strconv.Atoi(f.Argument)
		if err != nil || count < 1 || strconv.Itoa(count) != f.Argument {
			return bad()
		}
		// The count is display only; it cannot enlarge a native capability.
	}
	s.calls[key]++ // record BEFORE invoking native code (also covers panic unwind)
	ack := lifecycleAck{Status: "project_lifecycle_observed", RequestID: s.requestID,
		Sequence: f.Sequence, Operation: f.Operation, Target: f.Target,
		Argument: f.Argument, Members: []lifecycleMember{}}
	var original error
	switch f.Operation {
	case "discover":
		members, err := fs.DiscoverAgents(s.options.Root)
		original = err
		s.discoveries++
		if err != nil && s.options.Action == "clean" && s.options.Force && s.discoveries == 1 {
			fmt.Fprintf(os.Stderr, "Warning: cannot list agents under %s (%v); removing anyway (--force)\n", s.options.Root, err)
		}
		for _, m := range members {
			id, ok := s.identities[m.WorkingDir]
			if !ok {
				id = len(s.identities) + 1
				s.identities[m.WorkingDir] = id
				s.paths[id] = m.WorkingDir
			}
			ack.Members = append(ack.Members, lifecycleMember{Target: id, Human: m.IsHuman})
		}
	case "alive":
		if f.Argument == "clear" {
			if s.native.ClearAlive == nil {
				return bad()
			}
			ack.Flag = s.native.ClearAlive(dir)
		} else {
			ack.Flag = fs.IsAlive(dir, fs.AgentAliveThresholdSec())
		}
	case "external_owner":
		if s.native.Puffo == nil {
			return bad()
		}
		ack.Flag = s.native.Puffo(dir)
		if ack.Flag {
			original = ErrPuffoManagedAgent
		}
	case "probe_lock":
		if s.native.ProbeLock == nil {
			return bad()
		}
		ack.Flag = s.native.ProbeLock(filepath.Join(dir, ".agent.lock"))
	case "write_signal":
		name, contents := "."+f.Argument, ""
		if f.Argument == "clear" {
			contents = "tui\n"
		}
		original = os.WriteFile(filepath.Join(dir, name), []byte(contents), 0o644)
		if original != nil && s.options.Action == "clean" {
			fmt.Fprintf(os.Stderr, "Failed to signal %s: %v\n", dir, original)
		}
	case "sleep":
		milliseconds, _ := strconv.Atoi(f.Argument)
		time.Sleep(time.Duration(milliseconds) * time.Millisecond)
	case "remove_handshake":
		name := map[string]string{"lock": ".agent.lock", "refresh": ".refresh", "refresh_taken": ".refresh.taken", "suspend": ".suspend"}[f.Argument]
		original = os.Remove(filepath.Join(dir, name))
	case "running":
		ack.Flag = IsAgentRunning(dir)
	case "terminate":
		original = TerminateAgentProcesses(dir)
		ack.OwnershipChanged = errors.Is(original, ErrPuffoManagedAgent)
	case "preset":
		if f.Argument == "explicit" {
			if s.native.SetPreset == nil {
				return bad()
			}
			original = s.native.SetPreset(dir, s.options.PresetPath)
		} else {
			if s.native.ResetPreset == nil {
				return bad()
			}
			s.native.ResetPreset(dir)
		}
	case "launch":
		var cmd *exec.Cmd
		if f.Argument == "force" {
			cmd, original = ForceLaunchAgent(s.launchCommand(), dir)
		} else {
			cmd, original = LaunchAgent(s.launchCommand(), dir)
		}
		s.commands[f.Target] = cmd
	case "heartbeat":
		if s.native.Heartbeat == nil {
			return bad()
		}
		if _, ok := s.commands[f.Target]; !ok {
			return bad()
		}
		original = s.native.Heartbeat(s.commands[f.Target], dir, 10*time.Second)
	case "revive_for_clear":
		if s.native.ReviveForClear == nil {
			return bad()
		}
		original = s.native.ReviveForClear(dir)
	case "clear_alive_wait":
		if s.native.ClearAliveWait == nil {
			return bad()
		}
		original = s.native.ClearAliveWait(dir)
	case "clear_capture":
		if s.native.ClearCapture == nil {
			return bad()
		}
		s.native.ClearCapture(dir)
	case "clear_completion_wait":
		if s.native.ClearCompletionWait == nil {
			return bad()
		}
		original = s.native.ClearCompletionWait(dir)
	case "clear_suspend":
		if s.native.ClearSuspend == nil {
			return bad()
		}
		original = s.native.ClearSuspend(dir)
	case "clean_wait_start":
		fmt.Printf("Suspending %s agent(s)...\n", f.Argument)
		s.deadline = time.Now().Add(s.options.WaitTimeout)
	case "clean_before_deadline":
		if s.deadline.IsZero() {
			return bad()
		}
		ack.Flag = time.Now().Before(s.deadline)
	case "clean_survivor_notice":
		fmt.Fprintf(os.Stderr, "Warning: removing %s with %s live agent(s) (--force)\n", s.options.Root, f.Argument)
	case "suspend_wait":
		fs.SuspendAndWait(dir, 5*time.Second)
	case "reset_barrier":
		original = fs.RemoveHumanManifestForReset(filepath.Join(s.options.Root, "human"))
	case "remove_tree":
		original = os.RemoveAll(s.options.Root)
	}
	ack.Failed = original != nil
	s.retained[f.Sequence] = retainedLifecycle{f, ack, original}
	s.transcript = append(s.transcript, ack)
	return ack, nil
}

// launchCommand is the caller's original fallback. It is not the interpreter
// chosen to run this exchange (which may come from selected init/global venv).
func (s *lifecycleSession) launchCommand() string { return s.command }

const lifecycleFrameFields = "status request_id sequence operation target argument"
const lifecycleAckFields = lifecycleFrameFields + " flag failed ownership_changed members"
const lifecycleResultFields = "code error_sequence count completed already_alive failures survivors"

func (s *lifecycleSession) nativeFailure(sequence, target int) (error, bool) {
	r, ok := s.retained[sequence]
	if !ok || r.err == nil || r.frame.Target != target {
		return nil, false
	}
	allowed := false
	switch s.options.Action {
	case "refresh", "revive", "cpr":
		allowed = r.frame.Operation == "external_owner" || r.frame.Operation == "launch" || r.frame.Operation == "heartbeat" || r.frame.Operation == "terminate" && r.ack.OwnershipChanged
	case "clear":
		allowed = r.frame.Operation == "write_signal" || r.frame.Operation == "revive_for_clear" || r.frame.Operation == "clear_alive_wait" || r.frame.Operation == "clear_completion_wait" || r.frame.Operation == "clear_suspend"
	case "clean":
		allowed = r.frame.Operation == "remove_tree"
	}
	if !allowed {
		return nil, false
	}
	var tail []lifecycleAck
	for _, ack := range s.transcript {
		if ack.Sequence > sequence && ack.Target == target {
			tail = append(tail, ack)
		}
	}
	switch {
	case s.options.Action == "refresh" && r.frame.Operation == "launch":
		allowed = len(tail) == 1 && tail[0].Operation == "remove_handshake" && tail[0].Argument == "suspend"
	case s.options.Action == "clear" && (r.frame.Operation == "clear_alive_wait" || r.frame.Operation == "clear_completion_wait"):
		allowed = len(tail) == 1 && tail[0].Operation == "clear_suspend"
	case s.options.Action == "clear" && r.frame.Operation == "write_signal":
		allowed = len(tail) == 0 || len(tail) == 1 && tail[0].Operation == "clear_suspend"
	default:
		allowed = len(tail) == 0
	}
	// Completion failure wins even if temporary suspension also failed.
	if s.options.Action == "clear" && r.frame.Operation == "clear_suspend" {
		for _, ack := range s.transcript {
			if ack.Operation == "clear_completion_wait" && ack.Failed {
				allowed = false
			}
		}
	}
	return r.err, allowed
}

// finish is called only AFTER accepted terminal + EOF + exit 0 + empty stderr.
// Every restored error is the retained ORIGINAL from this request's exact
// sequence/operation/target/argument, not a reconstructed remote exception.
func (s *lifecycleSession) finish(data []byte) (ProjectLifecycleResult, error) {
	unknown := func() (ProjectLifecycleResult, error) { return ProjectLifecycleResult{}, &ProjectLifecycleUnknown{} }
	var terminal struct {
		Status     string                 `json:"status"`
		RequestID  string                 `json:"request_id"`
		Action     string                 `json:"action"`
		Result     ProjectLifecycleResult `json:"result"`
		Transcript []lifecycleAck         `json:"transcript"`
	}
	raw, err := recipeObject(data, &terminal, "status request_id action result transcript", "")
	if s.stopped || err != nil || terminal.Status != "project_lifecycle_complete" || terminal.RequestID != s.requestID || terminal.Action != s.options.Action || !reflect.DeepEqual(terminal.Transcript, s.transcript) {
		return unknown()
	}
	var transcript []json.RawMessage
	if json.Unmarshal(raw["transcript"], &transcript) != nil {
		return unknown()
	}
	for _, item := range transcript {
		ackRaw, err := recipeObject(item, nil, lifecycleAckFields, "")
		if err != nil {
			return unknown()
		}
		var members []json.RawMessage
		if json.Unmarshal(ackRaw["members"], &members) != nil {
			return unknown()
		}
		for _, member := range members {
			if _, err := recipeObject(member, nil, "target human", ""); err != nil {
				return unknown()
			}
		}
	}
	result := terminal.Result
	resultRaw, err := recipeObject(raw["result"], nil, lifecycleResultFields, "")
	if err != nil || result.Count < 0 || result.ErrorSequence < 0 {
		return unknown()
	}
	if s.options.All && result.Code != "ok" {
		return unknown()
	}
	if !s.options.All && s.options.Action != "sleep" && s.options.Action != "suspend" && result.Count != 0 {
		return unknown()
	}
	if s.options.Action == "clean" || s.options.Action == "nirvana" {
		code, sequence, survivors, valid := s.cleanupReceipt(nil)
		if !valid || result.Code != code || result.ErrorSequence != sequence || !reflect.DeepEqual(result.Survivors, survivors) {
			return unknown()
		}
	}
	var failures []json.RawMessage
	if json.Unmarshal(resultRaw["failures"], &failures) != nil {
		return unknown()
	}
	failedTargets := map[int]bool{}
	for i, item := range failures {
		if _, err := recipeObject(item, nil, "target sequence", ""); err != nil {
			return unknown()
		}
		f := &result.Failures[i]
		dir, exists := s.paths[f.Target]
		original, valid := s.nativeFailure(f.Sequence, f.Target)
		if !exists || !valid || failedTargets[f.Target] || !s.options.All || (s.options.Action != "refresh" && s.options.Action != "cpr") {
			return unknown()
		}
		failedTargets[f.Target] = true
		f.Directory, f.Err = dir, original
	}
	if len(result.Failures) > result.Count {
		return unknown()
	}
	for _, target := range result.Survivors {
		dir, exists := s.paths[target]
		if !exists || s.options.Action != "clean" {
			return unknown()
		}
		result.SurvivorDirectories = append(result.SurvivorDirectories, dir)
	}
	if result.Completed && s.options.Action != "clear" || result.AlreadyAlive && (s.options.Action != "cpr" || s.options.All) {
		return unknown()
	}
	if result.Code == "native" {
		r, ok := s.retained[result.ErrorSequence]
		if !ok {
			return unknown()
		}
		original, valid := s.nativeFailure(result.ErrorSequence, r.frame.Target)
		if !valid || result.Completed && r.frame.Operation != "clear_suspend" {
			return unknown()
		}
		return result, original
	}
	if result.Code == "discovery" || result.Code == "rediscovery" {
		r, ok := s.retained[result.ErrorSequence]
		if !ok || s.options.Action != "clean" || s.options.Force || r.frame.Operation != "discover" || r.err == nil {
			return unknown()
		}
		return result, &ProjectLifecycleRefusal{Code: result.Code, Cause: r.err}
	}
	if result.ErrorSequence != 0 {
		return unknown()
	}
	if result.Code == "survivors" {
		if s.options.Action != "clean" || s.options.Force || len(result.Survivors) == 0 {
			return unknown()
		}
		return result, &ProjectLifecycleRefusal{Code: "survivors"}
	}
	if result.Code != "ok" {
		return unknown()
	}
	// Receipt validation is not a second eligibility policy: successful final
	// native phases and the reported attempt count must actually be present.
	last := lifecycleAck{}
	if len(s.transcript) > 0 {
		last = s.transcript[len(s.transcript)-1]
	}
	switch s.options.Action {
	case "sleep", "suspend":
		writes := 0
		for _, ack := range s.transcript {
			if ack.Operation == "write_signal" {
				writes++
			}
		}
		if writes != result.Count {
			return unknown()
		}
	case "refresh", "revive", "cpr":
		if s.options.All {
			attempts := 0
			lastByTarget := map[int]lifecycleAck{}
			attemptedTargets := map[int]bool{}
			for _, ack := range s.transcript {
				lastByTarget[ack.Target] = ack
				if ack.Operation == "external_owner" {
					attempts++
					attemptedTargets[ack.Target] = true
				}
			}
			if attempts != result.Count {
				return unknown()
			}
			for target := range attemptedTargets {
				last := lastByTarget[target]
				if !failedTargets[target] && (last.Operation != "heartbeat" || last.Failed) {
					return unknown()
				}
			}
		} else if result.AlreadyAlive {
			if last.Operation != "alive" || !last.Flag {
				return unknown()
			}
		} else if s.options.Selected != "" {
			if last.Operation != "heartbeat" || last.Failed {
				return unknown()
			}
		} else if s.options.Action != "cpr" {
			return unknown()
		}
	case "clear":
		if result.Completed {
			if last.Operation != "clear_suspend" || last.Failed {
				return unknown()
			}
		} else if last.Operation != "write_signal" || last.Argument != "clear" || last.Failed {
			return unknown()
		}
	case "clean":
		if last.Operation != "remove_tree" || last.Failed {
			return unknown()
		}
	case "nirvana":
		if last.Operation != "remove_tree" || len(s.transcript) < 2 || s.transcript[len(s.transcript)-2].Operation != "reset_barrier" {
			return unknown()
		}
	}
	return result, nil
}

// RunProjectLifecycle launches only the specialized Project use case. It does
// not launch any action at authoring time; production callbacks execute once on
// the original calling goroutine. There is no worker, deadline, retry or cap.
func RunProjectLifecycle(command string, options ProjectLifecycleOptions, native ProjectLifecycleNative) (ProjectLifecycleResult, error) {
	unknown := func() (ProjectLifecycleResult, error) { return ProjectLifecycleResult{}, &ProjectLifecycleUnknown{} }
	switch options.Action {
	case "sleep", "suspend", "refresh", "revive", "cpr", "clear", "clean", "nirvana":
	default:
		return unknown()
	}
	if options.All && options.Action != "sleep" && options.Action != "suspend" && options.Action != "refresh" && options.Action != "cpr" || options.Force && options.Action != "clean" || options.PresetPath != "" && (options.Action != "refresh" || options.All) {
		return unknown()
	}
	s := newLifecycleSession(options, native)
	s.command = command
	python := command
	if python == "" {
		global, err := config.GlobalDirPath() // non-mutating lookup, never bootstrap
		if err != nil {
			return unknown()
		}
		python = config.VenvPython(config.RuntimeVenvDir(global))
	}
	cmd := exec.Command(kernelCLI(python), "project", "lifecycle")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return unknown()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return unknown()
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if cmd.Start() != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return unknown()
	}
	finished := false
	defer func() {
		_ = stdin.Close()
		if !finished {
			// Includes callback panic/Goexit. Own direct child only; no tree kill,
			// substitute acknowledgement, next effect, fallback or compensation.
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = stdout.Close()
	}()
	writer := bufio.NewWriter(stdin)
	send := func(v interface{}) error {
		if err := json.NewEncoder(writer).Encode(v); err != nil {
			return err
		}
		return writer.Flush()
	}
	request := struct {
		RequestID      string `json:"request_id"`
		Action         string `json:"action"`
		AllTargets     bool   `json:"all_targets"`
		Selected       bool   `json:"selected"`
		ExplicitPreset bool   `json:"explicit_preset"`
		Force          bool   `json:"force"`
	}{s.requestID, options.Action, options.All, options.Selected != "", options.PresetPath != "", options.Force}
	if send(request) != nil {
		return unknown()
	}
	reader := bufio.NewReader(stdout)
	var terminal []byte
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil || terminal != nil {
			return unknown()
		}
		var tag struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(line, &tag) != nil {
			return unknown()
		}
		if tag.Status == "project_lifecycle_complete" {
			// Reject unknown shapes before reading another frame: malformed
			// producers may themselves be waiting for an acknowledgement.
			if _, err := recipeObject(line, nil, "status request_id action result transcript", ""); err != nil {
				return unknown()
			}
			terminal = line
			continue
		}
		var frame lifecycleFrame
		if _, err := recipeObject(line, &frame, lifecycleFrameFields, ""); err != nil {
			return unknown()
		}
		ack, err := s.observe(frame)
		if err != nil || send(ack) != nil {
			return unknown()
		}
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	finished = true
	if terminal == nil || waitErr != nil || stderr.Len() != 0 {
		return unknown()
	}
	return s.finish(terminal)
}

// CleanProjectLifecycle keeps CLI-only prose and original native causes here,
// outside Core; confirmation is still cleanMain's responsibility.
func CleanProjectLifecycle(root string, force bool, waitTimeout time.Duration) error {
	result, err := RunProjectLifecycle("", ProjectLifecycleOptions{Action: "clean", Root: root, Force: force, WaitTimeout: waitTimeout}, ProjectLifecycleNative{})
	var refusal *ProjectLifecycleRefusal
	if errors.As(err, &refusal) {
		switch refusal.Code {
		case "discovery":
			return fmt.Errorf("Cannot list agents under %s: %v\nNot removing it: a live agent could be hiding in there. Fix the error, or re-run with --force to delete anyway.", root, refusal.Cause)
		case "rediscovery":
			return fmt.Errorf("Cannot re-list agents under %s before deleting: %v\nNot removing it. Fix the error, or re-run with --force to delete anyway.", root, refusal.Cause)
		case "survivors":
			var b strings.Builder
			fmt.Fprintf(&b, "%d agent(s) still alive after %s:\n", len(result.SurvivorDirectories), waitTimeout)
			for _, dir := range result.SurvivorDirectories {
				fmt.Fprintf(&b, "  %s\n", dir)
			}
			fmt.Fprintf(&b, "Not removing %s.\nWait for them to stop, or re-run with --force to delete anyway.", root)
			return errors.New(b.String())
		}
	}
	if err != nil && result.Code == "native" {
		return fmt.Errorf("Failed to remove %s: %v", root, err)
	}
	return err
}

type lifecyclePresetCandidate struct {
	Exact bool `json:"exact"`
	Stem  bool `json:"stem"`
}

// ResolveExternalProjectPreset sends native string/path comparison evidence,
// NOT a Go-selected policy result. Core owns empty/list/exact-first/uniqueness
// decisions. Raw refs/query never cross wire, including unusual native bytes.
func ResolveExternalProjectPreset(command, dir, query string, allowed func(string) []string) (string, error) {
	unknown := func() (string, error) { return "", &ProjectLifecycleUnknown{} }
	query = strings.TrimSpace(query)
	requestID := strconv.FormatUint(atomic.AddUint64(&projectLifecycleSerial, 1), 10)
	python := command
	if python == "" {
		global, err := config.GlobalDirPath()
		if err != nil {
			return unknown()
		}
		python = config.VenvPython(config.RuntimeVenvDir(global))
	}
	cmd := exec.Command(kernelCLI(python), "project", "lifecycle")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return unknown()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return unknown()
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if cmd.Start() != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return unknown()
	}
	finished := false
	defer func() {
		_ = stdin.Close()
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = stdout.Close()
	}()
	writer := bufio.NewWriter(stdin)
	send := func(v interface{}) error {
		if err := json.NewEncoder(writer).Encode(v); err != nil {
			return err
		}
		return writer.Flush()
	}
	request := struct {
		RequestID     string `json:"request_id"`
		Action        string `json:"action"`
		QueryNonempty bool   `json:"query_nonempty"`
	}{requestID, "resolve_preset", query != ""}
	if send(request) != nil {
		return unknown()
	}
	reader := bufio.NewReader(stdout)
	var terminal []byte
	observed := []lifecyclePresetCandidate{}
	var refs, stems []string
	attempted := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil || terminal != nil {
			return unknown()
		}
		var tag struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(line, &tag) != nil {
			return unknown()
		}
		if tag.Status == "project_preset_resolved" {
			if _, err := recipeObject(line, nil, "status request_id code index matches candidates", ""); err != nil {
				return unknown()
			}
			terminal = line
			continue
		}
		var frame struct {
			Status    string `json:"status"`
			RequestID string `json:"request_id"`
			Sequence  int    `json:"sequence"`
		}
		if _, err := recipeObject(line, &frame, "status request_id sequence", ""); err != nil || frame.Status != "project_preset_candidates" || frame.RequestID != requestID || frame.Sequence != 1 || attempted || query == "" || allowed == nil {
			return unknown()
		}
		attempted = true
		refs = allowed(dir) // original decoder, exactly once, only if Core asks
		for _, ref := range refs {
			stem := strings.TrimSuffix(filepath.Base(ref), ".json")
			stems = append(stems, stem)
			observed = append(observed, lifecyclePresetCandidate{Exact: ref == query, Stem: stem == query})
		}
		ack := struct {
			Status     string                     `json:"status"`
			RequestID  string                     `json:"request_id"`
			Sequence   int                        `json:"sequence"`
			Candidates []lifecyclePresetCandidate `json:"candidates"`
		}{"project_preset_observed", requestID, 1, observed}
		if send(ack) != nil {
			return unknown()
		}
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	finished = true
	if terminal == nil || waitErr != nil || stderr.Len() != 0 {
		return unknown()
	}
	var result struct {
		Status     string                     `json:"status"`
		RequestID  string                     `json:"request_id"`
		Code       string                     `json:"code"`
		Index      int                        `json:"index"`
		Matches    int                        `json:"matches"`
		Candidates []lifecyclePresetCandidate `json:"candidates"`
	}
	raw, err := recipeObject(terminal, &result, "status request_id code index matches candidates", "")
	if err != nil || result.RequestID != requestID || !reflect.DeepEqual(result.Candidates, observed) || result.Matches < 0 {
		return unknown()
	}
	var candidates []json.RawMessage
	if json.Unmarshal(raw["candidates"], &candidates) != nil {
		return unknown()
	}
	for _, item := range candidates {
		if _, err := recipeObject(item, nil, "exact stem", ""); err != nil {
			return unknown()
		}
	}
	switch result.Code {
	case "ok":
		if !attempted || result.Index < 0 || result.Index >= len(refs) || (!observed[result.Index].Exact && !observed[result.Index].Stem) || result.Matches != 0 {
			return unknown()
		}
		return refs[result.Index], nil
	case "empty":
		if attempted || query != "" || result.Index != -1 || result.Matches != 0 {
			return unknown()
		}
		return "", fmt.Errorf("preset name is empty")
	case "no_allowed":
		if !attempted || len(refs) != 0 || result.Index != -1 || result.Matches != 0 {
			return unknown()
		}
		return "", fmt.Errorf("agent has no manifest.preset.allowed list — cannot switch")
	case "ambiguous":
		if !attempted || result.Index != -1 || result.Matches < 2 || result.Matches > len(refs) {
			return unknown()
		}
		return "", fmt.Errorf("preset %q is ambiguous (matches %d entries) — pass the full path", query, result.Matches)
	case "not_allowed":
		if !attempted || result.Index != -1 || result.Matches != 0 {
			return unknown()
		}
		return "", fmt.Errorf("preset %q is not in this agent's allowed list (available: %s)", query, strings.Join(stems, ", "))
	}
	return unknown()
}
