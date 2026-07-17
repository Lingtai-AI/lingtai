package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// stagingMarkerName is the file written inside a staging directory before
// any build step runs. Its presence — and its content matching the
// finalizer's own nonce — is what lets cleanup prove a given
// ".lingtai.create-*" directory was created by THIS attempt before deleting
// it. Never delete a staging directory without first confirming this
// marker: an unrelated/foreign directory that merely matches the naming
// pattern must never be blind-deleted (see design doc Invariant 5).
const stagingMarkerName = ".lingtai-launcher-staging"

// CreatePhase identifies a mutating step of the staging finalizer, used both
// for user-facing phase reporting and for failure-injection in tests.
type CreatePhase int

const (
	PhaseValidateDraft CreatePhase = iota
	PhaseCreateStaging
	PhaseInitProject
	PhaseApplyPreset
	PhaseApplyRecipe
	PhaseValidate
	PhaseRename
	PhasePostCommitConfig
	PhasePostCommitRegister
	PhasePostCommitLaunch
)

func (p CreatePhase) String() string {
	switch p {
	case PhaseValidateDraft:
		return "validate_draft"
	case PhaseCreateStaging:
		return "create_staging"
	case PhaseInitProject:
		return "init_project"
	case PhaseApplyPreset:
		return "apply_preset"
	case PhaseApplyRecipe:
		return "apply_recipe"
	case PhaseValidate:
		return "validate"
	case PhaseRename:
		return "rename"
	case PhasePostCommitConfig:
		return "post_commit_config"
	case PhasePostCommitRegister:
		return "post_commit_register"
	case PhasePostCommitLaunch:
		return "post_commit_launch"
	default:
		return "unknown"
	}
}

// preRenamePhase reports whether a phase runs before the atomic rename —
// i.e. whether its failure must leave NO final .lingtai (pre-commit) versus
// leaving a valid, already-published project that is merely unfinished
// (post-commit). See the design doc's commit-matrix / failure-semantics
// table.
func (p CreatePhase) preRenamePhase() bool {
	return p <= PhaseRename
}

// CreateResult reports the outcome of RunProjectCreate.
type CreateResult struct {
	// Committed is true once os.Rename(staging, final) has succeeded. A
	// true value means the final .lingtai/ exists and must never be rolled
	// back, even if a later post-commit phase failed.
	Committed bool
	// FailedPhase is the phase that failed, valid only when Err != nil.
	FailedPhase CreatePhase
	Err         error

	OrchDir  string // full path to the orchestrator directory, once known
	OrchName string
	// PostCommitWarnings collects non-fatal issues from phases that run
	// after Committed=true — these never cause rollback, only surface as
	// "created, but ..." guidance.
	PostCommitWarnings []string
}

// createFailureInjector lets tests force a specific phase to fail without
// touching production code paths. Nil (the default) means no injection.
// Tests set this via WithFailureInjector.
type createFailureInjector func(phase CreatePhase) error

// CreateOptions carries the pieces RunProjectCreate needs beyond the draft
// itself — everything that in a normal (non-launcher) startup would come
// from main.go's existing pipeline. LingtaiCmd may be empty before commit;
// the post-commit phase checks runtime readiness (never installs/repairs —
// see RuntimeReady), then resolves the command again before deciding whether
// an agent can be launched.
type CreateOptions struct {
	GlobalDir  string
	LingtaiCmd string
	// RuntimeReady/ResolveLingtaiCmd replace their config counterparts in
	// tests. Nil uses the real post-publish readiness check and resolver.
	// RuntimeReady is READ-ONLY: it must never create, repair, or upgrade
	// the runtime — install.sh installs the kernel by default, and the only
	// automatic install/repair opportunity is the interactive TUI's shared
	// startup preflight, gated on explicit human consent. A not-ready
	// runtime here (e.g. the human declined that prompt, or this is a
	// from-scratch machine) surfaces as a post-commit warning, never a
	// silent install.
	RuntimeReady      func(globalDir string) error
	ResolveLingtaiCmd func(globalDir string) string
	// ExpectedProjectRoot is the launcher-selected destination that the
	// commit-boundary validator must match. It is intentionally independent
	// of draft.ProjectRoot so a crafted/stale confirmation message cannot
	// redirect creation to another existing absolute directory.
	ExpectedProjectRoot string
	// InjectFailure, when non-nil, is consulted before each phase's real
	// work runs; returning a non-nil error simulates that phase failing.
	// Test-only seam.
	InjectFailure createFailureInjector
	// ScanInventory, when non-nil, replaces the real inventory.Scan call
	// RunProjectCreate makes to recheck for a phantom project immediately
	// before staging (see runPhantomRecheck). Nil (the default/production
	// case) uses the real scan against the OS process table. Tests inject
	// a fake here instead of needing real background processes to exercise
	// the phantom/scan-error paths.
	ScanInventory func(projectRoot string) (inventory.Snapshot, error)
}

