package process

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
	"reflect"
	"regexp"
	"strings"
	"time"
)

// InteractiveProjectRequest is deliberately not a wizard draft. Only reviewed,
// non-secret creation inputs cross this boundary; credentials stay in Go.
type InteractiveProjectRequest struct {
	ProjectDir      string                    `json:"project_dir"`
	ApprovedRoot    string                    `json:"approved_root"`
	AgentName       string                    `json:"agent_name"`
	DirectoryName   string                    `json:"directory_name"`
	PresetRef       string                    `json:"preset_ref"`
	PresetManifest  map[string]interface{}    `json:"preset_manifest"`
	Options         InteractiveProjectOptions `json:"options"`
	Recipe          string                    `json:"recipe"`
	ProcessEvidence string                    `json:"process_evidence"`
	Location        string                    `json:"location,omitempty"`
	Commands        string                    `json:"commands,omitempty"`
}

type InteractiveProjectOptions struct {
	Language       string                 `json:"language"`
	Covenant       string                 `json:"covenant"`
	Karma          bool                   `json:"karma"`
	Nirvana        bool                   `json:"nirvana"`
	Pad            string                 `json:"pad,omitempty"`
	BasePrompt     string                 `json:"base_prompt,omitempty"`
	EnvFile        string                 `json:"env_file,omitempty"`
	VenvPath       string                 `json:"venv_path,omitempty"`
	Addons         []string               `json:"addons,omitempty"`
	MCP            map[string]interface{} `json:"mcp,omitempty"`
	AllowedPresets []string               `json:"allowed_presets,omitempty"`
}

type InteractiveProjectResult struct {
	Status        string   `json:"status"`
	Committed     *bool    `json:"committed"`
	AttemptID     string   `json:"attempt_id"`
	AgentName     string   `json:"agent_name"`
	DirectoryName string   `json:"directory_name"`
	PresetRef     string   `json:"preset_ref"`
	Warnings      []string `json:"warnings"`
}

// ProjectOperationError never retains arbitrary child output/input. Known is
// true only for the documented single error envelope AND exit 1. Otherwise the
// publication outcome is unknown, requiring inspection, not replay or deletion.
type ProjectOperationError struct {
	Known bool
	Code  string
}

func (e *ProjectOperationError) Error() string {
	if e.Known {
		return "kernel Project operation refused (" + e.Code + "); external prerequisite writes may remain"
	}
	return "kernel Project outcome unknown; inspect the approved directory before any new attempt. This operation requires an installed kernel supporting project inspect/create-interactive; use the normal installer explicitly if unsupported (no automatic update or legacy fallback)"
}
func unknownProjectOperation() error { return &ProjectOperationError{} }

const projectLineLimit = 1 << 20

// boundedProjectOutput drains stderr without letting a noisy/old child allocate
// unbounded memory. Overflow is an unknown protocol outcome, never a refusal.
type boundedProjectOutput struct {
	bytes.Buffer
	overflow bool
	limit    int
}

func (b *boundedProjectOutput) Write(p []byte) (int, error) {
	n := len(p)
	limit := b.limit
	if limit == 0 {
		limit = 16384
	}
	remaining := limit - b.Len()
	if n > remaining {
		b.overflow = true
		p = p[:remaining]
	}
	_, _ = b.Buffer.Write(p)
	return n, nil
}

// strictProjectObject rejects duplicate keys (also nested), unknown fields and
// trailing values. UseNumber prevents loss of shared-field integer precision.
func strictProjectObject(data []byte, target interface{}) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var walk func() error
	walk = func() error {
		t, err := d.Token()
		if err != nil {
			return err
		}
		delim, ok := t.(json.Delim)
		if !ok {
			return nil
		}
		switch delim {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := k.(string)
				if !ok || seen[key] {
					return errors.New("duplicate key")
				}
				seen[key] = true
				if err := walk(); err != nil {
					return err
				}
			}
		case '[':
			for d.More() {
				if err := walk(); err != nil {
					return err
				}
			}
		default:
			return errors.New("invalid JSON")
		}
		_, err = d.Token()
		return err
	}
	if len(bytes.TrimSpace(data)) == 0 || bytes.TrimSpace(data)[0] != '{' {
		return errors.New("expected object")
	}
	if err := walk(); err != nil {
		return err
	}
	if _, err := d.Token(); err != io.EOF {
		return errors.New("extra JSON")
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(data, &fields) != nil {
		return errors.New("invalid object")
	}
	typ := reflect.TypeOf(target).Elem()
	for i := 0; i < typ.NumField(); i++ {
		key := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
		if raw, ok := fields[key]; !ok || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
			return errors.New("missing or null field")
		}
	}
	d = json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	d.DisallowUnknownFields()
	return d.Decode(target)
}

