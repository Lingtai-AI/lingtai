package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// --- Invariant 1: zero-write gate -------------------------------------------

// TestProbeNoProjectPure_DoesNotCreateAnything proves ProbeNoProjectPure is
// truly read-only: calling it on a directory with no .lingtai/ must not
// create the project dir, the .lingtai dir, or anything else.
func TestProbeNoProjectPure_DoesNotCreateAnything(t *testing.T) {
	root := t.TempDir()
	before := dirSnapshot(t, root)

	noProject, err := ProbeNoProjectPure(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !noProject {
		t.Fatal("expected ProbeNoProjectPure to report no project for an empty directory")
	}

	after := dirSnapshot(t, root)
	assertSnapshotsEqual(t, "ProbeNoProjectPure on empty dir", before, after)
}

// TestProbeNoProjectPure_DetectsExistingLingtai proves the probe correctly
// reports "has project" without following/creating through a real .lingtai
// directory, and without mutating it.
func TestProbeNoProjectPure_DetectsExistingLingtai(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".lingtai"), 0o755); err != nil {
		t.Fatal(err)
	}
	before := dirSnapshot(t, root)

	noProject, err := ProbeNoProjectPure(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if noProject {
		t.Fatal("expected ProbeNoProjectPure to report a project when .lingtai/ exists")
	}

	after := dirSnapshot(t, root)
	assertSnapshotsEqual(t, "ProbeNoProjectPure with existing .lingtai", before, after)
}

// TestProbeNoProjectPure_SymlinkCountsAsExists proves Lstat semantics: a
// symlink AT .lingtai (even a dangling one) counts as "project exists" and
// is never followed or created through. This is the exact scenario
// Invariant 1 calls out: os.Lstat, never os.Stat.
func TestProbeNoProjectPure_SymlinkCountsAsExists(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(t.TempDir(), "does-not-exist")
	linkPath := filepath.Join(root, ".lingtai")
	if err := os.Symlink(target, linkPath); err != nil {
		t.Skipf("symlink not supported in this environment: %v", err)
	}

	noProject, err := ProbeNoProjectPure(root)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if noProject {
		t.Fatal("expected a symlink at .lingtai to count as \"project exists\" (Lstat semantics)")
	}
}

// TestProbeNoProjectPure_FailsClosedOnNonNotExistError proves a genuine
// Lstat error that is NOT os.IsNotExist (e.g. a parent directory with its
// execute/search bit removed, making the path unstatable for permission
// reasons rather than absence) is surfaced as an error rather than silently
// folded into "project exists" — the exact fail-open defect this function
// exists to close. On success the probe must fail closed: callers see a
// non-nil error and must exit before any write, never guess either bool
// value.
func TestProbeNoProjectPure_FailsClosedOnNonNotExistError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root bypasses directory permission checks — cannot exercise this path")
	}
	root := t.TempDir()
	blocked := filepath.Join(root, "blocked")
	if err := os.MkdirAll(blocked, 0o755); err != nil {
		t.Fatal(err)
	}
	// Remove the search (execute) bit on the parent so Lstat on a child path
	// fails with permission-denied, NOT not-exist.
	if err := os.Chmod(blocked, 0o000); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(blocked, 0o755) // restore so t.TempDir() cleanup can remove it

	target := filepath.Join(blocked, "child")
	noProject, err := ProbeNoProjectPure(target)
	if err == nil {
		t.Fatalf("expected a non-nil error for an unstatable path due to permissions, got noProject=%v, err=nil", noProject)
	}
	if noProject {
		t.Fatalf("expected noProject=false alongside a non-nil error (fail closed), got noProject=true, err=%v", err)
	}
}

