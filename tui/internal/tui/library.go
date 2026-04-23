package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// skillEntry holds parsed metadata for one skill.
type skillEntry struct {
	Name        string
	Description string
	Version     string
	Path        string // absolute path to SKILL.md
	Body        string // raw content of SKILL.md (loaded on select)
	Group       string // group folder name (e.g., "intrinsic", "custom", recipe name)
}

// skillProblem describes a broken skill folder.
type skillProblem struct {
	Folder string
	Reason string
}

// ── Frontmatter parser ──────────────────────────────────────────────

var (
	fmRe = regexp.MustCompile(`(?s)\A---\s*\n(.*?\n)---\s*\n`)
	kvRe = regexp.MustCompile(`(?m)^(\w[\w-]*)\s*:\s*(.+)$`)
)

func parseFrontmatter(text string) map[string]string {
	m := fmRe.FindStringSubmatch(text)
	if m == nil {
		return nil
	}
	result := make(map[string]string)
	for _, kv := range kvRe.FindAllStringSubmatch(m[1], -1) {
		result[kv[1]] = strings.TrimSpace(kv[2])
	}
	return result
}

// ── Scan ────────────────────────────────────────────────────────────

// scanLibrary scans the library directory recursively for skill folders.
//
// A directory with SKILL.md is a skill folder (leaf).
// A directory containing only subdirectories (no loose files) is a group folder.
// A directory with loose files but no SKILL.md is corrupted.
func scanLibrary(libraryDir string) ([]skillEntry, []skillProblem) {
	entries, err := os.ReadDir(libraryDir)
	if err != nil {
		return nil, nil
	}

	var skills []skillEntry
	var problems []skillProblem

	for _, e := range entries {
		if isHiddenEntry(e.Name()) {
			continue
		}
		// Follow symlinks for stat
		childPath := filepath.Join(libraryDir, e.Name())
		info, err := os.Stat(childPath)
		if err != nil || !info.IsDir() {
			continue
		}

		skillFile := filepath.Join(childPath, "SKILL.md")
		if fileExists(skillFile) {
			// Flat skill (legacy or ungrouped)
			sk, prob := parseSkillFile(skillFile, e.Name(), "")
			if sk != nil {
				skills = append(skills, *sk)
			}
			if prob != nil {
				problems = append(problems, *prob)
			}
			continue
		}

		// No SKILL.md — check if it's a valid group folder
		scanGroup(childPath, e.Name(), &skills, &problems)
	}

	sort.Slice(skills, func(i, j int) bool { return skills[i].Name < skills[j].Name })
	return skills, problems
}

// scanGroup scans a group folder recursively.
func scanGroup(dir, group string, skills *[]skillEntry, problems *[]skillProblem) {
	children, err := os.ReadDir(dir)
	if err != nil {
		return
	}

	// Check for loose files (corruption check)
	for _, c := range children {
		if isHiddenEntry(c.Name()) {
			continue
		}
		childPath := filepath.Join(dir, c.Name())
		info, err := os.Stat(childPath)
		if err != nil {
			continue
		}
		if !info.IsDir() {
			*problems = append(*problems, skillProblem{
				Folder: group,
				Reason: "not a skill (no SKILL.md) and has loose files",
			})
			return
		}
	}

	// All children are directories — recurse
	for _, c := range children {
		if isHiddenEntry(c.Name()) {
			continue
		}
		childPath := filepath.Join(dir, c.Name())
		info, err := os.Stat(childPath)
		if err != nil || !info.IsDir() {
			continue
		}

		skillFile := filepath.Join(childPath, "SKILL.md")
		if fileExists(skillFile) {
			sk, prob := parseSkillFile(skillFile, c.Name(), group)
			if sk != nil {
				*skills = append(*skills, *sk)
			}
			if prob != nil {
				*problems = append(*problems, *prob)
			}
			continue
		}

		// Nested group — recurse deeper
		nestedGroup := group + "/" + c.Name()
		scanGroup(childPath, nestedGroup, skills, problems)
	}
}

