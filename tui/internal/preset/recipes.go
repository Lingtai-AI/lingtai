package preset

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Recipe source types. A recipe is a bundle containing a .recipe/ dotfolder
// with a recipe.json manifest plus optional greet/comment/covenant/procedures
// behavioral layers, and optionally a sibling library folder (framework-agnostic
// skills pointed to by recipe.json's library_name field).
//
// Not to be confused with preset (LLM/capabilities template).
const (
	RecipeCustom   = "custom"
	RecipeImported = "imported"
	RecipeAgora    = "agora" // from ~/lingtai-agora/recipes/
)

// RecipeDotDir is the dotfolder name inside a recipe bundle that holds the
// LingTai-facing behavioral layer (recipe.json, greet/, comment/, covenant/,
// procedures/). Its presence is how the TUI recognizes a bundle as a recipe.
const RecipeDotDir = ".recipe"

// RecipeInfo holds the metadata from a recipe's recipe.json manifest.
//
//   - ID: machine identifier (stable across locales), usually matches the
//     recipe bundle's directory name. Used for dedup and reference.
//   - Name: display name (localized per active language via the file-level
//     fallback — zh/recipe.json → recipe.json).
//   - Description: display description (same localization rules as Name).
//   - Version: recipe version string (semver-ish). Optional; defaults to
//     "1.0.0" when absent from the manifest.
//   - LibraryName: literal folder name of the sibling library that ships
//     with this recipe (relative to the bundle root — same level as the
//     .recipe/ dotfolder). The TUI registers this library into every
//     agent's init.json at recipe-apply time by writing the relative path
//     "../../<library_name>" into init.json#library.paths (the ../../
//     climbs out of .lingtai/<agent>/ to the project root). Nil means the
//     recipe has no library sibling.
//
// Recipe bundles live inside the project root by convention, so the
// relative path "../../<library_name>" is always correct regardless of
// where the project itself is located on disk.
type RecipeInfo struct {
	ID          string  `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Version     string  `json:"version,omitempty"`
	LibraryName *string `json:"library_name"`
}

// AgoraRecipe holds a discovered recipe from ~/lingtai-agora/recipes/.
type AgoraRecipe struct {
	Info RecipeInfo // from .recipe/recipe.json
	Dir  string     // absolute path to the recipe bundle directory
}

// DiscoveredRecipe holds a recipe found by scanning a category directory.
type DiscoveredRecipe struct {
	ID   string     // bundle directory name
	Info RecipeInfo // from .recipe/recipe.json
	Dir  string     // absolute path to the recipe bundle directory
}

// RecipeCategories defines the display order of built-in recipe categories.
var RecipeCategories = []string{"recommended", "intrinsic", "examples"}

// ScanAgoraRecipes returns all valid recipes found under ~/lingtai-agora/recipes/.
// Each subdirectory must contain a .recipe/recipe.json with a non-empty name.
// Returns nil if directory doesn't exist or is empty.
//
// Unlike the previous model, recipes are no longer filtered by directory-name
// language suffix. A single recipe bundle carries all locale variants inside
// its .recipe/ dotfolder (e.g. .recipe/greet/zh/greet.md).
func ScanAgoraRecipes(lang string) []AgoraRecipe {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	recipesDir := filepath.Join(home, "lingtai-agora", "recipes")
	entries, err := os.ReadDir(recipesDir)
	if err != nil {
		return nil
	}
	var recipes []AgoraRecipe
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "" || e.Name()[0] == '.' {
			continue
		}
		dir := filepath.Join(recipesDir, e.Name())
		info, err := LoadRecipeInfo(dir, lang)
		if err != nil {
			continue // skip dirs without valid .recipe/recipe.json
		}
		recipes = append(recipes, AgoraRecipe{Info: info, Dir: dir})
	}
	sort.Slice(recipes, func(i, j int) bool {
		return recipes[i].Info.ID < recipes[j].Info.ID
	})
	return recipes
}

// ScanCategory returns all valid recipes found under <globalDir>/recipes/<category>/.
// Each subdirectory must contain a .recipe/recipe.json with a non-empty name.
// Results are sorted alphabetically by bundle directory name.
//
// Locale filtering no longer happens at scan time — all recipes are returned
// regardless of language. Locale variants are resolved per-file inside the
// recipe bundle when displayed or applied.
func ScanCategory(globalDir, category, lang string) []DiscoveredRecipe {
	catDir := filepath.Join(globalDir, "recipes", category)
	entries, err := os.ReadDir(catDir)
	if err != nil {
		return nil
	}
	var recipes []DiscoveredRecipe
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "" || e.Name()[0] == '.' {
			continue
		}
		dir := filepath.Join(catDir, e.Name())
		info, err := LoadRecipeInfo(dir, lang)
		if err != nil {
			continue
		}
		recipes = append(recipes, DiscoveredRecipe{ID: e.Name(), Info: info, Dir: dir})
	}
	sort.Slice(recipes, func(i, j int) bool {
		return recipes[i].ID < recipes[j].ID
	})
	return recipes
}

// LoadRecipeInfo reads .recipe/recipe.json from a recipe bundle directory,
// resolved via the standard i18n fallback (<lang>/recipe.json → recipe.json).
// Returns an error if the file is not found, unparseable, or has an empty name.
//
// Defaults applied on load: Version -> "1.0.0" if absent.
func LoadRecipeInfo(bundleDir, lang string) (RecipeInfo, error) {
	if bundleDir == "" {
		return RecipeInfo{}, fmt.Errorf("empty recipe bundle directory")
	}
	path := resolveRecipeDotFile(bundleDir, lang, "recipe.json")
	if path == "" {
		return RecipeInfo{}, fmt.Errorf(".recipe/recipe.json not found in %s", bundleDir)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return RecipeInfo{}, fmt.Errorf("read recipe.json: %w", err)
	}
	var info RecipeInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return RecipeInfo{}, fmt.Errorf("parse recipe.json: %w", err)
	}
	if info.Name == "" {
		return RecipeInfo{}, fmt.Errorf("recipe.json has empty name in %s", bundleDir)
	}
	if info.Version == "" {
		info.Version = "1.0.0"
	}
	return info, nil
}

// RecipeDir returns the absolute directory for a discovered recipe by searching
// all category subdirectories under <globalDir>/recipes/. Returns empty string
// if the recipe is not found. Matches by bundle directory name.
func RecipeDir(globalDir, name string) string {
	recipesRoot := filepath.Join(globalDir, "recipes")
	entries, err := os.ReadDir(recipesRoot)
	if err != nil {
		return ""
	}
	for _, cat := range entries {
		if !cat.IsDir() || cat.Name()[0] == '.' {
			continue
		}
		candidate := filepath.Join(recipesRoot, cat.Name(), name)
		if info, err := os.Stat(candidate); err == nil && info.IsDir() {
			return candidate
		}
	}
	return ""
}

// ResolveGreetPath returns the absolute path to the greet file for a recipe
// bundle directory and language, applying the per-lang fallback rule:
//  1. <bundleDir>/.recipe/greet/<lang>/greet.md
//  2. <bundleDir>/.recipe/greet/greet.md
//  3. empty string (no greet)
//
// bundleDir can be either a bundled recipe directory (from RecipeDir) or a
// user-supplied custom directory. The rule is identical for both.
func ResolveGreetPath(bundleDir, lang string) string {
	return resolveRecipeBehavioralFile(bundleDir, lang, "greet")
}

// ResolveCommentPath returns the absolute path to the comment file for a recipe
// bundle directory and language, using the same fallback rule as ResolveGreetPath.
// Returns empty string if the recipe does not provide a comment.
func ResolveCommentPath(bundleDir, lang string) string {
	return resolveRecipeBehavioralFile(bundleDir, lang, "comment")
}

// ResolveCovenantPath returns the absolute path to the covenant file for a
// recipe bundle directory and language, using the same fallback rule as
// ResolveGreetPath. Returns empty string if the recipe does not provide a
// covenant override.
func ResolveCovenantPath(bundleDir, lang string) string {
	return resolveRecipeBehavioralFile(bundleDir, lang, "covenant")
}

// ResolveProceduresPath returns the absolute path to the procedures file for a
// recipe bundle directory and language, using the same fallback rule as
// ResolveGreetPath. Returns empty string if the recipe does not provide a
// procedures override.
func ResolveProceduresPath(bundleDir, lang string) string {
	return resolveRecipeBehavioralFile(bundleDir, lang, "procedures")
}

// ResolveLibraryDir returns the absolute path to the sibling library folder
// for a recipe bundle, or empty string if the recipe has no library (its
// recipe.json library_name field is nil or missing). The returned path
// points at the library root; callers walk into it to find individual
// skills.
//
// Returns empty string also when library_name is set but the corresponding
// directory doesn't exist on disk — i.e. a broken bundle. Callers can
// distinguish "recipe has no library" (info.LibraryName == nil) from
// "library is missing" (info.LibraryName != nil but this function returns
// "") by loading the info directly when they need to know.
func ResolveLibraryDir(bundleDir, lang string) string {
	if bundleDir == "" {
		return ""
	}
	info, err := LoadRecipeInfo(bundleDir, lang)
	if err != nil || info.LibraryName == nil || *info.LibraryName == "" {
		return ""
	}
	libPath := filepath.Join(bundleDir, *info.LibraryName)
	st, err := os.Stat(libPath)
	if err != nil || !st.IsDir() {
		return ""
	}
	return libPath
}

// LibraryPathForInitJSON returns the relative path string that should be
// appended to an agent's init.json#library.paths to register this recipe's
// library. Returns empty string if the recipe has no library.
//
// The path is "../../<library_name>" — the agent's init.json lives at
// <project>/.lingtai/<agent>/init.json, and the library lives at
// <project>/<library_name>/. Climbing "../../" from the agent dir lands at
// the project root; "<library_name>" then resolves the library folder.
//
// This string is computed from the manifest alone — it does NOT verify the
// library actually exists on disk. Callers that need existence verification
// should call ResolveLibraryDir in addition.
func LibraryPathForInitJSON(bundleDir, lang string) string {
	if bundleDir == "" {
		return ""
	}
	info, err := LoadRecipeInfo(bundleDir, lang)
	if err != nil || info.LibraryName == nil || *info.LibraryName == "" {
		return ""
	}
	return filepath.Join("..", "..", *info.LibraryName)
}

// langFallbackChain returns the ordered list of languages to try for a given
// lang. The rule is simple: try <lang> first, then root. Root is mandatory
// and serves as the universal fallback for all languages.
func langFallbackChain(lang string) []string {
	if lang == "" {
		return []string{""}
	}
	return []string{lang, ""}
}

// resolveRecipeDotFile resolves a file directly under .recipe/ with the
// standard lang fallback. Used for recipe.json.
func resolveRecipeDotFile(bundleDir, lang, filename string) string {
	if bundleDir == "" {
		return ""
	}
	recipeDot := filepath.Join(bundleDir, RecipeDotDir)
	for _, l := range langFallbackChain(lang) {
		var path string
		if l == "" {
			path = filepath.Join(recipeDot, filename)
		} else {
			path = filepath.Join(recipeDot, l, filename)
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// resolveRecipeBehavioralFile resolves one of the behavioral-layer files
// (greet, comment, covenant, procedures) inside the .recipe/<layer>/
// subfolder, with the standard lang fallback.
//
// Layout:
//
//	<bundleDir>/.recipe/<layer>/<lang>/<layer>.md  (localized variant)
//	<bundleDir>/.recipe/<layer>/<layer>.md         (default / root file)
//
// The <layer> name appears both as subfolder and as filename (e.g. "greet/greet.md")
// so that translators can drop a single file per locale with an obvious name.
func resolveRecipeBehavioralFile(bundleDir, lang, layer string) string {
	if bundleDir == "" {
		return ""
	}
	layerDir := filepath.Join(bundleDir, RecipeDotDir, layer)
	filename := layer + ".md"
	for _, l := range langFallbackChain(lang) {
		var path string
		if l == "" {
			path = filepath.Join(layerDir, filename)
		} else {
			path = filepath.Join(layerDir, l, filename)
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path
		}
	}
	return ""
}

// ResolveSkillDir is retained from the legacy recipe layout where skills
// could live at <recipeDir>/skills/<skillName>/<lang>/SKILL.md with root
// fallback. It still works for any library that happens to follow that
// shape, but the new export-skill flow produces monolingual libraries (one
// SKILL.md per skill, no per-lang subdirs) so new bundles won't hit the
// fallback. Left intact for tolerance of legacy content.
//
// Fallback chain:
//
//	<recipeDir>/skills/<skillName>/<lang>/SKILL.md → that dir
//	<recipeDir>/skills/<skillName>/SKILL.md → that dir
//	empty string (no match)
func ResolveSkillDir(recipeDir, skillName, lang string) string {
	if recipeDir == "" {
		return ""
	}
	base := filepath.Join(recipeDir, "skills", skillName)
	for _, l := range langFallbackChain(lang) {
		var dir string
		if l == "" {
			dir = base
		} else {
			dir = filepath.Join(base, l)
		}
		if info, err := os.Stat(filepath.Join(dir, "SKILL.md")); err == nil && !info.IsDir() {
			return dir
		}
	}
	return ""
}

// ValidateCustomDir checks that a user-supplied custom recipe bundle exists,
// is a directory, and contains a valid .recipe/recipe.json. Returns a
// human-readable error on failure.
//
// Empty or malformed bundles are rejected — a recipe must at minimum declare
// its identity via .recipe/recipe.json.
func ValidateCustomDir(dir string) error {
	if dir == "" {
		return fmt.Errorf("custom recipe folder path is empty")
	}
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("custom recipe folder does not exist: %q", dir)
		}
		return fmt.Errorf("cannot access custom recipe folder: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("custom recipe path is not a directory: %q", dir)
	}
	// Require a valid .recipe/recipe.json for identity.
	if _, err := LoadRecipeInfo(dir, ""); err != nil {
		return fmt.Errorf("custom recipe folder is missing a valid .recipe/recipe.json: %w", err)
	}
	return nil
}

// ProjectLocalRecipeDir returns the recipe bundle root when the project ships
// its own recipe bundle (i.e. <projectRoot>/.recipe/ exists as a directory).
// The returned path is <projectRoot> itself — the bundle root. Returns empty
// string if no local recipe bundle is present.
//
// Used to pre-fill the custom path input in /setup when the project dir
// doubles as the recipe bundle root.
func ProjectLocalRecipeDir(projectRoot string) string {
	if projectRoot == "" {
		return ""
	}
	dotRecipe := filepath.Join(projectRoot, RecipeDotDir)
	info, err := os.Stat(dotRecipe)
	if err != nil || !info.IsDir() {
		return ""
	}
	return projectRoot
}
