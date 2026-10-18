package process

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A portable test-binary child, not Python or a provider. Parent acceptance
// must exercise the real installed kernel as well; these lock transport only.
func TestMain(m *testing.M) {
	if mode := os.Getenv("LINGTAI_PROJECT_DRIVER_FIXTURE"); mode != "" {
		if mode == "readiness_pipe_holder" {
			time.Sleep(4 * time.Second)
			os.Exit(0)
		}
		projectDriverFixture(mode)
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func projectDriverFixture(mode string) {
	if len(os.Args) != 3 || os.Args[1] != "project" {
		os.Exit(2)
	}
	switch os.Args[2] {
	case "recipe-apply", "recipe-observe":
		projectRecipeFixture(mode)
		return
	case "initialize", "configure", "propagate-policy", "propagate-config", "rehydrate":
		projectConfigurationFixture(mode)
		return
	}
	input := bufio.NewReader(os.Stdin)
	line, err := input.ReadBytes('\n')
	if err != nil {
		os.Exit(2)
	}
	if path := os.Getenv("LINGTAI_PROJECT_INPUT_LOG"); path != "" {
		_ = os.WriteFile(path, line, 0600)
	}
	if os.Args[2] == "inspect" {
		if strings.HasPrefix(mode, "readiness_") {
			projectReadinessFixture(mode, input, line)
			return
		}
		if mode == "inspect_missing" {
			fmt.Println(`{"status":"inspected","initialized":false,"members":[],"startup_readiness":{"reason":"not_initialized","needs_rehydration":false,"agents":[],"orchestrators":[],"init_evidence":[]}}`)
			return
		}
		fmt.Println(`{"status":"inspected","initialized":true,"members":[{"directory_name":"leader-dir","manifest_kind":"valid","role":"orchestrator","agent_name":"Display identity","address":"address","agent_id":"id"},{"directory_name":"legacy-null","manifest_kind":"valid","role":"human","agent_name":"","address":"","agent_id":""},{"directory_name":"malformed","manifest_kind":"malformed","role":"unknown","agent_name":"","address":"","agent_id":""}],"startup_readiness":{"reason":"ready","needs_rehydration":false,"agents":["leader-dir"],"orchestrators":["leader-dir"],"init_evidence":[{"directory_name":"leader-dir","presence":"present","failure":null}]}}`)
		return
	}
	if os.Args[2] != "create-interactive" {
		os.Exit(2)
	}
	var request InteractiveProjectRequest
	d := json.NewDecoder(strings.NewReader(string(line)))
	d.UseNumber()
	if d.Decode(&request) != nil {
		os.Exit(2)
	}
	fail := func() {
		fmt.Fprintln(os.Stderr, `{"status":"error","committed":false,"code":"project_publication_failed","error":"bounded safe message"}`)
		os.Exit(1)
	}
	switch mode {
	case "refused":
		fail()
	case "old_kernel":
		fmt.Fprintln(os.Stderr, "unknown command")
		os.Exit(2)
	case "malformed_error":
		fmt.Fprintln(os.Stderr, "not-json")
		os.Exit(1)
	case "wrong_error_commit":
		fmt.Fprintln(os.Stderr, `{"status":"error","committed":true,"code":"fixture","error":"safe"}`)
		os.Exit(1)
	case "signal":
		self, _ := os.FindProcess(os.Getpid())
		_ = self.Kill()
		os.Exit(2)
	case "empty":
		return
	case "timeout":
		time.Sleep(time.Second)
		return
	case "wrong_early":
		fmt.Println(`{"status":"created","committed":true}`)
		return
	}
	// Prove nothing persisted before this actual child readiness point.
	path := os.Getenv("LINGTAI_PROJECT_PREREQUISITE")
	if path != "" {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			os.Exit(2)
		}
	}
	readyRef := request.PresetRef
	if mode == "wrong_ready_ref" {
		readyRef += "-wrong"
	}
	_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"status": "ready_for_prerequisites", "committed": false, "attempt_id": "fixture", "preset_ref": readyRef})
	if mode == "broken_ack" {
		return
	}
	ackLine, err := input.ReadBytes('\n')
	if err != nil {
		fail()
	}
	var ack struct {
		Status, AttemptID, PresetRef string
		Accepted                     bool
	}
	// Inspect exact ack fields, not Go's case-insensitive aliases.
	var wire map[string]interface{}
	if json.Unmarshal(ackLine, &wire) != nil || len(wire) != 4 {
		os.Exit(2)
	}
	ack.Status, _ = wire["status"].(string)
	ack.AttemptID, _ = wire["attempt_id"].(string)
	ack.PresetRef, _ = wire["preset_ref"].(string)
	ack.Accepted, _ = wire["accepted"].(bool)
	if ack.Status != "prerequisites_acknowledged" || ack.AttemptID != "fixture" || ack.PresetRef != request.PresetRef {
		os.Exit(2)
	}
	if !ack.Accepted || mode == "publication_failed" {
		fail()
	}
	if path != "" {
		if _, err := os.Stat(path); err != nil {
			os.Exit(2)
		}
	}
	result := map[string]interface{}{"status": "created", "committed": true, "attempt_id": "fixture", "agent_name": request.AgentName, "directory_name": request.DirectoryName, "preset_ref": request.PresetRef, "warnings": []string{}}
	switch mode {
	case "wrong_identity":
		result["agent_name"] = "wrong"
	case "wrong_directory":
		result["directory_name"] = "wrong"
	case "wrong_attempt":
		result["attempt_id"] = "other"
	case "wrong_terminal_ref":
		result["preset_ref"] = "other"
	case "false_commit":
		result["committed"] = false
	case "stderr_chatter":
		fmt.Fprintln(os.Stderr, "noise")
	case "no_terminal":
		return
	}
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if mode == "multiple" {
		_ = json.NewEncoder(os.Stdout).Encode(result)
	}
	if mode == "exit1_after_created" {
		fail()
	}
	if mode == "exit2" {
		os.Exit(2)
	}
}

