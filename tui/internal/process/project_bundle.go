package process

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os/exec"
	"path"
	"reflect"
	"sort"
	"strings"
)

// ProjectBundleRequest binds native strings for one operation. No JSON path
// labels, normalized paths or Python filesystem operations identify these roots.
type ProjectBundleRequest struct {
	Kind, Source, Destination, RecipeName, GlobalDir string
}

func bundleB64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func (r ProjectBundleRequest) binding() map[string]string {
	m := map[string]string{"kind": r.Kind, "destination_b64": bundleB64(r.Destination)}
	switch r.Kind {
	case "disk":
		m["source_b64"] = bundleB64(r.Source)
	case "packaged":
		m["recipe_name_b64"] = bundleB64(r.RecipeName)
	case "wizard":
		m["recipe_name_b64"] = bundleB64(r.RecipeName)
		m["global_dir_b64"] = bundleB64(r.GlobalDir)
	default:
		return nil
	}
	return m
}

type ProjectBundleMetadata struct {
	Name    string
	Library *string
}

// ProjectBundleNative is the concrete native phase boundary, not a callback to
// the whole old CopyBundle policy. Each method runs synchronously once, after
// its closed frame has been correlated. Native errors never cross into Python.
type ProjectBundleNative interface {
	ObserveLocalRecipe() string
	FindGlobal() bool
	LoadMetadata(source string) (ProjectBundleMetadata, error)
	AbsoluteLocations(source string) (*string, *string)
	RemoveRecipe() error
	CopyRecipe(source string) error
	ObserveLibrary(source, library string) string
	CopyLibrary(source, library string) error
	ObserveNetwork(source string) string
	CopyNetwork(source string) error
	PackagedCandidates() []string
	DecodePackagedMetadata(root string, content []byte) (ProjectBundleMetadata, error)
	PreparePackagedFile(relative string) error
	WritePackagedFile(relative string, content []byte) error
}

type ProjectBundleFailure struct {
	Sequence  int    `json:"sequence"`
	Operation string `json:"operation"`
}
type projectBundleReceipt struct {
	Sequence  int             `json:"sequence"`
	Operation string          `json:"operation"`
	Result    json.RawMessage `json:"result"`
}
type ProjectBundleResult struct {
	Status     string                 `json:"status"`
	Binding    map[string]string      `json:"binding"`
	Source     string                 `json:"source"`
	Outcome    string                 `json:"outcome"`
	Code       string                 `json:"code"`
	Failure    *ProjectBundleFailure  `json:"failure"`
	Transcript []projectBundleReceipt `json:"transcript"`
	// Original is exposed only after complete terminal/EOF/exit/stderr validation.
	Original error `json:"-"`
}

type ProjectBundleUnknownError struct{}

func (*ProjectBundleUnknownError) Error() string {
	return "kernel Recipe bundle outcome unknown; partial writes may remain; inspect before another action (no retry, fallback or rollback)"
}
func bundleUnknown() (ProjectBundleResult, error) {
	return ProjectBundleResult{}, &ProjectBundleUnknownError{}
}

// This is a wire member allowlist, NOT a resource reader/cache. The frozen
// installed catalog owns content and selection. Bind every operational member
// before native Join; do not impose confinement on operator disk/library paths.
func bundleCatalogMembers(root string) ([]string, bool) {
	roots := []string{"recipe_assets/recommended/adaptive", "recipe_assets/intrinsic/greeter", "recipe_assets/intrinsic/plain", "recipe_assets/examples/tutorial"}
	behavior := []string{".recipe/comment/comment.md", ".recipe/comment/wen/comment.md", ".recipe/comment/zh/comment.md", ".recipe/greet/greet.md", ".recipe/greet/wen/greet.md", ".recipe/greet/zh/greet.md", ".recipe/recipe.json"}
	var members []string
	present := root == "."
	for _, r := range roots {
		files := behavior
		if strings.HasSuffix(r, "/plain") {
			files = []string{".recipe/recipe.json"}
		}
		for _, rel := range files {
			full := r + "/" + rel
			for dir := path.Dir(full); dir != "."; dir = path.Dir(dir) {
				if dir == root {
					present = true
				}
			}
			if strings.HasPrefix(full, root+"/") {
				members = append(members, strings.TrimPrefix(full, root+"/"))
			}
		}
	}
	// Match WalkDir's component-wise depth-first order, not full-path sorting.
	sort.Slice(members, func(i, j int) bool {
		a, b := strings.Split(members[i], "/"), strings.Split(members[j], "/")
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				return a[k] < b[k]
			}
		}
		return len(a) < len(b)
	})
	return members, present
}

