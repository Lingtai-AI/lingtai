package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// recipeUsesCustomDir returns true for recipe types that carry their own
// on-disk bundle path rather than being resolved by name under the
// bundled-presets tree. Retained as a UI-level helper for the recipe
// picker; the apply flow itself treats all recipes uniformly.
func recipeUsesCustomDir(name string) bool {
	return name == preset.RecipeCustom || name == preset.RecipeImported || name == preset.RecipeAgora
}

// sourceBundleDir returns the on-disk recipe bundle directory for a given
// picker selection. For named/bundled recipes (greeter, tutorial, etc.)
// this resolves via the global preset tree; for custom/imported/agora
// recipes the caller-provided customDir is authoritative.
func sourceBundleDir(globalDir, recipeName, customDir string) string {
	if recipeUsesCustomDir(recipeName) {
		return customDir
	}
	return preset.RecipeDir(globalDir, recipeName)
}

// copyRecipeBundle stages the selected recipe bundle into the project
// root. After this call, <project>/.recipe/ contains the authoritative
// copy of the recipe and is the source of truth for all subsequent
// behavioral-layer resolution. Returns the project root so callers can
// chain path resolution.
func copyRecipeBundle(lingtaiDir, globalDir, recipeName, customDir string) (projectRoot string, err error) {
	projectRoot = filepath.Dir(lingtaiDir)
	src := sourceBundleDir(globalDir, recipeName, customDir)
	if src == "" {
		return projectRoot, fmt.Errorf("copyRecipeBundle: could not resolve source bundle for %q", recipeName)
	}
	if err := preset.CopyBundle(src, projectRoot); err != nil {
		return projectRoot, fmt.Errorf("copyRecipeBundle: %w", err)
	}
	return projectRoot, nil
}

// applyRecipe runs the greet/library/snapshot side of recipe application
// across every agent under .lingtai/<agent>/. Stages the selected bundle
// into the project root if it isn't already there, then hands off to
// preset.ApplyRecipe for the per-agent materialization.
//
// Idempotent with copyRecipeBundle — calling sequence
// (copyRecipeBundle → … → applyRecipe) does NOT re-copy the bundle
// because preset.CopyBundle is a RemoveAll+copy; the second invocation
// replaces the first copy with an identical one. Callers that want to
// skip the re-stage can call preset.ApplyRecipe directly.
//
// Behavioral layer defaults (all four layers are optional in a recipe):
//   - greet.md absent: no .prompt file is written (agent starts silently,
//     waits for mail). ApplyRecipe handles this internally.
//   - comment.md / covenant.md / procedures.md absent: the resolver
//     returns "" and the caller leaves those init.json fields empty. The
//     kernel supplies system defaults at agent launch when those fields
//     are unset.
//
// Callers are responsible for having already written the orchestrator's
// init.json via GenerateInitJSONWithOpts. applyRecipe itself only edits
// manifest.capabilities.library.paths (additive — never removes prior
// library path entries).
func applyRecipe(
	lingtaiDir, orchDir, globalDir, humanDir, humanAddr string,
	recipeName, customDir, lang, soulDelay string,
) error {
	_ = orchDir // ApplyRecipe iterates every agent under lingtaiDir itself

	projectRoot, err := copyRecipeBundle(lingtaiDir, globalDir, recipeName, customDir)
	if err != nil {
		return err
	}

	greetSubst := func(tmpl string) string {
		return substituteGreetPlaceholders(tmpl, humanAddr, humanDir, lang, soulDelay)
	}
	if _, err := preset.ApplyRecipe(projectRoot, lang, greetSubst); err != nil {
		return fmt.Errorf("applyRecipe: %w", err)
	}

	// Persist the picker selection (type + custom path) so /setup can
	// redisplay the last choice. The authoritative "what's applied" is
	// the directory snapshot at .lingtai/.tui-asset/.recipe/ which
	// ApplyRecipe already wrote; this JSON file is purely UI state.
	state := preset.RecipeState{Recipe: recipeName}
	if recipeUsesCustomDir(recipeName) {
		state.CustomDir = customDir
	}
	return preset.SaveRecipeState(lingtaiDir, state)
}

