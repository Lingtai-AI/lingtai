package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

func stubSpawnCreate(t *testing.T, fn func(context.Context, string, process.ProjectCreateRequest) (process.ProjectCreateResult, error)) {
	t.Helper()
	original := createProject
	createProject = fn
	t.Cleanup(func() { createProject = original })
}

func stubSpawnRegister(t *testing.T, fn func(string, string) error) {
	t.Helper()
	original := registerProject
	registerProject = fn
	t.Cleanup(func() { registerProject = original })
}

func stubSpawnSaveRecipeState(t *testing.T, fn func(string, preset.RecipeState) error) {
	t.Helper()
	original := saveRecipeState
	saveRecipeState = fn
	t.Cleanup(func() { saveRecipeState = original })
}

func stubSuccessfulSpawnCreate(t *testing.T) {
	t.Helper()
	stubSpawnCreate(t, func(_ context.Context, _ string, request process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
		if !filepath.IsAbs(request.CovenantFile) {
			t.Errorf("covenant file = %q, want absolute path", request.CovenantFile)
		}
		if data, err := os.ReadFile(request.CovenantFile); err != nil || len(data) == 0 {
			t.Errorf("covenant input is not readable and nonempty: data=%q err=%v", data, err)
		}
		lingtaiDir := filepath.Join(request.Dir, ".lingtai")
		for _, dir := range []string{
			filepath.Join(lingtaiDir, "human"),
			filepath.Join(lingtaiDir, request.AgentName),
		} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return process.ProjectCreateResult{}, err
			}
		}
		for path, body := range map[string][]byte{
			filepath.Join(lingtaiDir, "human", ".agent.json"):           []byte(`{"agent_name":"human"}`),
			filepath.Join(lingtaiDir, request.AgentName, ".agent.json"): []byte(`{"agent_name":"` + request.AgentName + `"}`),
			filepath.Join(lingtaiDir, request.AgentName, "init.json"):   []byte(`{"manifest":{"agent_name":"` + request.AgentName + `"}}`),
		} {
			if err := os.WriteFile(path, body, 0o644); err != nil {
				return process.ProjectCreateResult{}, err
			}
		}
		return process.ProjectCreateResult{
			Status: "created", AgentName: request.AgentName, PresetRef: request.Preset, ExitCode: 0,
		}, nil
	})
}

func TestRunSpawn_RejectsExistingLingtaiDir(t *testing.T) {
	withTempHome(t)
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, ".lingtai"), 0o755)

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:       dir,
		Preset:    "minimax",
		AgentName: "test",
		Language:  "en",
	})
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	var errResp map[string]string
	if err := json.Unmarshal(stderr.Bytes(), &errResp); err != nil {
		t.Fatalf("stderr not valid JSON: %v\nbody: %s", err, stderr.String())
	}
	if errResp["code"] != "already_initialized" {
		t.Errorf("error code = %q, want %q", errResp["code"], "already_initialized")
	}
}

func TestRunSpawn_RejectsUnknownPreset(t *testing.T) {
	withTempHome(t)
	preset.RefreshTemplates()
	dir := t.TempDir()

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:       dir,
		Preset:    "nonexistent-preset-xyz",
		AgentName: "test",
		Language:  "en",
		// SkipLaunch bypasses the runtime-readiness check: this test asserts
		// preset validation, not runtime setup, and headless spawn no longer
		// silently installs a venv into the temp HOME to make that check
		// pass — see config.RuntimeReady.
		SkipLaunch: true,
	})
	if code != 1 {
		t.Fatalf("expected exit code 1, got %d", code)
	}
	var errResp map[string]string
	json.Unmarshal(stderr.Bytes(), &errResp)
	if errResp["code"] != "preset_not_found" {
		t.Errorf("error code = %q, want %q", errResp["code"], "preset_not_found")
	}
}

func TestResolveAgentName_DefaultsToBasename(t *testing.T) {
	opts := SpawnOpts{Dir: "/path/to/my-project"}
	got := opts.resolveAgentName()
	if got != "my-project" {
		t.Errorf("resolveAgentName = %q, want %q", got, "my-project")
	}
}

