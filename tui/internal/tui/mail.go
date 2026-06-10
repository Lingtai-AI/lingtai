package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// unlimitedPageSize is the effective page size when the user selects "unlimited".
const unlimitedPageSize = 999999

const (
	mailInputMinMaxHeight    = 3
	mailInputHardMaxHeight   = 14
	mailInputViewportReserve = 8
)

// ChatMessage represents a single message in the chat stream.
type ChatMessage struct {
	From        string
	To          string
	Subject     string
	Body        string
	Timestamp   string
	IsFromMe    bool                 // human sent this
	IsFromOrch  bool                 // orchestrator (主我) sent this
	Type        string               // "mail", "thinking", "diary", "insight"
	Attachments []string             // file paths attached to the message
	Question    string               // question text (for /btw insight events)
	Dismissed   bool                 // true after user presses Esc; only show in verbose
	Delivered   bool                 // for Type=="mail" && IsFromMe: true if recipient picked up
	Sources     []string             // for Type=="notification": source keys (email, soul, system, ...)
	Source      string               // for Type=="aed": subtype ("attempt" | "exhausted" | "timeout")
	Meta        *fs.NotificationMeta // for Type=="notification": kernel vital signs at injection time (issue #40)
	ApiCallID   string               // for tool_call/tool_result: LLM API round-trip grouping id
}

// ViewChangeMsg requests the app to switch views.
type ViewChangeMsg struct {
	View string
}

type pulseTickMsg time.Time

func pulseTick() tea.Cmd {
	return tea.Every(250*time.Millisecond, func(t time.Time) tea.Msg { return pulseTickMsg(t) })
}

type mailRefreshMsg struct {
	cache        fs.MailCache // incrementally updated cache
	alive        bool
	state        string // active, idle, stuck, asleep, suspended, or ""
	activity     fs.NetworkActivity
	orchName     string // agent name from .agent.json (may change at runtime)
	orchNickname string // nickname from .agent.json
}
type tickMsg time.Time

// EditorDoneMsg carries the final text from the external editor.
type EditorDoneMsg struct {
	Text string
}

