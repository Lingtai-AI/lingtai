package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// Tier vocabulary mirrors the kernel-side TIER_VALUES in lingtai/presets.py.
// Stored on disk as `description.tier` ("1" through "5"). The TUI renders
// it locale-appropriately: stars (★..★★★★★) for English; the playful
// Chinese set (拉完了 / NPC / 顶级 / 人上人 / 夯) for zh and wen. The
// on-disk canonical form never changes — only the visual.
var tierValues = []string{"5", "4", "3", "2", "1"} // descending in the picker (best first)

// tierLabel renders a tier value as a locale-appropriate display string.
// Returns "" for unknown values so callers can guard with `if label != ""`.
//
// The Chinese set is intentionally vivid: 夯 (the strongest), 人上人 (people
// above persons — elite), 顶级 (top-tier), NPC (non-player character —
// gets work done, no glamour), 拉完了 (completely shit — free-tier scraps).
// `wen` falls back to the same Chinese set since classical Chinese reads
// the same characters.
func tierLabel(tier, lang string) string {
	if lang == "zh" || lang == "wen" {
		switch tier {
		case "1":
			return "拉完了"
		case "2":
			return "NPC"
		case "3":
			return "顶级"
		case "4":
			return "人上人"
		case "5":
			return "夯"
		}
		return ""
	}
	// English (and any other locale) → stars.
	switch tier {
	case "1":
		return "★"
	case "2":
		return "★★"
	case "3":
		return "★★★"
	case "4":
		return "★★★★"
	case "5":
		return "★★★★★"
	}
	return ""
}

// presetTier returns the preset's tier value, or "" when unset.
func presetTier(p preset.Preset) string {
	return p.Description.Tier
}

