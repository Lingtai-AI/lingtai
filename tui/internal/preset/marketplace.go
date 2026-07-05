package preset

import (
	"os"
	"path/filepath"
	"sort"
)

// The recipe marketplace.
//
// Product definition (Jason, Telegram #4224/#4225): a marketplace is NOT a
// generic recipe store and NOT a live GitHub search. A marketplace *entry* is
// a curated combination of TWO things:
//
//  1. a RECIPE (the .recipe/ behavioral bundle — greet/comment/covenant/
//     procedures), and
//  2. an EXTERNAL SKILL / LIBRARY source (the sibling library folder a recipe
//     ships via recipe.json#library_name, sourced from some community author).
//
// A recipe that carries no library is a plain recipe, not a marketplace entry.
// The presence of an external skill library is what makes the pairing a
// "marketplace" item. This maps onto the existing recipe model precisely:
// RecipeInfo.LibraryName != nil is the machine-level signal that a recipe is an
// external-skill + recipe combination.
//
// For the MVP the registry is a CURATED, STATIC list (see
// BuiltinMarketplaceEntries) plus a scan of already-present local recipes that
// happen to carry a library (see ScanLocalMarketplaceEntries). We do NOT fetch
// remote content at runtime — install/import is a manual, human-in-the-loop
// step (see InstallHintManual). The struct carries an Install field so future
// validated-import support drops in without reshaping callers.

// MarketplaceOrigin classifies where a marketplace entry comes from, so the UI
// can visibly separate trusted local/built-in pairings from community ones that
// require preview + manual import before use.
type MarketplaceOrigin string

const (
	// OriginLocal is a recipe+library pairing already present on this machine
	// (a bundled recipe or an agora recipe under ~/lingtai-agora/recipes/ that
	// declares a library). These are directly selectable from /setup.
	OriginLocal MarketplaceOrigin = "local"
	// OriginCommunity is a curated third-party pairing whose bundle is NOT on
	// this machine yet. It is browse/preview-only in the MVP; using it requires
	// the human to import a reviewed recipe bundle manually under
	// ~/lingtai-agora/recipes/ and validate it before it becomes selectable.
	OriginCommunity MarketplaceOrigin = "community"
)

// InstallState describes whether a marketplace entry can be used right now.
type InstallState string

const (
	// InstallReady means the recipe+library bundle is present locally and can
	// be selected in /setup like any other recipe.
	InstallReady InstallState = "ready"
	// InstallManual means the bundle is not present; the human must import it
	// manually. Automated remote install is intentionally not wired in the MVP.
	InstallManual InstallState = "manual"
)

// MarketplaceEntry is a single external-skill + recipe combination in the
// marketplace. It is deliberately a superset of RecipeInfo: the recipe half is
// captured by Recipe, and the external-skill half by the Library* fields.
type MarketplaceEntry struct {
	// ID is a stable machine identifier for the pairing (usually the recipe id).
	ID string
	// Origin separates trusted local pairings from community ones.
	Origin MarketplaceOrigin
	// Install tells the UI whether the entry is usable now or import-only.
	Install InstallState

	// Recipe is the recipe half of the pairing (name, description, version,
	// library_name). For local entries it is loaded from the bundle's
	// .recipe/recipe.json; for curated community entries it is authored inline.
	Recipe RecipeInfo

	// LibraryName is the external skill library the recipe pairs with. Mirrors
	// Recipe.LibraryName for convenience; a marketplace entry always has one
	// (that is what makes it a marketplace entry rather than a plain recipe).
	LibraryName string
	// LibrarySkills lists the skill folder names shipped by the library, when
	// they can be discovered on disk (local entries) or are documented by the
	// curator (community entries). Informational; may be empty.
	LibrarySkills []string

	// Author is the human/org behind the pairing (display string).
	Author string
	// SourceURL points at where the bundle lives (repo/homepage). Shown in the
	// detail view; NOT fetched at runtime.
	SourceURL string
	// Tags are freeform classification labels.
	Tags []string

	// Safety is human-facing preview/validation text: what the pairing does,
	// what skills it grants the agent, and what to check before importing. The
	// UI shows this on the detail page and treats it as the preview boundary
	// gate — community entries are never one-keystroke installable.
	Safety string

	// BundleDir is the absolute path to the local bundle when Install==ready,
	// or "" for community entries not yet present on this machine.
	BundleDir string
}

// InstallHintManual is the canonical instruction shown for community entries
// that are not yet present locally. Kept as a single constant so the UI and any
// future importer agree on the manual-import contract.
const InstallHintManual = "manual-import"

