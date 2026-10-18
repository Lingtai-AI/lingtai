package preset

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// ProjectConfigurationRequest projects references/public wizard options only.
// No credential/profile/preset document or existing arbitrary MCP config is sent.
// The kernel owns merge/preservation, authorization and owner-document writes.
func ProjectConfigurationRequest(p Preset, agentName, dirName, lingtaiDir, globalDir string, opts AgentOpts) (process.ProjectConfigureRequest, error) {
	if err := ValidateSafeName(dirName); err != nil {
		return process.ProjectConfigureRequest{}, fmt.Errorf("invalid agent directory name")
	}
	envFile, venvPath := opts.EnvFile, opts.VenvPath
	if envFile == "" {
		envFile = config.EnvFilePath(globalDir)
	}
	if venvPath == "" {
		venvPath = filepath.Join(globalDir, "runtime", "venv")
	}
	language := opts.Language
	if language == "" {
		language = "en"
	}
	keep := isSyntheticPreset(p)
	ref := ""
	if !keep {
		ref = RefFor(p)
	}
	options := process.ProjectConfigurationOptions{
		EnvFile: envFile, VenvPath: venvPath, Language: language,
		CovenantFile: opts.CovenantFile, DefaultCovenantFile: CovenantPath(globalDir, language),
		Karma: opts.Karma, Nirvana: opts.Nirvana, PreserveActive: opts.PreserveActivePreset || keep,
		ContextLimit: opts.ContextLimit, MaxRPM: opts.MaxRpm, MaxAEDAttempts: ClampAedAttempts(opts.MaxAedAttempts),
		Addons: opts.Addons, AllowedPresets: opts.AllowedPresets,
	}
	// Addons is compatibility selection metadata for existing legacy writers;
	// canonical output still authors only mcp (never duplicate addons).
	// Only explicitly selected curated MCP specs cross this request. Catalog
	// availability is not activation; existing specs/preservation belong to Core.
	options.MCP = map[string]interface{}{}
	for _, name := range opts.Addons {
		module, env, rel, supported := defaultMCPSpec(name)
		if !supported {
			continue
		}
		options.MCP[name] = map[string]interface{}{"type": "stdio", "command": config.VenvPython(venvPath), "args": []string{"-m", module}, "env": map[string]string{env: rel}}
	}
	if opts.CommentFile != "" {
		data, err := os.ReadFile(opts.CommentFile)
		if err != nil || !utf8.Valid(data) {
			return process.ProjectConfigureRequest{}, fmt.Errorf("Project Pad edit must be readable UTF-8 text")
		}
		pad := string(data)
		options.Pad = &pad
	}
	var prompts *process.ProjectPromptEdits
	if opts.CovenantFile != "" {
		covenant := opts.CovenantFile
		prompts = &process.ProjectPromptEdits{CovenantFile: &covenant}
	}
	return process.ProjectConfigureRequest{ProjectDir: filepath.Dir(lingtaiDir), DirectoryName: dirName, AgentName: agentName, PresetRef: ref, KeepCurrent: keep, Options: options, Prompts: prompts}, nil
}

func configureWithOpts(p Preset, agentName, dirName, lingtaiDir, globalDir string, opts AgentOpts, python string) (process.ProjectConfigurationResult, error) {
	request, err := ProjectConfigurationRequest(p, agentName, dirName, lingtaiDir, globalDir, opts)
	if err != nil {
		return process.ProjectConfigurationResult{}, err
	}
	// The caller already selected the management SDK. Target host selection
	// belongs to Agent launch/inspection, not this configuration operation.
	return process.ConfigureProject(context.Background(), python, request)
}

// SavedPresetPolicy reads the actual selected owner's post-configure policy.
// It neither synthesizes a reference nor reconciles active/default/allowed.
func SavedPresetPolicy(lingtaiDir, selected string) (string, []string, error) {
	if ValidateSafeName(selected) != nil {
		return "", nil, fmt.Errorf("invalid selected directory")
	}
	data, err := os.ReadFile(filepath.Join(lingtaiDir, selected, "init.json"))
	if err != nil {
		return "", nil, fmt.Errorf("selected Project policy unavailable")
	}
	var document map[string]interface{}
	if DecodeJSONUseNumber(data, &document) != nil {
		return "", nil, fmt.Errorf("selected Project policy unavailable")
	}
	policy := InitPresetPolicy(document)
	def, _ := policy["default"].(string)
	var allowed []string
	if refs, ok := policy["allowed"].([]interface{}); ok {
		for _, ref := range refs {
			if text, ok := ref.(string); ok {
				allowed = append(allowed, text)
			}
		}
	}
	if def == "" || len(allowed) == 0 {
		return "", nil, fmt.Errorf("selected Project policy unavailable")
	}
	return def, allowed, nil
}
