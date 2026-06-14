package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// TestBuildSkillFolderEntries_RecipeSkill verifies the drill-in catalog
// against the real lingtai-recipe skill folder, which has SKILL.md at root
// plus reference/, scripts/, and assets/ subdirectories.
func TestBuildSkillFolderEntries_RecipeSkill(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-recipe")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-recipe missing?")
	}

	// SKILL.md must be first.
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	// Group labels should include references, scripts, assets.
	groups := make(map[string]int)
	for _, e := range entries {
		if e.Group != "" {
			groups[e.Group]++
		}
	}
	for _, want := range []string{"reference", "scripts", "assets"} {
		if groups[want] == 0 {
			t.Errorf("expected non-empty group %q, groups=%v", want, groups)
		}
	}

	// No hidden entries (.pytest_cache) should leak in.
	for _, e := range entries {
		if strings.Contains(e.Label, ".pytest_cache") {
			t.Errorf("hidden entry leaked: %s", e.Label)
		}
	}

	// Python scripts should be pre-rendered as fenced python blocks.
	foundPy := false
	for _, e := range entries {
		if strings.HasSuffix(e.Label, ".py") {
			foundPy = true
			if !strings.Contains(e.Content, "```python") {
				t.Errorf("python entry %q not fenced as python: %q", e.Label, firstLineOf(e.Content))
			}
		}
	}
	if !foundPy {
		t.Error("no python scripts found in lingtai-recipe — did the fixture change?")
	}
}

func firstLineOf(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// TestBuildSkillFolderEntries_SwissKnifeNestedReferences verifies that the
// shipped swiss-knife utility skill is a router with nested reference SKILL.md
// files, and that the TUI drill-in view exposes those nested files under the
// reference group without promoting them to sibling top-level utility folders.
func TestBuildSkillFolderEntries_SwissKnifeNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "swiss-knife")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is swiss-knife missing?")
	}
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"bash-cli-harnesses",
		"bash-manual reference/bash-*/SKILL.md",
		"reference/minimax-cli/SKILL.md",
		"reference/vision/SKILL.md",
		"reference/listen/SKILL.md",
		"reference/academic-research/SKILL.md",
		"reference/dj/SKILL.md",
		"reference/token-usage/SKILL.md",
		"reference/html-report/SKILL.md",
		"reference/xiaomi-mimo/SKILL.md",
		"reference/zhipu-coding-plan/SKILL.md",
		"reference/find-something-to-do/SKILL.md",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("swiss-knife root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"html-report/SKILL.md",
		"html-report/assets/template.html",
		"vision/SKILL.md",
		"vision/scripts/describe.py",
		"vision/reference/local-models.md",
		"listen/SKILL.md",
		"listen/scripts/transcribe.py",
		"listen/scripts/appreciate.py",
		"academic-research/SKILL.md",
		"academic-research/scripts/fetch_paper.py",
		"academic-research/reference/api-arxiv.md",
		"dj/SKILL.md",
		"find-something-to-do/SKILL.md",
		"token-usage/SKILL.md",
		"token-usage/scripts/cost_report.py",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested swiss-knife entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	djBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "dj", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(djBodyBytes), "Nested swiss-knife reference for composing one music track") {
		t.Error("nested dj child should identify itself as a nested swiss-knife reference")
	}

	findBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "find-something-to-do", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(findBodyBytes), "Nested swiss-knife reference for idle curiosity practice") {
		t.Error("nested find-something-to-do child should identify itself as a nested swiss-knife reference")
	}

	visionBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "vision", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(visionBodyBytes), "Nested swiss-knife reference for image understanding") {
		t.Error("nested vision child should identify itself as a nested swiss-knife reference for image understanding")
	}
	// Cross-references to the sibling minimax-cli reference must use the
	// relative sibling path now that both live under reference/.
	visionBody := string(visionBodyBytes)
	if !strings.Contains(visionBody, "../minimax-cli/SKILL.md") {
		t.Error("nested vision child should point at the sibling minimax-cli reference by relative path")
	}
	if strings.Contains(visionBody, "bash python") || strings.Contains(visionBody, "python scripts/") || strings.Contains(visionBody, "${CLAUDE_SKILL_DIR}") {
		t.Error("nested vision child should use portable <skill-path>/scripts examples")
	}

	listenBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "listen", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	listenBody := string(listenBodyBytes)
	if !strings.Contains(listenBody, "Nested swiss-knife reference for local-only audio analysis") {
		t.Error("nested listen child should identify itself as a nested swiss-knife reference for local audio analysis")
	}
	if strings.Contains(listenBody, "bash python") || strings.Contains(listenBody, "python scripts/") || strings.Contains(listenBody, "${CLAUDE_SKILL_DIR}") || strings.Contains(listenBody, "media-creation") {
		t.Error("nested listen child should use portable <skill-path>/scripts examples and current sibling media routes")
	}

	academicBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "academic-research", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	academicBody := string(academicBodyBytes)
	if !strings.Contains(academicBody, "Nested swiss-knife reference") {
		t.Error("nested academic-research child should identify itself as a nested swiss-knife reference")
	}
	if strings.Contains(academicBody, "${CLAUDE_SKILL_DIR}") || strings.Contains(academicBody, "python scripts/") {
		t.Error("nested academic-research child should use portable <skill-path>/scripts examples")
	}
}