// BuiltinMarketplaceEntries returns the curated, static list of community
// external-skill + recipe combinations. This is DATA, not a live registry —
// nothing here is fetched at runtime. Each entry documents an off-machine
// bundle the human can choose to import manually.
//
// The list is intentionally small for the MVP; it exists to (a) give the
// marketplace surface real content, (b) demonstrate the external-skill + recipe
// framing with a concrete community example, and (c) fix the schema every
// future curated entry must fill in.
func BuiltinMarketplaceEntries() []MarketplaceEntry {
	roundtable := "roundtable"
	return []MarketplaceEntry{
		{
			ID:      "roundtable-expert-panel",
			Origin:  OriginCommunity,
			Install: InstallManual,
			Recipe: RecipeInfo{
				ID:          "roundtable-expert-panel",
				Name:        "Roundtable Expert Panel",
				Description: "Convene a panel of expert avatars that debate a question and converge on a recommendation.",
				Version:     "1.0.0",
				LibraryName: &roundtable,
			},
			LibraryName:   roundtable,
			LibrarySkills: []string{"roundtable"},
			Author:        "rawpaper123",
			SourceURL:     "https://github.com/rawpaper123/Roundtable-skill",
			Tags:          []string{"community", "panel", "deliberation", "external-skill"},
			Safety: "Community pairing. The `roundtable` skill instructs the orchestrator to " +
				"spawn multiple expert avatars and run a structured debate before answering. " +
				"Review the recipe's comment.md and the skill's SKILL.md before importing — it " +
				"changes how the orchestrator delegates and can spawn several avatars. Not " +
				"fetched automatically: use the source as a reference, import only a reviewed " +
				"recipe bundle into ~/lingtai-agora/recipes/, validate it, then it appears " +
				"as a local entry selectable in /setup.",
		},
	}
}

// ScanLocalMarketplaceEntries surfaces recipes already present on this machine
// that qualify as marketplace entries — i.e. they declare a library
// (external skill) via recipe.json#library_name. Two sources are scanned:
//
//   - agora recipes under ~/lingtai-agora/recipes/ (community bundles the human
//     already imported), and
//   - bundled category recipes under <globalDir>/recipes/<category>/.
//
// Recipes without a library are skipped — they are plain recipes, not
// external-skill + recipe combinations, so they do not belong in the
// marketplace by Jason's definition.
//
// The lang parameter is accepted for API symmetry with the recipe scanners but
// is not used to filter (recipe.json is never localized).
func ScanLocalMarketplaceEntries(globalDir, lang string) []MarketplaceEntry {
	var out []MarketplaceEntry

	add := func(dir string, info RecipeInfo) {
		if info.LibraryName == nil || *info.LibraryName == "" {
			return // plain recipe, not a marketplace entry
		}
		out = append(out, MarketplaceEntry{
			ID:            info.ID,
			Origin:        OriginLocal,
			Install:       InstallReady,
			Recipe:        info,
			LibraryName:   *info.LibraryName,
			LibrarySkills: scanLibrarySkillNames(dir, *info.LibraryName),
			SourceURL:     "",
			Tags:          []string{"local"},
			Safety: "Local pairing — the recipe and its `" + *info.LibraryName + "` skill " +
				"library are already on this machine and can be selected in /setup. Applying " +
				"it registers the library into every agent's skills.paths.",
			BundleDir: dir,
		})
	}

	for _, ar := range ScanAgoraRecipes(lang) {
		add(ar.Dir, ar.Info)
	}
	for _, cat := range RecipeCategories {
		for _, dr := range ScanCategory(globalDir, cat, lang) {
			add(dr.Dir, dr.Info)
		}
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Recipe.Name != out[j].Recipe.Name {
			return out[i].Recipe.Name < out[j].Recipe.Name
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// MarketplaceEntries returns the full marketplace listing: locally-present
// external-skill + recipe pairings first (Origin==local), then the curated
// community pairings that are not yet present (Origin==community), with any
// community entry that turns out to already exist locally (matched by recipe id)
// dropped in favor of its local, selectable form.
func MarketplaceEntries(globalDir, lang string) []MarketplaceEntry {
	local := ScanLocalMarketplaceEntries(globalDir, lang)
	present := make(map[string]bool, len(local))
	for _, e := range local {
		present[e.ID] = true
	}
	entries := make([]MarketplaceEntry, 0, len(local)+2)
	entries = append(entries, local...)
	for _, c := range BuiltinMarketplaceEntries() {
		if present[c.ID] {
			continue // already imported locally — the local entry wins
		}
		entries = append(entries, c)
	}
	return entries
}

// scanLibrarySkillNames lists the immediate subdirectories of a recipe's
// library folder that contain a SKILL.md — i.e. the skills the library ships.
// Returns nil when the library folder is absent or empty. Best-effort and
// non-fatal: a broken/missing library just yields no names.
func scanLibrarySkillNames(bundleDir, libraryName string) []string {
	if bundleDir == "" || libraryName == "" {
		return nil
	}
	libDir := filepath.Join(bundleDir, libraryName)
	entries, err := os.ReadDir(libDir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || e.Name() == "" || e.Name()[0] == '.' {
			continue
		}
		if st, err := os.Stat(filepath.Join(libDir, e.Name(), "SKILL.md")); err == nil && !st.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names
}
