package preset

import (
	"os"
	"path/filepath"
	"testing"
)

// --- fixture helpers for the new .recipe/ layout ---

// writeRecipeJSON writes a minimal valid .recipe/recipe.json (optionally
// localized) into the given bundle dir. If lang is non-empty it writes to
// .recipe/<lang>/recipe.json; empty lang writes to .recipe/recipe.json.
func writeRecipeJSON(t *testing.T, bundleDir, lang, name, description string) {
	t.Helper()
	var dir string
	if lang == "" {
		dir = filepath.Join(bundleDir, RecipeDotDir)
	} else {
		dir = filepath.Join(bundleDir, RecipeDotDir, lang)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	body := `{"id":"test","name":"` + name + `","description":"` + description + `","library_name":null}`
	if err := os.WriteFile(filepath.Join(dir, "recipe.json"), []byte(body), 0o644); err != nil {
		t.Fatalf("write recipe.json: %v", err)
	}
}

// writeBehavioralFile writes a behavioral-layer file (greet/comment/
// covenant/procedures) into its canonical .recipe/<layer>/<lang>/<layer>.md
// location. Empty lang writes to the root position .recipe/<layer>/<layer>.md.
func writeBehavioralFile(t *testing.T, bundleDir, layer, lang, content string) {
	t.Helper()
	var dir string
	if lang == "" {
		dir = filepath.Join(bundleDir, RecipeDotDir, layer)
	} else {
		dir = filepath.Join(bundleDir, RecipeDotDir, layer, lang)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, layer+".md"), []byte(content), 0o644); err != nil {
		t.Fatalf("write %s.md: %v", layer, err)
	}
}

// minimalBundle creates a temp dir with a valid root .recipe/recipe.json
// so ValidateCustomDir and LoadRecipeInfo succeed. Returns the bundle root.
func minimalBundle(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	writeRecipeJSON(t, dir, "", "Test Recipe", "A test")
	return dir
}

// --- bundled-recipe discovery tests (rely on Bootstrap) ---

func TestRecipeDir(t *testing.T) {
	globalDir := t.TempDir()
	if err := Bootstrap(globalDir); err != nil {
		t.Fatalf("Bootstrap err = %v", err)
	}
	got := RecipeDir(globalDir, "greeter")
	want := filepath.Join(globalDir, "recipes", "recommended", "greeter")
	if got != want {
		t.Errorf("RecipeDir(greeter) = %q, want %q", got, want)
	}
	got = RecipeDir(globalDir, "tutorial")
	want = filepath.Join(globalDir, "recipes", "examples", "tutorial")
	if got != want {
		t.Errorf("RecipeDir(tutorial) = %q, want %q", got, want)
	}
	got = RecipeDir(globalDir, "nonexistent")
	if got != "" {
		t.Errorf("RecipeDir(nonexistent) = %q, want empty", got)
	}
}

func TestScanCategory(t *testing.T) {
	globalDir := t.TempDir()
	if err := Bootstrap(globalDir); err != nil {
		t.Fatalf("Bootstrap err = %v", err)
	}
	recipes := ScanCategory(globalDir, "recommended", "en")
	if len(recipes) == 0 {
		t.Fatalf("ScanCategory(recommended) returned no recipes")
	}
	found := false
	for _, r := range recipes {
		if r.ID == "greeter" {
			found = true
			if r.Info.Name == "" {
				t.Errorf("greeter recipe has empty name")
			}
			if r.Dir == "" {
				t.Errorf("greeter recipe has empty dir")
			}
			if r.Info.ID != "greeter" {
				t.Errorf("greeter info.ID = %q, want %q", r.Info.ID, "greeter")
			}
		}
	}
	if !found {
		t.Errorf("ScanCategory(recommended) did not find greeter")
	}
}

func TestScanCategory_Intrinsic(t *testing.T) {
	globalDir := t.TempDir()
	if err := Bootstrap(globalDir); err != nil {
		t.Fatalf("Bootstrap err = %v", err)
	}
	recipes := ScanCategory(globalDir, "intrinsic", "en")
	if len(recipes) == 0 {
		t.Fatalf("ScanCategory(intrinsic) returned no recipes")
	}
	ids := make(map[string]bool)
	for _, r := range recipes {
		ids[r.ID] = true
	}
	for _, want := range []string{"adaptive", "plain"} {
		if !ids[want] {
			t.Errorf("ScanCategory(intrinsic) missing %q", want)
		}
	}
}

