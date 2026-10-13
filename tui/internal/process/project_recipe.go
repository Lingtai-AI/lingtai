package process

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"syscall"
)

// ProjectRecipeFailure is factual native evidence, not a Python exception string.
// Operation is the adapter phase operation, not necessarily an exact Go syscall.
type ProjectRecipeFailure struct {
	Phase       string `json:"phase"`
	Operation   string `json:"operation"`
	PathB64     string `json:"path_b64"`
	Errno       *int   `json:"errno"`
	Winerror    *int   `json:"winerror"`
	ParseOffset *int64 `json:"parse_offset"`
}

func (f *ProjectRecipeFailure) Path() string {
	data, _ := recipeBytes(f.PathB64)
	return string(data)
}

// NativeError retains errno matching where the platform and receipt prove it.
// Decode/normalize failures intentionally do not invent a json.SyntaxError,
// decoder message, or arbitrary exception text from insufficient evidence.
func (f *ProjectRecipeFailure) NativeError() error {
	if f.Errno != nil && *f.Errno > 0 {
		if f.Winerror != nil && *f.Winerror > 0 {
			if runtime.GOOS == "windows" {
				return &os.PathError{Op: f.Operation, Path: f.Path(), Err: syscall.Errno(*f.Winerror)}
			}
			return &ProjectRecipeNativeFailure{Failure: *f}
		}
		return &os.PathError{Op: f.Operation, Path: f.Path(), Err: syscall.Errno(*f.Errno)}
	}
	return &ProjectRecipeNativeFailure{Failure: *f}
}

type ProjectRecipeNativeFailure struct{ Failure ProjectRecipeFailure }

func (e *ProjectRecipeNativeFailure) Error() string {
	return fmt.Sprintf("kernel Recipe %s failure (%s); exact native cause unavailable", e.Failure.Phase, e.Failure.Operation)
}

type ProjectRecipeMember struct {
	DirectoryName    string `json:"directory_name"` // display only; NEVER a map key
	DirectoryNameB64 string `json:"directory_name_b64"`
	PromptWritten    bool   `json:"prompt_written"`
	InitWritten      bool   `json:"init_written"`
	ReaderStatus     string `json:"reader_status"`
	Code             string `json:"code"`
	LegacyCounted    bool   `json:"legacy_counted"`
	PromptCode       string `json:"prompt_code"`
	InitCode         string `json:"init_code"`
}

type ProjectRecipeSnapshot struct {
	Attempted        bool   `json:"attempted"`
	RemovalSucceeded bool   `json:"removal_succeeded"`
	Copied           bool   `json:"copied"`
	Code             string `json:"code"`
}

type ProjectRecipeResult struct {
	Status        string                `json:"status"`
	Outcome       string                `json:"outcome"`
	Members       []ProjectRecipeMember `json:"members"`
	Snapshot      ProjectRecipeSnapshot `json:"snapshot"`
	GreetStatus   string                `json:"greet_status"`
	Applied       int                   `json:"applied"`
	LegacyApplied int                   `json:"legacy_applied"`
	Failed        int                   `json:"failed"`
	Code          string                `json:"code"`
	Message       string                `json:"error"`
	LegacyFailure *ProjectRecipeFailure `json:"legacy_failure"`
	// Only stopped terminals contain these supplementary fields.
	LegacyCode              string `json:"-"`
	PendingSequence         int    `json:"-"`
	PendingDirectoryNameB64 string `json:"-"`
}

type ProjectRecipeError struct {
	Result ProjectRecipeResult
	Known  bool
}

