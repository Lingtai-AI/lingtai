package headless

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/globalmigrate"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

const defaultReadyTimeout = 10 * time.Second

var (
	createProject           = process.CreateProject
	registerProject         = config.Register
	saveRecipeState         = preset.SaveRecipeState
	runtimeReady            = config.RuntimeReady
	launchAgent             = process.LaunchAgent
	findAgentProcesses      = process.FindAgentProcesses
	terminateAgentProcesses = process.TerminateAgentProcesses
	readHeartbeat           = fs.ReadHeartbeat
	readySleep              = time.Sleep
	readyPollInterval       = 200 * time.Millisecond
	processExitGrace        = 2 * time.Second
	// Package-local defaults keep focused tests coordinated with the actual
	// acquisition/release boundary without changing the headless public surface.
	acquireSpawnSerializationLock       = fs.AcquireExclusiveFileLock
	releaseSpawnSerializationLock       = func(lock *fs.ExclusiveFileLock) error { return lock.Release() }
	beforeSpawnSerializationLockAcquire = func() {}
)

// SpawnOpts holds the parsed flags for the spawn subcommand.
type SpawnOpts struct {
	Dir          string        // target project directory (will be made absolute)
	Preset       string        // preset name
	AgentName    string        // agent name (empty = basename of Dir)
	Language     string        // "en", "zh", or "wen"
	SkipLaunch   bool          // skip agent launch (for testing)
	ReadyTimeout time.Duration // wait-ready timeout after launch
}

// SpawnOutput is the JSON response on success.
type SpawnOutput struct {
	Status                      string  `json:"status"`
	ReadinessStatus             string  `json:"readiness_status"`
	ProjectDir                  string  `json:"project_dir"`
	AgentName                   string  `json:"agent_name"`
	AgentDir                    string  `json:"agent_dir"`
	Preset                      string  `json:"preset"`
	Recipe                      string  `json:"recipe"`
	PID                         int     `json:"pid"`
	InspectableProcessConfirmed bool    `json:"inspectable_process_confirmed"`
	HeartbeatConfirmed          bool    `json:"heartbeat_confirmed"`
	HeartbeatAgeSeconds         float64 `json:"heartbeat_age_seconds,omitempty"`
	ReadyTimeoutSeconds         float64 `json:"ready_timeout_seconds"`
}

// resolveAgentName returns AgentName if set, otherwise the basename of Dir.
func (o SpawnOpts) resolveAgentName() string {
	if o.AgentName != "" {
		return o.AgentName
	}
	return filepath.Base(o.Dir)
}