// TestScanCategory_NoLangFilter confirms that unlike the old layout, we
// no longer produce separate -zh / -wen sibling recipe directories: each
// recipe is a single bundle with locale variants inside .recipe/.
func TestScanCategory_NoLangFilter(t *testing.T) {
	globalDir := t.TempDir()
	if err := Bootstrap(globalDir); err != nil {
		t.Fatalf("Bootstrap err = %v", err)
	}
	for _, lang := range []string{"en", "zh", "wen"} {
		recipes := ScanCategory(globalDir, "intrinsic", lang)
		ids := make(map[string]bool)
		for _, r := range recipes {
			ids[r.ID] = true
		}
		// Same recipe IDs regardless of lang — no more -zh / -wen suffixes.
		if !ids["adaptive"] {
			t.Errorf("ScanCategory(intrinsic, %q) missing adaptive", lang)
		}
		if !ids["plain"] {
			t.Errorf("ScanCategory(intrinsic, %q) missing plain", lang)
		}
		for id := range ids {
			if id == "adaptive-zh" || id == "adaptive-wen" || id == "plain-zh" || id == "plain-wen" {
				t.Errorf("ScanCategory(intrinsic, %q) returned legacy-suffix ID %q", lang, id)
			}
		}
	}
}

func TestScanCategory_Examples(t *testing.T) {
	globalDir := t.TempDir()
	if err := Bootstrap(globalDir); err != nil {
		t.Fatalf("Bootstrap err = %v", err)
	}
	recipes := ScanCategory(globalDir, "examples", "en")
	if len(recipes) == 0 {
		t.Fatalf("ScanCategory(examples) returned no recipes")
	}
	found := false
	for _, r := range recipes {
		if r.ID == "tutorial" {
			found = true
		}
	}
	if !found {
		t.Errorf("ScanCategory(examples) did not find tutorial")
	}
}

func TestScanCategory_Empty(t *testing.T) {
	globalDir := t.TempDir()
	recipes := ScanCategory(globalDir, "nonexistent", "en")
	if len(recipes) != 0 {
		t.Errorf("ScanCategory(nonexistent) = %d recipes, want 0", len(recipes))
	}
}

// --- behavioral-file resolver tests (new .recipe/<layer>/ layout) ---

func TestResolveGreetPath_LangSpecific(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "greet", "", "root greet")
	writeBehavioralFile(t, dir, "greet", "en", "en greet")

	got := ResolveGreetPath(dir, "en")
	want := filepath.Join(dir, RecipeDotDir, "greet", "en", "greet.md")
	if got != want {
		t.Errorf("ResolveGreetPath prefers lang-specific, got %q, want %q", got, want)
	}
}

func TestResolveGreetPath_FallbackToRoot(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "greet", "", "root greet")

	got := ResolveGreetPath(dir, "en")
	want := filepath.Join(dir, RecipeDotDir, "greet", "greet.md")
	if got != want {
		t.Errorf("ResolveGreetPath fallback to root, got %q, want %q", got, want)
	}
}

func TestResolveGreetPath_Empty(t *testing.T) {
	dir := t.TempDir()
	got := ResolveGreetPath(dir, "en")
	if got != "" {
		t.Errorf("ResolveGreetPath empty dir = %q, want empty string", got)
	}
}

func TestResolveGreetPath_EmptyLang(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "greet", "", "root greet")

	got := ResolveGreetPath(dir, "")
	want := filepath.Join(dir, RecipeDotDir, "greet", "greet.md")
	if got != want {
		t.Errorf("ResolveGreetPath empty lang = %q, want %q", got, want)
	}
}

func TestResolveGreetPath_EmptyBundleDir(t *testing.T) {
	got := ResolveGreetPath("", "en")
	if got != "" {
		t.Errorf("ResolveGreetPath empty bundleDir = %q, want empty", got)
	}
}

func TestResolveCovenantPath_LangSpecific(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "covenant", "en", "en covenant")
	got := ResolveCovenantPath(dir, "en")
	want := filepath.Join(dir, RecipeDotDir, "covenant", "en", "covenant.md")
	if got != want {
		t.Errorf("ResolveCovenantPath prefers lang-specific, got %q, want %q", got, want)
	}
}

func TestResolveCovenantPath_FallbackToRoot(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "covenant", "", "root covenant")
	got := ResolveCovenantPath(dir, "en")
	want := filepath.Join(dir, RecipeDotDir, "covenant", "covenant.md")
	if got != want {
		t.Errorf("ResolveCovenantPath fallback to root, got %q, want %q", got, want)
	}
}

