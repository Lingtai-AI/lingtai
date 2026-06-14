package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// buildNotificationEntries renders the current notification block visible to an
// agent. The kernel consumes the same <agent>/.notification/*.json files before
// synthesizing system(action="notification") tool-result payloads for the model.
func buildNotificationEntries(agentDir string) []MarkdownEntry {
	entries := []MarkdownEntry{{
		Label:   "block",
		Content: buildNotificationOverview(agentDir),
	}}
	if agentDir == "" {
		return entries
	}

	notifDir := filepath.Join(agentDir, ".notification")
	dirents, err := os.ReadDir(notifDir)
	if err != nil {
		return entries
	}
	sort.Slice(dirents, func(i, j int) bool { return dirents[i].Name() < dirents[j].Name() })

	for _, de := range dirents {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		path := filepath.Join(notifDir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		label := strings.TrimSuffix(de.Name(), ".json")
		entries = append(entries, MarkdownEntry{
			Label:       label,
			Description: path,
			Content:     formatNotificationFileMarkdown(label, path, data),
		})
	}
	return entries
}

func buildNotificationOverview(agentDir string) string {
	var b strings.Builder
	title := i18n.T("palette.notification")
	if title == "" {
		title = "Notification block"
	}
	fmt.Fprintf(&b, "# %s\n\n", title)
	if agentDir == "" {
		b.WriteString("No current agent is selected.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Agent: `%s`\n\n", filepath.Base(agentDir))
	fmt.Fprintf(&b, "Directory: `%s`\n\n", filepath.Join(agentDir, ".notification"))

	files := readNotificationFiles(agentDir)
	if len(files) == 0 {
		b.WriteString("No pending notification files are currently visible to this agent.\n\n")
		b.WriteString("Notifications that were already consumed by the kernel may still appear in chat history as a structured `system(action=\"notification\")` tool-call/tool-result pair, but they are no longer present in `.notification/`.\n")
		return b.String()
	}

	b.WriteString("This is the raw notification block the agent currently sees before producer-specific read/dismiss handling.\n\n")
	b.WriteString("```json\n")
	pretty, err := json.MarshalIndent(files, "", "  ")
	if err != nil {
		b.WriteString("[]\n")
	} else {
		b.Write(pretty)
		b.WriteString("\n")
	}
	b.WriteString("```\n")
	return b.String()
}

type notificationFileBlock struct {
	Source string          `json:"source"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
}

func readNotificationFiles(agentDir string) []notificationFileBlock {
	if agentDir == "" {
		return nil
	}
	notifDir := filepath.Join(agentDir, ".notification")
	dirents, err := os.ReadDir(notifDir)
	if err != nil {
		return nil
	}
	sort.Slice(dirents, func(i, j int) bool { return dirents[i].Name() < dirents[j].Name() })

	var files []notificationFileBlock
	for _, de := range dirents {
		if de.IsDir() || filepath.Ext(de.Name()) != ".json" {
			continue
		}
		path := filepath.Join(notifDir, de.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var raw json.RawMessage
		if json.Valid(data) {
			raw = append(raw, data...)
		} else {
			raw, _ = json.Marshal(string(data))
		}
		files = append(files, notificationFileBlock{
			Source: strings.TrimSuffix(de.Name(), ".json"),
			Path:   path,
			Body:   raw,
		})
	}
	return files
}

func formatNotificationFileMarkdown(label, path string, data []byte) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s\n\n", label)
	fmt.Fprintf(&b, "Path: `%s`\n\n", path)
	b.WriteString("```json\n")
	var v any
	if err := json.Unmarshal(data, &v); err == nil {
		pretty, _ := json.MarshalIndent(v, "", "  ")
		b.Write(pretty)
		b.WriteString("\n")
	} else {
		b.WriteString(strings.TrimRight(string(data), "\n"))
		b.WriteString("\n")
	}
	b.WriteString("```\n")
	return b.String()
}

func notificationTitleFor(agentDir string) string {
	base := i18n.T("palette.notification")
	if agentDir == "" {
		return base
	}
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

// NotificationModel is the /notification view: a read-only MarkdownViewer over
// the current agent's .notification files. Unlike /system, this intentionally
// stays scoped to the currently selected agent because the command answers the
// question "what notification block does this agent see right now?".
type NotificationModel struct {
	agentDir string
	inner    MarkdownViewerModel
}

func NewNotificationModel(agentDir string) NotificationModel {
	return NotificationModel{
		agentDir: agentDir,
		inner:    newNotificationViewer(agentDir),
	}
}

func newNotificationViewer(agentDir string) MarkdownViewerModel {
	viewer := NewMarkdownViewer(buildNotificationEntries(agentDir), notificationTitleFor(agentDir))
	viewer.FooterHint = "r reload"
	return viewer
}

func (m NotificationModel) Init() tea.Cmd { return m.inner.Init() }

func (m NotificationModel) Update(msg tea.Msg) (NotificationModel, tea.Cmd) {
	if key, ok := msg.(tea.KeyPressMsg); ok && key.String() == "r" {
		width, height := m.inner.width, m.inner.height
		m.inner = newNotificationViewer(m.agentDir)
		if width > 0 && height > 0 {
			var cmd tea.Cmd
			m.inner, cmd = m.inner.Update(tea.WindowSizeMsg{Width: width, Height: height})
			return m, cmd
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m NotificationModel) View() string { return m.inner.View() }
