package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// PropsModel is a full-screen view showing agent properties (left) and network dashboard (right).
type PropsModel struct {
	baseDir   string // .lingtai/ directory (for agent discovery)
	orchDir   string // admin agent's working dir (default selected)
	globalDir string // ~/.lingtai-tui/ (for resolving Config.Keys for preset health checks)
	width     int
	height    int

	// Left panel: selected agent
	selectedDir    string         // working dir of the agent shown on left (defaults to orchDir)
	selectedTokens fs.TokenTotals // cached token ledger for selected agent
	selectedStatus fs.AgentStatus // cached .status.json for selected agent
	agentDirs      []string       // all discovered agent dirs (for picker)
	agentNodes     []fs.AgentNode // discovered agents (for picker display)

	// Right panel: dashboard snapshot
	network    fs.Network
	tokens     fs.TokenTotals
	adminStart string // admin agent's started_at timestamp

	// AutoRefresh reflects whether the app-level 1s auto-refresh is enabled.
	// It only drives the footer hint (a "live" badge); the actual reloading is
	// driven by the app tick via AutoReloadCmd. Set by switchToView from
	// tuiConfig; defaults false so a bare NewPropsModel shows the manual-only
	// hint.
	AutoRefresh bool

	// Scrollable viewport for content
	viewport viewport.Model
	ready    bool // viewport initialized

	// Agent picker overlay
	pickerOpen bool
	pickerIdx  int

	// Detail view: full-screen breakdown of token usage, split recent
	// main/daemon calls, MCP servers, and daemon run counts. Toggled
	// with Ctrl+D. Esc closes detail and returns to the summary.
	detailOpen         bool
	detailByProvider   map[string]fs.TokenTotals
	detailRecent       []fs.LedgerEntry       // selected main agent recent calls (newest first)
	detailDaemonRecent []fs.DaemonLedgerEntry // all daemon calls, newest first, tagged by run
	detailContextStats fs.ContextStats
	detailDaemonCounts fs.DaemonCounts
	detailMCPNames     []string
}

// detailRecentCalls is the number of recent token-ledger calls shown in each
// Ctrl+D recent-call lane (main agent on the left, daemons on the right).
const detailRecentCalls = 100

func NewPropsModel(baseDir, orchDir, globalDir string) PropsModel {
	return PropsModel{
		baseDir:     baseDir,
		orchDir:     orchDir,
		globalDir:   globalDir,
		selectedDir: orchDir,
	}
}

type propsLoadMsg struct {
	network        fs.Network
	tokens         fs.TokenTotals
	selectedTokens fs.TokenTotals
	selectedStatus fs.AgentStatus
	adminStart     string
	agentDirs      []string
	agentNodes     []fs.AgentNode
}

func (m PropsModel) loadData() tea.Msg {
	net, _ := fs.BuildNetwork(m.baseDir)

	var dirs []string
	for _, n := range net.Nodes {
		if !n.IsHuman && n.WorkingDir != "" {
			dirs = append(dirs, n.WorkingDir)
		}
	}
	totals := fs.AggregateTokens(dirs)
	selectedTokens := fs.SumTokenLedger(filepath.Join(m.selectedDir, "logs", "token_ledger.jsonl"))
	selectedStatus := fs.ReadStatus(m.selectedDir)

	var adminStart string
	if raw, err := fs.ReadAgentRaw(m.orchDir); err == nil {
		if v, ok := raw["created_at"].(string); ok && v != "" {
			adminStart = v
		} else if v, ok := raw["started_at"].(string); ok && v != "" {
			adminStart = v
		}
	}

	var allDirs []string
	for _, n := range net.Nodes {
		allDirs = append(allDirs, n.WorkingDir)
	}

	return propsLoadMsg{
		network:        net,
		tokens:         totals,
		selectedTokens: selectedTokens,
		selectedStatus: selectedStatus,
		adminStart:     adminStart,
		agentDirs:      allDirs,
		agentNodes:     net.Nodes,
	}
}

func (m PropsModel) Init() tea.Cmd { return m.loadData }

// AutoReloadCmd implements autoReloadable: on the app-level 1s tick, reload the
// kanban dashboard from disk so network/token/status data stays live without a
// manual Ctrl+R. It returns the same command as Ctrl+R (loadData), which
// updates fields in place and re-renders without resetting scroll, cursor, or
// folder state.
//
// Returns nil — skipping this tick — while the agent picker is open (selectedDir
// is mid-change). The Ctrl+D detail pane remains live: App.autoRefreshActiveView
// refreshes the detail caches in place before this command reloads the outer
// dashboard data.
func (m PropsModel) AutoReloadCmd() tea.Cmd {
	if m.pickerOpen {
		return nil
	}
	return m.loadData
}

// propsHeaderLines is the number of lines used by the header (title + separator + optional callout).
const propsHeaderLines = 3

// propsFooterLines is the number of lines used by the footer (separator + hints).
const propsFooterLines = 2

