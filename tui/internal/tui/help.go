package tui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// helpSkillName is the bundled utility skill that owns the canonical TUI help
// content. /help is just a shortcut into this skill's slash-command assets.
const helpSkillName = "lingtai-tui-help"

// slashCommandsAsset returns the relative path (inside the lingtai-tui-help
// skill) of the slash-command guide for the given UI language. Unknown locales
// fall back to the English asset, which is canonical.
func slashCommandsAsset(lang string) string {
	switch lang {
	case "zh", "wen":
		return "assets/slash-commands." + lang + ".md"
	default:
		return "assets/slash-commands.en.md"
	}
}

// loadSlashCommands returns the slash-command help markdown for the given UI
// language, read from the embedded lingtai-tui-help skill. It falls back to the
// English asset if the language-specific asset is missing, and to a minimal
// placeholder if even that cannot be read (which should never happen, since the
// asset is embedded and covered by tests).
func loadSlashCommands(lang string) string {
	if content, err := preset.ReadBundledSkillFile(helpSkillName, slashCommandsAsset(lang)); err == nil {
		return content
	}
	if content, err := preset.ReadBundledSkillFile(helpSkillName, slashCommandsAsset("en")); err == nil {
		return content
	}
	return fmt.Sprintf("# %s\n\n%s", i18n.T("help.title"), i18n.T("help.missing_doc"))
}

// buildHelpEntries assembles the markdown viewer entries for the /help view. It
// is a single entry: the slash-command guide for the current UI language, loaded
// from the lingtai-tui-help skill. /help "jumps directly" to this asset rather
// than maintaining a separate embedded help-doc system.
func buildHelpEntries() []MarkdownEntry {
	return []MarkdownEntry{{
		Label:   i18n.T("help.overview_label"),
		Group:   i18n.T("help.group_commands"),
		Content: loadSlashCommands(i18n.Lang()),
	}}
}

// HelpModel is the /help view. It is a thin wrapper around MarkdownViewerModel
// showing the lingtai-tui-help slash-command guide for the current UI language.
// Esc/q close the inner viewer, which emits MarkdownViewerCloseMsg — App.Update
// already routes that back to the mail view, so no extra close plumbing is
// needed here.
type HelpModel struct {
	inner MarkdownViewerModel
}

// NewHelpModel builds the help view with the slash-command guide for the active
// UI language loaded from the bundled lingtai-tui-help skill.
func NewHelpModel() HelpModel {
	inner := NewMarkdownViewer(buildHelpEntries(), i18n.T("help.title"))
	return HelpModel{inner: inner}
}

func (m HelpModel) Init() tea.Cmd { return m.inner.Init() }

func (m HelpModel) Update(msg tea.Msg) (HelpModel, tea.Cmd) {
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m HelpModel) View() string { return m.inner.View() }
