package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
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

// CreatePhase identifies outer validation/dependency/UI work. Historical domain
// phase labels remain source-compatible, but Go no longer executes those phases.
type CreatePhase int

const (
	PhaseValidateDraft CreatePhase = iota
	PhaseCreateStaging
	PhaseInitProject
	PhaseApplyPreset
	PhaseApplyRecipe
	PhaseValidate
	PhasePersistDependencies
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
	case PhasePersistDependencies:
		return "persist_dependencies"
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

// CreateResult reports the outcome of RunProjectCreate.
type CreateResult struct {
	// Committed is true only after accepted kernel created output and exit 0. A
	// true value means the final .lingtai/ exists and must never be rolled
	// back, even if a later post-commit phase failed.
	Committed bool
	// Unknown means publication was not proved or disproved. The launcher must
	// not offer another confirmation until the operator leaves and reconciles.
	Unknown bool
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
	Context context.Context
	// Test seam for this real subprocess boundary, not a Go creation fallback.
	CreateInteractive func(context.Context, string, process.InteractiveProjectRequest, func() bool) (process.InteractiveProjectResult, error)
	GlobalDir         string
	LingtaiCmd        string
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
	// RunProjectCreate makes to observe matching processes immediately
	// before its one request (see createProcessEvidence). Nil (the default/production
	// case) uses the real scan against the OS process table. Tests inject
	// a fake here instead of needing real background processes to exercise
	// the phantom/scan-error paths.
	ScanInventory func(projectRoot string) (inventory.Snapshot, error)
}

// validateDraftForCommit revalidates a *ProjectDraft and its destination
// PURELY (no filesystem writes — Lstat/Stat reads only) at the commit
// boundary, immediately before RunProjectCreate performs its first mutation
// (the kernel creation request). This exists because a crafted or stale draft — e.g. one
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
	actualRoot, actualErr := filepath.EvalSymlinks(draft.ProjectRoot)
	approvedRoot, approvedErr := filepath.EvalSymlinks(expectedProjectRoot)
	actualInfo, actualStatErr := os.Stat(actualRoot)
	approvedInfo, approvedStatErr := os.Stat(approvedRoot)
	if actualErr != nil || approvedErr != nil || actualStatErr != nil || approvedStatErr != nil || !os.SameFile(actualInfo, approvedInfo) {
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
	reviewed := preset.Clone(*draft.DraftPreset, draft.DraftPreset.Name)
	if errs := reviewed.Validate(); len(errs) > 0 {
		return fmt.Errorf("selected preset %q is invalid: %w", draft.DraftPreset.Name, errs[0])
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

// RunProjectCreate consumes fresh interactive creation, not a second domain
// owner. Kernel owns staging, Recipe, readers and exclusive publication. Go
// persists prerequisites after readiness and runs UI work after created exit 0.
func RunProjectCreate(draft *ProjectDraft, opts CreateOptions) CreateResult {
	result := CreateResult{FailedPhase: PhaseValidateDraft}
	if err := validateDraftForCommit(draft, opts.ExpectedProjectRoot); err != nil {
		// Do not surface arbitrary preset validation values (they may be credentials).
		result.Err = fmt.Errorf("project draft validation failed; review destination, identity and selected preset")
		return result
	}
	chosen, err := resolveDraftPreset(draft)
	if err != nil {
		result.Err = fmt.Errorf("selected preset unavailable")
		return result
	}
	// Deep copy with UseNumber: normalization/stamping must not mutate Review or
	// round large/unknown preset fields through float64.
	data, err := json.Marshal(chosen.Manifest)
	if err != nil {
		result.Err = fmt.Errorf("selected preset cannot be serialized")
		return result
	}
	chosen.Manifest = nil // Decoder must not reuse the Review map.
	if err := preset.DecodeJSONUseNumber(data, &chosen.Manifest); err != nil {
		result.Err = fmt.Errorf("selected preset cannot be read")
		return result
	}
	// The exchange is JSON, not the older UI float64 context representation.
	// Relocate legacy context metadata without rounding exact JSON integers.
	if legacy, exists := chosen.Manifest["context_limit"]; exists {
		delete(chosen.Manifest, "context_limit")
		if llm, ok := chosen.Manifest["llm"].(map[string]interface{}); ok {
			if _, canonical := llm["context_limit"]; !canonical {
				llm["context_limit"] = legacy
			}
		}
	}
	if err := chosen.NormalizeLegacyCapabilities(); err != nil {
		result.Err = fmt.Errorf("selected preset cannot be normalized")
		return result
	}
	if draft.DraftPresetDirty {
		chosen = stampAutoEnvVar(chosen, draft.ExistingKeys.keyNames())
		preset.SyncCapabilityAPIKeyEnv(chosen.Manifest)
		chosen.Source = preset.SourceSaved
	}
	request, err := interactiveRequest(draft, opts, chosen)
	if err != nil {
		result.Err = err
		return result
	}
	python := opts.LingtaiCmd
	if python == "" {
		resolve := opts.ResolveLingtaiCmd
		if resolve == nil {
			resolve = config.LingtaiCmd
		}
		python = resolve(opts.GlobalDir)
	}
	if python == "" {
		result.Err = fmt.Errorf("installed kernel unavailable; use the normal installer explicitly; no legacy creation fallback")
		return result
	}
	driver := opts.CreateInteractive
	if driver == nil {
		driver = process.CreateInteractiveProject
	}
	// Last outer observation immediately before the one physical request. No
	// absence-only phantom heuristic, subprocess list parsing or mutation here.
	request.ProcessEvidence = createProcessEvidence(draft.ProjectRoot, opts)
	published, err := driver(opts.Context, python, request, func() bool {
		result.FailedPhase = PhasePersistDependencies
		if opts.InjectFailure != nil && opts.InjectFailure(PhasePersistDependencies) != nil {
			return false
		}
		// Never serialize these errors or send them to the child: the ack is a bool.
		return persistCreateDependencies(draft, opts, chosen) == nil
	})
	if err != nil {
		result.Err = err
		var outcome *process.ProjectOperationError
		result.Unknown = !errors.As(err, &outcome) || !outcome.Known
		return result
	}
	result.Committed = true
	result.OrchName = published.AgentName
	finalDir := filepath.Join(draft.ProjectRoot, ".lingtai")
	result.OrchDir = filepath.Join(finalDir, published.DirectoryName)
	result.PostCommitWarnings = append(result.PostCommitWarnings, published.Warnings...)
	runPostCommit(draft, opts, finalDir, &result)
	return result
}

// createProcessEvidence reports a fresh matching OS inventory, not liveness or
// a kernel lease. A failed/mismatched observation is unknown, never clean.
func createProcessEvidence(root string, opts CreateOptions) string {
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "unknown"
	}
	rootInfo, err := os.Stat(resolved)
	if err != nil || !rootInfo.IsDir() {
		return "unknown"
	}
	scan := opts.ScanInventory
	if scan == nil {
		scan = func(root string) (inventory.Snapshot, error) {
			// Observe all rows so lexical symlink aliases cannot be filtered out before
			// we compare the independently resolved existing project root.
			return inventory.Scan(inventory.Options{SelfPID: os.Getpid()})
		}
	}
	snap, err := scan(resolved)
	if err != nil {
		return "unknown"
	}
	sameRoot := func(candidate string) (bool, bool) {
		path, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			return false, false
		}
		info, err := os.Stat(path)
		if err != nil {
			return false, false
		}
		return os.SameFile(info, rootInfo), true
	}
	if snap.FilterDir != "" {
		same, valid := sameRoot(snap.FilterDir)
		if !valid || !same {
			return "unknown"
		}
	}
	for _, r := range snap.Records {
		candidate := r.Project
		if candidate == "" && r.AgentDir != "" {
			candidate = inventory.ProjectFromAgentDir(r.AgentDir)
		}
		if candidate == "" {
			return "unknown"
		}
		same, valid := sameRoot(candidate)
		if !valid {
			return "unknown"
		} // Incomplete evidence is not a clean scan.
		if same {
			return "blocked"
		}
	}
	return "clean"
}

func interactiveRequest(draft *ProjectDraft, opts CreateOptions, chosen preset.Preset) (process.InteractiveProjectRequest, error) {
	lang := draft.AgentOpts.Language
	if lang == "" {
		lang = "en"
	}
	covenant, err := preset.CreationCovenant(opts.GlobalDir, lang, draft.AgentOpts.CovenantFile)
	if err != nil {
		return process.InteractiveProjectRequest{}, fmt.Errorf("creation covenant must be readable nonempty UTF-8 text")
	}
	pad := ""
	if draft.AgentOpts.CommentFile != "" {
		data, err := os.ReadFile(draft.AgentOpts.CommentFile)
		if err != nil || !utf8.Valid(data) {
			return process.InteractiveProjectRequest{}, fmt.Errorf("creation Pad input must be readable UTF-8 text")
		}
		pad = string(data) // Current Pad input, NOT a retired comment_file setting.
	}
	dir := draft.AgentDirName
	if dir == "" {
		dir = draft.AgentName
	}
	// Public normalized snapshot retains unknown manifest extensions and exact
	// numbers. It is not the preset profile, credential store or wizard draft.
	manifest := chosen.Manifest
	mcp := map[string]interface{}{}
	for _, name := range draft.AgentOpts.Addons {
		module, env, rel, ok := preset.DefaultMCPSpec(name)
		if !ok {
			continue
		}
		mcp[name] = map[string]interface{}{"type": "stdio", "command": config.VenvPython(filepath.Join(opts.GlobalDir, "runtime", "venv")), "args": []string{"-m", module}, "env": map[string]string{env: rel}}
	}
	var commands []string
	for _, command := range DefaultCommands() {
		commands = append(commands, fmt.Sprintf("  - /%s — %s", command.Name, i18n.TIn(lang, command.Detail)))
	}
	return process.InteractiveProjectRequest{
		ProjectDir: draft.ProjectRoot, ApprovedRoot: opts.ExpectedProjectRoot,
		AgentName: draft.AgentName, DirectoryName: dir, PresetRef: preset.RefFor(chosen), PresetManifest: manifest,
		Options: process.InteractiveProjectOptions{Language: lang, Covenant: covenant, Karma: draft.AgentOpts.Karma, Nirvana: draft.AgentOpts.Nirvana, Pad: pad,
			EnvFile: config.EnvFilePath(opts.GlobalDir), VenvPath: filepath.Join(opts.GlobalDir, "runtime", "venv"), Addons: draft.AgentOpts.Addons, MCP: mcp, AllowedPresets: draft.AgentOpts.AllowedPresets},
		Recipe: draft.RecipeName, Location: "unknown", Commands: strings.Join(commands, "\n"),
	}, nil
}

// persistCreateDependencies makes the selected preset reference and any
// credential material collected during the draft available before project
// publication. It runs only after the staged project validates. The writes are
// deliberately not a cross-file transaction: a later dependency or rename
// failure may leave valid user-owned global state, which is safer than either
// deleting an older value or publishing a dangling project.
func persistCreateDependencies(draft *ProjectDraft, opts CreateOptions, chosenPreset preset.Preset) error {
	ref := preset.RefFor(chosenPreset)
	if ref == "" {
		return fmt.Errorf("selected preset dependency has no reference")
	}

	if draft.DraftPresetDirty {
		// chosenPreset is the already normalized/stamped SourceSaved object
		// used to generate staged init.json. Persist that exact value rather
		// than reconstructing it from the original draft a second time.
		if err := preset.Save(chosenPreset); err != nil {
			return fmt.Errorf("save selected preset dependency %q: %w", ref, err)
		}
	}

	if err := verifyPresetDependency(chosenPreset); err != nil {
		// A fresh-home draft can select a compiled built-in before templates
		// have ever been extracted. templates/ is TUI-owned, so materialize
		// the canonical set narrowly and verify the SAME ref again. Missing
		// saved/user-owned selections are never repaired or substituted.
		if !draft.DraftPresetDirty && chosenPreset.Source == preset.SourceTemplate {
			if refreshErr := preset.RefreshTemplates(); refreshErr != nil {
				return fmt.Errorf("materialize template dependency %q: %w", ref, refreshErr)
			}
			if retryErr := verifyPresetDependency(chosenPreset); retryErr != nil {
				return retryErr
			}
		} else {
			return err
		}
	}

	if !draft.DraftAPIKey.Empty() {
		envName := strings.TrimSpace(draft.DraftAPIKeyEnv)
		if envName == "" {
			return fmt.Errorf("pending API key has no environment variable name")
		}
		cfg, err := config.LoadConfigReadOnly(opts.GlobalDir)
		if err != nil {
			return fmt.Errorf("load config for pending API key %s: %w", envName, err)
		}
		if cfg.Keys == nil {
			cfg.Keys = map[string]string{}
		}
		cfg.Keys[envName] = draft.DraftAPIKey.Reveal()
		if err := config.SaveConfig(opts.GlobalDir, cfg); err != nil {
			return fmt.Errorf("persist pending API key %s: %w", envName, err)
		}
		persisted, err := config.LoadConfigReadOnly(opts.GlobalDir)
		if err != nil {
			return fmt.Errorf("verify pending API key %s: %w", envName, err)
		}
		if persisted.Keys[envName] != draft.DraftAPIKey.Reveal() {
			return fmt.Errorf("verify pending API key %s: persisted value unavailable", envName)
		}
		if info, err := os.Stat(config.EnvFilePath(opts.GlobalDir)); err != nil || info.IsDir() {
			if err != nil {
				return fmt.Errorf("verify pending API key environment file: %w", err)
			}
			return fmt.Errorf("verify pending API key environment file: path is a directory")
		}
	}

	if !draft.DraftCodexTokens.Empty() {
		tokens := draft.DraftCodexTokens.Reveal()
		if !json.Valid(tokens) {
			return fmt.Errorf("pending Codex token bundle is not valid JSON")
		}
		if err := os.MkdirAll(opts.GlobalDir, 0o755); err != nil {
			return fmt.Errorf("create Codex credential directory: %w", err)
		}
		authPath := legacyCodexAuthPath(opts.GlobalDir)
		if err := os.WriteFile(authPath, tokens, 0o600); err != nil {
			return fmt.Errorf("persist pending Codex token bundle: %w", err)
		}
		persisted, err := os.ReadFile(authPath)
		if err != nil {
			return fmt.Errorf("verify pending Codex token bundle: %w", err)
		}
		if string(persisted) != string(tokens) {
			return fmt.Errorf("verify pending Codex token bundle: persisted bundle differs")
		}
		if _, ok := readCodexTokenFile(authPath); !ok {
			return fmt.Errorf("verify pending Codex token bundle: credential unavailable")
		}
	}

	return nil
}

func verifyPresetDependency(p preset.Preset) error {
	ref := preset.RefFor(p)
	resolved := preset.ResolveRefs([]string{ref}, nil)
	if len(resolved) != 1 || resolved[0].Ref != ref {
		return fmt.Errorf("resolve selected preset dependency %q: unexpected result", ref)
	}
	if !resolved[0].Exists {
		return fmt.Errorf("selected preset dependency %q does not exist", ref)
	}
	if !resolved[0].ManifestValid {
		return fmt.Errorf("selected preset dependency %q is invalid", ref)
	}
	return nil
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

// runPostCommit performs the remaining best-effort steps that in a normal
// (non-launcher) startup already happen unconditionally: saving theme/language,
// registering the project, refreshing the bundled utility library, and
// attempting to launch the agent. Preset and pending credential dependencies
// are already durable and verified before this function is entered. None of
// these remaining steps may cause a rollback.
func runPostCommit(draft *ProjectDraft, opts CreateOptions, finalDir string, result *CreateResult) {
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
		// Ensure the global config directory for the remaining post-commit
		// writes. The dependency phase may already have created it while
		// persisting a preset or credential.
		if _, err := config.EnsureGlobalDir(); err != nil {
			warn(PhasePostCommitConfig, "ensure global dir: %v", err)
		}
		tuiCfg := config.LoadTUIConfig(opts.GlobalDir)
		tuiCfg = draft.applyToConfig(tuiCfg)
		if err := config.SaveTUIConfig(opts.GlobalDir, tuiCfg); err != nil {
			warn(PhasePostCommitConfig, "save tui config: %v", err)
		}
		config.EnsureConfigPersisted(opts.GlobalDir)
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

// DetectUnfinishedStaging performs a READ-ONLY scan of root for leftover
// ".lingtai.create-*" directories (e.g. left behind by a kill -9 mid-build)
// and returns the ones that carry a valid ownership marker WHOSE CONTENT
// matches the directory's own name — the same check the legacy explicit
// DiscardUnfinishedStaging requires before deleting. A directory that merely
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
// DetectUnfinishedStaging, gated by the legacy ownership-marker check. stagingDir must be an absolute path previously
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
