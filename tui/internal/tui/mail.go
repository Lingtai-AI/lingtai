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

// unlimitedPageSize is the effective page size when the user selects "infinite".
const unlimitedPageSize = 999999

const (
	mailInputMinMaxHeight    = 3
	mailInputHardMaxHeight   = 14
	mailInputViewportReserve = 8
)

// copyModeToggleKey toggles the chat/mail "copy mode", which disables terminal
// mouse capture for this view so the user can drag-select visible transcript
// text. Single constant so the binding is swappable in one place.
const copyModeToggleKey = "ctrl+y"

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
	ApiCallID   string               // for text_output/tool_call/tool_result: LLM API round-trip grouping id
	TokenUsage  *fs.TokenUsage       // for Type=="llm_response": per-round token scalars; rendered as a footer at the bottom of the api_call group
	Summary     *fs.AprioriSummary   // for Type=="apriori_summary" (and tool_result carrying the artifact): the model-visible summary=true result
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
	initial      bool   // true only for the deferred initial rebuild (clears the loading banner)
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
	verboseThinking                     // ctrl+o cycle: mail + soul + tool_call/tool_result first lines
	verboseExtended                     // ctrl+o cycle: full tool_call/tool_result
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
	lastTelemetryRow  bool             // whether the home telemetry row was reserved last sync
	pendingMessage    string           // full text from editor, sent on Enter
	globalDir         string           // ~/.lingtai-tui/
	wasActive         bool             // true if previous refresh was ACTIVE
	quoteIdx          int              // which quote to show (advances on each ACTIVE transition)
	pulseTick         int              // pulse animation counter while ACTIVE
	activeSince       time.Time        // when the agent last entered ACTIVE (zero when not active)
	inquiryState      string           // "", "sent", "taken" — tracks /btw lifecycle
	insightPending    bool             // true when waiting for 5s insight delay
	insightAt         time.Time        // when to fire the auto-insight
	dismissedInsights map[string]bool  // dismissed insight timestamps
	showEditorWarn    bool             // one-time vim warning overlay
	editorWarnText    string           // text to pass to editor after warning
	insightsEnabled   bool             // from settings — show insight events
	toolCallTruncate  int              // from settings — max chars per tool line (0 = no truncation)
	sessionCache      *fs.SessionCache // append-only session log
	initialLoading    bool             // true until the deferred initial rebuild's refresh has been applied
	copyMode          bool             // chat-only: disables mouse capture so the terminal can select/copy visible text

	// Home telemetry is resolved asynchronously off the render/input path (its
	// I/O reaches sqlite + the token ledger + .status.json, which can stall on a
	// locked/slow sidecar). View()/hasHomeTelemetry()/syncViewportHeight() read
	// this cached snapshot ONLY; the fetchHomeTelemetry background command
	// refreshes it via homeTelemetryMsg. See home_telemetry.go's async note.
	homeTelemetry          homeTelemetry // last-known snapshot; zero value renders no row
	homeTelemetryLoaded    bool          // true once a background fetch has completed at least once
	homeTelemetryInFlight  bool          // true while a fetchHomeTelemetry command is running (debounce)
	homeTelemetryLastFetch time.Time     // completion time of the last fetch, for the TTL floor
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
		// The authoritative session rebuild is deferred to initialRebuild() (see
		// below), so the first frames render before history is loaded. Show a
		// loading banner at the top of the stream until that rebuild's refresh
		// lands; the mailRefreshMsg handler clears it on the initial message.
		initialLoading: true,
	}
	// NOTE: the mail-cache refresh and the authoritative session rebuild are
	// intentionally NOT done here. NewMailModel runs on the synchronous launch
	// path (NewApp, before tea.Program.Run), so doing the rebuild here parses
	// the entire events.jsonl / soul_inquiry.jsonl / soul_flow.jsonl history
	// before the first frame can render — seconds of frozen terminal on
	// content-heavy projects. The rebuild is deferred to initialRebuild(), a
	// command run by Init(), so the first frame paints immediately (empty) and
	// the history fills in a beat later. See initialRebuild for the rationale.
	return m
}