// TestGlobalDirPath_NeverCreatesDirectory proves the pure path resolver
// never mkdirs ~/.lingtai-tui, in contrast to GlobalDir/EnsureGlobalDir.
func TestGlobalDirPath_NeverCreatesDirectory(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	path, err := config.GlobalDirPath()
	if err != nil {
		t.Fatalf("GlobalDirPath: %v", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("GlobalDirPath must not create %s; stat err = %v", path, statErr)
	}

	// Contrast: EnsureGlobalDir (the explicit mutating counterpart) DOES
	// create it, proving the split is real and not merely relabeled.
	ensured, err := config.EnsureGlobalDir()
	if err != nil {
		t.Fatalf("EnsureGlobalDir: %v", err)
	}
	if ensured != path {
		t.Fatalf("EnsureGlobalDir path %q != GlobalDirPath %q", ensured, path)
	}
	if _, statErr := os.Stat(path); statErr != nil {
		t.Fatalf("EnsureGlobalDir should have created %s: %v", path, statErr)
	}
}

// TestListRegisteredProjects_NeverPrunes proves the read-only registry list
// leaves registry.jsonl byte-identical even when it contains a stale entry
// pointing at a missing project — unlike LoadAndPrune, which rewrites the
// file. This is the exact contract the design doc calls out: launcher
// browse must never call LoadAndPrune.
func TestListRegisteredProjects_NeverPrunes(t *testing.T) {
	globalDir := t.TempDir()
	stale := filepath.Join(t.TempDir(), "gone")
	live := t.TempDir()
	if err := os.MkdirAll(filepath.Join(live, ".lingtai"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := config.Register(globalDir, stale); err != nil {
		t.Fatalf("register stale: %v", err)
	}
	if err := config.Register(globalDir, live); err != nil {
		t.Fatalf("register live: %v", err)
	}

	regPath := filepath.Join(globalDir, "registry.jsonl")
	before, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("read registry: %v", err)
	}

	rows := config.ListRegisteredProjects(globalDir)

	after, err := os.ReadFile(regPath)
	if err != nil {
		t.Fatalf("read registry after list: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("ListRegisteredProjects mutated registry.jsonl:\nbefore=%q\nafter=%q", before, after)
	}

	var sawStale, sawLive bool
	for _, r := range rows {
		if r.Path == stale {
			sawStale = true
			if r.Alive {
				t.Errorf("stale entry %q reported Alive=true", stale)
			}
			if r.StaleReason == "" {
				t.Errorf("stale entry %q has no StaleReason", stale)
			}
		}
		if r.Path == live {
			sawLive = true
			if !r.Alive {
				t.Errorf("live entry %q reported Alive=false", live)
			}
		}
	}
	if !sawStale || !sawLive {
		t.Fatalf("expected both stale and live entries in ListRegisteredProjects result, got %+v", rows)
	}
}

// TestLauncherRootModel_CreateEntryDoesNotTouchExistingConfigFile drives the
// FULL production entry path — NewLauncherRootModel construction, landing
// Enter on "Create new project" (which internally calls enterCreate ->
// NewDraftFirstRunModel -> FirstRunModel.Init()) — with a config.json
// PRE-SEEDED at 0644 (the pre-migration permission LoadConfig's chmod
// migration targets), and proves the file's CONTENT, MODE, and the overall
// path set under the isolated global dir are all byte-for-byte unchanged
// afterward.
//
// This is the exact scenario a parent review flagged: NewDraftFirstRunModel
// used to call the public NewFirstRunModel, whose shared body
// unconditionally called config.LoadConfig — which chmods an existing
// 0644 config.json to 0600 as a permission-tightening migration side
// effect, entirely independent of draftMode. A content-only snapshot (the
// shape every earlier purity test in this file used) would never have
// caught that: the JSON bytes stay identical, only the file's mode bit
// changes. dirSnapshot now folds mode into its hash specifically so this
// test (and every earlier one) proves both.
func TestLauncherRootModel_CreateEntryDoesNotTouchExistingConfigFile(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalDir := filepath.Join(home, ".lingtai-tui")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(globalDir, "config.json")
	seedContent := []byte(`{"keys":{"MINIMAX_API_KEY":"sk-existing-real-key"}}`)
	if err := os.WriteFile(configPath, seedContent, 0o644); err != nil {
		t.Fatal(err)
	}
	// Confirm the seed actually landed at 0644 before proceeding — if the
	// OS/umask silently coerced this, the test would pass vacuously.
	if info, err := os.Stat(configPath); err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("failed to seed config.json at 0644, got mode %v (err=%v)", info, err)
	}

	projectRoot := t.TempDir()
	before := dirSnapshot(t, home)

	m := NewLauncherRootModel(projectRoot, globalDir, "")
	updated, _ := m.updateChoose(tea.KeyPressMsg{Code: tea.KeyEnter}) // chooseCursor defaults to 0 = "Start a new project in this folder"
	lm := updated.(LauncherRootModel)
	if lm.view != launcherViewCreate || !lm.firstRunOn {
		t.Fatalf("expected choose Enter to enter the create flow, got view=%v firstRunOn=%v", lm.view, lm.firstRunOn)
	}
	// Init() itself — proves the constructor's config.LoadConfigReadOnly
	// branch (not just "some code ran without visibly changing content")
	// really is the read-only path, matching how main.go's own launcher
	// program would call Init() before the first Update.
	lm.firstRun.Init()

	after := dirSnapshot(t, home)
	assertSnapshotsEqual(t, "launcher Create entry with pre-existing 0644 config.json", before, after)

	// Belt-and-braces: assert the mode explicitly too, not just via the
	// snapshot diff, so a future dirSnapshot refactor that accidentally
	// drops mode-tracking still has a direct assertion here.
	info, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config.json disappeared: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("config.json mode changed from 0644 to %v — the launcher's Create entry path touched a file it must never write", info.Mode().Perm())
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("read config.json: %v", err)
	}
	if string(data) != string(seedContent) {
		t.Fatalf("config.json content changed:\nbefore=%s\nafter=%s", seedContent, data)
	}
}

// --- Draft purity: FirstRunModel in draftMode must not write -------------

// buildDraftModel constructs a draft-purpose FirstRunModel with HOME
// isolated to a temp dir, so any accidental disk write (SaveConfig,
// preset.Save, codex-auth.json, tui_config.json) is observable via a
// directory snapshot of that HOME.
func buildDraftModel(t *testing.T) (FirstRunModel, string, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	globalDir := filepath.Join(home, ".lingtai-tui")
	draft := NewProjectDraft(projectRoot)
	m := NewDraftFirstRunModel(filepath.Join(projectRoot, ".lingtai"), globalDir, false, draft)
	return m, home, projectRoot
}

// TestLauncherPrelude_ThemeLanguageDoNotPersist proves the launcher's
// welcome prelude — which now owns theme (ctrl+t) and language (↑↓)
// selection for the no-project flow — holds both choices in memory only,
// never touching tui_config.json (or anything else under HOME), and seeds
// them onto the ProjectDraft when the user proceeds into Create.
func TestLauncherPrelude_ThemeLanguageDoNotPersist(t *testing.T) {
	t.Cleanup(func() {
		SetThemeByName(DefaultThemeName)
		_ = i18n.SetLang("en")
	})
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	globalDir := filepath.Join(home, ".lingtai-tui")

	m := NewLauncherRootModel(projectRoot, globalDir, "")
	before := dirSnapshot(t, home)

	updated, _ := m.updateWelcome(tea.KeyPressMsg{Text: "ctrl+t"})
	lm := updated.(LauncherRootModel)
	updated, _ = lm.updateWelcome(tea.KeyPressMsg{Code: tea.KeyDown}) // en -> zh, live i18n preview
	lm = updated.(LauncherRootModel)
	updated, _ = lm.updateWelcome(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm = updated.(LauncherRootModel)
	if lm.view != launcherViewChoose {
		t.Fatalf("expected welcome Enter to reach the choose page, got view=%v", lm.view)
	}
	updated, _ = lm.updateChoose(tea.KeyPressMsg{Code: tea.KeyEnter}) // cursor 0 = start here
	lm = updated.(LauncherRootModel)

	after := dirSnapshot(t, home)
	assertSnapshotsEqual(t, "launcher prelude theme/language", before, after)

	if lm.draft == nil {
		t.Fatal("expected Create entry to build a draft")
	}
	if lm.draft.Theme != "xuan-paper" {
		t.Fatalf("expected the prelude's cycled theme seeded on the draft, got %q", lm.draft.Theme)
	}
	if lm.draft.Language != "zh" {
		t.Fatalf("expected the prelude's selected language seeded on the draft, got %q", lm.draft.Language)
	}
	if i18n.Lang() != "zh" {
		t.Fatalf("expected the language preview applied in-memory, got %q", i18n.Lang())
	}
}

// TestDraftFirstRun_PresetEditorCommitDoesNotPersist proves
// PresetEditorCommitMsg in draftMode never calls config.SaveConfig or
// preset.Save — both the API key and the edited preset must land in
// m.draft only.
func TestDraftFirstRun_PresetEditorCommitDoesNotPersist(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	m.step = stepPickPreset
	before := dirSnapshot(t, home)

	commit := PresetEditorCommitMsg{
		Preset: preset.Preset{
			Name: "minimax",
			Manifest: map[string]interface{}{
				"llm": map[string]interface{}{
					"provider":    "minimax",
					"api_key_env": "MINIMAX_API_KEY",
				},
			},
		},
		APIKeySet: true,
		APIKey:    "sk-draft-key",
	}
	m, _ = m.Update(commit)

	after := dirSnapshot(t, home)
	assertSnapshotsEqual(t, "draft preset editor commit", before, after)

	if m.draft == nil || m.draft.DraftPreset == nil {
		t.Fatal("expected draft preset to be captured")
	}
	if !m.draft.DraftAPIKey.Empty() {
		t.Fatal("last-edited key reached ProjectDraft before a preset was selected for Review")
	}
	m, _ = m.enterReviewStep("", "")
	if m.draft.DraftAPIKeyEnv != "MINIMAX_API_KEY" || m.draft.DraftAPIKey.Reveal() != "sk-draft-key" {
		t.Fatalf("review-selected draft key = env %q value %q", m.draft.DraftAPIKeyEnv, m.draft.DraftAPIKey.Reveal())
	}

	// draft.ExistingKeys must record presence only, never an alias of the
	// live m.existingKeys map — this is the exact production path (the
	// preset editor commit call site in firstrun.go) a parent review found
	// assigning the real key map directly onto the exported draft field.
	// keyPresenceValue carries no payload at all (see project_draft.go), so
	// there is no "real value" to compare against — presence is the only
	// thing to assert.
	if _, ok := m.draft.ExistingKeys["MINIMAX_API_KEY"]; !ok {
		t.Fatal("expected MINIMAX_API_KEY presence recorded in draft.ExistingKeys")
	}
	// Confirm it's a genuinely separate map, not an alias of m.existingKeys:
	// mutating/removing the live model's real key field must never
	// retroactively change what's already in the draft.
	delete(m.existingKeys, "MINIMAX_API_KEY")
	if _, ok := m.draft.ExistingKeys["MINIMAX_API_KEY"]; !ok {
		t.Fatal("draft.ExistingKeys shares backing storage with the live existingKeys map — must be an independent copy, not an alias")
	}
}

// TestDraftFirstRun_DeleteKeyNeverDeletesSavedPreset proves the stepPickPreset
// backspace/delete handler NEVER calls preset.Delete while draftMode is true
// — a parent review found this branch had NO draftMode guard at all, so a
// user merely browsing the picker during a new-project draft session (one
// that may never even commit) could permanently delete one of their own
// real saved presets. Creates a real saved preset on disk, sends the ACTUAL
// "backspace" key through the draft model with the cursor pointed at it (not
// a synthetic direct call to preset.Delete), and proves the file survives
// byte-for-byte while a localized "blocked" status is shown.
func TestDraftFirstRun_DeleteKeyNeverDeletesSavedPreset(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	savedPreset := preset.Preset{
		Name: "my-saved-preset",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_API_KEY",
			},
		},
	}
	if err := preset.Save(savedPreset); err != nil {
		t.Fatalf("seed saved preset: %v", err)
	}
	presetPath := filepath.Join(preset.SavedDir(), "my-saved-preset.json")
	before, err := os.ReadFile(presetPath)
	if err != nil {
		t.Fatalf("read seeded preset: %v", err)
	}

	projectRoot := t.TempDir()
	globalDir := filepath.Join(home, ".lingtai-tui")
	draft := NewProjectDraft(projectRoot)
	m := NewDraftFirstRunModel(filepath.Join(projectRoot, ".lingtai"), globalDir, true, draft)
	m.step = stepPickPreset
	m.presets, _ = preset.List()

	cursor := -1
	for i, p := range m.presets {
		if p.Name == "my-saved-preset" {
			cursor = i
			break
		}
	}
	if cursor < 0 {
		t.Fatalf("seeded preset not found in m.presets: %+v", m.presets)
	}
	m.cursor = cursor

	homeBefore := dirSnapshot(t, home)

	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

	homeAfter := dirSnapshot(t, home)
	assertSnapshotsEqual(t, "draft delete-key on saved preset", homeBefore, homeAfter)

	after, err := os.ReadFile(presetPath)
	if err != nil {
		t.Fatalf("preset file was deleted despite draftMode: %v", err)
	}
	if string(before) != string(after) {
		t.Fatalf("preset file content changed:\nbefore=%s\nafter=%s", before, after)
	}

	// The preset must still be listed too (not just the file surviving on
	// disk — m.presets itself must be unchanged, proving the handler
	// returned before doing anything, not merely before the disk write).
	stillPresent := false
	for _, p := range m.presets {
		if p.Name == "my-saved-preset" {
			stillPresent = true
		}
	}
	if !stillPresent {
		t.Fatal("preset was removed from m.presets despite draftMode blocking the delete")
	}

	if m.message == "" {
		t.Fatal("expected a status message explaining the delete was blocked in draft mode")
	}
}