// RunSpawn creates a new project and launches an agent. Returns exit code.
// Writes success JSON to stdout, error JSON to stderr.
func RunSpawn(stdout, stderr io.Writer, opts SpawnOpts) int {
	// Make directory absolute
	absDir, err := filepath.Abs(opts.Dir)
	if err != nil {
		WriteError(stderr, "invalid directory: "+err.Error(), "invalid_args")
		return 1
	}
	opts.Dir = absDir

	// The agent name doubles as a directory segment under .lingtai/, so it
	// must be a single contained path segment: reject absolute paths,
	// parent segments, and either platform's separators before any project
	// write (issue #849).
	agentName := opts.resolveAgentName()
	if err := preset.ValidateSafeName(agentName); err != nil {
		WriteError(stderr, fmt.Sprintf("invalid agent name %q: %s", agentName, err.Error()), "invalid_args")
		return 1
	}

	// Serialize cooperating headless creators for this root before the .lingtai
	// precheck. The persistent sibling sidecar keeps the kernel target empty.
	beforeSpawnSerializationLockAcquire()
	spawnLock, err := acquireSpawnSerializationLock(spawnSerializationLockPath(opts.Dir))
	if err != nil {
		WriteError(stderr, "cannot acquire project serialization lock: "+err.Error(), "init_failed")
		return 1
	}
	releaseSpawnLock := func() error {
		if spawnLock == nil {
			return nil
		}
		lock := spawnLock
		spawnLock = nil
		return releaseSpawnSerializationLock(lock)
	}
	defer func() { _ = releaseSpawnLock() }()

	// Validate: target must not already have .lingtai/
	lingtaiDir := filepath.Join(opts.Dir, ".lingtai")
	if _, err := os.Stat(lingtaiDir); err == nil {
		WriteError(stderr, ".lingtai/ already exists in "+opts.Dir, "already_initialized")
		return 1
	}

	// Ensure target directory exists
	if err := os.MkdirAll(opts.Dir, 0o755); err != nil {
		WriteError(stderr, "cannot create directory: "+err.Error(), "init_failed")
		return 1
	}

	// Global config directory
	globalDir, err := config.GlobalDir()
	if err != nil {
		WriteError(stderr, "cannot resolve global dir: "+err.Error(), "init_failed")
		return 1
	}

	// Run global migrations (best-effort, same as main TUI path)
	globalmigrate.Run(globalDir)

	// Headless spawn never installs, repairs, or upgrades the runtime: it
	// cannot prompt, and the only automatic opportunity to install/repair it
	// is the interactive TUI's shared startup preflight, gated on explicit
	// current-launch human consent. install.sh installs the kernel by
	// default; config.RuntimeReady is a pure readiness check (never
	// creates/repairs/upgrades) — a not-ready runtime surfaces as an
	// actionable error instead of a silent install.
	if !opts.SkipLaunch {
		if err := runtimeReady(globalDir); err != nil {
			WriteError(stderr, err.Error(), "bootstrap_failed")
			return 1
		}
	}

	// Bootstrap presets + assets
	if err := preset.Bootstrap(globalDir); err != nil {
		WriteError(stderr, "bootstrap failed: "+err.Error(), "bootstrap_failed")
		return 1
	}

	// Load the requested preset
	p, err := preset.Load(opts.Preset)
	if err != nil {
		WriteError(stderr,
			fmt.Sprintf("preset %q not found: %s", opts.Preset, err.Error()),
			"preset_not_found")
		return 1
	}

	// agentName was resolved and validated at the top of RunSpawn (before
	// any project write).
	lang := opts.Language
	if lang == "" {
		lang = "en"
	}
	presetPath, err := selectedPresetFile(p)
	if err != nil {
		WriteError(stderr, "cannot resolve selected preset: "+err.Error(), "init_failed")
		return 1
	}

	// The kernel's create contract takes covenant contents only through a file.
	// Materialize the selected TUI covenant in a new, TUI-owned input and remove
	// only that input after the child has reached a terminal result.
	covenantFile, err := materializeSpawnCovenant(globalDir, lang)
	if err != nil {
		WriteError(stderr, "cannot prepare covenant input: "+err.Error(), "init_failed")
		return 1
	}
	createResult, createErr := createProject(context.Background(), config.LingtaiCmd(globalDir), process.ProjectCreateRequest{
		Dir:          opts.Dir,
		AgentName:    agentName,
		Preset:       presetPath,
		CovenantFile: covenantFile,
	})
	cleanupErr := os.Remove(covenantFile)
	if createErr != nil {
		writeProjectCreateFailure(stderr, createErr)
		return 1
	}

	// Keep the kernel's canonical preset reference as trusted create state even
	// though the public SpawnOutput preserves its existing selected-preset field.
	// The adapter already validates this protocol; this guard keeps an injected
	// or future adapter from registering or launching a partial exit-0 result.
	canonicalPresetRef := createResult.PresetRef
	if canonicalPresetRef == "" || !filepath.IsAbs(canonicalPresetRef) {
		writeProjectCreateFailure(stderr, &process.ProjectCreateError{
			Kind:     process.ProjectCreateTransportFailure,
			Reason:   "malformed_result",
			ExitCode: createResult.ExitCode,
			Stdout:   createResult.Stdout,
			Stderr:   createResult.Stderr,
			Cause:    fmt.Errorf("kernel success omitted a canonical preset reference"),
		})
		return 1
	}

	agentDir := filepath.Join(lingtaiDir, agentName)
	if err := os.MkdirAll(filepath.Join(lingtaiDir, ".library_shared"), 0o755); err != nil {
		// The kernel owns the seed tree. Restore only the TUI-required shared
		// library directory and never compensate by deleting kernel-created data.
		writeCreatedProjectRecovery(stderr, opts.Dir, agentName, agentDir, "shared_library_setup", err)
		return 1
	}
	if cleanupErr != nil {
		// The kernel result is known, but do not proceed to registration or
		// launch when TUI-owned sensitive input cleanup was not completed.
		writeCreatedProjectRecovery(stderr, opts.Dir, agentName, agentDir, "temporary_covenant_cleanup", cleanupErr)
		return 1
	}
	if err := preserveSpawnLanguage(agentDir, lang); err != nil {
		// Kernel create has no language argument. Preserve the existing
		// headless language behavior only after its complete success protocol,
		// and never compensate by deleting the project on a later TUI failure.
		writeCreatedProjectRecovery(stderr, opts.Dir, agentName, agentDir, "language_compatibility", err)
		return 1
	}

	// Populate bundled library skills after kernel seeding succeeds.
	preset.PopulateBundledLibrary(globalDir)

	// Apply the existing plain recipe (no greet, no behavioral constraints).
	// This legacy bundle/apply work is deliberately best-effort: recipe state
	// records the selected recipe, not proof that every optional recipe artifact
	// was copied or applied.
	recipeDir := preset.RecipeDir(globalDir, "plain")
	if recipeDir != "" {
		if err := preset.CopyBundle(recipeDir, opts.Dir); err != nil {
			// Non-fatal for plain recipe (has no greet/library)
			fmt.Fprintf(os.Stderr, "warning: recipe copy: %v\n", err)
		}
		subst := func(tmpl string) string { return tmpl }
		preset.ApplyRecipe(opts.Dir, lang, subst)
	}

	// Persist the selected TUI-only plain recipe state before registration. Unlike
	// the best-effort bundle/apply side effects above, this is required TUI
	// metadata after trusted kernel creation.
	if err := saveRecipeState(lingtaiDir, preset.RecipeState{Recipe: "plain"}); err != nil {
		writeCreatedProjectRecovery(stderr, opts.Dir, agentName, agentDir, "recipe_state_persistence", err)
		return 1
	}

	// Register only after a fully trusted kernel create result and required
	// TUI-side metadata writes. A registration failure is a known-created recovery
	// state, not a successful spawn: the kernel has no rollback command.
	if err := registerProject(globalDir, opts.Dir); err != nil {
		writeCreatedProjectRecovery(stderr, opts.Dir, agentName, agentDir, "registration", err)
		return 1
	}

	// A competing creator now sees a complete created/registered root at its
	// guarded precheck, so do not hold it through launch or readiness waiting.
	if err := releaseSpawnLock(); err != nil {
		// Registration already succeeded, so the created_unregistered recovery
		// protocol is not honest here. Retain the established init_failed shape
		// without launching after an incomplete release boundary.
		WriteError(stderr, "project was created and registered but serialization lock release did not complete cleanly; it was not launched: "+err.Error(), "init_failed")
		return 1
	}

	// Launch the agent (async — start and return immediately)
	pid := 0
	status := "created"
	readinessStatus := "not_launched"
	inspectableProcessConfirmed := false
	heartbeatConfirmed := false
	heartbeatAgeSeconds := 0.0
	readyTimeout := readyTimeoutOrDefault(opts.ReadyTimeout)
	if !opts.SkipLaunch {
		lingtaiCmd := config.LingtaiCmd(globalDir)
		cmd, err := launchAgent(lingtaiCmd, agentDir)
		if err != nil {
			WriteError(stderr, "agent launch failed: "+err.Error(), "launch_failed")
			return 1
		}
		pid = cmd.Process.Pid
		cmd.Process.Release()

		ready := waitForAgentReady(agentDir, readyTimeout)
		if ready.Code != "ready" {
			details := map[string]interface{}{
				"agent_dir":                     agentDir,
				"pid":                           pid,
				"readiness_status":              ready.Code,
				"inspectable_process_confirmed": ready.InspectableProcessConfirmed,
				"heartbeat_confirmed":           ready.HeartbeatConfirmed,
				"heartbeat":                     ready.Heartbeat,
				"ready_timeout_seconds":         readyTimeout.Seconds(),
			}
			if cleanupErr := terminateAgentProcesses(agentDir); cleanupErr != nil {
				details["cleanup_error"] = cleanupErr.Error()
			}
			WriteErrorDetail(stderr, ready.Message, ready.Code, details)
			return 1
		}
		status = "ready"
		readinessStatus = ready.Code
		inspectableProcessConfirmed = ready.InspectableProcessConfirmed
		heartbeatConfirmed = ready.HeartbeatConfirmed
		heartbeatAgeSeconds = ready.Heartbeat.AgeSeconds
		if ready.PID != 0 {
			pid = ready.PID
		}
	}

	WriteJSON(stdout, SpawnOutput{
		Status:                      status,
		ReadinessStatus:             readinessStatus,
		ProjectDir:                  opts.Dir,
		AgentName:                   agentName,
		AgentDir:                    agentDir,
		Preset:                      opts.Preset,
		Recipe:                      "plain",
		PID:                         pid,
		InspectableProcessConfirmed: inspectableProcessConfirmed,
		HeartbeatConfirmed:          heartbeatConfirmed,
		HeartbeatAgeSeconds:         heartbeatAgeSeconds,
		ReadyTimeoutSeconds:         readyTimeout.Seconds(),
	})
	return 0
}

