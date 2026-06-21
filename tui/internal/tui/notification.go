package tui

import (
	"encoding/json"
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
// Each snapshot carries the actual canonical `_meta` envelope the agent saw —
// the four blocks tool_meta / agent_meta / guidance / notifications +
// notification_guidance — not just a compact summary. Older rows persisted in
// the pre-envelope payload/meta shape are still rendered via the legacy
// fallback in sqlitelog. Left/right keys step among the in-memory list;
// r/ctrl+r reloads. Esc returns to the mail view.
type NotificationModel struct {
	agentDir string
	width    int
	height   int

	snapshots []sqlitelog.NotificationBlockSnapshot
	// cursor into snapshots; -1 means no snapshots available
	cursor int
	err    string

	viewer MarkdownViewerModel
}

func NewNotificationModel(agentDir string) NotificationModel {
	m := NotificationModel{agentDir: agentDir, cursor: -1}
	m.load()
	return m
}

func (m *NotificationModel) load() {
	snaps, err := sqlitelog.QueryNotificationBlockSnapshots(m.agentDir, 10)
	if err != nil {
		m.err = err.Error()
		m.snapshots = nil
		m.cursor = -1
		m.viewer = MarkdownViewerModel{}
		return
	}
	m.err = ""
	m.snapshots = snaps
	if len(snaps) > 0 {
		m.cursor = 0
		m.rebuildViewer()
	} else {
		m.cursor = -1
		m.viewer = MarkdownViewerModel{}
	}
}

func (m *NotificationModel) rebuildViewer() {
	if m.cursor < 0 || m.cursor >= len(m.snapshots) {
		m.viewer = MarkdownViewerModel{}
		return
	}
	entries := notificationMarkdownEntries(m.snapshots[m.cursor], m.cursor, len(m.snapshots))
	viewer := NewMarkdownViewer(entries, notificationTitle(m.agentDir))
	viewer.FooterHint = "←/→ snapshots · r reload"
	if m.width > 0 || m.height > 0 {
		updated, _ := viewer.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		viewer = updated
	}
	viewer.syncLeft()
	viewer.syncRight()
	m.viewer = viewer
}

func (m NotificationModel) Init() tea.Cmd { return nil }

func (m NotificationModel) Update(msg tea.Msg) (NotificationModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.cursor >= 0 && m.cursor < len(m.snapshots) {
			var cmd tea.Cmd
			m.viewer, cmd = m.viewer.Update(msg)
			m.viewer.syncLeft()
			m.viewer.syncRight()
			return m, cmd
		}
		return m, nil
	case MarkdownViewerCloseMsg:
		return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
	case MarkdownViewerSelectMsg:
		return m, nil
	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc", "q", "backspace":
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
		case "left":
			// older = higher cursor index (index 0 = newest)
			if m.cursor >= 0 && m.cursor < len(m.snapshots)-1 {
				m.cursor++
				m.rebuildViewer()
			}
			return m, nil
		case "right":
			// newer = lower cursor index
			if m.cursor > 0 {
				m.cursor--
				m.rebuildViewer()
			}
			return m, nil
		case "r", "ctrl+r":
			m.load()
			return m, nil
		}
	}

	if m.cursor >= 0 && m.cursor < len(m.snapshots) {
		var cmd tea.Cmd
		m.viewer, cmd = m.viewer.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m NotificationModel) View() string {
	title := notificationTitle(m.agentDir)
	if m.err != "" {
		body := StyleSubtle.Render("Unable to read notification blocks from logs/log.sqlite:") + "\n\n" + m.err
		return renderNotificationPanel(title, body, "r reload  •  esc back", m.width, m.height)
	}
	if len(m.snapshots) == 0 || m.cursor < 0 {
		body := StyleFaint.Render("No persisted notification_block_injected events found yet.") + "\n\n" +
			"Run a tool that produces a notification block, then reopen /notification."
		return renderNotificationPanel(title, body, "r reload  •  esc back", m.width, m.height)
	}
	viewer := m.viewer
	if (m.width > 0 || m.height > 0) && (viewer.width != m.width || viewer.height != m.height || viewer.rightVP.Width() == 0) {
		updated, _ := viewer.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		viewer = updated
		viewer.syncLeft()
		viewer.syncRight()
	}
	return viewer.View()
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
// It shows the event identity, modern metadata sections, the full raw meta block,
// global _notification_guidance, and each channel's actual payload from the
// canonical block the agent saw.

func notificationMarkdownEntries(s sqlitelog.NotificationBlockSnapshot, cursor, total int) []MarkdownEntry {
	group := notificationSnapshotGroup(s, cursor, total)
	entries := []MarkdownEntry{
		{
			Label:       "all blocks",
			Description: "complete snapshot",
			Group:       group,
			Content:     notificationMarkdownAllBlocks(s, cursor, total),
		},
		{
			Label:       "overview",
			Description: "event identity and summary",
			Group:       group,
			Content:     notificationMarkdownOverview(s, cursor, total),
		},
	}
	addMap := func(label, desc string, value map[string]interface{}) {
		if len(value) == 0 {
			return
		}
		entries = append(entries, MarkdownEntry{Label: label, Description: desc, Group: group, Content: notificationMarkdownMapBlock(s, cursor, total, label, value)})
	}
	addMap("_meta.tool_meta", "per-result tool metadata", s.ToolMeta)
	addMap("_meta.agent_meta", "agent/current-state snapshot", s.AgentMeta)
	addMap("_meta.guidance", "kernel guidance", s.Guidance)
	if s.NotificationGuidance != "" {
		entries = append(entries, MarkdownEntry{Label: "_meta.notification_guidance", Description: "channel safety framing", Group: group, Content: notificationMarkdownStringBlock(s, cursor, total, "_meta.notification_guidance", s.NotificationGuidance)})
	}
	if len(s.Notifications) > 0 {
		entries = append(entries, MarkdownEntry{Label: "_meta.notifications", Description: "all channel payloads", Group: group, Content: notificationMarkdownNotificationsBlock(s, cursor, total, "_meta.notifications")})
	}
	channels := make([]string, 0, len(s.Notifications))
	for ch := range s.Notifications {
		channels = append(channels, ch)
	}
	sort.Strings(channels)
	for _, ch := range channels {
		entries = append(entries, MarkdownEntry{
			Label:       "_meta.notifications." + ch,
			Description: "channel payload",
			Group:       group,
			Content:     notificationMarkdownChannelBlock(s, cursor, total, ch, s.Notifications[ch]),
		})
	}
	return entries
}

func notificationSnapshotGroup(s sqlitelog.NotificationBlockSnapshot, cursor, total int) string {
	label := fmt.Sprintf("Snapshot %d/%d", cursor+1, total)
	if s.Ts > 0 {
		label += " · " + s.Time().Format("15:04 MST")
	} else if s.Meta != nil && s.Meta.CurrentTime != "" {
		if t := formatCurrentTimeShort(s.Meta.CurrentTime); t != "" {
			label += " · " + t
		}
	}
	return label
}

func notificationSnapshotMarkdownTitle(s sqlitelog.NotificationBlockSnapshot, cursor, total int, block string) string {
	return fmt.Sprintf("Snapshot %d/%d — %s", cursor+1, total, block)
}

func notificationMarkdownOverview(s sqlitelog.NotificationBlockSnapshot, cursor, total int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", notificationSnapshotMarkdownTitle(s, cursor, total, "overview"))
	fmt.Fprintf(&sb, "- **Position:** snapshot %d of %d\n", cursor+1, total)
	fmt.Fprintf(&sb, "- **Event ID:** `%d`\n", s.ID)
	if s.Ts > 0 {
		fmt.Fprintf(&sb, "- **Time:** `%s`\n", s.Time().Format(time.RFC3339))
	}
	if s.Source != "" {
		fmt.Fprintf(&sb, "- **Source:** `%s`\n", s.Source)
	}
	if s.Mode != "" {
		fmt.Fprintf(&sb, "- **Mode:** `%s`\n", s.Mode)
	}
	if s.CallID != "" {
		fmt.Fprintf(&sb, "- **Call ID:** `%s`\n", s.CallID)
	}
	if len(s.Sources) > 0 {
		fmt.Fprintf(&sb, "- **Channels:** `%s`\n", strings.Join(s.Sources, "`, `"))
	}
	if footer := formatBlockMetaFooter(s.Meta); footer != "" {
		fmt.Fprintf(&sb, "- **Meta summary:** %s\n", footer)
	}
	blocks := notificationBlockNames(s)
	if len(blocks) > 0 {
		sb.WriteString("\n## Available blocks\n\n")
		for _, block := range blocks {
			fmt.Fprintf(&sb, "- `%s`\n", block)
		}
	}
	return sb.String()
}

func notificationMarkdownAllBlocks(s sqlitelog.NotificationBlockSnapshot, cursor, total int) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", notificationSnapshotMarkdownTitle(s, cursor, total, "all blocks"))
	sb.WriteString(notificationMarkdownOverview(s, cursor, total))
	notificationWriteMarkdownMapSection(&sb, "_meta.tool_meta", s.ToolMeta)
	notificationWriteMarkdownMapSection(&sb, "_meta.agent_meta", s.AgentMeta)
	notificationWriteMarkdownMapSection(&sb, "_meta.guidance", s.Guidance)
	if s.NotificationGuidance != "" {
		notificationWriteMarkdownStringSection(&sb, "_meta.notification_guidance", s.NotificationGuidance)
	}
	notificationWriteMarkdownNotificationsSection(&sb, "_meta.notifications", s.Notifications)
	return sb.String()
}

