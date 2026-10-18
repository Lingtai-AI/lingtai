package process

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// ProjectStartupReadiness is kernel policy evidence, not a Go policy oracle.
// Callback results expose original native directory strings, not JSON labels.
type ProjectStartupReadiness struct {
	Reason           string                `json:"reason"`
	NeedsRehydration bool                  `json:"needs_rehydration"`
	Agents           []string              `json:"agents"`
	Orchestrators    []string              `json:"orchestrators"`
	InitEvidence     []ProjectInitEvidence `json:"init_evidence"`
}
type ProjectInitFailure struct {
	PathB64        string `json:"path_b64"`
	NativeErrno    *int64 `json:"native_errno"`
	NativeWinerror *int64 `json:"native_winerror"`
}
type ProjectInitEvidence struct {
	DirectoryName    string              `json:"directory_name"`
	Sequence         int64               `json:"-"`
	DirectoryNameB64 string              `json:"-"`
	Presence         string              `json:"presence"`
	Failure          *ProjectInitFailure `json:"failure"`
}
type projectInitAck struct {
	Status           string              `json:"status"`
	Sequence         int64               `json:"sequence"`
	DirectoryNameB64 string              `json:"directory_name_b64"`
	Presence         string              `json:"presence"`
	Failure          *ProjectInitFailure `json:"failure"`
}
type retainedProjectInit struct {
	ack        projectInitAck
	name, path string
	original   error
}

// Inspection has a mode-specific closed decoder. Do not weaken the generic
// strictProjectObject: only these documented native facts may be null.
func inspectionObject(data []byte) (map[string]interface{}, error) {
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	var value func() (interface{}, error)
	value = func() (interface{}, error) {
		start := d.InputOffset()
		token, err := d.Token()
		if err != nil {
			return nil, err
		}
		if text, ok := token.(string); ok {
			raw := bytes.TrimLeft(data[start:d.InputOffset()], " \t\r\n,:")
			points, err := inspectionStringRunes(raw)
			if err != nil {
				return nil, err
			}
			return inspectionWireString{text: text, points: points}, nil
		}
		delim, container := token.(json.Delim)
		if !container {
			return token, nil
		}
		switch delim {
		case '{':
			object := map[string]interface{}{}
			for d.More() {
				key, err := d.Token()
				if err != nil {
					return nil, err
				}
				name, ok := key.(string)
				if !ok {
					return nil, unknownProjectOperation()
				}
				if _, exists := object[name]; exists {
					return nil, unknownProjectOperation()
				}
				item, err := value()
				if err != nil {
					return nil, err
				}
				object[name] = item
			}
			end, err := d.Token()
			if err != nil || end != json.Delim('}') {
				return nil, unknownProjectOperation()
			}
			return object, nil
		case '[':
			array := []interface{}{}
			for d.More() {
				item, err := value()
				if err != nil {
					return nil, err
				}
				array = append(array, item)
			}
			end, err := d.Token()
			if err != nil || end != json.Delim(']') {
				return nil, unknownProjectOperation()
			}
			return array, nil
		}
		return nil, unknownProjectOperation()
	}
	item, err := value()
	if err != nil {
		return nil, err
	}
	if _, err = d.Token(); err != io.EOF {
		return nil, unknownProjectOperation()
	}
	object, ok := item.(map[string]interface{})
	if !ok {
		return nil, unknownProjectOperation()
	}
	return object, nil
}
func inspectionFields(v interface{}, keys ...string) (map[string]interface{}, bool) {
	m, ok := v.(map[string]interface{})
	if !ok || len(m) != len(keys) {
		return nil, false
	}
	for _, key := range keys {
		if _, exists := m[key]; !exists {
			return nil, false
		}
	}
	return m, true
}

// Keep isolated JSON surrogate codepoints until native-identity validation.
// encoding/json's replacement text alone cannot distinguish \udcfe from \udcff.
type inspectionWireString struct {
	text   string
	points []rune
}