func parseSkillFile(skillFile, folderName, group string) (*skillEntry, *skillProblem) {
	data, err := os.ReadFile(skillFile)
	if err != nil {
		return nil, &skillProblem{Folder: folderName, Reason: "cannot read SKILL.md"}
	}
	text := string(data)
	fm := parseFrontmatter(text)
	if fm == nil {
		return nil, &skillProblem{Folder: folderName, Reason: "invalid frontmatter"}
	}
	name := fm["name"]
	desc := fm["description"]
	if name == "" {
		return nil, &skillProblem{Folder: folderName, Reason: "missing name"}
	}
	if desc == "" {
		return nil, &skillProblem{Folder: folderName, Reason: "missing description"}
	}
	return &skillEntry{
		Name:        name,
		Description: desc,
		Version:     fm["version"],
		Path:        skillFile,
		Body:        text,
		Group:       group,
	}, nil
}

// scanGroupFlat scans a directory recursively and collects every skill folder
// (any directory with a SKILL.md + valid frontmatter) under a SINGLE group
// label. Unlike scanGroup, it does not split into nested groups — every skill
// regardless of depth gets the same `group` tag. Used by the agent-scoped
// catalog builder so each Tier-1 path or intrinsic subtree maps to one group.
func scanGroupFlat(dir, group string, skills *[]skillEntry, problems *[]skillProblem) {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return
	}
	children, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, c := range children {
		if isHiddenEntry(c.Name()) {
			continue
		}
		childPath := filepath.Join(dir, c.Name())
		ci, err := os.Stat(childPath)
		if err != nil || !ci.IsDir() {
			continue
		}
		skillFile := filepath.Join(childPath, "SKILL.md")
		if fileExists(skillFile) {
			sk, prob := parseSkillFile(skillFile, c.Name(), group)
			if sk != nil {
				*skills = append(*skills, *sk)
			}
			if prob != nil {
				*problems = append(*problems, *prob)
			}
			continue
		}
		// No SKILL.md — recurse deeper keeping the same group label.
		scanGroupFlat(childPath, group, skills, problems)
	}
}

// ── init.json reader + path resolution (mirrors kernel library cap) ─────────

// readLibraryPaths returns the Tier 1 library paths from an agent's init.json.
// Looks at manifest.capabilities.library.paths. Returns an empty slice if:
//   - init.json is missing or unreadable
//   - library capability is not declared
//   - paths field is absent or not a list of strings
func readLibraryPaths(agentDir string) []string {
	initPath := filepath.Join(agentDir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return nil
	}
	var initFile struct {
		Manifest struct {
			Capabilities map[string]json.RawMessage `json:"capabilities"`
		} `json:"manifest"`
	}
	if err := json.Unmarshal(data, &initFile); err != nil {
		return nil
	}
	libRaw, ok := initFile.Manifest.Capabilities["library"]
	if !ok {
		return nil
	}
	var lib struct {
		Paths []string `json:"paths"`
	}
	if err := json.Unmarshal(libRaw, &lib); err != nil {
		return nil
	}
	return lib.Paths
}

// resolveLibraryPath mirrors the kernel's _resolve_path: tilde expansion,
// absolute paths as-is, relative paths resolved against agentDir.
func resolveLibraryPath(raw string, agentDir string) string {
	s := raw
	if strings.HasPrefix(s, "~/") || s == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			if s == "~" {
				s = home
			} else {
				s = filepath.Join(home, s[2:])
			}
		}
	}
	if filepath.IsAbs(s) {
		return filepath.Clean(s)
	}
	return filepath.Clean(filepath.Join(agentDir, s))
}

// libraryPathGroup classifies a raw library path into its display group name.
// Matching is done against the raw (unresolved) string so the user's declared
// intent — ".library_shared", "~/.lingtai-tui/utilities", etc. — maps cleanly
// regardless of how it resolves on disk.
func libraryPathGroup(raw string) string {
	// shared network library (canonical name)
	if strings.Contains(raw, ".library_shared") {
		return "shared"
	}
	// user-level utilities shared across all TUI-managed agents
	if strings.Contains(raw, ".lingtai-tui/utilities") ||
		strings.Contains(raw, ".lingtai-tui"+string(filepath.Separator)+"utilities") {
		return "utilities"
	}
	// anything else: use the basename of the raw path as the group.
	// Strip a trailing slash to be safe.
	trimmed := strings.TrimRight(raw, "/"+string(filepath.Separator))
	base := filepath.Base(trimmed)
	if base == "" || base == "." || base == string(filepath.Separator) {
		return raw
	}
	return base
}