func (e *ProjectRecipeError) Error() string {
	if !e.Known {
		return "kernel Recipe outcome unknown; prior writes and callback side effects may remain; inspect before another action (no automatic retry or Go fallback)"
	}
	return fmt.Sprintf("kernel Recipe %s (%s); legacy count=%d; prior writes remain", e.Result.Outcome, e.Result.Code, e.Result.LegacyApplied)
}
func (e *ProjectRecipeError) Unwrap() error {
	if e.Known && e.Result.LegacyFailure != nil {
		return e.Result.LegacyFailure.NativeError()
	}
	return nil
}
func unknownRecipe() (ProjectRecipeResult, error) {
	r := ProjectRecipeResult{Outcome: "unknown"}
	return r, &ProjectRecipeError{Result: r}
}

// recipeObject is a specialized closed-object decoder. Unlike configuration's
// decoder it admits only the ABI's explicitly nullable native failure fields.
func recipeObject(data []byte, target interface{}, fields string, nullable string) (map[string]json.RawMessage, error) {
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
				t, err := d.Token()
				if err != nil {
					return err
				}
				key, ok := t.(string)
				if !ok || seen[key] {
					return errors.New("duplicate Recipe key")
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
			return errors.New("invalid Recipe JSON")
		}
		_, err = d.Token()
		return err
	}
	trim := bytes.TrimSpace(data)
	if len(trim) == 0 || trim[0] != '{' {
		return nil, errors.New("Recipe object required")
	}
	if err := walk(); err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("extra Recipe JSON")
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	wanted := bytes.Fields([]byte(fields))
	if len(raw) != len(wanted) {
		return nil, errors.New("Recipe fields")
	}
	for _, field := range wanted {
		value, ok := raw[string(field)]
		if !ok || (bytes.Equal(bytes.TrimSpace(value), []byte("null")) && !bytes.Contains([]byte(" "+nullable+" "), []byte(" "+string(field)+" "))) {
			return nil, errors.New("missing/null Recipe field")
		}
	}
	if target != nil {
		d = json.NewDecoder(bytes.NewReader(data))
		d.DisallowUnknownFields()
		if err := d.Decode(target); err != nil {
			return nil, err
		}
	}
	return raw, nil
}

func recipeBytes(text string) ([]byte, error) {
	data, err := base64.StdEncoding.Strict().DecodeString(text)
	if err != nil || base64.StdEncoding.EncodeToString(data) != text {
		return nil, errors.New("invalid Recipe base64")
	}
	return data, nil
}
func recipeCode(code string) bool {
	switch code {
	case "", "invalid_project_request", "credentials_must_remain_outer", "invalid_recipe", "recipe_enumeration_failed", "recipe_prompt_write_failed", "recipe_skills_read_failed", "recipe_init_write_failed", "recipe_reader_failed", "recipe_snapshot_copy_failed", "recipe_request_failed", "recipe_exchange_failed":
		return true
	}
	return false
}

const recipeTerminalFields = "status outcome members snapshot greet_status applied legacy_applied failed code error legacy_failure"

