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

// applyRecipe copies the selected recipe bundle into the project root
// (materializing <project>/.recipe/ + optional library sibling + optional
// .lingtai/ sibling from imported networks), then runs preset.ApplyRecipe
// to materialize the recipe across every agent under .lingtai/<agent>/.
// The apply phase writes .prompt (greet + substitutions), appends the
// library path to each agent's init.json when the recipe declares one,
// and snapshots the applied recipe to .lingtai/.tui-asset/.recipe/ for
// future change detection.
//
// Callers are responsible for having already written the orchestrator's
// init.json via GenerateInitJSONWithOpts — applyRecipe does not touch
// init.json fields that the AgentOpts pipeline owns (CommentFile,
// CovenantFile, ProceduresFile); it only edits manifest.capabilities.library.paths.
func applyRecipe(
	lingtaiDir, orchDir, globalDir, humanDir, humanAddr string,
	recipeName, customDir, lang, soulDelay string,
) error {
	_ = orchDir // no longer needed — ApplyRecipe iterates all agents under lingtaiDir itself

	projectRoot := filepath.Dir(lingtaiDir)
	src := sourceBundleDir(globalDir, recipeName, customDir)
	if src == "" {
		return fmt.Errorf("applyRecipe: could not resolve source bundle for %q", recipeName)
	}

	// 1. Copy the bundle into the project so it becomes self-contained.
	if err := preset.CopyBundle(src, projectRoot); err != nil {
		return fmt.Errorf("applyRecipe: copy bundle: %w", err)
	}

	// 2. Apply (write .prompt, update library.paths, snapshot).
	greetSubst := func(tmpl string) string {
		return substituteGreetPlaceholders(tmpl, humanAddr, humanDir, lang, soulDelay)
	}
	if _, err := preset.ApplyRecipe(projectRoot, lang, greetSubst); err != nil {
		return fmt.Errorf("applyRecipe: apply: %w", err)
	}

	// 3. Persist the picker selection (type + custom path) for UI
	// redisplay on subsequent launches. The authoritative "what's
	// currently applied" is the .tui-asset/.recipe/ snapshot written by
	// ApplyRecipe; this JSON file is purely UI state so /setup remembers
	// where the user last imported from.
	state := preset.RecipeState{Recipe: recipeName}
	if recipeUsesCustomDir(recipeName) {
		state.CustomDir = customDir
	}
	return preset.SaveRecipeState(lingtaiDir, state)
}

// resolveRecipeComment returns the comment.md path for the recipe
// currently copied into the project (<projectRoot>/.recipe/). Falls back
// to resolving from the source bundle if the project hasn't had a recipe
// copied in yet (e.g. /setup resolving paths before CopyBundle runs).
func resolveRecipeComment(globalDir, recipeName, customDir, lang string) string {
	if p := preset.ResolveCommentPath(customOrProjectBundleDir(globalDir, recipeName, customDir), lang); p != "" {
		return p
	}
	return ""
}

// resolveRecipeCovenant returns the covenant.md path. Returns empty string
// if the recipe does not override the system-wide covenant.
func resolveRecipeCovenant(globalDir, recipeName, customDir, lang string) string {
	return preset.ResolveCovenantPath(customOrProjectBundleDir(globalDir, recipeName, customDir), lang)
}

// resolveRecipeProcedures returns the procedures.md path. Returns empty
// string if the recipe does not override the system-wide procedures.
func resolveRecipeProcedures(globalDir, recipeName, customDir, lang string) string {
	return preset.ResolveProceduresPath(customOrProjectBundleDir(globalDir, recipeName, customDir), lang)
}

// customOrProjectBundleDir picks the right bundle directory to resolve
// behavioral-layer files from. For pre-apply calls (e.g. during /setup
// before CopyBundle runs) we resolve from the source bundle so the opts
// struct can be populated in one pass. After apply, the init.json has
// absolute paths baked in so this function is largely redundant but
// harmless — repeated resolution returns the source path, which by then
// is also mirrored in the project copy.
func customOrProjectBundleDir(globalDir, recipeName, customDir string) string {
	return sourceBundleDir(globalDir, recipeName, customDir)
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

