package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"time"
)

// ProjectConfigurationOptions is PRJ003's public projection, not a preset,
// credential map, runtime configuration or wizard Draft. Explicit empty path
// fallbacks remain subject to the current kernel init owner's validation.
type ProjectConfigurationOptions struct {
	EnvFile        string                 `json:"env_file"`
	VenvPath       string                 `json:"venv_path"`
	Language       string                 `json:"language"`
	Karma          bool                   `json:"karma"`
	Nirvana        bool                   `json:"nirvana"`
	PreserveActive bool                   `json:"preserve_active"`
	ContextLimit   int                    `json:"context_limit"`
	MaxRPM         int                    `json:"max_rpm"`
	MaxAEDAttempts int                    `json:"max_aed_attempts"`
	Pad            *string                `json:"pad,omitempty"`
	Addons         []string               `json:"addons,omitempty"`
	AllowedPresets []string               `json:"allowed_presets,omitempty"`
	MCP            map[string]interface{} `json:"mcp,omitempty"`
}

// Only explicit edits are sent. Nil preserves the current Psyche owner bytes;
// pointer fields distinguish omission from a deliberate empty value.
type ProjectPromptEdits struct {
	BasePrompt     *string `json:"base_prompt,omitempty"`
	BasePromptFile *string `json:"base_prompt_file,omitempty"`
	Covenant       *string `json:"covenant,omitempty"`
	CovenantFile   *string `json:"covenant_file,omitempty"`
}

type ProjectConfigureRequest struct {
	ProjectDir    string                      `json:"project_dir"`
	DirectoryName string                      `json:"directory_name"`
	AgentName     string                      `json:"agent_name"`
	PresetRef     string                      `json:"preset_ref"`
	KeepCurrent   bool                        `json:"keep_current"`
	Options       ProjectConfigurationOptions `json:"options"`
	Prompts       *ProjectPromptEdits         `json:"prompts,omitempty"`
}

type ProjectSourceReceipt struct {
	InitWritten     bool   `json:"init_written"`
	ManifestWritten bool   `json:"manifest_written"`
	PsycheWritten   bool   `json:"psyche_written"`
	ReaderStatus    string `json:"reader_status"`
}

type ProjectConfigurationResult struct {
	Status  string               `json:"status"`
	Outcome string               `json:"outcome"`
	Updated int                  `json:"updated"`
	Skipped int                  `json:"skipped"`
	Failed  int                  `json:"failed"`
	Code    string               `json:"code"`
	Message string               `json:"error"`
	Source  ProjectSourceReceipt `json:"source"`
}

// A validated partial receipt survives as both the return value and typed error.
// Counts are writes, NOT transactions or agent readiness. Unknown never means
// rollback, including when the kernel itself returned an unknown receipt.
type ProjectConfigurationError struct {
	Result ProjectConfigurationResult
	Known  bool
}

func (e *ProjectConfigurationError) Error() string {
	if !e.Known {
		return "kernel Project configuration outcome unknown; inspect project state before a new action. An installed kernel supporting project initialize/configure/propagate-policy/propagate-config/rehydrate is required; no automatic installation, retry or legacy fallback"
	}
	return fmt.Sprintf("kernel Project configuration %s (%s): updated=%d skipped=%d failed=%d; source init=%t manifest=%t psyche=%t reader=%s. Prior writes remain; inspect before another action", e.Result.Outcome, e.Result.Code, e.Result.Updated, e.Result.Skipped, e.Result.Failed, e.Result.Source.InitWritten, e.Result.Source.ManifestWritten, e.Result.Source.PsycheWritten, e.Result.Source.ReaderStatus)
}

func InitializeProject(ctx context.Context, python, root string) (ProjectConfigurationResult, error) {
	return runProjectConfiguration(ctx, python, "initialize", "initialized", struct {
		ProjectDir string `json:"project_dir"`
	}{root})
}
func ConfigureProject(ctx context.Context, python string, request ProjectConfigureRequest) (ProjectConfigurationResult, error) {
	return runProjectConfiguration(ctx, python, "configure", "configured", request)
}
func RehydrateProject(ctx context.Context, python string, request ProjectConfigureRequest) (ProjectConfigurationResult, error) {
	return runProjectConfiguration(ctx, python, "rehydrate", "rehydrated", request)
}
func PropagateProjectPolicy(ctx context.Context, python, root, selected, defaultRef string, allowed []string) (ProjectConfigurationResult, error) {
	return runProjectConfiguration(ctx, python, "propagate-policy", "policy_propagated", struct {
		ProjectDir     string   `json:"project_dir"`
		DirectoryName  string   `json:"directory_name"`
		DefaultRef     string   `json:"default_ref"`
		AllowedPresets []string `json:"allowed_presets"`
	}{root, selected, defaultRef, allowed})
}
func PropagateProjectConfig(ctx context.Context, python, root, selected string) (ProjectConfigurationResult, error) {
	return runProjectConfiguration(ctx, python, "propagate-config", "config_propagated", struct {
		ProjectDir    string `json:"project_dir"`
		DirectoryName string `json:"directory_name"`
	}{root, selected})
}