func TestResolveCovenantPath_Empty(t *testing.T) {
	dir := t.TempDir()
	got := ResolveCovenantPath(dir, "en")
	if got != "" {
		t.Errorf("ResolveCovenantPath empty dir = %q, want empty string", got)
	}
}

func TestResolveProceduresPath_LangSpecific(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "procedures", "en", "en procedures")
	got := ResolveProceduresPath(dir, "en")
	want := filepath.Join(dir, RecipeDotDir, "procedures", "en", "procedures.md")
	if got != want {
		t.Errorf("ResolveProceduresPath prefers lang-specific, got %q, want %q", got, want)
	}
}

func TestResolveProceduresPath_FallbackToRoot(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "procedures", "", "root procedures")
	got := ResolveProceduresPath(dir, "en")
	want := filepath.Join(dir, RecipeDotDir, "procedures", "procedures.md")
	if got != want {
		t.Errorf("ResolveProceduresPath fallback to root, got %q, want %q", got, want)
	}
}

func TestResolveCommentPath_LangSpecific(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "comment", "zh", "zh comment")
	got := ResolveCommentPath(dir, "zh")
	want := filepath.Join(dir, RecipeDotDir, "comment", "zh", "comment.md")
	if got != want {
		t.Errorf("ResolveCommentPath lang-specific = %q, want %q", got, want)
	}
}

func TestResolveCommentPath_FallbackToRoot(t *testing.T) {
	dir := t.TempDir()
	writeBehavioralFile(t, dir, "comment", "", "root comment")
	got := ResolveCommentPath(dir, "en")
	want := filepath.Join(dir, RecipeDotDir, "comment", "comment.md")
	if got != want {
		t.Errorf("ResolveCommentPath fallback to root, got %q, want %q", got, want)
	}
}

// --- ValidateCustomDir ---

func TestValidateCustomDir_OK(t *testing.T) {
	dir := minimalBundle(t) // has .recipe/recipe.json
	if err := ValidateCustomDir(dir); err != nil {
		t.Errorf("ValidateCustomDir(valid bundle) = %v, want nil", err)
	}
}

func TestValidateCustomDir_NoRecipeJSON(t *testing.T) {
	dir := t.TempDir() // empty — no .recipe/recipe.json
	if err := ValidateCustomDir(dir); err == nil {
		t.Errorf("ValidateCustomDir(dir without .recipe/recipe.json) = nil, want error")
	}
}

func TestValidateCustomDir_Missing(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "does-not-exist")
	if err := ValidateCustomDir(missing); err == nil {
		t.Errorf("ValidateCustomDir(missing) = nil, want error")
	}
}

func TestValidateCustomDir_IsFile(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "not-a-dir")
	os.WriteFile(filePath, []byte("x"), 0o644)
	if err := ValidateCustomDir(filePath); err == nil {
		t.Errorf("ValidateCustomDir(file) = nil, want error")
	}
}

// --- ProjectLocalRecipeDir ---

func TestProjectLocalRecipeDir_Present(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, RecipeDotDir), 0o755)

	got := ProjectLocalRecipeDir(root)
	if got != root {
		t.Errorf("ProjectLocalRecipeDir = %q, want %q", got, root)
	}
}

func TestProjectLocalRecipeDir_Absent(t *testing.T) {
	root := t.TempDir()
	got := ProjectLocalRecipeDir(root)
	if got != "" {
		t.Errorf("ProjectLocalRecipeDir = %q, want empty", got)
	}
}

func TestProjectLocalRecipeDir_IsFile(t *testing.T) {
	root := t.TempDir()
	fakeFile := filepath.Join(root, RecipeDotDir)
	os.WriteFile(fakeFile, []byte("x"), 0o644)

	got := ProjectLocalRecipeDir(root)
	if got != "" {
		t.Errorf("ProjectLocalRecipeDir(file) = %q, want empty", got)
	}
}

// --- langFallbackChain ---

func TestLangFallbackChain(t *testing.T) {
	tests := []struct {
		lang string
		want []string
	}{
		{"wen", []string{"wen", ""}},
		{"zh", []string{"zh", ""}},
		{"en", []string{"en", ""}},
		{"", []string{""}},
		{"fr", []string{"fr", ""}},
	}
	for _, tt := range tests {
		got := langFallbackChain(tt.lang)
		if len(got) != len(tt.want) {
			t.Errorf("langFallbackChain(%q) len = %d, want %d", tt.lang, len(got), len(tt.want))
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("langFallbackChain(%q)[%d] = %q, want %q", tt.lang, i, got[i], tt.want[i])
			}
		}
	}
}

