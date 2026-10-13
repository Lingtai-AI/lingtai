package process

import (
	"bufio"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

// Small extension of the existing test-binary child fixture: wire/lifecycle
// faults only. Real mutation/snapshot assertions are installed-CLI tests in preset.
func projectRecipeFixture(mode string) {
	reader := bufio.NewReader(os.Stdin)
	if os.Args[2] == "recipe-observe" {
		data, err := io.ReadAll(reader)
		var request struct {
			ProjectDir string `json:"project_dir"`
		}
		if err != nil || json.Unmarshal(data, &request) != nil {
			os.Exit(2)
		}
		fmt.Println(`{"status":"recipe_observed","state":"read_failure","needs_apply":true,"code":"","error":""}`)
		return
	}
	line, err := reader.ReadBytes('\n')
	if err != nil {
		os.Exit(2)
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(line, &request) != nil || len(request) != 4 {
		os.Exit(2)
	}
	if path := os.Getenv("LINGTAI_PROJECT_INPUT_LOG"); path != "" {
		_ = os.WriteFile(path, line, 0600)
	}
	if path := os.Getenv("LINGTAI_RECIPE_PID_LOG"); path != "" {
		_ = os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0600)
	}
	frame := `{"status":"render_recipe_member","sequence":1,"directory_name_b64":"YQ==","template_b64":"Rw==","template_bytes":1}`
	switch mode {
	case "recipe_duplicate":
		frame = `{"status":"render_recipe_member","status":"render_recipe_member","sequence":1,"directory_name_b64":"YQ==","template_b64":"Rw==","template_bytes":1}`
	case "recipe_length":
		frame = `{"status":"render_recipe_member","sequence":1,"directory_name_b64":"YQ==","template_b64":"Rw==","template_bytes":2}`
	case "recipe_base64":
		frame = `{"status":"render_recipe_member","sequence":1,"directory_name_b64":"YQ==","template_b64":"Rx==","template_bytes":1}` // noncanonical pad bits
	case "recipe_sequence":
		frame = `{"status":"render_recipe_member","sequence":2,"directory_name_b64":"YQ==","template_b64":"Rw==","template_bytes":1}`
	case "recipe_unknown_frame":
		frame = `{"status":"ready_for_prerequisites"}`
	}
	fmt.Println(frame)
	line, err = reader.ReadBytes('\n')
	if err != nil {
		return
	}
	if path := os.Getenv("LINGTAI_RECIPE_ACK_LOG"); path != "" {
		_ = os.WriteFile(path, line, 0600)
	}
	if mode == "recipe_lost_terminal" {
		return
	}
	var ack struct {
		Status        string `json:"status"`
		Sequence      int    `json:"sequence"`
		RenderedB64   string `json:"rendered_b64"`
		RenderedBytes int    `json:"rendered_bytes"`
	}
	if _, err := recipeObject(line, &ack, "status sequence rendered_b64 rendered_bytes", ""); err != nil || ack.Status != "recipe_member_rendered" || ack.Sequence != 1 {
		os.Exit(2)
	}
	rendered, err := recipeBytes(ack.RenderedB64)
	if err != nil || len(rendered) != ack.RenderedBytes {
		os.Exit(2)
	}
	r := recipeWireSuccess()
	_ = json.NewEncoder(os.Stdout).Encode(r)
	if mode == "recipe_extra_terminal" {
		fmt.Println(`{}`)
	}
	if mode == "recipe_wrong_exit" {
		os.Exit(1)
	}
	if mode == "recipe_stderr" {
		fmt.Fprintln(os.Stderr, "unexpected diagnostic")
	}
}

func recipeWireSuccess() ProjectRecipeResult {
	return ProjectRecipeResult{Status: "recipe_applied", Outcome: "complete", GreetStatus: "selected", Applied: 1, LegacyApplied: 1,
		Members:  []ProjectRecipeMember{{DirectoryName: "a", DirectoryNameB64: "YQ==", PromptWritten: true, ReaderStatus: "not_read", LegacyCounted: true}},
		Snapshot: ProjectRecipeSnapshot{Attempted: true, RemovalSucceeded: true, Copied: true}}
}

// 6. Closed frames validated BEFORE original callback; lost/extra terminal,
// wrong exit and unexpected stderr are UNKNOWN even after a bound ack.
func TestRecipeExchangeWireValidationAndUnknown(t *testing.T) {
	for _, mode := range []string{"duplicate", "length", "base64", "sequence", "unknown_frame", "lost_terminal", "extra_terminal", "wrong_exit", "stderr"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, "recipe_"+mode)
			calls := 0
			result, err := ApplyProjectRecipe(python, "literal bundle", "independent staging", "zh", func(raw string) string { calls++; return raw })
			var failure *ProjectRecipeError
			if !errors.As(err, &failure) || failure.Known || result.Outcome != "unknown" {
				t.Fatalf("malformed/transport outcome accepted: %+v %v", result, err)
			}
			want := 0
			if mode == "lost_terminal" || mode == "extra_terminal" || mode == "wrong_exit" || mode == "stderr" {
				want = 1
			}
			if calls != want {
				t.Fatalf("calls=%d want=%d (replay or validation-after-function)", calls, want)
			}
		})
	}
	t.Run("literal_ack", func(t *testing.T) {
		python := projectFixturePython(t, "recipe_success")
		input, ackPath := filepath.Join(t.TempDir(), "request"), filepath.Join(t.TempDir(), "ack")
		t.Setenv("LINGTAI_PROJECT_INPUT_LOG", input)
		t.Setenv("LINGTAI_RECIPE_ACK_LOG", ackPath)
		result, err := ApplyProjectRecipe(python, "bundle ;$(literal)", "separate staging", "zh", func(string) string { return "" })
		if err != nil || result.LegacyApplied != 1 {
			t.Fatalf("%+v %v", result, err)
		}
		data, err := os.ReadFile(input)
		if err != nil {
			t.Fatal(err)
		}
		var request map[string]string
		if json.Unmarshal(data, &request) != nil || len(request) != 4 || request["project_dir"] != "bundle ;$(literal)" || request["lingtai_dir"] != "separate staging" || request["rendering_mode"] != "callback" {
			t.Fatal("literal request drift")
		}
		data, err = os.ReadFile(ackPath)
		if err != nil {
			t.Fatal(err)
		}
		var ack map[string]interface{}
		if json.Unmarshal(data, &ack) != nil || len(ack) != 4 || ack["rendered_b64"] != "" || ack["rendered_bytes"] != float64(0) || ack["sequence"] != float64(1) {
			t.Fatal("empty bound ack drift")
		}
	})
	t.Run("observation_diagnostic", func(t *testing.T) {
		python := projectFixturePython(t, "recipe_observe")
		result, err := ObserveProjectRecipe(python, "literal")
		if err != nil || result.State != "read_failure" || !result.NeedsApply {
			t.Fatalf("legacy observation boolean lost: %+v %v", result, err)
		}
	})
}