func TestBuildSkillFolderEntries_MinimaxCliCanonicalReference(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillsRoot := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills")

	// The redundant top-level minimax-cli skill was removed; minimax-cli now lives
	// only as a swiss-knife nested reference. Guard that the top-level copy is gone.
	if _, err := os.Stat(filepath.Join(skillsRoot, "minimax-cli")); err == nil {
		t.Error("top-level minimax-cli skill should no longer exist; it lives under swiss-knife/reference/minimax-cli")
	}

	canonicalBodyBytes, err := os.ReadFile(filepath.Join(skillsRoot, "swiss-knife", "reference", "minimax-cli", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	canonicalBody := string(canonicalBodyBytes)
	for _, want := range []string{
		"Nested swiss-knife reference for the MiniMax `mmx` CLI",
		"## 3. Discover credentials without leaking them",
		"~/.lingtai-tui/presets/saved/",
		"Do **not** hardcode an unverified host",
	} {
		if !strings.Contains(canonicalBody, want) {
			t.Errorf("canonical swiss-knife minimax-cli reference missing %q", want)
		}
	}
}

func TestMinimaxCliPresetDiscoverySnippetPreservesHTTPSBaseURL(t *testing.T) {
	python, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not available")
	}

	_, thisFile, _, _ := runtime.Caller(0)
	skillPath := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "swiss-knife", "reference", "minimax-cli", "SKILL.md")
	bodyBytes, err := os.ReadFile(skillPath)
	if err != nil {
		t.Fatal(err)
	}
	body := string(bodyBytes)
	if strings.Contains(body, `re.sub(r"//`) || strings.Contains(body, `re.sub(r'//`) {
		t.Fatal("MiniMax preset-discovery snippet must not use regex // stripping; it corrupts https:// URLs")
	}

	marker := "List candidate presets (prints slot names and base URLs only, never secret values):"
	markerIdx := strings.Index(body, marker)
	if markerIdx < 0 {
		t.Fatal("missing MiniMax candidate-preset snippet marker")
	}
	start := strings.Index(body[markerIdx:], "python3 - <<'PY'\n")
	if start < 0 {
		t.Fatal("missing MiniMax candidate-preset python heredoc")
	}
	start += markerIdx + len("python3 - <<'PY'\n")
	end := strings.Index(body[start:], "\nPY\n```")
	if end < 0 {
		t.Fatal("missing end of MiniMax candidate-preset python heredoc")
	}
	script := body[start : start+end]

	home := t.TempDir()
	presetDir := filepath.Join(home, ".lingtai-tui", "presets", "saved")
	if err := os.MkdirAll(presetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	preset := `{
  // JSONC comment before the LLM block
  "manifest": {
    "llm": {
      "provider": "minimax",
      "api_key_env": "MINIMAX_CN_1_API_KEY",
      "base_url": "https://api.minimaxi.com/anthropic"
    }
  }
}`
	if err := os.WriteFile(filepath.Join(presetDir, "minimax.jsonc"), []byte(preset), 0o644); err != nil {
		t.Fatal(err)
	}

	cmd := exec.Command(python, "-c", script)
	cmd.Env = append(os.Environ(), "HOME="+home)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("MiniMax preset-discovery snippet failed: %v\n%s", err, out)
	}
	got := string(out)
	for _, want := range []string{
		"presets/saved/minimax.jsonc",
		"slot=MINIMAX_CN_1_API_KEY",
		"region=CN",
		"base_url=https://api.minimaxi.com/anthropic",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("MiniMax preset-discovery snippet output missing %q; got:\n%s", want, got)
		}
	}
}