// TestDraftFirstRun_EditPresetAThenSelectPresetBFinalizesB drives the exact
// realistic flow a parent review found broken: edit preset A (committing it
// via the REAL PresetEditorCommitMsg the preset editor sends, which sets
// m.draft.DraftPreset=&A and m.draft.DraftPresetDirty=true), then move the
// cursor via REAL "down"/"up" keypresses at stepPickPreset to a DIFFERENT,
// never-edited preset B, then call enterReviewStep (the function under
// test). The old code only re-captured the cursor-current preset when
// m.draft.DraftPreset was nil — so once A had been committed, B would
// silently never be captured and A's edit (with its dirty flag) would ride
// all the way to the finalizer even though the user had moved on to B.
func TestDraftFirstRun_EditPresetAThenSelectPresetBFinalizesB(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	m.step = stepPickPreset
	m.presets = []preset.Preset{
		{
			Name:        "preset-a",
			Description: preset.PresetDescription{Summary: "preset A"},
			Manifest: map[string]interface{}{
				"llm": map[string]interface{}{"provider": "minimax", "model": "a-model", "api_key_env": "PRESET_A_KEY"},
			},
		},
		{
			Name:        "preset-b",
			Description: preset.PresetDescription{Summary: "preset B"},
			Manifest: map[string]interface{}{
				"llm": map[string]interface{}{"provider": "minimax", "model": "b-model", "api_key_env": "PRESET_B_KEY"},
			},
		},
	}
	m.cursor = 0 // starts on preset-a

	before := dirSnapshot(t, home)

	// 1. Edit preset A via the REAL PresetEditorCommitMsg (exactly what the
	// preset editor sends on save) — splices the edited copy into
	// m.presets[0], sets m.cursor=0, m.draft.DraftPreset=&editedA,
	// m.draft.DraftPresetDirty=true, and m.draftEditedPresetIdx=0.
	editedA := m.presets[0]
	editedA.Manifest["llm"].(map[string]interface{})["model"] = "a-model-edited"
	commit := PresetEditorCommitMsg{Preset: editedA, APIKeySet: true, APIKey: "draft-a-key"}
	m, _ = m.Update(commit)

	if m.draft == nil || m.draft.DraftPreset == nil || m.draft.DraftPreset.Name != "preset-a" {
		t.Fatalf("expected preset-a captured as the dirty draft preset after edit, got %+v", m.draft)
	}
	if !m.draft.DraftPresetDirty {
		t.Fatal("expected DraftPresetDirty=true immediately after editing preset-a")
	}
	if m.cursor != 0 || m.draftEditedPresetIdx != 0 {
		t.Fatalf("expected cursor and draftEditedPresetIdx both at 0 after editing preset-a, got cursor=%d draftEditedPresetIdx=%d", m.cursor, m.draftEditedPresetIdx)
	}

	// 2. Move the cursor to preset-b via a REAL "down" keypress at
	// stepPickPreset — the actual user action of browsing away from the
	// just-edited preset without re-editing.
	if m.step != stepPickPreset {
		t.Fatalf("expected to still be on stepPickPreset after the editor commit, got %v", m.step)
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	if m.cursor != 1 {
		t.Fatalf("expected cursor=1 (preset-b) after down key, got %d", m.cursor)
	}

	assertSnapshotsEqual(t, "edit A then navigate to B", before, dirSnapshot(t, home))

	// 3. Enter Review — the function under test. Cursor (1) no longer
	// matches draftEditedPresetIdx (0), so this must resolve FRESH from the
	// cursor rather than keep the stale edited preset-a.
	m, _ = m.enterReviewStep("", "")

	if m.step != stepReview {
		t.Fatalf("expected stepReview, got %v", m.step)
	}
	if m.draft.DraftPreset == nil {
		t.Fatal("expected a resolved DraftPreset entering Review")
	}
	if m.draft.DraftPreset.Name != "preset-b" {
		t.Fatalf("expected preset-b to be the resolved/finalized preset, got %q — the stale edited preset-a leaked through", m.draft.DraftPreset.Name)
	}
	if model := presetModelName(*m.draft.DraftPreset); model != "b-model" {
		t.Fatalf("expected resolved preset's model to be preset-b's own (\"b-model\"), got %q", model)
	}
	if m.draft.DraftPresetDirty {
		t.Fatal("expected DraftPresetDirty=false for preset-b — it was never edited, so the finalizer must not attempt to Save it")
	}
	if !m.draft.DraftAPIKey.Empty() || m.draft.DraftAPIKeyEnv != "" {
		t.Fatalf("preset-a key leaked into preset-b review: env=%q key=%q", m.draft.DraftAPIKeyEnv, m.draft.DraftAPIKey.Reveal())
	}

	// preset-a's edit must not have silently leaked into the resolved
	// preset-b's manifest — confirm the model string is genuinely B's own,
	// not A's edited value under B's name.
	if model := presetModelName(*m.draft.DraftPreset); model == "a-model-edited" {
		t.Fatal("preset-a's edited state was incorrectly applied to preset-b")
	}

	// Returning to A in the same live draft recovers A's pending key without
	// ever having persisted it while B was selected.
	m.cursor = 0
	m, _ = m.enterReviewStep("", "")
	if m.draft.DraftPreset == nil || m.draft.DraftPreset.Name != "preset-a" {
		t.Fatalf("reselected preset = %+v, want preset-a", m.draft.DraftPreset)
	}
	if m.draft.DraftAPIKeyEnv != "PRESET_A_KEY" || m.draft.DraftAPIKey.Reveal() != "draft-a-key" {
		t.Fatalf("reselected preset-a key = env %q value %q", m.draft.DraftAPIKeyEnv, m.draft.DraftAPIKey.Reveal())
	}

	assertSnapshotsEqual(t, "after selected-key navigation", before, dirSnapshot(t, home))
}

func TestDraftFirstRun_EmptyPresetEditorKeyDoesNotDeleteSharedKey(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	m.step = stepPickPreset
	p := preset.Preset{
		Name: "shared-key-preset",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"model":       "test-model",
				"api_key_env": "SHARED_API_KEY",
			},
		},
	}
	m.presets = []preset.Preset{p}
	m.cursor = 0
	m.existingKeys["SHARED_API_KEY"] = "saved-secret"
	before := dirSnapshot(t, home)

	m, _ = m.Update(PresetEditorCommitMsg{Preset: p, APIKeySet: true, APIKey: ""})

	if got := m.existingKeys["SHARED_API_KEY"]; got != "saved-secret" {
		t.Fatalf("shared API key after empty draft edit = %q, want unchanged", got)
	}
	if _, ok := m.draftPendingAPIKeys["SHARED_API_KEY"]; ok {
		t.Fatal("empty draft key edit created a pending persistence value")
	}
	if m.message != i18n.T("firstrun.preset_pick.draft_key_delete_blocked") {
		t.Fatalf("message = %q, want localized blocked-delete message", m.message)
	}
	assertSnapshotsEqual(t, "empty draft key edit", before, dirSnapshot(t, home))
}

// --- Blocker 5: draft Codex Delete must not lie about pre-existing auth ----

// TestDraftFirstRun_CodexDeleteBlockedForPreExistingAuth proves the exact
// defect a parent review found: with REAL pre-existing global Codex auth on
// disk (seeded BEFORE the draft model is even constructed, so
// refreshCodexAuth's unconditional read of the real codex-auth.json — see
// newFirstRunModelForPurpose's doc comment — is what makes m.codexAuth.valid
// true here, exactly mirroring a user who logged into Codex before ever
// starting this project draft), the draft Delete/logout action must:
//  1. never mutate the real codex-auth.json file at all,
//  2. never show a false "logged out" status,
//  3. show a clear localized message that the action is blocked/deferred.
//
// Drives the ACTUAL two-press Del/backspace sequence at the Codex row (not
// a synthetic direct field mutation).
func TestDraftFirstRun_CodexDeleteBlockedForPreExistingAuth(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalDir := filepath.Join(home, ".lingtai-tui")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir globalDir: %v", err)
	}
	authPath := legacyCodexAuthPath(globalDir)
	seedTokens := CodexTokens{
		AccessToken:  "real-access-token",
		RefreshToken: "real-refresh-token",
		Email:        "user@example.com",
	}
	seedData, err := json.Marshal(seedTokens)
	if err != nil {
		t.Fatalf("marshal seed tokens: %v", err)
	}
	if err := os.WriteFile(authPath, seedData, 0o600); err != nil {
		t.Fatalf("seed real codex-auth.json: %v", err)
	}

	projectRoot := t.TempDir()
	draft := NewProjectDraft(projectRoot)
	// Constructed AFTER seeding the real auth file — refreshCodexAuth (run
	// inside the constructor) reads it here, exactly as it would for a real
	// user who already had Codex configured before opening the launcher.
	m := NewDraftFirstRunModel(filepath.Join(projectRoot, ".lingtai"), globalDir, false, draft)
	m.step = stepPickPreset
	m.presets = nil // empty picker list -> pickCodexAuthIdx (visibleCount) == 0
	m.cursor = 0

	if !m.codexAuth.valid {
		t.Fatal("expected codexAuth.valid=true from the real pre-existing auth file")
	}
	if !draft.DraftCodexTokens.Empty() {
		t.Fatal("sanity: draft.DraftCodexTokens must start empty — this draft session performed no login of its own")
	}

	homeBefore := dirSnapshot(t, home)

	// First Del press: arm.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if m.message == "" {
		t.Fatal("expected a status message after the first Del press")
	}
	if m.codexLogoutArmed {
		t.Fatal("expected the destructive logout to be blocked before it can even arm, for pre-existing draft auth")
	}
	blockedMsg := m.message

	// Second Del press (mirroring the normal two-press confirm gesture) must
	// remain blocked, not slip through as a confirm.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

	homeAfter := dirSnapshot(t, home)
	assertSnapshotsEqual(t, "draft Codex delete on pre-existing auth", homeBefore, homeAfter)

	rawAfter, err := os.ReadFile(authPath)
	if err != nil {
		t.Fatalf("real codex-auth.json must survive: %v", err)
	}
	if string(rawAfter) != string(seedData) {
		t.Fatalf("real codex-auth.json content changed:\nbefore=%s\nafter=%s", seedData, rawAfter)
	}

	if !m.codexAuth.valid {
		t.Fatal("must not show a false \"logged out\" state — codexAuth.valid flipped to false despite blocking the action")
	}
	if m.message != i18n.T("firstrun.preset_pick.draft_codex_logout_blocked") {
		t.Fatalf("expected the localized draft-blocked message, got %q", m.message)
	}
	if m.message == i18n.T("firstrun.preset_pick.codex_logged_out") {
		t.Fatal("must never show the real \"logged out\" message for pre-existing draft auth")
	}
	if blockedMsg != m.message {
		t.Fatalf("expected the blocked message to stay stable across repeated presses, got %q then %q", blockedMsg, m.message)
	}
}

// TestDraftFirstRun_CodexDeleteAllowedForSessionLogin proves the OTHER half
// of blocker 5's fix: when the Codex login happened DURING this draft
// session (draft.DraftCodexTokens non-empty, exactly what
// CodexOAuthDoneMsg's draftMode branch sets — see firstrun.go), clearing it
// via Delete remains fully functional and honest, since nothing was ever
// written to disk in the first place. This proves the blocking guard in
// blocker 5's fix is scoped correctly — it must not also break the
// legitimate, already-tested "undo a same-session login" path.
func TestDraftFirstRun_CodexDeleteAllowedForSessionLogin(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	m.step = stepPickPreset
	m.presets = nil
	m.cursor = 0

	// Simulate a completed same-session OAuth login via the real message
	// path (CodexOAuthDoneMsg), not a synthetic direct field assignment.
	m, _ = m.Update(CodexOAuthDoneMsg{
		Tokens: &CodexTokens{AccessToken: "session-token", RefreshToken: "session-refresh", Email: "session@example.com"},
	})
	if m.draft.DraftCodexTokens.Empty() {
		t.Fatal("expected DraftCodexTokens to be populated after a same-session OAuth completion")
	}
	if !m.codexAuth.valid {
		t.Fatal("expected codexAuth.valid=true after the same-session login")
	}

	before := dirSnapshot(t, home)

	// Two-press Del: arm, then confirm.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if !m.codexLogoutArmed {
		t.Fatal("expected the logout to arm for a same-session login — this path must remain unblocked")
	}
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})

	assertSnapshotsEqual(t, "draft Codex delete for same-session login", before, dirSnapshot(t, home))

	if !m.draft.DraftCodexTokens.Empty() {
		t.Fatal("expected DraftCodexTokens cleared after confirming the in-memory logout")
	}
	if m.codexAuth.valid {
		t.Fatal("expected codexAuth.valid=false after clearing the same-session login")
	}
	if m.message != i18n.T("firstrun.preset_pick.codex_logged_out") {
		t.Fatalf("expected the normal logged-out message for a same-session login, got %q", m.message)
	}
}