// buildAgentLibraryCatalog mirrors what the kernel library capability would
// inject for this agent: scans <agent>/.library/intrinsic/{capabilities,addons}
// and <agent>/.library/custom, plus every Tier-1 path declared in init.json
// (manifest.capabilities.library.paths). Each skill is labelled with its source
// group so the viewer can visually distinguish capabilities vs. addons vs.
// custom vs. shared vs. user-added corpora.
//
// Returns entries ready for MarkdownViewerModel. Problems (broken skills) are
// appended as a final group using the "library.problems" i18n label.
func buildAgentLibraryCatalog(agentDir string, lang string) []MarkdownEntry {
	if agentDir == "" {
		return nil
	}

	var allProblems []skillProblem

	// Track group order so the sidebar renders a stable, meaningful layout:
	// intrinsic capabilities first, then addons, then custom, then shared,
	// utilities, and finally any other user-added Tier-1 paths in declaration
	// order.
	type bucket struct {
		name   string
		skills []skillEntry
	}
	groupMap := make(map[string]*bucket)
	var groupOrder []string
	addToGroup := func(group string, sk skillEntry) {
		b, exists := groupMap[group]
		if !exists {
			b = &bucket{name: group}
			groupMap[group] = b
			groupOrder = append(groupOrder, group)
		}
		sk.Group = group
		b.skills = append(b.skills, sk)
	}

	scan := func(dir, group string) {
		var skills []skillEntry
		var problems []skillProblem
		scanGroupFlat(dir, group, &skills, &problems)
		for _, sk := range skills {
			addToGroup(group, sk)
		}
		allProblems = append(allProblems, problems...)
	}

	// 1. Intrinsic capabilities
	scan(filepath.Join(agentDir, ".library", "intrinsic", "capabilities"), "capabilities")
	// 2. Intrinsic addons
	scan(filepath.Join(agentDir, ".library", "intrinsic", "addons"), "addons")
	// 3. Agent-authored custom skills
	scan(filepath.Join(agentDir, ".library", "custom"), "custom")

	// 4. Tier-1 paths from init.json (in declared order).
	for _, raw := range readLibraryPaths(agentDir) {
		resolved := resolveLibraryPath(raw, agentDir)
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() {
			// Silently skip missing / non-directory paths — matches the
			// kernel which only logs a warning and continues.
			continue
		}
		group := libraryPathGroup(raw)
		scan(resolved, group)
	}

	// Sort each group's skills alphabetically by display name for readability.
	for _, b := range groupMap {
		sort.Slice(b.skills, func(i, j int) bool { return b.skills[i].Name < b.skills[j].Name })
	}

	var entries []MarkdownEntry
	for _, g := range groupOrder {
		b := groupMap[g]
		for _, sk := range b.skills {
			label := sk.Name
			if sk.Version != "" {
				label += " " + sk.Version
			}
			entry := MarkdownEntry{
				Label: label,
				Group: b.name,
				Path:  sk.Path,
			}
			// For intrinsic capability/addon skills, concat SKILL.md +
			// SKILL-{lang}.md for display (matches pre-rewrite behavior).
			if b.name == "capabilities" || b.name == "addons" {
				entry.Content = concatSkillI18n(sk.Path, lang)
			}
			entries = append(entries, entry)
		}
	}

	if len(allProblems) > 0 {
		problemsGroup := i18n.T("library.problems")
		for _, p := range allProblems {
			entries = append(entries, MarkdownEntry{
				Label:   p.Folder,
				Group:   problemsGroup,
				Content: p.Reason,
			})
		}
	}

	return entries
}

