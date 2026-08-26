package tui

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

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
	network            fs.Network
	mailStatsAvailable bool
	tokens             fs.TokenTotals
	adminStart         string // admin agent's started_at timestamp

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
	detailOpen                bool
	detailByProvider          map[string]fs.TokenTotals
	detailDaemonByProvider    map[string]fs.TokenTotals // daemon token usage by provider/backend
	detailRecent              []fs.LedgerEntry          // selected main agent recent calls (newest first)
	detailDaemonRecent        []fs.DaemonLedgerEntry    // all daemon calls, newest first, tagged by run
	detailCurrentSessionStats fs.SessionTokenStats
	detailLastSessionStats    fs.SessionTokenStats
	// detailCurrentSessionToolCalls / detailLastSessionToolCalls hold lifecycle
	// tool_call counts for the same molt windows as the session API stats.
	// They are sourced from the events store (fs.SumMoltSessionToolCalls) and
	// overlaid onto the detail view independently of the token-ledger cache, so
	// an events-only append invalidates the count even when the ledger is unchanged.
	detailCurrentSessionToolCalls int64
	detailLastSessionToolCalls    int64
	detailContextStats            fs.ContextStats
	detailDaemonCounts            fs.DaemonCounts
	detailDaemonRunsScanned       int // newest run dirs included in daemon token/call data
	detailDaemonRunsTotal         int // all run dirs counted without opening every daemon.json
	detailDaemonRunsTerminal      int // total minus bounded/observed-live nonterminal runs
	detailMCPNames                []string
	detailRebuilds                []time.Time // psyche_molt times, newest first; rendered as molt separators
	detailRefreshes               []time.Time // refresh_complete times, newest first; rendered as context-rebuilt separators
}

const (
	// detailRecentCalls is the number of recent token-ledger calls shown in each
	// Ctrl+D recent-call lane (main agent on the left, daemons on the right).
	detailRecentCalls = 100
	// detailRecentDaemonRuns caps daemon cards/ledgers opened by one detail scan.
	// The complete directory is metadata-scanned for ordering and an exact run-dir
	// total, but historical run contents outside this window are never opened.
	detailRecentDaemonRuns = 128
)

func NewPropsModel(baseDir, orchDir, globalDir string) PropsModel {
	return PropsModel{
		baseDir:     baseDir,
		orchDir:     orchDir,
		globalDir:   globalDir,
		selectedDir: orchDir,
	}
}

type propsLoadMsg struct {
	network            fs.Network
	mailStatsAvailable bool
	tokens             fs.TokenTotals
	selectedTokens     fs.TokenTotals
	selectedStatus     fs.AgentStatus
	adminStart         string
	agentDirs          []string
	agentNodes         []fs.AgentNode
}

func (m PropsModel) loadData() tea.Msg {
	net, _ := fs.BuildNetworkWithOptions(m.baseDir, fs.NetworkOptions{SkipMailEdges: true})

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
		network:            net,
		mailStatsAvailable: false,
		tokens:             totals,
		selectedTokens:     selectedTokens,
		selectedStatus:     selectedStatus,
		adminStart:         adminStart,
		agentDirs:          allDirs,
		agentNodes:         net.Nodes,
	}
}

func (m PropsModel) Init() tea.Cmd { return m.loadData }

