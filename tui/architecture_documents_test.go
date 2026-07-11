// Structural validation for the repo's two distributed architecture-document
// systems: the code-navigation graph rooted at ANATOMY.md and the
// interface/expected-behavior graph rooted at CONTRACT.md. It lives in the existing
// TUI module (runs under `cd tui && go test ./...`) and reads the root `../*.md`.
//
// The validator is one function, validateRepository(root), returning violation
// strings. It enforces today's invariants: the two roots' strict frontmatter,
// repo-contained duplicate-free related_files, the reciprocal root Anatomy/Contract
// pair and required routing edges, and the root heading skeletons. The repository
// intentionally has ZERO governed children right now, so instead of a speculative
// child gate the validator fails closed: root CONTRACT.md must list no non-root
// `/CONTRACT.md`, and the first governed-child PR must add real child validation
// before adding that root edge (see the zero-child sentinel). It checks machine
// invariants only — not prose, and not the citation drift scan (owned by the
// lingtai-tui-anatomy skill).
//
// TestArchitectureRealRepository runs the validator on the real repo; the
// data-driven parser/root/path/routing/sentinel fixtures live in
// architecture_documents_fixtures_test.go and run the SAME validateRepository over
// compact t.TempDir mini-repos.
package main

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

var kebabCase = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)*$`)

// Required root heading skeletons; the root ANATOMY is map-first (Components second).
var (
	rootAnatomyHeadings = []string{"## Purpose", "## Components", "## Connections",
		"## Composition", "## State", "## Notes", "## Anatomy convention", "## Maintenance", "## Template"}
	rootContractHeadings = []string{"## Purpose", "## Architecture foundation", "## Behavior",
		"## Frontmatter contract", "## Body contract", "## Link semantics",
		"## Maintenance contract", "## Validation", "## Template"}
)

// requiredRootEdges are the non-pair related_files every root must list: the dev
// guide, the public README, and both halves of this validator. Splitting the
// validator into two files means both are required graph edges.
var requiredRootEdges = []string{
	"dev-guide-skill/SKILL.md",
	"README.md",
	"tui/architecture_documents_test.go",
	"tui/architecture_documents_fixtures_test.go",
}

type archDoc struct {
	keys    []string
	scalars map[string]string
	// blockScalar records which keys were expressed as a `|`/`>` block scalar (the
	// only multi-line scalar form these documents use). Keys not present here are
	// plain scalars. Callers that require a block scalar (root maintenance, dev-guide
	// description) check this so a plain YAML-shaped value cannot masquerade as text.
	blockScalar map[string]bool
	related     []string
	body        string
}

// parseFrontmatter parses the strict (NOT general) YAML subset these documents use,
// returning a non-empty error string on any violation so it is directly unit-testable:
// unique top-level `key: value`; `related_files:` = empty scalar + >=1 `  - item`; a
// `key: |`/`>` block scalar with SPACE-indented continuations (tabs rejected, as YAML
// forbids them); a header with trailing junk (`| junk`) rejected.
func parseFrontmatter(text string) (archDoc, string) {
	doc := archDoc{scalars: map[string]string{}, blockScalar: map[string]bool{}}
	if !strings.HasPrefix(text, "---\n") {
		return doc, "missing leading frontmatter delimiter"
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	if end < 0 {
		return doc, "missing closing frontmatter delimiter"
	}
	doc.body = rest[end+len("\n---\n"):]
	lines := strings.Split(rest[:end], "\n")
	seen := map[string]bool{}
	for i := 0; i < len(lines); i++ {
		line := lines[i]
		if strings.TrimSpace(line) == "" {
			continue
		}
		if line[0] == ' ' || line[0] == '\t' || line[0] == '-' {
			return doc, "unexpected non-top-level frontmatter line: " + line
		}
		colon := strings.Index(line, ":")
		if colon < 0 {
			return doc, "frontmatter line without key: " + line
		}
		key, val := line[:colon], strings.TrimSpace(line[colon+1:])
		if seen[key] {
			return doc, "duplicate frontmatter key: " + key
		}
		seen[key] = true
		doc.keys = append(doc.keys, key)
		switch {
		case key == "related_files":
			if val != "" {
				return doc, "related_files must be a block list with an empty scalar, got: " + val
			}
			for i+1 < len(lines) {
				next := lines[i+1]
				if strings.HasPrefix(next, "  - ") {
					// Preserve the RAW item text (no TrimSpace) so downstream
					// related_files validation can reject leading/trailing whitespace;
					// emptiness is tested against a trimmed copy only.
					item := next[len("  - "):]
					if strings.TrimSpace(item) == "" {
						return doc, "empty related_files list item"
					}
					doc.related = append(doc.related, item)
					i++
					continue
				}
				if t := strings.TrimSpace(next); t != "" && (next[0] == ' ' || next[0] == '\t' || next[0] == '-') {
					return doc, "malformed related_files item: " + next
				}
				break
			}
			if len(doc.related) == 0 {
				return doc, "related_files must list at least one item"
			}
		case val == "|" || val == ">":
			var buf []string
			for i+1 < len(lines) {
				next := lines[i+1]
				switch {
				case next == "":
					buf = append(buf, "")
				case next[0] == '\t':
					return doc, "tab-indented block scalar continuation (YAML forbids tabs): " + key
				case next[0] == ' ':
					buf = append(buf, strings.TrimSpace(next))
				default:
					goto doneScalar
				}
				i++
			}
		doneScalar:
			sep := "\n"
			if val == ">" {
				sep = " "
			}
			doc.scalars[key] = strings.TrimSpace(strings.Join(buf, sep))
			doc.blockScalar[key] = true
		case strings.HasPrefix(val, "|") || strings.HasPrefix(val, ">"):
			return doc, "unsupported block-scalar header: " + val
		default:
			// Plain scalar. This strict subset supports ONLY plain string scalars and
			// the block scalars above. Reject YAML-shaped values (flow collections,
			// null/tilde, quoted-empty, aliases/anchors/tags, other quoted forms) so
			// they cannot be silently treated as non-empty text — a real YAML reader
			// would resolve them to a list/map/null/empty string, defeating the
			// non-empty and scalar-shape invariants.
			if e := rejectNonPlainScalar(key, val); e != "" {
				return doc, e
			}
			doc.scalars[key] = val
		}
	}
	return doc, ""
}

// rejectNonPlainScalar rejects a plain-scalar value whose first character marks it as
// a YAML shape this strict subset does not support: flow collections (`[`/`{`), quoted
// strings including quoted-empty (`'`/`"`), and aliases/anchors/tags (`*`/`&`/`!`). It
// also rejects the bare YAML null tokens (`null`/`Null`/`NULL`/`~`). The accepted plain
// forms — the root kebab `name` and positive-decimal `contract_version`, the dev-guide
// kebab `name` — begin with none of these and are never a null token, so they pass.
// Multi-line/quoted values must instead use the supported `|`/`>` block-scalar form.
func rejectNonPlainScalar(key, val string) string {
	switch val {
	case "null", "Null", "NULL", "~":
		return "unsupported YAML null value for " + key + ": use a `|` block scalar for text"
	}
	if val == "" {
		return "" // an empty plain scalar is handled by the non-empty checks, not here
	}
	switch val[0] {
	case '[', '{':
		return "unsupported YAML flow collection for " + key + ": use a `|` block scalar for text"
	case '\'', '"':
		return "unsupported quoted scalar for " + key + ": use a `|` block scalar for text"
	case '*', '&', '!':
		return "unsupported YAML alias/anchor/tag for " + key + ": use a `|` block scalar for text"
	}
	return ""
}

// headings returns the ordered `## ` headings, skipping fenced blocks (so an
// embedded child template does not pollute a root's heading order).
func headings(body string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "```") {
			inFence = !inFence
		} else if !inFence && strings.HasPrefix(line, "## ") {
			out = append(out, strings.TrimRight(line, " \t"))
		}
	}
	return out
}