// bundleExchangeState checks only what this bound native exchange may execute
// and what terminal its actual observations authorize. It is not a second copy
// implementation; Core must issue every phase, including tolerated observations.
type retainedBundlePhase struct {
	sequence  int
	operation string
	arguments json.RawMessage
	original  error
}

type bundleExchangeState struct {
	request                     ProjectBundleRequest
	native                      ProjectBundleNative
	binding                     map[string]string
	next, source, outcome, code string
	library                     *string
	root                        string
	members                     []string
	member                      int
	metadata                    []byte
	failure                     *ProjectBundleFailure
	transcript                  []projectBundleReceipt
	retained                    []retainedBundlePhase
}

func newBundleExchange(r ProjectBundleRequest, native ProjectBundleNative) *bundleExchangeState {
	s := &bundleExchangeState{request: r, native: native, binding: r.binding(), transcript: []projectBundleReceipt{}}
	if r.Kind == "wizard" {
		s.next = "observe_local_recipe"
		s.source = "packaged"
	} else if r.Kind == "disk" {
		s.source = "explicit"
		s.beginDisk()
	} else {
		s.source = "packaged"
		s.beginPackaged()
	}
	return s
}
func (s *bundleExchangeState) stop(outcome, code string) {
	s.next = ""
	s.outcome = outcome
	s.code = code
}
func (s *bundleExchangeState) beginDisk() {
	if s.request.Destination == "" || (s.request.Kind == "disk" && s.request.Source == "") {
		s.stop("rejected", "empty_paths")
	} else {
		s.next = "load_metadata"
	}
}
func (s *bundleExchangeState) beginPackaged() {
	if s.request.Destination == "" || s.request.RecipeName == "" {
		s.stop("rejected", "empty_paths")
	} else {
		s.next = "packaged_candidates"
	}
}
func (s *bundleExchangeState) afterRecipe() {
	if s.library != nil && *s.library != "" {
		s.next = "observe_library"
	} else {
		s.next = "observe_network"
	}
}
func bundleDirectory(k string) bool {
	return k == "directory" || k == "absent" || k == "not_directory" || k == "stat_failed"
}
func bundleJSON(v interface{}) json.RawMessage { data, _ := json.Marshal(v); return data }
func bundleBinding(raw []byte, expected map[string]string) bool {
	keys := []string{}
	for key := range expected {
		keys = append(keys, key)
	}
	var actual map[string]string
	_, err := recipeObject(raw, &actual, strings.Join(keys, " "), "")
	return err == nil && reflect.DeepEqual(actual, expected)
}
func bundleEqual(a, b []byte) bool {
	var av, bv interface{}
	return json.Unmarshal(a, &av) == nil && json.Unmarshal(b, &bv) == nil && reflect.DeepEqual(av, bv)
}
func bundleDecodeBytes(raw json.RawMessage, nullable bool) ([]byte, bool) {
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil, nullable
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return nil, false
	}
	data, err := recipeBytes(text)
	// An explicitly supplied empty file differs from a missing catalog member.
	if data == nil && err == nil {
		data = []byte{}
	}
	return data, err == nil
}