func TestResolveAgentName_UsesExplicit(t *testing.T) {
	opts := SpawnOpts{Dir: "/path/to/my-project", AgentName: "custom-name"}
	got := opts.resolveAgentName()
	if got != "custom-name" {
		t.Errorf("resolveAgentName = %q, want %q", got, "custom-name")
	}
}

func TestRunSpawn_CreatesInitJSON(t *testing.T) {
	withTempHome(t)
	globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
	preset.RefreshTemplates()
	preset.Bootstrap(globalDir)
	stubSuccessfulSpawnCreate(t)

	dir := filepath.Join(t.TempDir(), "test-project")
	os.MkdirAll(dir, 0o755)

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:        dir,
		Preset:     "minimax",
		AgentName:  "test-agent",
		Language:   "en",
		SkipLaunch: true,
	})
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d\nstderr: %s", code, stderr.String())
	}

	// Verify init.json was created
	initPath := filepath.Join(dir, ".lingtai", "test-agent", "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("init.json not created: %v", err)
	}
	var initJSON map[string]interface{}
	if err := json.Unmarshal(data, &initJSON); err != nil {
		t.Fatalf("init.json is invalid JSON: %v", err)
	}
	manifest := initJSON["manifest"].(map[string]interface{})
	if manifest["agent_name"] != "test-agent" {
		t.Errorf("agent_name = %q, want %q", manifest["agent_name"], "test-agent")
	}
	if manifest["language"] != "en" {
		t.Errorf("language = %q, want %q", manifest["language"], "en")
	}

	// Verify .agent.json was created
	agentJSON := filepath.Join(dir, ".lingtai", "test-agent", ".agent.json")
	if _, err := os.Stat(agentJSON); err != nil {
		t.Errorf(".agent.json not created: %v", err)
	}

	// Verify human directory was created
	humanManifest := filepath.Join(dir, ".lingtai", "human", ".agent.json")
	if _, err := os.Stat(humanManifest); err != nil {
		t.Errorf("human .agent.json not created: %v", err)
	}

	// Kernel create owns the seed tree; headless compatibility must add the
	// shared library directory without replacing any kernel-created entries.
	libraryShared := filepath.Join(dir, ".lingtai", ".library_shared")
	if info, err := os.Stat(libraryShared); err != nil || !info.IsDir() {
		t.Errorf(".library_shared was not restored as a directory: info=%v err=%v", info, err)
	}

	// Fresh headless projects must still refresh user-level utility skills.
	utilitySkill := filepath.Join(globalDir, "utilities", "lingtai-tui-help", "SKILL.md")
	if _, err := os.Stat(utilitySkill); err != nil {
		t.Errorf("utility skill not extracted: %v", err)
	}
}

func TestRunSpawn_SuccessOutput_HasRequiredFields(t *testing.T) {
	withTempHome(t)
	globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
	preset.RefreshTemplates()
	preset.Bootstrap(globalDir)
	stubSuccessfulSpawnCreate(t)

	dir := filepath.Join(t.TempDir(), "test-project")
	os.MkdirAll(dir, 0o755)

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:        dir,
		Preset:     "minimax",
		AgentName:  "test-agent",
		Language:   "en",
		SkipLaunch: true,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, stderr: %s", code, stderr.String())
	}

	var result SpawnOutput
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		t.Fatalf("stdout not valid JSON: %v\nbody: %s", err, stdout.String())
	}
	if result.Status != "created" {
		t.Errorf("status = %q, want %q", result.Status, "created")
	}
	if result.ReadinessStatus != "not_launched" {
		t.Errorf("readiness_status = %q, want %q", result.ReadinessStatus, "not_launched")
	}
	if result.AgentName != "test-agent" {
		t.Errorf("agent_name = %q, want %q", result.AgentName, "test-agent")
	}
	if result.Preset != "minimax" {
		t.Errorf("preset = %q, want %q", result.Preset, "minimax")
	}
	if result.Recipe != "plain" {
		t.Errorf("recipe = %q, want %q", result.Recipe, "plain")
	}
	if result.ReadyTimeoutSeconds != defaultReadyTimeout.Seconds() {
		t.Errorf("ready_timeout_seconds = %v, want %v", result.ReadyTimeoutSeconds, defaultReadyTimeout.Seconds())
	}
}

