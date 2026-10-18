package process

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// Test-binary child fixture only, not a runtime hook. This avoids changing the
// shared TestMain owned by other Project slices. No agent is launched here.
func init() {
	mode := os.Getenv("LINGTAI_EXTERNAL_LIFECYCLE_FIXTURE")
	if mode == "" {
		return
	}
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadBytes('\n')
	if err != nil {
		os.Exit(2)
	}
	var request struct {
		RequestID string `json:"request_id"`
		Action    string `json:"action"`
	}
	if json.Unmarshal(line, &request) != nil {
		os.Exit(2)
	}
	if mode == "clean_first_remove" || mode == "nirvana_barrier_remove" || mode == "effect_after_refusal" {
		frames := []lifecycleFrame{{Operation: "remove_tree", Target: -1}}
		switch mode {
		case "nirvana_barrier_remove":
			frames = []lifecycleFrame{{Operation: "reset_barrier", Target: -1}, {Operation: "remove_tree", Target: -1}}
		case "effect_after_refusal":
			frames = []lifecycleFrame{{Operation: "external_owner", Target: 0}, {Operation: "write_signal", Target: 0, Argument: "suspend"}}
		}
		transcript := []lifecycleAck{}
		for i, frame := range frames {
			frame.Status, frame.RequestID, frame.Sequence = "project_lifecycle_native", request.RequestID, i+1
			_ = json.NewEncoder(os.Stdout).Encode(frame)
			line, err := reader.ReadBytes('\n')
			if err != nil {
				os.Exit(0)
			}
			var ack lifecycleAck
			if json.Unmarshal(line, &ack) != nil {
				os.Exit(2)
			}
			transcript = append(transcript, ack)
		}
		_ = json.NewEncoder(os.Stdout).Encode(map[string]interface{}{
			"status": "project_lifecycle_complete", "request_id": request.RequestID,
			"action": request.Action, "transcript": transcript,
			"result": ProjectLifecycleResult{Code: "ok", Failures: []ProjectLifecycleFailure{}, Survivors: []int{}},
		})
		os.Exit(0)
	}
	frame := lifecycleFrame{Status: "project_lifecycle_native", RequestID: request.RequestID, Sequence: 1, Operation: "write_signal", Target: 0, Argument: "sleep"}
	if mode == "wrong_target" {
		frame.Target = 1
	}
	_ = json.NewEncoder(os.Stdout).Encode(frame)
	line, err = reader.ReadBytes('\n')
	if err != nil {
		os.Exit(0)
	}
	var ack lifecycleAck
	if json.Unmarshal(line, &ack) != nil {
		os.Exit(2)
	}
	if mode == "lost_terminal" {
		os.Exit(0)
	}
	if mode == "duplicate" {
		frame.Sequence = 2
		_ = json.NewEncoder(os.Stdout).Encode(frame)
		_, _ = reader.ReadBytes('\n')
		os.Exit(0)
	}
	transcript := []lifecycleAck{ack}
	if mode == "forged" {
		transcript[0].Argument = "suspend"
	}
	terminal := map[string]interface{}{
		"status": "project_lifecycle_complete", "request_id": request.RequestID,
		"action": request.Action, "transcript": transcript,
		"result": ProjectLifecycleResult{Code: "ok", Count: 1, Failures: []ProjectLifecycleFailure{}, Survivors: []int{}},
	}
	_ = json.NewEncoder(os.Stdout).Encode(terminal)
	if mode == "extra" {
		_ = json.NewEncoder(os.Stdout).Encode(terminal)
	}
	if mode == "stderr" {
		_, _ = io.WriteString(os.Stderr, "unexpected\n")
	}
	if mode == "exit" {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestLifecycleOwnedChildRejectsUnknownWithoutReplayingSignal(t *testing.T) {
	for _, mode := range []string{"success", "wrong_target", "lost_terminal", "duplicate", "forged", "extra", "stderr", "exit"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, "unused_lifecycle")
			t.Setenv("LINGTAI_EXTERNAL_LIFECYCLE_FIXTURE", mode)
			dir := t.TempDir()
			result, err := RunProjectLifecycle(python, ProjectLifecycleOptions{Action: "sleep", Selected: dir}, ProjectLifecycleNative{})
			if mode == "success" {
				if err != nil || result.Count != 1 {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else {
				var unknown *ProjectLifecycleUnknown
				if !errors.As(err, &unknown) {
					t.Fatalf("got %v, want unknown", err)
				}
			}
			_, statErr := os.Stat(filepath.Join(dir, ".sleep"))
			if mode == "wrong_target" {
				if !os.IsNotExist(statErr) {
					t.Fatalf("unbound target touched: %v", statErr)
				}
			} else if statErr != nil {
				t.Fatalf("original partial signal missing: %v", statErr)
			}
		})
	}
}

func TestLifecycleNativeErrorIdentityNeedsExactTranscript(t *testing.T) {
	s := newLifecycleSession(ProjectLifecycleOptions{Action: "revive", Selected: "native target"}, ProjectLifecycleNative{Puffo: func(string) bool { return true }})
	frame := lifecycleFrame{Status: "project_lifecycle_native", RequestID: s.requestID, Sequence: 1, Operation: "external_owner", Target: 0}
	ack, err := s.observe(frame)
	if err != nil {
		t.Fatal(err)
	}
	terminal := map[string]interface{}{
		"status": "project_lifecycle_complete", "request_id": s.requestID, "action": "revive",
		"result":     ProjectLifecycleResult{Code: "native", ErrorSequence: 1, Failures: []ProjectLifecycleFailure{}, Survivors: []int{}},
		"transcript": []lifecycleAck{ack},
	}
	data, _ := json.Marshal(terminal)
	_, err = s.finish(data) // driver calls this only after EOF/exit/stderr checks
	if err != ErrPuffoManagedAgent {
		t.Fatalf("native error identity lost: %v", err)
	}
	ack.Argument = "forged"
	terminal["transcript"] = []lifecycleAck{ack}
	data, _ = json.Marshal(terminal)
	_, err = s.finish(data)
	var unknown *ProjectLifecycleUnknown
	if !errors.As(err, &unknown) {
		t.Fatalf("forged transcript restored native error: %v", err)
	}
}

func TestLifecycleDuplicateEffectRejectedBeforeNativeWrite(t *testing.T) {
	dir := t.TempDir()
	s := newLifecycleSession(ProjectLifecycleOptions{Action: "sleep", Selected: dir}, ProjectLifecycleNative{})
	frame := lifecycleFrame{Status: "project_lifecycle_native", RequestID: s.requestID, Sequence: 1, Operation: "write_signal", Target: 0, Argument: "sleep"}
	if _, err := s.observe(frame); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, ".sleep")
	if err := os.WriteFile(path, []byte("not overwritten"), 0600); err != nil {
		t.Fatal(err)
	}
	frame.Sequence = 2
	if _, err := s.observe(frame); err == nil {
		t.Fatal("duplicate effect accepted")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "not overwritten" || !reflect.DeepEqual(s.transcript[0].Members, []lifecycleMember{}) {
		t.Fatal("effect replayed or receipt malformed")
	}
}

func TestLifecycleOwnedChildRejectsCleanFirstRemove(t *testing.T) {
	python := projectFixturePython(t, "unused_lifecycle")
	t.Setenv("LINGTAI_EXTERNAL_LIFECYCLE_FIXTURE", "clean_first_remove")
	root := t.TempDir()
	marker := filepath.Join(root, "retained")
	if err := os.WriteFile(marker, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := RunProjectLifecycle(python, ProjectLifecycleOptions{Action: "clean", Root: root}, ProjectLifecycleNative{})
	var unknown *ProjectLifecycleUnknown
	if !errors.As(err, &unknown) || result.Code == "ok" || result.Completed {
		t.Fatalf("illicit clean accepted: result=%+v err=%v", result, err)
	}
	if data, err := os.ReadFile(marker); err != nil || string(data) != "original" {
		t.Fatalf("first remove_tree dispatched: data=%q err=%v", data, err)
	}
}

func TestLifecycleOwnedChildRejectsNirvanaBarrierWithoutDiscovery(t *testing.T) {
	python := projectFixturePython(t, "unused_lifecycle")
	t.Setenv("LINGTAI_EXTERNAL_LIFECYCLE_FIXTURE", "nirvana_barrier_remove")
	root := t.TempDir()
	human := filepath.Join(root, "human")
	if err := os.Mkdir(human, 0700); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(human, ".agent.json")
	if err := os.WriteFile(manifest, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := RunProjectLifecycle(python, ProjectLifecycleOptions{Action: "nirvana", Root: root}, ProjectLifecycleNative{})
	var unknown *ProjectLifecycleUnknown
	if !errors.As(err, &unknown) || result.Code == "ok" || result.Completed {
		t.Fatalf("illicit nirvana accepted: result=%+v err=%v", result, err)
	}
	if data, err := os.ReadFile(manifest); err != nil || string(data) != "original" {
		t.Fatalf("reset barrier dispatched before discovery: data=%q err=%v", data, err)
	}
}

func TestLifecycleOwnedChildRejectsEffectAfterNativeRefusal(t *testing.T) {
	python := projectFixturePython(t, "unused_lifecycle")
	t.Setenv("LINGTAI_EXTERNAL_LIFECYCLE_FIXTURE", "effect_after_refusal")
	dir := t.TempDir()
	ownerCalls := 0
	result, err := RunProjectLifecycle(python, ProjectLifecycleOptions{Action: "refresh", Selected: dir}, ProjectLifecycleNative{
		Puffo: func(string) bool {
			ownerCalls++
			return true
		},
	})
	var unknown *ProjectLifecycleUnknown
	if !errors.As(err, &unknown) || result.Code == "ok" || ownerCalls != 1 {
		t.Fatalf("post-refusal exchange: result=%+v err=%v calls=%d", result, err, ownerCalls)
	}
	if _, err := os.Stat(filepath.Join(dir, ".suspend")); !os.IsNotExist(err) {
		t.Fatalf("signal dispatched after native refusal: %v", err)
	}
}
