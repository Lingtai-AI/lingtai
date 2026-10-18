package process

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// Core belongs to the management SDK, not to the controlled agent's older SDK.
func TestLifecyclePolicyHostKeepsExplicitSDK(t *testing.T) {
	python := projectFixturePython(t, "unused_lifecycle")
	t.Setenv("LINGTAI_EXTERNAL_LIFECYCLE_FIXTURE", "success")
	target := t.TempDir()
	old := t.TempDir()
	bin := "bin"
	if runtime.GOOS == "windows" {
		bin = "Scripts"
	}
	if err := os.MkdirAll(filepath.Join(old, bin), 0755); err != nil {
		t.Fatal(err)
	}
	name := "python"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(old, bin, name), []byte("older agent interpreter"), 0755); err != nil {
		t.Fatal(err)
	}
	// JSON quoting must preserve native Windows separators.
	raw, err := json.Marshal(map[string]interface{}{"venv_path": old})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(target, "init.json"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	result, err := RunProjectLifecycle(python, ProjectLifecycleOptions{Action: "sleep", Selected: target}, ProjectLifecycleNative{})
	if err != nil || result.Count != 1 {
		t.Fatalf("management SDK replaced by target SDK: result=%+v err=%v", result, err)
	}
	if _, err := os.Stat(filepath.Join(target, ".sleep")); err != nil {
		t.Fatal(err)
	}
}

func TestLifecyclePolicyHostDoesNotUseGlobalPATH(t *testing.T) {
	python := projectFixturePython(t, "unused_lifecycle")
	t.Setenv("LINGTAI_EXTERNAL_LIFECYCLE_FIXTURE", "success")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// A PATH Python has a working sibling child, but no managed runtime exists.
	name := "python3"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(filepath.Dir(python), name), []byte("PATH fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", filepath.Dir(python))
	target := t.TempDir()
	_, err := RunProjectLifecycle("", ProjectLifecycleOptions{Action: "sleep", Selected: target}, ProjectLifecycleNative{})
	var unknown *ProjectLifecycleUnknown
	if !errors.As(err, &unknown) {
		t.Fatalf("absent managed SDK used PATH: %v", err)
	}
	if _, err := os.Stat(filepath.Join(target, ".sleep")); !os.IsNotExist(err) {
		t.Fatalf("PATH child executed an effect: %v", err)
	}
}