func TestWaitForAgentReadySuccess(t *testing.T) {
	restore := stubReadyDeps(t)
	defer restore()

	readHeartbeat = func(string, float64) fs.HeartbeatStatus {
		return fs.HeartbeatStatus{Exists: true, Fresh: true, Timestamp: 1, AgeSeconds: 0.1}
	}
	findAgentProcesses = func(string) []process.AgentProcess {
		return []process.AgentProcess{{PID: 777, Command: "python -m lingtai run /tmp/a"}}
	}

	got := waitForAgentReady(t.TempDir(), 50*time.Millisecond)
	if got.Code != "ready" {
		t.Fatalf("Code = %q, want ready", got.Code)
	}
	if got.PID != 777 || !got.HeartbeatConfirmed || !got.InspectableProcessConfirmed {
		t.Fatalf("unexpected readiness result: %+v", got)
	}
}

func TestWaitForAgentReadyTimeout(t *testing.T) {
	restore := stubReadyDeps(t)
	defer restore()

	readHeartbeat = func(string, float64) fs.HeartbeatStatus {
		return fs.HeartbeatStatus{Exists: false, Fresh: false}
	}
	findAgentProcesses = func(string) []process.AgentProcess {
		return []process.AgentProcess{{PID: 777, Command: "python -m lingtai run /tmp/a"}}
	}

	got := waitForAgentReady(t.TempDir(), 5*time.Millisecond)
	if got.Code != "readiness_timeout" {
		t.Fatalf("Code = %q, want readiness_timeout", got.Code)
	}
	if !got.InspectableProcessConfirmed || got.HeartbeatConfirmed {
		t.Fatalf("unexpected readiness result: %+v", got)
	}
}

func TestWaitForAgentReadyProcessExited(t *testing.T) {
	restore := stubReadyDeps(t)
	defer restore()
	processExitGrace = time.Millisecond

	readHeartbeat = func(string, float64) fs.HeartbeatStatus {
		return fs.HeartbeatStatus{Exists: false, Fresh: false}
	}
	findAgentProcesses = func(string) []process.AgentProcess { return nil }

	got := waitForAgentReady(t.TempDir(), 50*time.Millisecond)
	if got.Code != "process_exited_before_ready" {
		t.Fatalf("Code = %q, want process_exited_before_ready", got.Code)
	}
}

func stubReadyDeps(t *testing.T) func() {
	t.Helper()
	origFind := findAgentProcesses
	origRead := readHeartbeat
	origSleep := readySleep
	origPoll := readyPollInterval
	origGrace := processExitGrace
	readySleep = func(time.Duration) {}
	readyPollInterval = time.Millisecond
	processExitGrace = 500 * time.Millisecond
	return func() {
		findAgentProcesses = origFind
		readHeartbeat = origRead
		readySleep = origSleep
		readyPollInterval = origPoll
		processExitGrace = origGrace
	}
}

func TestRunSpawn_LanguageDefault(t *testing.T) {
	withTempHome(t)
	globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
	preset.RefreshTemplates()
	preset.Bootstrap(globalDir)
	stubSuccessfulSpawnCreate(t)

	dir := filepath.Join(t.TempDir(), "lang-test")
	os.MkdirAll(dir, 0o755)

	var stdout, stderr bytes.Buffer
	RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:        dir,
		Preset:     "minimax",
		AgentName:  "agent",
		Language:   "zh",
		SkipLaunch: true,
	})

	initPath := filepath.Join(dir, ".lingtai", "agent", "init.json")
	data, _ := os.ReadFile(initPath)
	var initJSON map[string]interface{}
	json.Unmarshal(data, &initJSON)
	manifest := initJSON["manifest"].(map[string]interface{})
	if manifest["language"] != "zh" {
		t.Errorf("language = %q, want %q", manifest["language"], "zh")
	}
}

