package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// lingtaiPrefixRe matches any absolute path ending in /.lingtai/ — handles
// directories that were moved or renamed since the paths were written.
var lingtaiPrefixRe = regexp.MustCompile(`/[^\x00"]*?/\.lingtai/`)

// migrateRelativeAddressing replaces absolute .lingtai/ paths with relative
// directory names in all files under each agent directory.
func migrateRelativeAddressing(lingtaiDir string) error {
	lingtaiDir, _ = filepath.Abs(lingtaiDir)

	needed := false
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		manifestPath := filepath.Join(lingtaiDir, entry.Name(), ".agent.json")
		data, err := os.ReadFile(manifestPath)
		if err != nil {
			continue
		}
		if strings.Contains(string(data), "/.lingtai/") {
			needed = true
			break
		}
	}
	if !needed {
		return nil
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, entry.Name())
		if err := rewriteDir(agentDir); err != nil {
			return err
		}
	}

	os.Remove(filepath.Join(lingtaiDir, ".portal", "topology.jsonl"))
	os.RemoveAll(filepath.Join(lingtaiDir, ".portal", "replay"))

	return nil
}

func rewriteDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if info.IsDir() {
			return nil
		}
		if info.Size() > 10*1024*1024 {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		content := string(data)
		if !strings.Contains(content, "/.lingtai/") {
			return nil
		}
		newContent := lingtaiPrefixRe.ReplaceAllString(content, "")
		if newContent == content {
			return nil
		}
		tmpPath := path + ".tmp"
		if err := os.WriteFile(tmpPath, []byte(newContent), info.Mode()); err != nil {
			return err
		}
		return os.Rename(tmpPath, path)
	})
}
