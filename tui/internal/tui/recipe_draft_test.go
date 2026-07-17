package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewDraftFirstRunModel_FreshHomeShowsEmbeddedRecipesWithoutWrites(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	globalDir := filepath.Join(home, ".lingtai-tui")
	baseDir := filepath.Join(t.TempDir(), ".lingtai")

	draft := NewProjectDraft(filepath.Dir(baseDir))
	m := NewDraftFirstRunModel(baseDir, globalDir, false, draft)
	initCmd := m.Init()
	if initCmd == nil {
		t.Fatal("draft Init returned no bootstrap completion command")
	}
	m, _ = m.Update(initCmd())

	wantIDs := []string{"adaptive", "greeter", "plain", "tutorial"}
	if got := len(m.discoveredRecipes); got != len(wantIDs) {
		t.Fatalf("embedded recipe count = %d, want %d (%v)", got, len(wantIDs), wantIDs)
	}
	for i, want := range wantIDs {
		if got := m.discoveredRecipes[i].ID; got != want {
			t.Errorf("embedded recipe %d ID = %q, want %q", i, got, want)
		}
		if !m.discoveredRecipes[i].Embedded || m.discoveredRecipes[i].Dir != "" {
			t.Errorf("embedded recipe %q source = embedded:%v dir:%q, want no disk path", want, m.discoveredRecipes[i].Embedded, m.discoveredRecipes[i].Dir)
		}
		if got := m.recipeIdxToName(i); got != want {
			t.Errorf("recipe picker index %d = %q, want %q", i, got, want)
		}
	}
	if got, want := m.categoryBoundaries, []int{0, 1, 3}; len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Fatalf("category boundaries = %v, want %v", got, want)
	}
	if got := m.recipeIdxToName(m.recipeMaxIdx()); got != "custom" {
		t.Fatalf("last recipe picker entry = %q, want custom", got)
	}

	view := m.viewRecipe()
	for _, want := range []string{"Adaptive", "Greeter", "Plain", "Tutorial", "Custom"} {
		if !strings.Contains(view, want) {
			t.Errorf("recipe view does not contain %q: %q", want, view)
		}
	}

	if entries, err := os.ReadDir(home); err != nil {
		t.Fatalf("read fresh home: %v", err)
	} else if len(entries) != 0 {
		t.Fatalf("fresh home gained entries before confirmation: %v", entries)
	}
	if _, err := os.Stat(globalDir); !os.IsNotExist(err) {
		t.Fatalf("global dir was written before confirmation: stat err = %v", err)
	}
}

func TestBuildEmbeddedRecipeEntriesUsesContentWithoutDiskPath(t *testing.T) {
	entries := buildEmbeddedRecipeEntries("adaptive", "en")
	if len(entries) == 0 {
		t.Fatal("embedded preview entries are empty")
	}
	for _, entry := range entries {
		if entry.Path != "" {
			t.Errorf("embedded preview entry %q fabricated path %q", entry.Label, entry.Path)
		}
		if entry.Content == "" {
			t.Errorf("embedded preview entry %q has no in-memory content", entry.Label)
		}
	}
}

func TestRunProjectCreate_AppliesEmbeddedRecipeWithoutGlobalBootstrap(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	draft.RecipeName = "adaptive"

	checkedBeforeRename := false
	opts.InjectFailure = func(phase CreatePhase) error {
		if phase != PhaseRename {
			return nil
		}
		checkedBeforeRename = true
		if _, err := os.Stat(opts.GlobalDir); !os.IsNotExist(err) {
			t.Fatalf("global dir changed before rename: %v", err)
		}
		entries, err := os.ReadDir(root)
		if err != nil {
			t.Fatal(err)
		}
		var staging string
		for _, entry := range entries {
			if entry.IsDir() && strings.HasPrefix(entry.Name(), ".lingtai.create-") {
				staging = filepath.Join(root, entry.Name())
				break
			}
		}
		if staging == "" {
			t.Fatal("embedded recipe staging directory missing before rename")
		}
		manifest, err := os.ReadFile(filepath.Join(root, ".recipe", "recipe.json"))
		if err != nil {
			t.Fatalf("project embedded recipe manifest missing: %v", err)
		}
		if !strings.Contains(string(manifest), `"id": "adaptive"`) {
			t.Fatalf("project embedded recipe manifest = %s", manifest)
		}
		if _, err := os.Stat(filepath.Join(staging, ".tui-asset", ".recipe", "recipe.json")); err != nil {
			t.Fatalf("staged embedded recipe snapshot missing: %v", err)
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
	if _, err := os.Stat(filepath.Join(root, ".recipe", "recipe.json")); err != nil {
		t.Fatalf("published embedded recipe manifest missing: %v", err)
	}
}