func (m PropsModel) Update(msg tea.Msg) (PropsModel, tea.Cmd) {
	var cmd tea.Cmd
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - propsHeaderLines - propsFooterLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.viewport = viewport.New()
			m.viewport.SetWidth(m.width)
			m.viewport.SetHeight(vpHeight)
			m.ready = true
		} else {
			m.viewport.SetWidth(m.width)
			m.viewport.SetHeight(vpHeight)
		}
		m.syncViewportContent()

	case propsLoadMsg:
		m.network = msg.network
		m.tokens = msg.tokens
		m.selectedTokens = msg.selectedTokens
		m.selectedStatus = msg.selectedStatus
		m.adminStart = msg.adminStart
		m.agentDirs = msg.agentDirs
		m.agentNodes = msg.agentNodes
		m.syncViewportContent()

	case tea.MouseWheelMsg:
		if !m.pickerOpen {
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case tea.KeyPressMsg:
		if m.pickerOpen {
			return m.updatePicker(msg)
		}
		switch msg.String() {
		case "esc", "q":
			// Detail view first, then exit.
			if m.detailOpen {
				m.detailOpen = false
				m.viewport.GotoTop()
				m.syncViewportContent()
				return m, nil
			}
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
		case "ctrl+r":
			// Reload the dashboard data (network, tokens, agent status) from disk.
			return m, m.loadData
		case "ctrl+t":
			m.pickerOpen = true
			for i, n := range m.agentNodes {
				if n.WorkingDir == m.selectedDir {
					m.pickerIdx = i
					break
				}
			}
			m.syncViewportContent()
			return m, nil
		case "ctrl+d":
			// Toggle detail view. Reload the per-provider breakdown
			// from disk on every open so the data is fresh — these
			// reads are cheap (small local ledger, init, and daemon files).
			m.detailOpen = !m.detailOpen
			if m.detailOpen {
				m.loadDetail()
				m.viewport.GotoTop()
			}
			m.syncViewportContent()
			return m, nil
		default:
			// Forward navigation keys (up/down/pgup/pgdn/home/end) to viewport
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
	}
	return m, nil
}

// loadDetail populates the detail-view caches from disk for the
// currently-selected agent. Called every time the detail view is
// opened so the user sees fresh numbers.
func (m *PropsModel) loadDetail() {
	ledgerPath := filepath.Join(m.selectedDir, "logs", "token_ledger.jsonl")
	m.detailByProvider, m.detailRecent = fs.SumTokenLedgerByProvider(ledgerPath, detailRecentCalls)
	// Daemon calls are scoped to the selected agent's own daemon run dirs
	// (agentDir/daemons/<run_id>/logs/token_ledger.jsonl), not the whole
	// network. Missing ledgers render an empty lane.
	m.detailDaemonRecent = fs.DaemonRecentLedger(m.selectedDir, detailRecentCalls)
	m.detailContextStats = fs.ReadContextStats(m.selectedDir)

	// MCP names from init.json's mcp block.
	m.detailMCPNames = nil
	if initRaw, err := fs.ReadInitManifest(m.selectedDir); err == nil {
		if mcp, ok := initRaw["mcp"].(map[string]interface{}); ok {
			for name := range mcp {
				m.detailMCPNames = append(m.detailMCPNames, name)
			}
			sort.Strings(m.detailMCPNames)
		}
	}

	// Daemon run counts from daemons/<run_id>/daemon.json.
	m.detailDaemonCounts = fs.CountDaemons(m.selectedDir)
}

// syncViewportContent re-renders left+right panels into the viewport.
func (m *PropsModel) syncViewportContent() {
	if !m.ready {
		return
	}
	switch {
	case m.pickerOpen:
		m.viewport.SetContent(m.renderPicker())
	case m.detailOpen:
		m.viewport.SetContent(m.renderDetail())
	default:
		m.viewport.SetContent(m.renderBody())
	}
}

func (m PropsModel) updatePicker(msg tea.KeyPressMsg) (PropsModel, tea.Cmd) {
	switch msg.String() {
	case "esc", "ctrl+t":
		m.pickerOpen = false
		m.syncViewportContent()
	case "up", "k":
		if m.pickerIdx > 0 {
			m.pickerIdx--
			m.syncViewportContent()
		}
	case "down", "j":
		if m.pickerIdx < len(m.agentNodes)-1 {
			m.pickerIdx++
			m.syncViewportContent()
		}
	case "enter":
		if m.pickerIdx < len(m.agentNodes) {
			m.selectedDir = m.agentNodes[m.pickerIdx].WorkingDir
			m.selectedTokens = fs.SumTokenLedger(filepath.Join(m.selectedDir, "logs", "token_ledger.jsonl"))
			m.selectedStatus = fs.ReadStatus(m.selectedDir)
		}
		m.pickerOpen = false
		m.syncViewportContent()
	}
	return m, nil
}

type propsField struct {
	key   string
	label string
}

