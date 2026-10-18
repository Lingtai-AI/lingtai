package process

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Extend the existing portable real-child fixture, not a new execution port.
// No filesystem merger is simulated: these lock the wire framing only.
func projectConfigurationFixture(mode string) {
	data, err := io.ReadAll(io.LimitReader(os.Stdin, projectLineLimit+1))
	if err != nil || len(data) > projectLineLimit {
		os.Exit(2)
	}
	var request map[string]json.RawMessage
	if json.Unmarshal(data, &request) != nil {
		os.Exit(2)
	}
	if path := os.Getenv("LINGTAI_PROJECT_INPUT_LOG"); path != "" {
		_ = os.WriteFile(path, data, 0600)
	}
	operation := os.Args[2]
	status := map[string]string{"initialize": "initialized", "configure": "configured", "rehydrate": "rehydrated", "propagate-policy": "policy_propagated", "propagate-config": "config_propagated"}[operation]
	required := map[string][]string{
		"initialize": {"project_dir"}, "configure": {"project_dir", "directory_name", "agent_name", "preset_ref", "keep_current", "options"},
		"rehydrate":        {"project_dir", "directory_name", "agent_name", "preset_ref", "keep_current", "options"},
		"propagate-policy": {"project_dir", "directory_name", "default_ref", "allowed_presets"}, "propagate-config": {"project_dir", "directory_name"},
	}[operation]
	for _, key := range required {
		if _, ok := request[key]; !ok {
			os.Exit(2)
		}
	}
	if len(request) != len(required) {
		os.Exit(2)
	}
	result := ProjectConfigurationResult{Status: status, Outcome: "complete", Updated: 1, Source: ProjectSourceReceipt{ReaderStatus: "not_read"}}
	if operation == "configure" || operation == "rehydrate" {
		result.Source = ProjectSourceReceipt{InitWritten: true, ManifestWritten: true, ReaderStatus: "READ_OK_WITH_IGNORED_FIELDS"}
	}
	switch mode {
	case "configuration_partial":
		result = ProjectConfigurationResult{Status: "error", Outcome: "partial", Updated: 1, Failed: 1, Code: "manifest_write_failed", Message: "Project configuration could not complete", Source: ProjectSourceReceipt{InitWritten: true, ReaderStatus: "not_read"}}
	case "configuration_rejected":
		result = ProjectConfigurationResult{Status: "error", Outcome: "rejected", Code: "mixed_initialization_state", Message: "Project configuration could not complete", Source: ProjectSourceReceipt{ReaderStatus: "not_read"}}
	case "configuration_unknown":
		result = ProjectConfigurationResult{Status: "error", Outcome: "unknown", Code: "configuration_request_failed", Message: "Project configuration could not complete", Source: ProjectSourceReceipt{ReaderStatus: "not_read"}}
	case "configuration_timeout":
		time.Sleep(time.Second)
		return
	case "configuration_unsupported":
		fmt.Fprint(os.Stderr, "unknown operation raw-private-value")
		os.Exit(2)
	case "configuration_signal":
		self, _ := os.FindProcess(os.Getpid())
		_ = self.Kill()
		os.Exit(2)
	case "configuration_stderr":
		fmt.Fprint(os.Stderr, "warning raw-private-value")
	case "configuration_wrong_status":
		result.Status = "created"
	}
	data, _ = json.Marshal(result)
	switch mode {
	case "configuration_duplicate":
		data = []byte(`{"status":"configured",` + string(data)[1:])
	case "configuration_missing_source":
		data = []byte(strings.Replace(string(data), `"init_written":true,`, "", 1))
	case "configuration_null_source":
		data = []byte(strings.Replace(string(data), `"init_written":true`, `"init_written":null`, 1))
	case "configuration_extra":
		data = append(data, []byte(" {}")...)
	}
	if result.Status == "error" {
		fmt.Fprintln(os.Stderr, string(data))
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func TestProjectConfigurationLiteralOperations(t *testing.T) {
	for _, operation := range []string{"initialize", "configure", "propagate-policy", "propagate-config", "rehydrate"} {
		t.Run(operation, func(t *testing.T) {
			python := projectFixturePython(t, "configuration_success")
			root := filepath.Join(t.TempDir(), "space 中文 ;$(literal)")
			log := filepath.Join(t.TempDir(), "input.json")
			t.Setenv("LINGTAI_PROJECT_INPUT_LOG", log)
			req := ProjectConfigureRequest{ProjectDir: root, DirectoryName: "orch-dir", AgentName: "Display", PresetRef: "~/saved/real.json", Options: ProjectConfigurationOptions{EnvFile: "public.env", VenvPath: "public/venv", Karma: true}}
			var result ProjectConfigurationResult
			var err error
			switch operation {
			case "initialize":
				result, err = InitializeProject(context.Background(), python, root)
			case "configure":
				result, err = ConfigureProject(context.Background(), python, req)
			case "rehydrate":
				result, err = RehydrateProject(context.Background(), python, req)
			case "propagate-policy":
				result, err = PropagateProjectPolicy(context.Background(), python, root, "orch-dir", req.PresetRef, []string{req.PresetRef})
			case "propagate-config":
				result, err = PropagateProjectConfig(context.Background(), python, root, "orch-dir")
			}
			if err != nil || result.Updated != 1 {
				t.Fatalf("receipt=%+v err=%v", result, err)
			}
			if (operation == "configure" || operation == "rehydrate") && result.Source.ReaderStatus != "READ_OK_WITH_IGNORED_FIELDS" {
				t.Fatalf("fabricated reader status: %+v", result)
			}
			data, err := os.ReadFile(log)
			if err != nil {
				t.Fatal(err)
			}
			var sent struct {
				ProjectDir string `json:"project_dir"`
			}
			if json.Unmarshal(data, &sent) != nil || sent.ProjectDir != root {
				t.Fatalf("literal root changed: %s", data)
			}
		})
	}
}

func TestProjectConfigurationFailuresRetainOnlyValidatedReceipts(t *testing.T) {
	for _, mode := range []string{"partial", "rejected", "unknown", "unsupported", "signal", "timeout", "stderr", "wrong_status", "duplicate", "missing_source", "null_source", "extra"} {
		t.Run(mode, func(t *testing.T) {
			python := projectFixturePython(t, "configuration_"+mode)
			ctx := context.Background()
			if mode == "timeout" {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, 20*time.Millisecond)
				defer cancel()
			}
			result, err := ConfigureProject(ctx, python, ProjectConfigureRequest{ProjectDir: t.TempDir(), DirectoryName: "orch-dir", AgentName: "Display", PresetRef: "~/saved/real.json"})
			var failure *ProjectConfigurationError
			if !errors.As(err, &failure) {
				t.Fatalf("expected typed failure, got %+v %v", result, err)
			}
			known := mode == "partial" || mode == "rejected"
			if failure.Known != known || strings.Contains(err.Error(), "raw-private-value") {
				t.Fatalf("invalid failure: %+v %v", failure, err)
			}
			if mode == "partial" && (result.Updated != 1 || !result.Source.InitWritten || result.Source.ManifestWritten || failure.Result != result) {
				t.Fatalf("partial receipt lost: %+v", result)
			}
			if !known && result.Outcome != "unknown" {
				t.Fatalf("unknown became refusal: %+v", result)
			}
		})
	}
}

func TestProjectConfigurationBoundAndCancelledStartAreUnknown(t *testing.T) {
	python := projectFixturePython(t, "configuration_success")
	log := filepath.Join(t.TempDir(), "input.json")
	t.Setenv("LINGTAI_PROJECT_INPUT_LOG", log)
	pad := strings.Repeat("x", projectLineLimit)
	request := ProjectConfigureRequest{ProjectDir: t.TempDir(), DirectoryName: "orch-dir", Options: ProjectConfigurationOptions{Pad: &pad}}
	_, err := ConfigureProject(context.Background(), python, request)
	var failure *ProjectConfigurationError
	if !errors.As(err, &failure) || failure.Known {
		t.Fatal("oversized request not unknown")
	}
	if _, err := os.Stat(log); !os.IsNotExist(err) {
		t.Fatal("oversized request reached child")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = InitializeProject(ctx, python, t.TempDir())
	if !errors.As(err, &failure) || failure.Known {
		t.Fatal("cancelled start not unknown")
	}
}
