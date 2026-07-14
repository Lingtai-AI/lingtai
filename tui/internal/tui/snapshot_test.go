package tui

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// dirSnapshot walks root recursively (root need not exist — a missing root
// snapshots as "absent") and returns a deterministic listing of every path
// (relative to root) plus a content hash AND file mode, so two snapshots can
// be compared for byte-for-byte equality including file contents and
// permission bits, not just names/content. Mode is included specifically
// because config.LoadConfig's permission-tightening migration (chmod to
// 0600) is itself a filesystem write that content-only hashing would never
// catch — a test could seed a 0644 file, run a supposedly pure code path,
// and see identical content while missing that the mode silently changed to
// 0600 underneath it.
func dirSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	info, err := os.Lstat(root)
	if err != nil {
		if os.IsNotExist(err) {
			return out // absent directory snapshots as empty — valid "no writes" state
		}
		t.Fatalf("lstat %s: %v", root, err)
	}
	if !info.IsDir() {
		t.Fatalf("snapshot root %s is not a directory", root)
	}
	err = filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, relErr := filepath.Rel(root, path)
		if relErr != nil {
			return relErr
		}
		if rel == "." {
			return nil
		}
		if fi.IsDir() {
			out[rel+"/"] = "dir mode=" + fi.Mode().Perm().String()
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			out[rel] = "unreadable:" + readErr.Error()
			return nil
		}
		sum := sha256.Sum256(data)
		out[rel] = hex.EncodeToString(sum[:]) + " mode=" + fi.Mode().Perm().String()
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
	}
	return out
}

// assertSnapshotsEqual fails the test with a readable diff if before/after
// differ in any path or content hash.
func assertSnapshotsEqual(t *testing.T, label string, before, after map[string]string) {
	t.Helper()
	var keys []string
	seen := map[string]bool{}
	for k := range before {
		keys = append(keys, k)
		seen[k] = true
	}
	for k := range after {
		if !seen[k] {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	var diffs []string
	for _, k := range keys {
		b, bOK := before[k]
		a, aOK := after[k]
		if !bOK {
			diffs = append(diffs, fmt.Sprintf("  + %s (created)", k))
			continue
		}
		if !aOK {
			diffs = append(diffs, fmt.Sprintf("  - %s (removed)", k))
			continue
		}
		if a != b {
			diffs = append(diffs, fmt.Sprintf("  ~ %s (content changed)", k))
		}
	}
	if len(diffs) > 0 {
		t.Fatalf("%s: filesystem changed unexpectedly:\n%s", label, joinLines(diffs))
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}