func TestRunSpawn_PostCreateFailureReportsCreatedRecoveryAndDoesNotLaunch(t *testing.T) {
	cases := []struct {
		name                string
		recoveryStep        string
		expectedCause       string
		malformedInit       bool
		failRecipeState     bool
		failRegistration    bool
		assertSharedLibrary bool
	}{
		{
			name:                "language_compatibility",
			recoveryStep:        "language_compatibility",
			malformedInit:       true,
			assertSharedLibrary: true,
		},
		{
			name:                "recipe_state_persistence",
			recoveryStep:        "recipe_state_persistence",
			expectedCause:       "recipe state storage unavailable",
			failRecipeState:     true,
			assertSharedLibrary: true,
		},
		{
			name:             "registration",
			recoveryStep:     "registration",
			expectedCause:    "registry unavailable",
			failRegistration: true,
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			withTempHome(t)
			globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
			preset.RefreshTemplates()
			if err := preset.Bootstrap(globalDir); err != nil {
				t.Fatal(err)
			}

			originalRuntimeReady := runtimeReady
			runtimeReady = func(string) error { return nil }
			t.Cleanup(func() { runtimeReady = originalRuntimeReady })
			originalLaunch := launchAgent
			launchAgent = func(string, string) (*exec.Cmd, error) {
				t.Fatal("launchAgent was called after a post-create failure")
				return nil, nil
			}
			t.Cleanup(func() { launchAgent = originalLaunch })

			if test.malformedInit {
				stubSpawnCreate(t, func(_ context.Context, _ string, request process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
					agentDir := filepath.Join(request.Dir, ".lingtai", request.AgentName)
					if err := os.MkdirAll(agentDir, 0o755); err != nil {
						return process.ProjectCreateResult{}, err
					}
					if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte("{"), 0o644); err != nil {
						return process.ProjectCreateResult{}, err
					}
					return process.ProjectCreateResult{Status: "created", AgentName: request.AgentName, PresetRef: request.Preset, ExitCode: 0}, nil
				})
			} else {
				stubSuccessfulSpawnCreate(t)
			}
			if test.failRecipeState {
				stubSpawnSaveRecipeState(t, func(string, preset.RecipeState) error {
					return errors.New(test.expectedCause)
				})
			}
			if test.failRegistration {
				stubSpawnRegister(t, func(string, string) error { return errors.New(test.expectedCause) })
			} else {
				stubSpawnRegister(t, func(string, string) error {
					t.Fatal("registerProject was called after a pre-registration post-create failure")
					return nil
				})
			}

			dir := filepath.Join(t.TempDir(), "project")
			var stdout, stderr bytes.Buffer
			code := RunSpawn(&stdout, &stderr, SpawnOpts{Dir: dir, Preset: "minimax", AgentName: "alice", Language: "en"})
			if code != 1 || stdout.Len() != 0 {
				t.Fatalf("exit/stdout = %d/%q, want 1 and no stdout document", code, stdout.String())
			}
			var response map[string]interface{}
			if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
				t.Fatalf("stderr is not one structured JSON error: %v\n%s", err, stderr.String())
			}
			agentDir := filepath.Join(dir, ".lingtai", "alice")
			if response["code"] != "init_failed" || response["recovery_state"] != "created_unregistered" || response["recovery_step"] != test.recoveryStep || response["project_dir"] != dir || response["agent_name"] != "alice" || response["agent_dir"] != agentDir {
				t.Fatalf("%s recovery response = %#v", test.name, response)
			}
			cause, ok := response["cause"].(string)
			if !ok || cause == "" {
				t.Fatalf("%s recovery response has no cause: %#v", test.name, response)
			}
			if test.expectedCause != "" && cause != test.expectedCause {
				t.Fatalf("%s recovery cause = %q, want %q", test.name, cause, test.expectedCause)
			}
			if _, err := os.Stat(filepath.Join(agentDir, "init.json")); err != nil {
				t.Fatalf("known-created seed was removed after %s failure: %v", test.name, err)
			}
			if test.assertSharedLibrary {
				if info, err := os.Stat(filepath.Join(dir, ".lingtai", ".library_shared")); err != nil || !info.IsDir() {
					t.Fatalf("shared library was not preserved before %s recovery: info=%v err=%v", test.name, info, err)
				}
			}
		})
	}
}