func tickEvery(d time.Duration) tea.Cmd {
	return tea.Every(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// MailModel is the main chat view — a single chronological stream.
// verboseLevel controls what events.jsonl entries are shown
type verboseLevel int

const (
	verboseOff      verboseLevel = iota // normal: mail only
	verboseThinking                     // ctrl+o cycle: mail + soul (thinking, diary, text_input, text_output)
	verboseExtended                     // ctrl+o cycle: everything (+ tool_call, tool_result)
)

// spinnerFrames is a star-burst spinner shown flanking the thinking quote.
var spinnerFrames = []string{"✶", "✸", "✹", "✺", "✹", "✸"}

// thinkingQuotes are short phrases shown rotating in the header while thinking.
// Chinese: segments from the three Bodhi verses (菩提偈).
// English: Buddhist concepts and sutric phrases.
// Classical Chinese: same as Chinese (shared literary tradition).
var thinkingQuotesMap = map[string][]string{
	"zh": {
		"菩提本无树", "明镜亦非台", "佛性常清净", "何处有尘埃",
		"身是菩提树", "心为明镜台", "明镜本清净", "何处染尘埃",
		"菩提本无树", "明镜亦非台", "本来无一物", "何处惹尘埃",
	},
	"wen": {
		"菩提本无树", "明镜亦非台", "佛性常清净", "何处有尘埃",
		"身是菩提树", "心为明镜台", "明镜本清净", "何处染尘埃",
		"菩提本无树", "明镜亦非台", "本来无一物", "何处惹尘埃",
	},
	"en": {
		"Cogitating", "Meditating", "Contemplating", "Deliberating", "Ruminating",
		"Perceiving", "Discerning", "Reasoning", "Examining", "Reflecting",
	},
}

type MailModel struct {
	humanDir          string
	humanAddr         string
	orchestrator      string // 本我 directory path (full path under .lingtai/)
	orchAddr          string // 本我 address (from .agent.json)
	orchName          string // 本我 agent name (true name)
	orchNickname      string // 本我 nickname (display name override)
	baseDir           string // .lingtai/ directory
	verbose           verboseLevel
	messages          []ChatMessage // derived from cache on each refresh
	cache             fs.MailCache  // incremental mail cache
	pageSize          int           // max messages shown (from settings)
	loadedExtra       int           // additional older messages loaded via ctrl+u
	viewport          viewport.Model
	input             InputModel
	palette           PaletteModel
	width             int
	height            int
	ready             bool
	pollRate          time.Duration // refresh interval
	orchAlive         bool
	orchState         string // agent state from .agent.json
	networkActivity   fs.NetworkActivity
	statusFlash       string    // transient status message shown in status bar
	statusExpiry      time.Time // when to clear the flash
	lastInputLines    int
	lastPaletteLines  int
	lastBannerLines   int
	pendingMessage    string           // full text from editor, sent on Enter
	globalDir         string           // ~/.lingtai-tui/
	wasActive         bool             // true if previous refresh was ACTIVE
	quoteIdx          int              // which quote to show (advances on each ACTIVE transition)
	pulseTick         int              // pulse animation counter while ACTIVE
	inquiryState      string           // "", "sent", "taken" — tracks /btw lifecycle
	insightPending    bool             // true when waiting for 5s insight delay
	insightAt         time.Time        // when to fire the auto-insight
	dismissedInsights map[string]bool  // dismissed insight timestamps
	showEditorWarn    bool             // one-time vim warning overlay
	editorWarnText    string           // text to pass to editor after warning
	insightsEnabled   bool             // from settings — show insight events
	toolCallTruncate  int              // from settings — max chars per tool line (0 = no truncation)
	sessionCache      *fs.SessionCache // append-only session log
}

func NewMailModel(humanDir, humanAddr, baseDir, orchDir, orchName string, pageSize int, globalDir, lang string, insights bool, toolCallTruncate int) MailModel {
	input := NewInputModel(humanDir)
	input.textarea.Focus()
	palette := NewPaletteModel()
	// Resolve orchestrator address from .agent.json
	orchAddr := orchDir
	if orchDir != "" {
		if node, err := fs.ReadAgent(orchDir); err == nil && node.Address != "" {
			orchAddr = node.Address
		}
	}
	if pageSize <= 0 {
		pageSize = unlimitedPageSize
	}
	m := MailModel{
		humanDir:          humanDir,
		humanAddr:         humanAddr,
		baseDir:           baseDir,
		orchestrator:      orchDir,
		orchAddr:          orchAddr,
		orchName:          orchName,
		input:             input,
		palette:           palette,
		pollRate:          1 * time.Second,
		cache:             fs.NewMailCache(humanDir),
		pageSize:          pageSize,
		globalDir:         globalDir,
		quoteIdx:          -1,
		insightsEnabled:   insights,
		toolCallTruncate:  toolCallTruncate,
		dismissedInsights: make(map[string]bool),
		sessionCache:      fs.NewSessionCache(humanDir, filepath.Dir(baseDir)),
	}
	// Refresh mail cache before session rebuild so mail entries are included.
	m.cache = m.cache.Refresh()
	// Always rebuild session.jsonl from authoritative sources on launch. This is
	// cheap (ms-scale) and avoids the entire class of dedup bugs that come from
	// trying to patch an existing file across restarts.
	m.sessionCache.RebuildFromSources(m.cache, humanAddr, orchDir, m.orchDisplayName())
	return m
}

func adaptiveInputMaxHeight(windowHeight int) int {
	maxHeight := windowHeight / 3
	if maxHeight < mailInputMinMaxHeight {
		maxHeight = mailInputMinMaxHeight
	}
	if maxHeight > mailInputHardMaxHeight {
		maxHeight = mailInputHardMaxHeight
	}
	if reserveCap := windowHeight - mailInputViewportReserve; reserveCap < maxHeight {
		maxHeight = reserveCap
	}
	if maxHeight < 1 {
		maxHeight = 1
	}
	return maxHeight
}

func (m *MailModel) updateInputMaxHeight() {
	if m.height <= 0 {
		m.input.SetMaxHeight(defaultInputMaxHeight)
		return
	}
	m.input.SetMaxHeight(adaptiveInputMaxHeight(m.height))
}

// syncViewportHeight recalculates viewport height from current input/palette/banner size.
// Returns true if the height actually changed.
func (m *MailModel) syncViewportHeight() bool {
	if !m.ready {
		return false
	}
	m.updateInputMaxHeight()
	inputLines := m.input.LineCount()
	paletteLines := 0
	if m.input.IsPaletteActive() {
		paletteLines = m.palette.LineCount()
	}
	bannerLines := m.bannerLineCount()
	if inputLines == m.lastInputLines && paletteLines == m.lastPaletteLines && bannerLines == m.lastBannerLines {
		return false
	}
	m.lastInputLines = inputLines
	m.lastPaletteLines = paletteLines
	m.lastBannerLines = bannerLines
	// Layout: header(2) + topBanner(0-1) + viewport + bottomBanner(0-1) + sep(1) + palette(N) + input(N) + border(1) + status(1)
	footerHeight := 1 + paletteLines + inputLines + 1 + 1
	vpHeight := m.height - 2 - bannerLines - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	m.viewport.SetHeight(vpHeight)
	return true
}

func (m *MailModel) inputRegionBounds() (start, end int) {
	if !m.ready {
		return -1, -1
	}
	paletteLines := 0
	if m.input.IsPaletteActive() {
		paletteLines = m.palette.LineCount()
	}
	topBannerLines := 0
	if m.hasMoreOlder() {
		topBannerLines = 1
	}
	bottomBannerLines := 0
	if m.loadedExtra > 0 {
		bottomBannerLines = 1
	}
	start = 2 + topBannerLines + m.viewport.Height() + bottomBannerLines + 1 + paletteLines
	end = start + m.input.LineCount() + 1 // input rows plus border line
	return start, end
}

func (m *MailModel) mouseInInputRegion(msg tea.MouseWheelMsg) bool {
	start, end := m.inputRegionBounds()
	return start >= 0 && msg.Y >= start && msg.Y < end
}

func (m *MailModel) scrollInputByWheel(msg tea.MouseWheelMsg) bool {
	switch msg.Button {
	case tea.MouseWheelUp:
		m.input.PageUp()
		return true
	case tea.MouseWheelDown:
		m.input.PageDown()
		return true
	}
	return false
}

// bannerLineCount returns the total lines reserved for top and bottom banners.
func (m *MailModel) bannerLineCount() int {
	n := 0
	if m.hasMoreOlder() {
		n++ // top banner
	}
	if m.loadedExtra > 0 {
		n++ // bottom banner (reserved when expanded)
	}
	return n
}

// hasMoreOlder returns true when there are messages beyond the visible window.
func (m *MailModel) hasMoreOlder() bool {
	return len(m.messages) > m.pageSize+m.loadedExtra
}

// olderCount returns how many messages are hidden above the visible window.
func (m *MailModel) olderCount() int {
	hidden := len(m.messages) - m.pageSize - m.loadedExtra
	if hidden < 0 {
		return 0
	}
	return hidden
}

// visibleMessages returns the tail of m.messages limited by pageSize + loadedExtra.
func (m *MailModel) visibleMessages() []ChatMessage {
	limit := m.pageSize + m.loadedExtra
	if limit >= len(m.messages) {
		return m.messages
	}
	return m.messages[len(m.messages)-limit:]
}

func (m MailModel) refreshMail() tea.Msg {
	// Refresh human location (no-op if cache is <1h old)
	go fs.UpdateHumanLocation(m.humanDir)

	// Incremental cache refresh — only reads new messages from disk
	cache := m.cache.Refresh()

	alive := m.orchestrator != "" && fs.IsAlive(m.orchestrator, 3.0)
	var activity fs.NetworkActivity
	if m.baseDir != "" {
		if a, err := fs.ComputeNetworkActivity(m.baseDir); err == nil {
			activity = a
		}
	}
	state := ""
	orchName := m.orchName
	orchNickname := ""
	if m.orchestrator != "" {
		if node, err := fs.ReadAgent(m.orchestrator); err == nil {
			state = node.State
			if node.AgentName != "" {
				orchName = node.AgentName
			}
			orchNickname = node.Nickname
		}
	}
	if !alive {
		if fs.HasRefreshTaken(m.orchestrator) {
			state = "refreshing"
		} else {
			state = "suspended"
		}
	}
	return mailRefreshMsg{cache: cache, alive: alive, state: state, activity: activity, orchName: orchName, orchNickname: orchNickname}
}

// orchDisplayName returns the nickname if set, otherwise the agent name.
func (m MailModel) orchDisplayName() string {
	if m.orchNickname != "" {
		return m.orchNickname
	}
	return m.orchName
}

// buildMessages refreshes the session cache from all sources, then builds
// the display message list filtered by verbose level and insights settings.
// Delivered flags are overlaid from the live MailCache so outbox→sent
// transitions update the render without requiring session.jsonl rewrites.
func (m *MailModel) buildMessages() {
	// Ingest new entries from all sources into session.jsonl.
	m.sessionCache.Refresh(m.cache, m.humanAddr, m.orchestrator, m.orchDisplayName())

	// Build a timestamp → Delivered overlay from the live cache. Mail entries
	// use ReceivedAt as their session Ts, so this matching is stable.
	deliveredByTs := make(map[string]bool, len(m.cache.Messages))
	for _, mm := range m.cache.Messages {
		deliveredByTs[mm.ReceivedAt] = mm.Delivered
	}

	// Build filtered view from the session cache.
	allEntries := m.sessionCache.Entries()
	chatMsgs := make([]ChatMessage, 0, len(allEntries))

	currentApiCallID := ""
	derivedApiCallSeq := 0
	for _, e := range allEntries {
		switch e.Type {
		case "llm_response":
			if e.ApiCallID != "" {
				currentApiCallID = e.ApiCallID
			} else {
				derivedApiCallSeq++
				currentApiCallID = fmt.Sprintf("derived:%d:%s", derivedApiCallSeq, e.Ts)
			}
		case "llm_call":
			currentApiCallID = ""
		case "tool_call", "tool_result":
			if e.ApiCallID == "" {
				e.ApiCallID = currentApiCallID
			}
		}
		if !m.shouldShow(e) {
			continue
		}
		cm := sessionEntryToChatMessage(e, m.humanAddr)
		// Overlay fresh Delivered from the live cache (only for mail entries).
		if e.Type == "mail" {
			if d, ok := deliveredByTs[e.Ts]; ok {
				cm.Delivered = d
			} else {
				cm.Delivered = true
			}
		}
		chatMsgs = append(chatMsgs, cm)
	}

	// Restore dismissed state for insights.
	for i := range chatMsgs {
		if chatMsgs[i].Type == "insight" && m.dismissedInsights[chatMsgs[i].Timestamp] {
			chatMsgs[i].Dismissed = true
		}
	}
	m.messages = chatMsgs
}

// shouldShow returns whether a session entry should be displayed given the
// current verbose level and insights settings.
func (m *MailModel) shouldShow(e fs.SessionEntry) bool {
	switch e.Type {
	case "mail":
		return true
	case "thinking", "diary", "text_input", "text_output", "soul_flow", "notification", "aed":
		return m.verbose >= verboseThinking
	case "tool_call", "tool_result":
		return m.verbose >= verboseExtended
	case "llm_call", "llm_response":
		// Hidden boundary markers used to derive tool-call grouping for older
		// events that predate explicit api_call_id on tool events.
		return false
	case "insight":
		// Human /btw inquiries (source "human") are always shown.
		if e.Source == "human" {
			return true
		}
		// Auto-insight events and other insight sources are gated by insightsEnabled.
		return m.insightsEnabled
	}
	return false
}

// formatNotificationMetaFooter renders the kernel's per-injection vital
// signs (issue #40) as a single compact line: "ctx 14.7% · stamina 9h58m
// · 21:10 PDT · seq 2". Returns "" when meta is nil (older events
// pre-dating the kernel emitter change) or carries only sentinel values.
//
// Each fragment is independently gated: a sentinel field is silently
// dropped rather than rendered as "-1.0%" or "0s". When all fragments
// drop, the function returns "" so the caller writes no footer line.
func formatNotificationMetaFooter(meta *fs.NotificationMeta) string {
	if meta == nil {
		return ""
	}
	var parts []string
	if meta.Context != nil && meta.Context.Usage >= 0 {
		parts = append(parts, fmt.Sprintf("ctx %.1f%%", meta.Context.Usage*100))
	}
	if meta.StaminaLeftSeconds > 0 {
		parts = append(parts, "stamina "+formatStaminaShort(meta.StaminaLeftSeconds))
	}
	if meta.CurrentTime != "" {
		if short := formatCurrentTimeShort(meta.CurrentTime); short != "" {
			parts = append(parts, short)
		}
	}
	if meta.InjectionSeq > 0 {
		parts = append(parts, fmt.Sprintf("seq %d", meta.InjectionSeq))
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, " · ")
}

// formatStaminaShort renders seconds as "9h58m" / "12m" / "45s".
func formatStaminaShort(seconds float64) string {
	s := int(seconds)
	if s < 60 {
		return fmt.Sprintf("%ds", s)
	}
	if s < 3600 {
		return fmt.Sprintf("%dm", s/60)
	}
	return fmt.Sprintf("%dh%02dm", s/3600, (s%3600)/60)
}

// formatCurrentTimeShort renders an ISO-8601 timestamp as "HH:MM TZ"
// (e.g. "21:10 PDT"). Returns "" when parsing fails so the footer
// drops the field rather than showing the raw ISO string.
func formatCurrentTimeShort(iso string) string {
	t, err := time.Parse(time.RFC3339, iso)
	if err != nil {
		return ""
	}
	return t.Format("15:04 MST")
}

// sessionEntryToChatMessage converts a SessionEntry to a ChatMessage for rendering.
func sessionEntryToChatMessage(e fs.SessionEntry, humanAddr string) ChatMessage {
	cm := ChatMessage{
		From:        e.From,
		To:          e.To,
		Subject:     e.Subject,
		Body:        e.Body,
		Timestamp:   e.Ts,
		Type:        e.Type,
		Attachments: e.Attachments,
		Question:    e.Question,
		Delivered:   e.Delivered,
		Sources:     e.Sources,
		Source:      e.Source,
		Meta:        e.Meta,
		ApiCallID:   e.ApiCallID,
	}
	if e.Type == "mail" {
		cm.IsFromMe = e.From == "human"
		cm.IsFromOrch = !cm.IsFromMe
	}
	return cm
}

func (m MailModel) Init() tea.Cmd {
	return tea.Batch(
		m.input.Init(),
		m.refreshMail,
		tickEvery(m.pollRate),
		pulseTick(),
	)
}

func (m MailModel) Update(msg tea.Msg) (MailModel, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.MouseWheelMsg:
		if m.ready && m.mouseInInputRegion(msg) && m.scrollInputByWheel(msg) {
			m.syncViewportHeight()
			return m, nil
		}
		// Forward scroll wheel events outside the input box to the chat viewport.
		if m.ready {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.SetWidth(msg.Width)
		m.updateInputMaxHeight()
		if !m.ready {
			inputLines := m.input.LineCount()
			// sep(1) + input(N) + border(1) + status(1)
			footerHeight := 1 + inputLines + 1 + 1
			vpHeight := msg.Height - 2 - footerHeight
			if vpHeight < 1 {
				vpHeight = 1
			}
			m.viewport = viewport.New()
			m.viewport.SetWidth(msg.Width)
			m.viewport.SetHeight(vpHeight)
			m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
			m.lastInputLines = inputLines
			m.ready = true
		} else {
			m.viewport.SetWidth(msg.Width)
			m.lastInputLines = -1 // force recalculate
			m.syncViewportHeight()
			// Re-render content at new width so text wraps correctly.
			atBottom := m.viewport.AtBottom()
			m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
			if atBottom {
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case mailRefreshMsg:
		m.cache = msg.cache
		m.orchAlive = msg.alive
		m.orchState = msg.state
		m.networkActivity = msg.activity
		if msg.orchName != "" {
			m.orchName = msg.orchName
		}
		m.orchNickname = msg.orchNickname
		isActive := strings.EqualFold(m.orchState, "ACTIVE")
		isIdle := strings.EqualFold(m.orchState, "IDLE")
		if isActive && !m.wasActive {
			// Just became active — advance to next quote, reset pulse
			m.quoteIdx++
			m.pulseTick = 0
			m.insightPending = false
		}
		insightDone := fileExists(filepath.Join(m.baseDir, ".tui-asset", ".insight.done"))
		if isIdle && m.wasActive && !m.insightPending && !insightDone && m.insightsEnabled {
			// Just became idle — schedule auto-insight in 5s
			m.insightPending = true
			m.insightAt = time.Now().Add(5 * time.Second)
		}
		if m.insightPending && time.Now().After(m.insightAt) {
			m.insightPending = false
			if m.orchestrator != "" && isIdle {
				question := i18n.T("insight.auto_question")
				fs.WriteInquiry(m.orchestrator, "insight", question)
				// Write sentinel to prevent re-firing
				os.WriteFile(filepath.Join(m.baseDir, ".tui-asset", ".insight.done"), []byte(""), 0o644)
			}
		}
		m.wasActive = isActive
		m.buildMessages()
		// Track /btw inquiry lifecycle
		if m.orchestrator != "" {
			inquiryExists := fileExists(filepath.Join(m.orchestrator, ".inquiry"))
			takenExists := fileExists(filepath.Join(m.orchestrator, ".inquiry.taken"))
			switch {
			case inquiryExists:
				m.inquiryState = "sent"
			case takenExists:
				m.inquiryState = "taken"
			default:
				m.inquiryState = ""
			}
		}
		if m.ready {
			atBottom := m.viewport.AtBottom()
			m.syncViewportHeight()
			m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
			if atBottom {
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case pulseTickMsg:
		if strings.EqualFold(m.orchState, "ACTIVE") {
			m.pulseTick++
		}
		return m, pulseTick()

	case tickMsg:
		return m, tea.Batch(m.refreshMail, tickEvery(m.pollRate))

	case SendMsg:
		var text string
		if m.pendingMessage != "" {
			text = m.pendingMessage
			m.pendingMessage = ""
		} else {
			text = m.input.Value()
		}
		if text == "" {
			return m, nil
		}
		// If text starts with /, treat as slash command
		if len(text) > 1 && text[0] == '/' {
			parts := strings.SplitN(text[1:], " ", 2)
			cmd := parts[0]
			args := ""
			if len(parts) > 1 {
				args = strings.TrimSpace(parts[1])
			}
			m.input.Reset()
			m.syncViewportHeight()
			return m, func() tea.Msg { return PaletteSelectMsg{Command: cmd, Args: args} }
		}
		if m.orchestrator != "" {
			fs.WriteMail(m.orchestrator, m.humanDir, m.humanAddr, m.orchAddr, "", text)
			// Human sent a real message — allow new insight after next idle
			os.Remove(filepath.Join(m.baseDir, ".tui-asset", ".insight.done"))
			m.input.Reset()
			m.syncViewportHeight()
			return m, m.refreshMail
		}
		return m, nil

	case OpenEditorMsg:
		// Show editor intro page before launching
		m.showEditorWarn = true
		m.editorWarnText = msg.Text
		return m, nil

	case EditorDoneMsg:
		m.pendingMessage = msg.Text
		m.input.SetValue(msg.Text)
		m.syncViewportHeight()
		m.maybeShowEditorHint()
		// Refresh viewport and force a full repaint after the terminal returns from
		// the external editor; editors such as vim can leave the alt screen visually
		// stale until Bubble Tea draws a clean frame.
		return m, tea.Batch(m.refreshMail, tea.ClearScreen)

	case PaletteSelectMsg:
		m.input.Reset()
		m.syncViewportHeight()
		// Forward to app
		return m, func() tea.Msg { return PaletteSelectMsg{Command: msg.Command} }

	case tea.KeyPressMsg:
		// Editor warning overlay — Enter proceeds, Esc cancels
		if m.showEditorWarn {
			switch msg.String() {
			case "enter":
				m.showEditorWarn = false
				return m, m.launchEditor(m.editorWarnText)
			case "esc", "ctrl+c":
				m.showEditorWarn = false
				return m, nil
			}
			return m, nil
		}

		// If palette is active, route to palette
		if m.input.IsPaletteActive() {
			switch msg.String() {
			case "enter":
				// If input has args (space after /cmd), parse as command+args
				val := m.input.Value()
				if strings.Contains(val, " ") {
					parts := strings.SplitN(val[1:], " ", 2)
					cmd := parts[0]
					args := ""
					if len(parts) > 1 {
						args = strings.TrimSpace(parts[1])
					}
					m.input.Reset()
					return m, func() tea.Msg { return PaletteSelectMsg{Command: cmd, Args: args} }
				}
				// No args — select from palette
				m.input.Reset()
				m.syncViewportHeight()
				var cmd tea.Cmd
				m.palette, cmd = m.palette.Update(msg)
				return m, cmd
			case "up", "down":
				var cmd tea.Cmd
				m.palette, cmd = m.palette.Update(msg)
				return m, cmd
			case "esc":
				m.input.Reset()
				m.syncViewportHeight()
				return m, nil
			default:
				// Forward typing to input, then update palette filter
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.syncViewportHeight()
				m.maybeShowEditorHint()
				// Extract filter from input (text after "/")
				val := m.input.Value()
				if len(val) > 1 {
					m.palette.SetFilter(val[1:])
				} else {
					m.palette.SetFilter("")
				}
				return m, cmd
			}
		}

		switch msg.String() {
		case "ctrl+o":
			// Cycle: normal → thinking → extended → normal
			switch m.verbose {
			case verboseOff:
				m.verbose = verboseThinking
			case verboseThinking:
				m.verbose = verboseExtended
			case verboseExtended:
				m.verbose = verboseOff
			}
			return m, m.refreshMail

		case "ctrl+u":
			if m.ready && m.viewport.AtTop() && m.hasMoreOlder() {
				m.loadedExtra += m.pageSize
				m.syncViewportHeight()
				m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd

		case "ctrl+d":
			if m.ready && m.viewport.AtBottom() && m.loadedExtra > 0 {
				m.loadedExtra = 0
				m.syncViewportHeight()
				m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
				m.viewport.GotoBottom()
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd

		case "esc":
			// Dismiss all visible insights
			changed := false
			for _, msg := range m.messages {
				if msg.Type == "insight" && !msg.Dismissed {
					if m.dismissedInsights == nil {
						m.dismissedInsights = make(map[string]bool)
					}
					m.dismissedInsights[msg.Timestamp] = true
					changed = true
				}
			}
			if changed {
				m.buildMessages()
				if m.ready {
					m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
					m.viewport.GotoBottom()
				}
			}
			return m, nil

		case "pgup", "pgdown":
			if msg.String() == "pgup" && m.input.PageUp() {
				return m, nil
			}
			if msg.String() == "pgdown" && m.input.PageDown() {
				return m, nil
			}
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

		// If input is focused, forward keys to input
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		if m.syncViewportHeight() && m.viewport.AtBottom() {
			m.viewport.GotoBottom()
		}
		m.maybeShowEditorHint()
		// Check if slash was typed
		if m.input.IsPaletteActive() {
			val := m.input.Value()
			if len(val) > 1 {
				m.palette.SetFilter(val[1:])
			} else {
				m.palette.SetFilter("")
			}
		}
		return m, cmd
	}

	// Forward all other messages (including textarea paste and cursor blink) to input.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	if _, ok := msg.(tea.PasteMsg); ok {
		if m.syncViewportHeight() && m.viewport.AtBottom() {
			m.viewport.GotoBottom()
		}
		m.maybeShowEditorHint()
	}
	if cmd != nil {
		cmds = append(cmds, cmd)
	}
	return m, tea.Batch(cmds...)
}

func (m MailModel) renderMessages(msgs []ChatMessage) string {
	if len(msgs) == 0 {
		return "\n" + StyleFaint.Render("  "+RuneBullet+" "+i18n.T("mail.no_messages"))
	}

	humanStyle := lipgloss.NewStyle().Foreground(ColorHuman).Bold(true)
	agentStyle := lipgloss.NewStyle().Foreground(ColorAgent).Bold(true)
	avatarStyle := lipgloss.NewStyle().Foreground(ColorIdle).Bold(true)
	systemStyle := lipgloss.NewStyle().Foreground(ColorSystem).Bold(true)
	thinkingStyle := lipgloss.NewStyle().Foreground(ColorThinking)
	toolStyle := lipgloss.NewStyle().Foreground(ColorTool)
	sepStyle := lipgloss.NewStyle().Foreground(ColorTextDim)

	var b strings.Builder
	var prevVisibleTool *ChatMessage
	for _, msg := range msgs {
		if !isToolMessageType(msg.Type) {
			prevVisibleTool = nil
		}
		switch msg.Type {
		case "thinking", "diary", "text_input", "text_output", "tool_call", "tool_result":
			wrapWidth := m.width - 6
			if wrapWidth < 20 {
				wrapWidth = 20
			}
			var evStyle lipgloss.Style
			body := msg.Body
			tsPrefix := ""
			switch msg.Type {
			case "thinking", "diary", "text_input", "text_output":
				evStyle = thinkingStyle
			default:
				if toolGroupSeparatorBefore(prevVisibleTool, msg) {
					b.WriteString("\n")
				}
				evStyle = toolStyle
				// Tool lines get a leading timestamp and honor the user's
				// per-tool-call truncation setting (0 = full content, the default).
				if ts := formatToolTimestamp(msg.Timestamp); ts != "" {
					tsPrefix = StyleFaint.Render(ts) + " "
				}
				body = truncateToolBody(body, m.toolCallTruncate)
			}
			wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(tsPrefix + "[" + msg.Type + "] " + body)
			for _, line := range strings.Split(wrapped, "\n") {
				b.WriteString(evStyle.Render("  "+RuneBullet+" "+line) + "\n")
			}
			if msg.Type == "tool_call" || msg.Type == "tool_result" {
				msgCopy := msg
				prevVisibleTool = &msgCopy
			}

		case "soul_flow":
			// Each voice in msg.Body is its own line ("[insights] ..." or
			// "[past self] ..."); render with the agent accent color so it
			// reads as the agent's own reflection rather than tool noise.
			wrapWidth := m.width - 6
			if wrapWidth < 20 {
				wrapWidth = 20
			}
			soulStyle := lipgloss.NewStyle().Foreground(ColorAgent).Italic(true)
			labelStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
			b.WriteString(labelStyle.Render("  ☵ soul flow") + "\n")
			for _, voiceLine := range strings.Split(msg.Body, "\n") {
				if voiceLine == "" {
					continue
				}
				wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(voiceLine)
				for _, line := range strings.Split(wrapped, "\n") {
					b.WriteString(soulStyle.Render("    "+line) + "\n")
				}
			}

		case "notification":
			// Kernel notification-sync rewire. Mirrors the soul_flow style
			// (same green palette) so it reads as agent inner state rather
			// than tool noise. Body is the kernel-logged summary string;
			// when Sources has >1 entry we also list them on their own
			// lines for clarity. Issue #40: when the kernel attached a
			// `meta` block (build_meta + injection_seq), render a compact
			// faint footer with the agent's vital signs at injection time.
			wrapWidth := m.width - 6
			if wrapWidth < 20 {
				wrapWidth = 20
			}
			notifStyle := lipgloss.NewStyle().Foreground(ColorAgent).Italic(true)
			labelStyle := lipgloss.NewStyle().Foreground(ColorAccent).Bold(true)
			footerStyle := notifStyle.Faint(true)
			b.WriteString(labelStyle.Render("  ✉ notifications") + "\n")
			if msg.Body != "" {
				wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(msg.Body)
				for _, line := range strings.Split(wrapped, "\n") {
					b.WriteString(notifStyle.Render("    "+line) + "\n")
				}
			}
			if len(msg.Sources) > 1 {
				for _, src := range msg.Sources {
					b.WriteString(notifStyle.Render("    • "+src) + "\n")
				}
			}
			if footer := formatNotificationMetaFooter(msg.Meta); footer != "" {
				b.WriteString(footerStyle.Render("    "+footer) + "\n")
			}

		case "aed":
			// Agent error-recovery (kernel distress). Distinct orange palette
			// rather than the green soul/notification palette: AED is not
			// agent inner reflection, it's the kernel telling us the LLM
			// returned empty / errored and recovery was attempted. Subtype
			// (attempt | exhausted | timeout) is in msg.Source and inlined
			// in the header so users can scan AED storms quickly.
			wrapWidth := m.width - 6
			if wrapWidth < 20 {
				wrapWidth = 20
			}
			aedBodyStyle := lipgloss.NewStyle().Foreground(ColorTool).Italic(true)
			aedLabelStyle := lipgloss.NewStyle().Foreground(ColorTool).Bold(true)
			subtype := msg.Source
			if subtype == "" {
				subtype = "event"
			}
			b.WriteString(aedLabelStyle.Render("  ⚠ aed "+subtype) + "\n")
			if msg.Body != "" {
				wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(msg.Body)
				for _, line := range strings.Split(wrapped, "\n") {
					b.WriteString(aedBodyStyle.Render("    "+line) + "\n")
				}
			}

		case "insight":
			// Dismissed insights only show in verbose mode
			if msg.Dismissed && m.verbose == verboseOff {
				continue
			}
			wrapWidth := m.width - 6
			if wrapWidth < 20 {
				wrapWidth = 20
			}
			fullBar := m.width - 4
			barStyle := lipgloss.NewStyle().Foreground(ColorSubtle)
			labelStyle := lipgloss.NewStyle().Foreground(ColorAccent)

			// Label: "/btw › question" or "★ insight", with dismiss hint if undismissed
			var label string
			dismissHint := ""
			if !msg.Dismissed {
				dismissHint = " " + barStyle.Render(i18n.T("mail.esc_dismiss"))
			}
			if msg.Question != "" {
				label = labelStyle.Render("/btw › ") + msg.Question + dismissHint
			} else {
				label = labelStyle.Render("★ insight") + dismissHint
			}

			b.WriteString(barStyle.Render("  "+strings.Repeat("─", max(fullBar, 1))) + "\n")
			b.WriteString("  " + label + "\n")
			b.WriteString(barStyle.Render("  "+strings.Repeat("─", max(fullBar, 1))) + "\n")
			r, err := glamour.NewTermRenderer(
				glamour.WithStandardStyle(ActiveTheme().GlamourStyle),
				glamour.WithWordWrap(max(wrapWidth-2, 10)),
			)
			if err == nil {
				rendered, err := r.Render(msg.Body)
				if err == nil {
					rendered = strings.Trim(rendered, "\n")
					for _, line := range strings.Split(rendered, "\n") {
						b.WriteString("  " + line + "\n")
					}
				}
			}
			b.WriteString(barStyle.Render("  "+strings.Repeat("─", max(fullBar, 1))) + "\n")

		default: // "mail"
			if m.verbose != verboseOff {
				header := StyleFaint.Render("  "+RuneBullet+" ") +
					humanStyle.Render(msg.From) + sepStyle.Render(" → ") + sepStyle.Render(msg.To)
				if msg.Subject != "" {
					header += sepStyle.Render(" │ " + i18n.T("mail.subject_label") + " " + msg.Subject)
				}
				header += sepStyle.Render(" │ " + msg.Timestamp)
				b.WriteString(header + "\n")
			}

			var nameStyle lipgloss.Style
			if msg.IsFromMe {
				nameStyle = humanStyle
			} else if msg.From == i18n.T("mail.system_sender") {
				nameStyle = systemStyle
			} else if msg.IsFromOrch {
				nameStyle = agentStyle
			} else {
				nameStyle = avatarStyle
			}
			name := nameStyle.Render(msg.From)
			// Short timestamp (HH:MM) with optional delivering indicator.
			ts := ""
			if msg.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
					ts = StyleFaint.Render(" " + t.Local().Format("15:04"))
				}
			}
			if msg.IsFromMe && !msg.Delivered {
				// Quiet indicator: message sent to outbox but recipient hasn't picked up yet.
				ts += StyleFaint.Render(" ⏳")
			}
			// Wrap body to fit terminal width (indent 2 + name + ": ")
			prefix := fmt.Sprintf("  %s%s: ", name, ts)
			prefixWidth := lipgloss.Width(prefix)
			bodyWidth := m.width - prefixWidth
			if bodyWidth < 20 {
				bodyWidth = 20
			}
			// Render markdown for agent messages, plain wrap for user/system
			var wrappedBody string
			if !msg.IsFromMe && msg.From != i18n.T("mail.system_sender") {
				r, err := glamour.NewTermRenderer(
					glamour.WithStandardStyle(ActiveTheme().GlamourStyle),
					glamour.WithWordWrap(bodyWidth),
				)
				if err == nil {
					if rendered, rerr := r.Render(msg.Body); rerr == nil {
						wrappedBody = strings.TrimRight(rendered, "\n")
					}
				}
				if wrappedBody == "" {
					wrappedBody = lipgloss.NewStyle().Width(bodyWidth).Render(msg.Body)
				}
			} else {
				wrappedBody = lipgloss.NewStyle().Width(bodyWidth).Render(msg.Body)
			}
			// Hard-wrap any lines glamour produced wider than bodyWidth
			wrappedBody = ansi.Hardwrap(wrappedBody, bodyWidth, true)
			// Indent continuation lines to align with first line
			lines := strings.Split(wrappedBody, "\n")
			b.WriteString("\n" + prefix + lines[0] + "\n")
			indent := strings.Repeat(" ", prefixWidth)
			for _, line := range lines[1:] {
				b.WriteString(indent + line + "\n")
			}
			// Show attachment paths if present
			if len(msg.Attachments) > 0 {
				b.WriteString(indent + StyleFaint.Render("Attachments:") + "\n")
				for i, att := range msg.Attachments {
					b.WriteString(indent + StyleFaint.Render(fmt.Sprintf("  [%d] %s", i+1, att)) + "\n")
				}
			}
		}
	}
	return b.String()
}

// humanName returns the human's display name. Prefers nickname from .agent.json,
// falls back to i18n "mail.you".
func (m MailModel) humanName() string {
	if node, err := fs.ReadAgent(m.humanDir); err == nil {
		if node.Nickname != "" {
			return node.Nickname
		}
	}
	return i18n.T("mail.you")
}

func (m MailModel) networkActivityBadge() string {
	if m.networkActivity.Status == "" {
		return ""
	}
	style := lipgloss.NewStyle().Foreground(NetworkActivityColor(m.networkActivity.Status))
	return StyleFaint.Render(" · "+networkActivityShortLabel()+": ") + style.Render(networkActivityStatusLabel(m.networkActivity.Status))
}

// AddSystemMessage shows a transient status message in the status bar.
// It auto-expires after 5 seconds.
func (m *MailModel) AddSystemMessage(body string) {
	m.statusFlash = body
	m.statusExpiry = time.Now().Add(5 * time.Second)
}

func (m *MailModel) maybeShowEditorHint() {
	if strings.TrimSpace(m.input.Value()) == "" || !m.input.AtMaxHeight() {
		return
	}
	m.AddSystemMessage(i18n.T("mail.editor_hint"))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// launchEditor creates a temp file and opens $EDITOR (default: vim).
func (m MailModel) launchEditor(text string) tea.Cmd {
	tmpFile, err := os.CreateTemp("", "lingtai-input-*.txt")
	if err != nil {
		return nil
	}
	tmpFile.WriteString(text)
	tmpFile.Close()
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}
	cmd := exec.Command(editor, tmpFile.Name())
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		if err != nil {
			os.Remove(tmpFile.Name())
			return nil
		}
		content, _ := os.ReadFile(tmpFile.Name())
		os.Remove(tmpFile.Name())
		return EditorDoneMsg{Text: string(content)}
	})
}

// viewEditorWarn renders the editor confirmation overlay.
func (m MailModel) viewEditorWarn() string {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vim"
	}

	var b strings.Builder

	title := StyleTitle.Render("  " + i18n.T("editor_warn.title"))
	b.WriteString(title + "\n")
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	editorName := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent).Render(editor)
	b.WriteString("  " + i18n.TF("editor_warn.editor_is", editorName) + "\n\n")
	b.WriteString("  " + StyleFaint.Render(i18n.T("editor_warn.change_hint")) + "\n")

	b.WriteString("\n" + strings.Repeat("─", m.width) + "\n")
	enterHint := StyleAccent.Render("[Enter] ") + StyleSubtle.Render(i18n.T("editor_warn.proceed"))
	escHint := StyleAccent.Render("[Esc] ") + StyleSubtle.Render(i18n.T("editor_warn.cancel"))
	b.WriteString("  " + enterHint + "    " + escHint + "\n")

	return b.String()
}

func (m MailModel) View() string {
	if m.showEditorWarn {
		return m.viewEditorWarn()
	}
	if !m.ready {
		return "\n  " + i18n.T("app.loading")
	}

	// Build header: left = app title, center = thinking quote, right = agent [state]
	brand := i18n.T("app.brand")
	titleLeft := StyleTitle.Render("  " + brand)

	// State badge with color
	stateKey := m.orchState
	if stateKey == "" {
		stateKey = "unknown"
	}
	stateLabel := i18n.T("state." + stateKey)
	stateStyle := lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(stateKey)))
	orchNameStyle := lipgloss.NewStyle().Foreground(ColorText).Bold(true)
	titleRightBase := orchNameStyle.Render(m.orchDisplayName()) + " " + stateStyle.Render("◉ "+stateLabel)

	// Thinking indicator: fixed quote per ACTIVE session, pulsing color + spinners
	titleCenter := ""
	if strings.EqualFold(m.orchState, "ACTIVE") {
		quotes := thinkingQuotesMap[i18n.Lang()]
		if quotes == nil {
			quotes = thinkingQuotesMap["en"]
		}
		quote := quotes[m.quoteIdx%len(quotes)]
		spinner := spinnerFrames[m.pulseTick%len(spinnerFrames)]
		shades := ActiveTheme().PulseShades
		shade := lipgloss.Color(shades[m.pulseTick%len(shades)])
		style := lipgloss.NewStyle().Foreground(shade)
		titleCenter = style.Render(spinner + " " + quote + " " + spinner)
	}

	titleRight := titleRightBase
	if badge := m.networkActivityBadge(); badge != "" {
		needWidth := lipgloss.Width(titleLeft) + lipgloss.Width(titleCenter) + lipgloss.Width(titleRightBase) + lipgloss.Width(badge) + 4
		if needWidth <= m.width {
			titleRight += badge
		}
	}

	leftW := lipgloss.Width(titleLeft)
	rightW := lipgloss.Width(titleRight)
	centerW := lipgloss.Width(titleCenter)
	var titleLine string
	if titleCenter != "" {
		// Three-part layout: left ... center ... right
		gapTotal := m.width - leftW - centerW - rightW - 1
		if gapTotal > 0 {
			leftGap := gapTotal / 2
			rightGap := gapTotal - leftGap
			titleLine = titleLeft + strings.Repeat(" ", leftGap) + titleCenter + strings.Repeat(" ", rightGap) + titleRight
		} else {
			titleLine = titleLeft + " " + titleCenter + " " + titleRight
		}
	} else {
		padding := m.width - leftW - rightW - 1
		if padding > 0 {
			titleLine = titleLeft + strings.Repeat(" ", padding) + titleRight
		} else {
			titleLine = titleLeft + "  " + titleRight
		}
	}
	header := titleLine + "\n" + strings.Repeat("\u2500", m.width)

	// Build footer — "Email To: AgentName ─────────"
	toLabel := StyleFaint.Render("Email To: ") + lipgloss.NewStyle().Foreground(ColorAgent).Render(m.orchDisplayName()) + " "
	sepWidth := m.width - lipgloss.Width(toLabel)
	if sepWidth < 0 {
		sepWidth = 0
	}
	sep := toLabel + strings.Repeat("\u2500", sepWidth)
	var inputSection string
	if m.input.IsPaletteActive() {
		inputSection = m.palette.View() + "\n" + m.input.View()
	} else {
		inputSection = m.input.View()
	}

	// Status bar: left = flash or dir path, right = hints
	var leftLabel string
	if m.inquiryState == "sent" || m.inquiryState == "taken" {
		leftLabel = lipgloss.NewStyle().Foreground(ColorAccent).Render("  ◉ " + i18n.T("mail.btw_thinking"))
	} else if m.statusFlash != "" && time.Now().Before(m.statusExpiry) {
		leftLabel = lipgloss.NewStyle().Foreground(ColorAgent).Render("  ◉ " + m.statusFlash)
	} else {
		m.statusFlash = ""
		leftLabel = StyleSubtle.Render("  " + m.baseDir)
	}
	var hints string
	switch m.verbose {
	case verboseOff:
		hints = StyleSubtle.Render(i18n.T("hints.verbose")) +
			StyleFaint.Render(" "+RuneBullet+" "+i18n.T("hints.editor")+" "+RuneBullet+" "+i18n.T("hints.commands"))
	case verboseThinking:
		hints = lipgloss.NewStyle().Foreground(ColorAgent).Render(i18n.T("hints.verbose_on")) +
			StyleFaint.Render(" "+RuneBullet+" "+i18n.T("hints.editor")+" "+RuneBullet+" "+i18n.T("hints.commands"))
	case verboseExtended:
		hints = lipgloss.NewStyle().Foreground(ColorThinking).Render(i18n.T("hints.extended_on")) +
			StyleFaint.Render(" "+RuneBullet+" "+i18n.T("hints.editor")+" "+RuneBullet+" "+i18n.T("hints.commands"))
	}
	statusPad := m.width - lipgloss.Width(leftLabel) - lipgloss.Width(hints) - 1
	statusBar := leftLabel
	if statusPad > 0 {
		statusBar += strings.Repeat(" ", statusPad) + hints
	}

	footer := sep + "\n" + inputSection + "\n" + statusBar

	// Top banner: "▲ N older — ctrl+u to load"
	topBanner := ""
	if m.hasMoreOlder() {
		bannerText := i18n.TF("mail.load_more", m.olderCount())
		topBanner = StyleFaint.Render(centerText(bannerText, m.width)) + "\n"
	}

	// Bottom banner: "▼ ctrl+d to collapse to recent"
	bottomBanner := ""
	if m.loadedExtra > 0 {
		bannerText := i18n.T("mail.collapse")
		bottomBanner = StyleFaint.Render(centerText(bannerText, m.width)) + "\n"
	}

	// Viewport fills the middle
	return header + "\n" + topBanner + PaintViewportBG(m.viewport.View(), m.width) + "\n" + bottomBanner + footer
}