// buildLibraryEntries converts scan results into MarkdownEntry items for the
// markdown viewer. Skills are grouped by their Group field (folder name).
// "intrinsic" is always shown last. Empty group means ungrouped (legacy).
//
// For intrinsic skills with i18n variants (SKILL-{lang}.md), the viewer
// concatenates SKILL.md + SKILL-{lang}.md (falling back to SKILL-en.md).
//
// NOTE: this is the legacy, single-root builder. The agent-scoped view uses
// buildAgentLibraryCatalog instead. Kept in place so any caller that still
// needs per-folder grouping (e.g., future agora/recipe browsers) can reuse it.
func buildLibraryEntries(libraryDir, lang string, skills []skillEntry, problems []skillProblem) []MarkdownEntry {
	// Collect groups in order: custom first, then recipe groups, intrinsic last.
	type groupBucket struct {
		name   string
		skills []skillEntry
	}

	groupMap := make(map[string]*groupBucket)
	var groupOrder []string

	for _, sk := range skills {
		g := sk.Group
		if g == "" {
			g = "custom" // legacy ungrouped skills → custom
		}
		if _, exists := groupMap[g]; !exists {
			groupMap[g] = &groupBucket{name: g}
			groupOrder = append(groupOrder, g)
		}
		groupMap[g].skills = append(groupMap[g].skills, sk)
	}

	// Sort groups: custom first, intrinsic last, everything else alphabetical in between
	sort.SliceStable(groupOrder, func(i, j int) bool {
		a, b := groupOrder[i], groupOrder[j]
		if a == "custom" {
			return true
		}
		if b == "custom" {
			return false
		}
		if a == "intrinsic" {
			return false
		}
		if b == "intrinsic" {
			return true
		}
		return a < b
	})

	var entries []MarkdownEntry
	for _, g := range groupOrder {
		bucket := groupMap[g]
		for _, sk := range bucket.skills {
			label := sk.Name
			if sk.Version != "" {
				label += " " + sk.Version
			}
			entry := MarkdownEntry{
				Label: label,
				Group: bucket.name,
				Path:  sk.Path,
			}
			// For intrinsic skills, concat SKILL.md + SKILL-{lang}.md for display.
			if bucket.name == "intrinsic" {
				entry.Content = concatSkillI18n(sk.Path, lang)
			}
			entries = append(entries, entry)
		}
	}

	if len(problems) > 0 {
		for _, p := range problems {
			entries = append(entries, MarkdownEntry{
				Label:   p.Folder,
				Group:   i18n.T("library.problems"),
				Content: p.Reason,
			})
		}
	}

	return entries
}

// concatSkillI18n reads SKILL.md and appends the best SKILL-{lang}.md
// variant for display. Falls back to SKILL-en.md if the requested lang
// variant does not exist. If no lang variant exists at all, returns just
// the SKILL.md content.
func concatSkillI18n(skillMdPath, lang string) string {
	base, err := os.ReadFile(skillMdPath)
	if err != nil {
		return ""
	}
	dir := filepath.Dir(skillMdPath)

	// Try SKILL-{lang}.md, fall back to SKILL-en.md
	langFile := filepath.Join(dir, "SKILL-"+lang+".md")
	data, err := os.ReadFile(langFile)
	if err != nil && lang != "en" {
		langFile = filepath.Join(dir, "SKILL-en.md")
		data, err = os.ReadFile(langFile)
	}
	if err != nil {
		return string(base) // no lang variant — just show SKILL.md
	}

	return string(base) + "\n---\n\n" + string(data)
}

func isHiddenEntry(name string) bool {
	return len(name) > 0 && name[0] == '.'
}

// ────────────────────────────────────────────────────────────────────────────
// LibraryModel — agent-scoped wrapper around MarkdownViewerModel with Ctrl+T
// cycling. Mirrors the /kanban (PropsModel) picker convention: Ctrl+T opens
// an overlay listing all non-human agents in the current network; ↑↓ navigate,
// Enter selects, Esc / Ctrl+T cancel.
// ────────────────────────────────────────────────────────────────────────────

// LibraryModel is the top-level /library view. It owns the current agent
// selection and delegates list/content rendering to an inner MarkdownViewer.
type LibraryModel struct {
	baseDir     string // .lingtai/ directory (for agent discovery)
	selectedDir string // working dir of the currently-displayed agent
	lang        string

	inner MarkdownViewerModel

	// Agent picker overlay state
	pickerOpen bool
	pickerIdx  int
	agentNodes []fs.AgentNode

	width  int
	height int
	ready  bool

	// Picker scrolling viewport (used only while picker is open so very large
	// networks don't overflow the screen).
	pickerVP viewport.Model
}