func (m PropsModel) renderBody() string {
	leftW := m.width/2 - 1
	rightW := m.width - leftW - 1
	if leftW < 20 {
		leftW = 20
	}
	if rightW < 20 {
		rightW = 20
	}
	// Safety: don't exceed terminal width
	if leftW+1+rightW > m.width && m.width > 1 {
		rightW = m.width - leftW - 1
		if rightW < 0 {
			rightW = 0
		}
	}

	leftContent := m.renderLeft(leftW)
	rightContent := m.renderRight(rightW)

	leftLines := strings.Split(leftContent, "\n")
	rightLines := strings.Split(rightContent, "\n")

	maxLines := len(leftLines)
	if len(rightLines) > maxLines {
		maxLines = len(rightLines)
	}
	for len(leftLines) < maxLines {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < maxLines {
		rightLines = append(rightLines, "")
	}

	sep := lipgloss.NewStyle().Foreground(ColorTextFaint).Render("│")

	// Pad to viewport height so the separator column runs full-screen
	vpHeight := m.height - propsHeaderLines - propsFooterLines
	if vpHeight < 1 {
		vpHeight = 1
	}
	for len(leftLines) < vpHeight {
		leftLines = append(leftLines, "")
	}
	for len(rightLines) < vpHeight {
		rightLines = append(rightLines, "")
	}
	if len(leftLines) > len(rightLines) {
		for len(rightLines) < len(leftLines) {
			rightLines = append(rightLines, "")
		}
	} else {
		for len(leftLines) < len(rightLines) {
			leftLines = append(leftLines, "")
		}
	}

	var body strings.Builder
	for i := 0; i < len(leftLines); i++ {
		l := padToWidth(leftLines[i], leftW)
		body.WriteString(l + sep + rightLines[i] + "\n")
	}

	return strings.TrimRight(body.String(), "\n")
}

func (m PropsModel) View() string {
	title := i18n.T("props.title")
	if m.detailOpen {
		title = i18n.T("props.detail_title")
	}
	header := StyleTitle.Render("  "+title) + "\n" + strings.Repeat("\u2500", m.width)
	if !m.detailOpen {
		header += "\n" + "  " + StyleAccent.Render("⎔ "+i18n.T("props.ctrl_d_hint"))
	} else {
		header += "\n"
	}

	scrollHint := ""
	if m.ready && !m.viewport.AtBottom() {
		scrollHint = " " + RuneBullet + " ↑↓ scroll"
	}

	// Refresh hint: a "live" badge when auto-refresh is on, plus the ctrl+r
	// manual fallback that exists either way. Consolidated here so the kanban
	// has a single source of truth for the reload hint (supersedes #369, which
	// added a bare "ctrl+r reload" hint before auto-refresh existed).
	refreshHint := i18n.T("props.ctrl_r_reload")
	if m.AutoRefresh {
		refreshHint = i18n.T("hints.auto_refresh_live") + " " + RuneBullet + " " + refreshHint
	}

	var footerLine string
	if m.detailOpen {
		footerLine = "  " + refreshHint + " " + RuneBullet +
			" esc " + i18n.T("props.detail_back_to_summary") + scrollHint
	} else {
		footerLine = "  " + refreshHint + " " + RuneBullet +
			" " + i18n.T("hints.props_off") + " " + RuneBullet +
			" esc " + i18n.T("manage.back") + " " + RuneBullet +
			" " + i18n.T("hints.props_select") + " " + RuneBullet +
			" ctrl+d " + i18n.T("props.detail_open") + scrollHint
	}
	footer := strings.Repeat("\u2500", m.width) + "\n" + StyleFaint.Render(footerLine)

	return header + "\n" + PaintViewportBG(m.viewport.View(), m.width) + "\n" + footer
}

func padToWidth(s string, w int) string {
	visible := lipgloss.Width(s)
	if visible >= w {
		return s
	}
	return s + strings.Repeat(" ", w-visible)
}

func (m PropsModel) renderLeft(maxW int) string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string

	raw, err := fs.ReadAgentRaw(m.selectedDir)
	if err != nil {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.no_data")))
		return strings.Join(lines, "\n")
	}

	if initRaw, err := fs.ReadInitManifest(m.selectedDir); err == nil {
		for k, v := range initRaw {
			if _, exists := raw[k]; !exists {
				raw[k] = v
			}
		}
	}

	renderFields := func(fields []propsField) {
		for _, f := range fields {
			v, ok := raw[f.key]
			if !ok || v == nil {
				continue
			}
			val := fmt.Sprintf("%v", v)
			if val == "" {
				continue
			}
			if f.key == "state" {
				stateColor := StateColor(strings.ToUpper(val))
				val = lipgloss.NewStyle().Foreground(stateColor).Render(val)
			} else {
				val = valueStyle.Render(val)
			}
			lines = append(lines, "  "+labelStyle.Render(f.label+": ")+val)
		}
	}

	// Identity
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_identity")))
	lines = append(lines, "")
	renderFields([]propsField{
		{"agent_name", i18n.T("props.name")},
		{"nickname", i18n.T("props.nickname")},
		{"agent_id", i18n.T("props.id")},
		{"state", i18n.T("props.state")},
		{"address", i18n.T("props.address")},
		{"language", i18n.T("props.language")},
		{"started_at", i18n.T("props.started_at")},
		{"combo", i18n.T("props.combo")},
	})

	// LLM
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_llm")))
	lines = append(lines, "")
	renderFields([]propsField{
		{"model", i18n.T("props.model")},
		{"provider", i18n.T("props.provider")},
		{"base_url", i18n.T("props.base_url")},
		{"api_compat", i18n.T("props.api_compat")},
		{"api_key_env", i18n.T("props.api_key_env")},
		{"streaming", i18n.T("props.streaming")},
		{"context_limit", i18n.T("props.context_limit")},
	})

	// Runtime
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_runtime")))
	lines = append(lines, "")
	renderFields([]propsField{
		{"stamina", i18n.T("props.stamina")},
		{"molt_pressure", i18n.T("props.molt_pressure")},
		{"soul_delay", i18n.T("props.soul_delay")},
		{"molt_count", i18n.T("props.molt_count")},
		{"max_turns", i18n.T("props.max_turns")},
		{"max_rpm", i18n.T("props.max_rpm")},
	})

	// Context window (from cached .status.json)
	ctx := m.selectedStatus.Tokens.Context
	if ctx.WindowSize > 0 {
		lines = append(lines, "")
		lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_context")))
		lines = append(lines, "")
		pctColor := ColorAgent
		if ctx.UsagePct > 80 {
			pctColor = lipgloss.Color("#e06c75")
		} else if ctx.UsagePct > 60 {
			pctColor = lipgloss.Color("#e5c07b")
		}
		lines = append(lines, "  "+labelStyle.Render("usage:   ")+lipgloss.NewStyle().Foreground(pctColor).Render(
			fmt.Sprintf("%s / %s (%.1f%%)", formatComma(int64(ctx.TotalTokens)), formatComma(int64(ctx.WindowSize)), ctx.UsagePct)))
		lines = append(lines, "  "+labelStyle.Render("system:  ")+valueStyle.Render(formatComma(int64(ctx.SystemTokens))))
		lines = append(lines, "  "+labelStyle.Render("tools:   ")+valueStyle.Render(formatComma(int64(ctx.ToolsTokens))))
		lines = append(lines, "  "+labelStyle.Render("history: ")+valueStyle.Render(formatComma(int64(ctx.HistoryTokens))))
	}

	// Presets — surfaces manifest.preset.{default, active, allowed}
	// with a key-presence and existence check per allowed entry. Keeps
	// answers to "what can this agent run, and is anything broken?"
	// one screen away from the agent's other vitals.
	if presetBlock, ok := raw["preset"].(map[string]interface{}); ok {
		defaultRef, _ := presetBlock["default"].(string)
		activeRef, _ := presetBlock["active"].(string)
		var allowedRefs []string
		if al, ok := presetBlock["allowed"].([]interface{}); ok {
			for _, e := range al {
				if s, ok := e.(string); ok && s != "" {
					allowedRefs = append(allowedRefs, s)
				}
			}
		}
		if defaultRef != "" || activeRef != "" || len(allowedRefs) > 0 {
			lines = append(lines, "")
			lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_presets")))
			lines = append(lines, "")

			// Single line when active and default match (the common case);
			// otherwise show both. We render the home-shortened name
			// rather than the full ref string — the allowed list below
			// shows full names so the active line is just a label.
			defaultName := refDisplayName(defaultRef)
			activeName := refDisplayName(activeRef)
			if activeRef == defaultRef && activeRef != "" {
				lines = append(lines, "  "+labelStyle.Render(i18n.T("props.preset_active")+": ")+valueStyle.Render(activeName))
			} else {
				if activeName != "" {
					lines = append(lines, "  "+labelStyle.Render(i18n.T("props.preset_active")+": ")+valueStyle.Render(activeName))
				}
				if defaultName != "" {
					lines = append(lines, "  "+labelStyle.Render(i18n.T("props.preset_default")+": ")+valueStyle.Render(defaultName))
				}
			}

			if len(allowedRefs) > 0 {
				cfg, _ := config.LoadConfig(m.globalDir)
				auth := preset.AuthState{
					CodexOAuthConfigured:     codexOAuthConfigured(m.globalDir),
					ClaudeCodeAuthConfigured: claudeCodeAuthConfigured(),
				}
				resolved := preset.ResolveRefsWithAuth(allowedRefs, cfg.Keys, auth)
				lines = append(lines, "  "+labelStyle.Render(i18n.T("props.preset_allowed")+":"))
				for _, rr := range resolved {
					marker := lipgloss.NewStyle().Foreground(StateColor("ACTIVE")).Render("✓")
					if !rr.Exists || !rr.HasKey {
						marker = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")).Render("✗")
					}
					tag := ""
					switch rr.Source {
					case preset.SourceTemplate:
						tag = " " + labelStyle.Render("("+i18n.T("props.preset_source_template")+")")
					case preset.SourceSaved:
						tag = " " + labelStyle.Render("("+i18n.T("props.preset_source_saved")+")")
					}
					name := rr.Name
					if name == "" {
						name = rr.Ref
					}
					lines = append(lines, "    "+marker+" "+valueStyle.Render(name)+tag)
				}
			}
		}
	}

	// Capabilities
	if caps, ok := raw["capabilities"]; ok && caps != nil {
		lines = append(lines, "")
		lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_capabilities")))
		lines = append(lines, "")
		capsJSON, _ := json.Marshal(caps)
		capNames := fs.CapabilitiesForDisplay(fs.ParseCapabilities(capsJSON))
		if len(capNames) > 0 {
			capStr := strings.Join(capNames, ", ")
			wrapped := lipgloss.NewStyle().Width(maxW - 6).Render(capStr)
			for _, line := range strings.Split(wrapped, "\n") {
				lines = append(lines, "    "+valueStyle.Render(line))
			}
		}
	}

	// Tokens (from cached ledger)
	if m.selectedTokens.APICalls > 0 {
		lines = append(lines, "")
		lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_tokens")))
		if m.selectedStatus.Tokens.Estimated {
			warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b"))
			lines = append(lines, "  "+warnStyle.Render("⚠ estimated (provider did not return usage)"))
		}
		lines = append(lines, "")
		lines = append(lines, "    "+valueStyle.Render(fmt.Sprintf("input: %s", formatComma(m.selectedTokens.Input))))
		lines = append(lines, "    "+valueStyle.Render(fmt.Sprintf("output: %s", formatComma(m.selectedTokens.Output))))
		lines = append(lines, "    "+valueStyle.Render(fmt.Sprintf("thinking: %s", formatComma(m.selectedTokens.Thinking))))
		// Cache: show absolute cached tokens + hit rate as %. Rate = cached / input
		// across the ledger's lifetime — sum of cache_read_input_tokens over sum of
		// total input_tokens (input_tokens here is already the true total: raw +
		// cache_read + cache_write, normalised in each adapter).
		cacheRateStr := ""
		if m.selectedTokens.Input > 0 {
			cacheRateStr = fmt.Sprintf(" (%.1f%%)", 100.0*float64(m.selectedTokens.Cached)/float64(m.selectedTokens.Input))
		}
		lines = append(lines, "    "+valueStyle.Render(fmt.Sprintf("cached: %s%s", formatComma(m.selectedTokens.Cached), cacheRateStr)))
		lines = append(lines, "    "+valueStyle.Render(fmt.Sprintf("api_calls: %d", m.selectedTokens.APICalls)))
	}

	// Admin
	if admin, ok := raw["admin"]; ok && admin != nil {
		if adminMap, ok := admin.(map[string]interface{}); ok && len(adminMap) > 0 {
			lines = append(lines, "")
			lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_admin")))
			lines = append(lines, "")
			adminKeys := make([]string, 0, len(adminMap))
			for k := range adminMap {
				adminKeys = append(adminKeys, k)
			}
			sort.Strings(adminKeys)
			for _, k := range adminKeys {
				lines = append(lines, "    "+valueStyle.Render(fmt.Sprintf("%s: %v", k, adminMap[k])))
			}
		}
	}

	return strings.Join(lines, "\n")
}