// AutoReloadCmd implements autoReloadable: on the app-level 1s tick, reload the
// kanban dashboard from disk so network/token/status data stays live without a
// manual Ctrl+R. It returns the same command as Ctrl+R (loadData), which
// updates fields in place and re-renders without resetting scroll, cursor, or
// folder state.
//
// Returns nil while the agent picker or Ctrl+D detail pane is open. Detail is
// intentionally a point-in-time diagnostic: opening it, changing agent, or
// explicit Ctrl+R refreshes its O(number of runs) data, never the 1s tick.
func (m PropsModel) AutoReloadCmd() tea.Cmd {
	if m.pickerOpen || m.detailOpen {
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
		m.mailStatsAvailable = msg.mailStatsAvailable
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
			// Reload the cheap dashboard data. Detail is a point-in-time
			// diagnostic, so only an explicit refresh (or reopening it) pays for
			// its bounded, freshness-keyed filesystem snapshot.
			if m.detailOpen {
				m.loadDetail()
				m.syncViewportContent()
			}
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
	sessionStats := fs.SumMoltSessionTokenLedger(m.selectedDir)
	m.detailCurrentSessionStats = sessionStats.Current
	m.detailLastSessionStats = sessionStats.Last
	// Tool-call counts come from the events store, not the token ledger, so
	// they are fetched with their own (events-keyed) freshness and overlaid
	// onto the session stats here. The windows are resolved identically to
	// SumMoltSessionTokenLedger, so the counts align with the API rows.
	toolCounts := fs.SumMoltSessionToolCalls(m.selectedDir)
	m.detailCurrentSessionToolCalls = toolCounts.Current
	m.detailLastSessionToolCalls = toolCounts.Last
	// The daemon snapshot opens only the newest run window. It memoizes
	// unchanged cards/ledgers, forces a live run refresh when its heartbeat
	// changes, and returns the all-directory total from the same cached listing.
	// Per-run ledgers remain authoritative; CLI/legacy card snapshots fill in
	// when a selected run has no per-call ledger.
	daemonDetail := fs.ReadDaemonDetailSnapshot(m.selectedDir, detailRecentDaemonRuns, detailRecentCalls)
	m.detailDaemonByProvider = daemonDetail.ByProvider
	m.detailDaemonRecent = daemonDetail.Recent
	m.detailDaemonCounts = daemonDetail.Counts
	m.detailDaemonRunsScanned = daemonDetail.ScannedRuns
	m.detailDaemonRunsTotal = daemonDetail.TotalRuns
	m.detailDaemonRunsTerminal = daemonDetail.TerminalRuns
	m.detailContextStats = fs.ReadContextStats(m.selectedDir)
	// Boundary timestamps to mark in the main-agent ledger lane. Best-effort:
	// empty when no markers are available. Molt and /refresh reconstructions
	// are separate sources rendered with distinct labels.
	m.detailRebuilds = fs.RecentRebuildTimes(m.selectedDir, detailRecentCalls)
	m.detailRefreshes = fs.RecentRefreshCompleteTimes(m.selectedDir, detailRecentCalls)

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
			if m.detailOpen {
				m.loadDetail()
			}
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

const (
	kanbanLLMIdentityLimit     = 256
	kanbanLLMSettingLimit      = 64
	kanbanBaseURLLimit         = 2048
	kanbanCompactEndpointLimit = 64
)

// kanbanLLMConfig is the presentation-local, secret-free LLM identity used by
// /kanban. Runtime-safe fields prefer the kernel-materialized llm block in
// .agent.json (the same source Telegram's automatic Task Card reads) and fall
// back field-by-field to the resolved/init manifest. Thinking uses the same
// resolver for compatibility but normally falls back because today's runtime
// safelist does not publish it.
type kanbanLLMConfig struct {
	Model        string
	Provider     string
	BaseURL      string
	Endpoint     string
	ServiceTier  string
	Thinking     string
	APICompat    string
	APIKeyEnv    string
	Streaming    string
	ContextLimit string
}

type kanbanPresetInfo struct {
	DefaultRef string
	ActiveRef  string
	Allowed    []preset.ResolvedRef
}

func boundedKanbanText(v any, limit int) (string, bool) {
	s, ok := v.(string)
	if !ok {
		return "", false
	}
	s = strings.TrimSpace(s)
	if s == "" || len([]rune(s)) > limit || strings.IndexFunc(s, unicode.IsControl) >= 0 {
		return "", false
	}
	return s, true
}

func boundedKanbanField(raw map[string]any, key string, limit int) (value string, present bool) {
	v, present := raw[key]
	if !present {
		return "", false
	}
	value, _ = boundedKanbanText(v, limit)
	return value, true
}

// llmTextField distinguishes an omitted field (present=false) from a present
// malformed value (present=true, value=""). A malformed materialized value is
// omitted rather than replaced with potentially stale manifest data.
func llmTextField(raw map[string]any, key string, limit int) (value string, present bool) {
	block, blockPresent := raw["llm"]
	if !blockPresent {
		return "", false
	}
	llm, ok := block.(map[string]any)
	if !ok {
		return "", true
	}
	return boundedKanbanField(llm, key, limit)
}

func resolvedKanbanLLMText(agentRaw, initRaw map[string]any, key string, limit int) string {
	if value, present := llmTextField(agentRaw, key, limit); present {
		return value
	}
	if value, present := llmTextField(initRaw, key, limit); present {
		return value
	}
	value, _ := boundedKanbanField(initRaw, key, limit)
	return value
}

func boundedKanbanScalar(v any, limit int) (string, bool) {
	switch value := v.(type) {
	case string:
		return boundedKanbanText(value, limit)
	case bool, float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return boundedKanbanText(fmt.Sprint(value), limit)
	default:
		return "", false
	}
}

func llmScalarField(raw map[string]any, key string, limit int) (value string, present bool) {
	block, blockPresent := raw["llm"]
	if !blockPresent {
		return "", false
	}
	llm, ok := block.(map[string]any)
	if !ok {
		return "", true
	}
	v, present := llm[key]
	if !present {
		return "", false
	}
	switch number := v.(type) {
	case float64, float32, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		value, _ = boundedKanbanText(fmt.Sprint(number), limit)
	}
	return value, true
}

func resolvedKanbanLLMScalar(agentRaw, initRaw map[string]any, key string, limit int) string {
	if value, present := llmScalarField(agentRaw, key, limit); present {
		return value
	}
	if value, present := llmScalarField(initRaw, key, limit); present {
		return value
	}
	value, _ := boundedKanbanScalar(initRaw[key], limit)
	return value
}

func manifestLLMScalar(raw map[string]any, key string, limit int) string {
	if block, ok := raw["llm"].(map[string]any); ok {
		value, _ := boundedKanbanScalar(block[key], limit)
		return value
	}
	value, _ := boundedKanbanScalar(raw[key], limit)
	return value
}

// parseKanbanBaseURL validates once and derives a bounded, host-only summary
// endpoint. Malformed URLs and userinfo are omitted rather than exposed.
func parseKanbanBaseURL(raw string) (baseURL, endpoint string) {
	baseURL, ok := boundedKanbanText(raw, kanbanBaseURLLimit)
	if !ok {
		return "", ""
	}
	u, err := url.ParseRequestURI(baseURL)
	if err != nil || u.Scheme == "" || u.Hostname() == "" || u.User != nil {
		return "", ""
	}
	host := strings.ToLower(strings.TrimSpace(u.Hostname()))
	return baseURL, "@" + truncate(host, kanbanCompactEndpointLimit)
}

func resolveKanbanLLMConfig(agentRaw, initRaw map[string]any) kanbanLLMConfig {
	baseURL, endpoint := parseKanbanBaseURL(resolvedKanbanLLMText(agentRaw, initRaw, "base_url", kanbanBaseURLLimit))
	return kanbanLLMConfig{
		Model:        resolvedKanbanLLMText(agentRaw, initRaw, "model", kanbanLLMIdentityLimit),
		Provider:     resolvedKanbanLLMText(agentRaw, initRaw, "provider", kanbanLLMIdentityLimit),
		BaseURL:      baseURL,
		Endpoint:     endpoint,
		ServiceTier:  resolvedKanbanLLMText(agentRaw, initRaw, "service_tier", kanbanLLMSettingLimit),
		Thinking:     resolvedKanbanLLMText(agentRaw, initRaw, "thinking", kanbanLLMSettingLimit),
		APICompat:    resolvedKanbanLLMText(agentRaw, initRaw, "api_compat", kanbanLLMSettingLimit),
		APIKeyEnv:    manifestLLMScalar(initRaw, "api_key_env", kanbanLLMIdentityLimit),
		Streaming:    manifestLLMScalar(initRaw, "streaming", kanbanLLMSettingLimit),
		ContextLimit: resolvedKanbanLLMScalar(agentRaw, initRaw, "context_limit", kanbanLLMSettingLimit),
	}
}

func mergeKanbanAgentRaw(agentRaw, initRaw map[string]any) map[string]any {
	merged := make(map[string]any, len(agentRaw)+len(initRaw))
	for key, value := range initRaw {
		merged[key] = value
	}
	for key, value := range agentRaw {
		merged[key] = value
	}
	return merged
}

func (m PropsModel) resolveKanbanPresetInfo(raw map[string]any) kanbanPresetInfo {
	block, _ := raw["preset"].(map[string]any)
	info := kanbanPresetInfo{}
	if block == nil {
		return info
	}
	info.DefaultRef, _ = block["default"].(string)
	info.ActiveRef, _ = block["active"].(string)
	allowed, _ := block["allowed"].([]any)
	refs := make([]string, 0, len(allowed))
	for _, entry := range allowed {
		if ref, ok := entry.(string); ok && strings.TrimSpace(ref) != "" {
			refs = append(refs, ref)
		}
	}
	if len(refs) == 0 {
		return info
	}
	keys, _ := config.ResolveKeys(m.globalDir)
	poolEligible, poolEligibleModels, poolFallback := codexPoolEligibilityFacts(m.globalDir)
	auth := preset.AuthState{
		CodexOAuthConfigured:      codexOAuthConfigured(m.globalDir),
		CodexAuthDir:              m.globalDir,
		CodexPoolEligible:         poolEligible,
		CodexPoolEligibleModels:   poolEligibleModels,
		CodexPoolFallbackEligible: poolFallback,
		ClaudeCodeAuthConfigured:  claudeCodeAuthConfigured(),
	}
	info.Allowed = preset.ResolveRefsWithAuth(refs, keys, auth)
	return info
}

func presetHealthCounts(info kanbanPresetInfo) (healthy, broken int) {
	for _, resolved := range info.Allowed {
		if resolved.Exists && resolved.HasKey {
			healthy++
		} else {
			broken++
		}
	}
	return healthy, broken
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

	agentRaw, err := fs.ReadAgentRaw(m.selectedDir)
	if err != nil {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.no_data")))
		return strings.Join(lines, "\n")
	}
	initRaw, _ := fs.ReadInitManifest(m.selectedDir)
	raw := mergeKanbanAgentRaw(agentRaw, initRaw)
	llm := resolveKanbanLLMConfig(agentRaw, initRaw)
	presetInfo := m.resolveKanbanPresetInfo(raw)

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
				if isTimestampPropField(f.key) {
					val = formatKanbanTimestamp(val)
				}
				val = valueStyle.Render(val)
			}
			lines = append(lines, "  "+labelStyle.Render(f.label+": ")+val)
		}
	}
	appendRow := func(label, value string) {
		if value != "" {
			lines = append(lines, "  "+labelStyle.Render(label+": ")+valueStyle.Render(value))
		}
	}

	lines = append(lines, "", "  "+sectionStyle.Render(i18n.T("props.section_agent_now")), "")
	renderFields([]propsField{
		{"agent_name", i18n.T("props.name")},
		{"nickname", i18n.T("props.nickname")},
		{"state", i18n.T("props.state")},
	})
	llmParts := make([]string, 0, 2)
	if llm.Model != "" {
		llmParts = append(llmParts, llm.Model)
	}
	if llm.Provider != "" {
		llmParts = append(llmParts, llm.Provider)
	}
	appendRow(i18n.T("props.llm_identity"), strings.Join(llmParts, " · "))
	appendRow(i18n.T("props.service_tier"), llm.ServiceTier)
	appendRow(i18n.T("props.thinking_effort"), llm.Thinking)
	appendRow(i18n.T("props.endpoint"), llm.Endpoint)
	appendRow(i18n.T("props.preset_active"), refDisplayName(presetInfo.ActiveRef))
	if len(presetInfo.Allowed) > 0 {
		healthy, broken := presetHealthCounts(presetInfo)
		parts := []string{i18n.TF("props.preset_health_healthy", healthy)}
		if broken > 0 {
			parts = append(parts, i18n.TF("props.preset_health_broken", broken))
		}
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.preset_allowed")+": ")+valueStyle.Render(strings.Join(parts, " · ")))
	}

	// Context window (from cached .status.json)
	ctx := m.selectedStatus.Tokens.Context
	if ctx.WindowSize > 0 {
		pctColor := ColorAgent
		if ctx.UsagePct > 80 {
			pctColor = lipgloss.Color("#e06c75")
		} else if ctx.UsagePct > 60 {
			pctColor = lipgloss.Color("#e5c07b")
		}
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.context_usage")+": ")+lipgloss.NewStyle().Foreground(pctColor).Render(
			fmt.Sprintf("%s / %s (%.1f%%)", formatComma(int64(ctx.TotalTokens)), formatComma(int64(ctx.WindowSize)), ctx.UsagePct)))
	}

	// Current session: the complete since-molt economy tuple from .status.json.
	// The primary deliberately shows an absolute cache miss only; no default
	// cache-miss budget exists in the published status/manifest contract.
	session := m.selectedStatus.Tokens
	if session.InputTokens > 0 || session.OutputTokens > 0 || session.ThinkingTokens > 0 || session.CachedTokens > 0 || session.APICalls > 0 {
		tokens := session.InputTokens + session.OutputTokens + session.ThinkingTokens
		lines = append(lines, "", "  "+sectionStyle.Render(i18n.T("props.section_current_session")))
		if session.Estimated {
			warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#e5c07b"))
			lines = append(lines, "  "+warnStyle.Render(i18n.T("props.estimated_usage_warning")))
		}
		lines = append(lines, "")
		if session.InputTokens > 0 {
			appendRow(i18n.T("props.session_input_tokens"), formatComma(session.InputTokens))
			appendRow(i18n.T("props.session_cache_hit_rate"), formatCacheRate(session.CachedTokens, session.InputTokens))
			appendRow(i18n.T("props.session_cache_miss"), formatComma(cacheMiss(session.CachedTokens, session.InputTokens)))
		}
		appendRow(i18n.T("props.session_api_calls"), formatComma(session.APICalls))
		if session.APICalls > 0 {
			appendRow(i18n.T("props.session_tokens_per_api_call"), formatComma(avgPerCall(tokens, session.APICalls)))
		}
	}

	return strings.Join(lines, "\n")
}

