package preset

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// wantMaintenance is the exact canonical sentence that every TUI-shipped
// SKILL.md must carry in its YAML frontmatter.
const wantMaintenance = `If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths.`

// TestPresetSkillRouter_BuiltinBijection verifies the 5-sided bijection
// between BuiltinPresets(), embedded child dirs, parent YAML catalog,
// human routing table, and the extracted temp tree.
func TestPresetSkillRouter_BuiltinBijection(t *testing.T) {
	// A = want from BuiltinPresets()
	want := map[string]bool{}
	for _, p := range BuiltinPresets() {
		want[p.Name] = true
	}
	if len(want) != 12 {
		t.Fatalf("BuiltinPresets() returned %d unique names, want 12", len(want))
	}

	// B = embedded child dirs
	childDirs := map[string]bool{}
	err := fs.WalkDir(skillsFS, "skills/lingtai-preset-skill/reference", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "/SKILL.md") {
			return nil
		}
		// Extract the dir name: reference/<name>/SKILL.md
		rel := strings.TrimPrefix(path, "skills/lingtai-preset-skill/reference/")
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) == 2 && parts[1] == "SKILL.md" {
			childDirs[parts[0]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Assert A == B
	for name := range want {
		if !childDirs[name] {
			t.Errorf("BuiltinPresets() has %q but no embedded child dir", name)
		}
	}
	for name := range childDirs {
		if !want[name] {
			t.Errorf("embedded child dir %q not in BuiltinPresets()", name)
		}
	}

	// Read parent
	parentData, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/SKILL.md")
	if err != nil {
		t.Fatalf("cannot read parent SKILL.md: %v", err)
	}
	parentBody := string(parentData)

	// C = parent YAML catalog names
	yamlLocRE := regexp.MustCompile(`location:\s*reference/([^/ \n]+)/SKILL\.md`)
	yamlNameRE := regexp.MustCompile(`name:\s*preset-skill-([^\s\n]+)`)
	locMatches := yamlLocRE.FindAllStringSubmatch(parentBody, -1)
	nameMatches := yamlNameRE.FindAllStringSubmatch(parentBody, -1)

	catalogNames := map[string]bool{}
	if len(locMatches) != len(nameMatches) {
		t.Errorf("parent YAML catalog: %d location entries vs %d name entries", len(locMatches), len(nameMatches))
	}
	for i, m := range locMatches {
		childName := m[1]
		catalogNames[childName] = true
		if i < len(nameMatches) {
			expectedPrefix := "preset-skill-" + childName
			if nameMatches[i][1] != childName {
				t.Errorf("parent YAML catalog entry %d: name=%q does not match location=%q", i, nameMatches[i][1], childName)
			}
			_ = expectedPrefix
		}
	}

	// Assert A == C
	for name := range want {
		if !catalogNames[name] {
			t.Errorf("BuiltinPresets() has %q but parent YAML catalog does not", name)
		}
	}
	for name := range catalogNames {
		if !want[name] {
			t.Errorf("parent YAML catalog has %q but BuiltinPresets() does not", name)
		}
	}

	// D = human routing table: assert each want name and reference/<name>/SKILL.md appears
	if !strings.Contains(parentBody, "Nested reference catalog") {
		t.Error("parent missing 'Nested reference catalog' heading")
	}
	if !strings.Contains(parentBody, "Routing table") {
		t.Error("parent missing 'Routing table' heading")
	}
	for name := range want {
		loc := "reference/" + name + "/SKILL.md"
		if !strings.Contains(parentBody, loc) {
			t.Errorf("parent routing table missing %q", loc)
		}
	}

	// E = extracted temp tree
	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)
	utilitiesDir := filepath.Join(globalDir, "utilities", "lingtai-preset-skill")
	for name := range want {
		childPath := filepath.Join(utilitiesDir, "reference", name, "SKILL.md")
		if _, err := os.Stat(childPath); err != nil {
			t.Errorf("extracted tree missing %s: %v", childPath, err)
		}
	}
	// Check no extra children
	entries, err := os.ReadDir(filepath.Join(utilitiesDir, "reference"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() && !want[e.Name()] {
			t.Errorf("extracted tree has extra child dir %q not in BuiltinPresets()", e.Name())
		}
	}

	// BundledSkillNames check
	if !BundledSkillNames()["lingtai-preset-skill"] {
		t.Error("BundledSkillNames() does not contain lingtai-preset-skill")
	}

	// related_files check: every entry in parent must be a real repo-relative path
	// by walking up from the package directory to the worktree root.
	relatedRE := regexp.MustCompile(`(?m)^  - ([^\s#].+)$`)
	inRelated := false
	// Resolve the worktree root by walking up from the embedded skills dir
	// to the tui/ directory, then one more level.
	worktreeRoot := filepath.Join("..", "..", "..") // tui/internal/preset -> tui -> worktree root
	for _, line := range strings.Split(parentBody, "\n") {
		if strings.HasPrefix(line, "related_files:") {
			inRelated = true
			continue
		}
		if inRelated && strings.HasPrefix(line, "---") {
			break
		}
		if inRelated {
			m := relatedRE.FindStringSubmatch(line)
			if m != nil {
				rel := m[1]
				abs := filepath.Join(worktreeRoot, rel)
				if _, err := os.Stat(abs); err != nil {
					t.Errorf("related_files entry %q does not resolve to a real file (checked %s)", rel, abs)
				}
			}
		}
	}
}

// TestPresetSkillRouter_ChildNaming verifies each child's frontmatter name
// matches "preset-skill-<dirName>" and all are unique.
func TestPresetSkillRouter_ChildNaming(t *testing.T) {
	names := map[string]bool{}
	err := fs.WalkDir(skillsFS, "skills/lingtai-preset-skill/reference", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "/SKILL.md") {
			return nil
		}
		rel := strings.TrimPrefix(path, "skills/lingtai-preset-skill/reference/")
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) != 2 || parts[1] != "SKILL.md" {
			return nil
		}
		dirName := parts[0]

		data, err := fs.ReadFile(skillsFS, path)
		if err != nil {
			return err
		}
		body := string(data)
		if !strings.HasPrefix(body, "---\n") {
			t.Errorf("%s missing frontmatter", path)
			return nil
		}
		end := strings.Index(body[4:], "\n---")
		if end < 0 {
			t.Errorf("%s missing closing ---", path)
			return nil
		}
		fm := body[:4+end]
		nameRE := regexp.MustCompile(`(?m)^name:\s*(.+)$`)
		m := nameRE.FindStringSubmatch(fm)
		if m == nil {
			t.Errorf("%s missing name in frontmatter", path)
			return nil
		}
		gotName := strings.TrimSpace(m[1])
		wantName := "preset-skill-" + dirName
		if gotName != wantName {
			t.Errorf("%s: name=%q, want %q", path, gotName, wantName)
		}
		if names[gotName] {
			t.Errorf("duplicate child name %q", gotName)
		}
		names[gotName] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(names) != 12 {
		t.Errorf("got %d unique child names, want 12", len(names))
	}
}

