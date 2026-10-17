package process

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os/exec"
)

// Closed evidence vocabulary for existing Recipe only. Unknown owner JSON never
// crosses the child transport. Handles and opaque entries expire with this call.
type ProjectRecipeMetadata struct {
	SourcePathB64    string  `json:"source_path_b64"`
	Name             string  `json:"name"`
	LibraryName      *string `json:"library_name"`
	LibraryPathEntry string  `json:"library_path_entry"`
}
type ProjectRecipeSkillsEvidence struct {
	Present          bool          `json:"present"`
	CanonicalChanged bool          `json:"canonical_changed"`
	Handle           int           `json:"handle"`
	Paths            []interface{} `json:"paths"` // string or original non-string entry index
}
type ProjectRecipeLayerStat struct {
	Exists    bool   `json:"exists"`
	Directory bool   `json:"directory"`
	PathB64   string `json:"path_b64"`
}
type ProjectRecipeNativeOwner interface {
	Metadata() (ProjectRecipeMetadata, *ProjectRecipeFailure)
	ObserveSkills(string) (ProjectRecipeSkillsEvidence, *ProjectRecipeFailure)
	WriteSkills(string, int, []interface{}) *ProjectRecipeFailure
	StatLayer(string, string) ProjectRecipeLayerStat
	ReadLayer(string) ([]byte, bool)
	NativeError(*ProjectRecipeFailure) error
}

type recipeNativeSession struct {
	owner          ProjectRecipeNativeOwner
	sequence       int
	selected       string
	layerChosen    bool
	layerOnly      bool
	layer          string
	language       string
	statCount      int
	documentHandle int
	documentName   string
	documentPaths  []interface{}
}

func (s *recipeNativeSession) respond(line []byte) (interface{}, error) {
	var frame struct {
		Status    string          `json:"status"`
		Sequence  int             `json:"sequence"`
		Operation string          `json:"operation"`
		Arguments json.RawMessage `json:"arguments"`
	}
	if _, err := recipeObject(line, &frame, "status sequence operation arguments", ""); err != nil || frame.Status != "recipe_native" || frame.Sequence != s.sequence+1 {
		return nil, errors.New("native Recipe frame")
	}
	var evidence interface{}
	var failure *ProjectRecipeFailure
	if s.layerOnly && frame.Operation != "layer_stat" {
		return nil, errors.New("native Recipe query operation")
	}
	switch frame.Operation {
	case "metadata":
		if _, err := recipeObject(frame.Arguments, nil, "", ""); err != nil {
			return nil, err
		}
		evidence, failure = s.owner.Metadata()
	case "skills_observe":
		var args struct {
			DirectoryNameB64 string `json:"directory_name_b64"`
		}
		if _, err := recipeObject(frame.Arguments, &args, "directory_name_b64", ""); err != nil {
			return nil, err
		}
		name, err := recipeBytes(args.DirectoryNameB64)
		if err != nil || len(name) == 0 {
			return nil, errors.New("native Recipe member")
		}
		observed, failed := s.owner.ObserveSkills(string(name))
		evidence, failure = observed, failed
		s.documentHandle, s.documentName, s.documentPaths = observed.Handle, string(name), observed.Paths
	case "skills_write":
		var args struct {
			DirectoryNameB64 string            `json:"directory_name_b64"`
			Handle           int               `json:"handle"`
			Paths            []json.RawMessage `json:"paths"`
		}
		if _, err := recipeObject(frame.Arguments, &args, "directory_name_b64 handle paths", ""); err != nil {
			return nil, err
		}
		name, err := recipeBytes(args.DirectoryNameB64)
		if err != nil || len(name) == 0 || args.Handle < 1 {
			return nil, errors.New("native Recipe handle")
		}
		paths := make([]interface{}, 0, len(args.Paths))
		for _, raw := range args.Paths {
			var text string
			var entry int
			if len(raw) > 0 && raw[0] == '"' && json.Unmarshal(raw, &text) == nil {
				paths = append(paths, text)
			} else if json.Unmarshal(raw, &entry) == nil && entry >= 0 && !bytes.Equal(raw, []byte("null")) {
				paths = append(paths, entry)
			} else {
				return nil, errors.New("native Recipe path reference")
			}
		}
		if args.Handle != s.documentHandle || string(name) != s.documentName || (len(paths) != len(s.documentPaths) && len(paths) != len(s.documentPaths)+1) {
			return nil, errors.New("native Recipe document binding")
		}
		for i, original := range s.documentPaths {
			if paths[i] != original {
				return nil, errors.New("native Recipe entry binding")
			}
		}
		if len(paths) > len(s.documentPaths) {
			if _, ok := paths[len(paths)-1].(string); !ok {
				return nil, errors.New("native Recipe appended entry")
			}
		}
		s.documentHandle, s.documentName, s.documentPaths = 0, "", nil
		failure = s.owner.WriteSkills(string(name), args.Handle, paths)
		evidence = struct {
			Written bool `json:"written"`
		}{true}
	case "layer_stat":
		var args struct {
			Layer     string `json:"layer"`
			Localized bool   `json:"localized"`
		}
		if _, err := recipeObject(frame.Arguments, &args, "layer localized", ""); err != nil {
			return nil, err
		}
		if args.Layer != s.layer || s.layerChosen || s.statCount > 1 || (s.language == "" && s.statCount > 0) {
			return nil, errors.New("native Recipe layer binding")
		}
		localized := s.language != "" && s.statCount == 0
		if args.Localized != localized {
			return nil, errors.New("native Recipe locale binding")
		}
		language := ""
		if localized {
			language = s.language
		} // retain original native string bytes
		s.statCount++
		stat := s.owner.StatLayer(args.Layer, language)
		s.selected = ""
		if stat.Exists && !stat.Directory {
			s.selected = stat.PathB64
			s.layerChosen = true
		}
		evidence = stat
	case "layer_read":
		var args struct {
			PathB64 string `json:"path_b64"`
		}
		if _, err := recipeObject(frame.Arguments, &args, "path_b64", ""); err != nil {
			return nil, err
		}
		path, err := recipeBytes(args.PathB64)
		if err != nil || args.PathB64 == "" || args.PathB64 != s.selected {
			return nil, errors.New("native Recipe selected path")
		}
		content, failed := s.owner.ReadLayer(string(path))
		evidence = struct {
			ReadFailed bool   `json:"read_failed"`
			ContentB64 string `json:"content_b64"`
		}{failed, base64.StdEncoding.EncodeToString(content)}
		s.selected = ""
	default:
		return nil, errors.New("native Recipe operation")
	}
	s.sequence = frame.Sequence
	if failure != nil {
		evidence = nil
	}
	return struct {
		Status    string                `json:"status"`
		Sequence  int                   `json:"sequence"`
		Operation string                `json:"operation"`
		Evidence  interface{}           `json:"evidence"`
		Failure   *ProjectRecipeFailure `json:"failure"`
	}{"recipe_native_observed", frame.Sequence, frame.Operation, evidence, failure}, nil
}