// tierChipStyle returns the lipgloss style used for a tier chip in the list.
// Higher tiers get warmer colors so the cost ladder reads top-down at a glance.
func tierChipStyle(tier string) lipgloss.Style {
	base := lipgloss.NewStyle().Padding(0, 1)
	switch tier {
	case "5":
		return base.Foreground(lipgloss.Color("213")).Bold(true) // bright magenta
	case "4":
		return base.Foreground(lipgloss.Color("214")) // orange
	case "3":
		return base.Foreground(lipgloss.Color("39")) // blue
	case "2":
		return base.Foreground(lipgloss.Color("84")) // green
	case "1":
		return base.Foreground(lipgloss.Color("245")) // grey
	default:
		return base.Foreground(lipgloss.Color("245"))
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Model
// ───────────────────────────────────────────────────────────────────────────

type presetLibraryFocus int

const (
	presetLibFocusList presetLibraryFocus = iota
	presetLibFocusTagPicker
	presetLibFocusEditor
)

// PresetLibraryModel is the dedicated screen for browsing and tagging the
// preset library at ~/.lingtai-tui/presets/.
type PresetLibraryModel struct {
	presets []preset.Preset
	cursor  int
	lang    string // "en", "zh", or "wen" — drives tier label rendering

	focus    presetLibraryFocus
	tierIdx  int    // selection within the tag picker (0..len(tierValues), last = "untag")
	saveErr  string // short error from the last save attempt
	editor   PresetEditorModel // active when focus == presetLibFocusEditor

	width  int
	height int
}

// NewPresetLibraryModel constructs the screen with presets pre-loaded.
// `lang` is the user's TUI language (en/zh/wen) and selects the tier
// label vocabulary — stars for English, 夯/人上人/顶级/NPC/拉完了 for
// Chinese-family locales.
func NewPresetLibraryModel(lang string) PresetLibraryModel {
	presets, _ := preset.List()
	return PresetLibraryModel{
		presets: presets,
		cursor:  0,
		lang:    lang,
	}
}

func (m PresetLibraryModel) Init() tea.Cmd { return nil }

func (m PresetLibraryModel) Update(msg tea.Msg) (PresetLibraryModel, tea.Cmd) {
	// Editor focus consumes ALL non-resize messages until it commits or
	// cancels. The two messages it emits are intercepted below.
	if m.focus == presetLibFocusEditor {
		switch typed := msg.(type) {
		case PresetEditorCommitMsg:
			toSave := typed.Preset
			if globalDir, err := config.GlobalDir(); err == nil {
				if cfg, err := config.LoadConfig(globalDir); err == nil {
					toSave = stampAutoEnvVar(toSave, cfg.Keys)
					// Persist a new key value if the user typed one in
					// the editor. Look up the env-var name *after*
					// stampAutoEnvVar so newly-assigned slots get the
					// right value.
					if typed.APIKeySet {
						if llm, ok := toSave.Manifest["llm"].(map[string]interface{}); ok {
							if envName, _ := llm["api_key_env"].(string); envName != "" {
								if cfg.Keys == nil {
									cfg.Keys = map[string]string{}
								}
								if typed.APIKey == "" {
									delete(cfg.Keys, envName)
								} else {
									cfg.Keys[envName] = typed.APIKey
								}
								_ = config.SaveConfig(globalDir, cfg)
							}
						}
					}
				}
			}
			if err := preset.Save(toSave); err != nil {
				m.saveErr = fmt.Sprintf("save failed: %v", err)
				return m, nil
			}
			m.presets, _ = preset.List()
			for i, q := range m.presets {
				if q.Name == toSave.Name {
					m.cursor = i
					break
				}
			}
			m.focus = presetLibFocusList
			m.saveErr = ""
			return m, nil
		case PresetEditorCancelMsg:
			m.focus = presetLibFocusList
			return m, nil
		default:
			var cmd tea.Cmd
			m.editor, cmd = m.editor.Update(msg)
			return m, cmd
		}
	}

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		// Tag picker overlay swallows keys while open.
		if m.focus == presetLibFocusTagPicker {
			switch msg.String() {
			case "esc":
				m.focus = presetLibFocusList
				m.saveErr = ""
				return m, nil
			case "up", "k":
				if m.tierIdx > 0 {
					m.tierIdx--
				}
				return m, nil
			case "down", "j":
				if m.tierIdx < len(tierValues) { // +1 slot for "untag"
					m.tierIdx++
				}
				return m, nil
			case "enter":
				if m.cursor < 0 || m.cursor >= len(m.presets) {
					m.focus = presetLibFocusList
					return m, nil
				}
				p := m.presets[m.cursor]
				var newTier string
				if m.tierIdx < len(tierValues) {
					newTier = tierValues[m.tierIdx]
				} // else: "untag" — newTier stays ""
				p.Description.Tier = newTier
				if err := preset.Save(p); err != nil {
					m.saveErr = fmt.Sprintf("save failed: %v", err)
					return m, nil
				}
				// Reload from disk to reflect the persisted state.
				m.presets, _ = preset.List()
				// Restore cursor to the same name (Save preserves filename, but
				// list order is alphabetic-by-name so position is stable; still
				// re-find to be safe in case of edge cases).
				for i, q := range m.presets {
					if q.Name == p.Name {
						m.cursor = i
						break
					}
				}
				m.focus = presetLibFocusList
				m.saveErr = ""
				return m, nil
			}
			return m, nil
		}

		// List-focus key handling.
		switch msg.String() {
		case "esc":
			// Reuse the existing close signal so app.go routes us back to
			// mail without needing a dedicated PresetLibraryCloseMsg.
			return m, func() tea.Msg { return MarkdownViewerCloseMsg{} }
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case "down", "j":
			if m.cursor < len(m.presets)-1 {
				m.cursor++
			}
			return m, nil
		case "t":
			// Open tag picker, prefilled with current tier (or "untag" slot).
			if m.cursor < 0 || m.cursor >= len(m.presets) {
				return m, nil
			}
			cur := presetTier(m.presets[m.cursor])
			m.tierIdx = len(tierValues) // default to "untag"
			for i, v := range tierValues {
				if v == cur {
					m.tierIdx = i
					break
				}
			}
			m.focus = presetLibFocusTagPicker
			m.saveErr = ""
			return m, nil
		case "enter":
			// Open the dedicated editor on the focused preset.
			if m.cursor < 0 || m.cursor >= len(m.presets) {
				return m, nil
			}
			// Load existing keys so the editor can prefill api_key.
			var keys map[string]string
			if globalDir, err := config.GlobalDir(); err == nil {
				if cfg, err := config.LoadConfig(globalDir); err == nil {
					keys = cfg.Keys
				}
			}
			m.editor = NewPresetEditorModel(m.presets[m.cursor], m.lang, keys)
			// Forward the current size so the editor renders immediately.
			updated, _ := m.editor.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
			m.editor = updated
			m.focus = presetLibFocusEditor
			m.saveErr = ""
			return m, m.editor.Init()
		case "r":
			// Reload from disk.
			m.presets, _ = preset.List()
			if m.cursor >= len(m.presets) {
				m.cursor = len(m.presets) - 1
			}
			if m.cursor < 0 {
				m.cursor = 0
			}
			return m, nil
		}
	}
	return m, nil
}

// ───────────────────────────────────────────────────────────────────────────
// View
// ───────────────────────────────────────────────────────────────────────────

func (m PresetLibraryModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	// Editor takes over the screen when active.
	if m.focus == presetLibFocusEditor {
		return m.editor.View()
	}

	// Layout: title bar (1) + body (flexible) + footer hint (1).
	bodyHeight := m.height - 2
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	// Two columns: list (left, ~40 wide) + preview (right, fills the rest).
	listWidth := 40
	if m.width < 80 {
		listWidth = m.width / 3
		if listWidth < 24 {
			listWidth = 24
		}
	}
	previewWidth := m.width - listWidth - 1
	if previewWidth < 10 {
		previewWidth = 10
	}

	title := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("212")).
		Render(i18n.T("preset_library.title"))

	left := m.renderList(listWidth, bodyHeight)
	right := m.renderPreview(previewWidth, bodyHeight)
	body := lipgloss.JoinHorizontal(lipgloss.Top, left, " ", right)

	hint := i18n.T("preset_library.hint")
	if m.focus == presetLibFocusTagPicker {
		hint = i18n.T("preset_library.tag_hint")
	}
	if m.saveErr != "" {
		hint = lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render(m.saveErr)
	}
	footer := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(hint)

	full := lipgloss.JoinVertical(lipgloss.Left, title, body, footer)

	// Tag picker overlay.
	if m.focus == presetLibFocusTagPicker {
		full = m.renderWithTagPicker(full)
	}

	return full
}