func (s *bundleExchangeState) phase(line []byte) (interface{}, bool) {
	var frame struct {
		Status    string            `json:"status"`
		Binding   map[string]string `json:"binding"`
		Sequence  int               `json:"sequence"`
		Operation string            `json:"operation"`
		Arguments json.RawMessage   `json:"arguments"`
	}
	rawFrame, decodeErr := recipeObject(line, &frame, "status binding sequence operation arguments", "")
	if decodeErr != nil || !bundleBinding(rawFrame["binding"], s.binding) || frame.Status != "bundle_io" || !reflect.DeepEqual(frame.Binding, s.binding) || frame.Sequence != len(s.transcript)+1 || s.next == "" || frame.Operation != s.next {
		return nil, false
	}
	op := frame.Operation
	args := map[string]interface{}{}
	source := s.source
	switch op {
	case "load_metadata", "absolute_locations", "copy_recipe", "observe_network", "copy_network":
		args["source"] = source
	case "observe_library", "copy_library":
		if s.library == nil {
			return nil, false
		}
		args["source"] = source
		args["library_b64"] = bundleB64(*s.library)
	case "decode_packaged_metadata":
		args["root_b64"] = bundleB64(s.root)
		args["content_b64"] = nil
	case "prepare_packaged_file", "write_packaged_file":
		if s.member >= len(s.members) {
			return nil, false
		}
		args["relative_b64"] = bundleB64(s.members[s.member])
		if op == "write_packaged_file" {
			args["content_b64"] = ""
		}
	}
	keys := []string{}
	for key := range args {
		keys = append(keys, key)
	}
	raw, err := recipeObject(frame.Arguments, nil, strings.Join(keys, " "), "content_b64")
	if err != nil {
		return nil, false
	}
	var content []byte
	if op == "decode_packaged_metadata" || op == "write_packaged_file" {
		var valid bool
		content, valid = bundleDecodeBytes(raw["content_b64"], op == "decode_packaged_metadata")
		if !valid {
			return nil, false
		}
		if content != nil {
			args["content_b64"] = bundleB64(string(content))
		}
		if op == "decode_packaged_metadata" {
			_, isBundle := bundleCatalogMembers(s.root)
			hasMetadata := false
			for _, member := range s.members {
				hasMetadata = hasMetadata || member == ".recipe/recipe.json"
			}
			if !isBundle || hasMetadata != (content != nil) {
				return nil, false
			}
		} else if s.members[s.member] == ".recipe/recipe.json" && !bytes.Equal(content, s.metadata) {
			return nil, false
		}
	}
	if !bundleEqual(frame.Arguments, bundleJSON(args)) {
		return nil, false
	}
	var result interface{}
	var phaseErr, observedErr error
	failureCode := ""
	switch op {
	case "observe_local_recipe":
		k := s.native.ObserveLocalRecipe()
		if !bundleDirectory(k) {
			return nil, false
		}
		result = map[string]string{"kind": k}
		if k == "directory" {
			s.source = "local"
			s.next = "load_metadata"
		} else {
			s.next = "find_global"
		}
	case "find_global":
		found := s.native.FindGlobal()
		result = map[string]bool{"found": found}
		if found {
			s.source = "global"
			s.beginDisk()
		} else {
			s.source = "packaged"
			s.beginPackaged()
		}
	case "load_metadata", "decode_packaged_metadata":
		var m ProjectBundleMetadata
		if op == "load_metadata" {
			m, phaseErr = s.native.LoadMetadata(source)
		} else {
			m, phaseErr = s.native.DecodePackagedMetadata(s.root, content)
			s.metadata = content
		}
		observedErr = phaseErr
		var lib interface{}
		if m.Library != nil {
			lib = bundleB64(*m.Library)
		}
		result = map[string]interface{}{"name_b64": bundleB64(m.Name), "library_b64": lib}
		failureCode = "source_invalid"
		if source == "local" {
			if phaseErr == nil && m.Name != "" {
				s.stop("unchanged", "")
			} else {
				s.source = "packaged"
				s.next = "find_global"
			}
			// Local load errors are in the transcript, but deliberately tolerated.
			if phaseErr != nil {
				result = map[string]bool{"failed": true}
				phaseErr = nil
			}
		} else if phaseErr == nil {
			s.library = m.Library
			if m.Name == "" {
				s.stop("rejected", "source_empty_name")
			} else if source == "packaged" {
				s.next = "remove_recipe"
			} else {
				s.next = "absolute_locations"
			}
		}
	case "absolute_locations":
		a, b := s.native.AbsoluteLocations(source)
		var av, bv interface{}
		if a != nil {
			av = bundleB64(*a)
		}
		if b != nil {
			bv = bundleB64(*b)
		}
		result = map[string]interface{}{"source_b64": av, "destination_b64": bv}
		if a != nil && b != nil && *a == *b {
			s.stop("unchanged", "")
		} else {
			s.next = "remove_recipe"
		}
	case "remove_recipe":
		phaseErr = s.native.RemoveRecipe()
		failureCode = "remove_recipe_failed"
		if source == "packaged" {
			s.next = "prepare_packaged_file"
			if len(s.members) == 0 {
				s.stop("complete", "")
			}
		} else {
			s.next = "copy_recipe"
		}
	case "copy_recipe":
		phaseErr = s.native.CopyRecipe(source)
		failureCode = "copy_recipe_failed"
		s.afterRecipe()
	case "observe_library":
		k := s.native.ObserveLibrary(source, *s.library)
		if !bundleDirectory(k) {
			return nil, false
		}
		result = map[string]string{"kind": k}
		if k == "directory" {
			s.next = "copy_library"
		} else {
			s.next = "observe_network"
		}
	case "copy_library":
		phaseErr = s.native.CopyLibrary(source, *s.library)
		failureCode = "copy_library_failed"
		s.next = "observe_network"
	case "observe_network":
		k := s.native.ObserveNetwork(source)
		if !bundleDirectory(k) {
			return nil, false
		}
		result = map[string]string{"kind": k}
		if k == "directory" {
			s.next = "copy_network"
		} else {
			s.stop("complete", "")
		}
	case "copy_network":
		phaseErr = s.native.CopyNetwork(source)
		failureCode = "copy_network_failed"
		s.stop("complete", "")
	case "packaged_candidates":
		roots := s.native.PackagedCandidates()
		if len(roots) != 3 {
			return nil, false
		}
		encoded := []string{}
		for _, r := range roots {
			encoded = append(encoded, bundleB64(r))
		}
		result = map[string]interface{}{"roots_b64": encoded}
		for _, r := range roots {
			if members, present := bundleCatalogMembers(r); present {
				s.root = r
				s.members = members
				break
			}
		}
		if s.root == "" {
			s.stop("rejected", "packaged_not_found")
		} else {
			s.next = "decode_packaged_metadata"
		}
	case "prepare_packaged_file":
		phaseErr = s.native.PreparePackagedFile(s.members[s.member])
		failureCode = "packaged_create_failed"
		s.next = "write_packaged_file"
	case "write_packaged_file":
		phaseErr = s.native.WritePackagedFile(s.members[s.member], content)
		failureCode = "packaged_write_failed"
		s.member++
		if s.member == len(s.members) {
			s.stop("complete", "")
		} else {
			s.next = "prepare_packaged_file"
		}
	default:
		return nil, false
	}
	if observedErr == nil {
		observedErr = phaseErr
	}
	s.retained = append(s.retained, retainedBundlePhase{frame.Sequence, op, append(json.RawMessage(nil), frame.Arguments...), observedErr})
	if phaseErr != nil {
		result = map[string]bool{"failed": true}
		s.failure = &ProjectBundleFailure{frame.Sequence, op}
		s.stop("failed", failureCode)
	} else if result == nil {
		result = map[string]bool{"ok": true}
	}
	receipt := projectBundleReceipt{frame.Sequence, op, bundleJSON(result)}
	s.transcript = append(s.transcript, receipt)
	return struct {
		Status    string      `json:"status"`
		Sequence  int         `json:"sequence"`
		Operation string      `json:"operation"`
		Result    interface{} `json:"result"`
	}{"bundle_io_result", frame.Sequence, op, result}, true
}