func TestBuildSkillFolderEntries_WebBrowsingNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "web-browsing")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is web-browsing missing?")
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"reference/tier-quick-refs/SKILL.md",
		"reference/routing-and-sites/SKILL.md",
		"reference/maintenance-bundles/SKILL.md",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("web-browsing root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"tier-quick-refs/SKILL.md",
		"routing-and-sites/SKILL.md",
		"maintenance-bundles/SKILL.md",
		"tier-0-pdf.md",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested web-browsing entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "tier-quick-refs", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childBodyBytes), "Nested web-browsing reference") {
		t.Error("nested tier quick refs child should identify itself as a nested web-browsing reference")
	}
}

func TestBuildSkillFolderEntries_DevGuideNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-dev-guide")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-dev-guide missing?")
	}
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"```yaml",
		"- name: dev-guide-architecture",
		"- name: dev-guide-setup",
		"- name: dev-guide-contributing",
		"- name: dev-guide-gotchas",
		"- name: dev-guide-releasing",
		"- name: dev-guide-release-workflow",
		"- name: dev-guide-debug-troubleshoot",
		"- name: dev-guide-security-audit",
		"- name: dev-guide-network-governance",
		"- name: dev-guide-runtime-self-check",
		"- name: dev-guide-pr-review-deliverables",
		"- name: dev-guide-skill-stewardship",
		"Routing table",
		"reference/architecture/SKILL.md",
		"reference/setup/SKILL.md",
		"reference/contributing/SKILL.md",
		"reference/gotchas/SKILL.md",
		"reference/releasing/SKILL.md",
		"reference/release-workflow/SKILL.md",
		"reference/debug-troubleshoot/SKILL.md",
		"reference/security-audit/SKILL.md",
		"reference/network-governance/SKILL.md",
		"reference/runtime-self-check/SKILL.md",
		"reference/pr-review-deliverables/SKILL.md",
		"reference/skill-stewardship/SKILL.md",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("lingtai-dev-guide root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"architecture/SKILL.md",
		"setup/SKILL.md",
		"contributing/SKILL.md",
		"gotchas/SKILL.md",
		"releasing/SKILL.md",
		"release-workflow/SKILL.md",
		"release-workflow/assets/release-blog-template.md",
		"debug-troubleshoot/SKILL.md",
		"security-audit/SKILL.md",
		"network-governance/SKILL.md",
		"runtime-self-check/SKILL.md",
		"pr-review-deliverables/SKILL.md",
		"skill-stewardship/SKILL.md",
		"release-html-log-template.html",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested lingtai-dev-guide entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "architecture", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childBodyBytes), "Nested lingtai-dev-guide reference") {
		t.Error("nested architecture child should identify itself as a nested lingtai-dev-guide reference")
	}

	releaseWorkflowBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "release-workflow", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	releaseWorkflowBody := string(releaseWorkflowBytes)
	for _, want := range []string{
		"Nested lingtai-dev-guide reference",
		"assets/release-blog-template.md",
		"strict post-tag delta",
		"real release surfaces",
		"ReleaseDetail.astro",
		"GitHub Releases",
	} {
		if !strings.Contains(releaseWorkflowBody, want) {
			t.Errorf("release workflow child missing %q", want)
		}
	}

	// The three consolidated dev-guide references must each identify themselves
	// as nested references and carry the de-privatization / safe-reporting
	// guidance that the skill audit asked them to consolidate.
	runtimeBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "runtime-self-check", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	runtimeBody := string(runtimeBytes)
	for _, want := range []string{
		"Nested lingtai-dev-guide reference",
		"lingtai.__file__",
		"editable",
		"<REDACTED>",
		"<your-lingtai-checkout>",
	} {
		if !strings.Contains(runtimeBody, want) {
			t.Errorf("runtime-self-check child missing %q", want)
		}
	}

	prReviewBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "pr-review-deliverables", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	prReviewBody := string(prReviewBytes)
	for _, want := range []string{
		"Nested lingtai-dev-guide reference",
		"git diff --check",
		"explainer",
		"Maintainer authorization boundaries",
		"reference/release-workflow/SKILL.md",
	} {
		if !strings.Contains(prReviewBody, want) {
			t.Errorf("pr-review-deliverables child missing %q", want)
		}
	}

	stewardshipBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "skill-stewardship", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	stewardshipBody := string(stewardshipBytes)
	for _, want := range []string{
		"Nested lingtai-dev-guide reference",
		"router",
		"nested-reference",
		"skills-manual",
		"de-priv",
	} {
		if !strings.Contains(stewardshipBody, want) {
			t.Errorf("skill-stewardship child missing %q", want)
		}
	}
}