func inspectionStringRunes(raw []byte) ([]rune, error) {
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' {
		return nil, unknownProjectOperation()
	}
	raw = raw[1 : len(raw)-1]
	points := []rune{}
	for len(raw) > 0 {
		if raw[0] != '\\' {
			r, n := utf8.DecodeRune(raw)
			if r == utf8.RuneError && n == 1 {
				return nil, unknownProjectOperation()
			}
			points = append(points, r)
			raw = raw[n:]
			continue
		}
		if len(raw) < 2 {
			return nil, unknownProjectOperation()
		}
		if raw[1] == 'u' {
			if len(raw) < 6 {
				return nil, unknownProjectOperation()
			}
			n, err := strconv.ParseUint(string(raw[2:6]), 16, 16)
			if err != nil {
				return nil, err
			}
			r := rune(n)
			raw = raw[6:]
			if r >= 0xd800 && r <= 0xdbff && len(raw) >= 6 && raw[0] == '\\' && raw[1] == 'u' {
				low, err := strconv.ParseUint(string(raw[2:6]), 16, 16)
				if err != nil {
					return nil, err
				}
				if low >= 0xdc00 && low <= 0xdfff {
					r = utf16.DecodeRune(r, rune(low))
					raw = raw[6:]
				}
			}
			points = append(points, r)
			continue
		}
		escapes := map[byte]rune{'"': '"', '\\': '\\', '/': '/', 'b': '\b', 'f': '\f', 'n': '\n', 'r': '\r', 't': '\t'}
		r, ok := escapes[raw[1]]
		if !ok {
			return nil, unknownProjectOperation()
		}
		points = append(points, r)
		raw = raw[2:]
	}
	return points, nil
}
func inspectionText(v interface{}) (string, bool) {
	if s, ok := v.(inspectionWireString); ok {
		return s.text, true
	}
	s, ok := v.(string)
	return s, ok
}
func inspectionLabelMatches(v interface{}, native string) bool {
	if wire, ok := v.(inspectionWireString); ok {
		return reflect.DeepEqual(wire.points, inspectionNameRunes(native))
	}
	s, ok := inspectionText(v)
	return ok && s == inspectionLabel(native)
}
func inspectionInteger(v interface{}) (int64, bool) {
	n, ok := v.(json.Number)
	if !ok {
		return 0, false
	}
	i, err := n.Int64()
	return i, err == nil
}
func inspectionStrings(v interface{}) ([]string, bool) {
	a, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(a))
	for _, v := range a {
		s, ok := inspectionText(v)
		if !ok {
			return nil, false
		}
		result = append(result, s)
	}
	return result, true
}
func inspectionBytes(v interface{}) (string, string, bool) {
	encoded, ok := inspectionText(v)
	if !ok {
		return "", "", false
	}
	decoded, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if err != nil || base64.StdEncoding.EncodeToString(decoded) != encoded {
		return "", "", false
	}
	return string(decoded), encoded, true
}
func inspectionNativeName(name string) bool {
	// A native immediate directory segment; POSIX backslash is an ordinary byte.
	return name != "" && name != "." && name != ".." && !strings.ContainsRune(name, 0) && filepath.Base(name) == name && !filepath.IsAbs(name) && filepath.VolumeName(name) == ""
}

// fsdecode's surrogateescape order differs from Go byte order. The kernel's
// directory order stays authoritative; never sort its returned arrays in Go.
func inspectionNameRunes(name string) []rune {
	result := []rune{}
	for len(name) > 0 {
		r, size := utf8.DecodeRuneInString(name)
		if r == utf8.RuneError && size == 1 {
			// Windows fsencode uses surrogatepass for unpaired UTF-16 surrogates.
			if runtime.GOOS == "windows" && len(name) >= 3 && name[0] == 0xed && name[1] >= 0xa0 && name[1] <= 0xbf && name[2] >= 0x80 && name[2] <= 0xbf {
				r = rune(name[0]&15)<<12 | rune(name[1]&63)<<6 | rune(name[2]&63)
				size = 3
			} else {
				r = 0xdc00 + rune(name[0])
			}
		}
		result = append(result, r)
		name = name[size:]
	}
	return result
}
func inspectionNameLess(a, b string) bool {
	x, y := inspectionNameRunes(a), inspectionNameRunes(b)
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}
func inspectionLabel(name string) string {
	r := inspectionNameRunes(name)
	for i, v := range r {
		if v >= 0xd800 && v <= 0xdfff {
			r[i] = utf8.RuneError
		}
	}
	return string(r)
}
func decodeInspectionFailure(v interface{}, presence string) (*ProjectInitFailure, bool) {
	if presence != "stat_failed" {
		return nil, v == nil && (presence == "present" || presence == "absent")
	}
	m, ok := inspectionFields(v, "path_b64", "native_errno", "native_winerror")
	if !ok {
		return nil, false
	}
	_, path, ok := inspectionBytes(m["path_b64"])
	if !ok {
		return nil, false
	}
	f := &ProjectInitFailure{PathB64: path}
	for key, target := range map[string]**int64{"native_errno": &f.NativeErrno, "native_winerror": &f.NativeWinerror} {
		if m[key] != nil {
			n, ok := inspectionInteger(m[key])
			if !ok {
				return nil, false
			}
			*target = &n
		}
	}
	return f, true
}

