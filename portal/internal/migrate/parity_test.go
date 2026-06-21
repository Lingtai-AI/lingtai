package migrate

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"testing"
)

type registryEntry struct {
	Version int
	Name    string
}

func TestMigrationCurrentVersionMatchesTUI(t *testing.T) {
	repoRoot := filepath.Clean(filepath.Join("..", "..", ".."))
	portalVersion, portalEntries := readMigrationRegistry(t, filepath.Join(repoRoot, "portal", "internal", "migrate", "migrate.go"))
	tuiVersion, tuiEntries := readMigrationRegistry(t, filepath.Join(repoRoot, "tui", "internal", "migrate", "migrate.go"))

	if portalVersion != tuiVersion {
		t.Fatalf("portal CurrentVersion = %d, TUI CurrentVersion = %d", portalVersion, tuiVersion)
	}
	if len(portalEntries) != portalVersion {
		t.Fatalf("portal migrations = %d, CurrentVersion = %d", len(portalEntries), portalVersion)
	}
	if len(tuiEntries) != tuiVersion {
		t.Fatalf("TUI migrations = %d, CurrentVersion = %d", len(tuiEntries), tuiVersion)
	}
}

func readMigrationRegistry(t *testing.T, path string) (int, []registryEntry) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	text := string(data)

	versionMatch := regexp.MustCompile(`const\s+CurrentVersion\s*=\s*(\d+)`).FindStringSubmatch(text)
	if versionMatch == nil {
		t.Fatalf("CurrentVersion not found in %s", path)
	}
	version, err := strconv.Atoi(versionMatch[1])
	if err != nil {
		t.Fatalf("parse CurrentVersion in %s: %v", path, err)
	}

	entryMatches := regexp.MustCompile(`\{Version:\s*(\d+),\s*Name:\s*"([^"]+)"`).FindAllStringSubmatch(text, -1)
	if len(entryMatches) == 0 {
		t.Fatalf("no migrations found in %s", path)
	}
	entries := make([]registryEntry, 0, len(entryMatches))
	seen := map[int]string{}
	for _, match := range entryMatches {
		entryVersion, err := strconv.Atoi(match[1])
		if err != nil {
			t.Fatalf("parse migration version in %s: %v", path, err)
		}
		name := match[2]
		if prev, ok := seen[entryVersion]; ok {
			t.Fatalf("duplicate migration version %d in %s: %s and %s", entryVersion, path, prev, name)
		}
		seen[entryVersion] = name
		entries = append(entries, registryEntry{Version: entryVersion, Name: name})
	}
	for i, entry := range entries {
		wantVersion := i + 1
		if entry.Version != wantVersion {
			t.Fatalf("migration %q in %s has version %d, want contiguous version %d", entry.Name, path, entry.Version, wantVersion)
		}
	}
	if entries[len(entries)-1].Version != version {
		t.Fatalf("%s has CurrentVersion %d but last migration version %d", path, version, entries[len(entries)-1].Version)
	}
	return version, entries
}
