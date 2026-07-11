// Data-driven fixtures for the current-tree architecture-document validator. Each
// case builds a compact t.TempDir mini-repo and runs the SAME validateRepository as
// the real repo: wantErr "" means fully valid, otherwise some violation must contain
// that substring. These durably exercise the current root invariants — strict
// frontmatter, related_files path rules, symlink containment, reciprocity, required
// edges, entry routing — and the zero-child fail-closed sentinel, none of which the
// real (zero-child) repository exercises negatively.
package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestArchitectureFixtures(t *testing.T) {
	cases := []struct {
		name, wantErr string
		build         func(t *testing.T, root string)
	}{
		{"valid zero-child root", "", func(t *testing.T, root string) { base(t, root) }},
		{"tab-indented root maintenance rejected", "tab-indented", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomy(rootRelated, "|\n\ttabbed"))
		}},
		{"empty root maintenance rejected", "maintenance must be non-empty", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomy(rootRelated, "|\n  "))
		}},
		{"non-positive root version rejected", "contract_version", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "CONTRACT.md", rootContract("0", rootRelated))
		}},
		{"duplicate root related_files entry rejected", "duplicate related_files entry", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomy(withExtra("README.md"), goodMaint)) // README.md already in rootRelated
		}},
		{"illegal root path segment rejected", "illegal path segment", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomy(withExtra("../escape.go"), goodMaint))
		}},
		{"missing root related_files target rejected", "does not exist", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomy(withExtra("does/not/exist.go"), goodMaint))
		}},
		{"missing root reciprocity rejected", "must list ANATOMY.md", func(t *testing.T, root string) {
			base(t, root)
			// Root contract omits its ANATOMY.md back-link (rootRelated has no pair edge; nothing prepends it).
			writeRel(t, root, "CONTRACT.md", "---\nname: component-contract-convention\ncontract_version: 1\n"+
				frontmatterList(rootRelated)+"maintenance: |\n  root contract.\n---\n"+body("root", rootContractHeadings))
		}},
		{"missing required root edge rejected", "must list \"dev-guide-skill/SKILL.md\"", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomy(without(rootRelated, "dev-guide-skill/SKILL.md"), goodMaint))
		}},
		{"related_files symlink outside repo rejected", "outside the repository", func(t *testing.T, root string) {
			out := filepath.Join(t.TempDir(), "escape.go")
			write(t, out, "package x\n")
			if os.Symlink(out, filepath.Join(root, "escape.go")) != nil {
				t.Skip("symlink unsupported")
			}
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomy(withExtra("escape.go"), goodMaint))
		}},
		{"related_files symlink inside repo accepted", "", func(t *testing.T, root string) {
			base(t, root)
			write(t, filepath.Join(root, "real.go"), "package x\n")
			if os.Symlink(filepath.Join(root, "real.go"), filepath.Join(root, "alias.go")) != nil {
				t.Skip("symlink unsupported")
			}
			writeRel(t, root, "ANATOMY.md", rootAnatomy(withExtra("real.go", "alias.go"), goodMaint))
		}},
		{"negative routing prose without a link rejected", "must contain a Markdown link to dev-guide-skill/SKILL.md",
			func(t *testing.T, root string) {
				base(t, root)
				writeRel(t, root, "README.md", "# x\nSee [ANATOMY.md](ANATOMY.md) and [CONTRACT.md](CONTRACT.md).\n"+
					"There is no repository-local dev guide. Do not read dev-guide-skill.\n") // names it, denies it, NO link
			}},
		{"zero-child sentinel rejects a hypothetical child edge (canonical)", "first governed-child PR MUST add", func(t *testing.T, root string) {
			base(t, root)
			// A future edit adds a governed-child contract edge before the child gate exists.
			writeRel(t, root, "tui/fs/CONTRACT.md", childContract)
			writeRel(t, root, "CONTRACT.md", rootContract("1", withExtra("tui/fs/CONTRACT.md")))
		}},
		// --- B1: sentinel case/symlink/whitespace guards ---
		{"sentinel rejects a noncanonical-case child spelling", "noncanonical-case", func(t *testing.T, root string) {
			base(t, root)
			// On a case-insensitive filesystem tui/fs/contract.md resolves to the real
			// child CONTRACT.md; the sentinel must reject the noncanonical case spelling
			// lexically rather than trusting the host FS.
			writeRel(t, root, "tui/fs/CONTRACT.md", childContract)
			writeRel(t, root, "CONTRACT.md", rootContract("1", withExtra("tui/fs/contract.md")))
		}},
		{"sentinel rejects an inside-repo symlink alias to a child CONTRACT.md", "first governed-child PR MUST add",
			func(t *testing.T, root string) {
				base(t, root)
				writeRel(t, root, "tui/fs/CONTRACT.md", childContract)
				target := filepath.Join(root, "tui", "fs", "CONTRACT.md")
				if os.Symlink(target, filepath.Join(root, "tui", "fs", "child-contract.md")) != nil {
					t.Skip("symlink unsupported")
				}
				writeRel(t, root, "CONTRACT.md", rootContract("1", withExtra("tui/fs/child-contract.md")))
			}},
		{"raw trailing whitespace in a related_files item rejected", "whitespace/backslash", func(t *testing.T, root string) {
			base(t, root)
			write(t, filepath.Join(root, "real.go"), "package x\n")
			// A trailing space on the item must be rejected, not silently trimmed.
			rel := append(append([]string{"CONTRACT.md"}, rootRelated...), "real.go ")
			writeRel(t, root, "ANATOMY.md", "---\n"+frontmatterList(rel)+"maintenance: "+goodMaint+"\n---\n"+
				body("root", rootAnatomyHeadings))
		}},
		// --- B2: scalar-shape strictness ---
		{"root maintenance as quoted-empty rejected", "unsupported quoted scalar", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomyMaint(`""`))
		}},
		{"root maintenance as null rejected", "unsupported YAML null value", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomyMaint("null"))
		}},
		{"root maintenance as flow sequence rejected", "unsupported YAML flow collection", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomyMaint("[]"))
		}},
		{"root maintenance as flow map rejected", "unsupported YAML flow collection", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "ANATOMY.md", rootAnatomyMaint("{}"))
		}},
		{"root maintenance as plain word (not a block scalar) rejected", "must be a block scalar", func(t *testing.T, root string) {
			base(t, root)
			// A bare plain scalar parses but is not the required block-scalar form.
			writeRel(t, root, "ANATOMY.md", rootAnatomyMaint("plainword"))
		}},
		// --- B3: durable negative fixtures for keys / headings / regular-file / names ---
		{"wrong/extra root Anatomy keys rejected", "want related_files,maintenance", func(t *testing.T, root string) {
			base(t, root)
			// A valid-parsing ANATOMY with an extra top-level key: keys no longer exact.
			writeRel(t, root, "ANATOMY.md", "---\n"+frontmatterList(append([]string{"CONTRACT.md"}, rootRelated...))+
				"maintenance: "+goodMaint+"\nextra: x\n---\n"+body("root", rootAnatomyHeadings))
		}},
		{"reordered root Anatomy keys rejected", "want related_files,maintenance", func(t *testing.T, root string) {
			base(t, root)
			// maintenance before related_files: valid parse, wrong key order.
			writeRel(t, root, "ANATOMY.md", "---\nmaintenance: "+goodMaint+"\n"+
				frontmatterList(append([]string{"CONTRACT.md"}, rootRelated...))+"---\n"+body("root", rootAnatomyHeadings))
		}},
		{"root Anatomy heading order wrong (Components not second) rejected", "headings =", func(t *testing.T, root string) {
			base(t, root)
			// Swap Purpose/Components so Components is first, not second (map-first violated).
			swapped := slices.Clone(rootAnatomyHeadings)
			swapped[0], swapped[1] = swapped[1], swapped[0]
			writeRel(t, root, "ANATOMY.md", "---\n"+frontmatterList(append([]string{"CONTRACT.md"}, rootRelated...))+
				"maintenance: "+goodMaint+"\n---\n"+body("root", swapped))
		}},
		{"related_files entry that is a directory rejected", "is not a regular file", func(t *testing.T, root string) {
			base(t, root)
			// A directory exists and resolves inside the repo but is not a regular file.
			if err := os.MkdirAll(filepath.Join(root, "somedir"), 0o755); err != nil {
				t.Fatal(err)
			}
			writeRel(t, root, "ANATOMY.md", rootAnatomy(withExtra("somedir"), goodMaint))
		}},
		{"invalid root Contract kebab name rejected", "want kebab-case", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "CONTRACT.md", "---\nname: Not_Kebab\ncontract_version: 1\n"+
				frontmatterList(append([]string{"ANATOMY.md"}, rootRelated...))+
				"maintenance: |\n  root contract.\n---\n"+body("root", rootContractHeadings))
		}},
		{"wrong/extra dev-guide keys rejected", "want name,description", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "dev-guide-skill/SKILL.md", "---\nname: repo-dev\ndescription: |\n  guide\nextra: x\n---\n# Guide\n")
		}},
		{"invalid dev-guide name rejected", "dev-guide-skill/SKILL.md name", func(t *testing.T, root string) {
			base(t, root)
			writeRel(t, root, "dev-guide-skill/SKILL.md", "---\nname: Not_Kebab\ndescription: |\n  guide\n---\n# Guide\n")
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			tc.build(t, root)
			errs := validateRepository(root)
			if tc.wantErr == "" {
				if len(errs) > 0 {
					t.Fatalf("expected valid fixture, got:\n%s", strings.Join(errs, "\n"))
				}
				return
			}
			for _, e := range errs {
				if strings.Contains(e, tc.wantErr) {
					return
				}
			}
			t.Fatalf("expected a violation containing %q, got:\n%s", tc.wantErr, strings.Join(errs, "\n"))
		})
	}
}