func (m PropsModel) renderRight(maxW int) string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string

	// Network
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.section_network")))
	lines = append(lines, "")

	if m.adminStart != "" {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.network_created")+": ")+valueStyle.Render(m.adminStart))
		if t, err := time.Parse(time.RFC3339, m.adminStart); err == nil {
			uptime := time.Since(t)
			lines = append(lines, "  "+labelStyle.Render(i18n.T("props.network_uptime")+": ")+valueStyle.Render(formatDuration(uptime)))
		}
	}

	stats := m.network.Stats
	totalAgents := len(m.network.Nodes)
	var humanCount, agentCount int
	for _, n := range m.network.Nodes {
		if n.IsHuman {
			humanCount++
		} else {
			agentCount++
		}
	}
	lines = append(lines, "  "+labelStyle.Render(i18n.T("props.network_agents")+": ")+
		valueStyle.Render(fmt.Sprintf("%d", totalAgents))+
		labelStyle.Render(fmt.Sprintf("  (%d %s, %d %s)",
			agentCount, i18n.T("props.network_agents"), humanCount, i18n.T("props.network_humans"))))

	var stateParts []string
	if stats.Active > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("ACTIVE"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.active"), stats.Active)))
	}
	if stats.Idle > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("IDLE"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.idle"), stats.Idle)))
	}
	if stats.Stuck > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("STUCK"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.stuck"), stats.Stuck)))
	}
	if stats.Asleep > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("ASLEEP"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.asleep"), stats.Asleep)))
	}
	if stats.Suspended > 0 {
		c := lipgloss.NewStyle().Foreground(StateColor("SUSPENDED"))
		stateParts = append(stateParts, c.Render(fmt.Sprintf("%s: %d", i18n.T("state.suspended"), stats.Suspended)))
	}
	if len(stateParts) > 0 {
		lines = append(lines, "  "+strings.Join(stateParts, "  "))
	}
	if m.network.Activity.Status != "" {
		c := lipgloss.NewStyle().Foreground(NetworkActivityColor(m.network.Activity.Status))
		lines = append(lines, "  "+labelStyle.Render(networkActivityLabel()+": ")+c.Render(networkActivityStatusLabel(m.network.Activity.Status)))
	}
	lines = append(lines, "  "+labelStyle.Render(i18n.T("props.network_daemons")+": ")+
		valueStyle.Render(fmt.Sprintf("%d %s", m.network.Activity.RunningDaemons, i18n.T("props.network_daemons_running"))))

	// Tokens
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.total_tokens")))
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle.Render("Input:    ")+valueStyle.Render(formatComma(m.tokens.Input)))
	lines = append(lines, "  "+labelStyle.Render("Output:   ")+valueStyle.Render(formatComma(m.tokens.Output)))
	lines = append(lines, "  "+labelStyle.Render("Thinking: ")+valueStyle.Render(formatComma(m.tokens.Thinking)))
	// Cached row shows absolute + cache-hit rate across the whole network
	// (sum of cache_read / sum of total input, same denominator semantics
	// as the per-agent ledger view).
	cachedStr := formatComma(m.tokens.Cached)
	if m.tokens.Input > 0 {
		cachedStr = fmt.Sprintf("%s (%.1f%%)", cachedStr, 100.0*float64(m.tokens.Cached)/float64(m.tokens.Input))
	}
	lines = append(lines, "  "+labelStyle.Render("Cached:   ")+valueStyle.Render(cachedStr))

	// API Calls
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.total_api_calls")))
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle.Render("Total: ")+valueStyle.Render(formatComma(m.tokens.APICalls)))

	// Mail
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.total_mails")))
	lines = append(lines, "")
	lines = append(lines, "  "+labelStyle.Render("Total: ")+valueStyle.Render(fmt.Sprintf("%d", stats.TotalMails)))

	// Avatar tree
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.tree")))
	lines = append(lines, "")
	lines = append(lines, m.renderTree(maxW)...)

	return strings.Join(lines, "\n")
}