// validateDraftForCommit revalidates a *ProjectDraft and its destination
// PURELY (no filesystem writes — Lstat/Stat reads only) at the commit
// boundary, immediately before RunProjectCreate performs its first mutation
// (os.MkdirTemp). This exists because a crafted or stale draft — e.g. one
// assembled directly in a test, or one whose AgentDirName somehow contains
// "../escape" — must never reach MkdirTemp/directory-creation at all. A
// parent review found NO such revalidation existed: RunProjectCreate's only
// prior check was "does the final .lingtai already exist", which says
// nothing about the draft's own field values.
//
// Every check here is read-only and must run BEFORE any staging write.
// Returns the first violation found (fail-fast, not an accumulated list —
// callers only need to refuse and report, not enumerate every problem).
func validateDraftForCommit(draft *ProjectDraft, expectedProjectRoot string) error {
	if draft == nil {
		return fmt.Errorf("no project draft")
	}
	if expectedProjectRoot == "" {
		return fmt.Errorf("approved project directory is required")
	}
	if !filepath.IsAbs(expectedProjectRoot) {
		return fmt.Errorf("approved project directory must be an absolute path, got %q", expectedProjectRoot)
	}
	if draft.ProjectRoot == "" {
		return fmt.Errorf("project directory is required")
	}
	if !filepath.IsAbs(draft.ProjectRoot) {
		return fmt.Errorf("project directory must be an absolute path, got %q", draft.ProjectRoot)
	}
	if filepath.Clean(draft.ProjectRoot) != filepath.Clean(expectedProjectRoot) {
		return fmt.Errorf("project directory %q does not match approved destination %q", draft.ProjectRoot, expectedProjectRoot)
	}
	if info, err := os.Stat(draft.ProjectRoot); err != nil {
		return fmt.Errorf("project directory %s: %w", draft.ProjectRoot, err)
	} else if !info.IsDir() {
		return fmt.Errorf("project directory %s is not a directory", draft.ProjectRoot)
	}
	finalDir := filepath.Join(draft.ProjectRoot, ".lingtai")
	if _, err := os.Lstat(finalDir); err == nil {
		return fmt.Errorf("%s already exists (concurrent creation?)", finalDir)
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("checking %s: %w", finalDir, err)
	}

	agentName := draft.AgentName
	if strings.TrimSpace(agentName) == "" {
		return fmt.Errorf("agent name must not be blank")
	}
	dirName := draft.AgentDirName
	if dirName == "" {
		dirName = agentName
	}
	if err := validateSafeRelativeDirName(dirName); err != nil {
		return fmt.Errorf("agent directory name %q: %w", dirName, err)
	}

	// Preset coherence: Review must have captured one explicit selected
	// preset in the draft. Never fall back to whichever global preset happens
	// to be first on disk at commit time — that could silently finalize a
	// different preset than the one the user reviewed.
	if draft.DraftPreset == nil {
		return fmt.Errorf("selected preset is required")
	}
	if draft.DraftPreset.Name == "" {
		return fmt.Errorf("selected preset has no name")
	}
	if errs := draft.DraftPreset.Validate(); len(errs) > 0 {
		return fmt.Errorf("selected preset %q is invalid: %w", draft.DraftPreset.Name, errs[0])
	}

	if draft.RecipeName == preset.RecipeCustom && strings.TrimSpace(draft.RecipeCustomDir) == "" {
		return fmt.Errorf("custom recipe selected but no custom recipe directory given")
	}

	return nil
}