// renderSoulFlowValue renders the /kanban "soul flow" line as an on/off
// status rather than a raw delay number. The enable control is the env
// opt-in (LINGTAI_SOUL_FLOW_ENABLED in the agent's env_file), not
// soul.delay. When enabled we surface soul.delay as cadence (falling back
// to a localized "default cadence" when the manifest omits it, since the
// kernel applies its own default); when disabled we say so and point the
// reader at the opt-in var and the soul-manual tool.
func (m PropsModel) renderSoulFlowValue(raw map[string]interface{}, valueStyle, labelStyle lipgloss.Style) string {
	enabled := config.SoulFlowEnabledInEnvFile(m.soulFlowEnvPath(raw))

	if !enabled {
		// disabled — localized "disabled · opt in via <var> · see soul-manual"
		return valueStyle.Render(i18n.T("props.soul_flow_disabled")) +
			labelStyle.Render(i18n.TF("props.soul_flow_disabled_hint", config.SoulFlowEnabledEnvVar))
	}

	// enabled — show cadence. soul_delay is the flattened soul.delay.
	cadence := i18n.T("props.soul_flow_cadence_default")
	if v, ok := raw["soul_delay"]; ok && v != nil {
		if s := fmt.Sprintf("%v", v); s != "" {
			cadence = i18n.TF("props.soul_flow_cadence", s)
		}
	}
	return valueStyle.Render(i18n.T("props.soul_flow_enabled")) +
		labelStyle.Render(" · "+cadence) +
		labelStyle.Render(i18n.T("props.soul_flow_manual_hint"))
}