// --- ResolveSkillDir (legacy tolerance — kept intact) ---

func TestResolveSkillDir_FallsBackToRoot(t *testing.T) {
	dir := t.TempDir()
	rootDir := filepath.Join(dir, "skills", "my-skill")
	os.MkdirAll(rootDir, 0o755)
	os.WriteFile(filepath.Join(rootDir, "SKILL.md"), []byte("---\nname: my-skill\n---\n"), 0o644)

	got := ResolveSkillDir(dir, "my-skill", "wen")
	if got != rootDir {
		t.Errorf("ResolveSkillDir wen→root fallback = %q, want %q", got, rootDir)
	}
}

func TestResolveSkillDir_LangSpecific(t *testing.T) {
	dir := t.TempDir()
	skillDir := filepath.Join(dir, "skills", "my-skill", "en")
	os.MkdirAll(skillDir, 0o755)
	os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte("---\nname: my-skill\n---\n"), 0o644)

	got := ResolveSkillDir(dir, "my-skill", "en")
	if got != skillDir {
		t.Errorf("ResolveSkillDir lang-specific = %q, want %q", got, skillDir)
	}
}

func TestResolveSkillDir_NoMatch(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "skills", "my-skill"), 0o755)

	got := ResolveSkillDir(dir, "my-skill", "en")
	if got != "" {
		t.Errorf("ResolveSkillDir no match = %q, want empty", got)
	}
}

func TestResolveSkillDir_EmptyRecipeDir(t *testing.T) {
	got := ResolveSkillDir("", "my-skill", "en")
	if got != "" {
		t.Errorf("ResolveSkillDir empty recipeDir = %q, want empty", got)
	}
}

// --- LoadRecipeInfo (new .recipe/recipe.json layout) ---

func TestLoadRecipeInfo_Valid(t *testing.T) {
	dir := t.TempDir()
	writeRecipeJSON(t, dir, "", "Test Recipe", "A test")

	info, err := LoadRecipeInfo(dir, "en")
	if err != nil {
		t.Fatalf("LoadRecipeInfo error: %v", err)
	}
	if info.Name != "Test Recipe" {
		t.Errorf("Name = %q, want %q", info.Name, "Test Recipe")
	}
	if info.Description != "A test" {
		t.Errorf("Description = %q, want %q", info.Description, "A test")
	}
	if info.Version != "1.0.0" {
		t.Errorf("Version default = %q, want %q", info.Version, "1.0.0")
	}
	if info.LibraryName != nil {
		t.Errorf("LibraryName = %v, want nil", *info.LibraryName)
	}
}

func TestLoadRecipeInfo_LangSpecific(t *testing.T) {
	dir := t.TempDir()
	writeRecipeJSON(t, dir, "", "Root", "root")
	writeRecipeJSON(t, dir, "zh", "中文名", "中文描述")

	info, err := LoadRecipeInfo(dir, "zh")
	if err != nil {
		t.Fatalf("LoadRecipeInfo error: %v", err)
	}
	if info.Name != "中文名" {
		t.Errorf("Name = %q, want %q", info.Name, "中文名")
	}
}

func TestLoadRecipeInfo_FallbackToRoot(t *testing.T) {
	dir := t.TempDir()
	writeRecipeJSON(t, dir, "", "Root Name", "root")

	info, err := LoadRecipeInfo(dir, "wen")
	if err != nil {
		t.Fatalf("LoadRecipeInfo error: %v", err)
	}
	if info.Name != "Root Name" {
		t.Errorf("Name = %q, want %q", info.Name, "Root Name")
	}
}

func TestLoadRecipeInfo_Missing(t *testing.T) {
	dir := t.TempDir()
	_, err := LoadRecipeInfo(dir, "en")
	if err == nil {
		t.Errorf("LoadRecipeInfo should error when .recipe/recipe.json missing")
	}
}

func TestLoadRecipeInfo_EmptyName(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, RecipeDotDir), 0o755)
	os.WriteFile(
		filepath.Join(dir, RecipeDotDir, "recipe.json"),
		[]byte(`{"id":"test","name":"","description":"has desc"}`),
		0o644,
	)

	_, err := LoadRecipeInfo(dir, "en")
	if err == nil {
		t.Errorf("LoadRecipeInfo should error when name is empty")
	}
}