// renderDetail renders the full-screen detail view: token usage broken
// down by provider, recent activity, MCP servers, and daemon run counts.
// Toggled with Ctrl+D from the kanban summary.
func (m PropsModel) renderDetail() string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	subtleStyle := lipgloss.NewStyle().Foreground(ColorTextFaint)

	var lines []string

	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_tokens_by_provider")))
	lines = append(lines, "")

	// Compute total tokens (input+output+thinking) across providers so
	// each provider's bar shows its share. Cached are excluded from the
	// share denominator — they're a discount, not consumption.
	var grandSpend int64
	for _, t := range m.detailByProvider {
		grandSpend += t.Input + t.Output + t.Thinking
	}

	// Stable order: highest spend first.
	type provLine struct {
		name  string
		t     fs.TokenTotals
		spend int64
	}
	var rows []provLine
	for name, t := range m.detailByProvider {
		rows = append(rows, provLine{name, t, t.Input + t.Output + t.Thinking})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].spend != rows[j].spend {
			return rows[i].spend > rows[j].spend
		}
		return rows[i].name < rows[j].name
	})

	if len(rows) == 0 {
		lines = append(lines, "  "+subtleStyle.Render(i18n.T("props.detail_no_tokens")))
	}
	for _, r := range rows {
		pct := 0.0
		if grandSpend > 0 {
			pct = 100.0 * float64(r.spend) / float64(grandSpend)
		}
		bar := renderShareBar(pct, 20)
		nameStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
		header := fmt.Sprintf("  %-14s %s %5.1f%%",
			nameStyle.Render(r.name), bar, pct)
		lines = append(lines, header)
		lines = append(lines, "    "+labelStyle.Render("input:    ")+valueStyle.Render(formatComma(r.t.Input))+
			labelStyle.Render("    output:    ")+valueStyle.Render(formatComma(r.t.Output)))
		lines = append(lines, "    "+labelStyle.Render("thinking: ")+valueStyle.Render(formatComma(r.t.Thinking))+
			labelStyle.Render("    cached:    ")+valueStyle.Render(formatComma(r.t.Cached)))
		hitStr := ""
		if r.t.Input > 0 {
			hitStr = fmt.Sprintf("    cache hit: %.1f%%", 100.0*float64(r.t.Cached)/float64(r.t.Input))
		}
		lines = append(lines, "    "+labelStyle.Render("api_calls: ")+valueStyle.Render(fmt.Sprintf("%d", r.t.APICalls))+
			labelStyle.Render(hitStr))
		lines = append(lines, "")
	}

	// Totals.
	if len(rows) > 0 {
		lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_totals")))
		lines = append(lines, "")
		var tot fs.TokenTotals
		for _, r := range rows {
			tot.Input += r.t.Input
			tot.Output += r.t.Output
			tot.Thinking += r.t.Thinking
			tot.Cached += r.t.Cached
			tot.APICalls += r.t.APICalls
		}
		lines = append(lines, "    "+labelStyle.Render("input + output + thinking: ")+
			valueStyle.Render(formatComma(tot.Input+tot.Output+tot.Thinking)))
		lines = append(lines, "    "+labelStyle.Render("cached:                    ")+
			valueStyle.Render(formatComma(tot.Cached)))
		lines = append(lines, "    "+labelStyle.Render("api_calls:                 ")+
			valueStyle.Render(fmt.Sprintf("%d", tot.APICalls)))
		if tot.Input > 0 {
			lines = append(lines, "    "+labelStyle.Render("cache hit rate:            ")+
				valueStyle.Render(fmt.Sprintf("%.1f%%", 100.0*float64(tot.Cached)/float64(tot.Input))))
		}
		lines = append(lines, "")
	}

	// Current retained context statistics.
	if m.detailContextStats.Entries > 0 {
		stats := m.detailContextStats
		lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_context_stats")))
		lines = append(lines, "")
		lines = append(lines, "    "+labelStyle.Render("entries:                  ")+
			valueStyle.Render(fmt.Sprintf("%d", stats.Entries)))
		lines = append(lines, "    "+labelStyle.Render("messages:                 ")+
			valueStyle.Render(fmt.Sprintf("system:%d  assistant:%d  user:%d", stats.SystemMessages, stats.AssistantMessages, stats.UserMessages)))
		lines = append(lines, "    "+labelStyle.Render("text input / output:      ")+
			valueStyle.Render(fmt.Sprintf("%d / %d", stats.TextInputs, stats.TextOutputs)))
		lines = append(lines, "    "+labelStyle.Render("tool calls / results:     ")+
			valueStyle.Render(fmt.Sprintf("%d / %d", stats.ToolCalls, stats.ToolResults)))
		if len(stats.ToolCounts) > 0 {
			lines = append(lines, "")
			lines = append(lines, "    "+labelStyle.Render("tools in context:"))
			for _, tc := range stats.ToolCounts {
				lines = append(lines, fmt.Sprintf("      %-14s calls:%s  results:%s",
					valueStyle.Render(tc.Name),
					formatComma(int64(tc.Calls)),
					formatComma(int64(tc.Results)),
				))
			}
		}
		lines = append(lines, "")
	}

	// MCP servers.
	if len(m.detailMCPNames) > 0 {
		lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_mcp")))
		lines = append(lines, "")
		for _, name := range m.detailMCPNames {
			lines = append(lines, "    "+valueStyle.Render(name))
		}
		lines = append(lines, "")
	}

	// Daemon run counts.
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_daemons")))
	lines = append(lines, "")
	lines = append(lines, "    "+labelStyle.Render(i18n.T("props.detail_daemons_running")+": ")+
		valueStyle.Render(fmt.Sprintf("%d", m.detailDaemonCounts.Running)))
	lines = append(lines, "    "+labelStyle.Render(i18n.T("props.detail_daemons_total")+": ")+
		valueStyle.Render(fmt.Sprintf("%d", m.detailDaemonCounts.Total)))
	lines = append(lines, "")

	// Raw recent token-ledger lanes are useful for diagnosis but visually noisy,
	// so they come last after the higher-signal provider totals, context, MCP,
	// and daemon-count summaries.
	lines = append(lines, m.renderRecentCallLanes()...)

	return strings.Join(lines, "\n")
}

