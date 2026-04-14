package timemachine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// InitGit initializes a git repo in dir if one doesn't exist.
func InitGit(dir string) error {
	if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
		return nil
	}
	if err := Git(dir, "init"); err != nil {
		return fmt.Errorf("git init: %w", err)
	}
	if err := Git(dir, "config", "user.email", "timemachine@lingtai"); err != nil {
		return fmt.Errorf("git config email: %w", err)
	}
	if err := Git(dir, "config", "user.name", "灵台 Time Machine"); err != nil {
		return fmt.Errorf("git config name: %w", err)
	}
	gitignore := filepath.Join(dir, ".gitignore")
	if _, err := os.Stat(gitignore); err == nil {
		Git(dir, "add", ".gitignore")
	}
	return Git(dir, "commit", "--allow-empty", "-m", "init: time machine")
}

// Commit stages all changes and commits with the given message.
// Returns true if a commit was made.
func Commit(dir, message string) (bool, error) {
	if err := Git(dir, "add", "-A"); err != nil {
		return false, fmt.Errorf("git add: %w", err)
	}
	cmd := exec.Command("git", "diff", "--cached", "--quiet")
	cmd.Dir = dir
	if err := cmd.Run(); err == nil {
		return false, nil
	}
	if err := Git(dir, "commit", "-m", message); err != nil {
		return false, fmt.Errorf("git commit: %w", err)
	}
	return true, nil
}

// ScanLargeFiles appends files exceeding maxBytes to .gitignore.
func ScanLargeFiles(dir string, maxBytes int64) {
	gitignorePath := filepath.Join(dir, ".gitignore")
	existing, _ := os.ReadFile(gitignorePath)
	ignoredLines := make(map[string]bool)
	for _, line := range strings.Split(string(existing), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			ignoredLines[line] = true
		}
	}

	var toAdd []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() && info.Name() == ".git" {
			return filepath.SkipDir
		}
		if info.IsDir() {
			return nil
		}
		if info.Size() > maxBytes {
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return nil
			}
			if !ignoredLines[rel] {
				toAdd = append(toAdd, rel)
			}
		}
		return nil
	})

	if len(toAdd) > 0 {
		f, err := os.OpenFile(gitignorePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
		if err != nil {
			return
		}
		defer f.Close()
		for _, rel := range toAdd {
			f.WriteString(rel + "\n")
		}
	}
}

// Git runs a git command in the given directory.
func Git(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	return cmd.Run()
}
