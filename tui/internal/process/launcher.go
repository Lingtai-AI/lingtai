package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// ErrAgentAlreadyRunning is returned by LaunchAgent when a `lingtai-agent run`
// process is already alive in the target workdir. Callers should surface this
// to the user rather than re-attempting the launch.
var ErrAgentAlreadyRunning = errors.New("a lingtai agent is already running in this workdir")

// ProjectCreateRequest is the literal input to `lingtai-agent project create`.
// Callers must keep CovenantFile available until CreateProject returns because
// the kernel reads its contents while the child is running.
type ProjectCreateRequest struct {
	Dir          string
	AgentName    string
	Preset       string
	CovenantFile string
}

// ProjectCreateResult is a complete, accepted kernel create response. Child
// output remains captured here rather than leaking into a headless caller's
// stdout protocol.
type ProjectCreateResult struct {
	Status    string
	AgentName string
	PresetRef string
	Stdout    string
	Stderr    string
	ExitCode  int
}

// ProjectCreateFailureKind distinguishes a kernel-declared application error
// from an execution outcome for which the TUI must not infer project state.
type ProjectCreateFailureKind string

const (
	ProjectCreateHandlerFailure   ProjectCreateFailureKind = "handler"
	ProjectCreateTransportFailure ProjectCreateFailureKind = "transport"
)

// ProjectCreateError retains the child outcome for diagnostic rendering. A
// Handler failure is trusted only for the kernel's documented exit-1 JSON
// stderr protocol; all other failures are transport/unknown outcomes.
type ProjectCreateError struct {
	Kind     ProjectCreateFailureKind
	Reason   string
	Code     string
	Message  string
	ExitCode int
	Stdout   string
	Stderr   string
	Cause    error
}

func (e *ProjectCreateError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return fmt.Sprintf("project create %s: %v", e.Reason, e.Cause)
	}
	return fmt.Sprintf("project create %s", e.Reason)
}

func (e *ProjectCreateError) Unwrap() error { return e.Cause }

// InitProject creates the legacy TUI-owned project skeleton. It remains for
// its independent callers; headless spawn seeds fresh projects through
// CreateProject instead.
func InitProject(lingtaiDir string) error {
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		return fmt.Errorf("create .lingtai: %w", err)
	}
	humanDir := filepath.Join(lingtaiDir, "human")
	manifestPath := filepath.Join(humanDir, ".agent.json")
	if _, err := os.Stat(manifestPath); err == nil {
		return nil
	}
	for _, sub := range []string{
		"mailbox/inbox",
		"mailbox/sent",
		"mailbox/archive",
	} {
		if err := os.MkdirAll(filepath.Join(humanDir, sub), 0o755); err != nil {
			return fmt.Errorf("create %s: %w", sub, err)
		}
	}
	manifest := map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
	}
	data, _ := json.MarshalIndent(manifest, "", "  ")
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	contactsPath := filepath.Join(humanDir, "mailbox", "contacts.json")
	if err := os.WriteFile(contactsPath, []byte("[]"), 0o644); err != nil {
		return fmt.Errorf("write contacts: %w", err)
	}
	// TUI asset directory — viz data, topology snapshots, NOT agent state
	tuiAssetDir := filepath.Join(lingtaiDir, ".tui-asset")
	if err := os.MkdirAll(tuiAssetDir, 0o755); err != nil {
		return fmt.Errorf("create .tui-asset: %w", err)
	}
	// Network-shared library — the collective knowledge base that agents
	// reach through the default library.paths entry "../.library_shared"
	// (relative to <agent>/). Create it empty so the kernel's library cap
	// doesn't warn about a missing Tier-1 path on first launch. Idempotent;
	// m018 also creates this for legacy projects during upgrade.
	libraryShared := filepath.Join(lingtaiDir, ".library_shared")
	if err := os.MkdirAll(libraryShared, 0o755); err != nil {
		return fmt.Errorf("create .library_shared: %w", err)
	}
	return nil
}

