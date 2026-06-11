package preset

import (
	"io/fs"
	"strings"
	"testing"
)

// TestPreMoltLingtaiGuidance locks in the strengthened pre-molt guidance from
// issue #175: the bundled covenant, procedures, and tutorial-guide assets must
// operationally prompt agents to update lingtai after identity-changing work,
// and must say that pad/knowledge/skills are not a substitute for lingtai.
// (The detailed molt template now lives in the resident psyche-manual, not in a
// bundled TUI utility skill.)
//
// Before this issue the guidance only asked the abstract "did who I am change?"
// question, which agents skipped under molt pressure while dutifully tending
// the concrete stores. These assertions guard against regressing back to the
// abstract-only wording.
func TestPreMoltLingtaiGuidance(t *testing.T) {
	read := func(t *testing.T, fsys fs.FS, path string) string {
		t.Helper()
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(data)
	}

	// Every covenant language must carry the concrete identity-change prompt
	// and the "not a substitute" warning. We assert on a stable, language-
	// independent anchor — the literal `lingtai` token and the explicit
	// "before"/"molt" coupling are present in the English covenant; the
	// localized files are checked for the structural cue that the question is
	// now operational (the example-driven dimensions list).
	t.Run("english covenant is operational", func(t *testing.T) {
		body := read(t, covenantFS, "covenant/en/covenant.md")
		mustContainAll(t, "en covenant", body,
			"operating style",
			"safety posture",
			"trust model",
			"before",
			"substitute for lingtai",
		)
	})

	// The localized covenants must still mention lingtai before molt and the
	// non-substitution rule. We assert on the dimension list, which is
	// translated but structurally parallel, plus the `lingtai` token.
	for _, c := range []struct{ name, path, dims, beforeMolt string }{
		{"zh covenant", "covenant/zh/covenant.md", "安全姿态", "凝蜕之前"},
		{"wen covenant", "covenant/wen/covenant.md", "守安之姿", "未蜕之先"},
	} {
		t.Run(c.name+" is operational", func(t *testing.T) {
			body := read(t, covenantFS, c.path)
			mustContainAll(t, c.name, body,
				"lingtai",    // the literal store name survives translation
				c.dims,       // "safety posture" dimension, localized
				c.beforeMolt, // "before you molt", localized
			)
		})
	}

	t.Run("procedures couple lingtai with molt", func(t *testing.T) {
		body := read(t, proceduresFS, "procedures/procedures.md")
		mustContainAll(t, "procedures", body,
			"lingtai",
			"safety posture",
			"not a\nsubstitute", // wrapped across a line in the prose
		)
	})

	t.Run("tutorial molt ritual names lingtai first", func(t *testing.T) {
		body := read(t, skillsFS,
			"skills/lingtai-tutorial-guide/reference/memory-and-molt/SKILL.md")
		mustContainAll(t, "tutorial memory-and-molt", body,
			"skip most often",
			"safety posture",
			"not a substitute",
		)
	})
}

func TestEnglishTutorialAssetsContainNoCJKPromptCues(t *testing.T) {
	read := func(t *testing.T, fsys fs.FS, path string) string {
		t.Helper()
		data, err := fs.ReadFile(fsys, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return string(data)
	}

	assertNoCJK := func(t *testing.T, label, body string) {
		t.Helper()
		for _, r := range body {
			if isCJK(r) {
				t.Fatalf("%s: English tutorial asset contains CJK rune %q", label, r)
			}
		}
	}

	assertNoCJK(t, "tutorial English comment", read(t, recipeAssetsFS, "recipe_assets/examples/tutorial/.recipe/comment/comment.md"))
	assertNoCJK(t, "tutorial English greet", read(t, recipeAssetsFS, "recipe_assets/examples/tutorial/.recipe/greet/greet.md"))

	if err := fs.WalkDir(skillsFS, "skills/lingtai-tutorial-guide", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		assertNoCJK(t, path, read(t, skillsFS, path))
		return nil
	}); err != nil {
		t.Fatalf("walk tutorial guide: %v", err)
	}
}

func TestPrincipleAssetsAreNotTriplicated(t *testing.T) {
	read := func(t *testing.T, path string) string {
		t.Helper()
		data, err := fs.ReadFile(principleFS, path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		return strings.TrimSpace(string(data))
	}

	for _, path := range []string{
		"principle/en/principle.md",
		"principle/zh/principle.md",
		"principle/wen/principle.md",
	} {
		t.Run(path, func(t *testing.T) {
			body := read(t, path)
			paragraphs := strings.Split(body, "\n\n")
			if len(paragraphs) != 1 {
				t.Fatalf("%s: expected exactly one principle paragraph, got %d", path, len(paragraphs))
			}
			if strings.Contains(body, "three times") || strings.Contains(body, "说三次") {
				t.Fatalf("%s: principle still carries the old triple-repetition cue", path)
			}
		})
	}
}

func isCJK(r rune) bool {
	return (r >= '\u4e00' && r <= '\u9fff') ||
		(r >= '\u3400' && r <= '\u4dbf') ||
		(r >= '\uf900' && r <= '\ufaff')
}

func mustContainAll(t *testing.T, label, body string, needles ...string) {
	t.Helper()
	for _, n := range needles {
		if !strings.Contains(body, n) {
			t.Errorf("%s: expected to contain %q, but it did not", label, n)
		}
	}
}
