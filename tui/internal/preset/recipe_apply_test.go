package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// --- helpers ---

// makeBundleRoot builds a complete recipe bundle at a temp dir. Returns
// the bundle root path. Includes .recipe/recipe.json with the given id /
// library_name, a greet.md, and optionally a library sibling folder
// containing a SKILL.md marker.
func makeBundleRoot(t *testing.T, id string, libraryName *string) string {
	t.Helper()
	root := t.TempDir()

	// .recipe/recipe.json
	dotRecipe := filepath.Join(root, RecipeDotDir)
	if err := os.MkdirAll(dotRecipe, 0o755); err != nil {
		t.Fatalf("mkdir .recipe: %v", err)
	}
	var libField string
	if libraryName == nil {
		libField = `null`
	} else {
		libField = `"` + *libraryName + `"`
	}
	manifest := `{"id":"` + id + `","name":"Test Recipe","description":"d","library_name":` + libField + `}`
	if err := os.WriteFile(filepath.Join(dotRecipe, "recipe.json"), []byte(manifest), 0o644); err != nil {
		t.Fatalf("write recipe.json: %v", err)
	}

	// .recipe/greet/greet.md (minimum required behavioral layer)
	greetDir := filepath.Join(dotRecipe, "greet")
	os.MkdirAll(greetDir, 0o755)
	if err := os.WriteFile(filepath.Join(greetDir, "greet.md"), []byte("hello {{addr}}"), 0o644); err != nil {
		t.Fatalf("write greet.md: %v", err)
	}

	// library sibling (if declared)
	if libraryName != nil && *libraryName != "" {
		libDir := filepath.Join(root, *libraryName)
		os.MkdirAll(filepath.Join(libDir, "skill-a"), 0o755)
		os.WriteFile(filepath.Join(libDir, "SKILL.md"), []byte("# lib root\n"), 0o644)
		os.WriteFile(filepath.Join(libDir, "skill-a", "SKILL.md"), []byte("# skill a\n"), 0o644)
	}
	return root
}

// makeProjectWithAgents builds a fresh project root with a .lingtai/ dir
// containing the named agents, each with a minimal init.json that
// exercises the library.paths slot.
func makeProjectWithAgents(t *testing.T, agentNames ...string) string {
	t.Helper()
	root := t.TempDir()
	lingtai := filepath.Join(root, ".lingtai")
	os.MkdirAll(lingtai, 0o755)
	for _, name := range agentNames {
		agentDir := filepath.Join(lingtai, name)
		os.MkdirAll(agentDir, 0o755)
		init := map[string]interface{}{
			"manifest": map[string]interface{}{
				"capabilities": map[string]interface{}{
					"library": map[string]interface{}{
						"paths": []interface{}{"../.library_shared"},
					},
				},
			},
		}
		data, _ := json.MarshalIndent(init, "", "  ")
		os.WriteFile(filepath.Join(agentDir, "init.json"), data, 0o644)
	}
	return root
}

