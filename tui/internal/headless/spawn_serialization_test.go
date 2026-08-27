package headless

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

const spawnSerializationHelperEnv = "LINGTAI_SPAWN_SERIALIZATION_HELPER"

func TestRunSpawn_SerializationLockFailureWritesOneInitError(t *testing.T) {
	parent := t.TempDir()
	blocker := filepath.Join(parent, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("block lock parent\n"), 0o644); err != nil {
		t.Fatalf("create lock-parent blocker: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:        filepath.Join(blocker, "project"),
		Preset:     "minimax",
		AgentName:  "alice",
		Language:   "en",
		SkipLaunch: true,
	})
	if code != 1 || stdout.Len() != 0 {
		t.Fatalf("exit/stdout = %d/%q, want 1 and no stdout JSON", code, stdout.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
		t.Fatalf("stderr is not one JSON error: %v\n%s", err, stderr.String())
	}
	if response["code"] != "init_failed" || !strings.Contains(fmt.Sprint(response["error"]), "cannot acquire project serialization lock") {
		t.Fatalf("lock acquisition response = %#v", response)
	}
}

func TestRunSpawn_SerializationReleaseFailureDoesNotLaunch(t *testing.T) {
	prepareSpawnSerializationHome(t)
	originalRuntimeReady := runtimeReady
	runtimeReady = func(string) error { return nil }
	t.Cleanup(func() { runtimeReady = originalRuntimeReady })
	stubSuccessfulSpawnCreate(t)
	stubSpawnRegister(t, func(string, string) error { return nil })

	originalRelease := releaseSpawnSerializationLock
	releaseSpawnSerializationLock = func(lock *fs.ExclusiveFileLock) error {
		if err := lock.Release(); err != nil {
			return err
		}
		return fmt.Errorf("simulated serialization lock release failure")
	}
	t.Cleanup(func() { releaseSpawnSerializationLock = originalRelease })
	originalLaunch := launchAgent
	launchAgent = func(string, string) (*exec.Cmd, error) {
		t.Fatal("launchAgent was called after serialization lock release failure")
		return nil, nil
	}
	t.Cleanup(func() { launchAgent = originalLaunch })

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:       filepath.Join(t.TempDir(), "project"),
		Preset:    "minimax",
		AgentName: "alice",
		Language:  "en",
	})
	if code != 1 || stdout.Len() != 0 {
		t.Fatalf("exit/stdout = %d/%q, want 1 and no stdout JSON", code, stdout.String())
	}
	var response map[string]interface{}
	if err := json.Unmarshal(stderr.Bytes(), &response); err != nil {
		t.Fatalf("stderr is not one JSON error: %v\n%s", err, stderr.String())
	}
	if response["code"] != "init_failed" || !strings.Contains(fmt.Sprint(response["error"]), "created and registered") {
		t.Fatalf("release failure response = %#v", response)
	}
	if _, hasRecoveryState := response["recovery_state"]; hasRecoveryState {
		t.Fatalf("release failure must not claim an unregistered recovery state: %#v", response)
	}
}

func TestRunSpawn_SerializesSameRootAcrossProcesses(t *testing.T) {
	target := filepath.Join(t.TempDir(), "project")
	runSpawnSerializationContention(t, target, target, target+".lingtai-spawn.lock")
}

func TestRunSpawn_SerializesRealRootAndSymlinkAliasAcrossProcesses(t *testing.T) {
	fixture := t.TempDir()
	realTarget := filepath.Join(fixture, "real-project")
	if err := os.MkdirAll(realTarget, 0o755); err != nil {
		t.Fatalf("create real target: %v", err)
	}
	aliasTarget := filepath.Join(fixture, "project-alias")
	if err := os.Symlink(realTarget, aliasTarget); err != nil {
		t.Skipf("symlink fixture unavailable: %v", err)
	}

	runSpawnSerializationContention(t, realTarget, aliasTarget, realTarget+".lingtai-spawn.lock")
	if _, err := os.Stat(aliasTarget + ".lingtai-spawn.lock"); !os.IsNotExist(err) {
		t.Fatalf("symlink alias unexpectedly created a second sibling lock: %v", err)
	}
}