func (s *bundleExchangeState) terminal(line []byte) (ProjectBundleResult, bool) {
	var r ProjectBundleResult
	if s.next != "" {
		return r, false
	}
	raw, err := recipeObject(line, nil, "status binding source outcome code failure transcript", "failure")
	if err != nil || !bundleBinding(raw["binding"], s.binding) {
		return r, false
	}
	// Decode only the closed wire object, never the adapter-only Original field.
	if json.Unmarshal(line, &r) != nil || r.Status != "bundle_materialized" || !reflect.DeepEqual(r.Binding, s.binding) || r.Source != s.source || r.Outcome != s.outcome || r.Code != s.code || !reflect.DeepEqual(r.Failure, s.failure) {
		return r, false
	}
	if r.Failure != nil {
		if _, err := recipeObject(raw["failure"], &r.Failure, "sequence operation", ""); err != nil {
			return r, false
		}
	}
	if r.Transcript == nil || len(r.Transcript) != len(s.transcript) {
		return r, false
	}
	var rows []json.RawMessage
	if json.Unmarshal(raw["transcript"], &rows) != nil {
		return r, false
	}
	for i, row := range rows {
		if _, err := recipeObject(row, nil, "sequence operation result", ""); err != nil || !bundleEqual(row, bundleJSON(s.transcript[i])) {
			return r, false
		}
	}
	return r, true
}