// validateSafeRelativeDirName rejects any directory-name value that isn't a
// single, non-empty path segment safe to filepath.Join beneath a staging
// directory: no path separators (forward or backslash, so this rejects
// escapes on both POSIX and Windows layouts regardless of build OS), and
// neither "." nor ".." (which would resolve to the staging dir itself or its
// parent — an escape one level up is exactly the "../escape" attack this
// check exists to close).
func validateSafeRelativeDirName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("must not be blank")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("must not be %q", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("must not contain a path separator")
	}
	return nil
}

// runPhantomRecheck performs a typed, read-only inventory scan of
// draft.ProjectRoot immediately before staging and fails closed on either a
// phantom result (a LEFTOVER PROCESS references an agent dir under this
// project even though .lingtai doesn't exist yet — almost always a remnant
// of an interrupted prior attempt or a stale registration) or a scan error
// itself. The former main (non-launcher) startup path had exactly this
// phantom check via a shelled-out `self list` subprocess and "[PHANTOM]"
// string match; the new strict launcher path bypassed it entirely until
// this fix. This reimplements the same semantics using the typed
// inventory.Scan/Options seam the `list` command itself is built on (see
// list_unix.go), not the subprocess+string-match approach — and
// additionally fails closed on a scan error (the legacy code silently
// ignored `exec.Command(...).Output()`'s error return, which would fail
// OPEN on a broken `list` subprocess).
//
// IMPORTANT: inventory.Snapshot.PhantomDirs is NOT by itself "there is a
// leftover process" — detectPhantomDirs's FilterDir branch reports
// filterDir as phantom purely because <filterDir>/.lingtai doesn't exist,
// with NO regard for whether any process actually matched the filter. That
// is trivially true for every never-yet-created project (including the
// entire reason this launcher exists), so using PhantomDirs directly here
// would reject creating a project in ANY empty directory. list_unix.go's
// listMain already knows this and gates on it: it zeroes snap.PhantomDirs
// whenever len(snap.Records) == 0 before ever consulting it. This function
// applies that exact same gate — phantom means "there are matching records
// for this project AND no .lingtai exists", never "no .lingtai exists" on
// its own.
//
// opts.ScanInventory lets tests substitute a fake scanner; production always
// leaves it nil and gets the real inventory.Scan against the OS process
// table.
func runPhantomRecheck(draft *ProjectDraft, opts CreateOptions) error {
	scan := opts.ScanInventory
	if scan == nil {
		scan = func(projectRoot string) (inventory.Snapshot, error) {
			return inventory.Scan(inventory.Options{FilterDir: projectRoot, SelfPID: os.Getpid()})
		}
	}
	snap, err := scan(draft.ProjectRoot)
	if err != nil {
		return fmt.Errorf("phantom-project recheck failed: %w", err)
	}
	if len(snap.Records) > 0 && len(snap.PhantomDirs) > 0 {
		return fmt.Errorf("phantom project state detected at %s — run `lingtai-tui list` / `lingtai-tui purge` before creating a project here", draft.ProjectRoot)
	}
	return nil
}