func runSpawnSerializationContention(t *testing.T, ownerTarget, contenderTarget, lockPath string) {
	t.Helper()
	prepareSpawnSerializationHome(t)
	controls := t.TempDir()

	owner, err := startSpawnSerializationHelper(ownerTarget, "owner", spawnSerializationHelperControls{
		Entered:      filepath.Join(controls, "A.entered-create"),
		Release:      filepath.Join(controls, "A.release"),
		ResultPrefix: filepath.Join(controls, "A.result"),
	})
	if err != nil {
		t.Fatalf("start owner helper: %v", err)
	}
	t.Cleanup(owner.stop)
	if err := waitForSpawnSerializationPaths(10*time.Second, filepath.Join(controls, "A.entered-create")); err != nil {
		t.Fatalf("owner did not enter create while holding the lock: %v\n%s", err, owner.output.String())
	}

	contender, err := startSpawnSerializationHelper(contenderTarget, "contender", spawnSerializationHelperControls{
		Started:      filepath.Join(controls, "B.started"),
		Acquiring:    filepath.Join(controls, "B.acquiring-lock"),
		Entered:      filepath.Join(controls, "B.entered-create"),
		Registered:   filepath.Join(controls, "B.registered"),
		Launched:     filepath.Join(controls, "B.launched"),
		ResultPrefix: filepath.Join(controls, "B.result"),
	})
	if err != nil {
		owner.stop()
		t.Fatalf("start contender helper: %v", err)
	}
	t.Cleanup(contender.stop)
	if err := waitForSpawnSerializationPaths(10*time.Second, filepath.Join(controls, "B.started"), filepath.Join(controls, "B.acquiring-lock")); err != nil {
		owner.stop()
		contender.stop()
		t.Fatalf("contender did not reach lock acquisition while owner held it: %v\n%s", err, contender.output.String())
	}
	assertSpawnSerializationBlocked(t, contender, filepath.Join(controls, "B.entered-create"))

	if err := os.WriteFile(filepath.Join(controls, "A.release"), []byte("release owner\n"), 0o644); err != nil {
		owner.stop()
		contender.stop()
		t.Fatalf("release owner helper: %v", err)
	}
	if err := owner.wait(10 * time.Second); err != nil {
		contender.stop()
		t.Fatalf("owner helper did not complete: %v\n%s", err, owner.output.String())
	}
	ownerResult := readSpawnSerializationResult(t, filepath.Join(controls, "A.result"))
	assertSpawnSerializationSuccess(t, ownerResult)

	if err := contender.wait(10 * time.Second); err != nil {
		t.Fatalf("contender helper did not complete: %v\n%s", err, contender.output.String())
	}
	contenderResult := readSpawnSerializationResult(t, filepath.Join(controls, "B.result"))
	assertSpawnSerializationAlreadyInitialized(t, contenderResult)
	assertSpawnSerializationNoContenderSideEffects(t,
		filepath.Join(controls, "B.entered-create"),
		filepath.Join(controls, "B.registered"),
		filepath.Join(controls, "B.launched"),
	)
	if _, err := os.Stat(lockPath); err != nil {
		t.Fatalf("stable sibling lock file was not retained: %v", err)
	}
}

