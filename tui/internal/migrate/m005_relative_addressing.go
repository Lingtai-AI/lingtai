package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// lingtaiPrefixRe matches any absolute path ending in /.lingtai/ — i.e. any
// string like "/Users/alice/project/.lingtai/" regardless of where the project
// lived previously. This handles directories that were moved or renamed since
// the paths were written.
var lingtaiPrefixRe = regexp.MustCompile(`/[^\x00"]*?/\.lingtai/`)

// migrateRelativeAddressing replaces absolute .lingtai/ paths with relative
// directory names in all files under each agent directory.
//
// Detection: any .agent.json whose "address" field contains a "/" character
// is still using absolute paths.
//
// Method: for every file in every agent subdirectory, strip any absolute path
// prefix ending in /.lingtai/ — turning paths like
// "/old/path/.lingtai/本我" into just "本我". This handles both the current
// directory and any previous directory the .lingtai/ may have lived at.
func migrateRelativeAddressing(lingtaiDir string) error {
	lingtaiDir, _ = filepath.Abs(lingtaiDir)

	// Check if migration is needed: any .agent.json with "/" in address?
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

	// Walk every agent subdirectory and strip .lingtai/ prefixes in all files
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		// Skip dot-directories (.portal, .tui-asset, .skills)
		if strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, entry.Name())
		if err := rewriteDir(agentDir); err != nil {
			return err
		}
	}

	// Delete topology tape — derived cache with stale addresses
	os.Remove(filepath.Join(lingtaiDir, ".portal", "topology.jsonl"))
	os.RemoveAll(filepath.Join(lingtaiDir, ".portal", "replay"))

	return nil
}

// rewriteDir walks all files under dir and strips /.lingtai/ prefixes.
func rewriteDir(dir string) error {
	return filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil // skip unreadable entries
		}
		if info.IsDir() {
			return nil
		}
		if info.Size() > 10*1024*1024 { // skip files > 10MB
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil // skip unreadable
		}
		content := string(data)
		if !strings.Contains(content, "/.lingtai/") {
			return nil // nothing to replace
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