// spawnSerializationLockPath returns the stable sibling lock path for root. A
// resolved root makes an existing symlink alias share the same lock identity;
// an unresolved root remains creatable through its cleaned absolute input.
func spawnSerializationLockPath(root string) string {
	identity := filepath.Clean(root)
	if resolved, err := filepath.EvalSymlinks(root); err == nil {
		if absolute, err := filepath.Abs(filepath.Clean(resolved)); err == nil {
			identity = absolute
		}
	}
	return identity + ".lingtai-spawn.lock"
}

func selectedPresetFile(p preset.Preset) (string, error) {
	ref := preset.RefFor(p)
	if ref == "" {
		return "", fmt.Errorf("loaded preset has no reference")
	}
	if strings.HasPrefix(ref, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		ref = filepath.Join(home, ref[2:])
	}
	return filepath.Abs(ref)
}

func materializeSpawnCovenant(globalDir, lang string) (string, error) {
	data, err := os.ReadFile(preset.CovenantPath(globalDir, lang))
	if err != nil {
		return "", err
	}
	if len(data) == 0 {
		return "", fmt.Errorf("selected covenant is empty")
	}
	file, err := os.CreateTemp("", "lingtai-spawn-covenant-*")
	if err != nil {
		return "", err
	}
	path := file.Name()
	fail := func(cause error) (string, error) {
		_ = file.Close()
		_ = os.Remove(path) // only the file this function created
		return "", cause
	}
	if _, err := file.Write(data); err != nil {
		return fail(err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path) // only the file this function created
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		_ = os.Remove(path) // only the file this function created
		return "", err
	}
	return absolute, nil
}

