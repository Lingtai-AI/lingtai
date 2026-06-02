package tui

import (
	"fmt"
	"image/color"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/charmbracelet/x/ansi"
)

type statusLineSegment struct {
	label string
	value string
	color color.Color
}

func (a App) statusLineMode() string {
	if a.tuiConfig.StatusLine == "" {
		return config.StatusLineDefault
	}
	return a.tuiConfig.StatusLine
}

func (a App) statusLineEnabled() bool {
	return a.statusLineMode() != config.StatusLineOff
}

func (a App) childWindowSize(msg tea.WindowSizeMsg) tea.WindowSizeMsg {
	if a.statusLineEnabled() && msg.Height > 1 {
		msg.Height--
	}
	return msg
}

func (a App) childHeight() int {
	if a.statusLineEnabled() && a.height > 1 {
		return a.height - 1
	}
	return a.height
}

func (a App) renderStatusLine() string {
	if !a.statusLineEnabled() || a.width <= 0 {
		return ""
	}

	mode := a.statusLineMode()
	segments := []statusLineSegment{
		{label: "LingTai", value: "", color: ColorAccent},
		{label: i18n.T("statusline.view"), value: a.statusLineViewName(), color: ColorText},
		{label: i18n.T("statusline.project"), value: a.statusLineProjectName(), color: ColorAgent},
	}
	segments = append(segments, a.statusLineAgentSegments()...)

	if mode == config.StatusLineDefault || mode == config.StatusLineFull {
		if segment := a.statusLineContextSegment(); segment.value != "" {
			segments = append(segments, segment)
		}
		if segment := a.statusLineStaminaSegment(); segment.value != "" {
			segments = append(segments, segment)
		}
		if segment := a.statusLineActivitySegment(); segment.value != "" {
			segments = append(segments, segment)
		}
	}

	if mode == config.StatusLineFull {
		segments = append(segments, a.statusLineTokenSegments()...)
	}

	return renderStatusLineSegments(segments, a.width)
}

func (a App) statusLineViewName() string {
	switch a.currentView {
	case appViewFirstRun:
		return "/welcome"
	case appViewMail:
		return "/mail"
	case appViewSetup:
		return "/setup"
	case appViewSettings:
		return "/settings"
	case appViewProps:
		return "/kanban"
	case appViewAddon:
		return "/mcp"
	case appViewDoctor:
		return "/doctor"
	case appViewNirvana:
		return "/nirvana"
	case appViewLibrary:
		return "/skills"
	case appViewProjects:
		return "/projects"
	case appViewAgora:
		return "/agora"
	case appViewLogin:
		return "/login"
	case appViewCodex:
		return "/knowledge"
	case appViewMailbox:
		return "/mailbox"
	case appViewSystem:
		return "/system"
	case appViewPresets:
		return "/presets"
	default:
		return "/mail"
	}
}

func (a App) statusLineProjectName() string {
	base := filepath.Base(filepath.Clean(a.projectDir))
	if base == ".lingtai" {
		base = filepath.Base(filepath.Dir(filepath.Clean(a.projectDir)))
	}
	if base == "." || base == string(filepath.Separator) || base == "" {
		return i18n.T("statusline.none")
	}
	return base
}

func (a App) statusLineAgentSegments() []statusLineSegment {
	name := strings.TrimSpace(a.orchName)
	state := ""
	if a.orchDir != "" {
		if node, err := fs.ReadAgent(a.orchDir); err == nil {
			if node.Nickname != "" {
				name = node.Nickname
			} else if node.AgentName != "" {
				name = node.AgentName
			} else if node.Address != "" {
				name = node.Address
			}
			state = strings.ToUpper(strings.TrimSpace(node.State))
		}
		if state != "" && !fs.IsAlive(a.orchDir, fs.AgentAliveThresholdSec) {
			state = "SUSPENDED"
		}
	}
	if name == "" && a.orchDir != "" {
		name = filepath.Base(a.orchDir)
	}
	if name == "" {
		name = i18n.T("statusline.none")
	}
	if state == "" {
		return []statusLineSegment{{label: i18n.T("statusline.agent"), value: name, color: ColorAgent}}
	}
	return []statusLineSegment{
		{label: i18n.T("statusline.agent"), value: name, color: ColorAgent},
		{label: "", value: state, color: StateColor(state)},
	}
}

func (a App) statusLineContextSegment() statusLineSegment {
	if a.orchDir == "" {
		return statusLineSegment{}
	}
	ctx := fs.ReadStatus(a.orchDir).Tokens.Context
	if ctx.TotalTokens <= 0 && ctx.WindowSize <= 0 && ctx.UsagePct <= 0 {
		return statusLineSegment{}
	}

	value := ""
	if ctx.UsagePct > 0 {
		value = fmt.Sprintf("%.0f%%", ctx.UsagePct)
	} else if ctx.WindowSize > 0 {
		value = fmt.Sprintf("%s/%s", compactTokenCount(int64(ctx.TotalTokens)), compactTokenCount(int64(ctx.WindowSize)))
	} else {
		value = compactTokenCount(int64(ctx.TotalTokens))
	}
	return statusLineSegment{label: i18n.T("statusline.context"), value: value, color: contextUsageColor(ctx.UsagePct)}
}