func notificationMarkdownMapBlock(s sqlitelog.NotificationBlockSnapshot, cursor, total int, label string, value map[string]interface{}) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", notificationSnapshotMarkdownTitle(s, cursor, total, label))
	notificationWriteMarkdownMapSection(&sb, label, value)
	return sb.String()
}

func notificationMarkdownAnyBlock(s sqlitelog.NotificationBlockSnapshot, cursor, total int, label string, value interface{}) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", notificationSnapshotMarkdownTitle(s, cursor, total, label))
	notificationWriteMarkdownAnySection(&sb, label, value)
	return sb.String()
}

func notificationMarkdownNotificationsBlock(s sqlitelog.NotificationBlockSnapshot, cursor, total int, label string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", notificationSnapshotMarkdownTitle(s, cursor, total, label))
	notificationWriteMarkdownNotificationsSection(&sb, label, s.Notifications)
	return sb.String()
}

func notificationMarkdownChannelBlock(s sqlitelog.NotificationBlockSnapshot, cursor, total int, channel, payload string) string {
	var sb strings.Builder
	label := "notifications." + channel
	fmt.Fprintf(&sb, "# %s\n\n", notificationSnapshotMarkdownTitle(s, cursor, total, label))
	fmt.Fprintf(&sb, "## `%s`\n\n", label)
	fmt.Fprintf(&sb, "```json\n%s\n```\n\n", payload)
	return sb.String()
}

