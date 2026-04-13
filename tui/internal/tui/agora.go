package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/bubbles/v2/viewport"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// ── types ────────────────────────────────────────────────────────────

type agoraTab int

const (
	agoraTabNetworks agoraTab = iota
	agoraTabRecipes
)

type agoraLevel int

const (
	agoraLevelList agoraLevel = iota
	agoraLevelDetail
)

type recipeListEntry struct {
	Info preset.RecipeInfo
	Dir  string
}

// recipesLoadMsg carries loaded recipe entries.
type recipesLoadMsg struct {
	recipes []recipeListEntry
}

// AgoraModel is a two-tab browser (Networks / Recipes) for the /agora command.
type AgoraModel struct {
	globalDir, projectDir string
	width, height         int
	tab                   agoraTab
	level                 agoraLevel

	// Networks tab
	networks ProjectsModel

	// Recipes tab
	recipes      []recipeListEntry
	recipeCursor int
	recipeVP     viewport.Model
	recipeReady  bool

	// Detail level (shared)
	detail     MarkdownViewerModel
	detailName string
}

// ── constructor ──────────────────────────────────────────────────────

// NewAgoraModel creates the agora model with its networks sub-model.
func NewAgoraModel(globalDir, projectDir string) AgoraModel {
	return AgoraModel{
		globalDir: globalDir,
		projectDir: projectDir,
		networks:  NewAgoraProjectsModel(globalDir, projectDir),
	}
}

// ── tea.Model ────────────────────────────────────────────────────────

func (m AgoraModel) Init() tea.Cmd {
	return tea.Batch(m.networks.Init(), m.loadRecipes)
}

func (m AgoraModel) loadRecipes() tea.Msg {
	lang := i18n.Lang()
	agora := preset.ScanAgoraRecipes(lang)
	var entries []recipeListEntry
	for _, ar := range agora {
		entries = append(entries, recipeListEntry{
			Info: ar.Info,
			Dir:  ar.Dir,
		})
	}
	return recipesLoadMsg{recipes: entries}
}

func (m AgoraModel) Update(msg tea.Msg) (AgoraModel, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		// Relay to active sub-models
		var cmd tea.Cmd
		m.networks, cmd = m.networks.Update(msg)
		// Recipe viewport
		vpHeight := m.height - agoraHeaderLines - agoraFooterLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.recipeReady {
			m.recipeVP = viewport.New()
			m.recipeVP.SetWidth(m.width)
			m.recipeVP.SetHeight(vpHeight)
			m.recipeReady = true
		} else {
			m.recipeVP.SetWidth(m.width)
			m.recipeVP.SetHeight(vpHeight)
		}
		m.syncRecipeViewport()
		if m.level == agoraLevelDetail {
			var dcmd tea.Cmd
			m.detail, dcmd = m.detail.Update(msg)
			return m, tea.Batch(cmd, dcmd)
		}
		return m, cmd

	case recipesLoadMsg:
		m.recipes = msg.recipes
		if m.recipeCursor >= len(m.recipes) {
			m.recipeCursor = max(0, len(m.recipes)-1)
		}
		m.syncRecipeViewport()

	case MarkdownViewerCloseMsg:
		m.level = agoraLevelList
		return m, nil

	case agoraTabToggleMsg:
		if m.level == agoraLevelList {
			if m.tab == agoraTabNetworks {
				m.tab = agoraTabRecipes
			} else {
				m.tab = agoraTabNetworks
			}
		}
		return m, nil

	case agoraDetailMsg:
		// Open detail for a network's recipe directory
		entries := buildRecipeEntries(msg.dir)
		if len(entries) == 0 {
			return m, nil
		}
		m.detail = NewMarkdownViewer(entries, msg.name)
		m.detailName = msg.name
		m.level = agoraLevelDetail
		sizeCmd := func() tea.Msg {
			return tea.WindowSizeMsg{Width: m.width, Height: m.height}
		}
		return m, sizeCmd

	default:
		// Delegate to the active sub-model at the current level
		if m.level == agoraLevelDetail {
			var cmd tea.Cmd
			m.detail, cmd = m.detail.Update(msg)
			return m, cmd
		}

		switch m.tab {
		case agoraTabNetworks:
			var cmd tea.Cmd
			m.networks, cmd = m.networks.Update(msg)
			return m, cmd
		case agoraTabRecipes:
			return m.updateRecipeList(msg)
		}
	}

	return m, nil
}

// ── recipe list key handling ─────────────────────────────────────────