// TestPresetSkillRouter_ParentMaintenanceAndRelated verifies the parent has
// the canonical maintenance value and a non-empty related_files list.
func TestPresetSkillRouter_ParentMaintenanceAndRelated(t *testing.T) {
	data, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	if !strings.HasPrefix(body, "---\n") {
		t.Fatal("parent missing frontmatter")
	}
	end := strings.Index(body[4:], "\n---")
	if end < 0 {
		t.Fatal("parent missing closing ---")
	}
	fm := body[:4+end]

	maintRE := regexp.MustCompile(`(?m)^maintenance:\s*"?(.+?)"?\s*$`)
	m := maintRE.FindStringSubmatch(fm)
	if m == nil {
		t.Fatal("parent missing maintenance field")
	}
	got := strings.TrimSpace(m[1])
	if got != wantMaintenance {
		t.Errorf("parent maintenance mismatch.\n got: %s\nwant: %s", got, wantMaintenance)
	}

	if !strings.Contains(fm, "related_files:") {
		t.Error("parent missing related_files field")
	}
}

// TestPresetSkillRouter_ChildSizeBudget verifies each child body is within
// the 1800-6000 char band (approx 450-1500 tokens).
func TestPresetSkillRouter_ChildSizeBudget(t *testing.T) {
	err := fs.WalkDir(skillsFS, "skills/lingtai-preset-skill/reference", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "/SKILL.md") {
			return nil
		}
		data, err := fs.ReadFile(skillsFS, path)
		if err != nil {
			return err
		}
		text := string(data)
		secondDash := strings.Index(text, "\n---")
		if secondDash < 0 {
			t.Errorf("%s missing closing ---", path)
			return nil
		}
		body := strings.TrimPrefix(text[secondDash+4:], "\n")
		bodyChars := len(body)
		approxTokens := bodyChars / 4

		rel := strings.TrimPrefix(path, "skills/lingtai-preset-skill/reference/")
		parts := strings.SplitN(rel, "/", 2)
		name := parts[0]
		t.Logf("%s: %d chars ~ %d tokens", name, bodyChars, approxTokens)

		if bodyChars < 1800 {
			t.Errorf("%s body too short: %d chars (min 1800)", name, bodyChars)
		}
		if bodyChars > 6000 {
			t.Errorf("%s body too long: %d chars (max 6000)", name, bodyChars)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestPresetSkillRouter_ParentIsLeanRouter verifies the parent body is
// within the 2500-7000 char band and contains the required headings.
func TestPresetSkillRouter_ParentIsLeanRouter(t *testing.T) {
	data, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	secondDash := strings.Index(text, "\n---")
	if secondDash < 0 {
		t.Fatal("parent missing closing ---")
	}
	body := strings.TrimPrefix(text[secondDash+4:], "\n")
	bodyChars := len(body)
	t.Logf("parent body: %d chars ~ %d tokens", bodyChars, bodyChars/4)

	if bodyChars < 2500 {
		t.Errorf("parent body too short: %d chars (min 2500)", bodyChars)
	}
	if bodyChars > 7000 {
		t.Errorf("parent body too long: %d chars (max 7000)", bodyChars)
	}
	if !strings.Contains(body, "Nested reference catalog") {
		t.Error("parent body missing 'Nested reference catalog' heading")
	}
	if !strings.Contains(body, "Routing table") {
		t.Error("parent body missing 'Routing table' heading")
	}
}

// TestPresetSkillRouter_ParentDoesNotDuplicateChildren verifies no >=48-char
// normalized child body line appears verbatim in the parent body.
func TestPresetSkillRouter_ParentDoesNotDuplicateChildren(t *testing.T) {
	parentData, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	parentText := string(parentData)
	parentDash := strings.Index(parentText, "\n---")
	if parentDash < 0 {
		t.Fatal("parent missing closing ---")
	}
	parentBody := strings.TrimPrefix(parentText[parentDash+4:], "\n")

	err = fs.WalkDir(skillsFS, "skills/lingtai-preset-skill/reference", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "/SKILL.md") {
			return nil
		}
		data, err := fs.ReadFile(skillsFS, path)
		if err != nil {
			return err
		}
		text := string(data)
		secondDash := strings.Index(text, "\n---")
		if secondDash < 0 {
			return nil
		}
		childBody := strings.TrimPrefix(text[secondDash+4:], "\n")

		rel := strings.TrimPrefix(path, "skills/lingtai-preset-skill/reference/")
		childName := strings.SplitN(rel, "/", 2)[0]

		for _, line := range strings.Split(childBody, "\n") {
			normalized := strings.TrimSpace(line)
			// Strip common markdown prefixes
			normalized = strings.TrimPrefix(normalized, "- ")
			normalized = strings.TrimPrefix(normalized, "* ")
			normalized = strings.TrimPrefix(normalized, "> ")
			normalized = strings.TrimPrefix(normalized, "# ")
			// Strip backtick edges
			normalized = strings.Trim(normalized, "`")
			if len(normalized) >= 48 && strings.Contains(parentBody, normalized) {
				t.Errorf("child %s: verbatim line in parent body: %.80s...", childName, normalized)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestCodexPoolManualDocumentsClassifiedContract verifies the codex-pool
// child body documents the dedicated preset, v1/v2 exact-model classification
// from kernel #841 and TUI #612, and links official sources without reviving
// cancelled convergence proposals.
func TestCodexPoolManualDocumentsClassifiedContract(t *testing.T) {
	data, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/reference/codex-pool/SKILL.md")
	if err != nil {
		t.Fatalf("cannot read codex-pool child: %v", err)
	}
	body := string(data)

	// Must mention the dedicated codex-pool preset
	if !strings.Contains(body, "codex-pool") {
		t.Error("codex-pool child does not mention codex-pool preset")
	}

	// Must document v1/v2 pool shapes
	if !strings.Contains(body, "version") || !strings.Contains(body, "models") {
		t.Error("codex-pool child missing v1/v2 pool shape documentation")
	}

	// Must reference kernel #841 and TUI #612
	if !strings.Contains(body, "#841") {
		t.Error("codex-pool child missing kernel #841 reference")
	}
	if !strings.Contains(body, "#612") {
		t.Error("codex-pool child missing TUI #612 reference")
	}

	// Must link official OpenAI/Codex sources
	if !strings.Contains(body, "developers.openai.com/codex") {
		t.Error("codex-pool child missing official OpenAI/Codex source link")
	}

	// Must link LingTai pool code/tests
	if !strings.Contains(body, "codex_pool_store") {
		t.Error("codex-pool child missing reference to TUI pool store code")
	}

	// Must NOT revive cancelled convergence proposals
	if strings.Contains(body, "convergence") {
		t.Error("codex-pool child references cancelled convergence proposals")
	}

	// Must mention exact-model classification
	if !strings.Contains(body, "exact") {
		t.Error("codex-pool child missing exact-model classification reference")
	}
}

// TestBundledSkillsHaveMaintenance verifies all 70 SKILL.md files have the
// exact canonical maintenance sentence.
func TestBundledSkillsHaveMaintenance(t *testing.T) {
	count := 0
	err := fs.WalkDir(skillsFS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "/SKILL.md") {
			return nil
		}
		count++
		data, err := fs.ReadFile(skillsFS, path)
		if err != nil {
			return err
		}
		body := string(data)
		if !strings.HasPrefix(body, "---\n") {
			t.Errorf("%s missing YAML frontmatter", path)
			return nil
		}
		end := strings.Index(body[4:], "\n---")
		if end < 0 {
			t.Errorf("%s missing closing YAML frontmatter delimiter", path)
			return nil
		}
		frontmatter := body[:4+end]
		maintRE := regexp.MustCompile(`(?m)^maintenance:\s*"?(.+?)"?\s*$`)
		match := maintRE.FindStringSubmatch(frontmatter)
		if match == nil {
			t.Errorf("%s missing maintenance frontmatter", path)
			return nil
		}
		value := strings.TrimSpace(match[1])
		if value != wantMaintenance {
			t.Errorf("%s maintenance value does not match canonical sentence.\n got: %.80s...\nwant: %.80s...", path, value, wantMaintenance)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if count != 70 {
		t.Fatalf("checked %d bundled skill SKILL.md files, want 70", count)
	}
}