func notificationMarkdownStringBlock(s sqlitelog.NotificationBlockSnapshot, cursor, total int, label, value string) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "# %s\n\n", notificationSnapshotMarkdownTitle(s, cursor, total, label))
	notificationWriteMarkdownStringSection(&sb, label, value)
	return sb.String()
}

func notificationBlockNames(s sqlitelog.NotificationBlockSnapshot) []string {
	blocks := []string{"all blocks", "overview"}
	if len(s.ToolMeta) > 0 {
		blocks = append(blocks, "_meta.tool_meta")
	}
	if len(s.AgentMeta) > 0 {
		blocks = append(blocks, "_meta.agent_meta")
	}
	if len(s.Guidance) > 0 {
		blocks = append(blocks, "_meta.guidance")
	}
	if s.NotificationGuidance != "" {
		blocks = append(blocks, "_meta.notification_guidance")
	}
	if len(s.Notifications) > 0 {
		blocks = append(blocks, "_meta.notifications")
		channels := make([]string, 0, len(s.Notifications))
		for ch := range s.Notifications {
			channels = append(channels, ch)
		}
		sort.Strings(channels)
		for _, ch := range channels {
			blocks = append(blocks, "_meta.notifications."+ch)
		}
	}
	return blocks
}

func notificationWriteMarkdownMapSection(sb *strings.Builder, label string, value map[string]interface{}) {
	if len(value) == 0 {
		return
	}
	notificationWriteMarkdownAnySection(sb, label, value)
}

func notificationWriteMarkdownAnySection(sb *strings.Builder, label string, value interface{}) {
	fmt.Fprintf(sb, "## `%s`\n\n", label)
	fmt.Fprintf(sb, "```json\n%s\n```\n\n", notificationJSON(value))
}