func decodeRecipeResult(data []byte) (ProjectRecipeResult, error) {
	var result ProjectRecipeResult
	var tag struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(data, &tag) != nil {
		return result, errors.New("Recipe terminal")
	}
	fields := recipeTerminalFields
	stopped := tag.Status == "recipe_exchange_stopped"
	if stopped {
		fields += " legacy_code pending_sequence pending_directory_name_b64"
	}
	raw, err := recipeObject(data, nil, fields, "legacy_failure")
	if err != nil {
		return result, err
	}
	ordinary := map[string]json.RawMessage{}
	for key, value := range raw {
		if key != "legacy_code" && key != "pending_sequence" && key != "pending_directory_name_b64" {
			ordinary[key] = value
		}
	}
	plain, err := json.Marshal(ordinary)
	if err != nil {
		return result, err
	}
	if _, err = recipeObject(plain, &result, recipeTerminalFields, "legacy_failure"); err != nil {
		return result, err
	}
	if _, err = recipeObject(raw["snapshot"], &result.Snapshot, "attempted removal_succeeded copied code", ""); err != nil {
		return result, err
	}
	if result.LegacyFailure != nil {
		f := result.LegacyFailure
		if _, err = recipeObject(raw["legacy_failure"], f, "phase operation path_b64 errno winerror parse_offset", "errno winerror parse_offset"); err != nil {
			return result, err
		}
		if _, err = recipeBytes(f.PathB64); err != nil || f.PathB64 == "" {
			return result, errors.New("Recipe failure path")
		}
		if (f.Errno != nil && *f.Errno < 0) || (f.Winerror != nil && *f.Winerror < 0) || (f.ParseOffset != nil && *f.ParseOffset < 0) {
			return result, errors.New("Recipe failure number")
		}
		switch f.Phase {
		case "metadata", "enumeration", "prompt", "skills", "snapshot":
		default:
			return result, errors.New("Recipe legacy phase")
		}
		switch f.Operation {
		case "read", "decode", "readdir", "write", "normalize", "copy":
		default:
			return result, errors.New("Recipe native operation")
		}
	}
	var members []json.RawMessage
	if json.Unmarshal(raw["members"], &members) != nil {
		return result, errors.New("Recipe members")
	}
	actual, legacy, failed := 0, 0, 0
	legacyFirst := ""
	firstPhase := ""
	diagnosticFailure := false
	identities := map[string]bool{}
	var previousIdentity []byte
	for i, member := range members {
		m := &result.Members[i]
		if _, err = recipeObject(member, m, "directory_name directory_name_b64 prompt_written init_written reader_status code legacy_counted prompt_code init_code", ""); err != nil {
			return result, err
		}
		identity, err := recipeBytes(m.DirectoryNameB64)
		if err != nil || len(identity) == 0 || identities[string(identity)] {
			return result, errors.New("Recipe member identity")
		}
		if identity[0] == '.' || bytes.Equal(identity, []byte("human")) || (previousIdentity != nil && bytes.Compare(previousIdentity, identity) >= 0) {
			return result, errors.New("Recipe member order/eligibility")
		}
		previousIdentity = identity
		identities[string(identity)] = true
		switch m.ReaderStatus {
		case "not_read", "READ_FAILED", "FULLY_EFFECTIVE", "READ_OK_WITH_IGNORED_FIELDS":
		default:
			return result, errors.New("Recipe reader status")
		}
		if !recipeCode(m.Code) || !recipeCode(m.PromptCode) || !recipeCode(m.InitCode) {
			return result, errors.New("Recipe member code")
		}
		if m.PromptCode != "" && m.PromptCode != "recipe_prompt_write_failed" {
			return result, errors.New("Recipe prompt code")
		}
		if m.InitCode != "" && m.InitCode != "recipe_skills_read_failed" && m.InitCode != "recipe_init_write_failed" && m.InitCode != "recipe_reader_failed" {
			return result, errors.New("Recipe init code")
		}
		first := m.PromptCode
		if first == "" {
			first = m.InitCode
		}
		if m.Code != first || (m.InitCode == "recipe_reader_failed" && (!m.InitWritten || m.ReaderStatus != "READ_FAILED")) || (!m.InitWritten && m.ReaderStatus != "not_read") {
			return result, errors.New("Recipe member evidence")
		}
		expectedCount := true
		if legacyFirst == "" && m.PromptCode != "" {
			legacyFirst, firstPhase, expectedCount = m.PromptCode, "prompt", false
			if m.InitWritten || m.InitCode != "" {
				return result, errors.New("Recipe first prompt continuation")
			}
		} else if legacyFirst == "" && m.InitCode != "" && m.InitCode != "recipe_reader_failed" {
			legacyFirst, firstPhase, expectedCount = m.InitCode, "skills", false
		}
		if m.LegacyCounted != expectedCount || (m.PromptWritten && m.PromptCode != "") {
			return result, errors.New("Recipe legacy counting")
		}
		diagnosticFailure = diagnosticFailure || m.Code != ""
		if m.Code == "" {
			actual++
		} else {
			failed++
		}
		if m.LegacyCounted {
			legacy++
		}
	}
	if result.Applied != actual || result.LegacyApplied != legacy || result.Failed != failed || !recipeCode(result.Code) {
		return result, errors.New("Recipe terminal counts/code")
	}
	s := result.Snapshot
	if (s.Copied || s.RemovalSucceeded || s.Code != "") && !s.Attempted {
		return result, errors.New("Recipe snapshot attempt")
	}
	if (s.Code != "" && s.Code != "recipe_snapshot_copy_failed") || (s.Copied && s.Code != "") {
		return result, errors.New("Recipe snapshot code")
	}
	switch result.GreetStatus {
	case "absent", "empty", "selected", "unreadable":
	default:
		return result, errors.New("Recipe greet status")
	}
	for _, m := range result.Members {
		if (m.PromptWritten || m.PromptCode != "") != (result.GreetStatus == "selected") || (m.InitWritten && m.ReaderStatus == "not_read") || (m.InitWritten && m.InitCode != "" && m.InitCode != "recipe_reader_failed") {
			return result, errors.New("Recipe phase evidence")
		}
	}
	switch result.Outcome {
	case "complete", "partial", "rejected", "unknown":
	default:
		return result, errors.New("Recipe outcome")
	}
	if !stopped && s.Attempted {
		if !s.Copied && s.Code == "" {
			return result, errors.New("Recipe missing snapshot failure")
		}
		if legacyFirst == "" && s.Code != "" {
			legacyFirst, firstPhase = s.Code, "snapshot"
		}
		if result.Code != legacyFirst {
			return result, errors.New("Recipe first failure code")
		}
		if (diagnosticFailure || s.Code != "") != (result.Outcome == "partial") {
			return result, errors.New("Recipe traversal outcome")
		}
	} else if !stopped {
		if len(result.Members) != 0 || s.RemovalSucceeded || s.Copied || result.LegacyFailure == nil && result.Code != "invalid_project_request" && result.Code != "credentials_must_remain_outer" && result.Code != "recipe_request_failed" {
			return result, errors.New("Recipe pretraversal evidence")
		}
		if result.Code == "invalid_recipe" {
			firstPhase = "metadata"
		} else if result.Code == "recipe_enumeration_failed" {
			firstPhase = "enumeration"
		} else if result.Code != "invalid_project_request" && result.Code != "credentials_must_remain_outer" && result.Code != "recipe_request_failed" {
			return result, errors.New("Recipe pretraversal code")
		}
		if result.Outcome != "rejected" && !(result.Outcome == "unknown" && result.Code == "recipe_request_failed") {
			return result, errors.New("Recipe pretraversal outcome")
		}
	}
	if firstPhase != "" && (result.LegacyFailure == nil || result.LegacyFailure.Phase != firstPhase) {
		return result, errors.New("Recipe legacy failure evidence")
	}
	if stopped {
		if json.Unmarshal(raw["legacy_code"], &result.LegacyCode) != nil || json.Unmarshal(raw["pending_sequence"], &result.PendingSequence) != nil || json.Unmarshal(raw["pending_directory_name_b64"], &result.PendingDirectoryNameB64) != nil {
			return result, errors.New("Recipe stopped fields")
		}
		if result.LegacyCode != legacyFirst || (legacyFirst == "" && result.LegacyFailure != nil) {
			return result, errors.New("Recipe stopped legacy failure")
		}
		identity, err := recipeBytes(result.PendingDirectoryNameB64)
		if err != nil || len(identity) == 0 || identities[string(identity)] || result.PendingSequence < 1 || !recipeCode(result.LegacyCode) || result.Code != "recipe_exchange_failed" || result.Message != "Project Recipe rendering exchange stopped" || s.Attempted {
			return result, errors.New("Recipe stopped binding")
		}
		written := false
		for _, m := range result.Members {
			written = written || m.PromptWritten || m.InitWritten
		}
		if (written && result.Outcome != "partial") || (!written && result.Outcome != "rejected") {
			return result, errors.New("Recipe stopped outcome")
		}
	} else if result.Code == "" {
		if result.Status != "recipe_applied" || result.Message != "" || (result.Outcome != "complete" && result.Outcome != "partial") || !s.Attempted || !s.Copied || result.LegacyFailure != nil {
			return result, errors.New("Recipe success envelope")
		}
	} else if result.Status != "error" || result.Message != "Project Recipe operation could not complete" || result.Outcome == "complete" {
		return result, errors.New("Recipe error envelope")
	}
	return result, nil
}

