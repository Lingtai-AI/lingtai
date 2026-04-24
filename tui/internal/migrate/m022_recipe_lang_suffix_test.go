package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// writeRecipeStateFile writes .lingtai/.tui-asset/.recipe with the given
// recipe field value.
func writeRecipeStateFile(t *testing.T, lingtaiDir, recipe, customDir string) string {
	t.Helper()
	assetDir := filepath.Join(lingtaiDir, ".tui-asset")
	if err := os.MkdirAll(assetDir, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", assetDir, err)
	}
	recipePath := filepath.Join(assetDir, ".recipe")
	state := map[string]interface{}{"recipe": recipe}
	if customDir != "" {
		state["custom_dir"] = customDir
	}
	data, _ := json.MarshalIndent(state, "", "  ")
	if err := os.WriteFile(recipePath, data, 0o644); err != nil {
		t.Fatalf("write recipe state: %v", err)
	}
	return recipePath
}

func readRecipeStateFile(t *testing.T, path string) (string, string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read recipe state: %v", err)
	}
	var obj struct {
		Recipe    string `json:"recipe"`
		CustomDir string `json:"custom_dir,omitempty"`
	}
	if err := json.Unmarshal(data, &obj); err != nil {
		t.Fatalf("parse recipe state: %v", err)
	}
	return obj.Recipe, obj.CustomDir
}

func TestMigrateRecipeLangSuffix_StripsZH(t *testing.T) {
	lingtaiDir := t.TempDir()
	path := writeRecipeStateFile(t, lingtaiDir, "greeter-zh", "")

	if err := migrateRecipeLangSuffix(lingtaiDir); err != nil {
		t.Fatalf("migration error: %v", err)
	}
	recipe, _ := readRecipeStateFile(t, path)
	if recipe != "greeter" {
		t.Errorf("after migration: recipe = %q, want %q", recipe, "greeter")
	}
}

func TestMigrateRecipeLangSuffix_StripsWEN(t *testing.T) {
	lingtaiDir := t.TempDir()
	path := writeRecipeStateFile(t, lingtaiDir, "adaptive-wen", "")

	if err := migrateRecipeLangSuffix(lingtaiDir); err != nil {
		t.Fatalf("migration error: %v", err)
	}
	recipe, _ := readRecipeStateFile(t, path)
	if recipe != "adaptive" {
		t.Errorf("after migration: recipe = %q, want %q", recipe, "adaptive")
	}
}

func TestMigrateRecipeLangSuffix_LeavesBareUntouched(t *testing.T) {
	lingtaiDir := t.TempDir()
	path := writeRecipeStateFile(t, lingtaiDir, "greeter", "")

	if err := migrateRecipeLangSuffix(lingtaiDir); err != nil {
		t.Fatalf("migration error: %v", err)
	}
	recipe, _ := readRecipeStateFile(t, path)
	if recipe != "greeter" {
		t.Errorf("after migration: recipe = %q, want %q unchanged", recipe, "greeter")
	}
}

func TestMigrateRecipeLangSuffix_PreservesCustomDir(t *testing.T) {
	lingtaiDir := t.TempDir()
	path := writeRecipeStateFile(t, lingtaiDir, "tutorial-zh", "/some/path")

	if err := migrateRecipeLangSuffix(lingtaiDir); err != nil {
		t.Fatalf("migration error: %v", err)
	}
	recipe, customDir := readRecipeStateFile(t, path)
	if recipe != "tutorial" {
		t.Errorf("recipe = %q, want %q", recipe, "tutorial")
	}
	if customDir != "/some/path" {
		t.Errorf("custom_dir = %q, want %q preserved", customDir, "/some/path")
	}
}

func TestMigrateRecipeLangSuffix_NoRecipeFile(t *testing.T) {
	lingtaiDir := t.TempDir()
	// No .tui-asset/.recipe file exists.
	if err := migrateRecipeLangSuffix(lingtaiDir); err != nil {
		t.Errorf("migration should be no-op on missing file, got %v", err)
	}
}

func TestStripRecipeLangSuffix(t *testing.T) {
	tests := []struct{ in, want string }{
		{"greeter", "greeter"},
		{"greeter-zh", "greeter"},
		{"greeter-wen", "greeter"},
		{"tutorial-zh", "tutorial"},
		{"plain-wen", "plain"},
		{"adaptive", "adaptive"},
		{"", ""},
		{"custom-zhou", "custom-zhou"}, // "-zh" is not a suffix here
		// Edge: ends in "-zh" but coincidentally happens to be a real name;
		// we can't distinguish, so we strip. Acceptable — no legitimate
		// recipe uses "-zh" suffix in the new model.
		{"something-zh", "something"},
	}
	for _, tt := range tests {
		got := stripRecipeLangSuffix(tt.in)
		if got != tt.want {
			t.Errorf("stripRecipeLangSuffix(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
