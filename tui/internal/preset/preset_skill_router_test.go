package preset

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const wantMaintenance = `If you find stale or incorrect information here, use the lingtai-issue-report skill to assemble evidence and obtain per-issue human consent before filing an issue. Never include secrets, credentials, tokens, or private paths.`

var (
	maintenanceRE  = regexp.MustCompile(`(?m)^maintenance:\s*(.+?)\s*$`)
	catalogEntryRE = regexp.MustCompile(`(?m)^- name:\s*(\S+)\n  location:\s*(\S+)$`)
	catalogNameRE  = regexp.MustCompile(`(?m)^- name:`)
	relatedFileRE  = regexp.MustCompile(`^  - ([^\s#].+)$`)
)

// frontmatter returns only the YAML frontmatter, reporting malformed or short
// files before attempting to slice past the opening delimiter.
func frontmatter(path string, data []byte) (string, error) {
	body := string(data)
	if !strings.HasPrefix(body, "---\n") {
		return "", fmt.Errorf("%s missing opening frontmatter delimiter", path)
	}
	end := strings.Index(body[4:], "\n---")
	if end < 0 {
		return "", fmt.Errorf("%s missing closing frontmatter delimiter", path)
	}
	return body[:4+end], nil
}

func maintenanceValue(frontmatter string) (string, bool) {
	match := maintenanceRE.FindStringSubmatch(frontmatter)
	if match == nil {
		return "", false
	}
	value := strings.TrimSpace(match[1])
	if len(value) >= 2 && value[0] == '"' && value[len(value)-1] == '"' {
		value = value[1 : len(value)-1]
	}
	return value, true
}