func (m AgoraModel) updateRecipeList(msg tea.Msg) (AgoraModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		switch msg.String() {
		case "up", "k":
			if m.recipeCursor > 0 {
				m.recipeCursor--
				m.syncRecipeViewport()
			}
			return m, nil
		case "down", "j":
			if m.recipeCursor < len(m.recipes)-1 {
				m.recipeCursor++
				m.syncRecipeViewport()
			}
			return m, nil
		case "enter":
			if m.recipeCursor < len(m.recipes) && len(m.recipes) > 0 {
				r := m.recipes[m.recipeCursor]
				entries := buildRecipeEntries(r.Dir)
				if len(entries) == 0 {
					return m, nil
				}
				m.detail = NewMarkdownViewer(entries, r.Info.Name)
				m.detailName = r.Info.Name
				m.level = agoraLevelDetail
				sizeCmd := func() tea.Msg {
					return tea.WindowSizeMsg{Width: m.width, Height: m.height}
				}
				return m, sizeCmd
			}
			return m, nil
		case "ctrl+t":
			return m, func() tea.Msg { return agoraTabToggleMsg{} }
		case "r":
			return m, m.loadRecipes
		case "esc", "q":
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
		default:
			var cmd tea.Cmd
			m.recipeVP, cmd = m.recipeVP.Update(msg)
			return m, cmd
		}
	case tea.MouseWheelMsg:
		var cmd tea.Cmd
		m.recipeVP, cmd = m.recipeVP.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ── recipe viewport sync ────────────────────────────────────────────

const (
	agoraHeaderLines = 2
	agoraFooterLines = 2
)

func (m *AgoraModel) syncRecipeViewport() {
	if !m.recipeReady {
		return
	}
	m.recipeVP.SetContent(m.renderRecipeBody())
}

func (m AgoraModel) renderRecipeBody() string {
	leftW := m.width / 3
	if leftW < 25 {
		leftW = 25
	}
	if leftW > 40 {
		leftW = 40
	}
	rightW := m.width - leftW - 1
	if rightW < 20 {
		rightW = 20
	}
	if leftW+1+rightW > m.width && m.width > 1 {
		rightW = m.width - leftW - 1
		if rightW < 0 {
			rightW = 0
		}
	}

	leftContent := m.renderRecipeLeft(leftW)
	rightContent := m.renderRecipeRight(rightW)

	leftLines := strings.Split(leftContent, "\n")
	rightLines := strings.Split(rightContent, "\n")

	vpHeight := m.height - agoraHeaderLines - agoraFooterLines
	if vpHeight < 1 {
		vpHeight = 1
	}
	for len(leftLines) < vpHeight {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < vpHeight {
		rightLines = append(rightLines, "")
	}
	for len(leftLines) < len(rightLines) {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < len(leftLines) {
		rightLines = append(rightLines, "")
	}

	sep := lipgloss.NewStyle().Foreground(ColorTextFaint).Render("│")
	var body strings.Builder
	for i := 0; i < len(leftLines); i++ {
		l := padToWidth(leftLines[i], leftW)
		body.WriteString(l + sep + rightLines[i] + "\n")
	}
	return strings.TrimRight(body.String(), "\n")
}

func (m AgoraModel) renderRecipeLeft(maxW int) string {
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("agora.tab_recipes")))
	lines = append(lines, "")

	if len(m.recipes) == 0 {
		lines = append(lines, "  "+StyleFaint.Render(i18n.T("agora.no_recipes")))
	}

	for i, r := range m.recipes {
		marker := "  "
		style := nameStyle
		if i == m.recipeCursor {
			marker = "> "
			style = selectedStyle
		}
		lines = append(lines, "  "+marker+style.Render(r.Info.Name))
	}

	return strings.Join(lines, "\n")
}

func (m AgoraModel) renderRecipeRight(maxW int) string {
	if len(m.recipes) == 0 {
		return "\n  " + StyleFaint.Render("(no recipes)")
	}
	if m.recipeCursor >= len(m.recipes) {
		return ""
	}

	r := m.recipes[m.recipeCursor]
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string
	lines = append(lines, "")

	// Description
	if r.Info.Description != "" {
		lines = append(lines, "  "+valueStyle.Render(r.Info.Description))
		lines = append(lines, "")
	}

	// File listing
	lines = append(lines, "  "+sectionStyle.Render("Files"))
	lines = append(lines, "")

	checkFile := func(name string) bool {
		_, err := os.Stat(filepath.Join(r.Dir, name))
		return err == nil
	}

	for _, fname := range []string{"greet.md", "comment.md", "covenant.md", "procedures.md"} {
		if checkFile(fname) {
			lines = append(lines, "  "+labelStyle.Render("  ✓ ")+valueStyle.Render(fname))
		}
	}

	// Skills count
	skillsDir := filepath.Join(r.Dir, "skills")
	skillCount := 0
	if entries, err := os.ReadDir(skillsDir); err == nil {
		for _, e := range entries {
			if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
				skillCount++
			}
		}
	}
	if skillCount > 0 {
		lines = append(lines, "  "+labelStyle.Render("  ✓ ")+valueStyle.Render(fmt.Sprintf("skills (%d)", skillCount)))
	}

	return strings.Join(lines, "\n")
}

// ── view ─────────────────────────────────────────────────────────────

func (m AgoraModel) View() string {
	// Detail level — delegate entirely
	if m.level == agoraLevelDetail {
		return m.detail.View()
	}

	// Networks tab — delegate to ProjectsModel
	if m.tab == agoraTabNetworks {
		return m.networks.View()
	}

	// Recipes tab
	title := StyleTitle.Render("  "+i18n.T("agora.title_recipes")) + "\n" + strings.Repeat("\u2500", m.width)

	scrollHint := ""
	if m.recipeReady && !m.recipeVP.AtBottom() {
		scrollHint = " " + RuneBullet + " pgup/pgdn scroll"
	}
	footer := strings.Repeat("\u2500", m.width) + "\n" +
		StyleFaint.Render("  "+i18n.T("hints.agora_recipes")+scrollHint)

	return title + "\n" + m.recipeVP.View() + "\n" + footer
}