// TestDraftFirstRun_CtrlENeverOpensEditorOrWritesTempFile proves stepPresetKey's
// ctrl+e handler is blocked entirely in draftMode — a parent review found it
// had NO draftMode guard: os.CreateTemp + tea.ExecProcess (which shells out to
// $EDITOR) are a real filesystem write plus a subprocess exec, unconditional,
// with no draft-shaped equivalent. This drives the ACTUAL "ctrl+e" keypress
// (not a synthetic direct call) through a real key-entry step, with $TMPDIR
// pointed at an isolated, watched directory and $EDITOR pointed at a marker
// script that would prove it ran (by writing a sentinel file) if invoked —
// then asserts both the watched TMPDIR and the editor's marker file are
// untouched.
func TestDraftFirstRun_CtrlENeverOpensEditorOrWritesTempFile(t *testing.T) {
	watchedTmp := t.TempDir()
	t.Setenv("TMPDIR", watchedTmp)

	editorRanMarker := filepath.Join(t.TempDir(), "editor-ran-marker")
	editorScript := filepath.Join(t.TempDir(), "fake-editor.sh")
	if err := os.WriteFile(editorScript, []byte("#!/bin/sh\ntouch \""+editorRanMarker+"\"\n"), 0o755); err != nil {
		t.Fatalf("write fake editor script: %v", err)
	}
	t.Setenv("EDITOR", editorScript)

	m, home, _ := buildDraftModel(t)
	m.presets = []preset.Preset{{
		Name: "minimax",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "minimax",
				"api_key_env": "MINIMAX_API_KEY",
			},
		},
	}}
	m.cursor = 0
	m, _ = m.enterPresetKeyFor(m.presets[0])
	if m.step != stepPresetKey {
		t.Fatalf("expected stepPresetKey, got %v", m.step)
	}

	homeBefore := dirSnapshot(t, home)
	tmpBefore := dirSnapshot(t, watchedTmp)

	m, _ = m.Update(tea.KeyPressMsg{Text: "ctrl+e"})

	homeAfter := dirSnapshot(t, home)
	tmpAfter := dirSnapshot(t, watchedTmp)
	assertSnapshotsEqual(t, "draft ctrl+e home dir", homeBefore, homeAfter)
	assertSnapshotsEqual(t, "draft ctrl+e watched TMPDIR", tmpBefore, tmpAfter)

	if _, err := os.Stat(editorRanMarker); !os.IsNotExist(err) {
		t.Fatalf("editor marker file exists (err=%v) — external editor was executed in draft mode", err)
	}
	if m.step != stepPresetKey {
		t.Fatalf("expected to remain on stepPresetKey, moved to %v", m.step)
	}
	if m.message == "" {
		t.Fatal("expected a status message explaining ctrl+e was blocked in draft mode")
	}
}

// TestDraftFirstRun_SecretsNeverInFormattedOutput proves ProjectDraft's
// secret fields never leak through %v/%+v/%#v formatting or error-wrapping —
// the exact "opaque, no String()/log exposure" requirement from the design
// doc. Covers all three fields a parent review named explicitly: DraftAPIKey,
// DraftCodexTokens, and ExistingKeys. ExistingKeys used to be a plain
// map[string]string that call sites had to remember to redact before
// assigning (and three of them didn't — they aliased FirstRunModel's REAL
// live key map directly); it is now keyPresence, a distinct map type whose
// VALUES carry no payload at all, so there is no real secret to even test
// for at this field — presence only.
func TestDraftFirstRun_SecretsNeverInFormattedOutput(t *testing.T) {
	draft := NewProjectDraft("/tmp/whatever")
	draft.DraftAPIKey = secretString("super-secret-key")
	draft.DraftCodexTokens = secretBytes(`{"access_token":"super-secret-token"}`)
	draft.ExistingKeys = redactedKeyPresence(map[string]string{"MINIMAX_API_KEY": "super-secret-existing-key"})

	secrets := []string{"super-secret-key", "super-secret-token", "super-secret-existing-key"}
	assertNoSecretLeak := func(label, rendered string) {
		t.Helper()
		for _, secret := range secrets {
			if containsSecret(rendered, secret) {
				t.Fatalf("%s: secret %q leaked: %s", label, secret, rendered)
			}
		}
	}

	assertNoSecretLeak("%v", fmt.Sprintf("%v", *draft))
	assertNoSecretLeak("%+v", fmt.Sprintf("%+v", *draft))
	assertNoSecretLeak("%#v", fmt.Sprintf("%#v", *draft))
	assertNoSecretLeak("%v pointer", fmt.Sprintf("%v", draft))
	assertNoSecretLeak("%+v pointer", fmt.Sprintf("%+v", draft))
	assertNoSecretLeak("%#v pointer", fmt.Sprintf("%#v", draft))
	assertNoSecretLeak("error %v", fmt.Errorf("context: %v", *draft).Error())
	assertNoSecretLeak("error %+v", fmt.Errorf("context: %+v", *draft).Error())

	// redactedKeyPresence itself: proves the helper the firstrun.go call
	// sites now use returns a keyPresence map — by TYPE there is no way for
	// a real value to flow through it (keyPresenceValue carries no
	// payload), so the only thing left to assert is that key presence
	// (the name) survives the conversion.
	redacted := redactedKeyPresence(map[string]string{"OPENAI_API_KEY": "sk-real-value-xyz"})
	if fmt.Sprintf("%#v", redacted) == fmt.Sprintf("%#v", map[string]string{"OPENAI_API_KEY": "sk-real-value-xyz"}) {
		t.Fatal("redactedKeyPresence output must not equal the raw input map's formatting")
	}
	if _, ok := redacted["OPENAI_API_KEY"]; !ok {
		t.Fatal("redactedKeyPresence must preserve key presence (the name)")
	}
}

func containsSecret(haystack, needle string) bool {
	return needle != "" && strings.Contains(haystack, needle)
}

// TestDraftFirstRun_PurityWalkAcrossSteps walks Next/Back/Esc across the
// draft-mode wizard (welcome -> pick preset -> agent name/dir -> recipe ->
// review -> back to recipe -> esc to launcher) and asserts the filesystem
// under a fresh isolated HOME never changes at any point, matching the
// design doc's "purity snapshot test" requirement.
func TestDraftFirstRun_PurityWalkAcrossSteps(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	m.setupDone = true
	before := dirSnapshot(t, home)

	// Draft models start at stepPickPreset — the launcher's welcome prelude
	// owns language/theme, so the wizard's own Welcome step is not part of
	// the draft flow at all.
	if m.step != stepPickPreset {
		t.Fatalf("expected draft model to start at stepPickPreset, got %v", m.step)
	}
	assertSnapshotsEqual(t, "after draft construction", before, dirSnapshot(t, home))

	// Stage the values gathered by the intervening preset/agent pages, then
	// exercise the production recipe -> Review transition. Dedicated tests
	// cover those pages' individual key paths; this test guards the aggregate
	// no-write boundary across the full draft state sequence.
	m.presets = []preset.Preset{minimalDraftPreset()}
	m.cursor = 0
	m.pendingAgentOpts = preset.DefaultAgentOpts()
	m.pendingDirName = "orchestrator"
	m.agentName = "orchestrator"
	m.step = stepRecipe
	assertSnapshotsEqual(t, "after agent and recipe state staged", before, dirSnapshot(t, home))

	m, _ = m.enterReviewStep("", "")
	if m.step != stepReview {
		t.Fatalf("expected recipe transition to reach stepReview, got %v", m.step)
	}
	assertSnapshotsEqual(t, "after entering Review", before, dirSnapshot(t, home))

	// Real Esc paths: Review -> recipe -> agent page.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.step != stepRecipe {
		t.Fatalf("expected Review Esc to return to recipe, got %v", m.step)
	}
	assertSnapshotsEqual(t, "after esc from Review", before, dirSnapshot(t, home))
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.step != stepAgentNameDir {
		t.Fatalf("expected recipe Esc to return to agent page, got %v", m.step)
	}
	assertSnapshotsEqual(t, "after esc from recipe", before, dirSnapshot(t, home))

	// The typed cancel path itself also remains pure: Esc at the draft
	// entry step (stepPickPreset) leaves the wizard via the typed cancel.
	m.step = stepPickPreset
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected entry-step Esc to emit a typed cancel command")
	}
	if _, ok := runCmd(cmd).(ProjectDraftCancelledMsg); !ok {
		t.Fatal("expected ProjectDraftCancelledMsg from entry-step Esc")
	}
	assertSnapshotsEqual(t, "after typed draft cancel", before, dirSnapshot(t, home))
}

// --- Invariant 6: draft cancel semantics via the REAL key path -------------

// TestDraftFirstRun_EscAtEntryStepEmitsCancelMsg drives the ACTUAL "esc"
// keypress at stepPickPreset — the draft wizard's entry step now that the
// launcher's welcome prelude owns language/theme — and proves it emits
// ProjectDraftCancelledMsg rather than navigating "back" to the wizard's
// own stepWelcome (which would show a second, duplicate welcome page the
// draft flow deliberately skips). This is the same class of defect a
// parent review found at the old entry step: an entry-step Esc with no
// typed cancel path leaves the launcher stuck.
func TestDraftFirstRun_EscAtEntryStepEmitsCancelMsg(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	if m.step != stepPickPreset {
		t.Fatalf("expected draft model to start at stepPickPreset, got %v", m.step)
	}
	before := dirSnapshot(t, home)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected esc at the draft entry step to return a command")
	}
	msg := runCmd(cmd)
	if _, ok := msg.(ProjectDraftCancelledMsg); !ok {
		t.Fatalf("expected ProjectDraftCancelledMsg from esc at the draft entry step, got %T (%v)", msg, msg)
	}

	assertSnapshotsEqual(t, "esc at draft entry step", before, dirSnapshot(t, home))
}