// ApplyProjectRecipe executes the original function synchronously on the calling
// goroutine, once immediately before each kernel prompt phase. No deadlines,
// Scanner/output caps, batching, replay, Go writes or recovery are added here.
func ApplyProjectRecipe(python, bundleDir, lingtaiDir, language string, render func(string) string) (ProjectRecipeResult, error) {
	mode := "identity"
	if render != nil {
		mode = "callback"
	}
	request := struct {
		ProjectDir    string `json:"project_dir"`
		LingtaiDir    string `json:"lingtai_dir"`
		Language      string `json:"language"`
		RenderingMode string `json:"rendering_mode"`
	}{bundleDir, lingtaiDir, language, mode}
	cmd := exec.Command(kernelCLI(python), "project", "recipe-apply")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return unknownRecipe()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return unknownRecipe()
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr // standard exec lifecycle drains concurrently; no cap
	if cmd.Start() != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return unknownRecipe()
	}
	finished := false
	defer func() {
		_ = stdin.Close()
		if !finished {
			// Includes panic and runtime.Goexit unwind. Never recover the value,
			// send a substitute ack, run another callback, or perform snapshot.
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = stdout.Close()
	}()
	writer := bufio.NewWriter(stdin)
	send := func(value interface{}) error {
		if err := json.NewEncoder(writer).Encode(value); err != nil {
			return err
		}
		return writer.Flush()
	}
	if send(request) != nil {
		return unknownRecipe()
	}
	reader := bufio.NewReader(stdout)
	sequence := 0
	var callbackNames []string
	var originalTemplate []byte
	var terminal []byte
	for {
		line, readErr := reader.ReadBytes('\n') // grows beyond 1 MiB
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil || terminal != nil {
			return unknownRecipe()
		}
		var tag struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(line, &tag) != nil {
			return unknownRecipe()
		}
		if tag.Status != "render_recipe_member" {
			// Validate BEFORE reading again: an unknown frame might itself be
			// waiting for input. Do not turn malformed framing into a hang.
			if _, err := decodeRecipeResult(line); err != nil || tag.Status != "recipe_applied" {
				return unknownRecipe()
			}
			terminal = line
			continue
		}
		if render == nil {
			return unknownRecipe()
		}
		var frame struct {
			Status           string `json:"status"`
			Sequence         int    `json:"sequence"`
			DirectoryNameB64 string `json:"directory_name_b64"`
			TemplateB64      string `json:"template_b64"`
			TemplateBytes    int    `json:"template_bytes"`
		}
		if _, err := recipeObject(line, &frame, "status sequence directory_name_b64 template_b64 template_bytes", ""); err != nil || frame.Status != "render_recipe_member" || frame.Sequence != sequence+1 {
			return unknownRecipe()
		}
		name, nameErr := recipeBytes(frame.DirectoryNameB64)
		template, templateErr := recipeBytes(frame.TemplateB64)
		if nameErr != nil || templateErr != nil || len(name) == 0 || len(template) == 0 || len(template) != frame.TemplateBytes {
			return unknownRecipe()
		}
		if len(callbackNames) > 0 && bytes.Compare([]byte(callbackNames[len(callbackNames)-1]), name) >= 0 {
			return unknownRecipe()
		}
		if name[0] == '.' || bytes.Equal(name, []byte("human")) || (originalTemplate != nil && !bytes.Equal(template, originalTemplate)) {
			return unknownRecipe()
		}
		if originalTemplate == nil {
			originalTemplate = template
		}
		sequence = frame.Sequence
		callbackNames = append(callbackNames, string(name))
		rendered := render(string(template)) // ORIGINAL Go function, current goroutine
		ack := struct {
			Status        string `json:"status"`
			Sequence      int    `json:"sequence"`
			RenderedB64   string `json:"rendered_b64"`
			RenderedBytes int    `json:"rendered_bytes"`
		}{"recipe_member_rendered", sequence, base64.StdEncoding.EncodeToString([]byte(rendered)), len(rendered)}
		if send(ack) != nil {
			return unknownRecipe()
		}
	}
	_ = stdin.Close()
	waitErr := cmd.Wait()
	finished = true
	if terminal != nil {
		if waitErr != nil || stderr.Len() != 0 {
			return unknownRecipe()
		}
	} else {
		var exit *exec.ExitError
		if !errors.As(waitErr, &exit) || exit.ExitCode() != 1 {
			return unknownRecipe()
		}
		terminal = stderr.Bytes()
		if len(terminal) == 0 || terminal[len(terminal)-1] != '\n' {
			return unknownRecipe()
		}
	}
	result, err := decodeRecipeResult(terminal)
	if err != nil || (waitErr == nil && result.Code != "") || (waitErr != nil && result.Code == "") {
		return unknownRecipe()
	}
	if render != nil && result.GreetStatus == "selected" {
		if result.Status == "recipe_exchange_stopped" {
			if result.PendingSequence != sequence || len(result.Members)+1 != len(callbackNames) || result.PendingDirectoryNameB64 != base64.StdEncoding.EncodeToString([]byte(callbackNames[len(callbackNames)-1])) {
				return unknownRecipe()
			}
		} else if len(result.Members) != len(callbackNames) {
			return unknownRecipe()
		}
		for i, m := range result.Members {
			if m.DirectoryNameB64 != base64.StdEncoding.EncodeToString([]byte(callbackNames[i])) {
				return unknownRecipe()
			}
		}
	} else if sequence != 0 {
		return unknownRecipe()
	}
	if result.Code == "" {
		return result, nil
	}
	known := result.Outcome != "unknown" && result.Code != "recipe_request_failed" && result.Code != "invalid_project_request"
	return result, &ProjectRecipeError{Result: result, Known: known}
}