// soulFlowEnvPath resolves the .env file that governs this agent's soul-flow
// opt-in: the agent's init.json env_file if present (the seam the kernel
// actually loads), else the global .env under m.globalDir. env_file is
// normally an absolute path, but tolerate a leading ~ just in case.
func (m PropsModel) soulFlowEnvPath(raw map[string]interface{}) string {
	ef, ok := raw["env_file"].(string)
	if !ok || ef == "" {
		return config.EnvFilePath(m.globalDir)
	}
	if strings.HasPrefix(ef, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			ef = filepath.Join(home, strings.TrimPrefix(ef, "~"))
		}
	}
	return ef
}

func (m PropsModel) renderRight(maxW int) string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)

	var lines []string

	lines = append(lines, "", "  "+sectionStyle.Render(i18n.T("props.section_network_now")), "")

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
	lines = append(lines, "  "+labelStyle.Render(i18n.T("props.network_total")+": ")+
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

	return strings.Join(lines, "\n")
}

// renderDetail preserves the full diagnostic dashboard behind Ctrl+D.
func (m PropsModel) renderDetail() string {
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)
	sectionStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
	subtleStyle := lipgloss.NewStyle().Foreground(ColorTextFaint)

	var lines []string

	agentRaw := map[string]any{}
	if loaded, err := fs.ReadAgentRaw(m.selectedDir); err == nil {
		agentRaw = loaded
	}
	initRaw := map[string]any{}
	if loaded, err := fs.ReadInitManifest(m.selectedDir); err == nil {
		initRaw = loaded
	}
	raw := mergeKanbanAgentRaw(agentRaw, initRaw)
	llm := resolveKanbanLLMConfig(agentRaw, initRaw)
	presetInfo := m.resolveKanbanPresetInfo(raw)

	appendSection := func(title string) {
		lines = append(lines, "", "  "+sectionStyle.Render(title), "")
	}
	appendDetailRow := func(label, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		lines = append(lines, "  "+labelStyle.Render(label+": ")+valueStyle.Render(value))
	}
	appendRawRow := func(key, label string) {
		v, ok := raw[key]
		if !ok || v == nil {
			return
		}
		value := fmt.Sprintf("%v", v)
		if isTimestampPropField(key) {
			value = formatKanbanTimestamp(value)
		}
		appendDetailRow(label, value)
	}

	appendSection(i18n.T("props.detail_identity_runtime"))
	appendRawRow("agent_name", i18n.T("props.name"))
	appendRawRow("nickname", i18n.T("props.nickname"))
	appendRawRow("agent_id", i18n.T("props.id"))
	appendRawRow("state", i18n.T("props.state"))
	appendRawRow("address", i18n.T("props.address"))
	appendRawRow("language", i18n.T("props.language"))
	appendRawRow("started_at", i18n.T("props.started_at"))
	appendRawRow("combo", i18n.T("props.combo"))
	lines = append(lines, "  "+labelStyle.Render(i18n.T("props.soul_flow")+": ")+m.renderSoulFlowValue(raw, valueStyle, labelStyle))
	appendRawRow("molt_count", i18n.T("props.molt_count"))
	appendRawRow("max_turns", i18n.T("props.max_turns"))
	appendRawRow("max_rpm", i18n.T("props.max_rpm"))
	appendDetailRow(i18n.T("props.detail_agent_path"), m.selectedDir)
	appendDetailRow(i18n.T("props.detail_network_path"), m.baseDir)
	if m.orchDir != "" && m.orchDir != m.selectedDir {
		appendDetailRow(i18n.T("props.detail_orchestrator_path"), m.orchDir)
	}

	appendSection(i18n.T("props.detail_llm_configuration"))
	appendDetailRow(i18n.T("props.model"), llm.Model)
	appendDetailRow(i18n.T("props.provider"), llm.Provider)
	appendDetailRow(i18n.T("props.service_tier"), llm.ServiceTier)
	appendDetailRow(i18n.T("props.thinking_effort"), llm.Thinking)
	appendDetailRow(i18n.T("props.base_url"), llm.BaseURL)
	appendDetailRow(i18n.T("props.api_compat"), llm.APICompat)
	appendDetailRow(i18n.T("props.api_key_env"), llm.APIKeyEnv)
	appendDetailRow(i18n.T("props.streaming"), llm.Streaming)
	appendDetailRow(i18n.T("props.context_limit"), llm.ContextLimit)

	appendSection(i18n.T("props.detail_presets_capabilities_admin"))
	appendDetailRow(i18n.T("props.preset_active"), refDisplayName(presetInfo.ActiveRef))
	appendDetailRow(i18n.T("props.preset_default"), refDisplayName(presetInfo.DefaultRef))
	if len(presetInfo.Allowed) > 0 {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.preset_allowed")+":"))
		for _, resolved := range presetInfo.Allowed {
			marker := lipgloss.NewStyle().Foreground(StateColor("ACTIVE")).Render("✓")
			if !resolved.Exists || !resolved.HasKey {
				marker = lipgloss.NewStyle().Foreground(lipgloss.Color("#e06c75")).Render("✗")
			}
			tag := ""
			switch resolved.Source {
			case preset.SourceTemplate:
				tag = " " + labelStyle.Render("("+i18n.T("props.preset_source_template")+")")
			case preset.SourceSaved:
				tag = " " + labelStyle.Render("("+i18n.T("props.preset_source_saved")+")")
			}
			name := resolved.Name
			if name == "" {
				name = resolved.Ref
			}
			lines = append(lines, "    "+marker+" "+valueStyle.Render(name)+tag)
		}
	}
	if caps, ok := raw["capabilities"]; ok && caps != nil {
		capsJSON, _ := json.Marshal(caps)
		capNames := fs.CapabilitiesForDisplay(fs.ParseCapabilities(capsJSON))
		if len(capNames) > 0 {
			lines = append(lines, "  "+labelStyle.Render(i18n.T("props.section_capabilities")+":"))
			wrapWidth := 74
			if m.width > 0 {
				wrapWidth = max(1, m.width-6)
			}
			wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(strings.Join(capNames, ", "))
			for _, line := range strings.Split(wrapped, "\n") {
				lines = append(lines, "    "+valueStyle.Render(line))
			}
		}
	}
	if adminMap, ok := raw["admin"].(map[string]any); ok && len(adminMap) > 0 {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.section_admin")+":"))
		keys := make([]string, 0, len(adminMap))
		for key := range adminMap {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			lines = append(lines, "    "+valueStyle.Render(fmt.Sprintf("%s: %v", key, adminMap[key])))
		}
	}

	appendSection(i18n.T("props.detail_context_detail"))
	ctx := m.selectedStatus.Tokens.Context
	if ctx.WindowSize > 0 {
		appendDetailRow(i18n.T("props.context_usage"), fmt.Sprintf("%s / %s (%.1f%%)",
			formatComma(int64(ctx.TotalTokens)), formatComma(int64(ctx.WindowSize)), ctx.UsagePct))
		appendDetailRow(i18n.T("props.context_system"), formatComma(int64(ctx.SystemTokens)))
		appendDetailRow(i18n.T("props.context_tools"), formatComma(int64(ctx.ToolsTokens)))
		appendDetailRow(i18n.T("props.context_history"), formatComma(int64(ctx.HistoryTokens)))
	}

	if m.selectedTokens.APICalls > 0 {
		appendSection(i18n.T("props.detail_agent_lifetime_usage"))
		appendDetailRow("input", formatComma(m.selectedTokens.Input))
		appendDetailRow("output", formatComma(m.selectedTokens.Output))
		appendDetailRow("thinking", formatComma(m.selectedTokens.Thinking))
		cached := formatComma(m.selectedTokens.Cached)
		if m.selectedTokens.Input > 0 {
			cached += fmt.Sprintf(" (%.1f%%)", 100.0*float64(m.selectedTokens.Cached)/float64(m.selectedTokens.Input))
		}
		appendDetailRow("cached", cached)
		appendDetailRow("api_calls", formatComma(m.selectedTokens.APICalls))
	}

	appendSection(i18n.T("props.detail_network_history_topology"))
	if m.adminStart != "" {
		appendDetailRow(i18n.T("props.network_created"), formatKanbanTimestamp(m.adminStart))
		if started, err := time.Parse(time.RFC3339, m.adminStart); err == nil {
			appendDetailRow(i18n.T("props.network_uptime"), formatDuration(time.Since(started)))
		}
	}
	lines = append(lines, "  "+labelStyle.Render(i18n.T("props.total_tokens")+":"))
	lines = append(lines, "    "+labelStyle.Render("Input:    ")+valueStyle.Render(formatComma(m.tokens.Input)))
	lines = append(lines, "    "+labelStyle.Render("Output:   ")+valueStyle.Render(formatComma(m.tokens.Output)))
	lines = append(lines, "    "+labelStyle.Render("Thinking: ")+valueStyle.Render(formatComma(m.tokens.Thinking)))
	networkCached := formatComma(m.tokens.Cached)
	if m.tokens.Input > 0 {
		networkCached += fmt.Sprintf(" (%.1f%%)", 100.0*float64(m.tokens.Cached)/float64(m.tokens.Input))
	}
	lines = append(lines, "    "+labelStyle.Render("Cached:   ")+valueStyle.Render(networkCached))
	appendDetailRow(i18n.T("props.total_api_calls"), formatComma(m.tokens.APICalls))
	if m.mailStatsAvailable {
		appendDetailRow(i18n.T("props.total_mails"), fmt.Sprintf("%d", m.network.Stats.TotalMails))
	}
	tree := m.renderTree(m.width)
	if len(tree) > 0 {
		lines = append(lines, "  "+labelStyle.Render(i18n.T("props.tree")+":"))
		lines = append(lines, tree...)
	}

	lines = append(lines, "")
	lines = append(lines, "  "+sectionStyle.Render(i18n.T("props.detail_main_tokens_by_provider")))
	lines = append(lines, "")
	lines = appendProviderRows(lines, m.detailByProvider, labelStyle, valueStyle, subtleStyle)

	lines = append(lines, "")
	daemonTokenTitle := i18n.T("props.detail_daemon_tokens_by_provider")
	if m.detailDaemonRunsTotal > m.detailDaemonRunsScanned {
		daemonTokenTitle += fmt.Sprintf(" (latest %d/%d runs)", m.detailDaemonRunsScanned, m.detailDaemonRunsTotal)
	}
	lines = append(lines, "  "+sectionStyle.Render(daemonTokenTitle))
	lines = append(lines, "")
	lines = appendProviderRows(lines, m.detailDaemonByProvider, labelStyle, valueStyle, subtleStyle)

	// Combined totals: arithmetic sum of main-provider and daemon provider/backend rows.
	combined := make(map[string]fs.TokenTotals)
	for name, t := range m.detailByProvider {
		combined[name] = t
	}
	for name, t := range m.detailDaemonByProvider {
		c := combined[name]
		c.Input += t.Input
		c.Output += t.Output
		c.Thinking += t.Thinking
		c.Cached += t.Cached
		c.APICalls += t.APICalls
		combined[name] = c
	}
	if len(combined) > 0 {
		lines = append(lines, "")
		combinedTitle := i18n.T("props.detail_combined_totals")
		if m.detailDaemonRunsTotal > m.detailDaemonRunsScanned {
			combinedTitle += " (main + recent daemon window)"
		}
		lines = append(lines, "  "+sectionStyle.Render(combinedTitle))
		lines = append(lines, "")
		var tot fs.TokenTotals
		for _, t := range combined {
			tot.Input += t.Input
			tot.Output += t.Output
			tot.Thinking += t.Thinking
			tot.Cached += t.Cached
			tot.APICalls += t.APICalls
		}
		lines = append(lines, "    "+labelStyle.Render("input + output + thinking: ")+
			valueStyle.Render(formatComma(tot.Input+tot.Output+tot.Thinking)))
		lines = append(lines, "    "+labelStyle.Render("cached:                    ")+
			valueStyle.Render(formatComma(tot.Cached)))
		lines = append(lines, "    "+labelStyle.Render("miss:                      ")+
			valueStyle.Render(formatComma(cacheMiss(tot.Cached, tot.Input))))
		lines = append(lines, "    "+labelStyle.Render("api_calls:                 ")+
			valueStyle.Render(fmt.Sprintf("%d", tot.APICalls)))
		if tot.Input > 0 {
			lines = append(lines, "    "+labelStyle.Render("cache hit rate:            ")+
				valueStyle.Render(fmt.Sprintf("%.1f%%", 100.0*float64(tot.Cached)/float64(tot.Input))))
		}
		lines = append(lines, "")
	}

	// Molt-session API/cache statistics.
	lines = appendSessionAPIStats(lines, "Current session API", m.detailCurrentSessionStats, m.detailCurrentSessionToolCalls, sectionStyle, labelStyle, valueStyle)
	lines = appendSessionAPIStats(lines, "Last session API", m.detailLastSessionStats, m.detailLastSessionToolCalls, sectionStyle, labelStyle, valueStyle)

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
	lines = append(lines, "    "+labelStyle.Render(i18n.T("props.detail_daemons_terminal")+": ")+
		valueStyle.Render(fmt.Sprintf("%d", m.detailDaemonRunsTerminal)))
	lines = append(lines, "    "+labelStyle.Render(i18n.T("props.detail_daemons_total")+": ")+
		valueStyle.Render(fmt.Sprintf("%d", m.detailDaemonCounts.Total)))
	if m.detailDaemonRunsTotal > m.detailDaemonRunsScanned {
		lines = append(lines, "    "+subtleStyle.Render(fmt.Sprintf("running state checked in latest %d runs; total is directory-wide", m.detailDaemonRunsScanned)))
	}
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

