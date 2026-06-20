package tui

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/sqlitelog"
)

// NotificationModel is the /notification view: a history browser over the
// latest 10 notification_block_injected snapshots from logs/log.sqlite.
// Each snapshot carries the actual canonical payload the agent saw
// (notifications + _notification_guidance), not just a compact summary.
// Left/right keys step among the in-memory list; r/ctrl+r reloads.
// Esc returns to the mail view.
type NotificationModel struct {
	agentDir string
	width    int
	height   int

	// latest 10 actual-block snapshots loaded on open/reload, index 0 = newest
	snapshots []sqlitelog.NotificationBlockSnapshot

	// cursor into snapshots; -1 means no snapshots available
	cursor int

	// error from last query (shown inline)
	err string
}

// NewNotificationModel creates the /notification model for agentDir.
// It immediately loads the latest 10 notification_block_injected snapshots.
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
	snaps, err := sqlitelog.QueryNotificationBlockSnapshots(m.agentDir, 10)
	if err != nil {
		m.err = fmt.Sprintf("query error: %v", err)
		return
	}
	m.err = ""
	m.snapshots = snaps
	if len(snaps) > 0 {
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
			if m.cursor >= 0 && m.cursor < len(m.snapshots)-1 {
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

	if len(m.snapshots) == 0 || m.cursor < 0 {
		body := StyleSubtle.Render(
			"No persisted notification_block_injected snapshots found. " +
				"This log may predate actual notification block persistence.",
		)
		return renderNotificationPanel(title, body, hint, m.width, m.height)
	}

	snap := m.snapshots[m.cursor]
	body := renderNotificationSnapshot(snap, m.cursor, len(m.snapshots), m.blockWrapWidth())
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

// renderNotificationSnapshot formats a single NotificationBlockSnapshot for display.
// It shows the event identity, global _notification_guidance, and each channel's
// actual payload from the canonical block the agent saw.
func renderNotificationSnapshot(s sqlitelog.NotificationBlockSnapshot, cursor, total, wrapWidth int) string {
	var sb strings.Builder

	if wrapWidth <= 0 {
		wrapWidth = 76
	}

	// ── Block index counter ─────────────────────────────────────────────────
	sb.WriteString(StyleFaint.Render(fmt.Sprintf("snapshot %d of %d", cursor+1, total)))
	sb.WriteString("\n")

	// ── Event identity row ──────────────────────────────────────────────────
	tsStr := s.Time().Format(time.RFC3339)
	idPart := StyleFaint.Render(fmt.Sprintf("id=%d", s.ID))
	tsPart := StyleSubtle.Render(tsStr)
	row := idPart + "  " + tsPart
	if s.Mode != "" {
		row += "  " + StyleFaint.Render("mode="+s.Mode)
	}
	if s.CallID != "" {
		row += "  " + StyleFaint.Render("call_id="+s.CallID)
	}
	sb.WriteString(row + "\n")
	sb.WriteString("\n")

	labelStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	notifStyle := lipgloss.NewStyle().Foreground(ColorAgent).Italic(true)

	// ── Global _notification_guidance ───────────────────────────────────────
	if s.Guidance != "" {
		sb.WriteString(labelStyle.Render("  ✦ _notification_guidance") + "\n")
		wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(s.Guidance)
		for _, line := range strings.Split(wrapped, "\n") {
			sb.WriteString(notifStyle.Faint(true).Render("    "+line) + "\n")
		}
		sb.WriteString("\n")
	}

	// ── Per-channel notification payloads ───────────────────────────────────
	if len(s.Notifications) > 0 {
		sb.WriteString(labelStyle.Render("  ✉ notifications") + "\n")
		// Render channels in sorted order for determinism.
		channels := make([]string, 0, len(s.Notifications))
		for ch := range s.Notifications {
			channels = append(channels, ch)
		}
		sort.Strings(channels)
		for _, ch := range channels {
			payload := s.Notifications[ch]
			sb.WriteString(labelStyle.Render("    ["+ch+"]") + "\n")
			for _, line := range strings.Split(payload, "\n") {
				sb.WriteString(notifStyle.Render("      "+line) + "\n")
			}
		}
	} else if len(s.Sources) > 0 {
		// Fallback: sources list without payload body (malformed/old event)
		sb.WriteString(labelStyle.Render("  ✉ sources") + "\n")
		for _, src := range s.Sources {
			sb.WriteString(notifStyle.Render("    • "+src) + "\n")
		}
	}

	// ── Meta footer (context%, stamina, time, seq) ──────────────────────────
	if s.Meta != nil {
		if footer := formatBlockMetaFooter(s.Meta); footer != "" {
			sb.WriteString(notifStyle.Faint(true).Render("    "+footer) + "\n")
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