func TestRunSpawn_UntrustedCreateOutcomeWritesOneErrorAndDoesNotRegisterOrLaunch(t *testing.T) {
	cases := []struct {
		name   string
		reason string
		create func(process.ProjectCreateRequest) (process.ProjectCreateResult, error)
	}{
		{
			name:   "incomplete_exit_zero_result",
			reason: "malformed_result",
			create: func(request process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
				return process.ProjectCreateResult{Status: "created", AgentName: request.AgentName, ExitCode: 0}, nil
			},
		},
		{
			name:   "argument_error_transport",
			reason: "argument_error",
			create: func(process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
				return process.ProjectCreateResult{}, &process.ProjectCreateError{
					Kind: process.ProjectCreateTransportFailure, Reason: "argument_error", ExitCode: 2,
					Stderr: "usage: lingtai-agent project create",
				}
			},
		},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			withTempHome(t)
			globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
			preset.RefreshTemplates()
			if err := preset.Bootstrap(globalDir); err != nil {
				t.Fatal(err)
			}

			originalRuntimeReady := runtimeReady
			runtimeReady = func(string) error { return nil }
			t.Cleanup(func() { runtimeReady = originalRuntimeReady })
			originalLaunch := launchAgent
			launchAgent = func(string, string) (*exec.Cmd, error) {
				t.Fatal("launchAgent was called after an untrusted create outcome")
				return nil, nil
			}
			t.Cleanup(func() { launchAgent = originalLaunch })
			stubSpawnRegister(t, func(string, string) error {
				t.Fatal("registerProject was called after an untrusted create outcome")
				return nil
			})
			stubSpawnCreate(t, func(_ context.Context, _ string, request process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
				return test.create(request)
			})

			var stdout, stderr bytes.Buffer
			code := RunSpawn(&stdout, &stderr, SpawnOpts{Dir: filepath.Join(t.TempDir(), "project"), Preset: "minimax", AgentName: "alice", Language: "en"})
			if code != 1 || stdout.Len() != 0 {
				t.Fatalf("exit/stdout = %d/%q, want 1 and no stdout document", code, stdout.String())
			}
			var response map[string]interface{}
			if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
				t.Fatalf("stderr is not one structured JSON error: %v\n%s", err, stderr.String())
			}
			if response["code"] != "init_failed" || response["reason"] != test.reason {
				t.Fatalf("%s response = %#v", test.name, response)
			}
		})
	}
}

// TestRunSpawn_RejectsUnsafeAgentName proves issue #849 at the headless
// boundary: path-form agent names (relative-parent, absolute, slash, and
// backslash forms) are rejected with a clear error before any project write,
// and cannot create an artifact outside the owning directory.
func TestRunSpawn_RejectsUnsafeAgentName(t *testing.T) {
	unsafe := []string{
		"..",
		".",
		"../escape",
		"..\\escape",
		"..\\..\\escape",
		"a/b",
		"a\\b",
		"/abs",
		"a/../b",
	}
	for _, name := range unsafe {
		withTempHome(t)
		globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
		preset.RefreshTemplates()
		preset.Bootstrap(globalDir)

		parent := t.TempDir()
		dir := filepath.Join(parent, "project")

		var stdout, stderr bytes.Buffer
		code := RunSpawn(&stdout, &stderr, SpawnOpts{
			Dir:        dir,
			Preset:     "minimax",
			AgentName:  name,
			Language:   "en",
			SkipLaunch: true,
		})
		if code != 1 {
			t.Fatalf("agent name %q: exit code = %d, want 1\nstderr: %s", name, code, stderr.String())
		}
		var errResp map[string]string
		if err := json.Unmarshal(stderr.Bytes(), &errResp); err != nil {
			t.Fatalf("agent name %q: stderr not valid JSON: %v\nbody: %s", name, err, stderr.String())
		}
		if errResp["code"] != "invalid_args" {
			t.Errorf("agent name %q: error code = %q, want invalid_args", name, errResp["code"])
		}
		if !strings.Contains(errResp["error"], "invalid agent name") {
			t.Errorf("agent name %q: error = %q, want a clear validation error", name, errResp["error"])
		}
		// No project write may have happened, and nothing may exist at the
		// escaped target path (parent/escape for "../escape").
		if _, err := os.Stat(filepath.Join(dir, ".lingtai")); err == nil {
			t.Errorf("agent name %q: .lingtai/ was created despite rejection", name)
		}
		if _, err := os.Stat(filepath.Join(parent, "escape")); err == nil {
			t.Errorf("agent name %q: escaped artifact created at %s", name, filepath.Join(parent, "escape"))
		}
	}
}