func knownProjectFailure(stderr *boundedProjectOutput, interactive bool) error {
	if stderr.overflow {
		return unknownProjectOperation()
	}
	var e struct {
		Status    string `json:"status"`
		Committed *bool  `json:"committed"`
		Code      string `json:"code"`
		Message   string `json:"error"`
	}
	if interactive {
		if strictProjectObject(stderr.Bytes(), &e) != nil || e.Committed == nil || *e.Committed {
			return unknownProjectOperation()
		}
	} else {
		var inspected struct {
			Status  string `json:"status"`
			Code    string `json:"code"`
			Message string `json:"error"`
		}
		if strictProjectObject(stderr.Bytes(), &inspected) != nil {
			return unknownProjectOperation()
		}
		e.Status, e.Code, e.Message = inspected.Status, inspected.Code, inspected.Message
	}
	if e.Status != "error" || e.Code == "" || e.Message == "" || len(e.Message) > 4096 {
		return unknownProjectOperation()
	}
	// Codes are displayed, not raw child messages; require bounded identifiers.
	if len(e.Code) > 100 || strings.Trim(e.Code, "abcdefghijklmnopqrstuvwxyz0123456789_") != "" {
		return unknownProjectOperation()
	}
	return &ProjectOperationError{Known: true, Code: e.Code}
}