// RunProjectCreate is the SOLE product-visible project commit point (design
// doc Invariant 4). It stages a new project in a sibling directory, builds
// and validates it there, then publishes it with a single os.Rename. No
// step before the rename touches draft.ProjectRoot itself except the final
// Lstat-and-rename; no step after the rename is allowed to delete or
// rewrite the already-published .lingtai/.
//
// Sequence:
//  0. Validate: validateDraftForCommit (pure) revalidates the draft and
//     destination — traversal-safe agent dir name, non-blank required
//     fields, coherent preset, recipe invariants — then runPhantomRecheck
//     (read-only inventory scan) fails closed on phantom process state or a
//     scan error. Both run BEFORE any write; see design doc's commit-
//     boundary validation requirement.
//  1. Revalidate: Lstat draft.ProjectRoot/.lingtai must still be absent —
//     rejects concurrent creation and symlinks (Lstat, not Stat: a symlink
//     counts as "exists", never followed/created through).
//  2. Create staging: os.MkdirTemp(draft.ProjectRoot, ".lingtai.create-*")
//     — guaranteed sibling, same filesystem as the final path, so the
//     eventual rename is atomic. A marker file naming this attempt is
//     written FIRST, before any build step, so cleanup can always prove
//     ownership before deleting.
//  3. Build: process.InitProject on the staging dir (called as-is, not
//     forked), then resolveDraftPreset + preset.GenerateInitJSONWithOpts +
//     the recipe apply chain, all against the staging dir. The dirty draft
//     preset is resolved and used HERE (in memory only) but NOT yet saved
//     to disk — preset.Save is deferred to step 6, post-rename (see
//     ProjectDraft.DraftPreset's doc comment and PhaseApplyPreset's own
//     comment below for why).
//  4. Validate: exactly one orchestrator, using the same DetectOrchestrators/
//     IsOrchestrator helpers main.go's own invariant checks use.
//  5. Publish: os.Rename(staging, final). This is the ONLY line in this
//     file that may cause draft.ProjectRoot/.lingtai to start existing.
//  6. Post-commit (best-effort, never rolls back): save deferred
//     config/credentials/preset (preset.Save runs for the FIRST time here,
//     not in step 3), config.Register, PopulateBundledLibrary, attempt
//     agent launch. Failures here are reported as warnings, not as
//     CreateResult.Err — the project is already valid and retryable.
func RunProjectCreate(draft *ProjectDraft, opts CreateOptions) CreateResult {
	result := CreateResult{}
	if draft == nil {
		result.Err = fmt.Errorf("no project draft")
		result.FailedPhase = PhaseCreateStaging
		return result
	}
	root := draft.ProjectRoot
	finalDir := filepath.Join(root, ".lingtai")

	fail := func(phase CreatePhase, err error) CreateResult {
		result.FailedPhase = phase
		result.Err = err
		return result
	}
	injected := func(phase CreatePhase) error {
		if opts.InjectFailure == nil {
			return nil
		}
		return opts.InjectFailure(phase)
	}

	// 0a. Pure revalidation of the draft/destination — see
	// validateDraftForCommit's doc comment. Must run before ANY write,
	// including the injected-failure seam below (there is nothing to
	// "inject a failure into" here — an invalid draft is refused
	// unconditionally, the same way it would be in production).
	if err := validateDraftForCommit(draft, opts.ExpectedProjectRoot); err != nil {
		return fail(PhaseValidateDraft, err)
	}
	if err := injected(PhaseValidateDraft); err != nil {
		return fail(PhaseValidateDraft, err)
	}

	// 0b. Phantom-project recheck — read-only inventory scan, fails closed
	// on phantom state or a scan error. See runPhantomRecheck's doc
	// comment for why this reuses inventory.Scan instead of reviving the
	// legacy shelled-out `self list` + "[PHANTOM]" string match.
	if err := runPhantomRecheck(draft, opts); err != nil {
		return fail(PhaseValidateDraft, err)
	}

	// 1. Revalidate: Lstat (never Stat) so a symlink is treated as
	// "exists" rather than followed/created through, and a concurrently
	// created project is rejected rather than merged/overwritten.
	if _, err := os.Lstat(finalDir); err == nil {
		return fail(PhaseCreateStaging, fmt.Errorf("%s already exists (concurrent creation?)", finalDir))
	} else if !os.IsNotExist(err) {
		return fail(PhaseCreateStaging, fmt.Errorf("checking %s: %w", finalDir, err))
	}
	if err := injected(PhaseCreateStaging); err != nil {
		return fail(PhaseCreateStaging, err)
	}

	// 2. Create staging as a sibling of the final dir (same filesystem —
	// MkdirTemp with a dir argument guarantees this, which is what makes
	// step 5's rename atomic).
	stagingDir, err := os.MkdirTemp(root, ".lingtai.create-*")
	if err != nil {
		return fail(PhaseCreateStaging, fmt.Errorf("create staging dir: %w", err))
	}
	nonce := filepath.Base(stagingDir)
	cleanupStaging := func() {
		removeOwnedStaging(stagingDir, nonce)
	}
	// Marker written FIRST, before any build step, so a later cleanup
	// (normal failure path here, or a future launcher-side Discard after a
	// kill -9) can always prove this directory belongs to this attempt
	// before deleting it. See design doc Invariant 5.
	if err := os.WriteFile(filepath.Join(stagingDir, stagingMarkerName), []byte(nonce+"\n"), 0o600); err != nil {
		cleanupStaging()
		return fail(PhaseCreateStaging, fmt.Errorf("write staging marker: %w", err))
	}

	// 3a. process.InitProject against staging — called as-is, never
	// forked/duplicated.
	if err := injected(PhaseInitProject); err != nil {
		cleanupStaging()
		return fail(PhaseInitProject, err)
	}
	if err := process.InitProject(stagingDir); err != nil {
		cleanupStaging()
		return fail(PhaseInitProject, fmt.Errorf("init staging project: %w", err))
	}

	// 3b. Resolve the draft's preset choice — IN MEMORY ONLY. This phase
	// used to also call preset.Save here, persisting a dirty draft preset
	// to the REAL global ~/.lingtai-tui/presets/saved/ directory before
	// validation/rename had even run. A parent review found that wrong:
	// PhaseApplyPreset is pre-rename, so a later pre-rename phase failing
	// (PhaseApplyRecipe, PhaseValidate, PhaseRename itself) would leave a
	// real global preset file behind — global state mutated by an attempt
	// that ultimately produced NO project — while this function's own
	// cleanupStaging only ever removes the STAGING directory, never that
	// global file. That is exactly the "global tree not byte-identical"
	// defect a pre-rename-failure purity test now guards against.
	//
	// preset.Save is deferred to the post-commit phase (runPostCommit,
	// PhasePostCommitConfig) below — the identical write, just moved to
	// run only after the atomic rename has already succeeded. This is safe
	// because GenerateInitJSONWithOpts (called further down, still
	// pre-rename) only ever writes manifest.preset.{default,active,allowed}
	// as PATH STRINGS derived from the preset's Name/Source via
	// preset.RefFor — it performs no filesystem existence check on that
	// path, so the staged init.json is fully well-formed whether or not
	// the referenced saved/<name>.json file exists yet. If the post-commit
	// save later fails, the project is still valid and rename'd (never
	// rolled back, per this function's existing post-commit contract) —
	// its init.json simply references a preset file that doesn't exist on
	// disk yet, which is exactly the kind of "created; setup not finished,
	// retryable" state runPostCommit's warnings already communicate.
	if err := injected(PhaseApplyPreset); err != nil {
		cleanupStaging()
		return fail(PhaseApplyPreset, err)
	}
	chosenPreset, err := resolveDraftPreset(draft)
	if err != nil {
		cleanupStaging()
		return fail(PhaseApplyPreset, err)
	}
	if draft.DraftPresetDirty && draft.DraftPreset != nil {
		toSave := stampAutoEnvVar(*draft.DraftPreset, draft.ExistingKeys.keyNames())
		preset.SyncCapabilityAPIKeyEnv(toSave.Manifest)
		chosenPreset = toSave
	}

	agentName := draft.AgentName
	if agentName == "" {
		agentName = "orchestrator"
	}
	dirName := draft.AgentDirName
	if dirName == "" {
		dirName = agentName
	}
	orchDir := filepath.Join(stagingDir, dirName)
	result.OrchDir = filepath.Join(finalDir, dirName) // final path, post-rename
	result.OrchName = agentName

	lang := draft.AgentOpts.Language
	if lang == "" {
		lang = "en"
	}

	// 3c. Recipe bundle staging + init.json + apply — all against the
	// staging dir, exactly mirroring firstrun.go's performRecipeSave but
	// targeting stagingDir instead of the real project.
	if err := injected(PhaseApplyRecipe); err != nil {
		cleanupStaging()
		return fail(PhaseApplyRecipe, err)
	}
	if draft.RecipeName != "" {
		stagedProjectRoot, err := copyRecipeBundle(stagingDir, opts.GlobalDir, draft.RecipeName, draft.RecipeCustomDir)
		if err != nil {
			cleanupStaging()
			return fail(PhaseApplyRecipe, fmt.Errorf("stage recipe bundle: %w", err))
		}
		agentOpts := draft.AgentOpts
		if commentPath := resolveRecipeComment(stagedProjectRoot, lang); commentPath != "" {
			agentOpts.CommentFile = commentPath
		}
		if covenantPath := resolveRecipeCovenant(stagedProjectRoot, lang); covenantPath != "" {
			agentOpts.CovenantFile = covenantPath
		}
		if proceduresPath := resolveRecipeProcedures(stagedProjectRoot, lang); proceduresPath != "" {
			agentOpts.ProceduresFile = proceduresPath
		}
		if err := preset.GenerateInitJSONWithOpts(chosenPreset, agentName, dirName, stagingDir, opts.GlobalDir, agentOpts); err != nil {
			cleanupStaging()
			return fail(PhaseApplyRecipe, fmt.Errorf("write staged init.json: %w", err))
		}
		humanDir := filepath.Join(stagingDir, "human")
		humanAddr := "human"
		if humanNode, err := fs.ReadAgent(humanDir); err == nil && humanNode.Address != "" {
			humanAddr = humanNode.Address
		}
		soulDelayStr := "kernel default"
		if agentOpts.SoulDelay != nil {
			soulDelayStr = formatNumber(*agentOpts.SoulDelay)
		}
		if err := applyRecipeBundle(stagedProjectRoot, stagingDir, humanDir, humanAddr, draft.RecipeName, draft.RecipeCustomDir, lang, soulDelayStr); err != nil {
			cleanupStaging()
			return fail(PhaseApplyRecipe, fmt.Errorf("apply staged recipe: %w", err))
		}
	} else {
		// No recipe selected — still need a valid init.json for the
		// orchestrator so the "exactly one orchestrator" invariant and
		// the eventual launch succeed.
		if err := preset.GenerateInitJSONWithOpts(chosenPreset, agentName, dirName, stagingDir, opts.GlobalDir, draft.AgentOpts); err != nil {
			cleanupStaging()
			return fail(PhaseApplyRecipe, fmt.Errorf("write staged init.json: %w", err))
		}
	}

	// 4. Validate the staged tree: exactly one orchestrator (the same
	// invariant main.go enforces on every launch, reused via the tui
	// package's exported detection helpers rather than re-implemented).
	if err := injected(PhaseValidate); err != nil {
		cleanupStaging()
		return fail(PhaseValidate, err)
	}
	orchestrators := DetectOrchestrators(stagingDir)
	if len(orchestrators) != 1 {
		cleanupStaging()
		return fail(PhaseValidate, fmt.Errorf("staged project has %d orchestrators, want exactly 1", len(orchestrators)))
	}
	if _, err := os.Stat(filepath.Join(orchDir, "init.json")); err != nil {
		cleanupStaging()
		return fail(PhaseValidate, fmt.Errorf("staged orchestrator missing init.json: %w", err))
	}

	// 5. Publish. This is the single product-visible commit point: the
	// instant this os.Rename succeeds, the final .lingtai/ exists and
	// nothing below this line may delete or roll it back.
	if err := injected(PhaseRename); err != nil {
		cleanupStaging()
		return fail(PhaseRename, err)
	}
	// Re-check immediately before rename to narrow (not eliminate — a
	// true TOCTOU race remains theoretically possible, os.Rename itself
	// has no "fail if destination exists and is a directory" atomic
	// primitive on all platforms) the concurrent-creation window opened
	// since step 1.
	if _, err := os.Lstat(finalDir); err == nil {
		cleanupStaging()
		return fail(PhaseRename, fmt.Errorf("%s appeared during staging (concurrent creation?)", finalDir))
	}
	if err := os.Rename(stagingDir, finalDir); err != nil {
		cleanupStaging()
		return fail(PhaseRename, fmt.Errorf("publish project: %w", err))
	}
	result.Committed = true
	result.OrchDir = filepath.Join(finalDir, dirName)

	// Staging is gone (renamed away) — the marker file travelled with it
	// into the final directory. Remove it now that it has served its
	// purpose; a failure here is cosmetic and does not affect validity.
	_ = os.Remove(filepath.Join(finalDir, stagingMarkerName))

	// 6. Post-commit: never rolls back. Every failure below is collected
	// as a warning; RunProjectCreate still returns Committed=true.
	runPostCommit(draft, opts, finalDir, chosenPreset, &result)

	return result
}