// ProjectPython uses the SAME existing init venv -> fallback selection as
// launch. Composition supplies config.LingtaiCmd(globalDir) or its real venv.
func ProjectPython(agentDir, fallback string) string { return resolvePython(agentDir, fallback) }

func configurationCode(code string) bool {
	switch code {
	case "invalid_project_request", "invalid_project_root", "invalid_member_directory", "invalid_preset",
		"credentials_must_remain_outer", "existing_document_unreadable", "source_document_unreadable",
		"member_document_unreadable", "blueprint_unreadable", "keep_current_reference_missing",
		"mixed_initialization_state", "rehydration_source_missing", "nonregular_project_document",
		"project_observation_failed", "skeleton_initialization_failed", "configuration_preflight_failed",
		"init_write_failed", "manifest_write_failed", "psyche_write_failed", "configuration_reader_failed",
		"configuration_request_failed":
		return true
	}
	return false
}

func decodeConfigurationResult(data []byte) (ProjectConfigurationResult, error) {
	var result ProjectConfigurationResult
	if err := strictProjectObject(data, &result); err != nil {
		return result, err
	}
	// The shared strict decoder checks top-level required fields. Check nested
	// required fields too; false/zero must never hide a missing/null receipt key.
	var raw struct {
		Source json.RawMessage `json:"source"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return result, err
	}
	if err := strictProjectObject(raw.Source, &result.Source); err != nil {
		return result, err
	}
	if result.Updated < 0 || result.Skipped < 0 || result.Failed < 0 {
		return result, errors.New("negative count")
	}
	if (result.Source.InitWritten && result.Updated == 0) || (result.Source.ManifestWritten && !result.Source.InitWritten) || (result.Source.PsycheWritten && !result.Source.ManifestWritten) {
		return result, errors.New("inconsistent source receipt")
	}
	switch result.Source.ReaderStatus {
	case "not_read", "READ_FAILED", "FULLY_EFFECTIVE", "READ_OK_WITH_IGNORED_FIELDS":
	default:
		return result, errors.New("invalid reader status")
	}
	return result, nil
}

func runProjectConfiguration(ctx context.Context, python, operation, status string, request interface{}) (ProjectConfigurationResult, error) {
	unknown := func() (ProjectConfigurationResult, error) {
		result := ProjectConfigurationResult{Outcome: "unknown", Source: ProjectSourceReceipt{ReaderStatus: "not_read"}}
		return result, &ProjectConfigurationError{Result: result}
	}
	input, err := json.Marshal(request)
	if err != nil || len(input) > projectLineLimit {
		return unknown()
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, kernelCLI(python), "project", operation)
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(input) // exactly one EOF-delimited object, no flags
	stdout := boundedProjectOutput{limit: projectLineLimit}
	stderr := boundedProjectOutput{limit: 16384}
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if ctx.Err() != nil || stdout.overflow || stderr.overflow {
		return unknown()
	}
	if err == nil {
		if stderr.Len() != 0 {
			return unknown()
		}
		result, decodeErr := decodeConfigurationResult(stdout.Bytes())
		if decodeErr != nil || result.Status != status || (result.Outcome != "complete" && result.Outcome != "unchanged") || result.Code != "" || result.Message != "" || result.Failed != 0 {
			return unknown()
		}
		if result.Outcome == "unchanged" && (result.Updated != 0 || result.Source.InitWritten || result.Source.ManifestWritten || result.Source.PsycheWritten) {
			return unknown()
		}
		if (operation == "configure" || operation == "rehydrate") && result.Outcome == "complete" {
			if !result.Source.InitWritten || !result.Source.ManifestWritten || (result.Source.ReaderStatus != "FULLY_EFFECTIVE" && result.Source.ReaderStatus != "READ_OK_WITH_IGNORED_FIELDS") {
				return unknown()
			}
		}
		return result, nil
	}
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 || stdout.Len() != 0 {
		return unknown()
	}
	result, decodeErr := decodeConfigurationResult(stderr.Bytes())
	if decodeErr != nil || result.Status != "error" || !configurationCode(result.Code) || result.Message != "Project configuration could not complete" {
		return unknown()
	}
	switch result.Outcome {
	case "rejected":
		if result.Updated != 0 || result.Source.InitWritten || result.Source.ManifestWritten || result.Source.PsycheWritten {
			return unknown()
		}
	case "partial", "unknown":
	default:
		return unknown()
	}
	return result, &ProjectConfigurationError{Result: result, Known: result.Outcome != "unknown"}
}