// TestArchitectureFrontmatterParser exercises the strict-subset frontmatter parser
// directly: two positive shapes (literal and folded block scalars + a list) and every
// malformed shape the roots must never contain.
func TestArchitectureFrontmatterParser(t *testing.T) {
	// Positive: a literal block keeps newlines and the list parses; a folded block joins
	// with spaces. Both must be recorded as block scalars, and a plain `name` must NOT be.
	if d, e := parseFrontmatter("---\nname: x\nrelated_files:\n  - a.go\n  - b.go\nmaintenance: |\n  line one\n  line two\n---\nb\n"); e != "" ||
		d.scalars["maintenance"] != "line one\nline two" || !slices.Equal(d.related, []string{"a.go", "b.go"}) ||
		!d.blockScalar["maintenance"] || d.blockScalar["name"] {
		t.Fatalf("literal/list parse: err=%q maintenance=%q related=%v block=%v", e, d.scalars["maintenance"], d.related, d.blockScalar)
	}
	if d, e := parseFrontmatter("---\nnote: >\n  a\n  b\n---\n"); e != "" || d.scalars["note"] != "a b" || !d.blockScalar["note"] {
		t.Fatalf("folded parse: err=%q note=%q block=%v", e, d.scalars["note"], d.blockScalar)
	}
	// Negative: every malformed shape must be rejected — including YAML-shaped plain
	// values that a real YAML reader would resolve to a list/map/null/empty string.
	neg := []struct{ name, text string }{
		{"tab-indented block scalar", "---\nmaintenance: |\n\tinvalid-yaml-tab-indent\n---\n"},
		{"duplicate key", "---\nname: x\nname: y\n---\n"},
		{"non-empty related_files scalar", "---\nrelated_files: garbage\n  - a.go\n---\n"},
		{"related_files with no items", "---\nrelated_files:\nname: x\n---\n"},
		{"empty list item", "---\nrelated_files:\n  - \n---\n"},
		{"block-scalar header with junk", "---\nmaintenance: | junk\n  x\n---\n"},
		{"missing closing delimiter", "---\nname: x\n"},
		{"missing opening delimiter", "name: x\n---\n"},
		{"bare top-level list line", "---\n- a.go\n---\n"},
		{"flow-sequence scalar", "---\nmaintenance: []\n---\n"},
		{"flow-map scalar", "---\nmaintenance: {}\n---\n"},
		{"single-quoted-empty scalar", "---\nmaintenance: ''\n---\n"},
		{"double-quoted-empty scalar", "---\nmaintenance: \"\"\n---\n"},
		{"null scalar", "---\nmaintenance: null\n---\n"},
		{"tilde scalar", "---\nmaintenance: ~\n---\n"},
		{"alias scalar", "---\nmaintenance: *anchor\n---\n"},
		{"tag scalar", "---\nmaintenance: !!str x\n---\n"},
	}
	for _, tc := range neg {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := parseFrontmatter(tc.text); e == "" {
				t.Fatalf("expected rejection, got success")
			}
		})
	}
}

