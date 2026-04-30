// Package dj provides the embedded recipe and launch logic for the DJ
// agent — an on-demand musician that composes one track per journal entry
// using the MiniMax music API. It is dormant by default and only acts
// when the human messages it.
//
// The DJ agent is gated on the orchestrator using a MiniMax preset:
// setupDJ refuses to write the agent's init.json if the orchestrator's
// llm.provider is not "minimax". The agent uses the `mmx` CLI (per the
// `minimax-cli` skill) to generate music — no MCP server registration
// is required.
package dj

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"
)

//go:embed all:assets
var assetsFS embed.FS

// ProjectDir returns the DJ's project root directory:
// ~/.lingtai-tui/dj/
func ProjectDir(globalDir string) string {
	return filepath.Join(globalDir, "dj")
}

// LingtaiDir returns the DJ's .lingtai directory:
// ~/.lingtai-tui/dj/.lingtai/
func LingtaiDir(globalDir string) string {
	return filepath.Join(globalDir, "dj", ".lingtai")
}

// AgentDir returns the DJ agent's working directory:
// ~/.lingtai-tui/dj/.lingtai/dj/
func AgentDir(globalDir string) string {
	return filepath.Join(globalDir, "dj", ".lingtai", "dj")
}

// RecipeDir populates the DJ recipe assets on disk and returns the path.
// The directory lives inside the DJ's project dir so it persists across
// launches; files are overwritten on every call so a TUI upgrade picks up
// the latest assets.
func RecipeDir(globalDir string) (string, error) {
	recipeDir := filepath.Join(ProjectDir(globalDir), "recipe")
	if err := populateAssets(recipeDir); err != nil {
		return "", err
	}
	return recipeDir, nil
}

// GreetContent returns the raw greet.md content for the DJ agent.
func GreetContent() string {
	data, err := assetsFS.ReadFile("assets/greet.md")
	if err != nil {
		return ""
	}
	return string(data)
}

// CommentPath returns the path to the comment.md file after populating assets.
func CommentPath(globalDir string) string {
	return filepath.Join(ProjectDir(globalDir), "recipe", "comment.md")
}

// CovenantPath returns the path to the covenant.md file after populating assets.
func CovenantPath(globalDir string) string {
	return filepath.Join(ProjectDir(globalDir), "recipe", "covenant.md")
}

func populateAssets(targetDir string) error {
	return fs.WalkDir(assetsFS, "assets", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("assets", path)
		if err != nil {
			return err
		}
		target := filepath.Join(targetDir, rel)

		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}

		data, err := assetsFS.ReadFile(path)
		if err != nil {
			return err
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
}