// TestDraftFirstRun_CtrlCAtEntryStepEmitsCancelMsgNotTeaQuit drives the
// ACTUAL "ctrl+c" keypress at stepPickPreset and proves it ALSO emits
// ProjectDraftCancelledMsg rather than a bare tea.Quit. A parent review
// found the equivalent defect at the old draft entry step: an
// unconditional `case "ctrl+c": return m, tea.Quit` would, because the
// launcher runs FirstRunModel inside its OWN tea.Program (see launcher.go),
// kill that whole program abruptly — bypassing LauncherRootModel's
// done/result bookkeeping (m.done would stay false) — instead of routing
// back through a proper decision the same way Esc does.
func TestDraftFirstRun_CtrlCAtEntryStepEmitsCancelMsgNotTeaQuit(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	before := dirSnapshot(t, home)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("expected ctrl+c at the draft entry step to return a command")
	}
	msg := runCmd(cmd)
	if _, ok := msg.(ProjectDraftCancelledMsg); !ok {
		t.Fatalf("expected ProjectDraftCancelledMsg from ctrl+c at the draft entry step, got %T (%v) — a bare tea.Quit here would bypass LauncherRootModel entirely", msg, msg)
	}

	assertSnapshotsEqual(t, "ctrl+c at draft entry step", before, dirSnapshot(t, home))
}

// TestDraftFirstRun_BackButtonAtEntryStepEmitsCancelMsg drives the Back
// footer button (cursor parked on Back, Enter) at stepPickPreset in draft
// mode and proves it exits via the same typed cancel as Esc — never
// `m.step = stepWelcome`, the non-draft behavior.
func TestDraftFirstRun_BackButtonAtEntryStepEmitsCancelMsg(t *testing.T) {
	m, _, _ := buildDraftModel(t)
	m.cursor = m.visiblePresetCount() + 1 // pickBackIdx
	updated, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if updated.step == stepWelcome {
		t.Fatal("draft Back button must not navigate to the wizard's own stepWelcome")
	}
	if cmd == nil {
		t.Fatal("expected Back at the draft entry step to return a command")
	}
	if _, ok := runCmd(cmd).(ProjectDraftCancelledMsg); !ok {
		t.Fatal("expected ProjectDraftCancelledMsg from the draft Back button")
	}
}

// TestLauncherRootModel_CancelReturnsToChooseAndDiscardsDraft drives the
// launcher root model through the REAL key path — choose Enter on "Start a
// new project in this folder" (entering enterCreate/NewDraftFirstRunModel),
// a real "esc" keypress inside the hosted FirstRunModel that produces
// ProjectDraftCancelledMsg, feeding that message back into
// LauncherRootModel.Update — and proves the model returns to
// launcherViewChoose with the old draft/firstRun state discarded, not
// merely hidden. This is item 6's exact requirement: "Esc at the draft
// entry step must emit a typed cancel/back result to LauncherRoot, return
// to the decision page, and discard/reset the old draft."
func TestLauncherRootModel_CancelReturnsToChooseAndDiscardsDraft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	globalDir := filepath.Join(home, ".lingtai-tui")

	m := NewLauncherRootModel(projectRoot, globalDir, "")
	updated, _ := m.updateChoose(tea.KeyPressMsg{Code: tea.KeyEnter}) // cursor 0 = start here
	lm := updated.(LauncherRootModel)
	if lm.view != launcherViewCreate || !lm.firstRunOn || lm.draft == nil {
		t.Fatalf("expected Create entry to be live, got view=%v firstRunOn=%v draft=%v", lm.view, lm.firstRunOn, lm.draft)
	}
	if lm.firstRun.step != stepPickPreset {
		t.Fatalf("expected the draft wizard to start at stepPickPreset (the launcher prelude owns welcome), got %v", lm.firstRun.step)
	}
	firstDraft := lm.draft
	// Mark the draft dirty so a later "resume" bug (reusing the same draft
	// pointer after cancel) would be observable, not silently identical by
	// coincidence of both being freshly zero-valued.
	firstDraft.AgentName = "agent-from-the-cancelled-attempt"

	// Drive the REAL key path inside the hosted model: esc at the entry step.
	updatedFirstRun, cmd := lm.firstRun.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	lm.firstRun = updatedFirstRun
	if cmd == nil {
		t.Fatal("expected a command from esc at the draft entry step")
	}
	msg := runCmd(cmd)
	cancelMsg, ok := msg.(ProjectDraftCancelledMsg)
	if !ok {
		t.Fatalf("expected ProjectDraftCancelledMsg, got %T", msg)
	}

	updated, _ = lm.Update(cancelMsg)
	lm = updated.(LauncherRootModel)

	if lm.view != launcherViewChoose {
		t.Fatalf("expected cancel to return to launcherViewChoose, got %v", lm.view)
	}
	if lm.draft != nil {
		t.Fatal("expected cancel to discard the old draft (lm.draft == nil), not merely leave the view")
	}
	if lm.firstRunOn {
		t.Fatal("expected cancel to reset firstRunOn to false")
	}

	// Subsequent Create must start a genuinely FRESH draft, not resume the
	// cancelled one — item 6's exact "subsequent Create starts a fresh
	// draft" requirement.
	updated, _ = lm.updateChoose(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm2 := updated.(LauncherRootModel)
	if lm2.draft == nil {
		t.Fatal("expected a second Create entry to build a new draft")
	}
	if lm2.draft == firstDraft {
		t.Fatal("expected a second Create entry to allocate a NEW *ProjectDraft, not reuse the cancelled one's pointer")
	}
	if lm2.draft.AgentName == "agent-from-the-cancelled-attempt" {
		t.Fatal("second Create entry's draft carries state from the cancelled attempt — must start fresh")
	}
}

// TestLauncherRootModel_AppliesPersistedThemeAndLanguageAndKeepsPreludeChoiceOnCancel
// proves two halves of the redesigned theme/language ownership model:
//
//  1. Construction applies the PERSISTED tui_config.json theme/language
//     in-memory (pure read + SetThemeByName/i18n.SetLang) — the exact
//     "theme is wrong" defect of the previous launcher, which rendered the
//     compiled-in defaults regardless of configuration.
//  2. The launcher's welcome prelude now OWNS theme/language selection for
//     the no-project flow, so cancelling a create draft keeps the PRELUDE
//     selection (the launcher-level baseline captured by enterCreate) —
//     the wizard itself no longer previews either, and a cancel must not
//     snap the UI back to the persisted values the user already changed
//     away from at the prelude.
//
// Persisted config stays byte-identical throughout: every application is
// in-memory only.
func TestLauncherRootModel_AppliesPersistedThemeAndLanguageAndKeepsPreludeChoiceOnCancel(t *testing.T) {
	// ActiveTheme()/i18n.Lang() are genuinely global, process-wide package
	// state (exactly the thing this test is exercising) — restore both to
	// the repo-wide test convention's neutral default on exit, matching
	// every other test in this package that calls i18n.SetLang with a
	// non-"en" value (see e.g. projects_test.go, status_hint_copy_test.go).
	t.Cleanup(func() {
		SetThemeByName(DefaultThemeName)
		_ = i18n.SetLang("en")
	})

	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	globalDir := filepath.Join(home, ".lingtai-tui")
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		t.Fatalf("mkdir globalDir: %v", err)
	}

	// Seed a KNOWN, non-default persisted config so "constructor applied
	// the persisted values" is a meaningful assertion.
	persisted := config.TUIConfig{Theme: "xuan-paper", Language: "wen", MailPageSize: config.DefaultMailPageSize}
	if err := config.SaveTUIConfig(globalDir, persisted); err != nil {
		t.Fatalf("seed persisted tui_config.json: %v", err)
	}
	// Deliberately desync the in-memory state first, so the constructor's
	// application is observable.
	SetThemeByName("ink-dark")
	_ = i18n.SetLang("en")

	m := NewLauncherRootModel(projectRoot, globalDir, "")
	want := ThemeByName(persisted.Theme)
	if got := ActiveTheme(); got.BG != want.BG || got.Text != want.Text {
		t.Fatalf("expected construction to apply the persisted theme %q in-memory", persisted.Theme)
	}
	if i18n.Lang() != persisted.Language {
		t.Fatalf("expected construction to apply the persisted language %q, got %q", persisted.Language, i18n.Lang())
	}
	if m.themeName != persisted.Theme || launcherLangs[m.langIdx] != persisted.Language {
		t.Fatalf("expected prelude state seeded from persisted config, got theme=%q langIdx=%d", m.themeName, m.langIdx)
	}

	// Prelude: real "up" moves wen -> zh (live preview); real ctrl+t cycles
	// xuan-paper -> ink-dark (ThemeNames is sorted; xuan-paper wraps).
	updated, _ := m.updateWelcome(tea.KeyPressMsg{Code: tea.KeyUp})
	lm := updated.(LauncherRootModel)
	updated, _ = lm.updateWelcome(tea.KeyPressMsg{Text: "ctrl+t"})
	lm = updated.(LauncherRootModel)
	if lm.themeName != "ink-dark" || launcherLangs[lm.langIdx] != "zh" {
		t.Fatalf("prelude selection = (%q, %q), want (ink-dark, zh)", lm.themeName, launcherLangs[lm.langIdx])
	}
	updated, _ = lm.updateWelcome(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm = updated.(LauncherRootModel)
	updated, _ = lm.updateChoose(tea.KeyPressMsg{Code: tea.KeyEnter}) // cursor 0 = start here
	lm = updated.(LauncherRootModel)

	// enterCreate captures the PRELUDE selection as the cancel baseline
	// and seeds it onto the draft.
	if lm.preDraftTheme != "ink-dark" || lm.preDraftLanguage != "zh" {
		t.Fatalf("expected enterCreate to capture the prelude baseline, got theme=%q lang=%q", lm.preDraftTheme, lm.preDraftLanguage)
	}
	if lm.draft.Theme != "ink-dark" || lm.draft.Language != "zh" {
		t.Fatalf("expected the draft seeded with the prelude selection, got theme=%q lang=%q", lm.draft.Theme, lm.draft.Language)
	}

	// Real Esc at the wizard's entry step -> typed cancel -> launcher.
	updatedFirstRun, cmd := lm.firstRun.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	lm.firstRun = updatedFirstRun
	if cmd == nil {
		t.Fatal("expected a command from esc at the draft entry step")
	}
	cancelMsg, ok := runCmd(cmd).(ProjectDraftCancelledMsg)
	if !ok {
		t.Fatal("expected ProjectDraftCancelledMsg")
	}
	updated, _ = lm.Update(cancelMsg)
	lm = updated.(LauncherRootModel)
	if lm.view != launcherViewChoose {
		t.Fatalf("expected cancel to return to launcherViewChoose, got %v", lm.view)
	}

	// The authoritative assertions: the PRELUDE selection survives cancel —
	// not the persisted values the user already navigated away from.
	wantPrelude := ThemeByName("ink-dark")
	if got := ActiveTheme(); got.BG != wantPrelude.BG || got.Text != wantPrelude.Text {
		t.Fatal("expected the prelude theme to survive a draft cancel")
	}
	if i18n.Lang() != "zh" {
		t.Fatalf("expected the prelude language to survive a draft cancel, got %q", i18n.Lang())
	}

	// Persisted config must be untouched — everything above is in-memory.
	tuiCfgAfter := config.LoadTUIConfig(globalDir)
	if tuiCfgAfter.Theme != persisted.Theme || tuiCfgAfter.Language != persisted.Language {
		t.Fatalf("expected persisted tui_config.json unchanged, got theme=%q lang=%q", tuiCfgAfter.Theme, tuiCfgAfter.Language)
	}

	// A fresh draft started after cancel begins from the SAME prelude
	// baseline.
	updated, _ = lm.updateChoose(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm2 := updated.(LauncherRootModel)
	if lm2.preDraftTheme != "ink-dark" || lm2.preDraftLanguage != "zh" {
		t.Fatalf("expected a fresh draft's baseline to still be the prelude selection, got %q/%q",
			lm2.preDraftTheme, lm2.preDraftLanguage)
	}
}

// --- Open Existing reuses the established /projects model ------------------

func TestLauncherOpenExistingReusesProjectsModelAndMapsValidatedRoot(t *testing.T) {
	project := t.TempDir()
	record := projectRecord(project, "admin", "Admin", true)
	snapshot := inventory.Snapshot{
		Records: []inventory.Record{record},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{record}}},
	}
	withProjectsScan(t, func(inventory.Options) (inventory.Snapshot, error) {
		return snapshot, nil
	})

	m := NewLauncherRootModel(t.TempDir(), t.TempDir(), "")
	updated, cmd := m.enterPicker()
	lm := updated.(LauncherRootModel)
	if lm.view != launcherViewPicker {
		t.Fatalf("enterPicker view = %v, want picker", lm.view)
	}
	if lm.projects.source != projectSourceRegistry {
		t.Fatalf("launcher picker source = %v, want established registry ProjectsModel", lm.projects.source)
	}
	if cmd == nil {
		t.Fatal("enterPicker did not initialize ProjectsModel")
	}

	// The child owns the inventory load and selection validation. Feed its
	// messages through LauncherRootModel rather than constructing launcher rows.
	updated, _ = lm.Update(runCmd(cmd))
	lm = updated.(LauncherRootModel)
	updated, cmd = lm.updatePicker(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm = updated.(LauncherRootModel)
	if cmd == nil {
		t.Fatal("ProjectsModel Enter did not request validated selection")
	}
	updated, cmd = lm.Update(runCmd(cmd))
	lm = updated.(LauncherRootModel)
	if cmd == nil {
		t.Fatal("validated ProjectsModel selection did not emit ProjectsAgentSelectedMsg")
	}
	updated, cmd = lm.Update(runCmd(cmd))
	lm = updated.(LauncherRootModel)
	if !lm.done || cmd == nil {
		t.Fatalf("validated selection did not finish launcher: done=%v cmd=%T", lm.done, cmd)
	}
	if lm.result.Kind != DecisionOpenExisting || lm.result.ProjectRoot != project {
		t.Fatalf("result = (%v, %q), want (%v, %q)", lm.result.Kind, lm.result.ProjectRoot, DecisionOpenExisting, project)
	}
}