// SelectProjectRecipeLayer shares the closed native evidence vocabulary, not an
// apply attempt. Only native stat occurs; the existing consumer reads afterward.
func SelectProjectRecipeLayer(python, language, layer string, owner ProjectRecipeNativeOwner) (string, error) {
	cmd := exec.Command(kernelCLI(python), "project", "recipe-layer")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return "", &ProjectRecipeError{}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return "", &ProjectRecipeError{}
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if cmd.Start() != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return "", &ProjectRecipeError{}
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
	send := func(value interface{}) error { return json.NewEncoder(stdin).Encode(value) }
	if send(struct {
		Language string `json:"language"`
		Layer    string `json:"layer"`
	}{language, layer}) != nil {
		return "", &ProjectRecipeError{}
	}
	session := recipeNativeSession{owner: owner, layerOnly: true, layer: layer, language: language}
	reader := bufio.NewReader(stdout)
	selected := ""
	terminal := false
	for {
		line, readErr := reader.ReadBytes('\n')
		if readErr == io.EOF && len(line) == 0 {
			break
		}
		if readErr != nil || terminal {
			return "", &ProjectRecipeError{}
		}
		var tag struct {
			Status string `json:"status"`
		}
		if json.Unmarshal(line, &tag) != nil {
			return "", &ProjectRecipeError{}
		}
		if tag.Status == "recipe_native" {
			ack, err := session.respond(line)
			if err != nil || send(ack) != nil {
				return "", &ProjectRecipeError{}
			}
			continue
		}
		var result struct {
			Status  string `json:"status"`
			PathB64 string `json:"path_b64"`
		}
		if _, err := recipeObject(line, &result, "status path_b64", ""); err != nil || result.Status != "recipe_layer_selected" || result.PathB64 != session.selected || session.statCount == 0 || (!session.layerChosen && language != "" && session.statCount != 2) {
			return "", &ProjectRecipeError{}
		}
		path, err := recipeBytes(result.PathB64)
		if err != nil {
			return "", &ProjectRecipeError{}
		}
		selected, terminal = string(path), true
	}
	_ = stdin.Close()
	err = cmd.Wait()
	finished = true
	if err != nil || stderr.Len() != 0 || !terminal {
		return "", &ProjectRecipeError{}
	}
	return selected, nil
}
