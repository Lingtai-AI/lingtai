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
		Karma: opts.Karma, Nirvana: opts.Nirvana, PreserveActive: opts.PreserveActivePreset || keep,
		ContextLimit: opts.ContextLimit, MaxRPM: opts.MaxRpm, MaxAEDAttempts: ClampAedAttempts(opts.MaxAedAttempts),
		Addons: opts.Addons, AllowedPresets: opts.AllowedPresets,
	}
	// Seeds for the curated catalog also cover previously-selected addons whose
	// activation entry is absent. Existing registrations/specs win in Core.
	options.MCP = map[string]interface{}{}
	for _, name := range []string{"imap", "telegram", "feishu", "wechat", "whatsapp"} {
		module, env, rel, _ := defaultMCPSpec(name)
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
	python = process.ProjectPython(filepath.Join(lingtaiDir, dirName), python)
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
	var document struct {
		Manifest struct {
			Preset struct {
				Default string   `json:"default"`
				Allowed []string `json:"allowed"`
			} `json:"preset"`
		} `json:"manifest"`
	}
	if DecodeJSONUseNumber(data, &document) != nil || document.Manifest.Preset.Default == "" || len(document.Manifest.Preset.Allowed) == 0 {
		return "", nil, fmt.Errorf("selected Project policy unavailable")
	}
	return document.Manifest.Preset.Default, document.Manifest.Preset.Allowed, nil
}
