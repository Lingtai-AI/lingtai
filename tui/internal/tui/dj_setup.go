package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/dj"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// setupDJ creates the DJ agent. The DJ is a general agent — same
// capabilities as a regular orchestrator running on the orchestrator's
// **default** preset (manifest.preset.default). What makes it the "DJ"
// is its covenant + comment, which give it a music-creation mandate and
// tell it to load any library skill tagged `media-creation` to find out
// what providers are available given the user's saved presets.
//
// The DJ is unconditionally set up. Whether it can actually compose music
// is a runtime question the agent answers on the first request — by
// scanning its library for media-creation skills, cross-checking against
// ~/.lingtai-tui/presets/, and either composing or telling the human what
// is missing.
//
// We substitute the orchestrator's default preset's llm + capabilities at
// spawn time and strip manifest.preset, so the kernel's _read_init does
// not re-substitute on every boot.
//
// Layout (mirrors secretary):
//
//	~/.lingtai-tui/dj/                       project root
//	~/.lingtai-tui/dj/.lingtai/              base dir
//	~/.lingtai-tui/dj/.lingtai/dj/           agent working dir
//	~/.lingtai-tui/dj/.lingtai/human/        human mailbox
//	~/.lingtai-tui/dj/recipe/                covenant + comment markdown
func setupDJ(baseDir, globalDir, orchDirName string) error {
	orchInitPath := filepath.Join(baseDir, orchDirName, "init.json")
	data, err := os.ReadFile(orchInitPath)
	if err != nil {
		return fmt.Errorf("read orchestrator init.json: %w", err)
	}
	var initJSON map[string]interface{}
	if err := json.Unmarshal(data, &initJSON); err != nil {
		return fmt.Errorf("parse orchestrator init.json: %w", err)
	}

	manifest, _ := initJSON["manifest"].(map[string]interface{})
	if manifest == nil {
		return fmt.Errorf("orchestrator init.json has no manifest")
	}

	// Substitute the orchestrator's DEFAULT preset's llm + capabilities
	// for whatever the orch has materialized. Keeps DJ's home base stable
	// across orchestrator preset swaps.
	if err := substituteDefaultPreset(manifest); err != nil {
		return fmt.Errorf("load default preset for dj: %w", err)
	}

	// Populate DJ recipe assets on disk
	recipeDir, err := dj.RecipeDir(globalDir)
	if err != nil {
		return fmt.Errorf("populate dj recipe: %w", err)
	}

	manifest["agent_name"] = "dj"
	manifest["soul"] = map[string]interface{}{"delay": 9999999}
	manifest["admin"] = map[string]interface{}{"karma": false, "nirvana": false}

	// Make sure DJ can find its own recipe-skills directory in addition to
	// whatever Tier-1 paths the default preset declared. This is how the
	// DJ discovers any DJ-specific skills we ship in assets/skills/ later.
	if caps, ok := manifest["capabilities"].(map[string]interface{}); ok {
		djSkills := filepath.Join(recipeDir, "skills")
		if libCfg, ok := caps["library"].(map[string]interface{}); ok {
			paths, _ := libCfg["paths"].([]interface{})
			merged := []interface{}{djSkills}
			merged = append(merged, paths...)
			libCfg["paths"] = merged
		} else {
			caps["library"] = map[string]interface{}{
				"paths": []interface{}{djSkills},
			}
		}
	}

	// Strip preset umbrella — DJ runs on materialized values (substituted
	// above). Leaving the umbrella in place would cause the kernel's
	// _read_init to re-substitute the preset's capabilities on every boot,
	// which would clobber the DJ-skills path we just merged in.
	delete(manifest, "preset")

	// DJ recipe files (no procedures override — inherits system-wide)
	initJSON["covenant_file"] = filepath.Join(recipeDir, "covenant.md")
	initJSON["comment_file"] = filepath.Join(recipeDir, "comment.md")

	// No brief / no addons / clear init prompt (greet delivered via .prompt)
	delete(initJSON, "brief_file")
	delete(initJSON, "addons")
	initJSON["prompt"] = ""

	// Standard project skeleton (mirrors secretary).
	agentDir := dj.AgentDir(globalDir)
	lingtaiDir := dj.LingtaiDir(globalDir)
	humanDir := filepath.Join(lingtaiDir, "human")

	for _, sub := range []string{
		"system",
		"logs",
		"mailbox/inbox",
		"mailbox/sent",
		"mailbox/archive",
	} {
		if err := os.MkdirAll(filepath.Join(agentDir, sub), 0o755); err != nil {
			return fmt.Errorf("create dj %s dir: %w", sub, err)
		}
	}
	for _, sub := range []string{"mailbox/inbox", "mailbox/sent", "mailbox/archive"} {
		if err := os.MkdirAll(filepath.Join(humanDir, sub), 0o755); err != nil {
			return fmt.Errorf("create human %s dir: %w", sub, err)
		}
	}

	agentManifest := map[string]interface{}{
		"agent_name": "dj",
		"address":    "dj",
		"state":      "",
		"admin":      map[string]interface{}{"karma": false, "nirvana": false},
	}
	mdata, err := json.MarshalIndent(agentManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dj .agent.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, ".agent.json"), mdata, 0o644); err != nil {
		return fmt.Errorf("write dj .agent.json: %w", err)
	}

	humanManifest := map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
	}
	hmdata, err := json.MarshalIndent(humanManifest, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal human .agent.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(humanDir, ".agent.json"), hmdata, 0o644); err != nil {
		return fmt.Errorf("write human .agent.json: %w", err)
	}

	out, err := json.MarshalIndent(initJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal dj init.json: %w", err)
	}
	if err := os.WriteFile(filepath.Join(agentDir, "init.json"), out, 0o644); err != nil {
		return fmt.Errorf("write dj init.json: %w", err)
	}

	if err := fs.WritePrompt(agentDir, dj.GreetContent()); err != nil {
		return fmt.Errorf("write dj .prompt: %w", err)
	}

	return nil
}

