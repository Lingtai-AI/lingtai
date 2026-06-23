package globalmigrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeMeta writes a meta.json with the given version into globalDir.
func writeMeta(t *testing.T, globalDir string, version int) {
	t.Helper()
	data, err := json.Marshal(metaFile{Version: version})
	if err != nil {
		t.Fatalf("marshal meta: %v", err)
	}
	if err := os.WriteFile(filepath.Join(globalDir, "meta.json"), data, 0o644); err != nil {
		t.Fatalf("write meta.json: %v", err)
	}
}

// readMetaVersion reads the persisted version from meta.json.
func readMetaVersion(t *testing.T, globalDir string) int {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(globalDir, "meta.json"))
	if err != nil {
		t.Fatalf("read meta.json: %v", err)
	}
	var m metaFile
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse meta.json: %v", err)
	}
	return m.Version
}

// TestRunDoesNotTouchFlatPresetFiles is the regression test for the
// preset-loss incident: the doctor-triggered globalmigrate version 2
// ("split-presets-dir") used to move flat ~/.lingtai-tui/presets/*.json
// files into templates/ or saved/, and on a destination collision it
// silently DELETED the source. Combined with preset.Bootstrap rewriting
// templates, this destroyed user presets — especially built-in-stem
// names like zhipu.json / mimo.json / deepseek.json.
//
// After the fix, version 2 is a neutralized no-op tombstone, so running
// the full migration set against a flat presets dir must leave every
// flat preset file exactly where it was, with its original contents, and
// must NOT create the templates/ or saved/ subdirs.
func TestRunDoesNotTouchFlatPresetFiles(t *testing.T) {
	globalDir := t.TempDir()
	presetsDir := filepath.Join(globalDir, "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatalf("mkdir presets: %v", err)
	}

	// Flat preset files, including built-in-stem names that the old
	// destructive migration would have classified as templates and
	// then clobbered/deleted.
	files := map[string]string{
		"zhipu.json":        `{"name":"zhipu","user_edit":"keep-me-zhipu"}`,
		"mimo.json":         `{"name":"mimo","user_edit":"keep-me-mimo"}`,
		"deepseek.json":     `{"name":"deepseek","user_edit":"keep-me-deepseek"}`,
		"codex.json":        `{"name":"codex","user_edit":"keep-me-codex"}`,
		"custom.json":       `{"name":"custom","user_edit":"keep-me-custom"}`,
		"my_own.json":       `{"name":"my_own","user_edit":"keep-me-saved"}`,
		"_kernel_meta.json": `{"version":1}`,
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(presetsDir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	// Start below version 2 so the v2 migration is eligible to run.
	writeMeta(t, globalDir, 1)

	Run(globalDir)

	// Every flat file must still exist at its original path with its
	// original contents — not moved, not deleted, not overwritten.
	for name, want := range files {
		got, err := os.ReadFile(filepath.Join(presetsDir, name))
		if err != nil {
			t.Fatalf("flat preset %s was removed or moved by migration: %v", name, err)
		}
		if string(got) != want {
			t.Errorf("flat preset %s was overwritten: got %q want %q", name, got, want)
		}
	}

	// The destructive migration created templates/ and saved/ subdirs.
	// The neutralized version must not create them.
	for _, sub := range []string{"templates", "saved"} {
		if _, err := os.Stat(filepath.Join(presetsDir, sub)); err == nil {
			t.Errorf("migration created presets/%s subdir; expected no-op", sub)
		}
	}
}

// TestRunPreservesFlatPresetOnTemplateCollision targets the exact code
// path that caused the data loss: the old moveFile deleted the flat source
// file (os.Remove(src)) whenever the destination already existed. Here a
// templates/zhipu.json already exists alongside a flat zhipu.json. The old
// migration would have deleted the flat file; the neutralized version must
// leave both untouched.
func TestRunPreservesFlatPresetOnTemplateCollision(t *testing.T) {
	globalDir := t.TempDir()
	presetsDir := filepath.Join(globalDir, "presets")
	templatesDir := filepath.Join(presetsDir, "templates")
	if err := os.MkdirAll(templatesDir, 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}

	flatBody := `{"name":"zhipu","user_edit":"precious-flat-copy"}`
	tmplBody := `{"name":"zhipu","source":"template"}`
	if err := os.WriteFile(filepath.Join(presetsDir, "zhipu.json"), []byte(flatBody), 0o644); err != nil {
		t.Fatalf("write flat zhipu.json: %v", err)
	}
	if err := os.WriteFile(filepath.Join(templatesDir, "zhipu.json"), []byte(tmplBody), 0o644); err != nil {
		t.Fatalf("write template zhipu.json: %v", err)
	}

	writeMeta(t, globalDir, 1)

	Run(globalDir)

	got, err := os.ReadFile(filepath.Join(presetsDir, "zhipu.json"))
	if err != nil {
		t.Fatalf("flat zhipu.json deleted on template collision (the original bug): %v", err)
	}
	if string(got) != flatBody {
		t.Errorf("flat zhipu.json mutated: got %q want %q", got, flatBody)
	}
	// The pre-existing template file must also be left exactly as-is.
	gotTmpl, err := os.ReadFile(filepath.Join(templatesDir, "zhipu.json"))
	if err != nil {
		t.Fatalf("template zhipu.json removed: %v", err)
	}
	if string(gotTmpl) != tmplBody {
		t.Errorf("template zhipu.json mutated: got %q want %q", gotTmpl, tmplBody)
	}
}

// TestRunAdvancesVersionToCurrent verifies version-advancement semantics
// are preserved after neutralizing v2: a machine at version 1 still
// advances to CurrentVersion, and the persisted meta.json reflects it.
func TestRunAdvancesVersionToCurrent(t *testing.T) {
	globalDir := t.TempDir()
	writeMeta(t, globalDir, 1)

	Run(globalDir)

	if got := readMetaVersion(t, globalDir); got != CurrentVersion {
		t.Errorf("version after Run = %d, want CurrentVersion %d", got, CurrentVersion)
	}
}

// TestRunFromVersionZeroAdvances verifies a fresh machine (no meta.json
// at all, treated as version 0) advances to CurrentVersion without
// running any destructive preset migration.
func TestRunFromVersionZeroAdvances(t *testing.T) {
	globalDir := t.TempDir()
	// No meta.json written — loadMeta treats this as version 0.

	Run(globalDir)

	if got := readMetaVersion(t, globalDir); got != CurrentVersion {
		t.Errorf("version after Run = %d, want CurrentVersion %d", got, CurrentVersion)
	}
}

// TestRunAlreadyCurrentIsNoop verifies a machine already at CurrentVersion
// short-circuits and does not touch a flat presets dir.
func TestRunAlreadyCurrentIsNoop(t *testing.T) {
	globalDir := t.TempDir()
	presetsDir := filepath.Join(globalDir, "presets")
	if err := os.MkdirAll(presetsDir, 0o755); err != nil {
		t.Fatalf("mkdir presets: %v", err)
	}
	body := `{"name":"zhipu","user_edit":"keep-me"}`
	if err := os.WriteFile(filepath.Join(presetsDir, "zhipu.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write zhipu.json: %v", err)
	}
	writeMeta(t, globalDir, CurrentVersion)

	Run(globalDir)

	got, err := os.ReadFile(filepath.Join(presetsDir, "zhipu.json"))
	if err != nil {
		t.Fatalf("zhipu.json removed when already at CurrentVersion: %v", err)
	}
	if string(got) != body {
		t.Errorf("zhipu.json overwritten when already at CurrentVersion: got %q", got)
	}
}