// renderRecentCallLanes renders the lower diagnostic ledger section in a
// single-column order: selected main-agent calls first, then stacked daemon
// calls. Raw ledgers are intentionally below the higher-signal summaries in
// renderDetail.
func (m PropsModel) renderRecentCallLanes() []string {
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_recent_main")))
	lines = append(lines, "")
	lines = append(lines, m.renderMainCallRows()...)
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_recent_daemons")))
	lines = append(lines, "")
	lines = append(lines, m.renderDaemonCallRows()...)
	lines = append(lines, "")
	return lines
}

// renderMainCallRows renders the selected agent's recent per-call ledger
// entries (newest first). Rows deliberately avoid truncating model/endpoint
// fields so detail mode can preserve raw diagnostic evidence.
func (m PropsModel) renderMainCallRows() []string {
	subtleStyle := lipgloss.NewStyle().Foreground(ColorTextFaint)
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)

	if len(m.detailRecent) == 0 {
		return []string{"  " + subtleStyle.Render(i18n.T("props.detail_recent_empty"))}
	}

	lines := []string{
		"  " + labelStyle.Render(fmt.Sprintf("%-16s  %-10s  %-24s  %10s  %10s  %10s  %10s  %7s  %s",
			"time", "provider", "model", "input", "output", "thinking", "cached", "cache%", "endpoint")),
	}
	for _, e := range m.detailRecent {
		provider := fs.DeriveLedgerProvider(e.Endpoint, e.Model)
		model := e.Model
		if model == "" {
			model = "—"
		}
		endpoint := e.Endpoint
		if endpoint == "" {
			endpoint = "—"
		}
		line := fmt.Sprintf("  %-16s  %-10s  %-24s  %10s  %10s  %10s  %10s  %7s  %s",
			shortTS(e.TS),
			provider,
			model,
			formatComma(e.Input),
			formatComma(e.Output),
			formatComma(e.Thinking),
			formatComma(e.Cached),
			formatCacheRate(e.Cached, e.Input),
			endpoint,
		)
		lines = append(lines, valueStyle.Render(line))
	}
	return lines
}