// resolveRecipeComment returns the comment.md path inside the project's
// staged .recipe/ directory, or "" when the recipe does not ship a
// comment. The caller treats empty as "leave CommentFile unset" so no
// comment file is referenced in init.json.
//
// Requires that the bundle has already been staged via copyRecipeBundle
// — resolution happens against <projectRoot>/.recipe/ not the original
// source. This keeps the project self-contained: init.json paths point
// at project-local files, not at ~/.lingtai-tui/... or the user's
// download folder.
func resolveRecipeComment(projectRoot, lang string) string {
	return preset.ResolveCommentPath(projectRoot, lang)
}

// resolveRecipeCovenant returns the covenant.md path inside the project's
// staged .recipe/. Empty when the recipe does not override the covenant
// — the kernel falls back to its system default at agent launch.
func resolveRecipeCovenant(projectRoot, lang string) string {
	return preset.ResolveCovenantPath(projectRoot, lang)
}

// resolveRecipeProcedures returns the procedures.md path inside the
// project's staged .recipe/. Empty when the recipe does not override
// procedures — the kernel falls back to its system default at agent
// launch.
func resolveRecipeProcedures(projectRoot, lang string) string {
	return preset.ResolveProceduresPath(projectRoot, lang)
}

// SubstituteGreetPlaceholders is the exported wrapper used by main.go on
// startup when ReconcileRecipe needs to render a greet template without
// knowing the TUI's internal helper. Delegates to the internal
// implementation so both call sites share behavior exactly.
func SubstituteGreetPlaceholders(template, humanAddr, humanDir, lang, soulDelay string) string {
	return substituteGreetPlaceholders(template, humanAddr, humanDir, lang, soulDelay)
}

// substituteGreetPlaceholders replaces canonical placeholder tokens in a greet
// template with runtime values before writing to .prompt.
func substituteGreetPlaceholders(template, humanAddr, humanDir, lang, soulDelay string) string {
	out := template
	out = strings.ReplaceAll(out, "{{time}}", time.Now().Format("2006-01-02 15:04"))
	out = strings.ReplaceAll(out, "{{addr}}", humanAddr)
	out = strings.ReplaceAll(out, "{{lang}}", lang)
	out = strings.ReplaceAll(out, "{{soul_delay}}", soulDelay)
	loc := "unknown"
	if humanDir != "" {
		if humanNode, err := fs.ReadAgent(humanDir); err == nil && humanNode.Location != nil {
			parts := []string{}
			if humanNode.Location.City != "" {
				parts = append(parts, humanNode.Location.City)
			}
			if humanNode.Location.Region != "" {
				parts = append(parts, humanNode.Location.Region)
			}
			if humanNode.Location.Country != "" {
				parts = append(parts, humanNode.Location.Country)
			}
			if len(parts) > 0 {
				loc = strings.Join(parts, ", ")
			}
		}
	}
	// If location is still unknown (first run, cache empty), try resolving
	// synchronously. ResolveLocation has a 5-second timeout built in.
	if loc == "unknown" {
		if resolved, err := fs.ResolveLocation(); err == nil {
			parts := []string{}
			if resolved.City != "" {
				parts = append(parts, resolved.City)
			}
			if resolved.Region != "" {
				parts = append(parts, resolved.Region)
			}
			if resolved.Country != "" {
				parts = append(parts, resolved.Country)
			}
			if len(parts) > 0 {
				loc = strings.Join(parts, ", ")
			}
			// Also persist it to human's .agent.json so next time it's cached
			if humanDir != "" {
				go fs.UpdateHumanLocation(humanDir)
			}
		}
	}
	out = strings.ReplaceAll(out, "{{location}}", loc)

	// Generate slash command list from palette commands + i18n detailed descriptions
	if strings.Contains(out, "{{commands}}") {
		var cmds []string
		for _, cmd := range DefaultCommands() {
			desc := i18n.TIn(lang, cmd.Detail)
			cmds = append(cmds, fmt.Sprintf("  - /%s — %s", cmd.Name, desc))
		}
		out = strings.ReplaceAll(out, "{{commands}}", strings.Join(cmds, "\n"))
	}

	return out
}

