package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/preset"
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

	m.recipeIdx = m.recipeNameToIdx("adaptive")
	m, _ = m.enterReviewStep("adaptive", "")
	if !draft.RecipeEmbedded {
		t.Fatal("reviewed embedded picker row did not preserve source provenance")
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
	draft.RecipeEmbedded = true

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

func TestRecipePreviewDoesNotSubstituteEmbeddedForMissingDiskRecipe(t *testing.T) {
	globalDir := filepath.Join(t.TempDir(), "global")
	if err := preset.Bootstrap(globalDir); err != nil {
		t.Fatalf("bootstrap disk recipes: %v", err)
	}
	baseDir := filepath.Join(t.TempDir(), ".lingtai")
	m := NewDraftFirstRunModel(baseDir, globalDir, false, NewProjectDraft(filepath.Dir(baseDir)))
	m.recipeIdx = m.recipeNameToIdx("adaptive")
	m.step = stepRecipe
	if m.recipeIdxIsEmbedded(m.recipeIdx) {
		t.Fatal("disk-backed adaptive row was mislabeled embedded")
	}

	diskDir := preset.RecipeDir(globalDir, "adaptive")
	if diskDir == "" {
		t.Fatal("bootstrapped adaptive recipe was not discovered on disk")
	}
	if err := os.Rename(diskDir, diskDir+".gone"); err != nil {
		t.Fatalf("hide disk recipe: %v", err)
	}
	if got := m.resolveCurrentRecipeDir(); got != "" {
		t.Fatalf("missing disk recipe still resolved to %q", got)
	}

	updated, cmd := m.Update(ctrlOKey(t))
	if cmd != nil {
		t.Fatalf("missing disk recipe preview returned command %v", cmd)
	}
	if updated.recipeViewer != nil {
		t.Fatal("missing disk recipe silently opened compiled preview")
	}
}

func TestRunProjectCreate_DoesNotSubstituteEmbeddedForMissingDiskRecipe(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	if err := preset.Bootstrap(opts.GlobalDir); err != nil {
		t.Fatalf("bootstrap disk recipes: %v", err)
	}
	draft.RecipeName = "adaptive"
	if draft.RecipeEmbedded {
		t.Fatal("disk-backed draft unexpectedly marked embedded")
	}

	diskDir := preset.RecipeDir(opts.GlobalDir, draft.RecipeName)
	if diskDir == "" {
		t.Fatal("bootstrapped adaptive recipe was not discovered on disk")
	}
	if err := os.Rename(diskDir, diskDir+".gone"); err != nil {
		t.Fatalf("hide disk recipe: %v", err)
	}

	res := RunProjectCreate(draft, opts)
	if res.Err == nil || res.Committed {
		t.Fatalf("missing disk recipe create = committed %v err %v, want failure", res.Committed, res.Err)
	}
	if res.FailedPhase != PhaseApplyRecipe {
		t.Fatalf("missing disk recipe failed in phase %v, want %v", res.FailedPhase, PhaseApplyRecipe)
	}
	if !strings.Contains(res.Err.Error(), `could not resolve source bundle for "adaptive"`) {
		t.Fatalf("missing disk recipe error = %q", res.Err)
	}
	if _, err := os.Stat(filepath.Join(root, ".lingtai")); !os.IsNotExist(err) {
		t.Fatalf("failed create published .lingtai: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, ".recipe")); !os.IsNotExist(err) {
		t.Fatalf("failed create wrote project recipe: %v", err)
	}
}

func TestRunProjectCreate_EmbeddedProvenanceIgnoresLaterDiskRecipe(t *testing.T) {
	draft, root := newTestDraft(t)
	opts := testCreateOptions(t, root)
	if err := preset.Bootstrap(opts.GlobalDir); err != nil {
		t.Fatalf("bootstrap disk recipes: %v", err)
	}
	diskDir := preset.RecipeDir(opts.GlobalDir, "adaptive")
	if diskDir == "" {
		t.Fatal("bootstrapped adaptive recipe was not discovered on disk")
	}
	shadow := []byte(`{"id":"disk-shadow","name":"Disk Shadow"}`)
	if err := os.WriteFile(filepath.Join(diskDir, ".recipe", "recipe.json"), shadow, 0o644); err != nil {
		t.Fatalf("replace later disk recipe: %v", err)
	}

	draft.RecipeName = "adaptive"
	draft.RecipeEmbedded = true
	res := RunProjectCreate(draft, opts)
	if res.Err != nil || !res.Committed {
		t.Fatalf("embedded create = committed %v err %v", res.Committed, res.Err)
	}
	got, err := os.ReadFile(filepath.Join(root, ".recipe", "recipe.json"))
	if err != nil {
		t.Fatalf("read published recipe: %v", err)
	}
	want, err := preset.ReadEmbeddedRecipeFile("adaptive", ".recipe/recipe.json")
	if err != nil {
		t.Fatalf("read compiled adaptive recipe: %v", err)
	}
	if string(got) != string(want) {
		t.Fatalf("published recipe came from later disk source: got %q want compiled %q", got, want)
	}
}