// MaterializeProjectRecipe uses the existing caller's selected interpreter. No
// PATH search, shell, cap, timeout, worker goroutine or alternate writer is added.
// Unwind owns only this direct child, using the existing Recipe close/reap rule.
func MaterializeProjectRecipe(python string, request ProjectBundleRequest, native ProjectBundleNative) (ProjectBundleResult, error) {
	if request.binding() == nil || native == nil {
		return bundleUnknown()
	}
	cmd := exec.Command(python, "-m", "lingtai", "project", "recipe-materialize")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return bundleUnknown()
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return bundleUnknown()
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if cmd.Start() != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return bundleUnknown()
	}
	finished := false
	defer func() {
		_ = stdin.Close()
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = stdout.Close()
	}()
	s := newBundleExchange(request, native)
	writer := bufio.NewWriter(stdin)
	send := func(v interface{}) error {
		if err := json.NewEncoder(writer).Encode(v); err != nil {
			return err
		}
		return writer.Flush()
	}
	if send(s.binding) != nil {
		return bundleUnknown()
	}
	reader := bufio.NewReader(stdout)
	var terminal *ProjectBundleResult
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil || terminal != nil {
			return bundleUnknown()
		}
		var tag struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(line, &tag) != nil {
			return bundleUnknown()
		}
		if tag.Status == "bundle_io" {
			ack, ok := s.phase(line)
			if !ok || send(ack) != nil {
				return bundleUnknown()
			}
		} else {
			r, ok := s.terminal(line)
			if !ok {
				return bundleUnknown()
			}
			terminal = &r
			_ = stdin.Close()
		}
	}
	_ = stdin.Close()
	err = cmd.Wait()
	finished = true
	if err != nil || stderr.Len() != 0 || terminal == nil {
		return bundleUnknown()
	}
	if terminal.Failure != nil {
		f := terminal.Failure
		if f.Sequence < 1 || f.Sequence > len(s.retained) {
			return bundleUnknown()
		}
		retained := s.retained[f.Sequence-1]
		if retained.sequence != f.Sequence || retained.operation != f.Operation || retained.original == nil {
			return bundleUnknown()
		}
		terminal.Original = retained.original
	}
	return *terminal, nil
}

var _ error = (*ProjectBundleUnknownError)(nil)
