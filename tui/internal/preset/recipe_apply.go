package preset

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// Recipe lifecycle primitives.
//
// The TUI treats every project as "the recipe in <project>/.recipe/ is the
// one currently selected." There is no RecipeCustom/Imported/Agora
// distinction — all recipes, regardless of where they originated, get
// copied into the project root on selection and applied uniformly from
// there.
//
// Layout invariants (project-local):
//
//	<projectRoot>/
//	├── <library_name>/                  (optional — shared skills)
//	├── .recipe/                         (the currently-selected recipe bundle)
//	│   ├── recipe.json
//	│   ├── greet/{greet.md, <lang>/greet.md}
//	│   ├── comment/...                  (optional)
//	│   ├── covenant/...                 (optional)
//	│   └── procedures/...               (optional)
//	└── .lingtai/
//	    ├── <agent>/init.json            (agents)
//	    └── .tui-asset/.recipe/          (snapshot of last APPLIED recipe — audit trail + change detection)
//
// The "last applied" snapshot under .tui-asset/.recipe/ is a full directory
// copy of .recipe/ at the moment apply completed. Diffing project .recipe/
// against the snapshot tells us whether re-apply is needed on next startup.

const (
	// AppliedRecipeSubpath is the project-relative path where the TUI stores
	// a snapshot of the most recently applied .recipe/ bundle. Siblings of
	// .tui-asset/ (e.g. meta.json) are unrelated; this subpath is recipe-only.
	AppliedRecipeSubpath = ".tui-asset/.recipe"
)

// CopyBundle copies a recipe bundle from a source directory into a project
// root. The bundle's .recipe/ dotfolder, optional library sibling (named
// per recipe.json's library_name), and optional .lingtai/ sibling (for
// imported networks) are all mirrored into the destination project root.
//
// Copy is **replace** semantics for .recipe/ (old .recipe/ is removed) and
// **additive** for the library folder (existing library content at
// <project>/<library_name>/ is NOT removed; the new library is merged on
// top — last-write-wins per file). .lingtai/ is merged similarly.
//
// This matches the rule: behavioral layer is owned by the current recipe,
// libraries accumulate across recipe changes.
//
// Fails if srcBundleRoot does not contain .recipe/recipe.json. The caller
// should validate first via LoadRecipeInfo(srcBundleRoot, lang).
func CopyBundle(srcBundleRoot, projectRoot string) error {
	if srcBundleRoot == "" || projectRoot == "" {
		return fmt.Errorf("CopyBundle: empty paths")
	}
	info, err := LoadRecipeInfo(srcBundleRoot, "")
	if err != nil {
		return fmt.Errorf("CopyBundle: source bundle invalid: %w", err)
	}

	// 1. .recipe/ — replace wholesale.
	srcRecipe := filepath.Join(srcBundleRoot, RecipeDotDir)
	dstRecipe := filepath.Join(projectRoot, RecipeDotDir)
	if err := os.RemoveAll(dstRecipe); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("CopyBundle: remove old .recipe/: %w", err)
	}
	if err := copyTree(srcRecipe, dstRecipe); err != nil {
		return fmt.Errorf("CopyBundle: copy .recipe/: %w", err)
	}

	// 2. Library folder (if recipe declares one) — merge.
	if info.LibraryName != nil && *info.LibraryName != "" {
		srcLib := filepath.Join(srcBundleRoot, *info.LibraryName)
		if st, err := os.Stat(srcLib); err == nil && st.IsDir() {
			dstLib := filepath.Join(projectRoot, *info.LibraryName)
			if err := copyTree(srcLib, dstLib); err != nil {
				return fmt.Errorf("CopyBundle: copy library %q: %w", *info.LibraryName, err)
			}
		}
		// Silently tolerate missing library dir; LoadRecipeInfo already
		// validated the manifest, and the library absence is visible to
		// downstream callers via ResolveLibraryDir.
	}

	// 3. .lingtai/ sibling (if present — imported-network case) — merge.
	srcLingtai := filepath.Join(srcBundleRoot, ".lingtai")
	if st, err := os.Stat(srcLingtai); err == nil && st.IsDir() {
		dstLingtai := filepath.Join(projectRoot, ".lingtai")
		if err := copyTree(srcLingtai, dstLingtai); err != nil {
			return fmt.Errorf("CopyBundle: copy .lingtai/: %w", err)
		}
	}

	return nil
}