func TestRunSpawn_SerializationLockReleasesAfterOwnerCrash(t *testing.T) {
	prepareSpawnSerializationHome(t)
	target := filepath.Join(t.TempDir(), "project")
	controls := t.TempDir()

	owner, err := startSpawnSerializationHelper(target, "crash-owner", spawnSerializationHelperControls{Entered: filepath.Join(controls, "A.entered-create"), Release: filepath.Join(controls, "A.release"), ResultPrefix: filepath.Join(controls, "A.result")})
	if err != nil {
		t.Fatalf("start crash owner helper: %v", err)
	}
	t.Cleanup(owner.stop)
	if err := waitForSpawnSerializationPaths(10*time.Second, filepath.Join(controls, "A.entered-create")); err != nil {
		t.Fatalf("crash owner did not enter create while holding the lock: %v\n%s", err, owner.output.String())
	}
	if _, err := os.Stat(filepath.Join(target, ".lingtai")); !os.IsNotExist(err) {
		owner.stop()
		t.Fatalf("crash owner created .lingtai before the kill boundary: %v", err)
	}

	contender, err := startSpawnSerializationHelper(target, "crash-contender", spawnSerializationHelperControls{Started: filepath.Join(controls, "B.started"), Acquiring: filepath.Join(controls, "B.acquiring-lock"), Entered: filepath.Join(controls, "B.entered-create"), ResultPrefix: filepath.Join(controls, "B.result")})
	if err != nil {
		owner.stop()
		t.Fatalf("start crash-release contender helper: %v", err)
	}
	t.Cleanup(contender.stop)
	if err := waitForSpawnSerializationPaths(10*time.Second, filepath.Join(controls, "B.started"), filepath.Join(controls, "B.acquiring-lock")); err != nil {
		owner.stop()
		contender.stop()
		t.Fatalf("crash-release contender did not start: %v\n%s", err, contender.output.String())
	}
	assertSpawnSerializationBlocked(t, contender, filepath.Join(controls, "B.entered-create"))

	if err := owner.cmd.Process.Kill(); err != nil {
		owner.stop()
		contender.stop()
		t.Fatalf("kill crash owner helper: %v", err)
	}
	if err := owner.wait(10 * time.Second); err == nil {
		contender.stop()
		t.Fatal("crash owner unexpectedly exited cleanly after Kill")
	}

	if err := contender.wait(10 * time.Second); err != nil {
		t.Fatalf("contender did not acquire the released crash lock: %v\n%s", err, contender.output.String())
	}
	contenderResult := readSpawnSerializationResult(t, filepath.Join(controls, "B.result"))
	assertSpawnSerializationSuccess(t, contenderResult)
	if _, err := os.Stat(filepath.Join(controls, "B.entered-create")); err != nil {
		t.Fatalf("contender never became the creator after owner crash: %v", err)
	}
}

// TestRunSpawnSerializationHelper is selected in separate test-binary processes
// so each helper gets its own package-level create/register seams while the OS
// lock remains the only coordination mechanism between them.
func TestRunSpawnSerializationHelper(t *testing.T) {
	if os.Getenv(spawnSerializationHelperEnv) != "1" {
		return
	}

	target := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_TARGET")
	role := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_ROLE")
	started := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_STARTED")
	acquiring := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_ACQUIRING")
	entered := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_ENTERED")
	release := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_RELEASE")
	registered := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_REGISTERED")
	launched := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_LAUNCHED")
	resultPrefix := os.Getenv("LINGTAI_SPAWN_SERIALIZATION_RESULT")
	if target == "" || (role != "owner" && role != "crash-owner" && role != "contender" && role != "crash-contender") || resultPrefix == "" {
		t.Fatalf("invalid helper controls: target=%q role=%q result=%q", target, role, resultPrefix)
	}
	if started != "" {
		if err := os.WriteFile(started, []byte("helper started\n"), 0o644); err != nil {
			t.Fatalf("publish helper start barrier: %v", err)
		}
	}
	if acquiring != "" {
		originalBeforeAcquire := beforeSpawnSerializationLockAcquire
		beforeSpawnSerializationLockAcquire = func() {
			if err := os.WriteFile(acquiring, []byte("about to acquire lock\n"), 0o644); err != nil {
				t.Fatalf("publish lock-acquisition barrier: %v", err)
			}
		}
		t.Cleanup(func() { beforeSpawnSerializationLockAcquire = originalBeforeAcquire })
	}

	originalRuntimeReady := runtimeReady
	runtimeReady = func(string) error { return nil }
	t.Cleanup(func() { runtimeReady = originalRuntimeReady })
	if role == "contender" {
		stubSpawnRegister(t, func(string, string) error {
			if err := os.WriteFile(registered, []byte("register called\n"), 0o644); err != nil {
				return err
			}
			return fmt.Errorf("contender register should not be called")
		})
		originalLaunch := launchAgent
		launchAgent = func(string, string) (*exec.Cmd, error) {
			if err := os.WriteFile(launched, []byte("launch called\n"), 0o644); err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("contender launch should not be called")
		}
		t.Cleanup(func() { launchAgent = originalLaunch })
	} else {
		stubSpawnRegister(t, func(string, string) error { return nil })
	}
	stubSpawnCreate(t, func(_ context.Context, _ string, request process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
		if entered != "" {
			if err := os.WriteFile(entered, []byte("entered create\n"), 0o644); err != nil {
				return process.ProjectCreateResult{}, err
			}
		}
		if role == "owner" || role == "crash-owner" {
			if err := waitForSpawnSerializationPaths(30*time.Second, release); err != nil {
				return process.ProjectCreateResult{}, err
			}
		}
		return seedSpawnSerializationProject(request)
	})

	var stdout, stderr bytes.Buffer
	code := RunSpawn(&stdout, &stderr, SpawnOpts{
		Dir:        target,
		Preset:     "minimax",
		AgentName:  "alice",
		Language:   "en",
		SkipLaunch: role != "contender",
	})
	writeSpawnSerializationResult(t, resultPrefix, code, stdout.Bytes(), stderr.Bytes())
}