type ProjectRecipeObservation struct {
	Status     string `json:"status"`
	State      string `json:"state"`
	NeedsApply bool   `json:"needs_apply"`
	Code       string `json:"code"`
	Message    string `json:"error"`
}

func ObserveProjectRecipe(python, root string) (ProjectRecipeObservation, error) {
	var result ProjectRecipeObservation
	request, err := json.Marshal(struct {
		ProjectDir string `json:"project_dir"`
	}{root})
	if err != nil {
		return result, &ProjectRecipeError{}
	}
	cmd := exec.Command(kernelCLI(python), "project", "recipe-observe")
	cmd.Stdin = bytes.NewReader(request) // old EOF request unchanged
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if cmd.Run() != nil || stderr.Len() != 0 {
		return result, &ProjectRecipeError{}
	}
	if _, err := recipeObject(stdout.Bytes(), &result, "status state needs_apply code error", ""); err != nil || result.Status != "recipe_observed" || result.Code != "" || result.Message != "" {
		return result, &ProjectRecipeError{}
	}
	switch result.State {
	case "absent", "equal":
		if result.NeedsApply {
			return result, &ProjectRecipeError{}
		}
	case "changed":
		if !result.NeedsApply {
			return result, &ProjectRecipeError{}
		}
	case "read_failure": // kernel carries the exact legacy boolean
	default:
		return result, &ProjectRecipeError{}
	}
	return result, nil
}
