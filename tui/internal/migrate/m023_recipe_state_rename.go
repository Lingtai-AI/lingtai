package migrate

import (
	"fmt"
	"os"
	"path/filepath"
)

// migrateRecipeStateRename renames .lingtai/.tui-asset/.recipe (file) to
// .lingtai/.tui-asset/recipe-state.json.
//
// Why: the old path collides with the applied-recipe DIRECTORY snapshot used
// by recipe_apply.go (AppliedRecipeSubpath = ".tui-asset/.recipe"). Having a
// file and a directory at the exact same path is an OS-level impossibility,
// which caused silent failures during recipe apply: SaveRecipeState (file
// write) and ApplyRecipe (directory snapshot) fought over the same slot and
// whichever ran last won, leaving the other subsystem broken.
//
// Handling:
//   - If .tui-asset/.recipe exists as a FILE (legacy state), rename it to
//     recipe-state.json. If recipe-state.json already exists, prefer it
//     (current convention wins; legacy is removed).
//   - If .tui-asset/.recipe exists as a DIRECTORY, leave it alone — that's
//     a valid applied-recipe snapshot under the new naming scheme and
//     shouldn't be touched by this migration. The SaveRecipeState code now
//     writes to recipe-state.json so the two no longer collide.
//   - If .tui-asset/.recipe doesn't exist, no-op.
func migrateRecipeStateRename(lingtaiDir string) error {
	oldPath := filepath.Join(lingtaiDir, ".tui-asset", ".recipe")
	newPath := filepath.Join(lingtaiDir, ".tui-asset", "recipe-state.json")

	st, err := os.Stat(oldPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat %s: %w", oldPath, err)
	}

	// If it's a directory, that's the applied-recipe snapshot — leave it.
	if st.IsDir() {
		return nil
	}

	// Legacy file. Move to the new name unless the new name already exists.
	if _, err := os.Stat(newPath); err == nil {
		// Newer file already present — delete the legacy one.
		if err := os.Remove(oldPath); err != nil {
			return fmt.Errorf("remove legacy %s: %w", oldPath, err)
		}
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("stat %s: %w", newPath, err)
	}

	if err := os.Rename(oldPath, newPath); err != nil {
		return fmt.Errorf("rename %s → %s: %w", oldPath, newPath, err)
	}
	return nil
}