// RecipeNeedsApply reports whether the recipe currently selected in the
// project (.recipe/) differs from the last-applied snapshot
// (.lingtai/.tui-asset/.recipe/). Returns true when:
//
//   - .recipe/ exists but the snapshot does not (first-time apply, e.g.
//     fresh project or imported network)
//   - both exist but their contents differ
//
// Returns false when:
//
//   - .recipe/ does not exist (no recipe selected — nothing to apply)
//   - both exist and are byte-identical
//
// The comparison walks both trees and compares every file by content.
// Directories, mode bits, and mtimes are ignored — only the byte stream
// of each file matters. Extra files on either side make the recipes
// "different."
func RecipeNeedsApply(projectRoot string) bool {
	if projectRoot == "" {
		return false
	}
	srcRecipe := filepath.Join(projectRoot, RecipeDotDir)
	if st, err := os.Stat(srcRecipe); err != nil || !st.IsDir() {
		return false
	}
	snapshot := filepath.Join(projectRoot, ".lingtai", AppliedRecipeSubpath)
	if st, err := os.Stat(snapshot); err != nil || !st.IsDir() {
		return true
	}
	equal, _ := treesEqual(srcRecipe, snapshot)
	return !equal
}

// ApplyRecipe materializes the recipe currently in <projectRoot>/.recipe/
// across all agents under <projectRoot>/.lingtai/<agent>/.
//
// For each agent dir it:
//   - Writes .prompt from the recipe's greet template (with runtime
//     substitutions for {{time}}, {{addr}}, {{lang}}, {{soul_delay}},
//     {{location}}, {{commands}}). Greet substitution is delegated to the
//     caller via the greetSubstitutor callback so this package stays free
//     of TUI-only helpers.
//   - Updates init.json:
//     * comment_file / covenant_file / procedures_file fields in
//       manifest.capabilities.{psyche, ...} (TODO: caller-specified
//       structure — for v1 this function only handles library.paths and
//       .prompt; comment/covenant/procedures rewiring happens in the TUI
//       layer that also owns init.json generation)
//     * manifest.capabilities.library.paths gets "../../<library_name>"
//       appended when the recipe declares a library (additive — existing
//       entries are preserved, never removed)
//   - Snapshots the applied recipe to .lingtai/.tui-asset/.recipe/ so a
//     future RecipeNeedsApply call can detect future changes.
//
// Returns the number of agents successfully processed and, on failure,
// the first error encountered. Partial success is possible — the caller
// may want to re-invoke after remediating any per-agent errors.
//
// This function is additive for library paths by design: on recipe
// change, we DO NOT remove prior recipes' library entries. The user may
// have come to rely on that library, and auto-removal is the sort of
// thing that silently breaks agents. Manual cleanup is the user's
// responsibility.
func ApplyRecipe(projectRoot, lang string, greetSubstitutor func(template string) string) (applied int, err error) {
	if projectRoot == "" {
		return 0, fmt.Errorf("ApplyRecipe: empty projectRoot")
	}
	bundleDir := projectRoot
	info, err := LoadRecipeInfo(bundleDir, lang)
	if err != nil {
		return 0, fmt.Errorf("ApplyRecipe: invalid recipe in %s: %w", bundleDir, err)
	}

	greetPath := ResolveGreetPath(bundleDir, lang)
	var greetTemplate string
	if greetPath != "" {
		data, rerr := os.ReadFile(greetPath)
		if rerr == nil {
			greetTemplate = string(data)
		}
	}

	// Expand the library-path entry once; it's identical for every agent.
	var libPathEntry string
	if info.LibraryName != nil && *info.LibraryName != "" {
		libPathEntry = LibraryPathForInitJSON(bundleDir, lang)
	}

	lingtaiDir := filepath.Join(projectRoot, ".lingtai")
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return 0, fmt.Errorf("ApplyRecipe: read .lingtai: %w", err)
	}

	var firstErr error
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || name == "" || name[0] == '.' || name == "human" {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, name)

		// Write .prompt (greet).
		if greetTemplate != "" {
			var rendered string
			if greetSubstitutor != nil {
				rendered = greetSubstitutor(greetTemplate)
			} else {
				rendered = greetTemplate
			}
			promptPath := filepath.Join(agentDir, ".prompt")
			if werr := os.WriteFile(promptPath, []byte(rendered), 0o644); werr != nil && firstErr == nil {
				firstErr = fmt.Errorf("ApplyRecipe: write .prompt for %s: %w", name, werr)
				continue
			}
		}

		// Patch library.paths (additive) when recipe declares a library.
		if libPathEntry != "" {
			initPath := filepath.Join(agentDir, "init.json")
			if werr := AppendLibraryPath(initPath, libPathEntry); werr != nil && firstErr == nil {
				firstErr = fmt.Errorf("ApplyRecipe: update library.paths for %s: %w", name, werr)
				continue
			}
		}
		applied++
	}

	// Snapshot the applied recipe so future RecipeNeedsApply calls can
	// detect change vs the then-current .recipe/.
	snapshot := filepath.Join(projectRoot, ".lingtai", AppliedRecipeSubpath)
	_ = os.RemoveAll(snapshot) // best-effort; fresh copy on each apply
	if err := copyTree(filepath.Join(bundleDir, RecipeDotDir), snapshot); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("ApplyRecipe: snapshot applied recipe: %w", err)
	}

	return applied, firstErr
}