// libraryLoadMsg carries the discovered agent list for the picker.
type libraryLoadMsg struct {
	agentNodes []fs.AgentNode
}

// NewLibraryModel constructs the /library view rooted at baseDir (the .lingtai/
// directory) with the given agent pre-selected. The catalog is built eagerly so
// the first frame has content; the agent list for Ctrl+T is loaded async on
// Init.
func NewLibraryModel(baseDir, selectedDir, lang string) LibraryModel {
	entries := buildAgentLibraryCatalog(selectedDir, lang)
	inner := NewMarkdownViewer(entries, libraryTitleFor(selectedDir))
	inner.FooterHint = i18n.T("hints.props_select")
	return LibraryModel{
		baseDir:     baseDir,
		selectedDir: selectedDir,
		lang:        lang,
		inner:       inner,
	}
}

// libraryTitleFor returns the viewer title annotated with the current agent's
// display name so the user always knows whose catalog they're looking at.
func libraryTitleFor(agentDir string) string {
	base := i18n.T("library.title")
	if agentDir == "" {
		return base
	}
	// Try to read the agent's init.json for a nickname / agent_name. Falls
	// back to the directory basename.
	name := filepath.Base(agentDir)
	if manifest, err := fs.ReadInitManifest(agentDir); err == nil {
		if v, ok := manifest["nickname"].(string); ok && v != "" {
			name = v
		} else if v, ok := manifest["agent_name"].(string); ok && v != "" {
			name = v
		}
	}
	return fmt.Sprintf("%s — %s", base, name)
}

// loadAgents discovers all agents in baseDir for the Ctrl+T picker.
func (m LibraryModel) loadAgents() tea.Msg {
	net, _ := fs.BuildNetwork(m.baseDir)
	var nodes []fs.AgentNode
	for _, n := range net.Nodes {
		if n.IsHuman {
			continue
		}
		if n.WorkingDir == "" {
			continue
		}
		nodes = append(nodes, n)
	}
	return libraryLoadMsg{agentNodes: nodes}
}

func (m LibraryModel) Init() tea.Cmd {
	return tea.Batch(m.inner.Init(), m.loadAgents)
}

const (
	libraryHeaderLines = 2
	libraryFooterLines = 2
)

func (m LibraryModel) Update(msg tea.Msg) (LibraryModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - libraryHeaderLines - libraryFooterLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.pickerVP = viewport.New()
			m.ready = true
		}
		m.pickerVP.SetWidth(m.width)
		m.pickerVP.SetHeight(vpHeight)
		m.syncPicker()
		// Forward to inner so it can resize too.
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd

	case libraryLoadMsg:
		m.agentNodes = msg.agentNodes
		// If the current selectedDir isn't in the list (shouldn't happen but
		// be defensive), fall back to the first agent.
		found := false
		for _, n := range m.agentNodes {
			if n.WorkingDir == m.selectedDir {
				found = true
				break
			}
		}
		if !found && len(m.agentNodes) > 0 {
			// Don't switch underneath the user; just note it by leaving
			// pickerIdx at 0 so Ctrl+T highlights the first agent.
			m.pickerIdx = 0
		}
		return m, nil

	case tea.KeyPressMsg:
		if m.pickerOpen {
			return m.updatePicker(msg)
		}
		switch msg.String() {
		case "ctrl+t":
			if len(m.agentNodes) == 0 {
				// Picker opens even if empty so the user gets a visual cue;
				// the picker view handles the empty case.
				return m, nil
			}
			m.pickerOpen = true
			m.pickerIdx = 0
			for i, n := range m.agentNodes {
				if n.WorkingDir == m.selectedDir {
					m.pickerIdx = i
					break
				}
			}
			m.syncPicker()
			return m, nil
		}
		// Not a library-level key — forward to inner viewer (handles esc/q,
		// up/down, tab focus, pgup/pgdn, etc.).
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd

	case tea.MouseWheelMsg:
		if m.pickerOpen {
			var cmd tea.Cmd
			m.pickerVP, cmd = m.pickerVP.Update(msg)
			return m, cmd
		}
		var cmd tea.Cmd
		m.inner, cmd = m.inner.Update(msg)
		return m, cmd
	}

	// Default: forward to inner viewer.
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

