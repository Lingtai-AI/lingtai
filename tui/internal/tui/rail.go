package tui

import (
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/inventory"
)

// railRow is display-only rail state. Its targets are immutable value copies of
// the accepted activation and direct-mail coordinates; the row intentionally
// owns no MailModel, ProjectMailStore, MailCache, scanner, tick, or thread
// projection.
type railRow struct {
	label        string
	originalMain bool
	target       asyncTarget
	directTarget fs.DirectTarget
}

// AgentRailState is the root-owned display projection for the home Agent rail.
// Inventory-backed ordinary rows are installed into this projection separately;
// rendering never scans the filesystem or process table. The accepted inventory
// owner/readiness pair prevents temporary or stale rows from becoming an
// authoritative unread target set.
type AgentRailState struct {
	rows                   []railRow
	cursor                 int
	acceptedInventoryOwner asyncOwner
	acceptedInventoryReady bool
}

type mailPaneFocus uint8

const agentRailHeaderRows = 2 // localized title, then one blank line

const (
	mailFocusChat mailPaneFocus = iota
	mailFocusRail
)

func (a App) updateMailChildWindowSize(size tea.WindowSizeMsg) (App, tea.Cmd) {
	var focusCmd tea.Cmd
	if a.currentView == appViewMail && !a.layoutBudget().RailVisible && a.mailFocus == mailFocusRail {
		focusCmd = a.focusMailChat()
	}
	updated, sizeCmd := a.updateChildWindowSize(size)
	return updated, tea.Batch(focusCmd, sizeCmd)
}

func (a *App) focusMailChat() tea.Cmd {
	if a == nil {
		return nil
	}
	a.mailFocus = mailFocusChat
	return a.mail.input.Focus()
}

func (a *App) focusMailRail() {
	if a == nil {
		return
	}
	a.mailFocus = mailFocusRail
	a.mail.input.Blur()
}

func (a App) handleMailFocusKey(msg tea.KeyPressMsg) (App, tea.Cmd, bool) {
	if a.currentView != appViewMail {
		return a, nil, false
	}

	key := msg.String()
	if a.mail.copyMode {
		return a, nil, false
	}
	if key == "tab" || key == "shift+tab" {
		if !a.layoutBudget().RailVisible {
			cmd := a.focusMailChat()
			return a, cmd, true
		}
		if a.mailFocus == mailFocusRail {
			cmd := a.focusMailChat()
			return a, cmd, true
		}
		a.focusMailRail()
		return a, nil, true
	}

	if key == "esc" && a.mailFocus == mailFocusRail && !a.mail.copyMode {
		cmd := a.focusMailChat()
		return a, cmd, true
	}

	if a.mailFocus == mailFocusRail {
		switch key {
		case "up", "k":
			a.agentRail.moveCursor(-1)
			return a, nil, true
		case "down", "j":
			a.agentRail.moveCursor(1)
			return a, nil, true
		case "enter":
			row, ok := a.agentRail.selectedRow()
			if !ok || row.originalMain || row.target.policy != asyncTargetHomeAgentRail {
				return a, nil, true
			}
			updated, cmd := a.activateOrdinaryRailRow(row)
			return updated, cmd, true
		}
		if key != copyModeToggleKey {
			return a, nil, true
		}
	}
	return a, nil, false
}

func (a App) handleMailMouseClick(msg tea.MouseClickMsg) (App, tea.Cmd, bool) {
	if a.currentView != appViewMail {
		return a, nil, false
	}

	budget := a.layoutBudget()
	if msg.Y < budget.TopChromeRows || msg.Y >= budget.TopChromeRows+budget.ChildHeight {
		return a, nil, true
	}
	if localX, ok := budget.ContentLocalX(msg.X); ok {
		focusCmd := a.focusMailChat()
		mouse := msg.Mouse()
		mouse.X = localX
		mouse.Y -= budget.TopChromeRows
		updated, mailCmd := a.mail.Update(tea.MouseClickMsg(mouse))
		a.mail = updated
		return a, tea.Batch(focusCmd, mailCmd), true
	}
	if _, ok := budget.RailLocalX(msg.X); ok {
		a.focusMailRail()
		row, ok := a.agentRail.selectRowAtLocalY(msg.Y - budget.TopChromeRows)
		if !ok || row.originalMain || row.target.policy != asyncTargetHomeAgentRail {
			return a, nil, true
		}
		updated, cmd := a.activateOrdinaryRailRow(row)
		return updated, cmd, true
	}
	return a, nil, true
}

func (s *AgentRailState) clampCursor() {
	if s == nil || len(s.rows) == 0 {
		if s != nil {
			s.cursor = 0
		}
		return
	}
	if s.cursor < 0 {
		s.cursor = 0
	} else if s.cursor >= len(s.rows) {
		s.cursor = len(s.rows) - 1
	}
}

func (s *AgentRailState) moveCursor(delta int) bool {
	if s == nil || len(s.rows) == 0 || delta == 0 {
		return false
	}
	s.clampCursor()
	next := s.cursor + delta
	if next < 0 || next >= len(s.rows) {
		return false
	}
	s.cursor = next
	return true
}

func (s AgentRailState) selectedRow() (railRow, bool) {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return railRow{}, false
	}
	return s.rows[s.cursor], true
}

func (s *AgentRailState) selectRowAtLocalY(localY int) (railRow, bool) {
	if s == nil {
		return railRow{}, false
	}
	index := localY - agentRailHeaderRows
	if index < 0 || index >= len(s.rows) {
		return railRow{}, false
	}
	s.cursor = index
	return s.rows[index], true
}

