package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
	updated, _ := m.updateLanding(tea.KeyPressMsg{Code: tea.KeyEnter}) // landingCursor defaults to 0 = "Create new project"
	lm := updated.(LauncherRootModel)
	if lm.view != launcherViewCreate || !lm.firstRunOn {
		t.Fatalf("expected landing Enter to enter the create flow, got view=%v firstRunOn=%v", lm.view, lm.firstRunOn)
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

// TestDraftFirstRun_ThemeLanguageDoNotPersist proves the Welcome step's
// theme-cycle (ctrl+t) and language-confirm (enter) in draftMode hold their
// choice only in the ProjectDraft, never touching tui_config.json.
func TestDraftFirstRun_ThemeLanguageDoNotPersist(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	before := dirSnapshot(t, home)

	m, _ = m.Update(tea.KeyPressMsg{Text: "ctrl+t"})
	m.setupDone = true
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	after := dirSnapshot(t, home)
	assertSnapshotsEqual(t, "draft theme/language", before, after)

	if m.draft == nil || m.draft.Theme == "" {
		t.Fatalf("expected theme choice captured in draft, got %+v", m.draft)
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

	// Welcome -> pick preset through the real Enter path.
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if m.step != stepPickPreset {
		t.Fatalf("expected Welcome Enter to reach stepPickPreset, got %v", m.step)
	}
	assertSnapshotsEqual(t, "after welcome enter", before, dirSnapshot(t, home))

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

	// The typed cancel path itself also remains pure.
	m.step = stepWelcome
	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected Welcome Esc to emit a typed cancel command")
	}
	if _, ok := runCmd(cmd).(ProjectDraftCancelledMsg); !ok {
		t.Fatal("expected ProjectDraftCancelledMsg from Welcome Esc")
	}
	assertSnapshotsEqual(t, "after typed draft cancel", before, dirSnapshot(t, home))
}

// --- Invariant 6: draft cancel semantics via the REAL key path -------------

// TestDraftFirstRun_EscAtWelcomeEmitsCancelMsg drives the ACTUAL "esc"
// keypress at stepWelcome (draftMode's entry step) and proves it emits
// ProjectDraftCancelledMsg — not a bare unhandled no-op, which is what a
// parent review found: plain Esc (distinct from m.welcomeOnly's /settings
// language-only flow) fell through to a silent `return m, nil`.
func TestDraftFirstRun_EscAtWelcomeEmitsCancelMsg(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	if m.step != stepWelcome {
		t.Fatalf("expected draft model to start at stepWelcome, got %v", m.step)
	}
	before := dirSnapshot(t, home)

	_, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if cmd == nil {
		t.Fatal("expected esc at draft Welcome to return a command")
	}
	msg := runCmd(cmd)
	if _, ok := msg.(ProjectDraftCancelledMsg); !ok {
		t.Fatalf("expected ProjectDraftCancelledMsg from esc at draft Welcome, got %T (%v)", msg, msg)
	}

	assertSnapshotsEqual(t, "esc at draft Welcome", before, dirSnapshot(t, home))
}

// TestDraftFirstRun_CtrlCAtWelcomeEmitsCancelMsgNotTeaQuit drives the ACTUAL
// "ctrl+c" keypress at stepWelcome and proves it ALSO emits
// ProjectDraftCancelledMsg rather than a bare tea.Quit. A parent review
// found the prior unconditional `case "ctrl+c": return m, tea.Quit` would,
// because the launcher runs FirstRunModel inside its OWN tea.Program (see
// launcher.go), kill that whole program abruptly — bypassing
// LauncherRootModel's done/result bookkeeping (m.done would stay false) —
// instead of routing back through a proper decision the same way Esc does.
func TestDraftFirstRun_CtrlCAtWelcomeEmitsCancelMsgNotTeaQuit(t *testing.T) {
	m, home, _ := buildDraftModel(t)
	before := dirSnapshot(t, home)

	_, cmd := m.Update(tea.KeyPressMsg{Text: "ctrl+c"})
	if cmd == nil {
		t.Fatal("expected ctrl+c at draft Welcome to return a command")
	}
	msg := runCmd(cmd)
	if _, ok := msg.(ProjectDraftCancelledMsg); !ok {
		t.Fatalf("expected ProjectDraftCancelledMsg from ctrl+c at draft Welcome, got %T (%v) — a bare tea.Quit here would bypass LauncherRootModel entirely", msg, msg)
	}

	assertSnapshotsEqual(t, "ctrl+c at draft Welcome", before, dirSnapshot(t, home))
}

// TestLauncherRootModel_CancelReturnsToLandingAndDiscardsDraft drives the
// launcher root model through the REAL key path — landing Enter on "Create
// new project" (entering enterCreate/NewDraftFirstRunModel), a real "esc"
// keypress inside the hosted FirstRunModel that produces
// ProjectDraftCancelledMsg, feeding that message back into
// LauncherRootModel.Update — and proves the model returns to
// launcherViewLanding with the old draft/firstRun state discarded, not
// merely hidden. This is item 6's exact requirement: "Esc at draft Welcome
// must emit a typed cancel/back result to LauncherRoot, return to landing,
// and discard/reset the old draft."
func TestLauncherRootModel_CancelReturnsToLandingAndDiscardsDraft(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	projectRoot := t.TempDir()
	globalDir := filepath.Join(home, ".lingtai-tui")

	m := NewLauncherRootModel(projectRoot, globalDir, "")
	updated, _ := m.updateLanding(tea.KeyPressMsg{Code: tea.KeyEnter}) // cursor 0 = Create new
	lm := updated.(LauncherRootModel)
	if lm.view != launcherViewCreate || !lm.firstRunOn || lm.draft == nil {
		t.Fatalf("expected Create entry to be live, got view=%v firstRunOn=%v draft=%v", lm.view, lm.firstRunOn, lm.draft)
	}
	firstDraft := lm.draft
	// Mark the draft dirty so a later "resume" bug (reusing the same draft
	// pointer after cancel) would be observable, not silently identical by
	// coincidence of both being freshly zero-valued.
	firstDraft.Theme = "some-theme-from-the-cancelled-attempt"

	// Drive the REAL key path inside the hosted model: esc at stepWelcome.
	updatedFirstRun, cmd := lm.firstRun.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	lm.firstRun = updatedFirstRun
	if cmd == nil {
		t.Fatal("expected a command from esc at draft Welcome")
	}
	msg := runCmd(cmd)
	cancelMsg, ok := msg.(ProjectDraftCancelledMsg)
	if !ok {
		t.Fatalf("expected ProjectDraftCancelledMsg, got %T", msg)
	}

	updated, _ = lm.Update(cancelMsg)
	lm = updated.(LauncherRootModel)

	if lm.view != launcherViewLanding {
		t.Fatalf("expected cancel to return to launcherViewLanding, got %v", lm.view)
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
	updated, _ = lm.updateLanding(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm2 := updated.(LauncherRootModel)
	if lm2.draft == nil {
		t.Fatal("expected a second Create entry to build a new draft")
	}
	if lm2.draft == firstDraft {
		t.Fatal("expected a second Create entry to allocate a NEW *ProjectDraft, not reuse the cancelled one's pointer")
	}
	if lm2.draft.Theme == "some-theme-from-the-cancelled-attempt" {
		t.Fatal("second Create entry's draft carries state from the cancelled attempt — must start fresh")
	}
}

// TestLauncherRootModel_CancelRestoresThemeAndLanguageBaseline drives the
// REAL theme-cycle (ctrl+t) and language-selection (down + enter) keys at
// draft Welcome, then the REAL Esc cancel path, and proves the process-wide
// in-memory theme/language (ActiveTheme()/i18n.Lang()) end up back at the
// PERSISTED baseline — not wherever the cancelled draft last previewed. A
// parent review found SetThemeByName/i18n.SetLang mutate genuinely global
// state regardless of draftMode, and the old ProjectDraftCancelledMsg
// handler discarded the draft pointer without ever restoring either one, so
// the landing page (and the NEXT fresh draft) stayed stuck showing the
// cancelled attempt's preview.
func TestLauncherRootModel_CancelRestoresThemeAndLanguageBaseline(t *testing.T) {
	// ActiveTheme()/i18n.Lang() are genuinely global, process-wide package
	// state (exactly the thing this test is exercising) — restore both to
	// the repo-wide test convention's neutral default on exit, matching
	// every other test in this package that calls i18n.SetLang with a
	// non-"en" value (see e.g. projects_test.go, status_hint_copy_test.go).
	// Omitting this bled "wen" into later tests in the same `go test`
	// binary (e.g. TestLoginModel_EmptyViewShowsAddRow expecting English
	// strings) the first time this test was added — caught by running the
	// full package suite, not just this test in isolation.
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

	// Seed a KNOWN, non-default persisted baseline distinct from whatever
	// the preview keys below will select, so "restored to baseline" is a
	// meaningful assertion rather than trivially true from zero values.
	baseline := config.TUIConfig{Theme: "xuan-paper", Language: "wen", MailPageSize: config.DefaultMailPageSize}
	if err := config.SaveTUIConfig(globalDir, baseline); err != nil {
		t.Fatalf("seed baseline tui_config.json: %v", err)
	}
	SetThemeByName(baseline.Theme)
	if err := i18n.SetLang(baseline.Language); err != nil {
		t.Fatalf("seed baseline language: %v", err)
	}

	m := NewLauncherRootModel(projectRoot, globalDir, "")
	updated, _ := m.updateLanding(tea.KeyPressMsg{Code: tea.KeyEnter}) // cursor 0 = Create new
	lm := updated.(LauncherRootModel)
	if lm.preDraftTheme != baseline.Theme || lm.preDraftLanguage != baseline.Language {
		t.Fatalf("expected enterCreate to capture the persisted baseline, got theme=%q lang=%q", lm.preDraftTheme, lm.preDraftLanguage)
	}

	// Real ctrl+t: cycles ThemeNames() from "xuan-paper" — alphabetically
	// sorted, so this wraps to "ink-dark", a DIFFERENT theme than baseline.
	lm.firstRun.setupDone = true
	updatedFirstRun, _ := lm.firstRun.Update(tea.KeyPressMsg{Text: "ctrl+t"})
	lm.firstRun = updatedFirstRun
	if lm.firstRun.draft.Theme == "" || lm.firstRun.draft.Theme == baseline.Theme {
		t.Fatalf("expected ctrl+t to preview a DIFFERENT theme than baseline in the draft, got %q", lm.firstRun.draft.Theme)
	}

	// Real "up" then "enter": the constructor prefills langCursor from the
	// PERSISTED baseline ("wen", the last of {en,zh,wen}), so "up" is the
	// key that actually moves the selection to a different language
	// ("zh") and previews it via i18n.SetLang before advancing.
	updatedFirstRun, _ = lm.firstRun.Update(tea.KeyPressMsg{Code: tea.KeyUp})
	lm.firstRun = updatedFirstRun
	updatedFirstRun, _ = lm.firstRun.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm.firstRun = updatedFirstRun

	if ActiveTheme().Text == ThemeByName(baseline.Theme).Text && ActiveTheme().BG == ThemeByName(baseline.Theme).BG {
		t.Fatal("expected the in-memory ActiveTheme to have changed away from baseline after ctrl+t preview")
	}
	if i18n.Lang() == baseline.Language {
		t.Fatalf("expected the in-memory i18n language to have changed away from baseline %q after selection, got %q", baseline.Language, i18n.Lang())
	}

	// Real Esc back at Welcome (now on stepPickPreset after the language
	// enter above advanced the wizard — back up to Welcome first via the
	// wizard's own step field is out of scope here; instead cancel directly
	// from whatever step Esc reaches, since the RESTORE behavior lives in
	// LauncherRootModel's ProjectDraftCancelledMsg handler regardless of
	// which step emitted it). Drive Esc from stepPickPreset's own "esc"
	// case, which returns to stepWelcome first — send Esc twice to reach
	// the actual cancel-emitting step.
	updatedFirstRun, cmd := lm.firstRun.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	lm.firstRun = updatedFirstRun
	if lm.firstRun.step != stepWelcome {
		t.Fatalf("expected first esc (from stepPickPreset) to return to stepWelcome, got %v", lm.firstRun.step)
	}
	updatedFirstRun, cmd = lm.firstRun.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	lm.firstRun = updatedFirstRun
	if cmd == nil {
		t.Fatal("expected the second esc (from stepWelcome) to return a command")
	}
	msg := runCmd(cmd)
	cancelMsg, ok := msg.(ProjectDraftCancelledMsg)
	if !ok {
		t.Fatalf("expected ProjectDraftCancelledMsg, got %T", msg)
	}

	updated, _ = lm.Update(cancelMsg)
	lm = updated.(LauncherRootModel)

	if lm.view != launcherViewLanding {
		t.Fatalf("expected cancel to return to launcherViewLanding, got %v", lm.view)
	}
	if lm.draft != nil {
		t.Fatal("expected cancel to discard the draft")
	}

	// The authoritative assertions: process-wide in-memory state restored
	// to the PERSISTED baseline, not the cancelled preview.
	restored := ActiveTheme()
	want := ThemeByName(baseline.Theme)
	if restored.BG != want.BG || restored.Text != want.Text {
		t.Fatalf("expected ActiveTheme restored to baseline %q after cancel, got a different theme", baseline.Theme)
	}
	if i18n.Lang() != baseline.Language {
		t.Fatalf("expected i18n.Lang() restored to baseline %q after cancel, got %q", baseline.Language, i18n.Lang())
	}

	// Persisted config/home snapshot must be untouched — restoration is
	// in-memory only (SetThemeByName/i18n.SetLang), never a write.
	tuiCfgAfter := config.LoadTUIConfig(globalDir)
	if tuiCfgAfter.Theme != baseline.Theme || tuiCfgAfter.Language != baseline.Language {
		t.Fatalf("expected persisted tui_config.json unchanged, got theme=%q lang=%q", tuiCfgAfter.Theme, tuiCfgAfter.Language)
	}

	// A fresh draft started after cancel must begin its preview from the
	// SAME restored baseline, not the just-cancelled preview.
	updated, _ = lm.updateLanding(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm2 := updated.(LauncherRootModel)
	if lm2.preDraftTheme != baseline.Theme || lm2.preDraftLanguage != baseline.Language {
		t.Fatalf("expected a fresh draft's captured baseline to still be %q/%q after a prior cancel, got %q/%q",
			baseline.Theme, baseline.Language, lm2.preDraftTheme, lm2.preDraftLanguage)
	}
}

// --- Invariant 2/7: typed launcher selection is NOT ProjectsAgentSelectedMsg ---

// TestLauncherProjectsModel_EmitsTypedSelectionNotVisitMsg proves a
// NewLauncherProjectsModel selection emits LauncherProjectSelectedMsg, never
// ProjectsAgentSelectedMsg — the exact "typed result, not App visit
// semantics" requirement. Mirrors TestProjectsModelGroupedRowsMarkersAndEnter's
// setup but asserts the launcher-mode message type.
func TestLauncherProjectsModel_EmitsTypedSelectionNotVisitMsg(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "other")
	rec := projectRecord(project, "agent-b", "Agent B", true)
	snap := inventory.Snapshot{
		Records: []inventory.Record{rec},
		Groups:  []inventory.Group{{Project: project, Records: []inventory.Record{rec}}},
	}

	m := NewLauncherProjectsModel("", ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(projectsInventoryForModel(m, snap))

	withProjectsScan(t, func(inventory.Options) (inventory.Snapshot, error) {
		return snap, nil
	})

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("expected a validation command from Enter on an enterable row")
	}
	msg := runCmd(cmd)
	validationMsg, ok := msg.(projectsValidationMsg)
	if !ok {
		t.Fatalf("expected projectsValidationMsg from validateSelection, got %T", msg)
	}
	m, cmd = m.Update(validationMsg)
	if cmd == nil {
		t.Fatal("expected a selection command after successful validation")
	}
	final := runCmd(cmd)

	if _, isVisit := final.(ProjectsAgentSelectedMsg); isVisit {
		t.Fatal("launcher-mode selection must NEVER emit ProjectsAgentSelectedMsg (visit semantics)")
	}
	sel, ok := final.(LauncherProjectSelectedMsg)
	if !ok {
		t.Fatalf("expected LauncherProjectSelectedMsg, got %T", final)
	}
	if sel.ProjectRoot != project {
		t.Fatalf("ProjectRoot = %q, want %q", sel.ProjectRoot, project)
	}
}

// TestLauncherProjectsModel_ReusesFreshnessGuard proves the launcher-mode
// model drops a stale (out-of-order) inventory result exactly like the
// normal visit-mode model does — same acceptsRequest gate, no
// reimplementation. Mirrors TestProjectsModelDropsOutOfOrderInventoryResults.
func TestLauncherProjectsModel_ReusesFreshnessGuard(t *testing.T) {
	m := NewLauncherProjectsModel("", ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})

	fresh := inventory.Snapshot{Records: []inventory.Record{projectRecord("/fresh", "a", "A", true)}}
	stale := inventory.Snapshot{Records: []inventory.Record{projectRecord("/stale", "b", "B", true)}}

	// Bump requestSeq once (simulating a refresh) before the stale result
	// arrives, so it carries an old requestSeq.
	staleSeq := m.requestSeq
	m.requestSeq++

	m, _ = m.Update(projectsInventoryMsg{activationID: m.activationID, requestSeq: m.requestSeq, snapshot: fresh})
	m, _ = m.Update(projectsInventoryMsg{activationID: m.activationID, requestSeq: staleSeq, snapshot: stale})

	if len(m.snapshot.Records) != 1 || m.snapshot.Records[0].Project != "/fresh" {
		t.Fatalf("stale result was not dropped: %+v", m.snapshot.Records)
	}
}

// TestLauncherProjectsModel_ValidationRejectsStaleTargets proves launcher
// mode reuses the exact same re-validation-before-selection path as visit
// mode — a target that stopped between listing and Enter must not emit a
// selection message. Mirrors TestProjectsModelValidationRejectsStaleTargets.
func TestLauncherProjectsModel_ValidationRejectsStaleTargets(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "project")
	rec := projectRecord(project, "agent", "Agent", true)
	initial := inventory.Snapshot{Records: []inventory.Record{rec}, Groups: []inventory.Group{{Project: project, Records: []inventory.Record{rec}}}}

	withProjectsScan(t, func(inventory.Options) (inventory.Snapshot, error) {
		return inventory.Snapshot{}, nil // target vanished between list and Enter
	})

	m := NewLauncherProjectsModel("", ProjectsContext{})
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 24})
	m, _ = m.Update(projectsInventoryForModel(m, initial))

	m, cmd := m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	m, cmd = m.Update(runCmd(cmd))
	if cmd != nil {
		t.Fatal("stale target must not emit a selection command in launcher mode either")
	}
	if !strings.Contains(m.status, "Target stopped or changed") {
		t.Fatalf("status = %q, want stale-target message", m.status)
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
// by the time the landing page checked it and the entire Resume/Discard UI
// was unreachable dead code). This drives the real key path (landing Enter
// on "Create new") rather than reading the field directly, so it also proves
// updateLanding's `len(m.unfinishedStaging) > 0` branch fires.
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

	// Drive the actual key path: landing Enter on "Create new" (cursor 0)
	// must route to the unfinished-staging screen instead of straight into
	// enterCreate, because len(m.unfinishedStaging) > 0.
	m.landingCursor = 0
	updated, _ := m.updateLanding(tea.KeyPressMsg{Code: tea.KeyEnter})
	lm := updated.(LauncherRootModel)
	if !lm.showUnfinishedStaging {
		t.Fatal("expected landing Enter on Create to route to the unfinished-staging screen when unfinishedStaging is non-empty")
	}
	if lm.view != launcherViewOpenExisting {
		t.Fatalf("expected showUnfinishedStaging to reuse launcherViewOpenExisting's key surface, got view=%v", lm.view)
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

// TestLauncherRoot_RevalidatesAsyncTypedSelection proves the launcher root
// applies the same activation/request freshness guard as App and validates
// the project again at the final Open Existing decision boundary.
func TestLauncherRoot_RevalidatesAsyncTypedSelection(t *testing.T) {
	validRoot := t.TempDir()
	if err := os.Mkdir(filepath.Join(validRoot, ".lingtai"), 0o755); err != nil {
		t.Fatalf("create valid project: %v", err)
	}

	lm := NewLauncherRootModel(t.TempDir(), t.TempDir(), "")
	lm.view = launcherViewOpenExisting
	lm.projects = NewLauncherProjectsModel(lm.globalDirPath, ProjectsContext{})
	lm.projects.activationID = 7
	lm.projects.requestSeq = 9

	updated, cmd := lm.Update(LauncherProjectSelectedMsg{
		ActivationID: 7,
		RequestSeq:   8, // stale
		ProjectRoot:  validRoot,
	})
	got := updated.(LauncherRootModel)
	if got.done || cmd != nil {
		t.Fatalf("stale typed selection was accepted: done=%v cmd=%T", got.done, cmd)
	}

	updated, cmd = got.Update(LauncherProjectSelectedMsg{
		ActivationID: 7,
		RequestSeq:   9,
		ProjectRoot:  filepath.Join(t.TempDir(), "missing-project"),
	})
	got = updated.(LauncherRootModel)
	if got.done || cmd != nil {
		t.Fatalf("missing project selection was accepted: done=%v cmd=%T", got.done, cmd)
	}
	if got.projects.status != i18n.T("projects.target_changed") {
		t.Fatalf("missing project status = %q, want target-changed message", got.projects.status)
	}

	updated, cmd = got.Update(LauncherProjectSelectedMsg{
		ActivationID: 7,
		RequestSeq:   9,
		ProjectRoot:  validRoot,
	})
	got = updated.(LauncherRootModel)
	if !got.done || cmd == nil {
		t.Fatalf("fresh valid selection was not accepted: done=%v cmd=%T", got.done, cmd)
	}
	wantRoot, err := filepath.Abs(validRoot)
	if err != nil {
		t.Fatalf("abs valid root: %v", err)
	}
	if got.result.Kind != DecisionOpenExisting || got.result.ProjectRoot != filepath.Clean(wantRoot) {
		t.Fatalf("result = (%v, %q), want (%v, %q)", got.result.Kind, got.result.ProjectRoot, DecisionOpenExisting, filepath.Clean(wantRoot))
	}
}

// TestLauncherRegisteredSelection_RevalidatesLiveness proves a row that was
// Alive when the catalog loaded cannot route a now-missing .lingtai path into
// the normal write-capable startup pipeline.
func TestLauncherRegisteredSelection_RevalidatesLiveness(t *testing.T) {
	lm := NewLauncherRootModel(t.TempDir(), t.TempDir(), "")
	lm.view = launcherViewOpenExisting
	lm.openExistingRegistered = []config.RegisteredProject{{
		Path:  filepath.Join(t.TempDir(), "vanished"),
		Alive: true, // deliberately stale opening-time snapshot
	}}

	updated, cmd := lm.updateOpenExisting(tea.KeyPressMsg{Code: tea.KeyEnter})
	got := updated.(LauncherRootModel)
	if got.done || cmd != nil {
		t.Fatalf("stale registered row was accepted: done=%v cmd=%T", got.done, cmd)
	}
	row := got.openExistingRegistered[0]
	if row.Alive || row.StaleReason != "missing_dir" {
		t.Fatalf("stale row not disabled after revalidation: %+v", row)
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
