package preset

// Regression tests for issue #849: agent directory names and saved-preset
// filename stems must be single, contained path segments so relative-parent
// or path-form names cannot escape their owning directories.

import (
	"os"
	"path/filepath"
	"testing"
)

// unsafeNames covers the forms the owning-layer containment check must
// reject: blank, "." and "..", forward/backslash separators (both
// platforms), absolute paths, and parent-relative paths.
var unsafeNames = []string{
	"",
	"   ",
	".",
	"..",
	"../escape",
	"..\\escape",
	"..\\..\\escape",
	"a/b",
	"a\\b",
	"/abs",
	"abs/",
	"a/../b",
}

func TestValidateSafeName_RejectsUnsafeNames(t *testing.T) {
	for _, name := range unsafeNames {
		if err := ValidateSafeName(name); err == nil {
			t.Errorf("ValidateSafeName(%q) = nil, want error", name)
		}
	}
}

func TestValidateSafeName_AcceptsNormalNames(t *testing.T) {
	for _, name := range []string{
		"my-preset",
		"test-agent",
		"foo.bar",
		"hello world",
		"preset-1",
		"中文-agent",
	} {
		if err := ValidateSafeName(name); err != nil {
			t.Errorf("ValidateSafeName(%q) = %v, want nil", name, err)
		}
	}
}

func TestSave_RejectsUnsafeNamesAndWritesNothingOutside(t *testing.T) {
	withTempPresets(t, func() {
		for _, name := range unsafeNames {
			p := DefaultPreset()
			p.Name = name
			if err := Save(p); err == nil {
				t.Errorf("Save(name=%q) = nil, want error", name)
			}
			// The would-be join target must not exist anywhere.
			if _, err := os.Lstat(filepath.Join(SavedDir(), name+".json")); err == nil {
				t.Errorf("Save(name=%q) created an artifact at the join target", name)
			}
		}
		// A "../escape" name would target presets/escape.json, just outside
		// saved/; it must not have been written.
		if _, err := os.Lstat(filepath.Join(PresetsDir(), "escape.json")); err == nil {
			t.Errorf("Save wrote an escape artifact outside saved/")
		}
	})
}

func TestSave_AcceptsNormalName(t *testing.T) {
	withTempPresets(t, func() {
		p := DefaultPreset()
		p.Name = "my-saved-preset"
		if err := Save(p); err != nil {
			t.Fatalf("Save(normal name) error: %v", err)
		}
		if _, err := os.Stat(filepath.Join(SavedDir(), "my-saved-preset.json")); err != nil {
			t.Errorf("normal-name preset not written: %v", err)
		}
	})
}

func TestDelete_RejectsUnsafeNamesAndDoesNotTouchOutsideFiles(t *testing.T) {
	withTempPresets(t, func() {
		// Plant a decoy exactly where a "../escape" delete would target:
		// presets/escape.json (just outside saved/). It must survive.
		decoy := filepath.Join(PresetsDir(), "escape.json")
		if err := os.MkdirAll(PresetsDir(), 0o755); err != nil {
			t.Fatalf("mkdir presets: %v", err)
		}
		if err := os.WriteFile(decoy, []byte("{}"), 0o644); err != nil {
			t.Fatalf("write decoy: %v", err)
		}

		for _, name := range unsafeNames {
			if err := Delete(name); err == nil {
				t.Errorf("Delete(name=%q) = nil, want error", name)
			}
		}

		if _, err := os.Stat(decoy); err != nil {
			t.Errorf("decoy %s was removed by an unsafe Delete: %v", decoy, err)
		}
	})
}

func TestGenerateInitJSONWithOpts_RejectsUnsafeDirName(t *testing.T) {
	for _, name := range unsafeNames {
		tmp := t.TempDir()
		lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
		if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
			t.Fatalf("mkdir lingtai: %v", err)
		}
		globalDir := filepath.Join(tmp, "global")

		err := GenerateInitJSONWithOpts(DefaultPreset(), "alice", name, lingtaiDir, globalDir, DefaultAgentOpts())
		if err == nil {
			t.Errorf("GenerateInitJSONWithOpts(dirName=%q) = nil, want error", name)
		}
		// The would-be joined agent dir must not exist: check for an init.json
		// at the join target, which an escaped write would have produced
		// (plain "", "." and ".." resolve to already-existing ancestors, so
		// only their subpaths are meaningful).
		if _, err := os.Lstat(filepath.Join(lingtaiDir, name, "init.json")); err == nil {
			t.Errorf("GenerateInitJSONWithOpts(dirName=%q) created an artifact at the join target", name)
		}
		// A "../escape" dirName would target <project>/escape (outside
		// .lingtai/); it must not exist.
		if _, err := os.Lstat(filepath.Join(tmp, "project", "escape")); err == nil {
			t.Errorf("GenerateInitJSONWithOpts created an agent dir outside .lingtai/")
		}
	}
}

func TestGenerateInitJSONWithOpts_AcceptsNormalDirName(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, "project", ".lingtai")
	if err := os.MkdirAll(lingtaiDir, 0o755); err != nil {
		t.Fatalf("mkdir lingtai: %v", err)
	}
	globalDir := filepath.Join(tmp, "global")

	if err := GenerateInitJSONWithOpts(DefaultPreset(), "alice", "alice", lingtaiDir, globalDir, DefaultAgentOpts()); err != nil {
		t.Fatalf("GenerateInitJSONWithOpts(normal dirName) error: %v", err)
	}
	if _, err := os.Stat(filepath.Join(lingtaiDir, "alice", "init.json")); err != nil {
		t.Errorf("init.json not written for normal dirName: %v", err)
	}
}