// AppendLibraryPath ensures the given path string appears in the
// manifest.capabilities.library.paths list of the agent's init.json.
// Idempotent: no-op if the path is already present. Preserves all other
// fields in init.json including existing library.paths entries.
//
// The library capability object must already exist in the manifest (via
// libraryDefault() or equivalent). If it's absent, this function is a
// no-op — creating it would be an architectural choice best left to
// preset generation. This mirrors the behavior of migrateLibraryPaths
// (m021).
//
// Writes atomically via temp+rename on change. Returns nil on successful
// no-op or successful write; returns error only on I/O or parse failure.
func AppendLibraryPath(initJSONPath, pathEntry string) error {
	if initJSONPath == "" || pathEntry == "" {
		return nil
	}
	data, err := os.ReadFile(initJSONPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // agent has no init.json yet; caller orders operations
		}
		return fmt.Errorf("read %s: %w", initJSONPath, err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", initJSONPath, err)
	}
	manifest, ok := root["manifest"].(map[string]interface{})
	if !ok {
		return nil
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		return nil
	}
	lib, ok := caps["library"].(map[string]interface{})
	if !ok {
		return nil
	}

	var existing []interface{}
	if raw, ok := lib["paths"].([]interface{}); ok {
		existing = raw
	}
	for _, p := range existing {
		if s, ok := p.(string); ok && s == pathEntry {
			return nil // already present
		}
	}
	lib["paths"] = append(existing, pathEntry)

	out, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", initJSONPath, err)
	}
	tmp := initJSONPath + ".tmp"
	if err := os.WriteFile(tmp, out, 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, initJSONPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", tmp, err)
	}
	return nil
}

// AgentsMissingInit returns the names (directory names under .lingtai/)
// of agents whose dir exists but whose init.json is missing. This is the
// signature of an imported network — the export flow strips each agent's
// init.json so the receiving user gets fresh provider/capability config
// rather than inheriting the exporter's.
//
// Skips the human/ pseudo-agent and any dot-prefixed entries. Only
// returns agents that have a .agent.json blueprint (identity) but no
// init.json (runtime config).
func AgentsMissingInit(projectRoot string) []string {
	if projectRoot == "" {
		return nil
	}
	lingtaiDir := filepath.Join(projectRoot, ".lingtai")
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return nil
	}
	var missing []string
	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || name == "" || name[0] == '.' || name == "human" {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, name)
		// Must have .agent.json (blueprint) AND be missing init.json to
		// qualify as an imported agent. A dir with neither is probably
		// mid-construction cruft and not our problem.
		if _, err := os.Stat(filepath.Join(agentDir, ".agent.json")); err != nil {
			continue
		}
		if _, err := os.Stat(filepath.Join(agentDir, "init.json")); err == nil {
			continue // init.json exists; nothing to do
		}
		missing = append(missing, name)
	}
	return missing
}

// --- internal helpers ---

// copyTree mirrors src to dst, creating dst if necessary. Skips symlinks
// (copies target content, not link) and preserves file content exactly.
// Does not remove files under dst that are absent from src — callers
// wanting wholesale replacement should RemoveAll(dst) first.
func copyTree(src, dst string) error {
	st, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return copyFile(src, dst, st.Mode())
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, e := range entries {
		srcPath := filepath.Join(src, e.Name())
		dstPath := filepath.Join(dst, e.Name())
		if e.IsDir() {
			if err := copyTree(srcPath, dstPath); err != nil {
				return err
			}
			continue
		}
		info, err := e.Info()
		if err != nil {
			return err
		}
		if err := copyFile(srcPath, dstPath, info.Mode()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	sf, err := os.Open(src)
	if err != nil {
		return err
	}
	defer sf.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	df, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, mode.Perm())
	if err != nil {
		return err
	}
	_, cerr := io.Copy(df, sf)
	closeErr := df.Close()
	if cerr != nil {
		return cerr
	}
	return closeErr
}

// treesEqual returns true iff the two directory trees contain the same
// set of files with the same byte content. Directory entries, mode bits,
// mtimes, and symlink attributes are not compared.
func treesEqual(a, b string) (bool, error) {
	aFiles, err := listFiles(a)
	if err != nil {
		return false, err
	}
	bFiles, err := listFiles(b)
	if err != nil {
		return false, err
	}
	if len(aFiles) != len(bFiles) {
		return false, nil
	}
	for rel := range aFiles {
		if _, ok := bFiles[rel]; !ok {
			return false, nil
		}
	}
	for rel := range aFiles {
		aData, err := os.ReadFile(filepath.Join(a, rel))
		if err != nil {
			return false, err
		}
		bData, err := os.ReadFile(filepath.Join(b, rel))
		if err != nil {
			return false, err
		}
		if string(aData) != string(bData) {
			return false, nil
		}
	}
	return true, nil
}

// listFiles returns the set of relative file paths under root (files
// only, skipping directories). Keys are forward-slash normalized so
// comparisons work cross-platform.
func listFiles(root string) (map[string]struct{}, error) {
	out := make(map[string]struct{})
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(rel)] = struct{}{}
		return nil
	})
	return out, err
}