// resolvePython returns the Python executable for an agent.
// Priority: agent init.json venv_path → fallbackCmd.
func resolvePython(agentDir, fallbackCmd string) string {
	initPath := filepath.Join(agentDir, "init.json")
	data, err := os.ReadFile(initPath)
	if err == nil {
		var init map[string]interface{}
		if json.Unmarshal(data, &init) == nil {
			if vp, ok := init["venv_path"].(string); ok && vp != "" {
				python := config.VenvPython(vp)
				if _, err := os.Stat(python); err == nil {
					return python
				}
			}
		}
	}
	return fallbackCmd
}

// kernelCLI resolves the managed kernel executable beside its interpreter.
func kernelCLI(python string) string {
	return kernelCLIForOS(python, runtime.GOOS)
}

// kernelCLIForOS keeps the platform suffix decision independently testable
// without executing a platform-specific child process.
func kernelCLIForOS(python, goos string) string {
	name := "lingtai-agent"
	if goos == "windows" {
		name += ".exe"
	}
	return filepath.Join(filepath.Dir(python), name)
}

// CreateProject runs the kernel-owned fresh-project seed command with literal
// argv. It captures all child output and accepts success only when exit 0 emits
// exactly one expected JSON object. ctx cancellation or deadline expiry, signal
// termination, parser failures, and unexpected exit statuses are deliberately
// returned as transport outcomes rather than trusted create results.
func CreateProject(ctx context.Context, lingtaiCmd string, request ProjectCreateRequest) (ProjectCreateResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	cmd := exec.CommandContext(ctx, kernelCLI(lingtaiCmd),
		"project", "create",
		"--dir", request.Dir,
		"--name", request.AgentName,
		"--preset", request.Preset,
		"--covenant-file", request.CovenantFile,
		"--json",
	)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	stdoutText, stderrText := stdout.String(), stderr.String()

	if ctxErr := ctx.Err(); ctxErr != nil {
		reason := "cancelled"
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			reason = "timeout"
		}
		return ProjectCreateResult{}, &ProjectCreateError{
			Kind: ProjectCreateTransportFailure, Reason: reason, ExitCode: -1,
			Stdout: stdoutText, Stderr: stderrText, Cause: ctxErr,
		}
	}
	if runErr == nil {
		return parseProjectCreateSuccess(request.AgentName, stdoutText, stderrText)
	}

	exitErr, ok := runErr.(*exec.ExitError)
	if !ok {
		return ProjectCreateResult{}, &ProjectCreateError{
			Kind: ProjectCreateTransportFailure, Reason: "start_failed", ExitCode: -1,
			Stdout: stdoutText, Stderr: stderrText, Cause: runErr,
		}
	}
	exitCode := exitErr.ProcessState.ExitCode()
	if exitCode < 0 {
		return ProjectCreateResult{}, &ProjectCreateError{
			Kind: ProjectCreateTransportFailure, Reason: "signal", ExitCode: exitCode,
			Stdout: stdoutText, Stderr: stderrText, Cause: runErr,
		}
	}
	if exitCode == 1 {
		return parseProjectCreateHandlerError(exitCode, stdoutText, stderrText)
	}
	reason := "unexpected_exit"
	if exitCode == 2 {
		reason = "argument_error"
	}
	return ProjectCreateResult{}, &ProjectCreateError{
		Kind: ProjectCreateTransportFailure, Reason: reason, ExitCode: exitCode,
		Stdout: stdoutText, Stderr: stderrText, Cause: runErr,
	}
}

func parseProjectCreateSuccess(expectedAgentName, stdout, stderr string) (ProjectCreateResult, error) {
	var response struct {
		Status    string `json:"status"`
		AgentName string `json:"agent_name"`
		PresetRef string `json:"preset_ref"`
	}
	if err := decodeProjectCreateJSONObject([]byte(stdout), &response); err != nil {
		return ProjectCreateResult{}, malformedProjectCreateError(0, stdout, stderr, err)
	}
	if response.Status != "created" || response.AgentName != expectedAgentName || response.PresetRef == "" || !filepath.IsAbs(response.PresetRef) {
		return ProjectCreateResult{}, malformedProjectCreateError(0, stdout, stderr,
			fmt.Errorf("unexpected success status %q, agent name %q, or canonical preset reference %q", response.Status, response.AgentName, response.PresetRef))
	}
	return ProjectCreateResult{
		Status: response.Status, AgentName: response.AgentName, PresetRef: response.PresetRef,
		Stdout: stdout, Stderr: stderr, ExitCode: 0,
	}, nil
}