// preserveSpawnLanguage is the narrow compatibility mapping for the existing
// headless language option. The kernel project-create grammar intentionally has
// no language flag, so this runs only after its trusted terminal success.
func preserveSpawnLanguage(agentDir, lang string) error {
	initPath := filepath.Join(agentDir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.UseNumber()
	var init map[string]interface{}
	if err := decoder.Decode(&init); err != nil {
		return err
	}
	var extra interface{}
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return fmt.Errorf("init.json contains more than one JSON document")
		}
		return err
	}
	manifest, ok := init["manifest"].(map[string]interface{})
	if !ok || manifest == nil {
		return fmt.Errorf("init.json manifest is missing")
	}
	manifest["language"] = lang
	updated, err := json.MarshalIndent(init, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(initPath, updated, 0o644)
}

// writeCreatedProjectRecovery reports a known kernel-created project that could
// not complete a later TUI compatibility or registration step. It deliberately
// leaves the seed in place for repair and prevents any later launch or retry.
func writeCreatedProjectRecovery(stderr io.Writer, projectDir, agentName, agentDir, step string, cause error) {
	WriteErrorDetail(stderr, fmt.Sprintf("project was created but %s failed; it was not registered or launched: %v", strings.ReplaceAll(step, "_", " "), cause), "init_failed", map[string]interface{}{
		"recovery_state": "created_unregistered",
		"recovery_step":  step,
		"project_dir":    projectDir,
		"agent_name":     agentName,
		"agent_dir":      agentDir,
		"cause":          cause.Error(),
	})
}