func projectFixturePython(t *testing.T, mode string) string {
	t.Helper()
	dir := t.TempDir()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(executable)
	if err != nil {
		t.Fatal(err)
	}
	name := "lingtai-agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LINGTAI_PROJECT_DRIVER_FIXTURE", mode)
	return filepath.Join(dir, "python")
}
func projectFixtureRequest(t *testing.T) InteractiveProjectRequest {
	root := filepath.Join(t.TempDir(), "spaces 中文 ;$(no-shell)")
	return InteractiveProjectRequest{ProjectDir: root, ApprovedRoot: root, AgentName: "Display identity", DirectoryName: "leader-dir", PresetRef: "~/saved/reviewed.json", PresetManifest: map[string]interface{}{"llm": map[string]interface{}{"provider": "openai", "model": "fixture", "api_key_env": "OPENAI_API_KEY"}, "extension": json.Number("9007199254740993")}, Options: InteractiveProjectOptions{Language: "en", Covenant: "fixture", Karma: true}, ProcessEvidence: "clean"}
}
func TestInteractiveProjectExchangeOrdersPrerequisites(t *testing.T) {
	python := projectFixturePython(t, "success")
	req := projectFixtureRequest(t)
	dependency := filepath.Join(t.TempDir(), "dependency")
	log := filepath.Join(t.TempDir(), "stdin.json")
	t.Setenv("LINGTAI_PROJECT_PREREQUISITE", dependency)
	t.Setenv("LINGTAI_PROJECT_INPUT_LOG", log)
	calls := 0
	got, err := CreateInteractiveProject(context.Background(), python, req, func() bool { calls++; return os.WriteFile(dependency, []byte("persisted"), 0600) == nil })
	if err != nil || calls != 1 || got.AgentName != req.AgentName || got.DirectoryName != req.DirectoryName {
		t.Fatalf("accepted result=%+v err=%v calls=%d", got, err, calls)
	}
	raw, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "9007199254740993") || !strings.HasSuffix(string(raw), "\n") || !strings.Contains(string(raw), "no-shell") {
		t.Fatalf("stdin lost literal path/precision: %s", raw)
	}
}
func TestInteractiveProjectUnknownNeverPretendsRefusal(t *testing.T) {
	for _, mode := range []string{"old_kernel", "malformed_error", "wrong_error_commit", "signal", "broken_ack", "empty", "wrong_early", "wrong_ready_ref", "wrong_identity", "wrong_directory", "wrong_attempt", "wrong_terminal_ref", "false_commit", "stderr_chatter", "no_terminal", "multiple", "exit1_after_created", "exit2", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, mode)
			calls := 0
			deadline := 5 * time.Second
			if mode == "timeout" {
				deadline = 100 * time.Millisecond
			}
			ctx, cancel := context.WithTimeout(context.Background(), deadline)
			defer cancel()
			_, err := CreateInteractiveProject(ctx, python, projectFixtureRequest(t), func() bool { calls++; return true })
			var outcome *ProjectOperationError
			if !errors.As(err, &outcome) || outcome.Known {
				t.Fatalf("%s must be unknown: %v", mode, err)
			}
			if (mode == "wrong_ready_ref" || mode == "wrong_early" || mode == "old_kernel" || mode == "empty" || mode == "timeout") && calls != 0 {
				t.Fatalf("persisted before accepted readiness: %d", calls)
			}
		})
	}
}
func TestInteractiveProjectKnownFailureAndRejectedAck(t *testing.T) {
	for _, mode := range []string{"refused", "publication_failed", "success"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, mode)
			calls := 0
			_, err := CreateInteractiveProject(context.Background(), python, projectFixtureRequest(t), func() bool { calls++; return mode != "success" })
			var outcome *ProjectOperationError
			if !errors.As(err, &outcome) || !outcome.Known {
				t.Fatalf("expected known refusal: %v", err)
			}
			if mode == "refused" && calls != 0 {
				t.Fatal("prerequisites before validation")
			}
		})
	}
}
func TestInteractiveProjectRejectsCredentialProjectionBeforeStart(t *testing.T) {
	python := projectFixturePython(t, "success")
	req := projectFixtureRequest(t)
	req.PresetManifest["extension"] = map[string]interface{}{"botToken": "fixture-secret-never-log"}
	calls := 0
	_, err := CreateInteractiveProject(context.Background(), python, req, func() bool { calls++; return true })
	if err == nil || calls != 0 || strings.Contains(err.Error(), "fixture-secret") {
		t.Fatalf("credential projection escaped: %v calls=%d", err, calls)
	}
}
func TestProjectInspectionIsReadOnlyAndKeepsSeparateIdentities(t *testing.T) {
	for _, mode := range []string{"inspect_missing", "inspect_members"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, mode)
			root := t.TempDir()
			got, err := InspectProject(context.Background(), python, root)
			if err != nil {
				t.Fatal(err)
			}
			if mode == "inspect_missing" && (*got.Initialized || len(got.Members) != 0) {
				t.Fatalf("absent inspection: %+v", got)
			}
			if mode == "inspect_members" && (got.Members[0].DirectoryName == got.Members[0].AgentName || got.Members[1].Role != "human" || got.Members[2].Role != "unknown") {
				t.Fatalf("lost evidence: %+v", got)
			}
			entries, err := os.ReadDir(root)
			if err != nil || len(entries) != 0 {
				t.Fatal("inspection initialized a skeleton")
			}
		})
	}
}
func TestInteractiveProjectCancellationAndStartFailureAreUnknown(t *testing.T) {
	python := projectFixturePython(t, "success")
	req := projectFixtureRequest(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, input := range []struct {
		ctx    context.Context
		python string
	}{{ctx, python}, {context.Background(), filepath.Join(t.TempDir(), "not-installed", "python")}} {
		calls := 0
		_, err := CreateInteractiveProject(input.ctx, input.python, req, func() bool { calls++; return true })
		var outcome *ProjectOperationError
		if !errors.As(err, &outcome) || outcome.Known || calls != 0 {
			t.Fatalf("transport pretended refusal/persisted: %v calls=%d", err, calls)
		}
	}
}
func TestProjectWireRequiresExactObjectsAndNewlines(t *testing.T) {
	var ready struct {
		Status    string `json:"status"`
		Committed *bool  `json:"committed"`
		AttemptID string `json:"attempt_id"`
		PresetRef string `json:"preset_ref"`
	}
	for _, raw := range []string{`{"status":"ready_for_prerequisites","committed":false,"attempt_id":"a"}`, `{"status":"ready_for_prerequisites","committed":false,"attempt_id":"a","preset_ref":"r","extra":0}`, `{"status":"ready_for_prerequisites","committed":false,"attempt_id":"a","preset_ref":"r","preset_ref":"r"}`, `{"status":"ready_for_prerequisites","committed":null,"attempt_id":"a","preset_ref":"r"}`, `{} {}`} {
		if strictProjectObject([]byte(raw), &ready) == nil {
			t.Fatalf("accepted wrong envelope: %s", raw)
		}
	}
	if _, _, err := projectExchangeLine([]byte(`{"status":"created"}`), true); err == nil {
		t.Fatal("accepted unterminated stdout")
	}
}
func TestUnknownProjectTransportNeverDeletesCallerTree(t *testing.T) {
	python := projectFixturePython(t, "old_kernel")
	req := projectFixtureRequest(t)
	final := filepath.Join(req.ProjectDir, ".lingtai", "kept")
	if err := os.MkdirAll(filepath.Dir(final), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(final, []byte("caller publication"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := CreateInteractiveProject(context.Background(), python, req, func() bool { t.Fatal("old kernel reached prerequisites"); return false })
	var outcome *ProjectOperationError
	if !errors.As(err, &outcome) || outcome.Known {
		t.Fatal("old kernel pretended refusal")
	}
	bytes, err := os.ReadFile(final)
	if err != nil || string(bytes) != "caller publication" {
		t.Fatal("unknown transport removed publication")
	}
}

// Readiness fixtures exercise the same literal command with live, flushed
// callback/ack binding. They are transport doubles, not installed acceptance.
func projectReadinessFixture(mode string, input *bufio.Reader, line []byte) {
	var request map[string]interface{}
	if json.Unmarshal(line, &request) != nil || len(request) != 2 || request["init_observation_mode"] != "callback" {
		os.Exit(2)
	}
	writePID := func() {
		if path := os.Getenv("LINGTAI_READINESS_PID"); path != "" {
			_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600)
		}
	}
	if mode == "readiness_inherited_pipe" {
		child := exec.Command(os.Args[0], "project", "inspect")
		for _, v := range os.Environ() {
			if !strings.HasPrefix(v, "LINGTAI_PROJECT_DRIVER_FIXTURE=") {
				child.Env = append(child.Env, v)
			}
		}
		child.Env = append(child.Env, "LINGTAI_PROJECT_DRIVER_FIXTURE=readiness_pipe_holder")
		child.Stdout = os.Stdout
		if child.Start() != nil {
			os.Exit(2)
		}
		writePID() // inherited stdout exists before the parent cancels
		return     // unrelated descendant retains stdout briefly; NOT tree-kill proof
	}
	writePID()
	if mode == "readiness_cancel" {
		time.Sleep(5 * time.Second)
		return
	}
	if mode == "readiness_eof" {
		return
	}
	if mode == "readiness_bad_frame" {
		fmt.Println(`{"status":"observe_init_presence","sequence":true,"directory_name_b64":"YQ=="}`)
		return
	}
	if mode == "readiness_missing" {
		fmt.Println(`{"status":"inspected","initialized":false,"members":[],"startup_readiness":{"reason":"not_initialized","needs_rehydration":false,"agents":[],"orchestrators":[],"agents_b64":[],"orchestrators_b64":[],"init_evidence":[]}}`)
		return
	}
	name := "leader-dir"
	encoded := base64.StdEncoding.EncodeToString([]byte(name))
	_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{"status": "observe_init_presence", "sequence": 1, "directory_name_b64": encoded})
	ackLine, err := input.ReadBytes('\n')
	if err != nil {
		return
	}
	if path := os.Getenv("LINGTAI_READINESS_ACK"); path != "" {
		_ = os.WriteFile(path, ackLine, 0600)
	}
	var ack projectInitAck
	if json.Unmarshal(ackLine, &ack) != nil || ack.Status != "init_presence_observed" || ack.Sequence != 1 || ack.DirectoryNameB64 != encoded {
		os.Exit(2)
	}
	if mode == "readiness_reap" {
		time.Sleep(5 * time.Second)
		return
	}
	member := map[string]interface{}{"directory_name": name, "directory_name_b64": encoded, "manifest_kind": "valid", "role": "orchestrator", "agent_name": "Display identity", "address": "", "agent_id": ""}
	item := map[string]interface{}{"directory_name": name, "directory_name_b64": encoded, "sequence": 1, "presence": ack.Presence, "failure": ack.Failure}
	reason := "ready"
	if ack.Presence == "stat_failed" {
		reason = "cannot_stat_init"
	}
	ready := map[string]interface{}{"reason": reason, "needs_rehydration": ack.Presence == "absent", "agents": []string{name}, "orchestrators": []string{name}, "agents_b64": []string{encoded}, "orchestrators_b64": []string{encoded}, "init_evidence": []interface{}{item}}
	terminal := map[string]interface{}{"status": "inspected", "initialized": true, "members": []interface{}{member}, "startup_readiness": ready}
	switch mode {
	case "readiness_forged":
		item["failure"] = map[string]interface{}{"path_b64": encoded, "native_errno": nil, "native_winerror": nil}
	case "readiness_wrong_seq":
		item["sequence"] = 2
	case "readiness_null":
		member["agent_name"] = nil
	case "readiness_no_evidence":
		ready["init_evidence"] = []interface{}{}
	case "readiness_extra":
		item["private"] = "never-log"
	case "readiness_stderr":
		fmt.Fprintln(os.Stderr, "never-log")
	case "readiness_no_terminal":
		return
	}
	data, _ := json.Marshal(terminal)
	if mode == "readiness_unterminated" {
		fmt.Print(string(data))
		return
	}
	fmt.Println(string(data))
	if mode == "readiness_extra_terminal" {
		fmt.Println(string(data))
	}
	if mode == "readiness_exit2" {
		os.Exit(2)
	}
}

func TestProjectReadinessOnceNativeStatAndOriginalError(t *testing.T) {
	for _, observation := range []string{"present", "absent", "failure"} {
		t.Run(observation, func(t *testing.T) {
			python := projectFixturePython(t, "readiness_success")
			root := t.TempDir()
			origin := filepath.Join(root, ".lingtai")
			path := filepath.Join(origin, "leader-dir", "init.json")
			original := &os.PathError{Op: "original-native-operation", Path: path, Err: syscall.EACCES}
			calls := 0
			got, err := inspectProjectReadiness(context.Background(), python, root, origin, func(actual string) (os.FileInfo, error) {
				calls++
				if actual != path {
					t.Fatalf("wrong stat path: %q", actual)
				}
				switch observation {
				case "absent":
					return nil, os.ErrNotExist
				case "failure":
					return nil, original
				}
				return nil, nil // any successful stat, even nonregular, is present
			})
			if calls != 1 {
				t.Fatalf("stat called %d times", calls)
			}
			if observation == "failure" {
				if !errors.Is(err, original) || err.Error() != fmt.Sprintf("sanity check: cannot stat %s: %s", path, original) {
					t.Fatalf("lost actual native chain/prose: %v", err)
				}
			} else if err != nil || got.StartupReadiness.Reason != "ready" || got.StartupReadiness.NeedsRehydration != (observation == "absent") || got.Members[0].AgentName != "Display identity" {
				t.Fatalf("lost readiness/identity: %+v %v", got, err)
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("inspection created state")
			}
		})
	}
}
func TestProjectReadinessClosedTranscriptAndZeroEffects(t *testing.T) {
	for _, mode := range []string{"forged", "wrong_seq", "null", "no_evidence", "extra", "stderr", "no_terminal", "unterminated", "extra_terminal", "exit2", "bad_frame", "eof", "missing"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, "readiness_"+mode)
			root := t.TempDir()
			calls := 0
			original := &os.PathError{Op: "native", Path: "retained", Err: syscall.EACCES}
			got, err := inspectProjectReadiness(context.Background(), python, root, root, func(string) (os.FileInfo, error) { calls++; return nil, original })
			if mode == "missing" {
				if err != nil || calls != 0 || got.StartupReadiness.Reason != "not_initialized" {
					t.Fatalf("missing: %+v %v calls=%d", got, err, calls)
				}
			} else {
				var unknown *ProjectOperationError
				if !errors.As(err, &unknown) || unknown.Known || errors.Is(err, original) || strings.Contains(err.Error(), "never-log") {
					t.Fatalf("forged/lost evidence escaped as native success/error: %v", err)
				}
				want := 1
				if mode == "bad_frame" || mode == "eof" {
					want = 0
				}
				if calls != want {
					t.Fatalf("wrong stat count %d", calls)
				}
			}
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("transport failure mutated project")
			}
		})
	}
}
func TestProjectReadinessNativeByteIdentityAndModeClosure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX surrogateescape fixture; native Windows acceptance remains separate")
	}
	encoded := base64.StdEncoding.EncodeToString([]byte{0xff})
	raw := `{"status":"inspected","initialized":true,"members":[{"directory_name":"\udcff","directory_name_b64":"` + encoded + `","manifest_kind":"valid","role":"orchestrator","agent_name":"","address":"","agent_id":""}],"startup_readiness":{"reason":"ready","needs_rehydration":false,"agents":["\udcff"],"orchestrators":["\udcff"],"agents_b64":["` + encoded + `"],"orchestrators_b64":["` + encoded + `"],"init_evidence":[{"directory_name":"\udcff","sequence":1,"directory_name_b64":"` + encoded + `","presence":"present","failure":null}]}}`
	transcript := []retainedProjectInit{{name: string([]byte{0xff}), ack: projectInitAck{Sequence: 1, DirectoryNameB64: encoded, Presence: "present"}}}
	got, err := decodeProjectInspection([]byte(raw), true, transcript)
	if err != nil || got.Members[0].DirectoryName != string([]byte{0xff}) {
		t.Fatalf("lossy identity: %+v %v", got, err)
	}
	for _, invalid := range []string{strings.Replace(raw, `\udcff`, `\udcfe`, 1), strings.Replace(raw, `"sequence":1`, `"sequence":true`, 1), strings.Replace(raw, encoded, "/w", 1), strings.Replace(raw, `"failure":null`, `"failure":{"path_b64":"","native_errno":null,"native_winerror":null}`, 1), strings.Replace(raw, `"role":"orchestrator"`, `"role":"orchestrator","role":"orchestrator"`, 1)} {
		if _, err := decodeProjectInspection([]byte(invalid), true, transcript); err == nil {
			t.Fatal("accepted forged native/closed shape")
		}
	}
	if _, err := decodeProjectInspection([]byte(raw), false, nil); err == nil {
		t.Fatal("ordinary mode accepted callback-only fields")
	}
}
func TestProjectReadinessCancellationAndUnwindReap(t *testing.T) {
	for _, mode := range []string{"cancel", "inherited_pipe"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, "readiness_"+mode)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			pidPath := filepath.Join(t.TempDir(), "pid")
			t.Setenv("LINGTAI_READINESS_PID", pidPath)
			done := make(chan error, 1)
			go func() {
				_, err := inspectProjectReadiness(ctx, python, "root", "origin", func(string) (os.FileInfo, error) { t.Error("stat after cancelled/empty stream"); return nil, nil })
				done <- err
			}()
			deadline := time.After(5 * time.Second)
			for {
				if _, err := os.Stat(pidPath); err == nil {
					break
				}
				select {
				case <-deadline:
					t.Fatal("child never started")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			cancel()
			select {
			case err := <-done:
				if err == nil {
					t.Fatal("cancel accepted")
				}
			case <-time.After(3 * time.Second):
				t.Fatal("cancellation did not release/reap direct child and pipes")
			}
		})
	}
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, "readiness_reap")
			pidPath, ackPath := filepath.Join(t.TempDir(), "pid"), filepath.Join(t.TempDir(), "ack")
			t.Setenv("LINGTAI_READINESS_PID", pidPath)
			t.Setenv("LINGTAI_READINESS_ACK", ackPath)
			marker := &struct{}{}
			stat := func(string) (os.FileInfo, error) {
				if mode == "goexit" {
					runtime.Goexit()
				}
				panic(marker)
			}
			if mode == "panic" {
				func() {
					defer func() {
						if recover() != marker {
							t.Error("original panic lost")
						}
					}()
					_, _ = inspectProjectReadiness(context.Background(), python, "root", "origin", stat)
					t.Error("panic returned")
				}()
			} else {
				done := make(chan struct{})
				go func() {
					defer close(done)
					_, _ = inspectProjectReadiness(context.Background(), python, "root", "origin", stat)
					t.Error("Goexit returned")
				}()
				<-done
			}
			if _, err := os.Stat(ackPath); !os.IsNotExist(err) {
				t.Fatal("substitute ack after unwind")
			}
			data, err := os.ReadFile(pidPath)
			if err != nil {
				t.Fatal(err)
			}
			pid, err := strconv.Atoi(string(data))
			if err != nil {
				t.Fatal(err)
			}
			if runtime.GOOS != "windows" {
				child, err := os.FindProcess(pid)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := child.Wait(); err == nil {
					t.Fatal("direct child not reaped")
				}
			}
		})
	}
}