// resolveDraftPreset returns the explicit preset captured in Review. The
// commit-boundary validator rejects nil before this helper runs; keep the
// defensive check here as well so future callers can never silently fall back
// to an unrelated first-on-disk preset.
func resolveDraftPreset(draft *ProjectDraft) (preset.Preset, error) {
	if draft == nil || draft.DraftPreset == nil {
		return preset.Preset{}, fmt.Errorf("selected preset is required")
	}
	return *draft.DraftPreset, nil
}

// runPostCommit performs the deferred, best-effort steps that in a normal
// (non-launcher) startup already happen unconditionally: saving the API
// key / theme / language / Codex tokens gathered in the draft, registering
// the project, refreshing the bundled utility library, and attempting to
// launch the agent. None of these may cause a rollback — the project is
// already valid and retryable the moment this function is entered.
func runPostCommit(draft *ProjectDraft, opts CreateOptions, finalDir string, chosenPreset preset.Preset, result *CreateResult) {
	warn := func(phase CreatePhase, format string, args ...interface{}) {
		result.PostCommitWarnings = append(result.PostCommitWarnings, fmt.Sprintf("%s: "+format, append([]interface{}{phase.String()}, args...)...))
	}
	injected := func(phase CreatePhase) error {
		if opts.InjectFailure == nil {
			return nil
		}
		return opts.InjectFailure(phase)
	}

	if err := injected(PhasePostCommitConfig); err != nil {
		warn(PhasePostCommitConfig, "%v", err)
	} else {
		// Global config directory now becomes an intentional side effect —
		// this is post-commit, so EnsureGlobalDir (not the pure
		// GlobalDirPath) is correct here.
		if _, err := config.EnsureGlobalDir(); err != nil {
			warn(PhasePostCommitConfig, "ensure global dir: %v", err)
		}
		// Save a dirty draft preset now — deferred from the pre-rename
		// staging phase (see RunProjectCreate's PhaseApplyPreset comment)
		// so a pre-rename failure never leaves an orphaned global preset
		// file behind for a project that was never actually created. A
		// failure here is a warning, not a rollback trigger: the project
		// is already valid and rename'd; its staged init.json already
		// references this preset's path (via preset.RefFor, computed from
		// Name/Source alone, no existence check) regardless of whether
		// this save succeeds — so a failure here just means the referenced
		// file doesn't exist yet, exactly the "created; setup not
		// finished, retryable" state these warnings communicate.
		if draft.DraftPresetDirty && draft.DraftPreset != nil {
			toSave := stampAutoEnvVar(*draft.DraftPreset, draft.ExistingKeys.keyNames())
			preset.SyncCapabilityAPIKeyEnv(toSave.Manifest)
			if err := preset.Save(toSave); err != nil {
				warn(PhasePostCommitConfig, "save draft preset: %v", err)
			}
		}
		tuiCfg := config.LoadTUIConfig(opts.GlobalDir)
		tuiCfg = draft.applyToConfig(tuiCfg)
		if err := config.SaveTUIConfig(opts.GlobalDir, tuiCfg); err != nil {
			warn(PhasePostCommitConfig, "save tui config: %v", err)
		}
		if !draft.DraftAPIKey.Empty() && draft.DraftAPIKeyEnv != "" {
			cfg, err := config.LoadConfig(opts.GlobalDir)
			if err != nil {
				warn(PhasePostCommitConfig, "load config: %v", err)
			} else {
				if cfg.Keys == nil {
					cfg.Keys = map[string]string{}
				}
				cfg.Keys[draft.DraftAPIKeyEnv] = draft.DraftAPIKey.Reveal()
				if err := config.SaveConfig(opts.GlobalDir, cfg); err != nil {
					warn(PhasePostCommitConfig, "save api key: %v", err)
				}
			}
		} else {
			config.EnsureConfigPersisted(opts.GlobalDir)
		}
		if !draft.DraftCodexTokens.Empty() {
			authPath := legacyCodexAuthPath(opts.GlobalDir)
			var tokens json.RawMessage = draft.DraftCodexTokens.Reveal()
			if err := os.WriteFile(authPath, tokens, 0o600); err != nil {
				warn(PhasePostCommitConfig, "save codex tokens: %v", err)
			}
		}
	}

	if err := injected(PhasePostCommitRegister); err != nil {
		warn(PhasePostCommitRegister, "%v", err)
	} else {
		if err := config.Register(opts.GlobalDir, draft.ProjectRoot); err != nil {
			warn(PhasePostCommitRegister, "%v", err)
		}
		preset.PopulateBundledLibrary(opts.GlobalDir)
	}

	if err := injected(PhasePostCommitLaunch); err != nil {
		warn(PhasePostCommitLaunch, "%v", err)
		return
	}
	runtimeReady := opts.RuntimeReady
	if runtimeReady == nil {
		runtimeReady = config.RuntimeReady
	}
	if err := runtimeReady(opts.GlobalDir); err != nil {
		warn(PhasePostCommitLaunch, "%v", err)
		return
	}
	lingtaiCmd := opts.LingtaiCmd
	if lingtaiCmd == "" {
		// A genuinely fresh launcher entry can begin before the runtime exists,
		// so resolve its command only after confirming the runtime is ready.
		resolveLingtaiCmd := opts.ResolveLingtaiCmd
		if resolveLingtaiCmd == nil {
			resolveLingtaiCmd = config.LingtaiCmd
		}
		lingtaiCmd = resolveLingtaiCmd(opts.GlobalDir)
	}
	if lingtaiCmd == "" {
		warn(PhasePostCommitLaunch, "runtime command unavailable")
		return
	}
	if err := preset.Bootstrap(opts.GlobalDir); err != nil {
		warn(PhasePostCommitLaunch, "bootstrap: %v", err)
	}
	if _, err := process.LaunchAgent(lingtaiCmd, result.OrchDir); err != nil {
		warn(PhasePostCommitLaunch, "launch agent: %v", err)
	}
}