func TestLoadRecipeInfo_ExtraFieldsIgnored(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, RecipeDotDir), 0o755)
	os.WriteFile(
		filepath.Join(dir, RecipeDotDir, "recipe.json"),
		[]byte(`{"id":"test","name":"Test","description":"d","version":"2.0.0","author":"me","extra":"ignored"}`),
		0o644,
	)

	info, err := LoadRecipeInfo(dir, "en")
	if err != nil {
		t.Fatalf("LoadRecipeInfo error: %v", err)
	}
	if info.Name != "Test" {
		t.Errorf("Name = %q, want %q", info.Name, "Test")
	}
	if info.Version != "2.0.0" {
		t.Errorf("Version = %q, want %q", info.Version, "2.0.0")
	}
}

func TestLoadRecipeInfo_EmptyDir(t *testing.T) {
	_, err := LoadRecipeInfo("", "en")
	if err == nil {
		t.Errorf("LoadRecipeInfo should error on empty dir")
	}
}

// --- LibraryName / LibraryPath tests (new feature) ---

func TestLoadRecipeInfo_LibraryName(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, RecipeDotDir), 0o755)
	os.WriteFile(
		filepath.Join(dir, RecipeDotDir, "recipe.json"),
		[]byte(`{"id":"test","name":"Test","description":"d","library_name":"my-lib"}`),
		0o644,
	)

	info, err := LoadRecipeInfo(dir, "en")
	if err != nil {
		t.Fatalf("LoadRecipeInfo error: %v", err)
	}
	if info.LibraryName == nil {
		t.Fatalf("LibraryName = nil, want pointer to %q", "my-lib")
	}
	if *info.LibraryName != "my-lib" {
		t.Errorf("LibraryName = %q, want %q", *info.LibraryName, "my-lib")
	}
}

func TestResolveLibraryDir_Present(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, RecipeDotDir), 0o755)
	os.WriteFile(
		filepath.Join(dir, RecipeDotDir, "recipe.json"),
		[]byte(`{"id":"test","name":"Test","description":"d","library_name":"my-lib"}`),
		0o644,
	)
	// Create the sibling library dir.
	libDir := filepath.Join(dir, "my-lib")
	os.MkdirAll(libDir, 0o755)

	got := ResolveLibraryDir(dir, "en")
	if got != libDir {
		t.Errorf("ResolveLibraryDir = %q, want %q", got, libDir)
	}
}

func TestResolveLibraryDir_NoLibrary(t *testing.T) {
	dir := minimalBundle(t) // library_name: null
	got := ResolveLibraryDir(dir, "en")
	if got != "" {
		t.Errorf("ResolveLibraryDir with null library_name = %q, want empty", got)
	}
}

func TestResolveLibraryDir_LibraryMissing(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, RecipeDotDir), 0o755)
	os.WriteFile(
		filepath.Join(dir, RecipeDotDir, "recipe.json"),
		[]byte(`{"id":"test","name":"Test","description":"d","library_name":"missing-lib"}`),
		0o644,
	)
	got := ResolveLibraryDir(dir, "en")
	if got != "" {
		t.Errorf("ResolveLibraryDir with missing library dir = %q, want empty", got)
	}
}

func TestLibraryPathForInitJSON_WithLibrary(t *testing.T) {
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, RecipeDotDir), 0o755)
	os.WriteFile(
		filepath.Join(dir, RecipeDotDir, "recipe.json"),
		[]byte(`{"id":"test","name":"Test","description":"d","library_name":"my-lib"}`),
		0o644,
	)

	got := LibraryPathForInitJSON(dir, "en")
	want := filepath.Join("..", "..", "my-lib")
	if got != want {
		t.Errorf("LibraryPathForInitJSON = %q, want %q", got, want)
	}
}

func TestLibraryPathForInitJSON_NoLibrary(t *testing.T) {
	dir := minimalBundle(t) // library_name: null
	got := LibraryPathForInitJSON(dir, "en")
	if got != "" {
		t.Errorf("LibraryPathForInitJSON with null library_name = %q, want empty", got)
	}
}

func TestLibraryPathForInitJSON_EmptyDir(t *testing.T) {
	got := LibraryPathForInitJSON("", "en")
	if got != "" {
		t.Errorf("LibraryPathForInitJSON empty dir = %q, want empty", got)
	}
}
