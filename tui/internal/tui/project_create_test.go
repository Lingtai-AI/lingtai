package tui

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// minimalDraftPreset returns a preset with just enough manifest shape to
// pass through preset.GenerateInitJSONWithOpts without a real LLM API key,
// and a non-empty Description.Summary so it also passes Preset.Validate()
// (required by validateDraftForCommit's commit-boundary check — see
// project_create.go). It is an openai-family preset with no base_url (the
// official endpoint) and no api_key_env, so the api_key_env stamping these
// tests assert on is OPENAI_1_API_KEY.
func minimalDraftPreset() preset.Preset {
	return preset.Preset{
		Name:        "test-preset",
		Description: preset.PresetDescription{Summary: "test fixture preset"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "openai",
				"model":    "test-model",
			},
		},
	}
}

// newTestDraft builds a reviewed public preset with no Recipe selection.
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
		LingtaiCmd:          "fixture-python",
		ScanInventory:       func(root string) (inventory.Snapshot, error) { return inventory.Snapshot{FilterDir: root}, nil },
		ExpectedProjectRoot: expectedProjectRoot,
		// Production still checks runtime readiness (read-only) when
		// LingtaiCmd was empty at launcher entry. Tests inject a no-op so
		// they stay hermetic; the empty command then records a post-commit
		// warning instead of launching.
		RuntimeReady:      func(string) error { return nil },
		ResolveLingtaiCmd: func(string) string { return "" },
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
	// "."), the same content legacy DiscardUnfinishedStaging
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