func TestLauncherOpenExistingIgnoresStaleActivationAndRequest(t *testing.T) {
	project := t.TempDir()
	record := projectRecord(project, "admin", "Admin", true)
	m := NewLauncherRootModel(t.TempDir(), t.TempDir(), "")

	updated, _ := m.enterPicker()
	m = updated.(LauncherRootModel)
	firstActivation := m.projectsActivationID
	firstRequest := m.projects.requestSeq

	// Leave and re-enter Open Existing. The embedded model is replaced and
	// the launcher-owned activation must advance before its messages are
	// allowed to select a project.
	updated, _ = m.Update(ViewChangeMsg{View: "mail"})
	m = updated.(LauncherRootModel)
	updated, _ = m.enterPicker()
	m = updated.(LauncherRootModel)
	if m.projectsActivationID != firstActivation+1 {
		t.Fatalf("projects activation = %d, want %d after re-entry", m.projectsActivationID, firstActivation+1)
	}

	updated, cmd := m.Update(ProjectsAgentSelectedMsg{
		ActivationID: firstActivation,
		RequestSeq:   firstRequest,
		Record:       record,
	})
	m = updated.(LauncherRootModel)
	if m.done || cmd != nil {
		t.Fatalf("stale old-activation selection was accepted: done=%v cmd=%T", m.done, cmd)
	}

	staleRequest := m.projects.requestSeq
	m.projects.nextRequestSeq()
	updated, cmd = m.Update(ProjectsAgentSelectedMsg{
		ActivationID: m.projectsActivationID,
		RequestSeq:   staleRequest,
		Record:       record,
	})
	m = updated.(LauncherRootModel)
	if m.done || cmd != nil {
		t.Fatalf("stale request selection was accepted: done=%v cmd=%T", m.done, cmd)
	}

	updated, cmd = m.Update(ProjectsAgentSelectedMsg{
		ActivationID: m.projectsActivationID,
		RequestSeq:   m.projects.requestSeq,
		Record:       record,
	})
	m = updated.(LauncherRootModel)
	if !m.done || cmd == nil {
		t.Fatalf("current validated selection did not finish launcher: done=%v cmd=%T", m.done, cmd)
	}
	if m.result.Kind != DecisionOpenExisting || m.result.ProjectRoot != project {
		t.Fatalf("result = (%v, %q), want (%v, %q)", m.result.Kind, m.result.ProjectRoot, DecisionOpenExisting, project)
	}
}

func TestLauncherOpenExistingProjectsBackReturnsToChoose(t *testing.T) {
	m := NewLauncherRootModel(t.TempDir(), t.TempDir(), "")
	m.view = launcherViewPicker
	m.projects = NewProjectsModel(t.TempDir(), filepath.Join(t.TempDir(), ".lingtai"), ProjectsContext{})
	updated, cmd := m.updatePicker(tea.KeyPressMsg{Code: tea.KeyEscape})
	lm := updated.(LauncherRootModel)
	if cmd == nil {
		t.Fatal("ProjectsModel Esc did not emit its back message")
	}
	back, ok := runCmd(cmd).(ViewChangeMsg)
	if !ok || back.View != "mail" {
		t.Fatalf("ProjectsModel Esc emitted %T %#v, want ViewChangeMsg{View:mail}", runCmd(cmd), back)
	}
	updated, _ = lm.Update(back)
	lm = updated.(LauncherRootModel)
	if lm.view != launcherViewChoose {
		t.Fatalf("launcher view after ProjectsModel back = %v, want choose", lm.view)
	}
}

// --- Invariant 5: unfinished staging must actually be visible after construction ---