// 7. Authoritative byte identity / closed terminal / stopped supplementary
// receipt / factual native projection plus actual owned-child reap on unwind.
func TestRecipeExchangeTerminalEvidenceAndChildReap(t *testing.T) {
	t.Run("terminal", func(t *testing.T) {
		r := recipeWireSuccess()
		r.Members[0].DirectoryName = "\ufffd"
		r.Members[0].DirectoryNameB64 = base64.StdEncoding.EncodeToString([]byte{0xfe})
		r.Members = append(r.Members, r.Members[0])
		r.Members[1].DirectoryNameB64 = base64.StdEncoding.EncodeToString([]byte{0xff})
		r.Applied, r.LegacyApplied = 2, 2
		data, _ := json.Marshal(r)
		got, err := decodeRecipeResult(data)
		if err != nil || len(got.Members) != 2 {
			t.Fatal("lossy display names collapsed native identity")
		}
		var wire map[string]interface{}
		_ = json.Unmarshal(data, &wire)
		for _, field := range []string{"status", "outcome", "members", "snapshot", "greet_status", "applied", "legacy_applied", "failed", "code", "error", "legacy_failure"} {
			copy := map[string]interface{}{}
			for k, v := range wire {
				copy[k] = v
			}
			delete(copy, field)
			bad, _ := json.Marshal(copy)
			if _, err := decodeRecipeResult(bad); err == nil {
				t.Fatalf("missing field %s accepted", field)
			}
		}
		wire["unowned"] = true
		bad, _ := json.Marshal(wire)
		if _, err := decodeRecipeResult(bad); err == nil {
			t.Fatal("unknown terminal key accepted")
		}
	})
	t.Run("stopped", func(t *testing.T) {
		r := recipeWireSuccess()
		r.Status, r.Outcome, r.Code, r.Message = "recipe_exchange_stopped", "partial", "recipe_exchange_failed", "Project Recipe rendering exchange stopped"
		r.Snapshot = ProjectRecipeSnapshot{}
		data, _ := json.Marshal(r)
		var wire map[string]interface{}
		_ = json.Unmarshal(data, &wire)
		wire["legacy_code"], wire["pending_sequence"], wire["pending_directory_name_b64"] = "", 2, "Yg=="
		data, _ = json.Marshal(wire)
		got, err := decodeRecipeResult(data)
		if err != nil || got.PendingSequence != 2 || got.Snapshot.Attempted || got.LegacyApplied != 1 {
			t.Fatalf("stopped evidence lost: %+v %v", got, err)
		}
	})
	t.Run("native_cause", func(t *testing.T) {
		errno := 2
		f := ProjectRecipeFailure{Phase: "enumeration", Operation: "readdir", PathB64: base64.StdEncoding.EncodeToString([]byte("native/path")), Errno: &errno}
		err := (&ProjectRecipeError{Known: true, Result: ProjectRecipeResult{LegacyFailure: &f}})
		var native *os.PathError
		if !errors.As(err, &native) || native.Path != "native/path" || !errors.Is(err, os.ErrNotExist) {
			t.Fatal("factual path/errno chain lost")
		}
		f.Errno = nil
		f.Phase, f.Operation = "metadata", "decode"
		var evidence *ProjectRecipeNativeFailure
		if !errors.As(err, &evidence) {
			t.Fatal("non-native evidence hidden or fabricated decoder cause")
		}
	})
	for _, mode := range []string{"panic", "goexit"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, "recipe_reap")
			pidPath, ackPath := filepath.Join(t.TempDir(), "pid"), filepath.Join(t.TempDir(), "ack")
			t.Setenv("LINGTAI_RECIPE_PID_LOG", pidPath)
			t.Setenv("LINGTAI_RECIPE_ACK_LOG", ackPath)
			marker := &struct{}{}
			render := func(string) string {
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
					_, _ = ApplyProjectRecipe(python, "bundle", "destination", "", render)
					t.Error("panic returned")
				}()
			} else {
				done := make(chan struct{})
				go func() {
					defer close(done)
					_, _ = ApplyProjectRecipe(python, "bundle", "destination", "", render)
					t.Error("Goexit returned")
				}()
				<-done
			}
			if _, err := os.Stat(ackPath); !os.IsNotExist(err) {
				t.Fatal("substitute ack sent after unwind")
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
					t.Fatal("owned child was not already reaped by original call")
				}
			}
		})
	}
}