func (a App) statusLineStaminaSegment() statusLineSegment {
	if a.orchDir == "" {
		return statusLineSegment{}
	}
	staminaLeft := fs.ReadStatus(a.orchDir).Runtime.StaminaLeft
	value := compactDuration(staminaLeft)
	if value == "" {
		return statusLineSegment{}
	}
	color := ColorActive
	if staminaLeft <= 10*60 {
		color = ColorStuck
	} else if staminaLeft <= 30*60 {
		color = ColorIdle
	}
	return statusLineSegment{label: i18n.T("statusline.stamina"), value: value, color: color}
}

func (a App) statusLineActivitySegment() statusLineSegment {
	activity, err := fs.ComputeNetworkActivity(a.projectDir)
	if err != nil || activity.Status == "" {
		return statusLineSegment{}
	}
	value := networkActivityStatusLabel(activity.Status)
	if activity.ActiveAgents > 0 {
		value += fmt.Sprintf(":%d", activity.ActiveAgents)
	}
	if activity.RunningDaemons > 0 {
		value += fmt.Sprintf("+%d", activity.RunningDaemons)
	}
	return statusLineSegment{label: networkActivityShortLabel(), value: value, color: NetworkActivityColor(activity.Status)}
}

func (a App) statusLineTokenSegments() []statusLineSegment {
	agents, err := fs.DiscoverAgents(a.projectDir)
	if err != nil {
		return nil
	}
	dirs := make([]string, 0, len(agents))
	for _, agent := range agents {
		if agent.IsHuman {
			continue
		}
		dirs = append(dirs, agent.WorkingDir)
	}
	totals := fs.AggregateTokens(dirs)
	totalTokens := totals.Input + totals.Output + totals.Thinking
	if totalTokens <= 0 && totals.APICalls <= 0 {
		return nil
	}

	segments := []statusLineSegment{
		{label: i18n.T("statusline.tokens"), value: compactTokenCount(totalTokens), color: ColorThinking},
	}
	if totals.APICalls > 0 {
		segments = append(segments, statusLineSegment{
			label: i18n.T("statusline.calls"),
			value: compactTokenCount(totals.APICalls),
			color: ColorText,
		})
	}
	return segments
}

func renderStatusLineSegments(segments []statusLineSegment, width int) string {
	if width <= 0 {
		return ""
	}
	parts := make([]string, 0, len(segments))
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	brandStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	separator := StyleFaint.Render(" " + RuneBullet + " ")

	for _, segment := range segments {
		if segment.label == "" && segment.value == "" {
			continue
		}
		if segment.value == "" {
			parts = append(parts, brandStyle.Render(segment.label))
			continue
		}
		valueStyle := lipgloss.NewStyle().Foreground(segment.color)
		if segment.label == "" {
			parts = append(parts, valueStyle.Render(segment.value))
		} else {
			parts = append(parts, labelStyle.Render(segment.label+" ")+valueStyle.Render(segment.value))
		}
	}

	line := " " + strings.Join(parts, separator)
	if lipgloss.Width(line) > width {
		line = ansi.Truncate(line, width, "")
	}
	if pad := width - lipgloss.Width(line); pad > 0 {
		line += strings.Repeat(" ", pad)
	}
	return lipgloss.NewStyle().
		Background(ColorSurface).
		Foreground(ColorTextDim).
		Render(line)
}

func appendStatusLineContent(content, bar string, childHeight int) string {
	content = strings.TrimRight(content, "\n")
	if content == "" {
		if childHeight <= 0 {
			return bar
		}
		return strings.Repeat("\n", childHeight) + bar
	}

	lineCount := lipgloss.Height(content)
	newlines := 1
	if lineCount < childHeight {
		newlines = childHeight - lineCount + 1
	}
	return content + strings.Repeat("\n", newlines) + bar
}

func contextUsageColor(pct float64) color.Color {
	switch {
	case pct >= 80:
		return ColorStuck
	case pct >= 60:
		return ColorIdle
	default:
		return ColorActive
	}
}

func compactDuration(seconds float64) string {
	if seconds <= 0 {
		return ""
	}
	duration := time.Duration(seconds * float64(time.Second))
	hours := int(duration.Hours())
	minutes := int(duration.Minutes()) % 60
	secs := int(duration.Seconds()) % 60
	if hours > 0 {
		return fmt.Sprintf("%dh%dm", hours, minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	return fmt.Sprintf("%ds", secs)
}

func compactTokenCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1_000_000_000)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1_000_000)
	case n >= 10_000:
		return fmt.Sprintf("%dk", n/1_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1_000)
	default:
		return fmt.Sprintf("%d", n)
	}
}