// renderDaemonCallRows renders all daemon per-call ledger entries (newest
// first), each row retaining daemon handle/run id/state plus provider/model /
// endpoint data without truncation.
func (m PropsModel) renderDaemonCallRows() []string {
	subtleStyle := lipgloss.NewStyle().Foreground(ColorTextFaint)
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)

	if len(m.detailDaemonRecent) == 0 {
		return []string{"  " + subtleStyle.Render(i18n.T("props.detail_recent_daemons_empty"))}
	}

	lines := []string{
		"  " + labelStyle.Render(fmt.Sprintf("%-16s  %-10s  %-24s  %-8s  %-10s  %-24s  %10s  %10s  %10s  %10s  %7s  %s",
			"time", "daemon", "run", "state", "provider", "model", "input", "output", "thinking", "cached", "cache%", "endpoint")),
	}
	for _, e := range m.detailDaemonRecent {
		provider := fs.DeriveLedgerProvider(e.Endpoint, e.Model)
		model := e.Model
		if model == "" {
			model = "—"
		}
		endpoint := e.Endpoint
		if endpoint == "" {
			endpoint = "—"
		}
		handle := e.Handle
		if handle == "" {
			handle = "—"
		}
		runID := e.RunID
		if runID == "" {
			runID = "—"
		}
		state := e.State
		if state == "" {
			state = "—"
		}
		line := fmt.Sprintf("  %-16s  %-10s  %-24s  %-8s  %-10s  %-24s  %10s  %10s  %10s  %10s  %7s  %s",
			shortTS(e.TS),
			handle,
			runID,
			state,
			provider,
			model,
			formatComma(e.Input),
			formatComma(e.Output),
			formatComma(e.Thinking),
			formatComma(e.Cached),
			formatCacheRate(e.Cached, e.Input),
			endpoint,
		)
		lines = append(lines, valueStyle.Render(line))
	}
	return lines
}

func formatCacheRate(cached, input int64) string {
	if input <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", 100.0*float64(cached)/float64(input))
}

// shortTS trims an ISO-8601 timestamp to minute precision (2026-04-30T03:14)
// for compact display, leaving shorter/empty strings untouched.
func shortTS(ts string) string {
	if len(ts) > 16 {
		return ts[:16]
	}
	return ts
}