func parseProjectCreateHandlerError(exitCode int, stdout, stderr string) (ProjectCreateResult, error) {
	var response struct {
		Status  string `json:"status"`
		Code    string `json:"code"`
		Message string `json:"error"`
	}
	if err := decodeProjectCreateJSONObject([]byte(stderr), &response); err != nil {
		return ProjectCreateResult{}, malformedProjectCreateError(exitCode, stdout, stderr, err)
	}
	if response.Status != "error" || response.Code == "" || response.Message == "" {
		return ProjectCreateResult{}, malformedProjectCreateError(exitCode, stdout, stderr,
			fmt.Errorf("unexpected handler error response"))
	}
	return ProjectCreateResult{}, &ProjectCreateError{
		Kind: ProjectCreateHandlerFailure, Reason: "handler_error", Code: response.Code,
		Message: response.Message, ExitCode: exitCode, Stdout: stdout, Stderr: stderr,
	}
}

func malformedProjectCreateError(exitCode int, stdout, stderr string, cause error) error {
	return &ProjectCreateError{
		Kind: ProjectCreateTransportFailure, Reason: "malformed_result", ExitCode: exitCode,
		Stdout: stdout, Stderr: stderr, Cause: cause,
	}
}

func decodeProjectCreateJSONObject(data []byte, target interface{}) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || trimmed[0] != '{' {
		return errors.New("expected one JSON object")
	}
	if err := json.Unmarshal(trimmed, target); err != nil {
		return fmt.Errorf("parse JSON: %w", err)
	}
	return nil
}

// LaunchAgent starts an agent process. lingtaiCmd is the global fallback Python;
// the agent's init.json venv_path is tried first.
//
// Refuses to launch if another `lingtai-agent run <agentDir>` process is already
// alive on this machine — this prevents the suspend→relaunch race where the
// previous interpreter is still tearing down. Returns ErrAgentAlreadyRunning
// in that case. The kernel's flock guarantees correctness of on-disk state,
// but a duplicate Python process still shows up in `ps`/`lingtai-tui list`
// and can mislead users; this guard keeps the process accounting honest.
func LaunchAgent(lingtaiCmd, agentDir string) (*exec.Cmd, error) {
	if IsAgentRunning(agentDir) {
		return nil, ErrAgentAlreadyRunning
	}
	return launchAgentUnsafe(lingtaiCmd, agentDir)
}

// ForceLaunchAgent launches an agent without consulting IsAgentRunning. It
// exists for /refresh, which has already done the work of terminating any
// lingering interpreter and explicitly wants to bypass duplicate-launch
// protection. Non-refresh callers must keep using LaunchAgent — the gate in
// LaunchAgent is the only thing preventing two interpreters from racing on
// the same agent dir during the suspend→relaunch window.
func ForceLaunchAgent(lingtaiCmd, agentDir string) (*exec.Cmd, error) {
	return launchAgentUnsafe(lingtaiCmd, agentDir)
}

func launchAgentUnsafe(lingtaiCmd, agentDir string) (*exec.Cmd, error) {
	fs.CleanSignals(agentDir)
	python := resolvePython(agentDir, lingtaiCmd)

	// Verify required addons are importable before launch
	if err := config.EnsureAddons(python, agentDir); err != nil {
		return nil, fmt.Errorf("ensure addons: %w", err)
	}

	cmd := exec.Command(kernelCLI(python), "run", agentDir)
	// Redirect agent output to a log file instead of the TUI terminal
	logPath := filepath.Join(agentDir, "logs")
	if err := os.MkdirAll(logPath, 0o755); err != nil {
		return nil, fmt.Errorf("create log dir: %w", err)
	}
	logFile, err := os.OpenFile(filepath.Join(logPath, "agent.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}

	if err := cmd.Start(); err != nil {
		if logFile != nil {
			_ = logFile.Close()
		}
		return nil, fmt.Errorf("launch agent: %w", err)
	}
	// The child inherited its own copy of the descriptor for stdout/stderr;
	// close the parent's copy so repeated launches and /refresh cycles do
	// not accumulate open handles to agent.log.
	if logFile != nil {
		_ = logFile.Close()
	}
	return cmd, nil
}
