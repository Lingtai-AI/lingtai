package tui

import (
	"fmt"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const (
	agentRailWidth     = 24
	agentRailRowsStart = 2
)

// agentRailState is presentation state over the canonical V1 selector. Rows,
// cursor, current conversation, discovery, activation, publication, and durable
// unread state remain owned by agentSelectorState and the direct core.
type agentRailState struct {
	focused        bool
	scrollOffset   int
	unreadByThread map[string]int
}

func agentRailVisibleRows(height int) int {
	visible := height - agentRailRowsStart
	if visible < 1 {
		return 1
	}
	return visible
}

func agentRailMaxScroll(rowCount, height int) int {
	maximum := rowCount - agentRailVisibleRows(height)
	if maximum < 0 {
		return 0
	}
	return maximum
}

func clampAgentRailOffset(offset, rowCount, height int) int {
	if offset < 0 {
		return 0
	}
	maximum := agentRailMaxScroll(rowCount, height)
	if offset > maximum {
		return maximum
	}
	return offset
}

func (m MailModel) clampAgentRail() MailModel {
	m.agentRail.scrollOffset = clampAgentRailOffset(
		m.agentRail.scrollOffset,
		len(m.agentSelector.rows),
		m.height,
	)
	return m
}

func (m MailModel) keepAgentRailCursorVisible() MailModel {
	m = m.clampAgentRail()
	if len(m.agentSelector.rows) == 0 {
		return m
	}
	visible := agentRailVisibleRows(m.height)
	if m.agentSelector.cursor < m.agentRail.scrollOffset {
		m.agentRail.scrollOffset = m.agentSelector.cursor
	} else if m.agentSelector.cursor >= m.agentRail.scrollOffset+visible {
		m.agentRail.scrollOffset = m.agentSelector.cursor - visible + 1
	}
	return m.clampAgentRail()
}

func (m MailModel) focusAgentRail() MailModel {
	m.agentRail.focused = true
	m.input.Blur()
	return m.keepAgentRailCursorVisible()
}

func (m MailModel) blurAgentRail() MailModel {
	m.agentRail.focused = false
	_ = m.input.Focus()
	return m
}

func (m MailModel) scrollAgentRail(delta int) MailModel {
	m.agentRail.scrollOffset = clampAgentRailOffset(
		m.agentRail.scrollOffset+delta,
		len(m.agentSelector.rows),
		m.height,
	)
	return m
}

func (m MailModel) agentRailRowAt(childY int) int {
	if childY < agentRailRowsStart || childY >= m.height {
		return -1
	}
	offset := clampAgentRailOffset(m.agentRail.scrollOffset, len(m.agentSelector.rows), m.height)
	index := offset + childY - agentRailRowsStart
	if index < 0 || index >= len(m.agentSelector.rows) {
		return -1
	}
	return index
}

// recomputeAgentRailUnread replaces the ephemeral badge map only after every
// current canonical direct row is counted successfully from the installed V1
// store and immutable accepted publication. Any missing or mismatched input
// preserves the previous complete map.
func (m MailModel) recomputeAgentRailUnread() MailModel {
	if m.directUnread == nil || m.directPublication == nil {
		return m
	}
	next := make(map[string]int)
	for _, row := range m.agentSelector.rows {
		if row.Main {
			continue
		}
		count, err := m.directUnread.UnreadCountPublication(row.Target, m.directPublication)
		if err != nil {
			return m
		}
		if count > 0 {
			next[fs.DirectThreadKey(row.Target)] = count
		}
	}
	m.agentRail.unreadByThread = next
	return m
}

func fitAgentRailLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	line = ansi.Truncate(line, width, "")
	if padding := width - lipgloss.Width(line); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return line
}

func (m MailModel) renderAgentRailRow(row agentSelectorRow, index, width int) string {
	current := row.Main && m.agentSelector.selectedThreadKey == "" ||
		!row.Main && fs.DirectThreadKey(row.Target) == m.agentSelector.selectedThreadKey
	prefix := selectorRowPrefix(current, index == m.agentSelector.cursor)
	badge := ""
	if !row.Main {
		if count := m.agentRail.unreadByThread[fs.DirectThreadKey(row.Target)]; count > 0 {
			badge = fmt.Sprintf(" %d", count)
		}
	}
	labelWidth := width - lipgloss.Width(prefix) - lipgloss.Width(badge)
	if labelWidth < 0 {
		labelWidth = 0
	}
	label := ansi.Truncate(row.Label, labelWidth, "")
	line := prefix + label
	if padding := width - lipgloss.Width(line) - lipgloss.Width(badge); padding > 0 {
		line += strings.Repeat(" ", padding)
	}
	return fitAgentRailLine(line+badge, width)
}

// renderAgentRail is a pure projection of the current V1 selector and the
// ephemeral badge cache. It always returns exactly height lines of width cells.
func (m MailModel) renderAgentRail(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := make([]string, height)
	for index := range lines {
		lines[index] = strings.Repeat(" ", width)
	}
	lines[0] = fitAgentRailLine("  "+i18n.T("agent_rail.title"), width)
	if height > 1 {
		lines[1] = fitAgentRailLine(strings.Repeat("─", width), width)
	}

	offset := clampAgentRailOffset(m.agentRail.scrollOffset, len(m.agentSelector.rows), height)
	capacity := agentRailVisibleRows(height)
	for visibleIndex := 0; visibleIndex < capacity; visibleIndex++ {
		rowIndex := offset + visibleIndex
		lineIndex := agentRailRowsStart + visibleIndex
		if rowIndex >= len(m.agentSelector.rows) || lineIndex >= height {
			break
		}
		lines[lineIndex] = m.renderAgentRailRow(m.agentSelector.rows[rowIndex], rowIndex, width)
	}
	return strings.Join(lines, "\n")
}