func writeProjectCreateFailure(stderr io.Writer, err error) {
	var failure *process.ProjectCreateError
	if errors.As(err, &failure) && failure.Kind == process.ProjectCreateHandlerFailure {
		details := map[string]interface{}{
			"kernel_code": failure.Code,
		}
		if failure.Stderr != "" {
			details["kernel_stderr"] = failure.Stderr
		}
		WriteErrorDetail(stderr, failure.Message, "init_failed", details)
		return
	}

	details := map[string]interface{}{}
	message := "project create outcome is unknown; it was not registered or launched"
	if errors.As(err, &failure) {
		details["reason"] = failure.Reason
		details["exit_code"] = failure.ExitCode
		if failure.Stdout != "" {
			details["kernel_stdout"] = failure.Stdout
		}
		if failure.Stderr != "" {
			details["kernel_stderr"] = failure.Stderr
		}
	} else {
		details["cause"] = err.Error()
	}
	WriteErrorDetail(stderr, message, "init_failed", details)
}

type readinessResult struct {
	Code                        string
	Message                     string
	PID                         int
	InspectableProcessConfirmed bool
	HeartbeatConfirmed          bool
	Heartbeat                   fs.HeartbeatStatus
}

func readyTimeoutOrDefault(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return defaultReadyTimeout
	}
	return timeout
}

func waitForAgentReady(agentDir string, timeout time.Duration) readinessResult {
	timeout = readyTimeoutOrDefault(timeout)
	started := time.Now()
	deadline := started.Add(timeout)
	var lastHeartbeat fs.HeartbeatStatus
	var lastPID int
	var inspectable bool

	for {
		lastHeartbeat = readHeartbeat(agentDir, 3.0)
		heartbeatConfirmed := lastHeartbeat.Fresh
		procs := findAgentProcesses(agentDir)
		inspectable = len(procs) > 0
		if inspectable {
			lastPID = procs[0].PID
		}
		if heartbeatConfirmed && inspectable {
			status := fs.ReadStatus(agentDir)
			if status.Runtime.PID != 0 {
				lastPID = status.Runtime.PID
			}
			return readinessResult{
				Code:                        "ready",
				PID:                         lastPID,
				InspectableProcessConfirmed: true,
				HeartbeatConfirmed:          true,
				Heartbeat:                   lastHeartbeat,
			}
		}
		if !inspectable && time.Since(started) > processExitGrace {
			return readinessResult{
				Code:                        "process_exited_before_ready",
				Message:                     fmt.Sprintf("agent process exited before writing an inspectable fresh heartbeat; see %s", filepath.Join(agentDir, "logs", "agent.log")),
				InspectableProcessConfirmed: false,
				HeartbeatConfirmed:          heartbeatConfirmed,
				Heartbeat:                   lastHeartbeat,
			}
		}
		if time.Now().After(deadline) {
			return readinessResult{
				Code:                        "readiness_timeout",
				Message:                     fmt.Sprintf("agent did not become inspectable with a fresh heartbeat within %s; see %s", timeout, filepath.Join(agentDir, "logs", "agent.log")),
				PID:                         lastPID,
				InspectableProcessConfirmed: inspectable,
				HeartbeatConfirmed:          heartbeatConfirmed,
				Heartbeat:                   lastHeartbeat,
			}
		}
		readySleep(readyPollInterval)
	}
}