// TestLauncherRootModel_UnfinishedStagingVisibleAfterConstruction proves a
// marker-owned ".lingtai.create-*" directory left by a prior kill -9 is
// actually reachable through the constructed root model (not silently
// dropped by a value-receiver Init() that mutates a throwaway copy — the
// exact bug a parent review found: DetectUnfinishedStaging used to run
// inside Init(), whose tea.Cmd-only return signature has no way to hand an
// updated model back to the framework, so m.unfinishedStaging was always nil
// by the time the choose page checked it and the entire Resume/Discard UI
// was unreachable dead code). This drives the real key path (choose Enter
// on "Start a new project in this folder") rather than reading the field
// directly, so it also proves updateChoose's
// `len(m.unfinishedStaging) > 0` branch fires.
func TestLauncherRootModel_UnfinishedStagingVisibleAfterConstruction(t *testing.T) {
	root := t.TempDir()
	stagingDir, err := os.MkdirTemp(root, ".lingtai.create-*")
	if err != nil {
		t.Fatal(err)
	}
	nonce := filepath.Base(stagingDir)
	if err := os.WriteFile(filepath.Join(stagingDir, stagingMarkerName), []byte(nonce+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewLauncherRootModel(root, filepath.Join(t.TempDir(), ".lingtai-tui"), "")
	if len(m.unfinishedStaging) != 1 || m.unfinishedStaging[0] != stagingDir {
		t.Fatalf("expected constructor to populate unfinishedStaging with %q, got %v", stagingDir, m.unfinishedStaging)
	}

	// Drive the actual key path: choose Enter on "Start here" (cursor 0)
	// must route to the unfinished-staging screen instead of straight into
	// enterCreate, because len(m.unfinishedStaging) > 0.
	m.chooseCursor = 0
	updated, _ := m.updateChoose(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm := updated.(LauncherRootModel)
	if lm.view != launcherViewStaging {
		t.Fatalf("expected choose Enter on Create to route to launcherViewStaging when unfinishedStaging is non-empty, got view=%v", lm.view)
	}
}

// TestLauncherRootModel_UnmarkedStagingNeverOffered proves a directory that
// merely matches the ".lingtai.create-*" naming pattern but has no (or a
// mismatched) ownership marker is never surfaced by the constructor —
// DetectUnfinishedStaging's marker-gating must hold even when called from
// the constructor rather than Init().
func TestLauncherRootModel_UnmarkedStagingNeverOffered(t *testing.T) {
	root := t.TempDir()
	unmarked := filepath.Join(root, ".lingtai.create-nomark")
	if err := os.MkdirAll(unmarked, 0o755); err != nil {
		t.Fatal(err)
	}
	mismatched := filepath.Join(root, ".lingtai.create-mismatch")
	if err := os.MkdirAll(mismatched, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mismatched, stagingMarkerName), []byte("not-the-dir-name\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	m := NewLauncherRootModel(root, filepath.Join(t.TempDir(), ".lingtai-tui"), "")
	if len(m.unfinishedStaging) != 0 {
		t.Fatalf("expected no unfinished staging offered for unmarked/mismatched directories, got %v", m.unfinishedStaging)
	}

	// Confirm neither directory was deleted by construction (read-only).
	if _, err := os.Stat(unmarked); err != nil {
		t.Fatalf("unmarked staging directory must not be deleted by construction: %v", err)
	}
	if _, err := os.Stat(mismatched); err != nil {
		t.Fatalf("mismatched staging directory must not be deleted by construction: %v", err)
	}
}

// --- Blocker 7: Review must show model and capabilities --------------------

// TestPresetModelName_ReadsManifestLLMModel proves the helper reads the
// truthful manifest.llm.model value, and returns "" (never a fabricated
// default) when that field is genuinely absent.
func TestPresetModelName_ReadsManifestLLMModel(t *testing.T) {
	p := preset.Preset{
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{"provider": "minimax", "model": "abab6.5s-chat"},
		},
	}
	if got := presetModelName(p); got != "abab6.5s-chat" {
		t.Fatalf("expected model %q, got %q", "abab6.5s-chat", got)
	}

	empty := preset.Preset{Manifest: map[string]interface{}{}}
	if got := presetModelName(empty); got != "" {
		t.Fatalf("expected empty model for a manifest with no llm block, got %q", got)
	}
}

// TestPresetCapabilitiesSummary_ListsConfiguredCapabilities proves the
// helper lists exactly the capability names present in
// manifest.capabilities, sorted, comma-joined — never the full
// AllCapabilities list, never a placeholder.
func TestPresetCapabilitiesSummary_ListsConfiguredCapabilities(t *testing.T) {
	p := preset.Preset{
		Manifest: map[string]interface{}{
			"capabilities": map[string]interface{}{
				"vision":     map[string]interface{}{"provider": "minimax"},
				"web_search": map[string]interface{}{"provider": "minimax"},
			},
		},
	}
	if got := presetCapabilitiesSummary(p); got != "vision, web_search" {
		t.Fatalf("expected %q, got %q", "vision, web_search", got)
	}

	empty := preset.Preset{Manifest: map[string]interface{}{}}
	if got := presetCapabilitiesSummary(empty); got != "" {
		t.Fatalf("expected empty summary for a manifest with no capabilities block, got %q", got)
	}
}

// TestViewReview_ShowsModelAndCapabilities drives the launcher's real Create
// flow far enough to reach stepReview with a concrete preset carrying both
// a model and capabilities, then renders viewReview and proves both appear
// with their REAL values — not placeholders. A parent review found the
// approved design's Review page (folder, agent, preset, model, recipe,
// capabilities) omitted model and capabilities entirely.
func TestViewReview_ShowsModelAndCapabilities(t *testing.T) {
	m, _, _ := buildDraftModel(t)
	m.presets = []preset.Preset{{
		Name:        "minimax-vision",
		Description: preset.PresetDescription{Summary: "vision-capable preset"},
		Manifest: map[string]interface{}{
			"llm":          map[string]interface{}{"provider": "minimax", "model": "abab6.5s-chat"},
			"capabilities": map[string]interface{}{"vision": map[string]interface{}{"provider": "minimax"}},
		},
	}}
	m.cursor = 0
	m.draft.AgentName = "orchestrator"

	// Drive the REAL transition function (the same one the wizard's Enter
	// handler on stepRecipe calls) rather than directly assigning m.step —
	// this is what actually resolves DraftPreset from the cursor.
	m, _ = m.enterReviewStep("", "")
	if m.step != stepReview {
		t.Fatalf("expected enterReviewStep to reach stepReview, got %v", m.step)
	}

	out := m.viewReview()

	if !strings.Contains(out, "abab6.5s-chat") {
		t.Fatalf("expected viewReview output to contain the preset's real model \"abab6.5s-chat\", got:\n%s", out)
	}
	if !strings.Contains(out, "vision") {
		t.Fatalf("expected viewReview output to contain the preset's real capability \"vision\", got:\n%s", out)
	}
	if !strings.Contains(out, i18n.T("firstrun.review.model")) {
		t.Fatalf("expected viewReview output to contain the localized Model row label, got:\n%s", out)
	}
	if !strings.Contains(out, i18n.T("firstrun.review.capabilities")) {
		t.Fatalf("expected viewReview output to contain the localized Capabilities row label, got:\n%s", out)
	}
}

// TestViewReview_ShowsPlaceholderWhenPresetHasNoModelOrCapabilities proves
// the rows are truthful in the OTHER direction too: a preset that genuinely
// declares neither must render the row's own empty-value placeholder ("—"),
// never a fabricated model name or capability list.
func TestViewReview_ShowsPlaceholderWhenPresetHasNoModelOrCapabilities(t *testing.T) {
	m, _, _ := buildDraftModel(t)
	m.presets = []preset.Preset{{
		Name:        "bare-preset",
		Description: preset.PresetDescription{Summary: "minimal preset"},
		Manifest:    map[string]interface{}{"llm": map[string]interface{}{"provider": "custom"}},
	}}
	m.cursor = 0
	m.agentName = "orchestrator"
	m.pendingDirName = "orchestrator"

	m, _ = m.enterReviewStep("no-recipe", "")

	out := m.viewReview()

	if !strings.Contains(out, i18n.T("firstrun.review.model")) || !strings.Contains(out, i18n.T("firstrun.review.capabilities")) {
		t.Fatalf("expected both row labels to still render even when empty, got:\n%s", out)
	}
	if strings.Count(out, "—") < 2 {
		t.Fatalf("expected truthful placeholders for both empty Review values, got:\n%s", out)
	}
}

// TestDraftFirstRun_ClearingPendingKeyRestoresSharedBaseline proves an empty
// second editor commit clears only the key entered during this draft. It must
// neither leave that stale pending secret active nor delete the shared key that
// existed before the draft started.
func TestDraftFirstRun_ClearingPendingKeyRestoresSharedBaseline(t *testing.T) {
	globalDir := t.TempDir()
	const envName = "DRAFT_CLEAR_TEST_KEY"
	if err := config.SaveConfig(globalDir, config.Config{Keys: map[string]string{envName: "shared-baseline"}}); err != nil {
		t.Fatalf("seed shared key: %v", err)
	}

	projectRoot := t.TempDir()
	draft := NewProjectDraft(projectRoot)
	m := NewDraftFirstRunModel(filepath.Join(projectRoot, ".lingtai"), globalDir, true, draft)
	p := preset.Preset{
		Name: "draft-clear-test",
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider":    "openai",
				"model":       "test-model",
				"api_key_env": envName,
			},
		},
	}
	m.presets = []preset.Preset{p}
	m.cursor = 0

	m, _ = m.Update(PresetEditorCommitMsg{Preset: p, APIKeySet: true, APIKey: "draft-override"})
	if got := m.draftPendingAPIKeys[envName]; got.Reveal() != "draft-override" {
		t.Fatalf("pending key after first edit = %q", got.Reveal())
	}
	if got := m.existingKeys[envName]; got != "draft-override" {
		t.Fatalf("in-memory key after first edit = %q", got)
	}

	m, _ = m.Update(PresetEditorCommitMsg{Preset: p, APIKeySet: true, APIKey: ""})
	if _, ok := m.draftPendingAPIKeys[envName]; ok {
		t.Fatal("empty second edit left stale pending key active")
	}
	if got := m.existingKeys[envName]; got != "shared-baseline" {
		t.Fatalf("in-memory key after clear = %q, want shared baseline", got)
	}
	if got := m.message; got != i18n.T("firstrun.preset_pick.draft_key_override_cleared") {
		t.Fatalf("clear message = %q", got)
	}
	cfg, err := config.LoadConfigReadOnly(globalDir)
	if err != nil {
		t.Fatalf("reload shared config: %v", err)
	}
	if got := cfg.Keys[envName]; got != "shared-baseline" {
		t.Fatalf("shared key changed to %q", got)
	}
}

// TestLauncherWelcomeScreen_MatchesCanonicalFirstRunWelcome exercises the actual
// root model View after a WindowSizeMsg rather than a helper-only renderer. The
// launcher Welcome must exactly match the canonical welcome-only FirstRunModel
// presentation at the same size and language cursor; the existing dynamic
// project sentence belongs on Choose instead.
func TestLauncherWelcomeScreen_MatchesCanonicalFirstRunWelcome(t *testing.T) {
	t.Cleanup(func() {
		SetThemeByName(DefaultThemeName)
		_ = i18n.SetLang("en")
	})
	t.Setenv("HOME", t.TempDir())
	projectRoot := filepath.Join(t.TempDir(), strings.Repeat("long-project-segment-", 8))
	m := NewLauncherRootModel(projectRoot, filepath.Join(t.TempDir(), ".lingtai-tui"), "")
	updated, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m = updated.(LauncherRootModel)
	if m.view != launcherViewWelcome {
		t.Fatalf("expected initial root view to be Welcome, got %v", m.view)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m = updated.(LauncherRootModel)
	if m.langIdx != 1 {
		t.Fatalf("Welcome language cursor = %d, want 1 after one Down", m.langIdx)
	}
	content := ansi.Strip(m.View().Content)
	canonical := ansi.Strip((FirstRunModel{
		width:       m.width,
		height:      m.height,
		langCursor:  m.langIdx,
		welcomeOnly: true,
	}).viewWelcome())
	if content != canonical {
		t.Fatalf("launcher Welcome differs from the canonical welcome-only FirstRunModel:\n--- launcher ---\n%s\n--- canonical ---\n%s", content, canonical)
	}
	for _, want := range []string{"English", "现代汉语", "文言"} {
		if !strings.Contains(content, want) {
			t.Errorf("80x24 welcome screen missing %q:\n%s", want, content)
		}
	}
	compact := func(s string) string {
		return strings.Map(func(r rune) rune {
			if unicode.IsSpace(r) {
				return -1
			}
			return r
		}, s)
	}
	dynamicSentence := compact(i18n.TF("launcher.welcome.explain2", abbreviateHomePath(projectRoot)))
	compactContent := compact(content)
	if strings.Contains(compactContent, dynamicSentence) {
		t.Fatalf("Welcome rendered the dynamic project sentence:\n%s", content)
	}
	for _, unwanted := range []string{
		i18n.T("launcher.welcome.continue"),
		"Esc/q/Ctrl+C",
		i18n.T("launcher.hint_quit"),
	} {
		if strings.Contains(compactContent, compact(unwanted)) {
			t.Fatalf("Welcome rendered launcher-only copy %q:\n%s", unwanted, content)
		}
	}

	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(LauncherRootModel)
	if m.view != launcherViewChoose {
		t.Fatalf("Welcome Enter did not reach Choose, got %v", m.view)
	}
	chooseContent := ansi.Strip(m.View().Content)
	compactChoose := compact(chooseContent)
	sentenceAt := strings.Index(compactChoose, dynamicSentence)
	if sentenceAt < 0 {
		t.Fatalf("Choose omitted the dynamic project sentence:\n%s", chooseContent)
	}
	hereAt := strings.Index(compactChoose, compact(i18n.T("launcher.choose.here")))
	openAt := strings.Index(compactChoose, compact(i18n.T("launcher.choose.open")))
	if hereAt < 0 || openAt < 0 || sentenceAt >= hereAt || sentenceAt >= openAt {
		t.Fatalf("Choose did not place the dynamic sentence above both decisions:\n%s", chooseContent)
	}
	assertLauncherScreenWidth(t, content, 80)
}

// TestLauncherStagingRootViewReservesFooter exercises the production root View
// with a long Unicode staging path and a reachable long discard error. The
// variable body may be windowed, but the title and actionable footer must
// remain inside both supported terminal heights and every rendered line must
// stay within the terminal width.
func TestLauncherStagingRootViewReservesFooter(t *testing.T) {
	t.Cleanup(func() {
		SetThemeByName(DefaultThemeName)
		_ = i18n.SetLang("en")
	})
	t.Setenv("HOME", t.TempDir())
	stagingRoot := t.TempDir()
	longPath := filepath.Join(stagingRoot, strings.Repeat("路径-", 24)+"e\u0301-final")
	longError := strings.Repeat("discard-error-", 24) + "权限 denied"

	for _, tc := range []struct{ width, height int }{{60, 16}, {80, 24}} {
		t.Run(fmt.Sprintf("%dx%d", tc.width, tc.height), func(t *testing.T) {
			m := NewLauncherRootModel(stagingRoot, filepath.Join(t.TempDir(), ".lingtai-tui"), "")
			m.view = launcherViewStaging
			m.unfinishedStaging = []string{longPath}
			m.unfinishedDiscardStatus = longError
			updated, _ := m.Update(tea.WindowSizeMsg{Width: tc.width, Height: tc.height})
			m = updated.(LauncherRootModel)
			content := ansi.Strip(m.View().Content)
			lines := renderedLauncherLines(content)
			if len(lines) > tc.height {
				t.Fatalf("staging rendered %d physical lines at %dx%d:\n%s", len(lines), tc.width, tc.height, content)
			}
			assertLauncherScreenWidth(t, content, tc.width)
			for _, want := range []string{"[d]", "[Esc]", "[q/Ctrl+C]", "discard-error-"} {
				if !strings.Contains(content, want) {
					t.Fatalf("staging %dx%d lost required %q:\n%s", tc.width, tc.height, want, content)
				}
			}
		})
	}
}

func renderedLauncherLines(content string) []string {
	content = strings.TrimSuffix(content, "\n")
	if content == "" {
		return nil
	}
	return strings.Split(content, "\n")
}

func assertLauncherScreenWidth(t *testing.T, content string, width int) {
	t.Helper()
	for i, line := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Errorf("rendered line %d is %d columns wide, terminal width is %d: %q", i+1, got, width, line)
		}
	}
}

