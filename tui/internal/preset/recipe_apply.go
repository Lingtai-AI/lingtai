package preset

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/process"
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
	return materializeRecipeBundle(process.ProjectBundleRequest{Kind: "disk", Source: srcBundleRoot, Destination: projectRoot})
}

// CopyEmbeddedBundle materializes a compiled recipe into a project root after
// user confirmation. Discovery and preview stay in memory; RunProjectCreate
// then applies this project-root copy into its owned .lingtai staging tree
// before the atomic rename.
func CopyEmbeddedBundle(recipeName, projectRoot string) error {
	return materializeRecipeBundle(process.ProjectBundleRequest{Kind: "packaged", RecipeName: recipeName, Destination: projectRoot})
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
// The kernel comparison walks both trees and compares every file by content.
// Directories, mode bits, and mtimes are ignored — only the byte stream
// of each file matters. Extra files on either side make the recipes
// "different." Unknown transport cannot establish absence/equality; this
// bool-only API returns true so the subsequent apply can surface its error.
func RecipeNeedsApply(projectRoot string) bool {
	if projectRoot == "" {
		return false
	}
	python, err := recipePython()
	if err != nil {
		return true
	} // transport uncertainty is not equality/absence
	result, err := process.ObserveProjectRecipe(python, projectRoot)
	if err != nil {
		return true
	}
	return result.NeedsApply
}

// ApplyRecipe materializes the recipe currently in <projectRoot>/.recipe/
// across all agents under <projectRoot>/.lingtai/<agent>/.
//
// For each agent dir it:
//   - Writes .prompt from the recipe's greet template (with runtime
//     substitutions for {{time}}, {{addr}}, {{lang}}, {{location}},
//     {{commands}}). Greet substitution is delegated to the
//     caller via the greetSubstitutor callback so this package stays free
//     of TUI-only helpers.
//   - Updates init.json:
//   - strips ignored legacy prompt-file fields when this additive
//     skills-path rewrite serializes an existing document
//   - manifest.capabilities.skills.paths gets "../../<library_name>"
//     appended when the recipe declares a library (additive — existing
//     entries are preserved, never removed)
//   - Snapshots the applied recipe to .lingtai/.tui-asset/.recipe/ so a
//     future RecipeNeedsApply call can detect future changes.
//
// Returns the legacy processed count (including its first-error count quirk) and, on failure,
// the first error encountered. Partial success is possible — the caller
// must inspect partial state before explicitly deciding on another action.
//
// This function is additive for skill-catalog paths by design: on recipe
// change, we DO NOT remove prior recipes' skill-library entries. The user may
// have come to rely on that skill library, and auto-removal is the sort of
// thing that silently breaks agents. Manual cleanup is the user's
// responsibility.
func ApplyRecipe(projectRoot, lang string, greetSubstitutor func(template string) string) (applied int, err error) {
	if projectRoot == "" {
		return 0, fmt.Errorf("ApplyRecipe: empty projectRoot")
	}
	return ApplyRecipeToLingTaiDir(projectRoot, filepath.Join(projectRoot, ".lingtai"), lang, greetSubstitutor)
}

// ApplyRecipeToLingTaiDir materializes the recipe in bundleDir across agents
// in an explicit LingTai directory. Advanced project creation uses this while
// the future <project>/.lingtai tree still lives in a sibling staging directory;
// ordinary callers should use ApplyRecipe.
func ApplyRecipeToLingTaiDir(bundleDir, lingtaiDir, lang string, greetSubstitutor func(template string) string) (applied int, err error) {
	if bundleDir == "" {
		return 0, fmt.Errorf("ApplyRecipe: empty bundleDir")
	}
	if lingtaiDir == "" {
		return 0, fmt.Errorf("ApplyRecipe: empty lingtaiDir")
	}
	return applyKernelRecipe(bundleDir, lingtaiDir, lang, greetSubstitutor)
}

// AppendSkillsPath ensures the given path string appears in the
// manifest.capabilities.skills.paths list of the agent's init.json.
// Idempotent: no-op if the path is already present. Preserves all other
// fields in init.json including existing skills.paths entries.
//
// The skills capability object must already exist in the manifest (via
// skillsDefault() or equivalent). If it's absent, this function is a
// no-op — creating it would be an architectural choice best left to
// preset generation.
//
// Writes atomically via temp+rename on change. Returns nil on successful
// no-op or successful write; returns error only on I/O or parse failure.
func AppendSkillsPath(initJSONPath, pathEntry string) error {
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
	if err := DecodeJSONUseNumber(data, &root); err != nil {
		return fmt.Errorf("parse %s: %w", initJSONPath, err)
	}
	stripObsoleteInitFields(root)
	manifest, ok := root["manifest"].(map[string]interface{})
	if !ok {
		return nil
	}
	caps, ok := manifest["capabilities"].(map[string]interface{})
	if !ok {
		return nil
	}
	capsChanged, err := CanonicalizeCapabilities(caps)
	if err != nil {
		return fmt.Errorf("canonicalize %s: %w", initJSONPath, err)
	}
	skills, ok := caps["skills"].(map[string]interface{})
	if !ok {
		return nil
	}

	var existing []interface{}
	if raw, ok := skills["paths"].([]interface{}); ok {
		existing = raw
	}
	pathPresent := false
	for _, p := range existing {
		if s, ok := p.(string); ok && s == pathEntry {
			pathPresent = true
			break
		}
	}
	if !pathPresent {
		skills["paths"] = append(existing, pathEntry)
	} else if !capsChanged {
		return nil // path and capability aliases are already canonical
	}

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