func notificationWriteMarkdownNotificationsSection(sb *strings.Builder, label string, notifications map[string]string) {
	if len(notifications) == 0 {
		return
	}
	fmt.Fprintf(sb, "## `%s`\n\n", label)
	channels := make([]string, 0, len(notifications))
	for ch := range notifications {
		channels = append(channels, ch)
	}
	sort.Strings(channels)
	for _, ch := range channels {
		fmt.Fprintf(sb, "### `%s`\n\n", ch)
		fmt.Fprintf(sb, "```json\n%s\n```\n\n", notifications[ch])
	}
}

func notificationWriteMarkdownStringSection(sb *strings.Builder, label, value string) {
	fmt.Fprintf(sb, "## `%s`\n\n%s\n\n", label, value)
}

func notificationJSON(value interface{}) string {
	b, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", value)
	}
	return string(b)
}

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
	valueStyle := lipgloss.NewStyle().Foreground(ColorAgent)
	notifStyle := lipgloss.NewStyle().Foreground(ColorAgent).Italic(true)

	// ── _meta envelope blocks (tool_meta / agent_meta / guidance) ───────────
	writeNotificationMapBlock(&sb, "_meta.tool_meta", s.ToolMeta, []string{
		"id", "timestamp", "char_count", "elapsed_ms", "status", "spilled_char_count", "synthetic",
	}, wrapWidth, labelStyle, valueStyle)
	writeNotificationMapBlock(&sb, "_meta.agent_meta", s.AgentMeta, []string{
		"current_time", "context", "stamina_left_seconds", "stamina", "active_turn_tool_calls",
		"current_tool_result_chars", "elapsed_ms",
	}, wrapWidth, labelStyle, valueStyle)
	writeNotificationMapBlock(&sb, "_meta.guidance", s.Guidance, []string{
		"schema_version", "guidance_version", "priority", "render_mode", "sections", "meta_readme",
	}, wrapWidth, labelStyle, valueStyle)

	// ── Channel safety framing: _meta.notification_guidance ─────────────────
	if s.NotificationGuidance != "" {
		sb.WriteString(labelStyle.Render("  ✦ _meta.notification_guidance") + "\n")
		for _, line := range wrappedNotificationLines(s.NotificationGuidance, wrapWidth) {
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

func writeNotificationMapBlock(sb *strings.Builder, title string, data map[string]interface{}, preferred []string, wrapWidth int, labelStyle, valueStyle lipgloss.Style) {
	if len(data) == 0 {
		return
	}
	sb.WriteString(labelStyle.Render("  ◈ "+title) + "\n")
	for _, key := range orderedNotificationKeys(data, preferred) {
		lines := wrappedNotificationLines(formatNotificationValue(data[key]), wrapWidth-10)
		if len(lines) == 0 {
			continue
		}
		sb.WriteString(labelStyle.Render("    "+key+": ") + valueStyle.Render(lines[0]) + "\n")
		for _, line := range lines[1:] {
			sb.WriteString(valueStyle.Render("      "+line) + "\n")
		}
	}
	sb.WriteString("\n")
}

func orderedNotificationKeys(data map[string]interface{}, preferred []string) []string {
	seen := make(map[string]bool, len(data))
	keys := make([]string, 0, len(data))
	for _, key := range preferred {
		if _, ok := data[key]; ok {
			keys = append(keys, key)
			seen[key] = true
		}
	}
	extra := make([]string, 0, len(data))
	for key := range data {
		if !seen[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(extra)
	return append(keys, extra...)
}

func formatNotificationValue(v interface{}) string {
	switch x := v.(type) {
	case nil:
		return "<nil>"
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		if x == float64(int64(x)) {
			return fmt.Sprintf("%.0f", x)
		}
		return fmt.Sprintf("%g", x)
	case float32:
		return fmt.Sprintf("%g", x)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return fmt.Sprintf("%v", x)
	default:
		b, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			return string(b)
		}
		return fmt.Sprintf("%v", v)
	}
}

func wrappedNotificationLines(text string, wrapWidth int) []string {
	if wrapWidth <= 0 {
		wrapWidth = 76
	}
	if text == "" {
		return []string{""}
	}
	wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(text)
	return strings.Split(wrapped, "\n")
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