// updatePicker mirrors PropsModel.updatePicker: up/down navigate, Enter selects,
// Esc / Ctrl+T cancel.
func (m LibraryModel) updatePicker(msg tea.KeyPressMsg) (LibraryModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+t":
		m.pickerOpen = false
		m.syncPicker()
		return m, nil
	case "up", "k":
		if m.pickerIdx > 0 {
			m.pickerIdx--
			m.syncPicker()
		}
		return m, nil
	case "down", "j":
		if m.pickerIdx < len(m.agentNodes)-1 {
			m.pickerIdx++
			m.syncPicker()
		}
		return m, nil
	case "enter":
		if m.pickerIdx < len(m.agentNodes) {
			newDir := m.agentNodes[m.pickerIdx].WorkingDir
			if newDir != "" && newDir != m.selectedDir {
				m.selectedDir = newDir
				// Rebuild the catalog + reset inner viewer so the list panel
				// scrolls back to top.
				entries := buildAgentLibraryCatalog(m.selectedDir, m.lang)
				m.inner = NewMarkdownViewer(entries, libraryTitleFor(m.selectedDir))
				m.inner.FooterHint = i18n.T("hints.props_select")
				// Propagate size so the new inner viewer is laid out.
				if m.width > 0 && m.height > 0 {
					var cmd tea.Cmd
					m.inner, cmd = m.inner.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
					m.pickerOpen = false
					m.syncPicker()
					return m, cmd
				}
			}
		}
		m.pickerOpen = false
		m.syncPicker()
		return m, nil
	}
	return m, nil
}

func (m *LibraryModel) syncPicker() {
	if !m.ready {
		return
	}
	if m.pickerOpen {
		m.pickerVP.SetContent(m.renderPicker())
	}
}

func (m LibraryModel) renderPicker() string {
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.select_agent")))
	lines = append(lines, "")

	if len(m.agentNodes) == 0 {
		lines = append(lines, "  "+StyleFaint.Render("(no agents)"))
		lines = append(lines, "")
		lines = append(lines, "  "+StyleFaint.Render("[esc/ctrl+t] "+i18n.T("manage.back")))
		return strings.Join(lines, "\n")
	}

	for i, n := range m.agentNodes {
		name := n.AgentName
		if n.Nickname != "" {
			name = n.Nickname
		}
		if name == "" {
			name = "(unknown)"
		}

		state := n.State
		if state == "" {
			state = "──"
		}
		stateRendered := lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(state))).Render(state)

		marker := "  "
		style := nameStyle
		if n.WorkingDir == m.selectedDir {
			marker = "● "
		}
		if i == m.pickerIdx {
			style = selectedStyle
			marker = "> "
			if n.WorkingDir == m.selectedDir {
				marker = ">●"
			}
		}

		lines = append(lines, fmt.Sprintf("  %s%-18s %s", marker, style.Render(name), stateRendered))
	}

	lines = append(lines, "")
	lines = append(lines, "  "+StyleFaint.Render("↑↓ "+i18n.T("manage.select")+"  [enter]  [esc/ctrl+t] "+i18n.T("manage.back")))

	return strings.Join(lines, "\n")
}

func (m LibraryModel) View() string {
	if m.pickerOpen {
		header := StyleTitle.Render("  "+libraryTitleFor(m.selectedDir)) + "\n" + strings.Repeat("\u2500", m.width)
		footer := strings.Repeat("\u2500", m.width) + "\n" +
			StyleFaint.Render("  "+i18n.T("hints.props_select"))
		body := ""
		if m.ready {
			body = m.pickerVP.View()
		}
		return header + "\n" + PaintViewportBG(body, m.width) + "\n" + footer
	}
	// Non-picker: show the inner viewer. We append a Ctrl+T hint to the footer
	// by letting the inner viewer render normally; the hint is included in
	// library's top-level footer (inner viewer has its own footer already).
	return m.inner.View()
}
