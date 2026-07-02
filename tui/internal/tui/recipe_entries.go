package tui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// buildRecipeEntries resolves a recipe bundle through the same canonical
// .recipe/ helpers used by recipe apply, then returns MarkdownEntry items for
// the markdown viewer.
func buildRecipeEntries(recipeDir, lang string) []MarkdownEntry {
	if recipeDir == "" {
		return nil
	}
	if _, err := preset.LoadRecipeInfo(recipeDir, lang); err != nil {
		return nil
	}
	var entries []MarkdownEntry

	addPath := func(label, group, path string) {
		if path == "" {
			return
		}
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			entries = append(entries, MarkdownEntry{
				Label: label,
				Group: group,
				Path:  path,
			})
		}
	}

	addLayer := func(layer string, resolve func(string, string) string) {
		path := resolve(recipeDir, lang)
		label := layer + ".md"
		if recipeLayerUsesLang(recipeDir, layer, lang, path) {
			label += " (" + lang + ")"
		}
		addPath(label, layer+".md", path)
	}

	addLayer("greet", preset.ResolveGreetPath)
	addLayer("comment", preset.ResolveCommentPath)
	addPath("recipe.json", "recipe.json", filepath.Join(recipeDir, preset.RecipeDotDir, "recipe.json"))

	// Recipe libraries are sibling folders named by .recipe/recipe.json's
	// library_name field. Ignore the old top-level skills/ layout here; apply
	// no longer consumes it for canonical bundles.
	libraryRoot := preset.ResolveLibraryDir(recipeDir, lang)
	if libraryRoot != "" {
		skillDirs, err := os.ReadDir(libraryRoot)
		if err == nil {
			for _, sd := range skillDirs {
				if !sd.IsDir() || strings.HasPrefix(sd.Name(), ".") {
					continue
				}
				skillName := sd.Name()
				rootSkill := filepath.Join(libraryRoot, skillName, "SKILL.md")
				if info, err := os.Stat(rootSkill); err == nil && !info.IsDir() {
					entries = append(entries, MarkdownEntry{
						Label: skillName + "/SKILL.md",
						Group: i18n.T("skills.title"),
						Path:  rootSkill,
					})
				}
				langDirs, err := os.ReadDir(filepath.Join(libraryRoot, skillName))
				if err != nil {
					continue
				}
				for _, ld := range langDirs {
					if !ld.IsDir() || strings.HasPrefix(ld.Name(), ".") {
						continue
					}
					langSkill := filepath.Join(libraryRoot, skillName, ld.Name(), "SKILL.md")
					if info, err := os.Stat(langSkill); err == nil && !info.IsDir() {
						entries = append(entries, MarkdownEntry{
							Label: skillName + "/SKILL.md (" + ld.Name() + ")",
							Group: i18n.T("skills.title"),
							Path:  langSkill,
						})
					}
				}
			}
		}
	}

	// Optional overrides (only shown if they exist)
	addLayer("covenant", preset.ResolveCovenantPath)
	addLayer("procedures", preset.ResolveProceduresPath)

	return entries
}

func recipeLayerUsesLang(recipeDir, layer, lang, path string) bool {
	if lang == "" || path == "" {
		return false
	}
	return filepath.Clean(filepath.Dir(path)) == filepath.Clean(filepath.Join(recipeDir, preset.RecipeDotDir, layer, lang))
}