func prepareSpawnSerializationHome(t *testing.T) {
	t.Helper()
	withTempHome(t)
	globalDir := filepath.Join(os.Getenv("HOME"), ".lingtai-tui")
	if err := preset.RefreshTemplates(); err != nil {
		t.Fatalf("refresh templates: %v", err)
	}
	if err := preset.Bootstrap(globalDir); err != nil {
		t.Fatalf("bootstrap presets: %v", err)
	}
}

func seedSpawnSerializationProject(request process.ProjectCreateRequest) (process.ProjectCreateResult, error) {
	agentDir := filepath.Join(request.Dir, ".lingtai", request.AgentName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return process.ProjectCreateResult{}, err
	}
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), []byte(`{"manifest":{"agent_name":"`+request.AgentName+`"}}`), 0o644); err != nil {
		return process.ProjectCreateResult{}, err
	}
	return process.ProjectCreateResult{
		Status: "created", AgentName: request.AgentName, PresetRef: request.Preset, ExitCode: 0,
	}, nil
}

type spawnSerializationHelperProcess struct {
	cmd      *exec.Cmd
	output   bytes.Buffer
	done     chan error
	finished bool
	waitErr  error
}

type spawnSerializationHelperControls struct {
	Started      string
	Acquiring    string
	Entered      string
	Release      string
	Registered   string
	Launched     string
	ResultPrefix string
}

func startSpawnSerializationHelper(target, role string, controls spawnSerializationHelperControls) (*spawnSerializationHelperProcess, error) {
	process := &spawnSerializationHelperProcess{done: make(chan error, 1)}
	process.cmd = exec.Command(os.Args[0], "-test.run=^TestRunSpawnSerializationHelper$")
	process.cmd.Env = append(os.Environ(),
		spawnSerializationHelperEnv+"=1",
		"LINGTAI_SPAWN_SERIALIZATION_TARGET="+target,
		"LINGTAI_SPAWN_SERIALIZATION_ROLE="+role,
		"LINGTAI_SPAWN_SERIALIZATION_STARTED="+controls.Started,
		"LINGTAI_SPAWN_SERIALIZATION_ACQUIRING="+controls.Acquiring,
		"LINGTAI_SPAWN_SERIALIZATION_ENTERED="+controls.Entered,
		"LINGTAI_SPAWN_SERIALIZATION_RELEASE="+controls.Release,
		"LINGTAI_SPAWN_SERIALIZATION_REGISTERED="+controls.Registered,
		"LINGTAI_SPAWN_SERIALIZATION_LAUNCHED="+controls.Launched,
		"LINGTAI_SPAWN_SERIALIZATION_RESULT="+controls.ResultPrefix,
	)
	process.cmd.Stdout = &process.output
	process.cmd.Stderr = &process.output
	if err := process.cmd.Start(); err != nil {
		return nil, err
	}
	go func() {
		process.done <- process.cmd.Wait()
	}()
	return process, nil
}

func (process *spawnSerializationHelperProcess) wait(timeout time.Duration) error {
	if process.finished {
		return process.waitErr
	}
	select {
	case process.waitErr = <-process.done:
		process.finished = true
		return process.waitErr
	case <-time.After(timeout):
		_ = process.cmd.Process.Kill()
		select {
		case process.waitErr = <-process.done:
			process.finished = true
			return fmt.Errorf("helper timed out and was killed: %w", process.waitErr)
		case <-time.After(5 * time.Second):
			return fmt.Errorf("helper timed out and did not terminate after kill")
		}
	}
}