const (
	ledgerSeparatorReconstructLabel = "props.detail_recent_rebuild"
	ledgerSeparatorMoltLabel        = "props.detail_recent_molt"
)

// ledgerSeparatorLabelKeys maps each ledger row index (in a newest-first
// entries slice) to the dotted separator label(s) that should be drawn after
// that row.
//
// Three boundary sources, all rendered after the newer (earlier-in-slice) row:
//   - Reconstructed-context token-ledger rows — the first call after a context
//     reconstruction (codex_ws_delta_reason="epoch_reset"/"no_baseline" or
//     codex_transfer_mode="full"). That row is itself the low-cache
//     reconstruct call, so the boundary belongs immediately after it. Labeled
//     "context rebuilt".
//   - refreshTimes (refresh_complete event timestamps) — also labeled
//     "context rebuilt", for refreshes that left no distinguishing ledger row.
//   - moltTimes (psyche_molt event timestamps) — a separate "molt" label.
//
// addLedgerSeparatorLabel deduplicates, so a refresh timestamp and a
// full/no_baseline ledger row that land on the same boundary yield a single
// "context rebuilt" label.
func ledgerSeparatorLabelKeys(entries []fs.LedgerEntry, moltTimes, refreshTimes []time.Time) map[int][]string {
	out := map[int][]string{}
	if len(entries) < 2 {
		return out
	}
	for i := 0; i+1 < len(entries); i++ {
		if isReconstructLedgerEntry(entries[i]) {
			addLedgerSeparatorLabel(out, i, ledgerSeparatorReconstructLabel)
		}
	}

	parsed := make([]time.Time, len(entries))
	ok := make([]bool, len(entries))
	for i, e := range entries {
		if t, err := time.Parse(time.RFC3339, e.TS); err == nil {
			parsed[i] = t
			ok[i] = true
		}
	}
	markBoundaryTimes(out, parsed, ok, refreshTimes, ledgerSeparatorReconstructLabel)
	markBoundaryTimes(out, parsed, ok, moltTimes, ledgerSeparatorMoltLabel)
	return out
}

