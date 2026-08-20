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

// taskCardServiceTierSummary reads only the kernel's public Codex/Responses
// service-tier identity from .agent.json. Missing, malformed, or oversized
// values are intentionally omitted rather than inferred from presets.
func taskCardServiceTierSummary(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	data, err := os.ReadFile(filepath.Join(agentDir, ".agent.json"))
	if err != nil {
		return ""
	}
	var manifest struct {
		LLM struct {
			ServiceTier string `json:"service_tier"`
		} `json:"llm"`
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return ""
	}
	tier := strings.TrimSpace(manifest.LLM.ServiceTier)
	if tier == "" || len(tier) > 48 {
		return ""
	}
	return i18n.T("preset_editor.field_service_tier") + ": " + tier
}

// taskCardEntries builds the /taskcard markdown-viewer content: the card body
// verbatim when active and non-blank, otherwise one small localized fallback
// message. An active card may receive point-in-time display-only service-tier
// and daemon-model annotations; this never changes the card files or starts polling.
func taskCardEntries(agentDir string) []MarkdownEntry {
	title := i18n.T("taskcard.title")
	body, status, ok := readTaskCardBody(agentDir)
	if !ok {
		reason := i18n.T("taskcard.unavailable")
		if status == "inactive" {
			reason = i18n.T("taskcard.inactive")
		}
		body = fmt.Sprintf("# %s\n\n%s", title, reason)
	} else {
		var annotations []string
		if tier := taskCardServiceTierSummary(agentDir); tier != "" {
			annotations = append(annotations, tier)
		}
		if models := taskCardDaemonModelSummary(agentDir); models != "" {
			annotations = append(annotations, models)
		}
		if len(annotations) > 0 {
			body += "\n\n" + strings.Join(annotations, "\n")
		}
	}
	return []MarkdownEntry{{Label: title, Group: title, Content: body}}
}

// taskCardDaemonModelSummary returns a compact task-card annotation for current
// backend="lingtai" daemon runs. RecentLingtaiDaemonModels already applies the
// same 10-minute active/recent-terminal window as the Telegram-aligned home row.
// A single eligible run shows its raw daemon.json model; several runs aggregate
// deterministically as model × count without deriving a model for other backends.
func taskCardDaemonModelSummary(agentDir string) string {
	models := fs.RecentLingtaiDaemonModels(agentDir)
	if len(models) == 0 {
		return ""
	}
	if len(models) == 1 {
		return i18n.T("taskcard.daemon_model") + ": " + models[0]
	}

	counts := make(map[string]int, len(models))
	for _, model := range models {
		counts[model]++
	}
	keys := make([]string, 0, len(counts))
	for model := range counts {
		keys = append(keys, model)
	}
	sort.Strings(keys)
	stats := make([]string, 0, len(keys))
	for _, model := range keys {
		stats = append(stats, fmt.Sprintf("%s × %d", model, counts[model]))
	}
	return i18n.T("taskcard.daemon_models") + ": " + strings.Join(stats, " · ")
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