// --- fixture builders -------------------------------------------------------

// rootRelated is the minimal current-tree edge set both roots must carry (the pair
// link plus every required routing edge). The pair link differs per file, so each
// builder prepends its counterpart.
var rootRelated = requiredRootEdges

const goodMaint = "|\n  root doc."

func write(t *testing.T, abs, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func writeRel(t *testing.T, root, rel, content string) {
	write(t, filepath.Join(root, filepath.FromSlash(rel)), content)
}

func without(list []string, drop string) []string {
	out := make([]string, 0, len(list))
	for _, v := range list {
		if v != drop {
			out = append(out, v)
		}
	}
	return out
}

// withExtra returns rootRelated with extra entries appended (a fresh slice, so the
// shared rootRelated is never mutated).
func withExtra(extra ...string) []string {
	return append(append([]string{}, rootRelated...), extra...)
}

// frontmatterList renders `related_files:` with the given repo-relative entries.
func frontmatterList(related []string) string {
	b := strings.Builder{}
	b.WriteString("related_files:\n")
	for _, r := range related {
		b.WriteString("  - " + r + "\n")
	}
	return b.String()
}

// body returns a minimal document body: a one-line intro then the given `##`
// headings in order.
func body(title string, hs []string) string {
	b := strings.Builder{}
	b.WriteString("# " + title + "\n\nintro\n\n")
	for _, h := range hs {
		b.WriteString(h + "\n\ntext\n\n")
	}
	return b.String()
}

// rootAnatomy renders a root ANATOMY.md with `CONTRACT.md` prepended to related and
// the given maintenance scalar body (`|\n  text`).
func rootAnatomy(related []string, maint string) string {
	return "---\n" + frontmatterList(append([]string{"CONTRACT.md"}, related...)) +
		"maintenance: " + maint + "\n---\n" + body("root", rootAnatomyHeadings)
}

// rootContract renders a root CONTRACT.md with `ANATOMY.md` prepended to related.
func rootContract(version string, related []string) string {
	return "---\nname: component-contract-convention\ncontract_version: " + version + "\n" +
		frontmatterList(append([]string{"ANATOMY.md"}, related...)) +
		"maintenance: |\n  root contract.\n---\n" + body("root", rootContractHeadings)
}

// rootAnatomyMaint renders a valid root ANATOMY.md but with an arbitrary RAW maintenance
// value (e.g. `[]`, `null`, `""`, or a bare plain word) so scalar-shape rejection can be
// exercised. maint is the literal text after `maintenance: `.
func rootAnatomyMaint(maint string) string {
	return rootAnatomy(rootRelated, maint)
}

// childContract is a well-formed governed-child CONTRACT.md the zero-child sentinel must
// reject regardless of how the root edge spells or aliases it.
const childContract = "---\nname: fs\ncontract_version: 1\nroot_contract: CONTRACT.md\n" +
	"related_files:\n  - ANATOMY.md\nmaintenance: |\n  child.\n---\n# fs\n"

// base lays down a minimal valid zero-child root: the two roots, dev guide, all four
// entry docs (READMEs + CLAUDE) with real routing links, and both validator files.
func base(t *testing.T, root string) {
	t.Helper()
	writeRel(t, root, "tui/architecture_documents_test.go", "package main\n")
	writeRel(t, root, "tui/architecture_documents_fixtures_test.go", "package main\n")
	writeRel(t, root, "dev-guide-skill/SKILL.md", "---\nname: repo-dev\ndescription: |\n  guide\n---\n# Guide\n")
	rm := "# x\nSee [ANATOMY.md](ANATOMY.md), [CONTRACT.md](CONTRACT.md), and [dev-guide-skill/SKILL.md](dev-guide-skill/SKILL.md).\n"
	for _, r := range []string{"README.md", "README.zh.md", "README.wen.md", "CLAUDE.md"} {
		writeRel(t, root, r, rm)
	}
	writeRel(t, root, "ANATOMY.md", rootAnatomy(rootRelated, goodMaint))
	writeRel(t, root, "CONTRACT.md", rootContract("1", rootRelated))
}