// markBoundaryTimes adds labelKey after the newest row that straddles each
// boundary time: the row at/after the boundary whose next-older row is before
// it (rows are newest-first). Out-of-window boundaries are ignored.
func markBoundaryTimes(out map[int][]string, parsed []time.Time, ok []bool, times []time.Time, labelKey string) {
	for _, r := range times {
		for i := 0; i+1 < len(parsed); i++ {
			if !ok[i] || !ok[i+1] {
				continue
			}
			if !parsed[i].Before(r) && parsed[i+1].Before(r) {
				addLedgerSeparatorLabel(out, i, labelKey)
				break
			}
		}
	}
}

func addLedgerSeparatorLabel(out map[int][]string, idx int, labelKey string) {
	for _, existing := range out[idx] {
		if existing == labelKey {
			return
		}
	}
	out[idx] = append(out[idx], labelKey)
}

// rebuildSeparatorIndexes is kept as a small test/helper compatibility wrapper
// for callers that only care whether any separator exists after a row.
func rebuildSeparatorIndexes(entries []fs.LedgerEntry, rebuilds []time.Time) map[int]bool {
	labels := ledgerSeparatorLabelKeys(entries, rebuilds, nil)
	out := make(map[int]bool, len(labels))
	for idx := range labels {
		out[idx] = true
	}
	return out
}

