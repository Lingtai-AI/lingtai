package headless

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

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