// renderShareBar returns a small unicode bar (filled + empty cells)
// proportional to pct (0..100). width is the total cell count.
func renderShareBar(pct float64, width int) string {
	if width < 1 {
		width = 1
	}
	filled := int((pct / 100.0) * float64(width))
	if filled < 0 {
		filled = 0
	}
	if filled > width {
		filled = width
	}
	full := lipgloss.NewStyle().Foreground(ColorAccent).Render(strings.Repeat("█", filled))
	empty := lipgloss.NewStyle().Foreground(ColorTextFaint).Render(strings.Repeat("░", width-filled))
	return full + empty
}

// truncate trims s to n runes, appending "…" when shortened. Used to
// keep the recent-activity model column from overflowing.
func truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if n == 1 {
		return "…"
	}
	return string(r[:n-1]) + "…"
}

func (m PropsModel) renderPicker() string {
	if len(m.agentNodes) == 0 {
		return ""
	}

	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string
	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.select_agent")))
	lines = append(lines, "")

	for i, n := range m.agentNodes {
		name := n.AgentName
		if n.Nickname != "" {
			name = n.Nickname
		}
		if name == "" {
			name = "(unknown)"
		}

		state := n.State
		if state == "" {
			state = "──"
		}
		stateRendered := lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(state))).Render(state)

		marker := "  "
		style := nameStyle
		if n.WorkingDir == m.selectedDir {
			marker = "● "
		}
		if i == m.pickerIdx {
			style = selectedStyle
			marker = "> "
			if n.WorkingDir == m.selectedDir {
				marker = ">●"
			}
		}

		lines = append(lines, fmt.Sprintf("  %s%-18s %s", marker, style.Render(name), stateRendered))
	}

	lines = append(lines, "")
	lines = append(lines, "  "+StyleFaint.Render("↑↓ "+i18n.T("manage.select")+"  [enter]  [esc/ctrl+t] "+i18n.T("manage.back")))

	return strings.Join(lines, "\n")
}

func (m PropsModel) renderTree(maxW int) []string {
	nodes := m.network.Nodes
	edges := m.network.AvatarEdges
	if len(nodes) == 0 {
		return nil
	}

	nodeMap := make(map[string]fs.AgentNode)
	for _, n := range nodes {
		nodeMap[n.Address] = n
	}

	childrenOf := make(map[string][]string)
	childSet := make(map[string]bool)
	for _, e := range edges {
		childrenOf[e.Parent] = append(childrenOf[e.Parent], e.Child)
		childSet[e.Child] = true
	}

	// Roots: human first, then admins (no parent)
	var roots []fs.AgentNode
	for _, n := range nodes {
		if n.IsHuman {
			roots = append([]fs.AgentNode{n}, roots...)
		} else if !childSet[n.Address] {
			roots = append(roots, n)
		}
	}

	nameOf := func(n fs.AgentNode) string {
		if n.Nickname != "" {
			return n.Nickname
		}
		if n.AgentName != "" {
			return n.AgentName
		}
		parts := strings.Split(n.Address, "/")
		return parts[len(parts)-1]
	}

	var lines []string
	var walk func(addr, prefix string, isLast, isRoot bool)
	walk = func(addr, prefix string, isLast, isRoot bool) {
		n, ok := nodeMap[addr]
		if !ok {
			return
		}
		connector := ""
		if !isRoot {
			if isLast {
				connector = "└ "
			} else {
				connector = "├ "
			}
		}
		stateColor := StateColor(strings.ToUpper(n.State))
		name := lipgloss.NewStyle().Foreground(stateColor).Render(nameOf(n))
		dimPrefix := lipgloss.NewStyle().Foreground(ColorTextFaint).Render(prefix + connector)
		lines = append(lines, "  "+dimPrefix+name)

		children := childrenOf[addr]
		childPrefix := prefix
		if !isRoot {
			if isLast {
				childPrefix += "  "
			} else {
				childPrefix += "│ "
			}
		}
		for i, c := range children {
			walk(c, childPrefix, i == len(children)-1, false)
		}
	}

	for i, r := range roots {
		walk(r.Address, "", i == len(roots)-1, true)
	}
	return lines
}

func formatComma(n int64) string {
	if n < 0 {
		return "-" + formatComma(-n)
	}
	s := fmt.Sprintf("%d", n)
	if len(s) <= 3 {
		return s
	}
	var result strings.Builder
	offset := len(s) % 3
	if offset > 0 {
		result.WriteString(s[:offset])
	}
	for i := offset; i < len(s); i += 3 {
		if result.Len() > 0 {
			result.WriteByte(',')
		}
		result.WriteString(s[i : i+3])
	}
	return result.String()
}

func formatDuration(d time.Duration) string {
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	minutes := int(d.Minutes()) % 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh %dm", days, hours, minutes)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, minutes)
	}
	return fmt.Sprintf("%dm", minutes)
}

// refDisplayName extracts the filename stem from a preset path string
// for compact display. "~/.lingtai-tui/presets/saved/mimo-1.json"
// → "mimo-1". Empty input → empty output.
func refDisplayName(ref string) string {
	if ref == "" {
		return ""
	}
	// Strip directory prefix.
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		ref = ref[i+1:]
	}
	// Strip extension.
	if i := strings.LastIndex(ref, "."); i >= 0 {
		ref = ref[:i]
	}
	return ref
}