func isReconstructLedgerEntry(entry fs.LedgerEntry) bool {
	switch strings.ToLower(strings.TrimSpace(entry.CodexWSDeltaReason)) {
	case "epoch_reset", "no_baseline", "reconstruct", "reconstructed", "context_rebuild", "context_reconstructed":
		return true
	}
	// A full provider transfer rebuilds the whole working set, which is what a
	// /refresh forces ("codex_transfer_mode=full" + "no_baseline"). Treat any
	// full transfer as a reconstruction boundary even when the delta reason is
	// absent or unrecognized.
	if strings.EqualFold(strings.TrimSpace(entry.CodexTransferMode), "full") {
		return true
	}
	return false
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

	headerLine := fmt.Sprintf("  %-24s  %-10s  %-24s  %10s  %10s  %10s  %10s  %10s  %7s  %s",
		"time", "provider", "model", "input", "output", "thinking", "cached", "miss", "cache%", "endpoint")
	lines := []string{"  " + labelStyle.Render(headerLine)}

	// Rows that start a reconstructed context, or that straddle a fallback
	// rebuild timestamp, get a faint dotted separator drawn after them.
	separators := ledgerSeparatorLabelKeys(m.detailRecent, m.detailRebuilds, m.detailRefreshes)
	sepWidth := lipgloss.Width(headerLine)

	for i, e := range m.detailRecent {
		provider := fs.DeriveLedgerProvider(e.Endpoint, e.Model)
		model := e.Model
		if model == "" {
			model = "—"
		}
		endpoint := e.Endpoint
		if endpoint == "" {
			endpoint = "—"
		}
		line := fmt.Sprintf("  %-24s  %-10s  %-24s  %10s  %10s  %10s  %10s  %10s  %7s  %s",
			shortTS(e.TS),
			provider,
			model,
			formatComma(e.Input),
			formatComma(e.Output),
			formatComma(e.Thinking),
			formatComma(e.Cached),
			formatComma(cacheMiss(e.Cached, e.Input)),
			formatCacheRate(e.Cached, e.Input),
			endpoint,
		)
		lines = append(lines, valueStyle.Render(line))
		for _, labelKey := range separators[i] {
			lines = append(lines, rebuildSeparatorLine(sepWidth, labelKey))
		}
	}
	return lines
}

// rebuildSeparatorLine renders the faint dotted boundary marking a context
// rebuild between two adjacent ledger calls.
func rebuildSeparatorLine(width int, labelKey string) string {
	if width < 8 {
		width = 8
	}
	faint := lipgloss.NewStyle().Foreground(ColorTextFaint)
	if labelKey == "" {
		labelKey = ledgerSeparatorReconstructLabel
	}
	label := i18n.T(labelKey)
	dots := width - lipgloss.Width(label) - 1
	if dots < 3 {
		return "  " + faint.Render(strings.Repeat("┈", width))
	}
	return "  " + faint.Render(strings.Repeat("┈", dots)+" "+label)
}

// renderDaemonCallRows renders all daemon per-call ledger entries (newest
// first), each row retaining daemon handle/state/backend/model/endpoint
// dimensions without truncation.
func (m PropsModel) renderDaemonCallRows() []string {
	subtleStyle := lipgloss.NewStyle().Foreground(ColorTextFaint)
	labelStyle := lipgloss.NewStyle().Foreground(ColorTextDim)
	valueStyle := lipgloss.NewStyle().Foreground(ColorText)

	if len(m.detailDaemonRecent) == 0 {
		return []string{"  " + subtleStyle.Render(i18n.T("props.detail_recent_daemons_empty"))}
	}

	lines := []string{
		"  " + labelStyle.Render(fmt.Sprintf("%-24s  %-10s  %-8s  %-10s  %-24s  %10s  %10s  %10s  %10s  %10s  %7s  %s",
			"time", "daemon", "state", "backend", "model", "input", "output", "thinking", "cached", "miss", "cache%", "endpoint")),
	}
	for _, e := range m.detailDaemonRecent {
		backend := e.Backend
		if backend == "" {
			backend = "—"
		}
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
		state := e.State
		if state == "" {
			state = "—"
		}
		line := fmt.Sprintf("  %-24s  %-10s  %-8s  %-10s  %-24s  %10s  %10s  %10s  %10s  %10s  %7s  %s",
			shortTS(e.TS),
			handle,
			state,
			backend,
			model,
			formatComma(e.Input),
			formatComma(e.Output),
			formatComma(e.Thinking),
			formatComma(e.Cached),
			formatComma(cacheMiss(e.Cached, e.Input)),
			formatCacheRate(e.Cached, e.Input),
			endpoint,
		)
		lines = append(lines, valueStyle.Render(line))
	}
	return lines
}

