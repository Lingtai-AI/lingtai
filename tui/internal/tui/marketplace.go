package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// MarketplaceModel is the /marketplace view: a browse + detail surface over the
// recipe marketplace, where a marketplace entry is an external-skill + recipe
// combination (a recipe that ships a library the agent can use).
//
// It is a thin wrapper over MarkdownViewerModel (the same two-panel list/detail
// widget /notification and the skills browser use). The left panel lists
// entries grouped by origin — locally-present pairings selectable in /setup vs
// community pairings that require manual import + preview before use. The right
// panel renders each entry's detail: source, recipe/library references, tags,
// and the safety/preview text.
//
// The MVP does NOT install or fetch anything. Community entries are browse +
// preview only; using one is a documented manual step. The model is structured
// so a future validated importer slots in at the "install" action without
// reshaping the surface.
type MarketplaceModel struct {
	globalDir  string
	projectDir string
	lang       string

	width  int
	height int

	entries []preset.MarketplaceEntry
	viewer  MarkdownViewerModel
	err     string
}

// NewMarketplaceModel builds the marketplace view from the curated static
// registry plus any locally-present external-skill + recipe pairings.
func NewMarketplaceModel(globalDir, projectDir, lang string) MarketplaceModel {
	m := MarketplaceModel{globalDir: globalDir, projectDir: projectDir, lang: lang}
	m.load()
	return m
}

func (m *MarketplaceModel) load() {
	m.entries = preset.MarketplaceEntries(m.globalDir, m.lang)
	entries := marketplaceMarkdownEntries(m.entries, m.lang)
	viewer := NewMarkdownViewer(entries, i18n.T("marketplace.title"))
	viewer.FooterHint = i18n.T("marketplace.footer")
	if m.width > 0 || m.height > 0 {
		updated, _ := viewer.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		viewer = updated
	}
	viewer.syncLeft()
	viewer.syncRight()
	m.viewer = viewer
}

func (m MarketplaceModel) Init() tea.Cmd { return nil }

func (m MarketplaceModel) Update(msg tea.Msg) (MarketplaceModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		m.viewer.syncLeft()
		m.viewer.syncRight()
		return m, cmd
	case MarkdownViewerCloseMsg:
		return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
	case MarkdownViewerSelectMsg:
		// Selecting an entry is a no-op in the MVP — install/use is manual.
		// A future validated importer would branch here on the entry's Install
		// state. Keeping the hook silent avoids implying an action exists.
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "backspace":
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
		case "r", "ctrl+r":
			m.load()
			return m, nil
		}
	}
	var cmd tea.Cmd
	m.viewer, cmd = m.viewer.Update(msg)
	return m, cmd
}

func (m MarketplaceModel) View() string {
	viewer := m.viewer
	if (m.width > 0 || m.height > 0) && (viewer.width != m.width || viewer.height != m.height || viewer.rightVP.Width() == 0) {
		updated, _ := viewer.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		viewer = updated
		viewer.syncLeft()
		viewer.syncRight()
	}
	return viewer.View()
}

// marketplaceMarkdownEntries converts marketplace entries into MarkdownEntry
// items for the two-panel viewer, grouped by origin. Local (selectable)
// pairings come first, then community (import-only) pairings.
func marketplaceMarkdownEntries(entries []preset.MarketplaceEntry, lang string) []MarkdownEntry {
	groupLocal := i18n.T("marketplace.group.local")
	groupCommunity := i18n.T("marketplace.group.community")

	var out []MarkdownEntry
	emit := func(want preset.MarketplaceOrigin, group string) {
		for _, e := range entries {
			if e.Origin != want {
				continue
			}
			out = append(out, MarkdownEntry{
				Label:       e.Recipe.Name,
				Description: marketplaceEntrySubtitle(e),
				Group:       group,
				Content:     marketplaceEntryDetail(e),
				Remote:      e.SourceURL,
			})
		}
	}
	emit(preset.OriginLocal, groupLocal)
	emit(preset.OriginCommunity, groupCommunity)

	if len(out) == 0 {
		out = append(out, MarkdownEntry{
			Label:   i18n.T("marketplace.empty.label"),
			Group:   groupCommunity,
			Content: i18n.T("marketplace.empty.body"),
		})
	}
	return out
}

// marketplaceEntrySubtitle is the faint one-liner under the entry name in the
// list: install state + external skill library name.
func marketplaceEntrySubtitle(e preset.MarketplaceEntry) string {
	state := i18n.T("marketplace.install.manual")
	if e.Install == preset.InstallReady {
		state = i18n.T("marketplace.install.ready")
	}
	return fmt.Sprintf("%s · %s %s", state, i18n.T("marketplace.field.skill"), e.LibraryName)
}

// marketplaceEntryDetail renders the full detail markdown for one entry: the
// recipe half, the external-skill half, provenance, tags, install state, and
// the safety/preview boundary text.
func marketplaceEntryDetail(e preset.MarketplaceEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", e.Recipe.Name)
	if e.Recipe.Description != "" {
		fmt.Fprintf(&b, "%s\n\n", e.Recipe.Description)
	}

	install := i18n.T("marketplace.install.manual")
	if e.Install == preset.InstallReady {
		install = i18n.T("marketplace.install.ready")
	}
	fmt.Fprintf(&b, "- **%s:** %s\n", i18n.T("marketplace.field.status"), install)

	// The recipe half.
	fmt.Fprintf(&b, "- **%s:** `%s` (v%s)\n", i18n.T("marketplace.field.recipe"), e.Recipe.ID, e.Recipe.Version)
	// The external-skill half — this is what makes it a marketplace entry.
	fmt.Fprintf(&b, "- **%s:** `%s`\n", i18n.T("marketplace.field.skill"), e.LibraryName)
	if len(e.LibrarySkills) > 0 {
		fmt.Fprintf(&b, "- **%s:** %s\n", i18n.T("marketplace.field.skills"), "`"+strings.Join(e.LibrarySkills, "`, `")+"`")
	}
	if e.Author != "" {
		fmt.Fprintf(&b, "- **%s:** %s\n", i18n.T("marketplace.field.author"), e.Author)
	}
	if e.SourceURL != "" {
		fmt.Fprintf(&b, "- **%s:** %s\n", i18n.T("marketplace.field.source"), e.SourceURL)
	}
	if len(e.Tags) > 0 {
		fmt.Fprintf(&b, "- **%s:** %s\n", i18n.T("marketplace.field.tags"), strings.Join(e.Tags, ", "))
	}
	b.WriteString("\n")

	// Safety / preview boundary.
	fmt.Fprintf(&b, "## %s\n\n%s\n\n", i18n.T("marketplace.field.safety"), e.Safety)

	// Use / install guidance — differs by install state.
	fmt.Fprintf(&b, "## %s\n\n", i18n.T("marketplace.field.use"))
	if e.Install == preset.InstallReady {
		b.WriteString(i18n.T("marketplace.use.ready") + "\n")
	} else {
		b.WriteString(i18n.T("marketplace.use.manual") + "\n")
		if e.SourceURL != "" {
			fmt.Fprintf(&b, "\n%s: %s\n\n```\n~/lingtai-agora/recipes/%s\n```\n", i18n.T("marketplace.field.source"), e.SourceURL, e.Recipe.ID)
		}
	}
	return b.String()
}