// CreateInteractiveProject uses one live pipe and exactly one attempt. The
// callback runs only after actual kernel staged-reader readiness, never before.
// A false callback result is acknowledged without exposing credential errors.
func CreateInteractiveProject(ctx context.Context, python string, request InteractiveProjectRequest, prerequisites func() bool) (InteractiveProjectResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	payload, err := json.Marshal(request)
	if err != nil || len(payload)+1 > projectLineLimit || !publicProjectInput(payload) {
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	cmd := exec.CommandContext(ctx, kernelCLI(python), "project", "create-interactive")
	// WaitDelay also bounds an inherited pipe after the direct child exits.
	cmd.WaitDelay = time.Second
	in, err := cmd.StdinPipe()
	if err != nil {
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	var stderr boundedProjectOutput
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		_ = in.Close()
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	abort := func() (InteractiveProjectResult, error) {
		cancel()
		_ = in.Close()
		_ = cmd.Wait()
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	// A context deadline must also release a stdout pipe inherited by a stray
	// descendant; waiting on Scanner before Wait cannot rely on WaitDelay alone.
	stopClosing := context.AfterFunc(ctx, func() { _ = out.Close(); _ = in.Close() })
	defer stopClosing()
	if _, err := in.Write(append(payload, '\n')); err != nil {
		return abort()
	}
	scan := bufio.NewScanner(out)
	scan.Buffer(make([]byte, 4096), projectLineLimit+1)
	scan.Split(projectExchangeLine)
	if !scan.Scan() {
		scanErr := scan.Err()
		_ = in.Close()
		runErr := cmd.Wait()
		var exit *exec.ExitError
		if scanErr == nil && ctx.Err() == nil && errors.As(runErr, &exit) && exit.ExitCode() == 1 {
			return InteractiveProjectResult{}, knownProjectFailure(&stderr, true)
		}
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	var ready struct {
		Status    string `json:"status"`
		Committed *bool  `json:"committed"`
		AttemptID string `json:"attempt_id"`
		PresetRef string `json:"preset_ref"`
	}
	if strictProjectObject(scan.Bytes(), &ready) != nil || ready.Status != "ready_for_prerequisites" || ready.Committed == nil || *ready.Committed || ready.AttemptID == "" || len(ready.AttemptID) > 256 || ready.PresetRef != request.PresetRef {
		return abort()
	}
	// Cancellation before readiness acceptance must not persist credentials.
	if ctx.Err() != nil {
		return abort()
	}
	accepted := prerequisites != nil && prerequisites()
	ack := struct {
		Status    string `json:"status"`
		AttemptID string `json:"attempt_id"`
		PresetRef string `json:"preset_ref"`
		Accepted  bool   `json:"accepted"`
	}{"prerequisites_acknowledged", ready.AttemptID, ready.PresetRef, accepted}
	if err := json.NewEncoder(in).Encode(ack); err != nil {
		return abort()
	}
	if err := in.Close(); err != nil {
		return abort()
	}
	var result InteractiveProjectResult
	terminal := scan.Scan()
	if terminal && strictProjectObject(scan.Bytes(), &result) != nil {
		return abort()
	}
	if scan.Scan() {
		return abort()
	}
	scanErr := scan.Err()
	runErr := cmd.Wait()
	if ctx.Err() != nil || scanErr != nil {
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	if runErr != nil {
		var exit *exec.ExitError
		if !terminal && errors.As(runErr, &exit) && exit.ExitCode() == 1 {
			return InteractiveProjectResult{}, knownProjectFailure(&stderr, true)
		}
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	if !accepted || !terminal || stderr.overflow || stderr.Len() != 0 || result.Status != "created" || result.Committed == nil || !*result.Committed || result.AttemptID != ready.AttemptID || result.AgentName != request.AgentName || result.DirectoryName != request.DirectoryName || result.PresetRef != request.PresetRef || result.Warnings == nil {
		return InteractiveProjectResult{}, unknownProjectOperation()
	}
	return result, nil
}

type ProjectMember struct {
	DirectoryName string `json:"directory_name"`
	ManifestKind  string `json:"manifest_kind"`
	Role          string `json:"role"`
	AgentName     string `json:"agent_name"`
	Address       string `json:"address"`
	AgentID       string `json:"agent_id"`
}
type ProjectInspection struct {
	Status           string                  `json:"status"`
	Initialized      *bool                   `json:"initialized"`
	Members          []ProjectMember         `json:"members"`
	StartupReadiness ProjectStartupReadiness `json:"startup_readiness"`
}

// InspectProject is read-only evidence, never a skeleton initializer or a
// runtime/liveness assertion. Ordinary callers retain EOF/capped transport.
func InspectProject(ctx context.Context, python, root string) (ProjectInspection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	input, err := json.Marshal(struct {
		ProjectDir string `json:"project_dir"`
	}{root})
	if err != nil {
		return ProjectInspection{}, unknownProjectOperation()
	}
	cmd := exec.CommandContext(ctx, kernelCLI(python), "project", "inspect")
	cmd.WaitDelay = time.Second
	cmd.Stdin = bytes.NewReader(append(input, '\n'))
	var stderr boundedProjectOutput
	stdout := boundedProjectOutput{limit: projectLineLimit}
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	if ctx.Err() != nil || stdout.overflow {
		return ProjectInspection{}, unknownProjectOperation()
	}
	if runErr != nil {
		var exit *exec.ExitError
		if stdout.Len() == 0 && errors.As(runErr, &exit) && exit.ExitCode() == 1 {
			return ProjectInspection{}, knownProjectFailure(&stderr, false)
		}
		return ProjectInspection{}, unknownProjectOperation()
	}
	if stderr.overflow || stderr.Len() != 0 {
		return ProjectInspection{}, unknownProjectOperation()
	}
	result, err := decodeProjectInspection(stdout.Bytes(), false, nil)
	if err != nil {
		return ProjectInspection{}, err
	}
	return result, nil
}

// Same classifier as the kernel's existing exchange boundary. Unknown public
// extensions are retained; credential-shaped fields reject before child start.
var projectSecretKey = regexp.MustCompile(`(^|_)(api_?key|secret|secrets|password|passwd|credential|credentials|private_key|access_key|token)(_|$)`)
var projectKeyPunctuation = regexp.MustCompile(`[^a-z0-9]+`)
var projectEnvIdentifier = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func publicProjectInput(data []byte) bool {
	var value interface{}
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	if d.Decode(&value) != nil {
		return false
	}
	var public func(interface{}) bool
	public = func(item interface{}) bool {
		switch v := item.(type) {
		case map[string]interface{}:
			for key, child := range v {
				lower := strings.ToLower(key)
				normalized := strings.Trim(projectKeyPunctuation.ReplaceAllString(lower, "_"), "_")
				compact := projectKeyPunctuation.ReplaceAllString(lower, "")
				secret := projectSecretKey.MatchString(normalized) || compact == "apikey" || compact == "password" || compact == "passwd" || compact == "credential" || compact == "credentials" || compact == "privatekey" || compact == "accesskey" || strings.HasSuffix(compact, "secret") || strings.HasSuffix(compact, "token")
				if secret {
					name, ok := child.(string)
					if !strings.HasSuffix(key, "_env") || !ok || !projectEnvIdentifier.MatchString(name) {
						return false
					}
				}
				if !public(child) {
					return false
				}
			}
		case []interface{}:
			for _, child := range v {
				if !public(child) {
					return false
				}
			}
		}
		return true
	}
	return public(value)
}

func projectExchangeLine(data []byte, atEOF bool) (int, []byte, error) {
	if i := bytes.IndexByte(data, '\n'); i >= 0 {
		return i + 1, data[:i], nil
	}
	if atEOF && len(data) != 0 {
		return 0, nil, io.ErrUnexpectedEOF
	}
	return 0, nil, nil
}