func (m PresetLibraryModel) renderList(width, height int) string {
	// Header row inside the list pane.
	header := lipgloss.NewStyle().
		Foreground(lipgloss.Color("245")).
		Render(fmt.Sprintf(i18n.T("preset_library.list_header"), len(m.presets)))

	rowStyle := lipgloss.NewStyle().Width(width)
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)

	var rows []string
	rows = append(rows, header)

	maxRows := height - 2 // leave room for header + bottom padding
	if maxRows < 1 {
		maxRows = 1
	}

	// Simple scrolling: keep cursor visible.
	start := 0
	if m.cursor >= maxRows {
		start = m.cursor - maxRows + 1
	}
	end := start + maxRows
	if end > len(m.presets) {
		end = len(m.presets)
	}

	for i := start; i < end; i++ {
		p := m.presets[i]
		marker := "  "
		nameStyle := lipgloss.NewStyle()
		if i == m.cursor {
			marker = "▸ "
			nameStyle = cursorStyle
		}
		// Render: "▸ name              [★★★]" (or 顶级 etc. in zh/wen)
		tier := presetTier(p)
		chip := ""
		if label := tierLabel(tier, m.lang); label != "" {
			chip = tierChipStyle(tier).Render(label)
		}
		nameField := nameStyle.Render(p.Name)
		// Crude right-alignment of the chip via padding.
		// Available room for (marker + name + chip) = width.
		used := lipgloss.Width(marker) + lipgloss.Width(nameField) + lipgloss.Width(chip)
		pad := width - used
		if pad < 1 {
			pad = 1
		}
		row := marker + nameField + strings.Repeat(" ", pad) + chip
		rows = append(rows, rowStyle.Render(row))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("245")).
		Width(width).
		Height(height).
		Padding(0, 1)

	return box.Render(strings.Join(rows, "\n"))
}