func (process *spawnSerializationHelperProcess) stillRunning() error {
	if process.finished {
		return fmt.Errorf("helper already exited: %w", process.waitErr)
	}
	select {
	case process.waitErr = <-process.done:
		process.finished = true
		return fmt.Errorf("helper exited while expected to block: %w\n%s", process.waitErr, process.output.String())
	default:
		return nil
	}
}

func (process *spawnSerializationHelperProcess) stop() {
	if process == nil || process.finished {
		return
	}
	_ = process.cmd.Process.Kill()
	select {
	case process.waitErr = <-process.done:
		process.finished = true
	case <-time.After(5 * time.Second):
	}
}

type spawnSerializationResult struct {
	code   int
	stdout []byte
	stderr []byte
}

func writeSpawnSerializationResult(t *testing.T, prefix string, code int, stdout, stderr []byte) {
	t.Helper()
	if err := os.WriteFile(prefix+".code", []byte(strconv.Itoa(code)+"\n"), 0o644); err != nil {
		t.Fatalf("write helper result code: %v", err)
	}
	if err := os.WriteFile(prefix+".stdout", stdout, 0o644); err != nil {
		t.Fatalf("write helper stdout: %v", err)
	}
	if err := os.WriteFile(prefix+".stderr", stderr, 0o644); err != nil {
		t.Fatalf("write helper stderr: %v", err)
	}
}

func readSpawnSerializationResult(t *testing.T, prefix string) spawnSerializationResult {
	t.Helper()
	codeData, err := os.ReadFile(prefix + ".code")
	if err != nil {
		t.Fatalf("read helper result code: %v", err)
	}
	code, err := strconv.Atoi(strings.TrimSpace(string(codeData)))
	if err != nil {
		t.Fatalf("parse helper result code %q: %v", codeData, err)
	}
	stdout, err := os.ReadFile(prefix + ".stdout")
	if err != nil {
		t.Fatalf("read helper stdout: %v", err)
	}
	stderr, err := os.ReadFile(prefix + ".stderr")
	if err != nil {
		t.Fatalf("read helper stderr: %v", err)
	}
	return spawnSerializationResult{code: code, stdout: stdout, stderr: stderr}
}

func assertSpawnSerializationBlocked(t *testing.T, process *spawnSerializationHelperProcess, entered string) {
	t.Helper()
	if err := process.stillRunning(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(entered); !os.IsNotExist(err) {
		t.Fatalf("contender reached create while it should block on the owner: %v", err)
	}
}

func assertSpawnSerializationNoContenderSideEffects(t *testing.T, paths ...string) {
	t.Helper()
	for _, path := range paths {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("contender performed a prohibited side effect at %q: %v", path, err)
		}
	}
}

func assertSpawnSerializationSuccess(t *testing.T, result spawnSerializationResult) {
	t.Helper()
	if result.code != 0 || len(result.stderr) != 0 {
		t.Fatalf("success result code/stderr = %d/%q", result.code, result.stderr)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(result.stdout, &response); err != nil {
		t.Fatalf("success stdout is not one JSON document: %v\n%s", err, result.stdout)
	}
	if response["status"] != "created" {
		t.Fatalf("success response = %#v", response)
	}
}

func assertSpawnSerializationAlreadyInitialized(t *testing.T, result spawnSerializationResult) {
	t.Helper()
	if result.code != 1 || len(result.stdout) != 0 {
		t.Fatalf("contender result code/stdout = %d/%q, want 1 and no stdout", result.code, result.stdout)
	}
	var response map[string]interface{}
	if err := json.Unmarshal(result.stderr, &response); err != nil {
		t.Fatalf("contender stderr is not one JSON error: %v\n%s", err, result.stderr)
	}
	if response["code"] != "already_initialized" {
		t.Fatalf("contender response = %#v", response)
	}
}

func waitForSpawnSerializationPaths(timeout time.Duration, paths ...string) error {
	deadline := time.Now().Add(timeout)
	for {
		missing := ""
		for _, path := range paths {
			if _, err := os.Stat(path); err != nil {
				if !os.IsNotExist(err) {
					return fmt.Errorf("stat barrier %q: %w", path, err)
				}
				missing = path
				break
			}
		}
		if missing == "" {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for %q", missing)
		}
		time.Sleep(5 * time.Millisecond)
	}
}