func decodeProjectInspection(data []byte, callback bool, transcript []retainedProjectInit) (ProjectInspection, error) {
	bad := func() (ProjectInspection, error) { return ProjectInspection{}, unknownProjectOperation() }
	object, err := inspectionObject(data)
	if err != nil {
		return bad()
	}
	top, ok := inspectionFields(object, "status", "initialized", "members", "startup_readiness")
	if !ok {
		return bad()
	}
	status, ok := inspectionText(top["status"])
	if !ok || status != "inspected" {
		return bad()
	}
	initialized, ok := top["initialized"].(bool)
	if !ok {
		return bad()
	}
	members, ok := top["members"].([]interface{})
	if !ok || (!initialized && len(members) != 0) {
		return bad()
	}
	result := ProjectInspection{Status: "inspected", Initialized: &initialized, Members: []ProjectMember{}}
	labels := map[string]string{}
	eligible, orchestrators := []string{}, []string{}
	for _, value := range members {
		keys := []string{"directory_name", "manifest_kind", "role", "agent_name", "address", "agent_id"}
		if callback {
			keys = append(keys, "directory_name_b64")
		}
		m, ok := inspectionFields(value, keys...)
		if !ok {
			return bad()
		}
		texts := map[string]string{}
		for _, key := range keys {
			s, ok := inspectionText(m[key])
			if !ok {
				return bad()
			}
			texts[key] = s
		}
		label, name := texts["directory_name"], texts["directory_name"]
		if callback {
			native, _, ok := inspectionBytes(m["directory_name_b64"])
			if !ok || !inspectionLabelMatches(m["directory_name"], native) {
				return bad()
			}
			name = native
		}
		if !inspectionNativeName(name) {
			return bad()
		}
		if len(result.Members) > 0 && !inspectionNameLess(result.Members[len(result.Members)-1].DirectoryName, name) {
			return bad()
		}
		kind, role := texts["manifest_kind"], texts["role"]
		if kind == "malformed" {
			if role != "unknown" {
				return bad()
			}
		} else if kind != "valid" || (role != "human" && role != "worker" && role != "orchestrator") {
			return bad()
		}
		result.Members = append(result.Members, ProjectMember{DirectoryName: name, ManifestKind: kind, Role: role, AgentName: texts["agent_name"], Address: texts["address"], AgentID: texts["agent_id"]})
		labels[name] = label
		// Validate representations against the reported member roles, not manifests.
		if !strings.HasPrefix(name, ".") && kind == "valid" && (role == "worker" || role == "orchestrator") {
			eligible = append(eligible, name)
			if role == "orchestrator" {
				orchestrators = append(orchestrators, name)
			}
		}
	}
	keys := []string{"reason", "needs_rehydration", "agents", "orchestrators", "init_evidence"}
	if callback {
		keys = append(keys, "agents_b64", "orchestrators_b64")
	}
	ready, ok := inspectionFields(top["startup_readiness"], keys...)
	if !ok {
		return bad()
	}
	reason, ok := inspectionText(ready["reason"])
	if !ok {
		return bad()
	}
	rehydrate, ok := ready["needs_rehydration"].(bool)
	if !ok {
		return bad()
	}
	arrays := map[string][]string{}
	for _, key := range []string{"agents", "orchestrators"} {
		names, ok := inspectionStrings(ready[key])
		if !ok {
			return bad()
		}
		if callback {
			encoded, ok := inspectionStrings(ready[key+"_b64"])
			if !ok || len(names) != len(encoded) {
				return bad()
			}
			for i, v := range encoded {
				native, _, ok := inspectionBytes(v)
				if !ok || !inspectionLabelMatches(ready[key].([]interface{})[i], native) {
					return bad()
				}
				names[i] = native
			}
		}
		arrays[key] = names
	}
	if !reflect.DeepEqual(arrays["agents"], eligible) || !reflect.DeepEqual(arrays["orchestrators"], orchestrators) {
		return bad()
	}
	rawEvidence, ok := ready["init_evidence"].([]interface{})
	if !ok || len(rawEvidence) > len(eligible) || (callback && len(rawEvidence) != len(transcript)) {
		return bad()
	}
	evidence := []ProjectInitEvidence{}
	present, absent, failed := false, false, false
	for i, value := range rawEvidence {
		keys := []string{"directory_name", "presence", "failure"}
		if callback {
			keys = append(keys, "sequence", "directory_name_b64")
		}
		m, ok := inspectionFields(value, keys...)
		if !ok || failed {
			return bad()
		}
		name := eligible[i]
		label, ok := inspectionText(m["directory_name"])
		if !ok || label != labels[name] || (callback && !inspectionLabelMatches(m["directory_name"], name)) {
			return bad()
		}
		item := ProjectInitEvidence{DirectoryName: name}
		item.Presence, ok = inspectionText(m["presence"])
		if !ok {
			return bad()
		}
		item.Failure, ok = decodeInspectionFailure(m["failure"], item.Presence)
		if !ok {
			return bad()
		}
		if callback {
			item.Sequence, ok = inspectionInteger(m["sequence"])
			if !ok || item.Sequence != int64(i+1) {
				return bad()
			}
			native, encoded, ok := inspectionBytes(m["directory_name_b64"])
			if !ok || native != name {
				return bad()
			}
			item.DirectoryNameB64 = encoded
			seen := transcript[i]
			if seen.name != name || seen.ack.Sequence != item.Sequence || seen.ack.DirectoryNameB64 != encoded || seen.ack.Presence != item.Presence || !reflect.DeepEqual(seen.ack.Failure, item.Failure) {
				return bad()
			}
		}
		present = present || item.Presence == "present"
		absent = absent || item.Presence == "absent"
		failed = item.Presence == "stat_failed"
		evidence = append(evidence, item)
	}
	// Semantic validation of the oracle's reason; never select a substitute reason.
	if failed != (reason == "cannot_stat_init") || (!failed && len(evidence) != len(eligible)) {
		return bad()
	}
	if rehydrate != (!failed && !present && absent && reason != "mixed_init") {
		return bad()
	}
	switch reason {
	case "not_initialized":
		if initialized || len(members) != 0 || len(evidence) != 0 || rehydrate {
			return bad()
		}
	case "cannot_stat_init":
		if !initialized || !failed || rehydrate {
			return bad()
		}
	case "mixed_init":
		if !initialized || failed || !present || !absent || rehydrate {
			return bad()
		}
	case "ready", "no_agents", "no_orchestrator", "multiple_orchestrators":
		if !initialized || failed || (present && absent) {
			return bad()
		}
		switch reason {
		case "ready":
			if len(orchestrators) != 1 {
				return bad()
			}
		case "no_agents":
			if len(eligible) != 0 {
				return bad()
			}
		case "no_orchestrator":
			if len(eligible) == 0 || len(orchestrators) != 0 {
				return bad()
			}
		case "multiple_orchestrators":
			if len(orchestrators) < 2 {
				return bad()
			}
		}
	default:
		return bad()
	}
	result.StartupReadiness = ProjectStartupReadiness{Reason: reason, NeedsRehydration: rehydrate, Agents: arrays["agents"], Orchestrators: arrays["orchestrators"], InitEvidence: evidence}
	return result, nil
}

