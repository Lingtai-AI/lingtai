package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// readTaskCardBody reads the current agent's declarative Task Card — the
// kernel's intrinsic task_card capability, which owns exactly two files under
// <agent>/taskcard/: status (exact "active" or "inactive") and taskcard.md
// (the rendered body). This mirrors the read-only consumer contract other
// channels use (lingtai-kernel's task_card CONTRACT.md, "consumer example"
// in mcp_servers/telegram/manager.py): status must match "active"
// byte-for-byte and the body must be non-blank, but the returned body itself
// is never trimmed or otherwise modified. ok is false for any unavailable
// state (missing/unreadable files, a non-active status, or a blank body);
// status carries the raw status bytes ("" when unreadable) so callers may
// distinguish an explicit "inactive" card cheaply.
func readTaskCardBody(agentDir string) (body, status string, ok bool) {
	if agentDir == "" {
		return "", "", false
	}
	dir := filepath.Join(agentDir, "taskcard")
	statusBytes, err := os.ReadFile(filepath.Join(dir, "status"))
	if err != nil {
		return "", "", false
	}
	status = string(statusBytes)
	if status != "active" {
		return "", status, false
	}
	bodyBytes, err := os.ReadFile(filepath.Join(dir, "taskcard.md"))
	if err != nil {
		return "", status, false
	}
	body = string(bodyBytes)
	if strings.TrimSpace(body) == "" {
		return "", status, false
	}
	return body, status, true
}

// taskCardEntries builds the /taskcard markdown-viewer content: the card body
// verbatim when active and non-blank, otherwise one small localized fallback
// message. This is the entire view — no renderer, no polling, no second
// state store.
func taskCardEntries(agentDir string) []MarkdownEntry {
	title := i18n.T("taskcard.title")
	body, status, ok := readTaskCardBody(agentDir)
	if !ok {
		reason := i18n.T("taskcard.unavailable")
		if status == "inactive" {
			reason = i18n.T("taskcard.inactive")
		}
		body = fmt.Sprintf("# %s\n\n%s", title, reason)
	}
	return []MarkdownEntry{{Label: title, Group: title, Content: body}}
}

// TaskCardModel is the /taskcard view: a thin MarkdownViewerModel wrapper
// showing the current agent's declarative Task Card. There is no in-view
// refresh — re-running /taskcard reconstructs this model from disk, the same
// way every other point-in-time markdown view in this package refreshes.
type TaskCardModel struct {
	agentDir string
	inner    MarkdownViewerModel
}

func NewTaskCardModel(agentDir string) TaskCardModel {
	return TaskCardModel{
		agentDir: agentDir,
		inner:    NewMarkdownViewer(taskCardEntries(agentDir), i18n.T("taskcard.title")),
	}
}

func (m TaskCardModel) Init() tea.Cmd { return m.inner.Init() }

func (m TaskCardModel) Update(msg tea.Msg) (TaskCardModel, tea.Cmd) {
	var cmd tea.Cmd
	m.inner, cmd = m.inner.Update(msg)
	return m, cmd
}

func (m TaskCardModel) View() string { return m.inner.View() }