// TestPresetSkillRouter_BuiltinBijection keeps the source preset list, the
// embedded manuals, the parent router, and extracted utility tree aligned.
func TestPresetSkillRouter_BuiltinBijection(t *testing.T) {
	want := map[string]bool{}
	for _, p := range BuiltinPresets() {
		if want[p.Name] {
			t.Errorf("BuiltinPresets() contains duplicate name %q", p.Name)
		}
		want[p.Name] = true
	}

	children := map[string]bool{}
	err := fs.WalkDir(skillsFS, "skills/lingtai-preset-skill/reference", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, "/SKILL.md") {
			return nil
		}
		rel := strings.TrimPrefix(path, "skills/lingtai-preset-skill/reference/")
		parts := strings.SplitN(rel, "/", 2)
		if len(parts) == 2 && parts[1] == "SKILL.md" {
			if children[parts[0]] {
				t.Errorf("embedded children contains duplicate %q", parts[0])
			}
			children[parts[0]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	assertSameNames(t, "embedded children", want, children)

	parentData, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/SKILL.md")
	if err != nil {
		t.Fatalf("read parent: %v", err)
	}
	_, err = frontmatter("parent", parentData)
	if err != nil {
		t.Fatal(err)
	}
	parent := string(parentData)
	if !strings.Contains(parent, "When `BuiltinPresets()` gains a new template name") {
		t.Error("parent does not state the new-preset maintenance contract")
	}

	catalogNames := map[string]bool{}
	catalogLocations := map[string]bool{}
	catalogEntries := catalogEntryRE.FindAllStringSubmatch(parent, -1)
	if len(catalogEntries) != len(catalogNameRE.FindAllString(parent, -1)) {
		t.Errorf("parent catalog has malformed or short name/location entries")
	}
	for _, match := range catalogEntries {
		name, location := match[1], match[2]
		if !strings.HasPrefix(name, "preset-skill-") {
			t.Errorf("parent catalog has unexpected name %q", name)
		}
		child := strings.TrimPrefix(name, "preset-skill-")
		wantLocation := "reference/" + child + "/SKILL.md"
		if location != wantLocation {
			t.Errorf("parent catalog pairs name %q with location %q, want %q", name, location, wantLocation)
		}
		if catalogNames[name] {
			t.Errorf("parent catalog duplicates name %q", name)
		}
		if catalogLocations[location] {
			t.Errorf("parent catalog duplicates location %q", location)
		}
		catalogNames[name] = true
		catalogLocations[location] = true
	}
	wantCatalogNames := map[string]bool{}
	for name := range want {
		wantCatalogNames["preset-skill-"+name] = true
	}
	assertSameNames(t, "parent catalog names", wantCatalogNames, catalogNames)
	wantLocations := map[string]bool{}
	for name := range want {
		wantLocations["reference/"+name+"/SKILL.md"] = true
	}
	assertSameNames(t, "parent catalog locations", wantLocations, catalogLocations)

	globalDir := t.TempDir()
	PopulateBundledLibrary(globalDir)
	referenceDir := filepath.Join(globalDir, "utilities", "lingtai-preset-skill", "reference")
	entries, err := os.ReadDir(referenceDir)
	if err != nil {
		t.Fatal(err)
	}
	extracted := map[string]bool{}
	for _, entry := range entries {
		if !entry.IsDir() {
			t.Errorf("extracted reference has unexpected file %q", entry.Name())
			continue
		}
		extracted[entry.Name()] = true
		if _, err := os.Stat(filepath.Join(referenceDir, entry.Name(), "SKILL.md")); err != nil {
			t.Errorf("extracted child %q: %v", entry.Name(), err)
		}
	}
	assertSameNames(t, "extracted children", want, extracted)
	if !BundledSkillNames()["lingtai-preset-skill"] {
		t.Error("parent router is not a bundled skill")
	}
}

func assertSameNames(t *testing.T, label string, want, got map[string]bool) {
	t.Helper()
	for name := range want {
		if !got[name] {
			t.Errorf("%s missing %q", label, name)
		}
	}
	for name := range got {
		if !want[name] {
			t.Errorf("%s has unexpected %q", label, name)
		}
	}
}

func TestPresetSkillRouter_ChildMetadata(t *testing.T) {
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
		fm, err := frontmatter(path, data)
		if err != nil {
			t.Error(err)
			return nil
		}
		rel := strings.TrimPrefix(path, "skills/lingtai-preset-skill/reference/")
		name := strings.SplitN(rel, "/", 2)[0]
		wantName := "preset-skill-" + name
		if !regexp.MustCompile(`(?m)^name:\s*` + regexp.QuoteMeta(wantName) + `\s*$`).MatchString(fm) {
			t.Errorf("%s has no name %q", path, wantName)
		}
		value, ok := maintenanceValue(fm)
		if !ok || value != wantMaintenance {
			t.Errorf("%s has maintenance %q, want %q", path, value, wantMaintenance)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestPresetSkillRouter_AllBundledMaintenance(t *testing.T) {
	err := fs.WalkDir(skillsFS, "skills", func(path string, d fs.DirEntry, err error) error {
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
		fm, err := frontmatter(path, data)
		if err != nil {
			t.Error(err)
			return nil
		}
		value, ok := maintenanceValue(fm)
		if !ok {
			t.Errorf("%s missing maintenance frontmatter", path)
		} else if value != wantMaintenance {
			t.Errorf("%s maintenance value %q, want %q", path, value, wantMaintenance)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinPresetVisionWiring(t *testing.T) {
	presets := map[string]Preset{}
	for _, p := range BuiltinPresets() {
		presets[p.Name] = p
	}

	geminiCaps, ok := presets["gemini"].Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatal("gemini capabilities has unexpected type")
	}
	geminiVision, ok := geminiCaps["vision"].(map[string]interface{})
	if !ok {
		t.Fatal("gemini must expose a vision capability")
	}
	if got := geminiVision["provider"]; got != "gemini" {
		t.Fatalf("gemini vision provider = %#v, want gemini", got)
	}
	if got := geminiVision["api_key_env"]; got != "GEMINI_API_KEY" {
		t.Fatalf("gemini vision api_key_env = %#v, want GEMINI_API_KEY", got)
	}

	zhipuCaps, ok := presets["zhipu"].Manifest["capabilities"].(map[string]interface{})
	if !ok {
		t.Fatal("zhipu capabilities has unexpected type")
	}
	if _, ok := zhipuCaps["vision"]; ok {
		t.Fatal("zhipu must not expose a default vision capability for text-only GLM-5.2")
	}
}

func TestPresetVisionManualContracts(t *testing.T) {
	readChild := func(t *testing.T, name string) string {
		t.Helper()
		data, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/reference/"+name+"/SKILL.md")
		if err != nil {
			t.Fatalf("read %s manual: %v", name, err)
		}
		return string(data)
	}

	gemini := readChild(t, "gemini")
	for _, want := range []string{"gemini-3-flash-preview", "GEMINI_API_KEY", "explicit LingTai `vision` capability"} {
		if !strings.Contains(gemini, want) {
			t.Errorf("gemini manual missing %q", want)
		}
	}

	zhipu := readChild(t, "zhipu")
	for _, want := range []string{"GLM-5.2", "@z_ai/mcp-server", "GLM-4.6V", "ZHIPU_API_KEY", "Z_AI_API_KEY", "Z_AI_MODE", "5-hour prompt pool"} {
		if !strings.Contains(zhipu, want) {
			t.Errorf("zhipu manual missing %q", want)
		}
	}

	retiredModel := strings.Join([]string{"mimo", "v2", "flash"}, "-")
	if mimo := readChild(t, "mimo"); strings.Contains(mimo, retiredModel) {
		t.Fatalf("mimo manual still mentions retired model %q", retiredModel)
	}
}

func TestPresetSkillRouter_ParentMaintenanceAndRelated(t *testing.T) {
	data, err := fs.ReadFile(skillsFS, "skills/lingtai-preset-skill/SKILL.md")
	if err != nil {
		t.Fatal(err)
	}
	fm, err := frontmatter("parent", data)
	if err != nil {
		t.Fatal(err)
	}
	value, ok := maintenanceValue(fm)
	if !ok || value != wantMaintenance {
		t.Errorf("parent maintenance value %q, want %q", value, wantMaintenance)
	}

	seenRelated := false
	relatedCount := 0
	for _, line := range strings.Split(fm, "\n") {
		if strings.HasPrefix(line, "related_files:") {
			seenRelated = true
			continue
		}
		if !seenRelated {
			continue
		}
		match := relatedFileRE.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		rel := match[1]
		relatedCount++
		if rel == "" {
			t.Error("parent related_files contains an empty path")
			continue
		}
		if _, err := os.Stat(filepath.Join("..", "..", "..", rel)); err != nil {
			t.Errorf("related_files entry %q does not resolve to a repo file: %v", rel, err)
		}
	}
	if !seenRelated {
		t.Fatal("parent missing related_files field")
	}
	if relatedCount == 0 {
		t.Fatal("parent related_files field is empty")
	}
}