// InspectProjectReadiness retains native stat errors through a live, uncapped
// same-command exchange. No deadline, retry, scan, or policy fallback is added.
func InspectProjectReadiness(ctx context.Context, python, root, lingtaiDir string) (ProjectInspection, error) {
	return inspectProjectReadiness(ctx, python, root, lingtaiDir, os.Stat)
}
func inspectProjectReadiness(ctx context.Context, python, root, lingtaiDir string, stat func(string) (os.FileInfo, error)) (ProjectInspection, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(ctx, kernelCLI(python), "project", "inspect")
	cmd.WaitDelay = time.Second // existing direct-child/inherited-pipe semantics
	in, err := cmd.StdinPipe()
	if err != nil {
		return ProjectInspection{}, unknownProjectOperation()
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		_ = in.Close()
		return ProjectInspection{}, unknownProjectOperation()
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if cmd.Start() != nil {
		_ = in.Close()
		_ = out.Close()
		return ProjectInspection{}, unknownProjectOperation()
	}
	finished := false
	defer func() {
		_ = in.Close()
		if !finished {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
		}
		_ = out.Close()
	}() // preserves panic/Goexit, no substitute ack
	stopClosing := context.AfterFunc(ctx, func() { _ = out.Close(); _ = in.Close() })
	defer stopClosing()
	writer := bufio.NewWriter(in)
	send := func(value interface{}) error {
		if err := json.NewEncoder(writer).Encode(value); err != nil {
			return err
		}
		return writer.Flush()
	}
	request := struct {
		ProjectDir string `json:"project_dir"`
		Mode       string `json:"init_observation_mode"`
	}{root, "callback"}
	if send(request) != nil {
		return ProjectInspection{}, unknownProjectOperation()
	}
	reader := bufio.NewReader(out)
	transcript := []retainedProjectInit{}
	var terminal []byte
	var result ProjectInspection
	stopped := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil || terminal != nil || ctx.Err() != nil {
			return ProjectInspection{}, unknownProjectOperation()
		}
		frame, err := inspectionObject(line)
		if err != nil {
			return ProjectInspection{}, unknownProjectOperation()
		}
		tag, tagOK := inspectionText(frame["status"])
		if !tagOK {
			return ProjectInspection{}, unknownProjectOperation()
		}
		if tag != "observe_init_presence" {
			result, err = decodeProjectInspection(line, true, transcript)
			if err != nil {
				return ProjectInspection{}, err
			}
			terminal = line
			_ = in.Close()
			continue
		}
		m, ok := inspectionFields(frame, "status", "sequence", "directory_name_b64")
		if !ok || stopped {
			return ProjectInspection{}, unknownProjectOperation()
		}
		sequence, ok := inspectionInteger(m["sequence"])
		if !ok || sequence != int64(len(transcript)+1) {
			return ProjectInspection{}, unknownProjectOperation()
		}
		name, encoded, ok := inspectionBytes(m["directory_name_b64"])
		if !ok || !inspectionNativeName(name) || strings.HasPrefix(name, ".") || (len(transcript) > 0 && !inspectionNameLess(transcript[len(transcript)-1].name, name)) {
			return ProjectInspection{}, unknownProjectOperation()
		}
		path := filepath.Join(lingtaiDir, name, "init.json")
		_, original := stat(path) // EXACTLY ONCE, on the original calling goroutine
		ack := projectInitAck{Status: "init_presence_observed", Sequence: sequence, DirectoryNameB64: encoded, Presence: "present"}
		if original != nil {
			if os.IsNotExist(original) {
				ack.Presence = "absent"
			} else {
				ack.Presence = "stat_failed"
				stopped = true
				ack.Failure = &ProjectInitFailure{PathB64: base64.StdEncoding.EncodeToString([]byte(path))}
				var errno syscall.Errno
				if errors.As(original, &errno) {
					code := int64(errno)
					if runtime.GOOS == "windows" {
						ack.Failure.NativeWinerror = &code
					} else {
						ack.Failure.NativeErrno = &code
					}
				}
			}
		}
		transcript = append(transcript, retainedProjectInit{ack: ack, name: name, path: path, original: original})
		if send(ack) != nil {
			return ProjectInspection{}, unknownProjectOperation()
		}
	}
	_ = in.Close()
	runErr := cmd.Wait()
	finished = true
	if ctx.Err() != nil {
		return ProjectInspection{}, unknownProjectOperation()
	}
	if runErr != nil {
		var exit *exec.ExitError
		if terminal == nil && errors.As(runErr, &exit) && exit.ExitCode() == 1 {
			// Specialized uncapped error shape; no generic limits or child text retained.
			m, err := inspectionObject(stderr.Bytes())
			if err == nil {
				e, ok := inspectionFields(m, "status", "code", "error")
				code, codeOK := inspectionText(e["code"])
				message, messageOK := inspectionText(e["error"])
				status, statusOK := inspectionText(e["status"])
				if ok && statusOK && status == "error" && codeOK && messageOK && message != "" {
					switch code {
					case "invalid_project_request", "invalid_project_root", "project_inspect_failed", "invalid_project_exchange":
						return ProjectInspection{}, &ProjectOperationError{Known: true, Code: code}
					}
				}
			}
		}
		return ProjectInspection{}, unknownProjectOperation()
	}
	if terminal == nil || stderr.Len() != 0 {
		return ProjectInspection{}, unknownProjectOperation()
	}
	if result.StartupReadiness.Reason == "cannot_stat_init" {
		// Full closed terminal/transcript AND exit/stderr validation precedes use.
		seen := transcript[len(transcript)-1]
		if seen.original == nil || seen.ack.Presence != "stat_failed" {
			return ProjectInspection{}, unknownProjectOperation()
		}
		return ProjectInspection{}, fmt.Errorf("sanity check: cannot stat %s: %w", seen.path, seen.original)
	}
	return result, nil
}