func (m PresetLibraryModel) renderPreview(width, height int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("245")).
		Width(width).
		Height(height).
		Padding(0, 1)

	if m.cursor < 0 || m.cursor >= len(m.presets) {
		return box.Render(i18n.T("preset_library.no_selection"))
	}
	p := m.presets[m.cursor]

	var b strings.Builder

	// Title with tier chip
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	b.WriteString(titleStyle.Render(p.Name))
	if label := tierLabel(presetTier(p), m.lang); label != "" {
		b.WriteString("  ")
		b.WriteString(tierChipStyle(presetTier(p)).Render(label))
	}
	b.WriteString("\n\n")

	// Description summary
	if p.Description.Summary != "" {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(p.Description.Summary))
		b.WriteString("\n\n")
	}

	// LLM block
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	if llm != nil {
		b.WriteString(sectionHead("LLM"))
		b.WriteString(kv("provider", asString(llm["provider"])))
		b.WriteString(kv("model", asString(llm["model"])))
		if v := asString(llm["base_url"]); v != "" {
			b.WriteString(kv("base_url", v))
		}
		if v := asString(llm["api_compat"]); v != "" {
			b.WriteString(kv("api_compat", v))
		}
		if v := asString(llm["api_key_env"]); v != "" {
			b.WriteString(kv("api_key_env", v))
		}
		if ctx, ok := llm["context_limit"].(float64); ok && ctx > 0 {
			b.WriteString(kv("context_limit", fmt.Sprintf("%d", int(ctx))))
		}
		b.WriteString("\n")
	}

	// Capabilities
	caps, _ := p.Manifest["capabilities"].(map[string]interface{})
	if caps != nil {
		b.WriteString(sectionHead(fmt.Sprintf("Capabilities (%d)", len(caps))))
		var names []string
		for k := range caps {
			names = append(names, k)
		}
		// Sort for stable rendering
		sortStrings(names)
		b.WriteString(strings.Join(names, ", "))
		b.WriteString("\n\n")
	}

	return box.Render(b.String())
}

// renderWithTagPicker overlays a small picker box centered on the screen.
func (m PresetLibraryModel) renderWithTagPicker(base string) string {
	if m.cursor < 0 || m.cursor >= len(m.presets) {
		return base
	}
	p := m.presets[m.cursor]

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	rowStyle := lipgloss.NewStyle().Width(28)
	cursorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)

	var rows []string
	rows = append(rows, titleStyle.Render(fmt.Sprintf(i18n.T("preset_library.tag_picker_title"), p.Name)))
	rows = append(rows, "")
	for i, v := range tierValues {
		marker := "  "
		style := lipgloss.NewStyle()
		if i == m.tierIdx {
			marker = "▸ "
			style = cursorStyle
		}
		label := tierLabel(v, m.lang)
		chip := tierChipStyle(v).Render(label)
		// Format: "▸  ★★★★    tier:4"  or  "▸  人上人    tier:4"
		row := rowStyle.Render(marker + chip + "  " + style.Render("tier:"+v))
		rows = append(rows, row)
	}
	// Bottom slot: "untag" / 清除
	{
		marker := "  "
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
		if m.tierIdx == len(tierValues) {
			marker = "▸ "
			style = cursorStyle
		}
		rows = append(rows, rowStyle.Render(marker+style.Render(i18n.T("preset_library.tag_clear"))))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("212")).
		Padding(1, 2).
		Render(strings.Join(rows, "\n"))

	// Center the overlay over the base view. lipgloss.Place is overkill here;
	// the underlying screen is still rendered, the overlay just sits on top
	// when the host terminal redraws — but Bubble Tea views are single-string.
	// We approximate centering by replacing a center region of the base.
	// For an MVP: render the base with the picker prepended via Place().
	return lipgloss.Place(
		m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

// ───────────────────────────────────────────────────────────────────────────
// Small helpers (kept private to avoid leaking into other screens)
// ───────────────────────────────────────────────────────────────────────────

func sectionHead(title string) string {
	style := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true)
	return style.Render("── "+title+" ──") + "\n"
}

func kv(key, value string) string {
	if value == "" {
		return ""
	}
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(14)
	return keyStyle.Render("  "+key) + value + "\n"
}

func asString(v interface{}) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}

// sortStrings is a tiny in-place sort. Using sort.Strings would be fine but
// we want to avoid importing "sort" twice in this file's import block.
func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j-1] > xs[j]; j-- {
			xs[j-1], xs[j] = xs[j], xs[j-1]
		}
	}
}