// TestLauncherRootModel_CreateReviewCtrlCRecordsCancel drives the actual root
// dispatcher into the advanced draft review step, then proves Ctrl+C records a
// root-owned DecisionCancel instead of returning FirstRunModel's bare tea.Quit.
// Executing the returned command and snapshotting both HOME and the project
// root preserve the no-write guarantee while covering the real handoff.
func TestLauncherRootModel_CreateReviewCtrlCRecordsCancel(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	globalDir := filepath.Join(home, ".lingtai-tui")

	m := NewLauncherRootModel(projectRoot, globalDir, "")
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(LauncherRootModel)
	if m.view != launcherViewChoose {
		t.Fatalf("expected Welcome Enter to reach Choose, got %v", m.view)
	}
	updated, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m = updated.(LauncherRootModel)
	if m.view != launcherViewCreate || !m.firstRunOn {
		t.Fatalf("expected Choose Enter to reach Create, got view=%v firstRunOn=%v", m.view, m.firstRunOn)
	}
	m.firstRun.step = stepReview

	homeBefore := dirSnapshot(t, home)
	projectBefore := dirSnapshot(t, projectRoot)
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	got := updated.(LauncherRootModel)
	if !got.done || got.result.Kind != DecisionCancel {
		t.Fatalf("Create review Ctrl+C did not record root cancellation: done=%v result=%v", got.done, got.result.Kind)
	}
	if cmd == nil {
		t.Fatal("Create review Ctrl+C did not return the root quit command")
	}
	if runCmd(cmd) == nil {
		t.Fatal("Create review Ctrl+C returned a command with no message")
	}
	assertSnapshotsEqual(t, "Create review Ctrl+C HOME", homeBefore, dirSnapshot(t, home))
	assertSnapshotsEqual(t, "Create review Ctrl+C project root", projectBefore, dirSnapshot(t, projectRoot))
}

// TestLauncherKeyboardContract_ThroughRootUpdate proves the launcher-level
// contract at the production dispatcher boundary: Esc backs one level,
// while q and Ctrl+C cancel from every launcher-owned browsing screen.
func TestLauncherKeyboardContract_ThroughRootUpdate(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	newRoot := func() LauncherRootModel {
		m := NewLauncherRootModel(t.TempDir(), filepath.Join(t.TempDir(), ".lingtai-tui"), "")
		m.width, m.height = 80, 24
		return m
	}
	assertCancel := func(t *testing.T, m LauncherRootModel, key tea.KeyPressMsg) {
		t.Helper()
		updated, cmd := m.Update(key)
		got := updated.(LauncherRootModel)
		if !got.done || got.result.Kind != DecisionCancel || cmd == nil {
			t.Fatalf("key %q did not cancel through LauncherRootModel.Update: done=%v result=%v cmd=%T", key.String(), got.done, got.result.Kind, cmd)
		}
	}

	for _, key := range []tea.KeyPressMsg{{Text: "q"}, {Text: "ctrl+c"}} {
		assertCancel(t, newRoot(), key) // Welcome may cancel.
	}

	m := newRoot()
	m.view = launcherViewChoose
	updated, _ := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if got := updated.(LauncherRootModel); got.view != launcherViewWelcome {
		t.Fatalf("Choose Esc view=%v, want Welcome", got.view)
	}
	for _, key := range []tea.KeyPressMsg{{Text: "q"}, {Text: "ctrl+c"}} {
		m = newRoot()
		m.view = launcherViewChoose
		assertCancel(t, m, key)
	}

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Text: "q"}} {
		m = newRoot()
		m.view = launcherViewPicker
		m.projects = NewProjectsModel(t.TempDir(), filepath.Join(t.TempDir(), ".lingtai"), ProjectsContext{})
		updated, cmd := m.Update(key)
		got := updated.(LauncherRootModel)
		if cmd == nil {
			t.Fatalf("ProjectsModel key %q did not emit its back message", key.String())
		}
		back, ok := runCmd(cmd).(ViewChangeMsg)
		if !ok || back.View != "mail" {
			t.Fatalf("ProjectsModel key %q emitted %T %#v, want ViewChangeMsg{View:mail}", key.String(), runCmd(cmd), back)
		}
		updated, _ = got.Update(back)
		got = updated.(LauncherRootModel)
		if got.view != launcherViewChoose {
			t.Fatalf("ProjectsModel key %q did not return to Choose: view=%v", key.String(), got.view)
		}
	}
	m = newRoot()
	m.view = launcherViewPicker
	m.projects = NewProjectsModel(t.TempDir(), filepath.Join(t.TempDir(), ".lingtai"), ProjectsContext{})
	updated, cmd := m.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	got := updated.(LauncherRootModel)
	if !got.done || got.result.Kind != DecisionCancel || cmd == nil {
		t.Fatalf("picker Ctrl+C did not cancel through root Update: done=%v result=%v cmd=%T", got.done, got.result.Kind, cmd)
	}

	for _, key := range []tea.KeyPressMsg{{Code: tea.KeyEscape}, {Text: "q"}, {Text: "ctrl+c"}} {
		m = newRoot()
		m.view = launcherViewStaging
		m.unfinishedStaging = []string{filepath.Join(t.TempDir(), ".lingtai.create-long")}
		updated, cmd := m.Update(key)
		got := updated.(LauncherRootModel)
		if key.Code == tea.KeyEscape {
			if got.view != launcherViewChoose || cmd != nil {
				t.Fatalf("Staging Esc view=%v cmd=%T, want Choose/no command", got.view, cmd)
			}
		} else if !got.done || got.result.Kind != DecisionCancel || cmd == nil {
			t.Fatalf("Staging key %q did not cancel through root Update: done=%v result=%v cmd=%T", key.String(), got.done, got.result.Kind, cmd)
		}
	}
}