func avgPerCall(tokens, calls int64) int64 {
	if calls <= 0 {
		return 0
	}
	return (tokens + calls/2) / calls
}

func formatCacheRate(cached, input int64) string {
	if input <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", 100.0*float64(cached)/float64(input))
}

// cacheMiss is the absolute cache-miss token count for a ledger row: the input
// tokens NOT served from cache. input here is the true total input (raw +
// cache_read + cache_write, normalised per adapter), and cached is the
// cache_read portion, so the miss complement is input - cached. Clamped at 0 so
// rows with a missing/zero input (older ledger lines) never report a negative.
func cacheMiss(cached, input int64) int64 {
	miss := input - cached
	if miss < 0 {
		return 0
	}
	return miss
}

func isTimestampPropField(key string) bool {
	switch key {
	case "started_at", "created_at", "updated_at":
		return true
	default:
		return strings.HasSuffix(key, "_at") || strings.Contains(key, "timestamp")
	}
}

// formatKanbanTimestamp renders parseable timestamps in local time with an
// explicit UTC offset marker (for example, 2026-06-19 20:14 U-7:00).
// Non-parseable legacy strings keep the old compact trimming behavior.
func formatKanbanTimestamp(ts string) string {
	t, err := time.Parse(time.RFC3339Nano, ts)
	if err != nil {
		if len(ts) > 16 {
			return ts[:16]
		}
		return ts
	}
	local := t.Local()
	return local.Format("2006-01-02 15:04") + " " + utcOffsetLabel(local)
}

func utcOffsetLabel(t time.Time) string {
	_, offset := t.Zone()
	sign := "+"
	if offset < 0 {
		sign = "-"
		offset = -offset
	}
	hours := offset / 3600
	minutes := (offset % 3600) / 60
	return fmt.Sprintf("U%s%d:%02d", sign, hours, minutes)
}

// shortTS renders token-ledger timestamps for compact /kanban tables.
func shortTS(ts string) string {
	return formatKanbanTimestamp(ts)
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

// appendProviderRows renders a per-provider/backend token usage table for the
// given byProvider map. Returns the lines slice with rows appended. Used for
// both main-agent and daemon provider/backend breakdowns.
func appendProviderRows(lines []string, byProvider map[string]fs.TokenTotals, labelStyle, valueStyle, subtleStyle lipgloss.Style) []string {
	// Compute total spend across providers for the share bar.
	var grandSpend int64
	for _, t := range byProvider {
		grandSpend += t.Input + t.Output + t.Thinking
	}

	// Stable order: highest spend first.
	type provLine struct {
		name  string
		t     fs.TokenTotals
		spend int64
	}
	var rows []provLine
	for name, t := range byProvider {
		rows = append(rows, provLine{name, t, t.Input + t.Output + t.Thinking})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].spend != rows[j].spend {
			return rows[i].spend > rows[j].spend
		}
		return rows[i].name < rows[j].name
	})

	if len(rows) == 0 {
		return append(lines, "  "+subtleStyle.Render(i18n.T("props.detail_no_tokens")))
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
			labelStyle.Render("    miss:      ")+valueStyle.Render(formatComma(cacheMiss(r.t.Cached, r.t.Input)))+
			labelStyle.Render(hitStr))
		lines = append(lines, "")
	}
	return lines
}

func appendSessionAPIStats(lines []string, title string, stats fs.SessionTokenStats, toolCalls int64, sectionStyle, labelStyle, valueStyle lipgloss.Style) []string {
	if stats.APICalls <= 0 {
		return lines
	}
	tokens := stats.Input + stats.Output + stats.Thinking
	lines = append(lines, "  "+sectionStyle.Render(title))
	lines = append(lines, "")
	lines = append(lines, "    "+labelStyle.Render("api_calls:                 ")+
		valueStyle.Render(fmt.Sprintf("%d", stats.APICalls)))
	lines = append(lines, "    "+labelStyle.Render("tool_calls:                ")+
		valueStyle.Render(fmt.Sprintf("%d", toolCalls)))
	lines = append(lines, "    "+labelStyle.Render("tokens:                    ")+
		valueStyle.Render(formatComma(tokens)))
	lines = append(lines, "    "+labelStyle.Render("input / output / thinking: ")+
		valueStyle.Render(fmt.Sprintf("%s / %s / %s", formatComma(stats.Input), formatComma(stats.Output), formatComma(stats.Thinking))))
	lines = append(lines, "    "+labelStyle.Render("cached / missed:           ")+
		valueStyle.Render(fmt.Sprintf("%s / %s", formatComma(stats.Cached), formatComma(cacheMiss(stats.Cached, stats.Input)))))
	lines = append(lines, "    "+labelStyle.Render("cache hit rate:            ")+
		valueStyle.Render(formatCacheRate(stats.Cached, stats.Input)))
	lines = append(lines, "    "+labelStyle.Render("tokens/api_call:           ")+
		valueStyle.Render(formatComma(avgPerCall(tokens, stats.APICalls))))
	lines = append(lines, "    "+labelStyle.Render("tool_calls/api_call:       ")+
		valueStyle.Render(fmt.Sprintf("%.2f", float64(toolCalls)/float64(stats.APICalls))))
	if stats.HasCodexTransferMode {
		lines = append(lines, "    "+labelStyle.Render("transfer full / incremental: ")+
			valueStyle.Render(fmt.Sprintf("%d / %d", stats.CodexFull, stats.CodexIncremental)))
	}
	lines = append(lines, "")
	return lines
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