func TestBuildSkillFolderEntries_RecipeNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-recipe")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-recipe missing?")
	}
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"```yaml",
		"- name: recipe-format-reference",
		"- name: recipe-export-flow",
		"Routing table",
		"reference/recipe-format/SKILL.md",
		"reference/export-recipe/SKILL.md",
		"gitignore.template",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("lingtai-recipe root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"recipe-format/SKILL.md",
		"export-recipe/SKILL.md",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested lingtai-recipe entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "recipe-format", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childBodyBytes), "Nested lingtai-recipe reference") {
		t.Error("nested recipe-format child should identify itself as a nested lingtai-recipe reference")
	}
}

func TestBuildSkillFolderEntries_TutorialGuideNestedReferences(t *testing.T) {
	_, thisFile, _, _ := runtime.Caller(0)
	skillDir := filepath.Join(filepath.Dir(thisFile), "..", "preset", "skills", "lingtai-tutorial-guide")

	entries := buildSkillFolderEntries(skillDir)
	if len(entries) == 0 {
		t.Fatal("no entries; is lingtai-tutorial-guide missing?")
	}
	if entries[0].Label != "SKILL.md" {
		t.Errorf("first entry = %q, want SKILL.md", entries[0].Label)
	}

	rootBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	rootBody := string(rootBodyBytes)
	for _, want := range []string{
		"Nested reference catalog",
		"Routing table",
		"name: tutorial-guide-orientation",
		"name: tutorial-guide-agent-runtime",
		"name: tutorial-guide-communication",
		"name: tutorial-guide-memory-and-molt",
		"name: tutorial-guide-capabilities",
		"name: tutorial-guide-operations-and-graduation",
		"location: reference/orientation/SKILL.md",
		"location: reference/agent-runtime/SKILL.md",
		"location: reference/communication/SKILL.md",
		"location: reference/memory-and-molt/SKILL.md",
		"location: reference/capabilities/SKILL.md",
		"location: reference/operations-and-graduation/SKILL.md",
	} {
		if !strings.Contains(rootBody, want) {
			t.Errorf("tutorial-guide root missing %q", want)
		}
	}

	labels := make(map[string]MarkdownEntry)
	for _, e := range entries {
		labels[e.Label] = e
	}
	for _, want := range []string{
		"orientation/SKILL.md",
		"agent-runtime/SKILL.md",
		"communication/SKILL.md",
		"memory-and-molt/SKILL.md",
		"capabilities/SKILL.md",
		"operations-and-graduation/SKILL.md",
	} {
		e, ok := labels[want]
		if !ok {
			t.Fatalf("missing nested tutorial-guide entry %q", want)
		}
		if e.Group != "reference" {
			t.Errorf("entry %q group = %q, want reference", want, e.Group)
		}
	}

	childBodyBytes, err := os.ReadFile(filepath.Join(skillDir, "reference", "orientation", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(childBodyBytes), "Nested tutorial-guide reference") {
		t.Error("nested orientation child should identify itself as a nested tutorial-guide reference")
	}
}
