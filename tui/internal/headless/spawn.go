package headless

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/globalmigrate"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// SpawnOpts holds the parsed flags for the spawn subcommand.
type SpawnOpts struct {
	Dir        string // target project directory (will be made absolute)
	Preset     string // preset name
	AgentName  string // agent name (empty = basename of Dir)
	Language   string // "en", "zh", or "wen"
	SkipLaunch bool   // skip agent launch (for testing)
}

// SpawnOutput is the JSON response on success.
type SpawnOutput struct {
	Status     string `json:"status"`
	ProjectDir string `json:"project_dir"`
	AgentName  string `json:"agent_name"`
	AgentDir   string `json:"agent_dir"`
	Preset     string `json:"preset"`
	Recipe     string `json:"recipe"`
	PID        int    `json:"pid"`
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

	// Ensure venv exists, then always run the non-blocking upgrade check so
	// newly-created/repaired runtimes do not stay on a stale lingtai wheel until
	// the next launch.
	if _, err := config.EnsureRuntime(globalDir); err != nil {
		WriteError(stderr, "venv setup failed: "+err.Error(), "bootstrap_failed")
		return 1
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
	if err := process.InitProject(lingtaiDir, globalDir); err != nil {
		WriteError(stderr, "project init failed: "+err.Error(), "init_failed")
		return 1
	}

	// Populate bundled library skills
	preset.PopulateBundledLibrary(lingtaiDir, globalDir)

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
	if !opts.SkipLaunch {
		lingtaiCmd := config.LingtaiCmd(globalDir)
		cmd, err := process.LaunchAgent(lingtaiCmd, agentDir)
		if err != nil {
			WriteError(stderr, "agent launch failed: "+err.Error(), "launch_failed")
			return 1
		}
		pid = cmd.Process.Pid
		cmd.Process.Release()
	}

	WriteJSON(stdout, SpawnOutput{
		Status:     "launched",
		ProjectDir: opts.Dir,
		AgentName:  agentName,
		AgentDir:   agentDir,
		Preset:     opts.Preset,
		Recipe:     "plain",
		PID:        pid,
	})
	return 0
}
