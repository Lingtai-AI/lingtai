package process

import (
	"encoding/json"
	"testing"
)

type recipeNativeTestOwner struct{ stats, reads, writes int }

func (*recipeNativeTestOwner) Metadata() (ProjectRecipeMetadata, *ProjectRecipeFailure) {
	return ProjectRecipeMetadata{}, nil
}
func (*recipeNativeTestOwner) ObserveSkills(string) (ProjectRecipeSkillsEvidence, *ProjectRecipeFailure) {
	return ProjectRecipeSkillsEvidence{Present: true, Handle: 9, Paths: []interface{}{0, "old"}}, nil
}
func (n *recipeNativeTestOwner) WriteSkills(string, int, []interface{}) *ProjectRecipeFailure {
	n.writes++
	return nil
}
func (n *recipeNativeTestOwner) StatLayer(string, string) ProjectRecipeLayerStat {
	n.stats++
	return ProjectRecipeLayerStat{Exists: true, PathB64: "bG9jYWxl"}
}
func (n *recipeNativeTestOwner) ReadLayer(string) ([]byte, bool)       { n.reads++; return nil, true }
func (*recipeNativeTestOwner) NativeError(*ProjectRecipeFailure) error { return nil }

func nativeRecipeTestFrame(sequence int, operation string, arguments interface{}) []byte {
	data, _ := json.Marshal(map[string]interface{}{"status": "recipe_native", "sequence": sequence, "operation": operation, "arguments": arguments})
	return data
}

func TestNativeRecipeReadFailureCannotResumeFallback(t *testing.T) {
	owner := &recipeNativeTestOwner{}
	s := recipeNativeSession{owner: owner, layer: "greet", language: "zh"}
	if _, err := s.respond(nativeRecipeTestFrame(1, "layer_stat", map[string]interface{}{"layer": "greet", "localized": true})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.respond(nativeRecipeTestFrame(2, "layer_read", map[string]interface{}{"path_b64": "bG9jYWxl"})); err != nil {
		t.Fatal(err)
	}
	if _, err := s.respond(nativeRecipeTestFrame(3, "layer_stat", map[string]interface{}{"layer": "greet", "localized": false})); err == nil {
		t.Fatal("read failure resumed root fallback")
	}
	if owner.stats != 1 || owner.reads != 1 {
		t.Fatal("native operations replayed")
	}
}

func TestNativeRecipeUnknownFramesAndStaleHandlesDoNotWrite(t *testing.T) {
	for _, fault := range []string{"unknown", "wrong_handle", "changed_entry", "duplicate_sequence"} {
		t.Run(fault, func(t *testing.T) {
			owner := &recipeNativeTestOwner{}
			s := recipeNativeSession{owner: owner}
			if _, err := s.respond(nativeRecipeTestFrame(1, "skills_observe", map[string]interface{}{"directory_name_b64": "YQ=="})); err != nil {
				t.Fatal(err)
			}
			sequence, operation, handle, paths := 2, "skills_write", 9, []interface{}{0, "old", "../../lib"}
			switch fault {
			case "unknown":
				operation = "generic_parse"
			case "wrong_handle":
				handle = 8
			case "changed_entry":
				paths[0] = 1
			case "duplicate_sequence":
				sequence = 1
			}
			_, err := s.respond(nativeRecipeTestFrame(sequence, operation, map[string]interface{}{"directory_name_b64": "YQ==", "handle": handle, "paths": paths}))
			if err == nil || owner.writes != 0 {
				t.Fatal("unknown transport reached writer")
			}
		})
	}
}

func TestNativeRecipeUnknownChildNeverRestoresCauseOrWrites(t *testing.T) {
	// Existing child fixture only accepts the frozen raw request, so the native
	// variant's extra field receives exit 2 with no closed terminal. It is UNKNOWN,
	// not native metadata rejection or an invitation to run the old writer.
	python := projectFixturePython(t, "recipe_success")
	owner := &recipeNativeTestOwner{}
	calls := 0
	result, err := ApplyProjectRecipeNative(python, "bundle", "staging", "zh", func(raw string) string { calls++; return raw }, owner)
	failure, ok := err.(*ProjectRecipeError)
	if !ok || failure.Known || failure.Unwrap() != nil || result.Outcome != "unknown" || owner.writes != 0 || calls != 0 {
		t.Fatal("unknown child restored native cause or reached writer/callback")
	}
}