func sameRailRowIdentity(a, b railRow) bool {
	if a.originalMain || b.originalMain {
		return a.originalMain && b.originalMain
	}
	return a.target.policy == asyncTargetHomeAgentRail && b.target.policy == asyncTargetHomeAgentRail &&
		a.target.directory != "" && a.target.directory == b.target.directory &&
		a.target.addressFingerprint != "" && a.target.addressFingerprint == b.target.addressFingerprint
}

func railInventoryLabel(record inventory.Record, target asyncTarget) string {
	label := firstNonEmpty(record.Nickname, record.AgentName, record.Address, record.Agent, filepath.Base(target.directory))
	return strings.TrimSpace(label)
}

func (s *AgentRailState) installInventory(owner asyncOwner, snapshot inventory.Snapshot) {
	if s == nil {
		return
	}

	oldCursor := s.cursor
	selected, hadSelection := s.selectedRow()
	main := railRow{label: "Main", originalMain: true}
	for _, row := range s.rows {
		if row.originalMain {
			main = row
			main.originalMain = true
			if strings.TrimSpace(main.label) == "" {
				main.label = "Main"
			}
			break
		}
	}

	rows := make([]railRow, 1, len(snapshot.Records)+1)
	rows[0] = main
	for _, record := range snapshot.Records {
		target := asyncTarget{
			directory:          inventory.NormalizePath(record.AgentDir),
			addressFingerprint: fs.AddressFingerprint(record.Address),
			policy:             asyncTargetHomeAgentRail,
			pid:                record.PID,
		}
		if !ordinaryRailRecordEligible(owner, target, record) {
			continue
		}
		rows = append(rows, railRow{
			label:  railInventoryLabel(record, target),
			target: target,
			directTarget: fs.DirectTarget{
				Directory: target.directory,
				Address:   record.Address,
			},
		})
	}

	s.rows = rows
	s.acceptedInventoryOwner = owner
	s.acceptedInventoryReady = true
	s.cursor = oldCursor
	if hadSelection {
		for i, row := range s.rows {
			if sameRailRowIdentity(selected, row) {
				s.cursor = i
				return
			}
		}
	}
	s.clampCursor()
}

func (s AgentRailState) acceptedDirectTargets(owner asyncOwner) ([]fs.DirectTarget, bool) {
	if !s.acceptedInventoryReady || s.acceptedInventoryOwner != owner || len(s.rows) == 0 {
		return nil, false
	}
	targets := make([]fs.DirectTarget, 0, len(s.rows))
	for _, row := range s.rows {
		if strings.TrimSpace(row.directTarget.Directory) == "" || strings.TrimSpace(row.directTarget.Address) == "" {
			return nil, false
		}
		targets = append(targets, row.directTarget)
	}
	return targets, true
}

func (s *AgentRailState) clearInventoryAcceptance() {
	if s == nil {
		return
	}
	s.acceptedInventoryOwner = asyncOwner{}
	s.acceptedInventoryReady = false
}

func (s *AgentRailState) installMain(label string, directTarget fs.DirectTarget) {
	if s == nil {
		return
	}
	label = strings.TrimSpace(label)
	if label == "" {
		label = "Main"
	}
	for i := range s.rows {
		if s.rows[i].originalMain {
			s.rows[i].label = label
			s.rows[i].directTarget = directTarget
			s.clampCursor()
			return
		}
	}
	if len(s.rows) > 0 && s.cursor >= 0 && s.cursor < len(s.rows) {
		s.cursor++
	}
	s.rows = append([]railRow{{label: label, originalMain: true, directTarget: directTarget}}, s.rows...)
	s.clampCursor()
}

func (s AgentRailState) rowsForView(mainLabel string) []railRow {
	rows := append([]railRow(nil), s.rows...)
	for i := range rows {
		if rows[i].originalMain {
			if current := strings.TrimSpace(mainLabel); current != "" {
				rows[i].label = current
			}
			return rows
		}
	}
	fallback := AgentRailState{}
	fallback.installMain(mainLabel, fs.DirectTarget{})
	return append(fallback.rows, rows...)
}

func fixedRailLine(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = ansi.Truncate(text, width, "…")
	return lipgloss.NewStyle().Width(width).Render(text)
}

// View renders exactly the rectangle supplied by the root LayoutBudget. It does
// not choose a width or height of its own.
func (s AgentRailState) View(width, height int, mainLabel string) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	logical := []string{
		StyleTitle.Render("  " + i18n.T("props.network_agents")),
		"",
	}
	rows := s.rowsForView(mainLabel)
	cursor := s.cursor
	if cursor < 0 {
		cursor = 0
	} else if cursor >= len(rows) {
		cursor = len(rows) - 1
	}
	for i, row := range rows {
		marker := StyleAccent.Render("› ")
		if i != cursor {
			marker = "  "
		}
		logical = append(logical, marker+StyleTitle.Render(row.label))
	}

	lines := make([]string, height)
	for i := range lines {
		if i < len(logical) {
			lines[i] = fixedRailLine(logical[i], width)
		} else {
			lines[i] = fixedRailLine("", width)
		}
	}
	return strings.Join(lines, "\n")
}

func fixedMailBlock(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	return lipgloss.NewStyle().Width(width).Height(height).Render(content)
}

// composeMailWithRail consumes the same root budget used for child sizing. The
// horizontal join happens before existing root-owned vertical chrome.
func (a App) composeMailWithRail(mailContent string) string {
	budget := a.layoutBudget()
	if !budget.RailVisible {
		return mailContent
	}

	rail := a.agentRail.View(budget.RailWidth, budget.ChildHeight, a.mail.orchDisplayName())
	chat := fixedMailBlock(mailContent, budget.ContentWidth, budget.ChildHeight)
	return lipgloss.JoinHorizontal(lipgloss.Top, rail, chat)
}