// substituteDefaultPreset reads manifest.preset.default (path string,
// possibly ~-prefixed) and replaces manifest.llm + manifest.capabilities
// with the values from that preset file. If the preset block is missing
// or the file can't be loaded, the manifest is left unchanged (and the
// caller falls back to the orchestrator's currently-materialized values
// — equivalent to the pre-default-preset behavior).
//
// This mirrors the kernel's _activate_preset substitution but runs at
// agent-spawn time in the TUI rather than at boot in the kernel: the
// spawned agent (secretary, DJ) receives a fully-materialized init.json
// without a manifest.preset block, so the kernel's _read_init won't
// re-substitute and clobber our spawn-time customizations.
func substituteDefaultPreset(manifest map[string]interface{}) error {
	preBlock, _ := manifest["preset"].(map[string]interface{})
	if preBlock == nil {
		return nil // older agent, no preset umbrella — keep existing llm/caps
	}
	defaultRef, _ := preBlock["default"].(string)
	if defaultRef == "" {
		return nil
	}
	stem := presetStemFromRef(defaultRef)
	if stem == "" {
		return nil
	}
	p, err := preset.Load(stem)
	if err != nil {
		// Don't hard-fail — the preset file may have been deleted by hand.
		// Falling back to the orch's materialized values is safer than
		// refusing to spawn.
		return nil
	}
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	caps, _ := p.Manifest["capabilities"].(map[string]interface{})
	if llm != nil {
		manifest["llm"] = llm
	}
	if caps != nil {
		manifest["capabilities"] = caps
	}
	return nil
}

// presetStemFromRef converts a stored preset reference (e.g.
// "~/.lingtai-tui/presets/foo.json", "/abs/path/foo.json", or a bare stem
// "foo") into the stem name expected by preset.Load. Returns "" if the
// reference doesn't look like a preset path.
func presetStemFromRef(ref string) string {
	base := filepath.Base(ref)
	base = strings.TrimSuffix(base, ".jsonc")
	base = strings.TrimSuffix(base, ".json")
	if base == "" || base == "." || base == "/" {
		return ""
	}
	return base
}
