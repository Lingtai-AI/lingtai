package headless

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/globalmigrate"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

const defaultReadyTimeout = 10 * time.Second

var (
	launchAgent             = process.LaunchAgent
	findAgentProcesses      = process.FindAgentProcesses
	terminateAgentProcesses = process.TerminateAgentProcesses
	readHeartbeat           = fs.ReadHeartbeat
	readySleep              = time.Sleep
	readyPollInterval       = 200 * time.Millisecond
	processExitGrace        = 2 * time.Second
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
		if err := config.RuntimeReady(globalDir); err != nil {
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

	agentName := opts.resolveAgentName()
	lang := opts.Language
	if lang == "" {
		lang = "en"
	}

	// Initialize project (.lingtai/, human dir, meta.json)
	if err := process.InitProject(lingtaiDir); err != nil {
		WriteError(stderr, "project init failed: "+err.Error(), "init_failed")
		return 1
	}

	// Populate bundled library skills
	preset.PopulateBundledLibrary(globalDir)

	// Generate init.json with default opts
	agentOpts := preset.DefaultAgentOpts()
	agentOpts.Language = lang
	if err := preset.GenerateInitJSONWithOpts(p, agentName, agentName, lingtaiDir, globalDir, agentOpts); err != nil {
		WriteError(stderr, "init.json generation failed: "+err.Error(), "init_failed")
		return 1
	}

	// Apply the plain recipe (no greet, no behavioral constraints)
	recipeDir := preset.RecipeDir(globalDir, "plain")
	if recipeDir != "" {
		if err := preset.CopyBundle(recipeDir, opts.Dir); err != nil {
			// Non-fatal for plain recipe (has no greet/library)
			fmt.Fprintf(os.Stderr, "warning: recipe copy: %v\n", err)
		}
		subst := func(tmpl string) string { return tmpl }
		preset.ApplyRecipe(opts.Dir, lang, subst)
	}

	// Save recipe state
	preset.SaveRecipeState(lingtaiDir, preset.RecipeState{Recipe: "plain"})

	agentDir := filepath.Join(lingtaiDir, agentName)

	// Register project in global registry (non-fatal)
	config.Register(globalDir, opts.Dir)

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