// These are consumer fixtures, not replacements for the kernel's staged-reader
// tests. All domain creation and exclusive publication acceptance runs in Python.
func TestProjectConsumerFailedOrUnknownDoesNotRegister(t *testing.T) {
	for _, known := range []bool{false, true} {
		t.Run(map[bool]string{false: "unknown", true: "refused"}[known], func(t *testing.T) {
			draft, root := newTestDraft(t)
			opts := testCreateOptions(t, root)
			draft.DraftAPIKey = secretString("fixture-key-never-in-request")
			draft.DraftAPIKeyEnv = "FIXTURE_API_KEY"
			before := dirSnapshot(t, opts.GlobalDir)
			calls := 0
			opts.CreateInteractive = func(_ context.Context, _ string, req process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
				calls++
				raw, _ := json.Marshal(req)
				if strings.Contains(string(raw), draft.DraftAPIKey.Reveal()) {
					t.Fatal("credential in projection")
				}
				return process.InteractiveProjectResult{}, &process.ProjectOperationError{Known: known, Code: "init_preflight_failed"}
			}
			res := RunProjectCreate(draft, opts)
			if res.Committed || res.Err == nil || res.Unknown == known || calls != 1 {
				t.Fatalf("wrong outcome: %+v calls=%d", res, calls)
			}
			assertSnapshotsEqual(t, "no readiness means no dependencies/UI registration", before, dirSnapshot(t, opts.GlobalDir))
			entries, _ := os.ReadDir(root)
			if len(entries) != 0 {
				t.Fatal("Go staged/created a project on failure")
			}
		})
	}
}
func TestProjectConsumerPrerequisitesOnlyAfterReadyAndRetainedOnFailure(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.DraftAPIKey = secretString("fixture-key")
	draft.DraftAPIKeyEnv = "FIXTURE_API_KEY"
	phaseSeen := false
	opts.InjectFailure = func(phase CreatePhase) error {
		if phase != PhasePersistDependencies {
			t.Fatal("Go retained a domain phase")
		}
		phaseSeen = true
		return nil
	}
	opts.CreateInteractive = func(_ context.Context, _ string, req process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
		cfg, _ := config.LoadConfigReadOnly(opts.GlobalDir)
		if cfg.Keys["FIXTURE_API_KEY"] != "" || phaseSeen {
			t.Fatal("credentials persisted before readiness")
		}
		if !ready() || !phaseSeen {
			t.Fatal("prerequisite persistence unavailable")
		}
		cfg, _ = config.LoadConfigReadOnly(opts.GlobalDir)
		if cfg.Keys["FIXTURE_API_KEY"] != draft.DraftAPIKey.Reveal() {
			t.Fatal("ack before verified credential")
		}
		return process.InteractiveProjectResult{}, &process.ProjectOperationError{Known: true, Code: "project_publication_failed"}
	}
	res := RunProjectCreate(draft, opts)
	if res.Committed || res.Unknown || res.Err == nil {
		t.Fatalf("wrong refusal: %+v", res)
	}
	cfg, _ := config.LoadConfigReadOnly(opts.GlobalDir)
	if cfg.Keys["FIXTURE_API_KEY"] != "fixture-key" {
		t.Fatal("rolled back external prerequisite")
	}
	if _, err := os.Stat(filepath.Join(root, ".lingtai")); !os.IsNotExist(err) {
		t.Fatal("Go created final tree")
	}
}
func TestProjectConsumerProjectionPreservesReaderFieldsAndRefs(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.DraftPreset.Manifest["llm"].(map[string]interface{})["context_limit"] = json.Number("9007199254740993")
	draft.DraftPreset.Manifest["extension"] = map[string]interface{}{"exact": json.Number("9007199254740993"), "unknown": "preserve"}
	draft.AgentOpts.AllowedPresets = []string{"~/saved/other.json", "~/saved/other.json"}
	draft.AgentOpts.Addons = []string{"imap", "unknown-addon"}
	draft.RecipeName = "adaptive"
	before, _ := json.Marshal(draft.DraftPreset.Manifest)
	opts.CreateInteractive = func(_ context.Context, _ string, req process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
		raw, _ := json.Marshal(req)
		if strings.Count(string(raw), "9007199254740993") != 2 || !strings.Contains(string(raw), "unknown") || req.Recipe != "adaptive" || req.AgentName != draft.AgentName || req.PresetRef != preset.RefFor(*draft.DraftPreset) {
			t.Fatalf("projection lost reviewed data: %s", raw)
		}
		if len(req.Options.AllowedPresets) != 2 || req.Options.AllowedPresets[0] != draft.AgentOpts.AllowedPresets[0] || len(req.Options.Addons) != 2 || req.Options.MCP["imap"] == nil || req.Options.MCP["unknown-addon"] != nil {
			t.Fatal("caller refs/addon semantics changed")
		}
		for _, retired := range []string{"context_limit", "max_rpm", "max_aed_attempts", "comment_file", "covenant_file"} {
			var wire map[string]interface{}
			_ = json.Unmarshal(raw, &wire)
			options := wire["options"].(map[string]interface{})
			if _, found := options[retired]; found {
				t.Fatalf("retired knob %s", retired)
			}
		}
		return process.InteractiveProjectResult{}, &process.ProjectOperationError{Known: true, Code: "fixture_refusal"}
	}
	_ = RunProjectCreate(draft, opts)
	after, _ := json.Marshal(draft.DraftPreset.Manifest)
	if string(before) != string(after) {
		t.Fatal("projection mutated Review")
	}
}
func TestProjectConsumerProcessEvidenceIsFreshAndFailClosed(t *testing.T) {
	for _, evidence := range []string{"clean", "blocked", "unknown"} {
		t.Run(evidence, func(t *testing.T) {
			draft, root := newTestDraft(t)
			opts := testCreateOptions(t, root)
			scans := 0
			opts.ScanInventory = func(path string) (inventory.Snapshot, error) {
				scans++
				if evidence == "unknown" {
					return inventory.Snapshot{}, errors.New("incomplete scan")
				}
				snap := inventory.Snapshot{FilterDir: path, PhantomDirs: []string{path}}
				if evidence == "blocked" {
					snap.Records = []inventory.Record{{Project: path, AgentDir: filepath.Join(path, ".lingtai", "missing")}}
				}
				return snap, nil
			}
			opts.CreateInteractive = func(_ context.Context, _ string, req process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
				if scans != 1 || req.ProcessEvidence != evidence {
					t.Fatalf("evidence=%s scans=%d", req.ProcessEvidence, scans)
				}
				return process.InteractiveProjectResult{}, &process.ProjectOperationError{Known: true, Code: "fixture_refusal"}
			}
			_ = RunProjectCreate(draft, opts)
		})
	}
}
func TestProjectConsumerKnownCommitKeepsUIFailuresAsWarnings(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	opts.CreateInteractive = func(_ context.Context, _ string, req process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
		if !ready() {
			t.Fatal("prerequisites failed")
		}
		committed := true
		return process.InteractiveProjectResult{Status: "created", Committed: &committed, AgentName: req.AgentName, DirectoryName: req.DirectoryName, PresetRef: req.PresetRef, AttemptID: "fixture", Warnings: []string{"kernel fixture warning"}}, nil
	}
	opts.InjectFailure = func(phase CreatePhase) error {
		if phase == PhasePostCommitConfig || phase == PhasePostCommitRegister || phase == PhasePostCommitLaunch {
			return errors.New("UI-only failure")
		}
		return nil
	}
	res := RunProjectCreate(draft, opts)
	if !res.Committed || res.Err != nil || len(res.PostCommitWarnings) != 4 {
		t.Fatalf("postcommit rolled back: %+v", res)
	}
}
func TestProjectConsumerResolvedApprovedRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink requires developer mode")
	}
	draft, root := newTestDraft(t)
	alias := filepath.Join(t.TempDir(), "alias")
	if err := os.Symlink(root, alias); err != nil {
		t.Fatal(err)
	}
	if err := validateDraftForCommit(draft, alias); err != nil {
		t.Fatalf("resolved root mismatch: %v", err)
	}
	if err := validateDraftForCommit(draft, t.TempDir()); err == nil {
		t.Fatal("unapproved destination accepted")
	}
}
func TestProjectConsumerRejectedDependenciesDoNotWriteCredentials(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.DraftPreset.Name = "stale-selection"
	draft.DraftAPIKey = secretString("fixture-key")
	draft.DraftAPIKeyEnv = "FIXTURE_API_KEY"
	before := dirSnapshot(t, opts.GlobalDir)
	opts.CreateInteractive = func(_ context.Context, _ string, _ process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
		if ready() {
			t.Fatal("accepted unresolved selected preset")
		}
		return process.InteractiveProjectResult{}, &process.ProjectOperationError{Known: true, Code: "prerequisites_rejected"}
	}
	res := RunProjectCreate(draft, opts)
	if res.Err == nil || res.Committed {
		t.Fatal("prerequisite refusal accepted")
	}
	assertSnapshotsEqual(t, "preset must verify before credentials", before, dirSnapshot(t, opts.GlobalDir))
}
func TestLauncherUnknownAttemptCannotConfirmAgain(t *testing.T) {
	model := LauncherRootModel{view: launcherViewCreate, createResult: &CreateResult{Unknown: true}, createErr: "inspect before another attempt"}
	updated, command := model.Update(ProjectDraftConfirmedMsg{Draft: NewProjectDraft(t.TempDir())})
	next := updated.(LauncherRootModel)
	if command != nil || next.createResult == nil || !next.createResult.Unknown || next.done {
		t.Fatal("unknown creation was replayed")
	}
}
func TestProjectConsumerDirtyPresetIsBoundAndSavedOnlyAfterReady(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.DraftPresetDirty = true
	draft.DraftPreset.Name = "reviewed-custom"
	draft.DraftPreset.Source = preset.SourceTemplate
	draft.DraftPreset.Manifest["extension"] = json.Number("9007199254740993")
	saved := filepath.Join(opts.GlobalDir, "presets", "saved", "reviewed-custom.json")
	opts.CreateInteractive = func(_ context.Context, _ string, req process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
		if req.PresetRef != "~/.lingtai-tui/presets/saved/reviewed-custom.json" {
			t.Fatalf("dirty reference not bound to save: %s", req.PresetRef)
		}
		if _, err := os.Stat(saved); !os.IsNotExist(err) {
			t.Fatal("dirty preset written before readiness")
		}
		if !ready() {
			t.Fatal("dirty preset prerequisite rejected")
		}
		data, err := os.ReadFile(saved)
		if err != nil || !strings.Contains(string(data), "9007199254740993") {
			t.Fatal("exact dirty snapshot not persisted")
		}
		return process.InteractiveProjectResult{}, &process.ProjectOperationError{Known: true, Code: "project_publication_failed"}
	}
	res := RunProjectCreate(draft, opts)
	if res.Committed || res.Err == nil || res.Unknown {
		t.Fatalf("publication refusal changed: %+v", res)
	}
	if _, err := os.Stat(saved); err != nil {
		t.Fatal("valid external preset rolled back")
	}
	if draft.DraftPreset.Source != preset.SourceTemplate {
		t.Fatal("normalized save changed Review source")
	}
}
func TestProjectConsumerInvalidPendingCodexAckIsSecretSafe(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.DraftCodexTokens = secretBytes([]byte("invalid-secret-fixture"))
	opts.CreateInteractive = func(_ context.Context, _ string, req process.InteractiveProjectRequest, ready func() bool) (process.InteractiveProjectResult, error) {
		data, _ := json.Marshal(req)
		if strings.Contains(string(data), "invalid-secret-fixture") {
			t.Fatal("Codex credential in stdin")
		}
		if ready() {
			t.Fatal("invalid pending token accepted")
		}
		return process.InteractiveProjectResult{}, &process.ProjectOperationError{Known: true, Code: "prerequisites_rejected"}
	}
	res := RunProjectCreate(draft, opts)
	if res.Err == nil || strings.Contains(res.Err.Error(), "invalid-secret-fixture") || res.Committed {
		t.Fatal("token failure leaked or published")
	}
	if _, err := os.Stat(legacyCodexAuthPath(opts.GlobalDir)); !os.IsNotExist(err) {
		t.Fatal("invalid token persisted")
	}
}
