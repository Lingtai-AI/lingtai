package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// minimalDraftPreset returns a preset with just enough manifest shape to
// pass through preset.GenerateInitJSONWithOpts without a real LLM API key,
// and a non-empty Description.Summary so it also passes Preset.Validate()
// (required by validateDraftForCommit's commit-boundary check — see
// project_create.go). base_url is set because minimax has a region table,
// and Validate requires a non-empty endpoint for those providers. It uses the
// INTL endpoint specifically: that is what an absent base_url already resolved
// to through preset.regionSuffix, so the api_key_env stamping these tests
// assert on (MINIMAX_INTL_1_API_KEY) is unchanged.
func minimalDraftPreset() preset.Preset {
	return preset.Preset{
		Name:        "test-preset",
		Description: preset.PresetDescription{Summary: "test fixture preset"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "minimax",
				"model":    "test-model",
				"base_url": preset.ProviderRegionURLs["minimax"][1].URL, // [1] = INTL
			},
		},
	}
}

// newTestDraft builds a ProjectDraft with no recipe (so RunProjectCreate
// takes the no-recipe init.json-only branch) rooted at a fresh temp dir.
func newTestDraft(t *testing.T) (*ProjectDraft, string) {
	t.Helper()
	root := t.TempDir()
	draft := NewProjectDraft(root)
	p := minimalDraftPreset()
	draft.DraftPreset = &p
	draft.AgentName = "orchestrator"
	draft.AgentDirName = "orchestrator"
	draft.AgentOpts = preset.DefaultAgentOpts()
	return draft, root
}

func testCreateOptions(t *testing.T, expectedProjectRoot string) CreateOptions {
	t.Helper()
	home := t.TempDir()
	setTestHome(t, home)
	// Non-dirty drafts must point at an exact backing preset. Seed the saved
	// fixture after HOME is isolated so dependency verification can distinguish
	// a real selection from the stale/missing-selection cases below.
	seed := minimalDraftPreset()
	seed.Source = preset.SourceSaved
	if err := preset.Save(seed); err != nil {
		t.Fatalf("seed saved preset dependency: %v", err)
	}
	return CreateOptions{
		GlobalDir:           filepath.Join(home, ".lingtai-tui"),
		ExpectedProjectRoot: expectedProjectRoot,
		// Production still checks runtime readiness (read-only) when
		// LingtaiCmd was empty at launcher entry. Tests inject a no-op so
		// they stay hermetic; the empty command then records a post-commit
		// warning instead of launching.
		RuntimeReady:      func(string) error { return nil },
		ResolveLingtaiCmd: func(string) string { return "" },
	}
}