func libraryPaths(t *testing.T, initJSONPath string) []string {
	t.Helper()
	data, err := os.ReadFile(initJSONPath)
	if err != nil {
		t.Fatalf("read init.json: %v", err)
	}
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatalf("parse init.json: %v", err)
	}
	m := root["manifest"].(map[string]interface{})
	c := m["capabilities"].(map[string]interface{})
	l := c["library"].(map[string]interface{})
	var out []string
	if raw, ok := l["paths"].([]interface{}); ok {
		for _, p := range raw {
			if s, ok := p.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

// --- CopyBundle ---

func TestCopyBundle_NoLibrary(t *testing.T) {
	src := makeBundleRoot(t, "test", nil)
	dst := t.TempDir()
	if err := CopyBundle(src, dst); err != nil {
		t.Fatalf("CopyBundle: %v", err)
	}
	// .recipe/ arrived
	if _, err := os.Stat(filepath.Join(dst, RecipeDotDir, "recipe.json")); err != nil {
		t.Errorf(".recipe/recipe.json missing at dst: %v", err)
	}
	// no library folder
	if entries, _ := os.ReadDir(dst); len(entries) > 1 {
		// dst has only .recipe/
		for _, e := range entries {
			if e.Name() != RecipeDotDir {
				t.Errorf("unexpected dst entry %q (expected only .recipe/)", e.Name())
			}
		}
	}
}

func TestCopyBundle_WithLibrary(t *testing.T) {
	libName := "my-lib"
	src := makeBundleRoot(t, "test", &libName)
	dst := t.TempDir()
	if err := CopyBundle(src, dst); err != nil {
		t.Fatalf("CopyBundle: %v", err)
	}
	// library sibling at dst
	if _, err := os.Stat(filepath.Join(dst, libName, "SKILL.md")); err != nil {
		t.Errorf("library SKILL.md missing at dst: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dst, libName, "skill-a", "SKILL.md")); err != nil {
		t.Errorf("library skill-a/SKILL.md missing at dst: %v", err)
	}
}

func TestCopyBundle_ReplacesOldRecipe(t *testing.T) {
	src := makeBundleRoot(t, "newrecipe", nil)
	dst := t.TempDir()
	// pre-existing .recipe/ with stale file
	staleRecipe := filepath.Join(dst, RecipeDotDir)
	os.MkdirAll(staleRecipe, 0o755)
	os.WriteFile(filepath.Join(staleRecipe, "leftover.md"), []byte("old"), 0o644)

	if err := CopyBundle(src, dst); err != nil {
		t.Fatalf("CopyBundle: %v", err)
	}
	// stale file must be gone (recipe replace semantics)
	if _, err := os.Stat(filepath.Join(staleRecipe, "leftover.md")); !os.IsNotExist(err) {
		t.Errorf("leftover.md should be removed on recipe replace")
	}
}

func TestCopyBundle_InvalidSource(t *testing.T) {
	dst := t.TempDir()
	empty := t.TempDir()
	if err := CopyBundle(empty, dst); err == nil {
		t.Errorf("CopyBundle with no .recipe/ at source should error")
	}
}

// --- AppendLibraryPath ---

func TestAppendLibraryPath_NewEntry(t *testing.T) {
	proj := makeProjectWithAgents(t, "orch")
	initPath := filepath.Join(proj, ".lingtai", "orch", "init.json")
	if err := AppendLibraryPath(initPath, "../../velli"); err != nil {
		t.Fatalf("AppendLibraryPath: %v", err)
	}
	paths := libraryPaths(t, initPath)
	if len(paths) != 2 || paths[0] != "../.library_shared" || paths[1] != "../../velli" {
		t.Errorf("paths = %v, want [.library_shared, ../../velli]", paths)
	}
}

func TestAppendLibraryPath_Idempotent(t *testing.T) {
	proj := makeProjectWithAgents(t, "orch")
	initPath := filepath.Join(proj, ".lingtai", "orch", "init.json")
	AppendLibraryPath(initPath, "../../velli")
	AppendLibraryPath(initPath, "../../velli") // again
	AppendLibraryPath(initPath, "../../velli") // again
	paths := libraryPaths(t, initPath)
	// Count "../../velli" occurrences
	count := 0
	for _, p := range paths {
		if p == "../../velli" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("'../../velli' appears %d times, want 1", count)
	}
}

func TestAppendLibraryPath_MissingFile(t *testing.T) {
	// Missing init.json should not error.
	if err := AppendLibraryPath(filepath.Join(t.TempDir(), "nope.json"), "x"); err != nil {
		t.Errorf("AppendLibraryPath missing file = %v, want nil", err)
	}
}

func TestAppendLibraryPath_NoLibraryCap(t *testing.T) {
	// Agent missing library capability in manifest — no-op, no error.
	proj := t.TempDir()
	initPath := filepath.Join(proj, "init.json")
	os.WriteFile(initPath, []byte(`{"manifest":{"capabilities":{}}}`), 0o644)
	if err := AppendLibraryPath(initPath, "../../velli"); err != nil {
		t.Errorf("AppendLibraryPath without library cap = %v, want nil (no-op)", err)
	}
}

// --- RecipeNeedsApply ---

func TestRecipeNeedsApply_NoRecipe(t *testing.T) {
	proj := t.TempDir()
	if RecipeNeedsApply(proj) {
		t.Errorf("RecipeNeedsApply with no .recipe/ = true, want false")
	}
}

func TestRecipeNeedsApply_NoSnapshot(t *testing.T) {
	src := makeBundleRoot(t, "test", nil)
	proj := t.TempDir()
	CopyBundle(src, proj)
	os.MkdirAll(filepath.Join(proj, ".lingtai"), 0o755)
	if !RecipeNeedsApply(proj) {
		t.Errorf("RecipeNeedsApply with .recipe/ but no snapshot = false, want true")
	}
}

func TestRecipeNeedsApply_Identical(t *testing.T) {
	src := makeBundleRoot(t, "test", nil)
	proj := t.TempDir()
	CopyBundle(src, proj)
	// Fake the snapshot by copying .recipe/ into place manually.
	snapshot := filepath.Join(proj, ".lingtai", AppliedRecipeSubpath)
	os.MkdirAll(filepath.Dir(snapshot), 0o755)
	copyTree(filepath.Join(proj, RecipeDotDir), snapshot)

	if RecipeNeedsApply(proj) {
		t.Errorf("RecipeNeedsApply with identical trees = true, want false")
	}
}

func TestRecipeNeedsApply_Different(t *testing.T) {
	src := makeBundleRoot(t, "test", nil)
	proj := t.TempDir()
	CopyBundle(src, proj)
	snapshot := filepath.Join(proj, ".lingtai", AppliedRecipeSubpath)
	os.MkdirAll(filepath.Dir(snapshot), 0o755)
	copyTree(filepath.Join(proj, RecipeDotDir), snapshot)

	// Modify the current recipe's greet.md so it diverges from snapshot.
	if err := os.WriteFile(filepath.Join(proj, RecipeDotDir, "greet", "greet.md"), []byte("changed"), 0o644); err != nil {
		t.Fatalf("overwrite greet.md: %v", err)
	}
	if !RecipeNeedsApply(proj) {
		t.Errorf("RecipeNeedsApply after greet change = false, want true")
	}
}

// --- ApplyRecipe ---

func TestApplyRecipe_WritesPromptAndSnapshots(t *testing.T) {
	src := makeBundleRoot(t, "test", nil)
	proj := makeProjectWithAgents(t, "alpha", "beta")
	CopyBundle(src, proj)

	applied, err := ApplyRecipe(proj, "", func(tmpl string) string {
		return strings.ReplaceAll(tmpl, "{{addr}}", "human")
	})
	if err != nil {
		t.Fatalf("ApplyRecipe: %v", err)
	}
	if applied != 2 {
		t.Errorf("applied count = %d, want 2", applied)
	}

	// Each agent got .prompt with substitution applied.
	for _, name := range []string{"alpha", "beta"} {
		prompt, err := os.ReadFile(filepath.Join(proj, ".lingtai", name, ".prompt"))
		if err != nil {
			t.Errorf("agent %s: read .prompt: %v", name, err)
			continue
		}
		if string(prompt) != "hello human" {
			t.Errorf("agent %s: .prompt = %q, want %q", name, string(prompt), "hello human")
		}
	}

	// Snapshot exists and contains recipe.json.
	snapshot := filepath.Join(proj, ".lingtai", AppliedRecipeSubpath, "recipe.json")
	if _, err := os.Stat(snapshot); err != nil {
		t.Errorf("snapshot missing at %s: %v", snapshot, err)
	}

	// After apply, RecipeNeedsApply should report false.
	if RecipeNeedsApply(proj) {
		t.Errorf("RecipeNeedsApply after ApplyRecipe = true, want false")
	}
}

func TestApplyRecipe_AppendsLibraryPath(t *testing.T) {
	libName := "velli"
	src := makeBundleRoot(t, "test", &libName)
	proj := makeProjectWithAgents(t, "alpha", "beta")
	CopyBundle(src, proj)

	_, err := ApplyRecipe(proj, "", nil)
	if err != nil {
		t.Fatalf("ApplyRecipe: %v", err)
	}
	for _, name := range []string{"alpha", "beta"} {
		paths := libraryPaths(t, filepath.Join(proj, ".lingtai", name, "init.json"))
		found := false
		for _, p := range paths {
			if p == filepath.Join("..", "..", "velli") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("agent %s: library.paths missing ../../velli, got %v", name, paths)
		}
	}
}

func TestApplyRecipe_LibraryPathAdditive_AcrossChanges(t *testing.T) {
	// First recipe has library "old-lib"; apply → agent paths include ../../old-lib.
	oldLib := "old-lib"
	src1 := makeBundleRoot(t, "r1", &oldLib)
	proj := makeProjectWithAgents(t, "orch")
	CopyBundle(src1, proj)
	ApplyRecipe(proj, "", nil)

	// Now switch to a recipe with library "new-lib".
	newLib := "new-lib"
	src2 := makeBundleRoot(t, "r2", &newLib)
	CopyBundle(src2, proj)
	ApplyRecipe(proj, "", nil)

	paths := libraryPaths(t, filepath.Join(proj, ".lingtai", "orch", "init.json"))
	var seenOld, seenNew bool
	for _, p := range paths {
		if p == filepath.Join("..", "..", "old-lib") {
			seenOld = true
		}
		if p == filepath.Join("..", "..", "new-lib") {
			seenNew = true
		}
	}
	if !seenOld {
		t.Errorf("old library path should be retained across recipe change; got %v", paths)
	}
	if !seenNew {
		t.Errorf("new library path should be added; got %v", paths)
	}
}

func TestApplyRecipe_SkipsHumanDir(t *testing.T) {
	src := makeBundleRoot(t, "test", nil)
	proj := makeProjectWithAgents(t, "orch", "human")
	CopyBundle(src, proj)

	applied, err := ApplyRecipe(proj, "", nil)
	if err != nil {
		t.Fatalf("ApplyRecipe: %v", err)
	}
	// human/ is excluded; only orch counts.
	if applied != 1 {
		t.Errorf("applied = %d, want 1 (human/ skipped)", applied)
	}
	if _, err := os.Stat(filepath.Join(proj, ".lingtai", "human", ".prompt")); !os.IsNotExist(err) {
		t.Errorf("human/ should not receive .prompt")
	}
}

func TestApplyRecipe_NoRecipeInProject(t *testing.T) {
	proj := makeProjectWithAgents(t, "orch")
	// No .recipe/ copied in.
	_, err := ApplyRecipe(proj, "", nil)
	if err == nil {
		t.Errorf("ApplyRecipe without .recipe/ should error")
	}
}