// checker validates one repository rooted at root, accumulating violations.
type checker struct {
	root string
	docs map[string]*archDoc // cached parse; nil value = read/parse failed
	errs []string
}

func (c *checker) errf(format string, a ...any) { c.errs = append(c.errs, fmt.Sprintf(format, a...)) }

// doc reads+parses a repo-relative document once, recording a violation (ok=false)
// if it is missing or fails the strict-subset parser.
func (c *checker) doc(rel string) (archDoc, bool) {
	if d, seen := c.docs[rel]; seen {
		if d == nil {
			return archDoc{}, false
		}
		return *d, true
	}
	c.docs[rel] = nil
	raw, err := os.ReadFile(filepath.Join(c.root, filepath.FromSlash(rel)))
	if err != nil {
		c.errf("%s: cannot read: %v", rel, err)
		return archDoc{}, false
	}
	if d, e := parseFrontmatter(string(raw)); e != "" {
		c.errf("%s: %s", rel, e)
	} else {
		c.docs[rel] = &d
		return d, true
	}
	return archDoc{}, false
}

// resolveRelatedEntry runs the shared lexical + symlink-resolution + containment logic
// for one related_files entry p and, when it resolves inside the repo, returns its
// repo-relative resolved identity (slash form). It emits the whitespace/backslash,
// illegal-segment, missing, unresolvable, and outside-repo violations itself, so both
// checkRelated and the zero-child sentinel share ONE path algorithm (no second copy).
// It deliberately does NOT emit the regular-file violation — that is checkRelated's
// concern; the sentinel needs the resolved identity of any inside-repo target,
// including a regular child CONTRACT.md reached via a symlink alias. ok=false means the
// entry failed a lexical/existence/containment check and no resolved identity is known.
func (c *checker) resolveRelatedEntry(container, p string) (repoRel string, ok bool) {
	if p != strings.TrimSpace(p) || strings.Contains(p, "\\") {
		c.errf("%s: bad related_files entry (whitespace/backslash): %q", container, p)
		return "", false
	}
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			c.errf("%s: illegal path segment in related_files entry: %q", container, p)
			return "", false
		}
	}
	rootResolved, err := filepath.EvalSymlinks(c.root)
	if err != nil {
		c.errf("%s: cannot resolve repo root: %v", container, err)
		return "", false
	}
	abs := filepath.Join(c.root, filepath.FromSlash(p))
	if _, err := os.Lstat(abs); err != nil {
		c.errf("%s: related_files entry does not exist: %q", container, p)
		return "", false
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		c.errf("%s: related_files entry cannot be resolved: %q (%v)", container, p, err)
		return "", false
	}
	rel, err := filepath.Rel(rootResolved, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		c.errf("%s: related_files entry resolves outside the repository: %q -> %q", container, p, resolved)
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// checkRelated validates the shared related_files rules: non-empty, duplicate-free,
// each entry repo-relative with no `.`/`..` segments and — resolving symlinks (via the
// shared resolveRelatedEntry) — a regular file whose target stays inside the repo.
func (c *checker) checkRelated(container string, d archDoc) {
	if len(d.related) == 0 {
		c.errf("%s: related_files must be a non-empty list", container)
		return
	}
	seen := map[string]bool{}
	for _, p := range d.related {
		if seen[p] {
			c.errf("%s: duplicate related_files entry: %q", container, p)
		}
		seen[p] = true
		if _, ok := c.resolveRelatedEntry(container, p); !ok {
			continue
		}
		abs := filepath.Join(c.root, filepath.FromSlash(p))
		if info, err := os.Stat(abs); err != nil || !info.Mode().IsRegular() {
			c.errf("%s: related_files entry is not a regular file: %q", container, p)
		}
	}
}

func (c *checker) checkKeys(container, want string, d archDoc) {
	if got := strings.Join(d.keys, ","); got != want {
		c.errf("%s: keys = %q, want %s", container, got, want)
	}
}

func (c *checker) checkVersion(container, val string) {
	if n, err := strconv.Atoi(val); err != nil || n < 1 {
		c.errf("%s: contract_version = %q, want positive integer", container, val)
	}
}

// checkDoc runs the frontmatter+heading checks shared by both roots: exact keys,
// non-empty block-scalar maintenance, repo-contained related_files, heading order.
func (c *checker) checkDoc(rel, keys string, d archDoc, hs []string) {
	c.checkKeys(rel, keys, d)
	// Root maintenance must be expressed as a `|`/`>` block scalar (the supported
	// multi-line-text form) and be non-empty. Requiring the block form means a plain
	// YAML-shaped value can never satisfy it — the plain-scalar rejection in the parser
	// already blocks flow collections/null/quoted-empty, and this blocks a bare plain
	// word too.
	if !d.blockScalar["maintenance"] {
		c.errf("%s: maintenance must be a block scalar (|/>)", rel)
	} else if strings.TrimSpace(d.scalars["maintenance"]) == "" {
		c.errf("%s: maintenance must be non-empty", rel)
	}
	c.checkRelated(rel, d)
	if got := headings(d.body); !slices.Equal(got, hs) {
		c.errf("%s: headings = %v, want %v", rel, got, hs)
	}
}

// validateRoots checks the two roots, their reciprocal pair, and the required
// routing edges — the full current-tree schema/graph gate.
func (c *checker) validateRoots() {
	anatomy, aok := c.doc("ANATOMY.md")
	contract, cok := c.doc("CONTRACT.md")
	if !aok || !cok {
		return
	}
	c.checkDoc("ANATOMY.md", "related_files,maintenance", anatomy, rootAnatomyHeadings)
	c.checkDoc("CONTRACT.md", "name,contract_version,related_files,maintenance", contract, rootContractHeadings)
	if !kebabCase.MatchString(contract.scalars["name"]) {
		c.errf("CONTRACT.md name = %q, want kebab-case", contract.scalars["name"])
	}
	c.checkVersion("CONTRACT.md", contract.scalars["contract_version"])
	if !slices.Contains(anatomy.related, "CONTRACT.md") {
		c.errf("root ANATOMY.md must list CONTRACT.md")
	}
	if !slices.Contains(contract.related, "ANATOMY.md") {
		c.errf("root CONTRACT.md must list ANATOMY.md")
	}
	for _, edge := range requiredRootEdges {
		if !slices.Contains(anatomy.related, edge) {
			c.errf("root ANATOMY.md related_files must list %q", edge)
		}
		if !slices.Contains(contract.related, edge) {
			c.errf("root CONTRACT.md related_files must list %q", edge)
		}
	}
}

// isContractBase reports whether a path's final component is the child-contract file
// name, comparing case-INSENSITIVELY so a noncanonical spelling (contract.md) is caught
// rather than trusting the host filesystem's case behavior. canonical reports whether
// the spelling is exactly CONTRACT.md.
func isContractBase(repoRelOrLexical string) (child, canonical bool) {
	base := path.Base(repoRelOrLexical)
	return strings.EqualFold(base, "CONTRACT.md"), base == "CONTRACT.md"
}

// validateNoGovernedChildren is the zero-child fail-closed sentinel. The repository has
// no governed children today and the validator deliberately ships no child gate. It
// fires when a root-contract related_files edge is child-shaped by EITHER its validated
// lexical path OR its resolved repo-relative target — the same symlink-aware resolution
// the root graph uses — so neither a noncanonical-case spelling (tui/fs/contract.md) nor
// an inside-repo symlink alias to a real child CONTRACT.md can slip a governed child in
// silently. It also rejects the noncanonical case spelling explicitly. The obligation
// text names the paired-child validation the first governed-child PR must add first.
func (c *checker) validateNoGovernedChildren() {
	contract, ok := c.doc("CONTRACT.md")
	if !ok {
		return
	}
	for _, p := range contract.related {
		if p == "CONTRACT.md" { // the root itself is not a governed child
			continue
		}
		lexChild, lexCanonical := isContractBase(p)
		// Resolve the edge through symlinks to catch an alias whose real target is a
		// child CONTRACT.md even when the lexical name is something else. Ignore ok:
		// an unresolved/outside edge still gets its lexical classification below and
		// the resolution violation is reported by the shared graph check.
		resolved, _ := c.resolveRelatedEntry("CONTRACT.md", p)
		resChild := false
		if resolved != "" && resolved != "CONTRACT.md" {
			resChild, _ = isContractBase(resolved)
		}
		if !lexChild && !resChild {
			continue
		}
		if lexChild && !lexCanonical {
			c.errf("root CONTRACT.md lists related_files entry %q whose final component is a "+
				"noncanonical-case child contract spelling; the canonical child-contract file name is "+
				"CONTRACT.md", p)
		}
		c.errf("root CONTRACT.md lists governed-child contract %q, but this repository has zero "+
			"governed children and ships no child validator: the first governed-child PR MUST add "+
			"paired-child validation (child schema, headings, parent graph, reciprocity) before adding "+
			"this root edge", p)
	}
}

// linksTo reports whether text has an actual Markdown link `](target)` (or `](./…)`)
// to the repo-relative target — not a mere substring mention.
func linksTo(text, target string) bool {
	return strings.Contains(text, "]("+target+")") || strings.Contains(text, "](./"+target+")")
}

// validateEntryRouting requires the public/agent entry docs to carry actual Markdown
// links (not phrase mentions) to both roots and the repository-local dev guide, and
// the dev guide itself to be well-formed. No client-specific discovery path.
func (c *checker) validateEntryRouting() {
	if dev, ok := c.doc("dev-guide-skill/SKILL.md"); ok {
		c.checkKeys("dev-guide-skill/SKILL.md", "name,description", dev)
		if !kebabCase.MatchString(dev.scalars["name"]) {
			c.errf("dev-guide-skill/SKILL.md name = %q, want kebab-case", dev.scalars["name"])
		}
		// description must be a block scalar (|/>), like the real dev guide, so a plain
		// YAML-shaped value cannot masquerade as the description text.
		if !dev.blockScalar["description"] {
			c.errf("dev-guide-skill/SKILL.md description must be a block scalar (|/>)")
		} else if strings.TrimSpace(dev.scalars["description"]) == "" {
			c.errf("dev-guide-skill/SKILL.md description must be non-empty")
		}
	}
	for _, rel := range []string{"README.md", "README.zh.md", "README.wen.md", "CLAUDE.md"} {
		raw, err := os.ReadFile(filepath.Join(c.root, rel))
		if err != nil {
			c.errf("%s: cannot read: %v", rel, err)
			continue
		}
		for _, target := range []string{"ANATOMY.md", "CONTRACT.md", "dev-guide-skill/SKILL.md"} {
			if !linksTo(string(raw), target) {
				c.errf("%s must contain a Markdown link to %s", rel, target)
			}
		}
	}
}

// validateRepository runs every current-tree check against one repository root and
// returns all violations (empty == valid). Single entry point shared by the real
// repo and every t.TempDir fixture.
func validateRepository(root string) []string {
	c := &checker{root: root, docs: map[string]*archDoc{}}
	c.validateRoots()
	c.validateNoGovernedChildren()
	c.validateEntryRouting()
	return c.errs
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		_, a := os.Stat(filepath.Join(dir, "ANATOMY.md"))
		_, c := os.Stat(filepath.Join(dir, "CONTRACT.md"))
		if a == nil && c == nil {
			return dir
		}
		if p := filepath.Dir(dir); p != dir {
			dir = p
		} else {
			t.Fatalf("no repository root (dir with ANATOMY.md and CONTRACT.md) above %q", dir)
		}
	}
}

func TestArchitectureRealRepository(t *testing.T) {
	if errs := validateRepository(findRepoRoot(t)); len(errs) > 0 {
		t.Fatalf("real repository has %d architecture-document violations:\n%s", len(errs), strings.Join(errs, "\n"))
	}
}