func activePresetRefFromInit(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read init.json: %v", err)
	}
	var doc struct {
		Manifest struct {
			Preset struct {
				Active string `json:"active"`
			} `json:"preset"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	if doc.Manifest.Preset.Active == "" {
		t.Fatal("init.json manifest.preset.active is empty")
	}
	return doc.Manifest.Preset.Active
}

// TestRunProjectCreate_Success proves the happy path: staging disappears,
// the final .lingtai/ tree is complete with exactly one orchestrator, and
// the result reports Committed=true with no error.
func TestRunProjectCreate_Success(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)

	res := RunProjectCreate(draft, opts)

	if res.Err != nil {
		t.Fatalf("unexpected error: %v (phase %v)", res.Err, res.FailedPhase)
	}
	if !res.Committed {
		t.Fatal("expected Committed=true on success")
	}
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(finalDir); err != nil {
		t.Fatalf("final .lingtai/ missing: %v", err)
	}
	orchestrators := DetectOrchestrators(finalDir)
	if len(orchestrators) != 1 {
		t.Fatalf("expected exactly 1 orchestrator, got %d: %v", len(orchestrators), orchestrators)
	}
	// No leftover staging directories.
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".lingtai.create-") {
			t.Fatalf("leftover staging directory after success: %s", e.Name())
		}
	}
}

func TestRunProjectCreate_AppliesRecipeInsideStagingBeforeRename(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	recipeRoot := t.TempDir()
	writeRecipeFile(t, filepath.Join(recipeRoot, ".recipe", "recipe.json"),
		`{"id":"staged","name":"Staged Recipe","description":"d","library_name":null}`)
	writeRecipeFile(t, filepath.Join(recipeRoot, ".recipe", "greet", "greet.md"), "hello {{addr}}")
	draft.RecipeName = preset.RecipeCustom
	draft.RecipeCustomDir = recipeRoot

	var checkedBeforeRename bool
	opts.InjectFailure = func(phase CreatePhase) error {
		if phase != PhaseRename {
			return nil
		}
		checkedBeforeRename = true
		finalDir := filepath.Join(root, ".lingtai")
		if _, err := os.Lstat(finalDir); !os.IsNotExist(err) {
			t.Fatalf("final .lingtai existed before atomic rename: %v", err)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		var stagingDir string
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".lingtai.create-") {
				stagingDir = filepath.Join(root, entry.Name())
				break
			}
		}
		if stagingDir == "" {
			t.Fatal("staging directory missing before rename")
		}
		prompt, err := os.ReadFile(filepath.Join(stagingDir, "orchestrator", ".prompt"))
		if err != nil {
			t.Fatalf("staged recipe prompt missing: %v", err)
		}
		if string(prompt) != "hello human" {
			t.Fatalf("staged prompt = %q, want %q", prompt, "hello human")
		}
		if _, err := os.Stat(filepath.Join(stagingDir, ".tui-asset", ".recipe", "recipe.json")); err != nil {
			t.Fatalf("staged recipe snapshot missing: %v", err)
		}
		return nil
	}

	res := RunProjectCreate(draft, opts)
	if res.Err != nil || !res.Committed {
		t.Fatalf("create result = committed %v err %v (phase %v)", res.Committed, res.Err, res.FailedPhase)
	}
	if !checkedBeforeRename {
		t.Fatal("rename boundary was not checked")
	}
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(filepath.Join(finalDir, "orchestrator", ".prompt")); err != nil {
		t.Fatalf("published recipe prompt missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(finalDir, ".tui-asset", ".recipe", "recipe.json")); err != nil {
		t.Fatalf("published recipe snapshot missing: %v", err)
	}
}

// TestRunProjectCreate_ChecksRuntimeReadinessWhenCommandWasInitiallyUnknown
// proves the post-commit launch phase checks runtime readiness exactly once
// (read-only — RuntimeReady must never install/repair on its own) when the
// launcher entry began with no resolved lingtai command.
func TestRunProjectCreate_ChecksRuntimeReadinessWhenCommandWasInitiallyUnknown(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	readyCalls := 0
	opts.RuntimeReady = func(globalDir string) error {
		readyCalls++
		if globalDir != opts.GlobalDir {
			t.Fatalf("RuntimeReady globalDir = %q, want %q", globalDir, opts.GlobalDir)
		}
		return nil
	}

	res := RunProjectCreate(draft, opts)
	if res.Err != nil || !res.Committed {
		t.Fatalf("create result = committed %v err %v", res.Committed, res.Err)
	}
	if readyCalls != 1 {
		t.Fatalf("RuntimeReady calls = %d, want 1", readyCalls)
	}
	foundUnavailable := false
	for _, warning := range res.PostCommitWarnings {
		if strings.Contains(warning, "runtime command unavailable") {
			foundUnavailable = true
			break
		}
	}
	if !foundUnavailable {
		t.Fatalf("post-commit warnings = %v, want unavailable-runtime warning", res.PostCommitWarnings)
	}
}

// TestRunProjectCreate_NotReadyRuntimeSurfacesWarningWithoutMutating proves
// that when RuntimeReady reports the runtime is not ready (e.g. the human
// declined the preflight's install/repair prompt, or this is a from-scratch
// machine), RunProjectCreate surfaces that as a post-commit warning and
// never falls back to installing/repairing on its own — there is no second
// mutating runtime path in this post-commit phase.
func TestRunProjectCreate_NotReadyRuntimeSurfacesWarningWithoutMutating(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	readyCalls := 0
	opts.RuntimeReady = func(string) error {
		readyCalls++
		return errors.New("lingtai kernel runtime is not ready")
	}

	res := RunProjectCreate(draft, opts)
	if res.Err != nil || !res.Committed {
		t.Fatalf("create result = committed %v err %v", res.Committed, res.Err)
	}
	if readyCalls != 1 {
		t.Fatalf("RuntimeReady calls = %d, want 1", readyCalls)
	}
	foundNotReady := false
	for _, warning := range res.PostCommitWarnings {
		if strings.Contains(warning, "not ready") {
			foundNotReady = true
			break
		}
	}
	if !foundNotReady {
		t.Fatalf("post-commit warnings = %v, want a not-ready runtime warning", res.PostCommitWarnings)
	}
}

// TestRunProjectCreate_ConcurrentCreationRejected proves that if the final
// .lingtai/ already exists (simulating a concurrent creation or a symlink),
// RunProjectCreate refuses to overwrite it and leaves it untouched. This is
// now caught at PhaseValidateDraft — validateDraftForCommit's own Lstat
// check, which runs before ANY staging write (see project_create.go) — one
// phase earlier than the historical PhaseCreateStaging revalidation, which
// still exists as a defense-in-depth re-check immediately before
// os.MkdirTemp but is no longer the FIRST place this is caught.
func TestRunProjectCreate_ConcurrentCreationRejected(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)

	finalDir := filepath.Join(root, ".lingtai")
	if err := os.MkdirAll(finalDir, 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(finalDir, "someone-elses-file")
	if err := os.WriteFile(sentinel, []byte("do not touch"), 0o644); err != nil {
		t.Fatal(err)
	}

	res := RunProjectCreate(draft, opts)

	if res.Committed {
		t.Fatal("expected Committed=false when final dir already exists")
	}
	if res.Err == nil {
		t.Fatal("expected an error for concurrent creation")
	}
	if res.FailedPhase != PhaseValidateDraft {
		t.Fatalf("expected failure at PhaseValidateDraft, got %v", res.FailedPhase)
	}
	data, err := os.ReadFile(sentinel)
	if err != nil || string(data) != "do not touch" {
		t.Fatalf("existing final dir was modified: err=%v data=%q", err, data)
	}
}

// failPhaseInjector returns a createFailureInjector that fails exactly the
// named phase and no other.
func failPhaseInjector(target CreatePhase) createFailureInjector {
	return func(phase CreatePhase) error {
		if phase == target {
			return errors.New("injected failure at " + phase.String())
		}
		return nil
	}
}

// TestRunProjectCreate_PreRenameFailureLeavesNoProject is the commit-matrix
// test: for every phase strictly before PhaseRename, injecting a failure
// there must result in (a) no final .lingtai/ directory, and (b) no leftover
// staging directory (cleanup removed it, gated by the ownership marker).
func TestRunProjectCreate_PreRenameFailureLeavesNoProject(t *testing.T) {
	phases := []CreatePhase{
		PhaseInitProject,
		PhaseApplyPreset,
		PhaseApplyRecipe,
		PhaseValidate,
		PhasePersistDependencies,
	}
	for _, phase := range phases {
		phase := phase
		t.Run(phase.String(), func(t *testing.T) {
			draft, root := newTestDraft(t)
			opts := testCreateOptions(t, root)
			opts.InjectFailure = failPhaseInjector(phase)

			res := RunProjectCreate(draft, opts)

			if res.Committed {
				t.Fatalf("phase %v: expected Committed=false", phase)
			}
			if res.Err == nil {
				t.Fatalf("phase %v: expected an error", phase)
			}
			if res.FailedPhase != phase {
				t.Fatalf("phase %v: expected FailedPhase=%v, got %v", phase, phase, res.FailedPhase)
			}
			finalDir := filepath.Join(root, ".lingtai")
			if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
				t.Fatalf("phase %v: final .lingtai/ must not exist, stat err = %v", phase, err)
			}
			entries, _ := os.ReadDir(root)
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), ".lingtai.create-") {
					t.Fatalf("phase %v: leftover staging directory %s not cleaned up", phase, e.Name())
				}
			}
		})
	}
}

// TestRunProjectCreate_RenameFailureLeavesNoProjectAndCleansStaging covers
// the rename phase itself: injecting a failure there must behave exactly
// like a pre-rename failure (no final dir, staging cleaned up) since the
// injector runs before the real os.Rename call.
func TestRunProjectCreate_RenameFailureLeavesNoProjectAndCleansStaging(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	opts.InjectFailure = failPhaseInjector(PhaseRename)

	res := RunProjectCreate(draft, opts)

	if res.Committed {
		t.Fatal("expected Committed=false when rename is injected to fail")
	}
	if res.FailedPhase != PhaseRename {
		t.Fatalf("expected FailedPhase=PhaseRename, got %v", res.FailedPhase)
	}
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist after rename failure, stat err = %v", err)
	}
	entries, _ := os.ReadDir(root)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".lingtai.create-") {
			t.Fatalf("leftover staging directory %s after rename failure", e.Name())
		}
	}
}

// TestRunProjectCreate_PostRenameFailureLeavesValidRetryableProject is the
// other half of the commit matrix: for every phase strictly after
// PhaseRename, injecting a failure there must NOT roll back the already
// -published project — Committed stays true, the final directory is a
// complete valid project, and the failure surfaces only as a
// PostCommitWarning, never as res.Err.
func TestRunProjectCreate_PostRenameFailureLeavesValidRetryableProject(t *testing.T) {
	phases := []CreatePhase{
		PhasePostCommitConfig,
		PhasePostCommitRegister,
		PhasePostCommitLaunch,
	}
	for _, phase := range phases {
		phase := phase
		t.Run(phase.String(), func(t *testing.T) {
			draft, root := newTestDraft(t)
			opts := testCreateOptions(t, root)
			opts.InjectFailure = failPhaseInjector(phase)

			res := RunProjectCreate(draft, opts)

			if !res.Committed {
				t.Fatalf("phase %v: expected Committed=true (post-rename failures never roll back)", phase)
			}
			if res.Err != nil {
				t.Fatalf("phase %v: post-commit failure must not set res.Err, got %v", phase, res.Err)
			}
			if len(res.PostCommitWarnings) == 0 {
				t.Fatalf("phase %v: expected a PostCommitWarning", phase)
			}
			finalDir := filepath.Join(root, ".lingtai")
			if _, err := os.Stat(finalDir); err != nil {
				t.Fatalf("phase %v: final .lingtai/ must still exist and be valid: %v", phase, err)
			}
			orchestrators := DetectOrchestrators(finalDir)
			if len(orchestrators) != 1 {
				t.Fatalf("phase %v: expected exactly 1 orchestrator in the retained project, got %d", phase, len(orchestrators))
			}
		})
	}
}

// --- Invariant 4: launch dependencies precede publication ------------------

// newTestDraftWithDirtyPreset is newTestDraft but with DraftPresetDirty=true
// — the exact condition that triggers RunProjectCreate's preset.Save call.
// The preset name is unique per-call (via t.Name()) so tests can assert on
// its specific saved/<name>.json path without collisions across parallel
// subtests sharing the same isolated HOME (they don't, each testCreateOptions
// call makes a fresh HOME, but keeping names unique is cheap insurance).
func newTestDraftWithDirtyPreset(t *testing.T) (*ProjectDraft, string, string) {
	t.Helper()
	root := t.TempDir()
	draft := NewProjectDraft(root)
	p := minimalDraftPreset()
	p.Name = "dirty-test-preset"
	draft.DraftPreset = &p
	draft.DraftPresetDirty = true
	draft.AgentName = "orchestrator"
	draft.AgentDirName = "orchestrator"
	draft.AgentOpts = preset.DefaultAgentOpts()
	return draft, root, p.Name
}

// TestRunProjectCreate_DirtyPresetPreValidationFailureDoesNotSavePreset proves
// the issue-scoped ordering boundary: a dirty preset is not persisted until
// the staged project passes every build and orchestrator-validation phase.
// Other pre-validation global writes (notably soul-flow configuration) are a
// separate contract and deliberately not asserted here.
func TestRunProjectCreate_DirtyPresetPreValidationFailureDoesNotSavePreset(t *testing.T) {
	phases := []CreatePhase{
		PhaseApplyRecipe, // the first phase strictly AFTER PhaseApplyPreset
		PhaseValidate,
	}
	for _, phase := range phases {
		phase := phase
		t.Run(phase.String(), func(t *testing.T) {
			draft, root, presetName := newTestDraftWithDirtyPreset(t)
			opts := testCreateOptions(t, root)
			opts.InjectFailure = failPhaseInjector(phase)

			res := RunProjectCreate(draft, opts)

			if res.Committed {
				t.Fatalf("phase %v: expected Committed=false", phase)
			}
			finalDir := filepath.Join(root, ".lingtai")
			if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
				t.Fatalf("phase %v: final .lingtai/ must not exist, stat err = %v", phase, err)
			}

			presetPath := filepath.Join(opts.GlobalDir, "presets", "saved", presetName+".json")
			if _, err := os.Stat(presetPath); !os.IsNotExist(err) {
				t.Fatalf("phase %v: dirty preset must NOT be saved to disk when a pre-rename phase fails, but found %s", phase, presetPath)
			}
		})
	}
}

// TestRunProjectCreate_DirtyPresetSavedAndReferencedOnSuccess proves the
// successful final state. The separate rename-failure test below observes the
// stronger ordering guarantee that this dependency exists before publication.
func TestRunProjectCreate_DirtyPresetSavedAndReferencedOnSuccess(t *testing.T) {
	draft, root, presetName := newTestDraftWithDirtyPreset(t)
	opts := testCreateOptions(t, root)

	res := RunProjectCreate(draft, opts)

	if res.Err != nil {
		t.Fatalf("unexpected error: %v (phase %v)", res.Err, res.FailedPhase)
	}
	if !res.Committed {
		t.Fatal("expected Committed=true")
	}
	// testCreateOptions intentionally resolves no runtime command so tests
	// never launch a real agent. The resulting post-commit launch warning is
	// expected here; this test only requires the config/preset phase to finish
	// without its own warning.
	if len(res.PostCommitWarnings) != 1 || !strings.Contains(res.PostCommitWarnings[0], "post_commit_launch: runtime command unavailable") {
		t.Fatalf("unexpected post-commit warnings: %v", res.PostCommitWarnings)
	}

	presetPath := filepath.Join(opts.GlobalDir, "presets", "saved", presetName+".json")
	savedData, err := os.ReadFile(presetPath)
	if err != nil {
		t.Fatalf("read dirty preset saved on successful create: %v", err)
	}
	var saved preset.Preset
	if err := json.Unmarshal(savedData, &saved); err != nil {
		t.Fatalf("parse saved dirty preset: %v", err)
	}
	if saved.Name != presetName {
		t.Fatalf("saved preset name = %q, want %q", saved.Name, presetName)
	}
	savedLLM, _ := saved.Manifest["llm"].(map[string]interface{})
	if got, _ := savedLLM["api_key_env"].(string); got != "MINIMAX_INTL_1_API_KEY" {
		t.Fatalf("saved preset api_key_env = %q, want normalized/stamped MINIMAX_INTL_1_API_KEY", got)
	}

	// The staged (now final) init.json must reference the same preset path that
	// dependency persistence verified before rename.
	initPath := filepath.Join(root, ".lingtai", "orchestrator", "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		t.Fatalf("read staged init.json: %v", err)
	}
	if !strings.Contains(string(data), presetName) {
		t.Fatalf("expected init.json to reference preset %q, got: %s", presetName, data)
	}
}

// TestRunProjectCreate_DirtyPresetPersistenceFailureDoesNotPublishProject is
// the primary issue-771 RED. A real saved-path collision makes preset.Save
// fail deterministically; launch-critical dependency failure must happen
// before rename, clean staging, and leave no final .lingtai/.
func TestRunProjectCreate_DirtyPresetPersistenceFailureDoesNotPublishProject(t *testing.T) {
	draft, root, presetName := newTestDraftWithDirtyPreset(t)
	opts := testCreateOptions(t, root)
	runtimeReadyCalled := false
	opts.RuntimeReady = func(string) error {
		runtimeReadyCalled = true
		return nil
	}
	presetPath := filepath.Join(opts.GlobalDir, "presets", "saved", presetName+".json")
	if err := os.Mkdir(presetPath, 0o755); err != nil {
		t.Fatalf("create deterministic preset save collision: %v", err)
	}

	res := RunProjectCreate(draft, opts)

	if res.Committed {
		t.Fatal("expected Committed=false when dirty preset persistence fails")
	}
	if res.Err == nil {
		t.Fatal("expected dirty preset persistence error")
	}
	if got := res.FailedPhase.String(); got != "persist_dependencies" {
		t.Fatalf("failed phase = %q, want persist_dependencies", got)
	}
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist after dependency failure, stat err = %v", err)
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".lingtai.create-") {
			t.Fatalf("leftover staging directory after dependency failure: %s", entry.Name())
		}
	}
	if runtimeReadyCalled {
		t.Fatal("runtime launch boundary ran after dependency persistence failed")
	}
}

// Once the staged project is valid, its exact preset and newly staged
// credentials are persisted before PhaseRename. A rename failure may leave
// those valid user-owned dependencies behind, but must not publish a project.
func TestRunProjectCreate_DependenciesPersistBeforeRenameFailure(t *testing.T) {
	draft, root, presetName := newTestDraftWithDirtyPreset(t)
	opts := testCreateOptions(t, root)
	const keyEnv = "ISSUE_771_API_KEY"
	llm := draft.DraftPreset.Manifest["llm"].(map[string]interface{})
	llm["api_key_env"] = keyEnv
	draft.DraftAPIKeyEnv = keyEnv
	draft.DraftAPIKey = secretString("synthetic-api-key")
	draft.DraftCodexTokens = secretBytes(`{"refresh_token":"synthetic-refresh","access_token":"synthetic-access"}`)

	var phases []string
	opts.InjectFailure = func(phase CreatePhase) error {
		phases = append(phases, phase.String())
		if phase == PhaseRename {
			return errors.New("injected rename failure")
		}
		return nil
	}

	res := RunProjectCreate(draft, opts)
	if res.Committed {
		t.Fatal("rename failure must leave Committed=false")
	}
	if res.Err == nil || res.FailedPhase != PhaseRename {
		t.Fatalf("result err=%v phase=%v, want rename failure", res.Err, res.FailedPhase)
	}
	persistIndex, renameIndex := -1, -1
	for i, phase := range phases {
		switch phase {
		case "persist_dependencies":
			persistIndex = i
		case PhaseRename.String():
			renameIndex = i
		}
	}
	if persistIndex < 0 || renameIndex < 0 || persistIndex >= renameIndex {
		t.Fatalf("phase order = %v, want persist_dependencies before rename", phases)
	}

	ref := preset.RefFor(preset.Preset{Name: presetName, Source: preset.SourceSaved})
	resolved := preset.ResolveRefs([]string{ref}, map[string]string{keyEnv: "present"})
	if len(resolved) != 1 || !resolved[0].Exists || !resolved[0].ManifestValid {
		t.Fatalf("saved dependency %q did not resolve after rename failure: %+v", ref, resolved)
	}
	cfg, err := config.LoadConfigReadOnly(opts.GlobalDir)
	if err != nil {
		t.Fatalf("read persisted API-key config: %v", err)
	}
	if cfg.Keys[keyEnv] == "" {
		t.Fatalf("pending API key %s was not persisted before rename", keyEnv)
	}
	if _, ok := readCodexTokenFile(legacyCodexAuthPath(opts.GlobalDir)); !ok {
		t.Fatal("pending Codex token bundle was not available before rename")
	}
	if _, err := os.Stat(filepath.Join(root, ".lingtai")); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist after rename failure, stat err = %v", err)
	}
}

func TestRunProjectCreate_StaleSelectedPresetDoesNotPublishProject(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	missing := minimalDraftPreset()
	missing.Name = "deleted-selection"
	missing.Source = preset.SourceSaved
	draft.DraftPreset = &missing

	res := RunProjectCreate(draft, opts)
	if res.Committed {
		t.Fatal("stale selected preset must not publish a project")
	}
	if res.Err == nil || res.FailedPhase.String() != "persist_dependencies" {
		t.Fatalf("result err=%v phase=%v, want persist_dependencies failure", res.Err, res.FailedPhase)
	}
	if _, err := os.Stat(filepath.Join(root, ".lingtai")); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist for a stale selection, stat err = %v", err)
	}
}

func TestRunProjectCreate_PendingAPIKeyPersistenceFailureDoesNotPublishProject(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.DraftAPIKeyEnv = "ISSUE_771_API_KEY"
	draft.DraftAPIKey = secretString("synthetic-api-key")
	opts.InjectFailure = func(phase CreatePhase) error {
		if phase.String() != "persist_dependencies" {
			return nil
		}
		envPath := filepath.Join(opts.GlobalDir, ".env")
		if err := os.Remove(envPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return os.Mkdir(envPath, 0o755)
	}

	res := RunProjectCreate(draft, opts)
	if res.Committed {
		t.Fatal("API-key persistence failure must not publish a project")
	}
	if res.Err == nil || res.FailedPhase.String() != "persist_dependencies" {
		t.Fatalf("result err=%v phase=%v, want persist_dependencies failure", res.Err, res.FailedPhase)
	}
	if _, err := os.Stat(filepath.Join(root, ".lingtai")); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist after API-key failure, stat err = %v", err)
	}
}

// A fresh-home draft may select a compiled built-in before templates have
// ever been materialized. The TUI-owned template dependency must exist and
// resolve at its exact template ref before rename, without changing the ref to
// saved/ or treating a missing user-owned saved selection the same way.
func TestRunProjectCreate_ColdCompiledTemplateMaterializesExactRefBeforeRename(t *testing.T) {
	draft, root := newTestDraft(t)
	var selected preset.Preset
	for _, candidate := range preset.BuiltinPresets() {
		if candidate.Name == "minimax" {
			selected = candidate
			break
		}
	}
	if selected.Name == "" {
		t.Fatal("minimax built-in fixture missing")
	}
	selected.Source = preset.SourceTemplate
	draft.DraftPreset = &selected
	draft.DraftPresetDirty = false
	opts := testCreateOptions(t, root)

	var resolvedBeforeRename bool
	opts.InjectFailure = func(phase CreatePhase) error {
		if phase == PhaseRename {
			resolved := preset.ResolveRefs([]string{preset.RefFor(selected)}, nil)
			resolvedBeforeRename = len(resolved) == 1 && resolved[0].Exists && resolved[0].ManifestValid
		}
		return nil
	}

	res := RunProjectCreate(draft, opts)
	if res.Err != nil || !res.Committed {
		t.Fatalf("create result = committed %v err %v (phase %v)", res.Committed, res.Err, res.FailedPhase)
	}
	if !resolvedBeforeRename {
		t.Fatal("compiled template dependency was not resolvable before rename")
	}
	wantRef := preset.RefFor(selected)
	gotRef := activePresetRefFromInit(t, filepath.Join(root, ".lingtai", "orchestrator", "init.json"))
	if gotRef != wantRef {
		t.Fatalf("final init active preset ref = %q, want %q", gotRef, wantRef)
	}
}

func TestRunProjectCreate_PendingCodexPersistenceFailureDoesNotPublishProject(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.DraftCodexTokens = secretBytes(`{"refresh_token":"synthetic-refresh","access_token":"synthetic-access"}`)
	if err := os.Mkdir(legacyCodexAuthPath(opts.GlobalDir), 0o755); err != nil {
		t.Fatalf("create deterministic Codex auth collision: %v", err)
	}

	res := RunProjectCreate(draft, opts)
	if res.Committed {
		t.Fatal("Codex token persistence failure must not publish a project")
	}
	if res.Err == nil || res.FailedPhase.String() != "persist_dependencies" {
		t.Fatalf("result err=%v phase=%v, want persist_dependencies failure", res.Err, res.FailedPhase)
	}
	if _, err := os.Stat(filepath.Join(root, ".lingtai")); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist after Codex persistence failure, stat err = %v", err)
	}
}

func TestRunProjectCreate_SuccessPublishesResolvableSavedRefAndCredentialsBeforeLaunch(t *testing.T) {
	draft, root := newTestDraft(t)
	p := minimalDraftPreset()
	p.Name = "minimax"
	p.Source = preset.SourceTemplate
	const (
		keyEnv         = "ISSUE_771_API_KEY"
		expectedKey    = "synthetic-api-key"
		expectedTokens = `{"refresh_token":"synthetic-refresh","access_token":"synthetic-access"}`
	)
	p.Manifest["llm"].(map[string]interface{})["api_key_env"] = keyEnv
	draft.DraftPreset = &p
	draft.DraftPresetDirty = true
	draft.DraftAPIKeyEnv = keyEnv
	draft.DraftAPIKey = secretString(expectedKey)
	draft.DraftCodexTokens = secretBytes(expectedTokens)
	opts := testCreateOptions(t, root)

	var launchSawKey, launchSawTokens bool
	opts.RuntimeReady = func(globalDir string) error {
		cfg, err := config.LoadConfigReadOnly(globalDir)
		launchSawKey = err == nil && cfg.Keys[keyEnv] == expectedKey
		persistedTokens, readErr := os.ReadFile(legacyCodexAuthPath(globalDir))
		launchSawTokens = readErr == nil && string(persistedTokens) == expectedTokens
		return errors.New("test stops before process launch")
	}

	res := RunProjectCreate(draft, opts)
	if res.Err != nil || !res.Committed {
		t.Fatalf("create result = committed %v err %v (phase %v)", res.Committed, res.Err, res.FailedPhase)
	}
	if !launchSawKey || !launchSawTokens {
		t.Fatalf("launch boundary saw key=%v tokens=%v, want both persisted", launchSawKey, launchSawTokens)
	}

	initPath := filepath.Join(root, ".lingtai", "orchestrator", "init.json")
	ref := activePresetRefFromInit(t, initPath)
	wantRef := "~/.lingtai-tui/presets/saved/minimax.json"
	if ref != wantRef {
		t.Fatalf("final init active preset ref = %q, want %q", ref, wantRef)
	}
	resolved := preset.ResolveRefs([]string{ref}, map[string]string{keyEnv: "present"})
	if len(resolved) != 1 || !resolved[0].Exists || !resolved[0].ManifestValid || !resolved[0].HasKey {
		t.Fatalf("final init preset ref did not resolve with its credential: %+v", resolved)
	}
}

// TestRunProjectCreate_NilDraft proves a nil draft fails closed rather than
// panicking or creating anything.
func TestRunProjectCreate_NilDraft(t *testing.T) {
	opts := testCreateOptions(t, t.TempDir())
	res := RunProjectCreate(nil, opts)
	if res.Committed {
		t.Fatal("expected Committed=false for nil draft")
	}
	if res.Err == nil {
		t.Fatal("expected an error for nil draft")
	}
}

// --- Invariant 5: unfinished staging detection/discard ---------------------

// TestDetectUnfinishedStaging_ReadOnlyAndMarkerGated proves
// DetectUnfinishedStaging never deletes anything and only reports
// directories that carry the ownership marker — a bare directory matching
// the naming pattern but without a marker (e.g. something else entirely)
// must never be offered for discard.
func TestDetectUnfinishedStaging_ReadOnlyAndMarkerGated(t *testing.T) {
	root := t.TempDir()

	// A directory that merely matches the naming convention but has no
	// marker — must be ignored entirely.
	unmarked := filepath.Join(root, ".lingtai.create-unmarked")
	if err := os.MkdirAll(unmarked, 0o755); err != nil {
		t.Fatal(err)
	}

	// A properly marked leftover staging dir. The marker's content must
	// match the directory's own basename exactly (including the leading
	// "."), the same content DiscardUnfinishedStaging/removeOwnedStaging
	// require before deleting — DetectUnfinishedStaging now enforces the
	// identical check so it never offers a directory Discard would refuse.
	marked := filepath.Join(root, ".lingtai.create-abc123")
	if err := os.MkdirAll(marked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(marked, stagingMarkerName), []byte(filepath.Base(marked)+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// A directory whose marker file EXISTS but whose content names a
	// different directory (foreign/stale/corrupt) — must be ignored exactly
	// like the unmarked case, not just "has a marker file present".
	mismatched := filepath.Join(root, ".lingtai.create-mismatch")
	if err := os.MkdirAll(mismatched, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(mismatched, stagingMarkerName), []byte(".lingtai.create-someone-else\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	before := dirSnapshot(t, root)
	found := DetectUnfinishedStaging(root)
	after := dirSnapshot(t, root)
	assertSnapshotsEqual(t, "DetectUnfinishedStaging read-only", before, after)

	if len(found) != 1 {
		t.Fatalf("expected exactly 1 marked staging dir detected, got %v", found)
	}
	if found[0] != marked {
		t.Fatalf("expected %s, got %s", marked, found[0])
	}
}

// TestDiscardUnfinishedStaging_RefusesWithoutMatchingMarker proves the
// discard path is gated by the same ownership proof, not merely
// "directory exists at this path" — deleting a foreign directory that
// happens to share the naming pattern must be refused.
func TestDiscardUnfinishedStaging_RefusesWithoutMatchingMarker(t *testing.T) {
	root := t.TempDir()
	foreign := filepath.Join(root, ".lingtai.create-notmine")
	if err := os.MkdirAll(foreign, 0o755); err != nil {
		t.Fatal(err)
	}
	// Wrong marker content (doesn't match directory's own basename).
	if err := os.WriteFile(filepath.Join(foreign, stagingMarkerName), []byte("some-other-nonce\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := DiscardUnfinishedStaging(foreign)
	if err == nil {
		t.Fatal("expected DiscardUnfinishedStaging to refuse a mismatched marker")
	}
	if _, statErr := os.Stat(foreign); statErr != nil {
		t.Fatalf("directory must survive a refused discard: %v", statErr)
	}
}

// TestDiscardUnfinishedStaging_RemovesOwnedStaging proves a correctly
// marked leftover IS removable via the explicit Discard path.
func TestDiscardUnfinishedStaging_RemovesOwnedStaging(t *testing.T) {
	root := t.TempDir()
	nonce := ".lingtai.create-owned123"
	owned := filepath.Join(root, nonce)
	if err := os.MkdirAll(owned, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(owned, stagingMarkerName), []byte(nonce+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := DiscardUnfinishedStaging(owned); err != nil {
		t.Fatalf("expected discard to succeed: %v", err)
	}
	if _, err := os.Stat(owned); !os.IsNotExist(err) {
		t.Fatalf("expected owned staging dir to be removed, stat err = %v", err)
	}
}

// --- Blocker 2: commit-boundary validation ---------------------------------
//
// A parent review found RunProjectCreate performed NO revalidation of the
// draft/destination before its first mutation (os.MkdirTemp) — only "does
// the final .lingtai already exist" was checked. A crafted or stale draft
// (e.g. AgentDirName="../escape") could reach MkdirTemp/directory creation
// entirely unvalidated. These tests snapshot the project root (and, for
// the traversal case, its PARENT — the directory an escape would actually
// write into) before calling RunProjectCreate on an invalid draft, then
// assert byte-for-byte equality afterward: not just "the specific escape
// path is absent", but the stronger claim the task asked for — nothing
// under the destination or its parent changed AT ALL.

// invalidDraftCase describes one commit-boundary validation scenario: a
// mutator that corrupts an otherwise-valid draft, plus a substring expected
// in validateDraftForCommit's resulting error.
type invalidDraftCase struct {
	name      string
	mutate    func(d *ProjectDraft)
	wantInErr string
}

func invalidDraftCases() []invalidDraftCase {
	return []invalidDraftCase{
		{
			name:      "traversal_agent_dir_name",
			mutate:    func(d *ProjectDraft) { d.AgentDirName = "../escape" },
			wantInErr: "path separator",
		},
		{
			name:      "dot_dot_agent_dir_name",
			mutate:    func(d *ProjectDraft) { d.AgentDirName = ".." },
			wantInErr: `must not be ".."`,
		},
		{
			name:      "dot_agent_dir_name",
			mutate:    func(d *ProjectDraft) { d.AgentDirName = "." },
			wantInErr: `must not be "."`,
		},
		{
			name:      "backslash_agent_dir_name",
			mutate:    func(d *ProjectDraft) { d.AgentDirName = `..\escape` },
			wantInErr: "path separator",
		},
		{
			name:      "blank_agent_name_and_dir",
			mutate:    func(d *ProjectDraft) { d.AgentName = ""; d.AgentDirName = "   " },
			wantInErr: "must not be blank",
		},
		{
			name:      "empty_agent_name_with_valid_dir",
			mutate:    func(d *ProjectDraft) { d.AgentName = ""; d.AgentDirName = "orchestrator" },
			wantInErr: "agent name must not be blank",
		},
		{
			name:      "relative_project_root",
			mutate:    func(d *ProjectDraft) { d.ProjectRoot = "relative/path" },
			wantInErr: "absolute path",
		},
		{
			name:      "selected_preset_missing",
			mutate:    func(d *ProjectDraft) { d.DraftPreset = nil },
			wantInErr: "selected preset is required",
		},
		{
			name:      "preset_missing_name",
			mutate:    func(d *ProjectDraft) { d.DraftPreset.Name = "" },
			wantInErr: "no name",
		},
		{
			name: "preset_fails_validate",
			mutate: func(d *ProjectDraft) {
				d.DraftPreset.Description = preset.PresetDescription{}
			},
			wantInErr: "invalid",
		},
		{
			name:      "custom_recipe_without_dir",
			mutate:    func(d *ProjectDraft) { d.RecipeName = preset.RecipeCustom; d.RecipeCustomDir = "" },
			wantInErr: "custom recipe",
		},
	}
}

// TestValidateDraftForCommit_RejectsInvalidDrafts drives validateDraftForCommit
// directly (the pure function itself) across every invalid-draft case and
// confirms each is rejected with a relevant error message. This is the
// direct unit-level proof; TestRunProjectCreate_InvalidDraftNeverWrites
// below proves the same cases never reach a filesystem write when driven
// through the full RunProjectCreate entry point.
func TestValidateDraftForCommit_RejectsInvalidDrafts(t *testing.T) {
	for _, tc := range invalidDraftCases() {
		t.Run(tc.name, func(t *testing.T) {
			draft, _ := newTestDraft(t)
			tc.mutate(draft)
			err := validateDraftForCommit(draft, draft.ProjectRoot)
			if err == nil {
				t.Fatalf("expected validateDraftForCommit to reject this draft, got nil")
			}
			if !strings.Contains(err.Error(), tc.wantInErr) {
				t.Fatalf("expected error containing %q, got %q", tc.wantInErr, err.Error())
			}
		})
	}
}

// TestRunProjectCreate_InvalidDraftNeverWrites proves the stronger,
// end-to-end claim: for every invalid-draft case, RunProjectCreate itself
// (a) fails at PhaseValidateDraft, before os.MkdirTemp ever runs, and (b)
// leaves BOTH the project root and its parent directory byte-for-byte
// unchanged — the parent is what a "../escape" traversal would actually
// write into, so snapshotting only the project root itself would miss
// exactly the defect this validation exists to close.
func TestRunProjectCreate_InvalidDraftNeverWrites(t *testing.T) {
	for _, tc := range invalidDraftCases() {
		t.Run(tc.name, func(t *testing.T) {
			draft, root := newTestDraft(t)
			tc.mutate(draft)
			opts := testCreateOptions(t, root)

			// Snapshot the PARENT of root (not just root itself) — an
			// "../escape" AgentDirName would resolve one level up from
			// whatever staging directory got created inside root, so the
			// parent is the actual blast radius a traversal would hit.
			parent := filepath.Dir(root)
			parentBefore := dirSnapshot(t, parent)

			res := RunProjectCreate(draft, opts)

			if res.Committed {
				t.Fatalf("expected Committed=false for invalid draft %q", tc.name)
			}
			if res.Err == nil {
				t.Fatalf("expected an error for invalid draft %q", tc.name)
			}
			if res.FailedPhase != PhaseValidateDraft {
				t.Fatalf("expected failure at PhaseValidateDraft for %q, got %v", tc.name, res.FailedPhase)
			}

			parentAfter := dirSnapshot(t, parent)
			assertSnapshotsEqual(t, "parent dir after invalid draft "+tc.name, parentBefore, parentAfter)

			finalDir := filepath.Join(root, ".lingtai")
			if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
				t.Fatalf("invalid draft %q: final .lingtai/ must not exist, stat err = %v", tc.name, err)
			}
		})
	}
}

// TestRunProjectCreate_ProjectRootMustMatchApprovedDestination proves a
// crafted/stale confirmation cannot redirect creation from the root selected
// by the launcher to a different, otherwise-valid absolute directory.
func TestRunProjectCreate_ProjectRootMustMatchApprovedDestination(t *testing.T) {
	draft, draftRoot := newTestDraft(t)
	approvedRoot := t.TempDir()
	opts := testCreateOptions(t, approvedRoot)

	draftBefore := dirSnapshot(t, draftRoot)
	approvedBefore := dirSnapshot(t, approvedRoot)

	res := RunProjectCreate(draft, opts)

	if res.Committed {
		t.Fatal("expected Committed=false for mismatched project root")
	}
	if res.Err == nil || !strings.Contains(res.Err.Error(), "does not match approved destination") {
		t.Fatalf("expected approved-destination mismatch error, got %v", res.Err)
	}
	if res.FailedPhase != PhaseValidateDraft {
		t.Fatalf("expected PhaseValidateDraft, got %v", res.FailedPhase)
	}
	assertSnapshotsEqual(t, "draft root after destination mismatch", draftBefore, dirSnapshot(t, draftRoot))
	assertSnapshotsEqual(t, "approved root after destination mismatch", approvedBefore, dirSnapshot(t, approvedRoot))
}

// TestRunProjectCreate_NilDraftNeverWrites is the pre-existing nil-draft
// guard (TestRunProjectCreate_NilDraft above), extended to additionally
// prove no write occurs — nil is the most trivially "invalid" draft of all,
// and it's worth confirming the same "byte-for-byte parent" standard the
// other invalid-draft cases are held to.
func TestRunProjectCreate_NilDraftNeverWrites(t *testing.T) {
	root := t.TempDir()
	before := dirSnapshot(t, root)
	opts := testCreateOptions(t, root)

	res := RunProjectCreate(nil, opts)

	if res.Committed {
		t.Fatal("expected Committed=false for nil draft")
	}
	if res.Err == nil {
		t.Fatal("expected an error for nil draft")
	}
	after := dirSnapshot(t, root)
	assertSnapshotsEqual(t, "root dir after nil draft", before, after)
}

// --- Blocker 3: phantom-project recheck -------------------------------------
//
// The former main (non-launcher) startup path had a phantom-process check
// before creating .lingtai/; a parent review found the new strict
// no-project gate bypassed it entirely. These tests drive RunProjectCreate
// with an injected opts.ScanInventory fake — never touching the real OS
// process table — to prove the three required outcomes: phantom result
// fails closed before staging, a scan error fails closed before staging,
// and a clean scan proceeds to the existing staged/committed path.

// TestRunProjectCreate_PhantomScanRejectsBeforeStaging proves that when the
// injected scan reports records AND phantom dirs for this project root
// (mirroring listMain's own "phantom only when records exist" semantics —
// see runPhantomRecheck's doc comment), RunProjectCreate fails at
// PhaseValidateDraft, before any staging write, and the root is left
// byte-for-byte unchanged.
func TestRunProjectCreate_PhantomScanRejectsBeforeStaging(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	opts.ScanInventory = func(projectRoot string) (inventory.Snapshot, error) {
		return inventory.Snapshot{
			Records:     []inventory.Record{{PID: 12345, Project: projectRoot}},
			PhantomDirs: []string{projectRoot},
		}, nil
	}

	before := dirSnapshot(t, root)
	res := RunProjectCreate(draft, opts)

	if res.Committed {
		t.Fatal("expected Committed=false on phantom scan result")
	}
	if res.Err == nil {
		t.Fatal("expected an error on phantom scan result")
	}
	if res.FailedPhase != PhaseValidateDraft {
		t.Fatalf("expected failure at PhaseValidateDraft, got %v", res.FailedPhase)
	}
	if !strings.Contains(res.Err.Error(), "phantom") {
		t.Fatalf("expected error to mention phantom state, got %q", res.Err.Error())
	}
	after := dirSnapshot(t, root)
	assertSnapshotsEqual(t, "root dir after phantom scan result", before, after)
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist after phantom rejection, stat err = %v", err)
	}
}

// TestRunProjectCreate_ScanErrorRejectsBeforeStaging proves a scan failure
// itself (not just a phantom RESULT) fails closed — the legacy shelled-out
// `self list` check silently ignored exec errors (fail-open); this must
// not repeat that mistake.
func TestRunProjectCreate_ScanErrorRejectsBeforeStaging(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	scanErr := errors.New("boom: ps unavailable")
	opts.ScanInventory = func(projectRoot string) (inventory.Snapshot, error) {
		return inventory.Snapshot{}, scanErr
	}

	before := dirSnapshot(t, root)
	res := RunProjectCreate(draft, opts)

	if res.Committed {
		t.Fatal("expected Committed=false on scan error")
	}
	if res.Err == nil {
		t.Fatal("expected an error on scan error")
	}
	if res.FailedPhase != PhaseValidateDraft {
		t.Fatalf("expected failure at PhaseValidateDraft, got %v", res.FailedPhase)
	}
	if !errors.Is(res.Err, scanErr) {
		t.Fatalf("expected res.Err to wrap the scan error, got %v", res.Err)
	}
	after := dirSnapshot(t, root)
	assertSnapshotsEqual(t, "root dir after scan error", before, after)
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(finalDir); !os.IsNotExist(err) {
		t.Fatalf("final .lingtai/ must not exist after scan-error rejection, stat err = %v", err)
	}
}

// TestRunProjectCreate_CleanScanProceeds proves the third required case: a
// clean scan (no records, or records with no phantom dirs) does NOT block
// creation — the recheck must be a real gate, not an unconditional refusal.
func TestRunProjectCreate_CleanScanProceeds(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	scanCalled := false
	opts.ScanInventory = func(projectRoot string) (inventory.Snapshot, error) {
		scanCalled = true
		if projectRoot != root {
			t.Fatalf("expected scan to be called with project root %q, got %q", root, projectRoot)
		}
		return inventory.Snapshot{}, nil // no records, no phantoms — clean
	}

	res := RunProjectCreate(draft, opts)

	if !scanCalled {
		t.Fatal("expected ScanInventory to be invoked")
	}
	if res.Err != nil {
		t.Fatalf("unexpected error on clean scan: %v (phase %v)", res.Err, res.FailedPhase)
	}
	if !res.Committed {
		t.Fatal("expected Committed=true on clean scan")
	}
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(finalDir); err != nil {
		t.Fatalf("expected final .lingtai/ to exist after clean scan: %v", err)
	}
}

// TestRunProjectCreate_PhantomDirsWithNoRecordsProceeds proves the specific
// false-positive this recheck must NOT reproduce:
// inventory.Snapshot.PhantomDirs is populated purely because <root>/.lingtai
// doesn't exist yet (detectPhantomDirs's FilterDir branch — see
// runPhantomRecheck's doc comment), which is true for literally every
// never-yet-created project. Without the "only when Records is non-empty"
// gate this would reject creating a project in ANY empty directory.
func TestRunProjectCreate_PhantomDirsWithNoRecordsProceeds(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	opts.ScanInventory = func(projectRoot string) (inventory.Snapshot, error) {
		return inventory.Snapshot{
			Records:     nil, // no matching processes at all
			PhantomDirs: []string{projectRoot},
		}, nil
	}

	res := RunProjectCreate(draft, opts)

	if res.Err != nil {
		t.Fatalf("unexpected error when PhantomDirs is set but Records is empty: %v (phase %v)", res.Err, res.FailedPhase)
	}
	if !res.Committed {
		t.Fatal("expected Committed=true — PhantomDirs alone, without matching Records, must not block creation")
	}
	finalDir := filepath.Join(root, ".lingtai")
	if _, err := os.Stat(finalDir); err != nil {
		t.Fatalf("expected final .lingtai/ to exist: %v", err)
	}
}
