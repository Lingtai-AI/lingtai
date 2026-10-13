package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// i18nPrefix returns the static text of a "…: %s"-style i18n template up to
// (but excluding) the first %-verb, so callers can substring-match a rendered
// message regardless of the interpolated tail or the active locale.
func i18nPrefix(key string) string {
	tmpl := i18n.T(key)
	if i := strings.IndexByte(tmpl, '%'); i >= 0 {
		tmpl = tmpl[:i]
	}
	return strings.TrimSpace(tmpl)
}

// writeRecipeFile writes p with the given contents, creating parent dirs.
func writeRecipeFile(t *testing.T, p, contents string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(p), err)
	}
	if err := os.WriteFile(p, []byte(contents), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

// newFirstRunRecipeProject builds a project laid out with production
// semantics: <root>/.recipe/ is the recipe bundle and <root>/.lingtai/ is
// the directory the TUI treats as App.projectDir (main.go passes lingtaiDir
// to NewApp). It seeds one agent under .lingtai/ with a minimal init.json.
// recipeName controls recipe.json's name field — pass "" to make ApplyRecipe
// fail (LoadRecipeInfo rejects an empty name), or a real name to make it
// succeed. Returns the .lingtai directory to assign to App.projectDir.
func newFirstRunRecipeProject(t *testing.T, recipeName string) string {
	t.Helper()
	// Transport fixture only: preserve ALL original first-run assertions while
	// selecting the parent's explicit installed kernel through the real resolver.
	python := os.Getenv("LINGTAI_PROJECT_CONFIGURATION_TEST_PYTHON")
	if python == "" || !filepath.IsAbs(python) {
		t.Fatal("set LINGTAI_PROJECT_CONFIGURATION_TEST_PYTHON to parent-verified installed interpreter")
	}
	if _, err := os.Stat(python); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	venv := filepath.Join(home, config.GlobalDirName, "runtime", "venv")
	if err := os.MkdirAll(filepath.Dir(venv), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Dir(filepath.Dir(python)), venv); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()

	// .recipe/recipe.json at the PARENT root, not under .lingtai.
	writeRecipeFile(t, filepath.Join(root, ".recipe", "recipe.json"),
		`{"id":"r","name":"`+recipeName+`","description":"d","library_name":null}`)
	// Minimum behavioral layer so a valid recipe has something to apply.
	writeRecipeFile(t, filepath.Join(root, ".recipe", "greet", "greet.md"), "hello {{addr}}")

	lingtaiDir := filepath.Join(root, ".lingtai")
	// One agent so ApplyRecipe has a target and reports applied >= 1.
	agentDir := filepath.Join(lingtaiDir, "manager")
	envFile := filepath.Join(root, "fixture.env")
	writeRecipeFile(t, envFile, "")
	init := map[string]interface{}{
		"env_file": envFile,
		"manifest": map[string]interface{}{
			"agent_name": "manager",
			"llm":        map[string]interface{}{"provider": "codex", "model": "fixture-model"},
			"capabilities": map[string]interface{}{
				"skills": map[string]interface{}{"paths": []interface{}{}},
			},
		},
	}
	data, _ := json.MarshalIndent(init, "", "  ")
	writeRecipeFile(t, filepath.Join(agentDir, "init.json"), string(data))

	return lingtaiDir
}

// TestFirstRunDoneDetectsRecipeAtParentRoot proves the first-run launch path
// resolves the recipe bundle from filepath.Dir(App.projectDir) — the parent
// project root — rather than from App.projectDir/.recipe (which would be
// .lingtai/.recipe and never exists in production). A valid recipe should be
// applied and its snapshot materialized under .lingtai/.tui-asset/.recipe/.
func TestFirstRunDoneDetectsRecipeAtParentRoot(t *testing.T) {
	lingtaiDir := newFirstRunRecipeProject(t, "Valid Recipe")
	projectRoot := filepath.Dir(lingtaiDir)

	// Sanity: the canonical resolver sees the recipe at the parent root and
	// not under .lingtai/.recipe (the buggy path).
	if !preset.RecipeNeedsApply(projectRoot) {
		t.Fatal("precondition: recipe at parent root should need apply")
	}
	if preset.RecipeNeedsApply(lingtaiDir) {
		t.Fatal("precondition: no recipe should be found under .lingtai/.recipe")
	}

	a := App{projectDir: lingtaiDir, globalDir: t.TempDir()}
	fixtureUIKernel(t, a.globalDir)
	updated, _ := a.Update(FirstRunDoneMsg{OrchDir: filepath.Join(lingtaiDir, "manager"), OrchName: "manager"})
	got := updated.(App)

	// The snapshot proves ApplyRecipe ran against the parent-root bundle.
	snapshot := filepath.Join(lingtaiDir, ".tui-asset", ".recipe", "recipe.json")
	if _, err := os.Stat(snapshot); err != nil {
		t.Fatalf("recipe snapshot not materialized (ApplyRecipe did not run against parent root): %v", err)
	}
	// No recipe failure message should be surfaced on success.
	failMarker := i18nPrefix("mail.recipe_reapply_failed")
	for _, m := range got.mail.messages {
		if strings.Contains(m.Body, failMarker) {
			t.Fatalf("unexpected recipe failure message on success: %q", m.Body)
		}
	}
}

// TestFirstRunDoneBlocksLaunchOnRecipeFailure proves that when ApplyRecipe
// fails during the first-run launch path, the failure is surfaced as a
// persistent mail message AND the agent launch is blocked. Launch is blocked
// is asserted indirectly: lingtaiCmd is a non-executable fixture handle, so if the launch
// path were reached it would append a mail.launch_failed message; we assert
// only the recipe-failure message is present.
func TestFirstRunDoneBlocksLaunchOnRecipeFailure(t *testing.T) {
	// Empty recipe name → LoadRecipeInfo error → ApplyRecipe error.
	lingtaiDir := newFirstRunRecipeProject(t, "")
	projectRoot := filepath.Dir(lingtaiDir)
	if !preset.RecipeNeedsApply(projectRoot) {
		t.Fatal("precondition: recipe at parent root should need apply")
	}
	if _, err := preset.ApplyRecipe(projectRoot, "en", nil); err == nil {
		t.Fatal("precondition: ApplyRecipe should fail for an empty-name recipe")
	}

	a := App{
		projectDir: lingtaiDir,
		globalDir:  t.TempDir(),
	}
	// Real Project CLI, non-executable Python launch guard: initialization can
	// succeed, but a regression entering LaunchAgent cannot start a provider.
	a.lingtaiCmd = fixtureUIKernel(t, a.globalDir)
	updated, _ := a.Update(FirstRunDoneMsg{OrchDir: filepath.Join(lingtaiDir, "manager"), OrchName: "manager"})
	got := updated.(App)

	recipeMarker := i18nPrefix("mail.recipe_reapply_failed")
	launchMarker := i18nPrefix("mail.launch_failed")

	var sawRecipeFailure, sawLaunchFailure bool
	for _, m := range got.mail.messages {
		if strings.Contains(m.Body, recipeMarker) {
			sawRecipeFailure = true
		}
		if strings.Contains(m.Body, launchMarker) {
			sawLaunchFailure = true
		}
	}
	if !sawRecipeFailure {
		t.Fatalf("expected a persistent recipe-failure mail message; got messages: %+v", got.mail.messages)
	}
	if sawLaunchFailure {
		t.Fatal("launch was not blocked: a launch-failure message was surfaced after recipe failure")
	}
}