// removeOwnedStaging deletes a staging directory ONLY after confirming its
// marker file exists and matches the expected nonce — cleanup must never
// blind-delete a ".lingtai.create-*" directory it cannot prove it created
// (design doc Invariant 5). Safe to call even if the directory was already
// partially built or removed.
func removeOwnedStaging(stagingDir, nonce string) {
	markerPath := filepath.Join(stagingDir, stagingMarkerName)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return // no marker (or unreadable) — cannot prove ownership, refuse to delete
	}
	if strings.TrimSpace(string(data)) != nonce {
		return // marker present but doesn't match — not ours, refuse to delete
	}
	_ = os.RemoveAll(stagingDir)
}

// DetectUnfinishedStaging performs a READ-ONLY scan of root for leftover
// ".lingtai.create-*" directories (e.g. left behind by a kill -9 mid-build)
// and returns the ones that carry a valid ownership marker WHOSE CONTENT
// matches the directory's own name — the same check removeOwnedStaging/
// DiscardUnfinishedStaging require before deleting. A directory that merely
// has *a* marker file present, with stale/foreign/corrupt content, must
// never be offered as "yours to discard" any more than a directory with no
// marker at all: presence-only used to be sufficient here (a narrower check
// than the delete path used), which meant a directory whose marker existed
// but named a DIFFERENT nonce — e.g. copied/moved from elsewhere, or a
// remnant of a filesystem-level rename — could be shown to the user as
// discardable even though DiscardUnfinishedStaging itself would still
// (correctly) refuse to delete it, silently mismatching what the UI offers
// against what Discard actually does. It never deletes anything. Callers
// (the launcher) use this to offer an explicit Resume/Discard choice;
// Discard must still go through DiscardUnfinishedStaging so the same
// marker-ownership check gates the delete.
func DetectUnfinishedStaging(root string) []string {
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasPrefix(e.Name(), ".lingtai.create-") {
			continue
		}
		markerPath := filepath.Join(root, e.Name(), stagingMarkerName)
		data, err := os.ReadFile(markerPath)
		if err != nil {
			continue // no ownership marker (or unreadable) — not something we can safely offer to discard
		}
		if strings.TrimSpace(string(data)) != e.Name() {
			continue // marker present but content doesn't match this directory — not provably ours
		}
		found = append(found, filepath.Join(root, e.Name()))
	}
	return found
}

// DiscardUnfinishedStaging removes a leftover staging directory found by
// DetectUnfinishedStaging, gated by the same ownership-marker check
// removeOwnedStaging uses. stagingDir must be an absolute path previously
// returned by DetectUnfinishedStaging.
func DiscardUnfinishedStaging(stagingDir string) error {
	markerPath := filepath.Join(stagingDir, stagingMarkerName)
	data, err := os.ReadFile(markerPath)
	if err != nil {
		return fmt.Errorf("refusing to discard %s: no ownership marker (%w)", stagingDir, err)
	}
	nonce := strings.TrimSpace(string(data))
	expected := filepath.Base(stagingDir)
	if nonce != expected {
		return fmt.Errorf("refusing to discard %s: marker %q does not match directory name", stagingDir, nonce)
	}
	return os.RemoveAll(stagingDir)
}
