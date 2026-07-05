package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeRecipeBundle creates a minimal recipe bundle at dir with the given id,
// name, and optional library. When libraryName is non-empty, a library folder
// with one skill (containing a SKILL.md) is created so the pairing qualifies as
// a marketplace entry.
func writeRecipeBundle(t *testing.T, dir, id, name, libraryName string) {
	t.Helper()
	recipeDot := filepath.Join(dir, RecipeDotDir)
	if err := os.MkdirAll(recipeDot, 0o755); err != nil {
		t.Fatalf("mkdir .recipe: %v", err)
	}
	info := RecipeInfo{ID: id, Name: name, Description: "desc " + id, Version: "1.0.0"}
	if libraryName != "" {
		info.LibraryName = &libraryName
		skillDir := filepath.Join(dir, libraryName, "sample-skill")
		if err := os.MkdirAll(skillDir, 0o755); err != nil {
			t.Fatalf("mkdir library skill: %v", err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("# sample\n"), 0o644); err != nil {
			t.Fatalf("write SKILL.md: %v", err)
		}
	}
	data, _ := json.Marshal(info)
	if err := os.WriteFile(filepath.Join(recipeDot, "recipe.json"), data, 0o644); err != nil {
		t.Fatalf("write recipe.json: %v", err)
	}
}

func TestBuiltinMarketplaceEntriesAllHaveLibrary(t *testing.T) {
	entries := BuiltinMarketplaceEntries()
	if len(entries) == 0 {
		t.Fatal("BuiltinMarketplaceEntries() returned no entries")
	}
	for _, e := range entries {
		if e.LibraryName == "" {
			t.Errorf("entry %q has empty LibraryName — a marketplace entry must pair a recipe with an external skill library", e.ID)
		}
		if e.Recipe.LibraryName == nil || *e.Recipe.LibraryName == "" {
			t.Errorf("entry %q recipe.LibraryName must mirror the external skill library", e.ID)
		}
		if e.Origin != OriginCommunity {
			t.Errorf("entry %q origin = %q, want community (curated off-machine)", e.ID, e.Origin)
		}
		if e.Install != InstallManual {
			t.Errorf("entry %q install = %q, want manual (no runtime remote fetch in MVP)", e.ID, e.Install)
		}
		if e.Safety == "" {
			t.Errorf("entry %q missing Safety preview text", e.ID)
		}
	}
}

func TestBuiltinMarketplaceIncludesRoundtable(t *testing.T) {
	var found *MarketplaceEntry
	for i, e := range BuiltinMarketplaceEntries() {
		if e.ID == "roundtable-expert-panel" {
			found = &BuiltinMarketplaceEntries()[i]
			break
		}
	}
	if found == nil {
		t.Fatal("expected the Roundtable Expert Panel community example in the registry")
	}
	if found.Author != "rawpaper123" {
		t.Errorf("roundtable author = %q, want rawpaper123", found.Author)
	}
	if found.SourceURL == "" {
		t.Error("roundtable entry should carry a SourceURL")
	}
}

func TestScanLocalMarketplaceSkipsRecipesWithoutLibrary(t *testing.T) {
	// Isolate HOME so ScanAgoraRecipes doesn't pick up this machine's real
	// ~/lingtai-agora/recipes/ and pollute the assertion.
	t.Setenv("HOME", t.TempDir())
	globalDir := t.TempDir()
	catDir := filepath.Join(globalDir, "recipes", "recommended")
	// One recipe with a library (qualifies), one without (should be skipped).
	writeRecipeBundle(t, filepath.Join(catDir, "with-lib"), "with-lib", "With Library", "mylib")
	writeRecipeBundle(t, filepath.Join(catDir, "no-lib"), "no-lib", "Plain Recipe", "")

	entries := ScanLocalMarketplaceEntries(globalDir, "en")
	if len(entries) != 1 {
		t.Fatalf("expected exactly 1 local marketplace entry, got %d: %+v", len(entries), entries)
	}
	e := entries[0]
	if e.ID != "with-lib" {
		t.Fatalf("expected with-lib entry, got %q", e.ID)
	}
	if e.Origin != OriginLocal || e.Install != InstallReady {
		t.Errorf("local entry origin/install = %q/%q, want local/ready", e.Origin, e.Install)
	}
	if e.LibraryName != "mylib" {
		t.Errorf("library name = %q, want mylib", e.LibraryName)
	}
	if len(e.LibrarySkills) != 1 || e.LibrarySkills[0] != "sample-skill" {
		t.Errorf("library skills = %v, want [sample-skill]", e.LibrarySkills)
	}
	if e.BundleDir == "" {
		t.Error("local entry should carry a BundleDir")
	}
}

func TestMarketplaceEntriesLocalWinsOverCommunity(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	globalDir := t.TempDir()
	catDir := filepath.Join(globalDir, "recipes", "recommended")
	// Simulate the user having imported the roundtable pairing locally.
	writeRecipeBundle(t, filepath.Join(catDir, "roundtable-expert-panel"),
		"roundtable-expert-panel", "Roundtable Expert Panel", "roundtable")

	entries := MarketplaceEntries(globalDir, "en")

	var count int
	for _, e := range entries {
		if e.ID == "roundtable-expert-panel" {
			count++
			if e.Origin != OriginLocal {
				t.Errorf("imported roundtable should show as local, got %q", e.Origin)
			}
		}
	}
	if count != 1 {
		t.Fatalf("roundtable appears %d times, want exactly 1 (local form should shadow the community entry)", count)
	}
}

func TestMarketplaceEntriesEmptyMachineShowsCommunityOnly(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	globalDir := t.TempDir() // no local recipes
	entries := MarketplaceEntries(globalDir, "en")
	if len(entries) == 0 {
		t.Fatal("expected curated community entries even on an empty machine")
	}
	for _, e := range entries {
		if e.Origin != OriginCommunity {
			t.Errorf("entry %q origin = %q, want community on an empty machine", e.ID, e.Origin)
		}
	}
}