// TestRunSpawn_AcceptsNormalAgentName is the positive control: a normal
// single-segment agent name still creates the agent dir and init.json.
func TestRunSpawn_AcceptsNormalAgentName(t *testing.T) {
	withTempHome(t)
	globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
	preset.RefreshTemplates()
	preset.Bootstrap(globalDir)
	stubSuccessfulSpawnCreate(t)

	dir := filepath.Join(t.TempDir(), "test-project")
	os.MkdirAll(dir, 0o755)

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:        dir,
		Preset:     "minimax",
		AgentName:  "ok-agent",
		Language:   "en",
		SkipLaunch: true,
	})
	if code != 0 {
		t.Fatalf("exit code = %d, want 0\nstderr: %s", code, stderr.String())
	}
	if _, err := os.Stat(filepath.Join(dir, ".lingtai", "ok-agent", "init.json")); err != nil {
		t.Errorf("init.json not created for normal agent name: %v", err)
	}
}

func TestRunSpawn_HandlerCreateFailureWritesOneStructuredErrorAndDoesNotLaunch(t *testing.T) {
	withTempHome(t)
	globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
	preset.RefreshTemplates()
	if err := preset.Bootstrap(globalDir); err != nil {
		t.Fatal(err)
	}

	originalRuntimeReady := runtimeReady
	runtimeReady = func(string) error { return nil }
	t.Cleanup(func() { runtimeReady = originalRuntimeReady })
	originalLaunch := launchAgent
	launchAgent = func(string, string) (*exec.Cmd, error) {
		t.Fatal("launchAgent was called after a failed project create")
		return nil, nil
	}
	t.Cleanup(func() { launchAgent = originalLaunch })

	var covenantFile string
	stubSpawnCreate(t, func(_ context.Context, _ string, request process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
		covenantFile = request.CovenantFile
		return process.ProjectCreateResult{}, &process.ProjectCreateError{
			Kind: process.ProjectCreateHandlerFailure, Reason: "handler_error",
			Code: "invalid_preset", Message: "kernel rejected preset", ExitCode: 1,
			Stderr: `{"code":"invalid_preset","error":"kernel rejected preset","status":"error"}`,
		}
	})

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir: filepath.Join(t.TempDir(), "project"), Preset: "minimax", AgentName: "alice", Language: "en",
	})
	if code != 1 || stdout.Len() != 0 {
		t.Fatalf("exit/stdout = %d/%q, want 1 and no stdout document", code, stdout.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
		t.Fatalf("stderr is not one structured JSON error: %v\n%s", err, stderr.String())
	}
	if response["code"] != "init_failed" || response["kernel_code"] != "invalid_preset" || response["error"] != "kernel rejected preset" {
		t.Fatalf("handler error mapping = %#v", response)
	}
	if _, err := os.Stat(covenantFile); !os.IsNotExist(err) {
		t.Fatalf("TUI-owned covenant file survived terminal child result: %v", err)
	}
	if _, err := os.Stat(filepath.Join(globalDir, "registry.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("failed create was registered: %v", err)
	}
}