// initialRebuild performs the one-time authoritative session rebuild off the
// synchronous launch path. It refreshes the mail cache, then rebuilds
// session.jsonl from all sources (mail + events.jsonl + soul_inquiry.jsonl +
// soul_flow.jsonl), merging and sorting chronologically. This is the heavy work
// that used to run in NewMailModel; running it as a tea.Cmd keeps the first
// frame instant. It returns a mailRefreshMsg so the standard refresh handler
// builds the view from the now-populated cache. sessionCache is a pointer, so
// the rebuild mutates the model's shared cache; the returned cache is installed
// by the mailRefreshMsg handler.
func (m MailModel) initialRebuild() tea.Msg {
	// Refresh mail cache before session rebuild so mail entries are included.
	cache := m.cache.Refresh()
	// Always rebuild session.jsonl from authoritative sources on launch. This
	// avoids the entire class of dedup bugs that come from trying to patch an
	// existing file across restarts.
	m.sessionCache.RebuildFromSources(cache, m.humanAddr, m.orchestrator, m.orchDisplayName())
	m.cache = cache
	// Tag the resulting refresh as the initial one so the handler can clear the
	// loading banner. Only this rebuild flips initialLoading off; periodic ticks
	// produce untagged mailRefreshMsg values and never re-show the banner.
	msg := m.refreshMail()
	if rm, ok := msg.(mailRefreshMsg); ok {
		rm.initial = true
		return rm
	}
	return msg
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
	telemetryRow := m.hasHomeTelemetry()
	if inputLines == m.lastInputLines && paletteLines == m.lastPaletteLines && bannerLines == m.lastBannerLines && telemetryRow == m.lastTelemetryRow {
		return false
	}
	m.lastInputLines = inputLines
	m.lastPaletteLines = paletteLines
	m.lastBannerLines = bannerLines
	m.lastTelemetryRow = telemetryRow
	// Layout: header(2) + topBanner(0-1) + viewport + bottomBanner(0-1) + footer.
	// The footer block (sep + palette + input + optional telemetry + status) is
	// sized by mailFooterHeight so View() and this height budget stay in lockstep
	// — the telemetry row added by PR #441 must be reserved here or it pushes the
	// bottom status bar (the "ctrl+o to expand" hint) off-screen.
	footerHeight := mailFooterHeight(paletteLines, inputLines, telemetryRow)
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

func (m MailModel) chatTailRemainingLines() int {
	if !m.ready || m.viewport.Height() <= 0 {
		return 0
	}
	remaining := m.viewport.TotalLineCount() - m.viewport.Height() - m.viewport.YOffset()
	if remaining < 0 {
		return 0
	}
	return remaining
}

func (m MailModel) showChatTailHint() bool {
	return m.chatTailRemainingLines() > m.viewport.Height()
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
		case "thinking", "diary", "text_input", "text_output", "tool_call", "tool_result":
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
		// Ctrl+O level 1 uses tool entries as compact progress markers:
		// render only the first line there, and reserve the full body for
		// level 2. The cycle has no third verbose layer.
		return m.verbose >= verboseThinking
	case "apriori_summary":
		// The model-visible `summary=true` result that replaced a raw tool
		// payload. Shown at the same Ctrl+O depth as the tool_result it follows
		// so the agent's actual (compressed) view sits right beside the raw.
		return m.verbose >= verboseThinking
	case "llm_response":
		// Normally a hidden boundary marker used to derive tool-call grouping
		// for older events. When it carries per-round token usage we keep it so
		// renderMessages can emit a compact usage footer at the bottom of the
		// api_call group (it never renders as a raw "[llm_response]" block).
		return e.TokenUsage != nil && m.verbose >= verboseThinking
	case "llm_call":
		// Hidden boundary marker used to derive tool-call grouping for older
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
// signs (issue #40) as a single compact line: "ctx 14.7% · 21:10 PDT · seq 2".
// Returns "" when meta is nil (older events
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
		TokenUsage:  e.TokenUsage,
		Summary:     e.Summary,
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
		// initialRebuild does the one-time authoritative session rebuild off the
		// synchronous launch path (see initialRebuild and NewMailModel). It
		// returns a mailRefreshMsg, so the standard refresh handler builds the
		// view once history is loaded. The periodic tick below then keeps it
		// current via the incremental Refresh path.
		m.initialRebuild,
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
		if msg.initial {
			// The deferred initial rebuild has landed — history is now built, so
			// drop the loading banner. Periodic refreshes leave this untouched.
			m.initialLoading = false
		}
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
			// Just became active — advance to next quote, reset pulse, start timer
			m.quoteIdx++
			m.pulseTick = 0
			m.insightPending = false
			m.activeSince = time.Now()
		} else if !isActive {
			// Not active — stop the elapsed timer so the badge drops it
			m.activeSince = time.Time{}
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
		// Kick off the first background telemetry fetch as soon as a refresh has
		// landed (including the deferred initial rebuild), so the row can appear on
		// first load without waiting a full poll tick. Debounced by the same
		// in-flight/TTL gate, so this and the tick driver never double-fetch.
		if cmd := m.maybeScheduleHomeTelemetry(time.Now()); cmd != nil {
			return m, cmd
		}
		return m, nil

	case pulseTickMsg:
		if strings.EqualFold(m.orchState, "ACTIVE") {
			m.pulseTick++
		}
		return m, pulseTick()

	case tickMsg:
		// Steady-state driver: alongside the incremental mail refresh, schedule a
		// background telemetry fetch (debounced by in-flight + TTL). All telemetry
		// I/O funnels through maybeScheduleHomeTelemetry — the UI path never gathers.
		cmds = append(cmds, m.refreshMail, tickEvery(m.pollRate))
		if cmd := m.maybeScheduleHomeTelemetry(time.Now()); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case homeTelemetryMsg:
		// A background telemetry fetch completed. Land the snapshot; only re-sync
		// the viewport height when the row's visibility flipped (data ⇄ no-data),
		// so ordinary numeric updates don't thrash the layout.
		if m.applyHomeTelemetry(msg.t, time.Now()) && m.ready {
			atBottom := m.viewport.AtBottom()
			m.syncViewportHeight()
			m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
			if atBottom {
				m.viewport.GotoBottom()
			}
		}
		return m, nil

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

		// Copy mode: the toggle key flips terminal-native text selection for the
		// chat transcript (App.View drops mouse capture while it is on). esc exits
		// copy mode when active. Handled before the palette/insight branches so esc
		// reliably exits copy mode instead of dismissing insights. Input keeps
		// focus throughout — copy mode only changes the mouse axis.
		if msg.String() == copyModeToggleKey {
			m.copyMode = !m.copyMode
			return m, nil
		}
		if m.copyMode && msg.String() == "esc" {
			m.copyMode = false
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
		case "ctrl+v":
			m.pasteClipboardImageFromSystem()
			return m, nil
		case "ctrl+r":
			// Refresh the mail thread and agent state from disk. ctrl+r is a
			// control key, so it does not interfere with typing `r` into the
			// compose textarea (which falls through to the default branch).
			return m, m.refreshMail
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
			// Re-render immediately and anchor to bottom so the user
			// sees the latest output after the verbose level changes.
			if m.ready {
				m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
				m.viewport.GotoBottom()
				return m, nil
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

		case "ctrl+end":
			if m.ready {
				// Ctrl+End is the chat-tail jump even while the compose textarea
				// keeps focus; do not forward it to textarea cursor handling.
				m.viewport.GotoBottom()
				return m, nil
			}
			return m, nil

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

	// glamour.NewTermRenderer is heavyweight (it parses a style and builds an
	// ANSI renderer), so constructing one per agent/insight message — as this
	// loop used to — costs O(visible messages) renderer builds every render
	// pass, the dominant source of mail-view lag at large page sizes. The word
	// wrap width is the only per-message variable (it depends on the sender-name
	// prefix length for mail bubbles), so cache one renderer per distinct width
	// for the duration of this call. A nil entry records a width whose renderer
	// failed to construct, so callers fall back to the plain-wrap path exactly
	// as before without retrying the failed build.
	glamourStyle := ActiveTheme().GlamourStyle
	renderers := make(map[int]*glamour.TermRenderer)
	markdownRenderer := func(wrap int) *glamour.TermRenderer {
		if r, ok := renderers[wrap]; ok {
			return r
		}
		r, err := glamour.NewTermRenderer(
			glamour.WithStandardStyle(glamourStyle),
			glamour.WithWordWrap(wrap),
		)
		if err != nil {
			renderers[wrap] = nil
			return nil
		}
		renderers[wrap] = r
		return r
	}

	var b strings.Builder
	var prevVisibleApiGroup *ChatMessage

	// Token-usage footer is deferred to the bottom of its api_call group. The
	// llm_response carrier that holds the scalars arrives at the TOP of the
	// group in stream order (llm_call → llm_response → tool_call/tool_result),
	// so we stash it and flush a single faint footer line once the group ends:
	// before the visual separator that precedes the next group, when a
	// non-grouped entry breaks the run, or at end of stream.
	tokenFooterStyle := lipgloss.NewStyle().Foreground(ColorTextDim).Faint(true)
	var pendingUsage *fs.TokenUsage
	pendingGroup := ""
	flushTokenFooter := func() {
		if pendingUsage == nil {
			return
		}
		if footer := formatTokenUsageFooter(pendingUsage); footer != "" {
			b.WriteString(tokenFooterStyle.Render("  "+RuneBullet+" "+footer) + "\n")
		}
		pendingUsage = nil
		pendingGroup = ""
	}

	for _, msg := range msgs {
		// The llm_response carrier renders nothing inline — it only arms the
		// deferred footer for its group. A second llm_response for the same
		// group (rare) keeps the latest scalars.
		if msg.Type == "llm_response" {
			if msg.TokenUsage != nil && m.verbose >= verboseThinking {
				if pendingGroup != "" && pendingGroup != msg.ApiCallID {
					flushTokenFooter()
				}
				pendingUsage = msg.TokenUsage
				pendingGroup = msg.ApiCallID
			}
			continue
		}
		// A pending footer belongs to the group identified by pendingGroup.
		// Flush it once that group ends: either the current entry is not part
		// of the same api_call group, or it is a non-grouped entry type.
		if pendingUsage != nil {
			if !isApiGroupedVerboseMessageType(msg.Type) || msg.ApiCallID != pendingGroup {
				flushTokenFooter()
			}
		}
		if !isApiGroupedVerboseMessageType(msg.Type) {
			prevVisibleApiGroup = nil
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
				if apiCallGroupSeparatorBefore(prevVisibleApiGroup, msg) {
					b.WriteString(renderApiCallGroupSeparator(m.width) + "\n")
				}
				evStyle = thinkingStyle
			default:
				if apiCallGroupSeparatorBefore(prevVisibleApiGroup, msg) {
					b.WriteString(renderApiCallGroupSeparator(m.width) + "\n")
				}
				evStyle = toolStyle
				// Tool lines get a leading timestamp. Ctrl+O level 1 shows only
				// the first line of tool_call/tool_result as a compact index; Ctrl+O
				// level 2 shows full tool calls/results, still honoring the user's
				// per-tool-call truncation setting (0 = full content, the default).
				if ts := formatToolTimestamp(msg.Timestamp); ts != "" {
					tsPrefix = StyleFaint.Render(ts) + " "
				}
				if isToolMessageType(msg.Type) && m.verbose == verboseThinking {
					if msg.Type == "tool_call" {
						body = compactToolCallSummary(body)
					} else {
						body = firstRenderedLine(body)
					}
				} else {
					body = truncateToolBody(body, m.toolCallTruncate)
				}
			}
			wrapped := lipgloss.NewStyle().Width(wrapWidth).Render(tsPrefix + "[" + msg.Type + "] " + body)
			for _, line := range strings.Split(wrapped, "\n") {
				b.WriteString(evStyle.Render("  "+RuneBullet+" "+line) + "\n")
			}
			// Defensive secondary shape: a tool_result whose logged result IS the
			// model-visible summary artifact. Render the labelled summary block
			// right after the raw block so the agent's actual (compressed) view
			// sits directly below the raw stdout. The common production shape
			// instead emits a standalone apriori_summary entry (case below).
			if msg.Type == "tool_result" && msg.Summary != nil {
				for _, line := range renderAprioriSummaryBlock(msg.Summary, wrapWidth, m.verbose != verboseExtended) {
					b.WriteString(line + "\n")
				}
			}
			if isApiGroupedVerboseMessageType(msg.Type) {
				msgCopy := msg
				prevVisibleApiGroup = &msgCopy
			}

		case "apriori_summary":
			// The model-visible `summary=true` result that replaced a raw tool
			// payload (kernel PR #586). Logged as a lifecycle event right after
			// its raw tool_result, so in stream order it already lands directly
			// below the corresponding raw block — exactly where Jason wants the
			// "this is what the agent actually saw" reminder. It shares the
			// raw result's api_call_id, so it stays grouped with it (no leading
			// separator) and a new api round still starts a fresh group.
			wrapWidth := m.width - 6
			if wrapWidth < 20 {
				wrapWidth = 20
			}
			if apiCallGroupSeparatorBefore(prevVisibleApiGroup, msg) {
				b.WriteString(renderApiCallGroupSeparator(m.width) + "\n")
			}
			for _, line := range renderAprioriSummaryBlock(msg.Summary, wrapWidth, m.verbose != verboseExtended) {
				b.WriteString(line + "\n")
			}
			msgCopy := msg
			prevVisibleApiGroup = &msgCopy

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
			if r := markdownRenderer(max(wrapWidth-2, 10)); r != nil {
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
				if r := markdownRenderer(bodyWidth); r != nil {
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
	// Flush a token footer left pending by the final api_call group.
	flushTokenFooter()
	return b.String()
}

func (m MailModel) viewportWithChatTailHint() string {
	view := m.viewport.View()
	if !m.showChatTailHint() {
		return view
	}

	text := i18n.T("mail.jump_bottom_hint")
	maxHintWidth := m.width - 4
	if maxHintWidth < 10 {
		return view
	}
	if lipgloss.Width(text) > maxHintWidth {
		text = ansi.Truncate(text, maxHintWidth, "…")
	}
	hint := chatTailHintStyle().Render(text)
	hintWidth := lipgloss.Width(hint)
	if hintWidth <= 0 || hintWidth > m.width {
		return view
	}

	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		return view
	}

	// Overlay into the viewport's already-padded output. This makes the hint
	// non-focus and non-layout-affecting: no scroll, history, or input state
	// changes are needed just to render it.
	row := len(lines) - 1
	suffix := m.width - hintWidth
	if suffix < 0 {
		suffix = 0
	}
	lines[row] = hint + strings.Repeat(" ", suffix)
	return strings.Join(lines, "\n")
}

func chatTailHintStyle() lipgloss.Style {
	return StyleSubtle
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

// stateGlyph returns the leading glyph for the agent-state badge. ACTIVE uses
// the rotating spinner frame so the badge visibly animates in normal mode;
// every other state gets a distinct static glyph (color carries the rest of
// the distinction via StateColor).
func (m MailModel) stateGlyph() string {
	switch strings.ToUpper(m.orchState) {
	case "ACTIVE":
		return spinnerFrames[m.pulseTick%len(spinnerFrames)]
	case "ASLEEP":
		return "◌"
	case "SUSPENDED":
		return "○"
	case "REFRESHING":
		return "⟳"
	default: // IDLE, STUCK, unknown
		return "◉"
	}
}

// activeElapsed returns a short " 12s" / " 3m" suffix while the agent is ACTIVE,
// or "" otherwise — the "how long has it been working" signal.
func (m MailModel) activeElapsed() string {
	if !strings.EqualFold(m.orchState, "ACTIVE") || m.activeSince.IsZero() {
		return ""
	}
	d := time.Since(m.activeSince)
	if d < time.Minute {
		return fmt.Sprintf(" %ds", int(d.Seconds()))
	}
	return fmt.Sprintf(" %dm", int(d.Minutes()))
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

// composeCenteredHeader lays out a three-part header line — left block anchored
// at column 0, right block flush with the terminal's right edge, and the center
// block placed at the *absolute* terminal midpoint (start column
// (width-centerW)/2) rather than centered in the leftover gap between left and
// right. All widths are display widths (lipgloss.Width) so ANSI styling and
// multibyte runes are handled correctly.
//
// When absolute centering would overlap the left or right block (or the
// terminal is too narrow), it falls back to centering the block in the leftover
// gap, and finally to a single-space compact layout, so the line is always
// non-empty and never drops a block.
func composeCenteredHeader(left, center, right string, width int) string {
	leftW := lipgloss.Width(left)
	centerW := lipgloss.Width(center)
	rightW := lipgloss.Width(right)

	// Absolute centering: place the center block so its midpoint sits at the
	// terminal midpoint. Require at least one space of separation on each side
	// of the center block to keep the blocks visually distinct.
	start := (width - centerW) / 2
	if start >= leftW+1 && start+centerW <= width-rightW-1 {
		leftGap := start - leftW
		rightGap := width - rightW - (start + centerW)
		return left + strings.Repeat(" ", leftGap) + center + strings.Repeat(" ", rightGap) + right
	}

	// Fallback: center the block in whatever gap remains between the anchored
	// left and right blocks.
	gapTotal := width - leftW - centerW - rightW - 1
	if gapTotal > 0 {
		leftGap := gapTotal / 2
		rightGap := gapTotal - leftGap
		return left + strings.Repeat(" ", leftGap) + center + strings.Repeat(" ", rightGap) + right
	}

	// Too narrow for any gap: compact single-space layout, no overlap math.
	return left + " " + center + " " + right
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
	var titleLine string
	if titleCenter != "" {
		titleLine = composeCenteredHeader(titleLeft, titleCenter, titleRight, m.width)
	} else {
		padding := m.width - leftW - rightW - 1
		if padding > 0 {
			titleLine = titleLeft + strings.Repeat(" ", padding) + titleRight
		} else {
			titleLine = titleLeft + "  " + titleRight
		}
	}
	header := titleLine + "\n" + strings.Repeat("\u2500", m.width)

	// Build footer — "Email To: AgentName  ◉ <state> <elapsed> ─────────"
	// The activity indicator lives here, in the interaction line, so the human
	// sees the agent's live state (animated spinner + elapsed while ACTIVE) right
	// where their attention already is. Reuses the header's stateStyle/stateLabel
	// and is independent of the verbose level.
	indicator := stateStyle.Render(m.stateGlyph() + " " + stateLabel + m.activeElapsed())
	toLabel := StyleFaint.Render("Email To: ") + lipgloss.NewStyle().Foreground(ColorAgent).Render(m.orchDisplayName()) + "  " + indicator + " "
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
	if m.copyMode {
		// Copy mode wins the left label: it is the most important thing to
		// communicate, and the user needs to see how to exit (mouse is off).
		// Truncate the plain text to the terminal width before styling so the
		// badge never wraps onto a second line (the height math assumes one row).
		badge := "  ◉ " + i18n.T("mail.copy_mode")
		if m.width > 0 {
			badge = ansi.Truncate(badge, m.width-1, "…")
		}
		leftLabel = lipgloss.NewStyle().Foreground(ColorAccent).Render(badge)
	} else if m.inquiryState == "sent" || m.inquiryState == "taken" {
		leftLabel = lipgloss.NewStyle().Foreground(ColorAccent).Render("  ◉ " + i18n.T("mail.btw_thinking"))
	} else if m.statusFlash != "" && time.Now().Before(m.statusExpiry) {
		leftLabel = lipgloss.NewStyle().Foreground(ColorAgent).Render("  ◉ " + m.statusFlash)
	} else {
		m.statusFlash = ""
		leftLabel = StyleSubtle.Render("  " + m.baseDir)
	}
	// Separator between the ctrl+o verbosity affordance and the slash-command
	// affordance. Localized (`hints.sep`): English reads `ctrl+o to expand, / for
	// commands` (comma); zh/wen keep the bullet convention. The first segment
	// carries no trailing separator so the comma attaches to "to expand".
	hintSep := i18n.T("hints.sep")
	var hints string
	switch m.verbose {
	case verboseOff:
		hints = StyleSubtle.Render(i18n.T("hints.verbose")) +
			StyleFaint.Render(hintSep+i18n.T("hints.commands"))
	case verboseThinking:
		hints = lipgloss.NewStyle().Foreground(ColorAgent).Render(i18n.T("hints.verbose_on")) +
			StyleFaint.Render(hintSep+i18n.T("hints.commands"))
	case verboseExtended:
		hints = lipgloss.NewStyle().Foreground(ColorThinking).Render(i18n.T("hints.extended_on")) +
			StyleFaint.Render(hintSep+i18n.T("hints.commands"))
	}
	statusPad := m.width - lipgloss.Width(leftLabel) - lipgloss.Width(hints) - 1
	statusBar := leftLabel
	if statusPad > 0 {
		statusBar += strings.Repeat(" ", statusPad) + hints
	}

	// Telemetry row: one muted, high-density line between the input box and the
	// status/path footer showing current-session token usage and live context
	// pressure ("tok 18.4k / 128k  ctx 14%  ▓▓▓░░"). Omitted entirely when no
	// session/context data is available (graceful hide). Scalar-only — never the
	// `_meta` block hidden by PR #440.
	footer := sep + "\n" + inputSection + "\n"
	// Read the cached telemetry snapshot ONLY — never gatherHomeTelemetry — so the
	// render path performs no sqlite/filesystem/JSONL work. The snapshot is
	// refreshed asynchronously by fetchHomeTelemetry (see home_telemetry.go).
	// Gate on hasHomeTelemetry() (which carries the homeTelemetryLoaded guard) so
	// View and syncViewportHeight share the exact same visibility predicate and can
	// never disagree about whether the row occupies a line.
	if m.hasHomeTelemetry() {
		if telemetry := formatHomeTelemetry(m.homeTelemetry, m.width); telemetry != "" {
			footer += telemetry + "\n"
		}
	}
	footer += statusBar

	// Top banner: a one-time "loading... / 加载中..." line while the deferred
	// initial session rebuild is still pending, then "▲ N older — ctrl+u to load".
	topBanner := ""
	if m.initialLoading {
		loadingText := i18n.T("mail.initial_loading")
		topBanner = StyleFaint.Render(centerText(loadingText, m.width)) + "\n"
	} else if m.hasMoreOlder() {
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
	return header + "\n" + topBanner + PaintViewportBG(m.viewportWithChatTailHint(), m.width) + "\n" + bottomBanner + footer
}
