package tui

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/sqlitelog"
)

// NotificationModel is the /notification view: a history browser over the
// latest 10 notification_pair_injected blocks from logs/log.sqlite.
// Left/right keys step among the in-memory list; r/ctrl+r reloads.
// Esc returns to the mail view.
type NotificationModel struct {
	agentDir string
	width    int
	height   int

	// latest 10 blocks loaded on open/reload, index 0 = newest
	blocks []sqlitelog.NotificationBlock

	// cursor into blocks; -1 means no blocks available
	cursor int

	// error from last query (shown inline)
	err string
}

// NewNotificationModel creates the /notification model for agentDir.
// It immediately loads the latest 10 notification blocks.
func NewNotificationModel(agentDir string) NotificationModel {
	m := NotificationModel{agentDir: agentDir, cursor: -1}
	m.load()
	return m
}

func (m *NotificationModel) load() {
	if m.agentDir == "" {
		m.err = "No agent selected."
		return
	}
	if !sqlitelog.Exists(m.agentDir) {
		m.err = "logs/log.sqlite not found. Run `lingtai-agent log rebuild <agent_dir>` to create it."
		return
	}
	blocks, err := sqlitelog.QueryNotificationBlocks(m.agentDir, 10)
	if err != nil {
		m.err = fmt.Sprintf("query error: %v", err)
		return
	}
	m.err = ""
	m.blocks = blocks
	if len(blocks) > 0 {
		m.cursor = 0
	} else {
		m.cursor = -1
	}
}

func (m NotificationModel) Init() tea.Cmd { return nil }

func (m NotificationModel) Update(msg tea.Msg) (NotificationModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "backspace":
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
		case "left":
			// older = higher cursor index (index 0 = newest)
			if m.cursor >= 0 && m.cursor < len(m.blocks)-1 {
				m.cursor++
			}
		case "right":
			// newer = lower cursor index
			if m.cursor > 0 {
				m.cursor--
			}
		case "ctrl+r", "r":
			m.load()
		}
	}
	return m, nil
}

func (m NotificationModel) View() string {
	title := notificationTitle(m.agentDir)
	hint := StyleFaint.Render("← older  → newer  r reload  esc back")

	if m.err != "" {
		body := StyleSubtle.Render(m.err)
		return renderNotificationPanel(title, body, hint, m.width, m.height)
	}

	if len(m.blocks) == 0 || m.cursor < 0 {
		body := StyleSubtle.Render("No notification_pair_injected blocks found in logs/log.sqlite.")
		return renderNotificationPanel(title, body, hint, m.width, m.height)
	}

	block := m.blocks[m.cursor]
	body := renderNotificationBlock(block, m.cursor, len(m.blocks), m.blockWrapWidth())
	return renderNotificationPanel(title, body, hint, m.width, m.height)
}

func notificationTitle(agentDir string) string {
	base := i18n.T("palette.notification")
	if agentDir == "" {
		return base
	}
	return fmt.Sprintf("%s — %s", base, filepath.Base(agentDir))
}

func (m NotificationModel) blockWrapWidth() int {
	wrapWidth := m.width - 8
	if wrapWidth < 40 {
		return 40
	}
	if wrapWidth > 120 {
		return 120
	}
	return wrapWidth
}

// renderNotificationBlock formats a single NotificationBlock for display,
// mirroring the mail view's notification render style.
func renderNotificationBlock(b sqlitelog.NotificationBlock, cursor, total, wrapWidth int) string {
	var sb strings.Builder

	if wrapWidth <= 0 {
		wrapWidth = 76
	}

	// ── Block index counter ─────────────────────────────────────────────────
	idxStyle := StyleFaint
	sb.WriteString(idxStyle.Render(fmt.Sprintf("block %d of %d", cursor+1, total)))
	sb.WriteString("\n")

	// ── Event identity row ──────────────────────────────────────────────────
	tsStr := b.Time().Format(time.RFC3339)
	idStyle := StyleFaint
	callStyle := StyleFaint

	idPart := idStyle.Render(fmt.Sprintf("id=%d", b.ID))
	tsPart := StyleSubtle.Render(tsStr)
	row := idPart + "  " + tsPart
	if b.CallID != "" {
		row += "  " + callStyle.Render("call_id="+b.CallID)
	}
	sb.WriteString(row + "\n")
	sb.WriteString("\n")

	// ── Body text (summary) ─────────────────────────────────────────────────
	notifStyle := lipgloss.NewStyle().Foreground(ColorAgent).Italic(true)
	labelStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	sb.WriteString(labelStyle.Render("  ✉ notifications") + "\n")
	if b.Summary != "" {
		wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(b.Summary)
		for _, line := range strings.Split(wrapped, "\n") {
			sb.WriteString(notifStyle.Render("    "+line) + "\n")
		}
	}

	// ── Sources list ────────────────────────────────────────────────────────
	if len(b.Sources) > 0 {
		for _, src := range b.Sources {
			sb.WriteString(notifStyle.Render("    • "+src) + "\n")
		}
	}

	// ── Meta footer (context%, stamina, time, seq) ──────────────────────────
	if b.Meta != nil {
		if footer := formatBlockMetaFooter(b.Meta); footer != "" {
			footerStyle := notifStyle.Faint(true)
			sb.WriteString(footerStyle.Render("    "+footer) + "\n")
		}
	}

	return sb.String()
}

// formatBlockMetaFooter renders the NotificationBlockMeta vital signs as
// a compact line like "ctx 14.8% · stamina 9h58m · 21:10 PDT · seq 2".
// Returns "" when no displayable fields are present.
func formatBlockMetaFooter(m *sqlitelog.NotificationBlockMeta) string {
	if m == nil {
		return ""
	}
	var parts []string
	if m.ContextUsage > 0 {
		parts = append(parts, fmt.Sprintf("ctx %.1f%%", m.ContextUsage*100))
	}
	if m.StaminaLeftSeconds > 0 {
		parts = append(parts, "stamina "+formatStaminaShort(m.StaminaLeftSeconds))
	}
	if m.CurrentTime != "" {
		if short := formatCurrentTimeShort(m.CurrentTime); short != "" {
			parts = append(parts, short)
		}
	}
	if m.InjectionSeq > 0 {
		parts = append(parts, fmt.Sprintf("seq %d", m.InjectionSeq))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// renderNotificationPanel wraps content in a simple titled box.
func renderNotificationPanel(title, body, hint string, width, height int) string {
	if width == 0 {
		width = 80
	}
	if height == 0 {
		height = 24
	}

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	divider := StyleFaint.Render(strings.Repeat("─", max(0, width-4)))

	var b strings.Builder
	b.WriteString(titleStyle.Render(title))
	b.WriteString("\n")
	b.WriteString(divider)
	b.WriteString("\n")
	b.WriteString(body)

	// Pad to height-2 so the hint sticks to the bottom.
	lines := strings.Count(b.String(), "\n") + 1
	pad := height - lines - 2
	if pad > 0 {
		b.WriteString(strings.Repeat("\n", pad))
	}
	b.WriteString("\n")
	b.WriteString(hint)

	return b.String()
}
