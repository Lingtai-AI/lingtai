package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

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
	Reasoning   string               // for Type=="tool_call": its own LTP _reasoning, rendered immediately above the call
	TokenUsage  *fs.TokenUsage       // for Type=="llm_response": per-round token scalars; rendered as a footer at the bottom of the api_call group
	Summary     *fs.AprioriSummary   // for Type=="apriori_summary" (and tool_result carrying the artifact): the model-visible summary=true result
}

// ViewChangeMsg requests the app to switch views.
type ViewChangeMsg struct {
	View string
}

type pulseTickMsg struct {
	generation uint64
	pollEpoch  uint64
	at         time.Time
}

func pulseTick(generation, pollEpoch uint64) tea.Cmd {
	return tea.Every(250*time.Millisecond, func(t time.Time) tea.Msg {
		return pulseTickMsg{generation: generation, pollEpoch: pollEpoch, at: t}
	})
}

type mailRefreshMsg struct {
	generation           uint64
	refreshRequestSerial uint64           // the one monotonic request serial issued on the Update loop; 0 only for fabricated internal test messages
	cache                fs.MailCache     // incrementally updated cache
	sessionCache         *fs.SessionCache // command-local authoritative rebuild; installed only after generation acceptance
	prepared             bool             // true only for real refresh commands prepared off the Bubble Tea loop
	acceptedSnapshot     acceptedMailSnapshot
	selectorRows         []agentSelectorRow
	directPublication    *fs.DirectMailPublication
	directStates         map[string]agentLifecycleState // stable direct-thread key -> target lifecycle presentation
	lastApiCallAt        time.Time                      // orchestrator .status.json runtime.last_api_call_at (fallback last_progress_at; zero when absent)
	directLastApiCall    map[string]time.Time           // stable direct-thread key -> target last_api_call_at
	alive                bool
	state                string // active, idle, stuck, asleep, stale, suspended, or ""
	staleSeconds         int    // ordinary stale heartbeat age; zero outside the stale presentation
	orchName             string // agent name from .agent.json (may change at runtime)
	orchNickname         string // nickname from .agent.json
	initial              bool   // true only for the deferred initial rebuild (clears the loading banner)
}

type networkActivityMsg struct {
	generation uint64
	activity   fs.NetworkActivity
	ok         bool
}

// mailPersistMsg is the second, post-frame phase of an accepted authoritative
// rebuild. The command that emits it performs no I/O; Update re-checks both the
// activation generation and installed cache identity before writing, so an old
// rebuild cannot race a newer activation's canonical session cache.
type mailPersistMsg struct {
	generation   uint64
	sessionCache *fs.SessionCache
}

type tickMsg struct {
	generation uint64
	pollEpoch  uint64
	at         time.Time
}

// EditorDoneMsg carries the final text from the external editor, tagged with
// the Mail generation and the exact Main/direct context captured at launch.
// Main launches leave the three direct fields empty/zero.
type EditorDoneMsg struct {
	Text             string
	Generation       uint64
	ProjectRoot      string
	DirectThreadKey  string
	DirectGeneration uint64
}

func tickEvery(d time.Duration, generation, pollEpoch uint64) tea.Cmd {
	return tea.Every(d, func(t time.Time) tea.Msg {
		return tickMsg{generation: generation, pollEpoch: pollEpoch, at: t}
	})
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

// heartbeatSuspendedGapSec is deliberately a presentation cutoff: the normal
// liveness threshold still decides fresh versus stale, while a much longer
// observed heartbeat gap may render as suspended.
const heartbeatSuspendedGapSec = 120.0

// agentLifecycleState is the command-prepared lifecycle presentation. State
// comes from the manifest unless a valid stale heartbeat needs the temporary
// stale/long-gap overlay; staleSeconds is rendered only for ordinary staleness.
type agentLifecycleState struct {
	state        string
	staleSeconds int
}

type directAgentLifecycle struct {
	state        string
	activeSince  time.Time
	staleSeconds int
}

type MailModel struct {
	humanDir                 string
	humanAddr                string
	orchestrator             string // 本我 directory path (full path under .lingtai/)
	orchAddr                 string // 本我 address (from .agent.json)
	orchName                 string // 本我 agent name (true name)
	orchNickname             string // 本我 nickname (display name override)
	baseDir                  string // .lingtai/ directory
	visitExitHint            bool   // append subtle Esc-Esc return hint to the title row
	verbose                  verboseLevel
	messages                 []ChatMessage        // derived from the accepted mailbox snapshot
	cache                    fs.MailCache         // sole incremental mail producer
	acceptedSnapshot         acceptedMailSnapshot // private consumer view installed after generation acceptance
	pageSize                 int                  // max messages shown (from settings)
	loadedExtra              int                  // additional older messages loaded via ctrl+u
	viewport                 viewport.Model
	input                    InputModel
	palette                  PaletteModel
	width                    int
	height                   int
	ready                    bool
	pollRate                 time.Duration // refresh interval
	orchAlive                bool
	orchState                string             // manifest lifecycle or heartbeat presentation state
	orchStaleSeconds         int                // ordinary stale heartbeat age; zero outside the stale presentation
	networkActivity          fs.NetworkActivity // last accepted cached project activity snapshot
	networkActivityInFlight  bool               // true while the detached filesystem observation is running
	networkActivityLastFetch time.Time          // completion time of the last observation, for the TTL floor
	statusFlash              string             // transient status message shown in status bar
	statusExpiry             time.Time          // when to clear the flash
	lastInputLines           int
	lastPaletteLines         int
	lastBannerLines          int
	lastTelemetryRows        int              // how many additive home rows were reserved last sync
	pendingMessage           string           // full text from editor, sent on Enter
	globalDir                string           // ~/.lingtai-tui/
	wasActive                bool             // true if previous refresh was ACTIVE
	quoteIdx                 int              // which quote to show (advances on each ACTIVE transition)
	pulseTick                int              // pulse animation counter while ACTIVE
	activeSince              time.Time        // when the agent last entered ACTIVE (zero when not active)
	inquiryState             string           // "", "sent", "taken" — tracks /btw lifecycle
	insightPending           bool             // true when waiting for 5s insight delay
	insightAt                time.Time        // when to fire the auto-insight
	dismissedInsights        map[string]bool  // dismissed insight timestamps
	showEditorWarn           bool             // one-time vim warning overlay
	editorWarnText           string           // text to pass to editor after warning
	insightsEnabled          bool             // from settings — show insight events
	toolCallTruncate         int              // from settings — max chars per tool line (0 = no truncation)
	sessionCache             *fs.SessionCache // append-only session log
	initialLoading           bool             // true until the bounded initial content rebuild has been applied
	auxiliaryMessages        int              // all renderable mail/inquiry entries, including older ones withheld across a partial event gap
	copyMode                 bool             // chat-only: disables mouse capture so the terminal can select/copy visible text
	generation               uint64           // activation token; stale async messages are ignored without rescheduling
	pollEpoch                uint64           // timer-chain token; one tick/pulse loop is current inside an activation
	beforeRebuild            func()           // optional deterministic test hook before deferred rebuild I/O

	// The one monotonic refresh request serial with its newest-accepted
	// completion watermark. Requests are issued only on the serialized Update
	// loop; a completion older than the newest accepted request installs no
	// mailbox/snapshot/selector/publication component. There is no second
	// coordinate and no Main-session ordinal.
	refreshRequestSerial         uint64 // newest request issued; 1 is reserved for the one-shot initial rebuild
	acceptedRefreshRequestSerial uint64 // newest prepared completion accepted for this activation
	mailRefreshInFlight          bool   // at most one periodic prepared Mail refresh command may run per activation
	mailRefreshInFlightSerial    uint64 // request serial that owns the lane; older initial work cannot release it
	mailRefreshPending           bool   // one coalesced demand to launch immediately after the owning completion

	// The /agents direct-conversation core: one accepted publication serial,
	// the Mail-owned canonical conversation catalog/selection, the one
	// current-only direct state projected from the accepted publication, the
	// accepted per-thread lifecycle cache, and the immutable fs-owned direct
	// publication itself.
	acceptedSnapshotSerial uint64
	directChat             directChatState
	agentSelector          agentSelectorState
	agentRail              agentRailState
	directPublication      *fs.DirectMailPublication
	directLifecycles       map[string]directAgentLifecycle

	// Durable direct-unread work is serialized through one accepted-coordinate
	// command at a time. A newer accepted publication coalesces into
	// syncPending; no command mutates the store installed in the live model.
	directUnread            *fs.DirectUnreadStore
	directUnreadProjectRoot string
	directUnreadOpSerial    uint64
	directUnreadOpInFlight  bool
	directUnreadSyncPending bool

	// Home telemetry is resolved asynchronously off the render/input path (its
	// I/O reads one .status.json snapshot plus context-only fallbacks, which can
	// stall on a slow volume). View()/hasHomeTelemetry()/syncViewportHeight() read
	// this cached snapshot ONLY; the fetchHomeTelemetry background command
	// refreshes it via homeTelemetryMsg. See home_telemetry.go's async note.
	homeTelemetry          homeTelemetry // last-known snapshot; zero value renders no row
	homeTelemetryLoaded    bool          // true once a background fetch has completed at least once
	homeTelemetryInFlight  bool          // true while a fetchHomeTelemetry command is running (debounce)
	homeTelemetryLastFetch time.Time     // completion time of the last fetch, for the TTL floor
	// homeTelemetryDisplay is the validated row expression from tui_config.json's
	// optional `home_telemetry_display` presentation preference; nil means the
	// built-in default expression. It is an app-owned TUI preference, not a
	// property of a mail context, so it is applied by App.installMailModel and
	// re-applied on the settings-return path (app.go) rather than threaded
	// through NewMailModel — the render path only ever walks a legal order.
	homeTelemetryDisplay []homeTelemetrySegment

	// Home async-work stats resolve exactly like home telemetry: one bounded
	// dispatch-ledger snapshot, cached result, in-flight debounce, and TTL floor.
	homeAsyncStats          homeAsyncStats // last-known snapshot; zero value renders an all-zero row once loaded
	homeAsyncStatsLoaded    bool           // true once a background fetch has completed at least once
	homeAsyncStatsInFlight  bool           // true while a fetchHomeAsyncStats command is running (debounce)
	homeAsyncStatsLastFetch time.Time      // completion time of the last fetch, for the TTL floor
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
	pageSize = config.NormalizeMailPageSize(pageSize)
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
		sessionCache:      fs.NewSessionCache(humanDir, filepath.Dir(baseDir), fs.MainAggregateWriter),
		// Serial 1 is reserved for the one-shot deferred initial rebuild issued
		// by Init; every later request is issued on the Update loop.
		refreshRequestSerial: 1,
		// The authoritative session rebuild is deferred to initialRebuild() (see
		// below), so the first frames render before history is loaded. Show a
		// loading banner at the top of the stream until that rebuild's refresh
		// lands; the mailRefreshMsg handler clears it on the initial message.
		initialLoading: true,
	}
	// NOTE: the mail-cache refresh and authoritative bounded session rebuild are
	// intentionally NOT done here. NewMailModel runs on the synchronous launch
	// path (NewApp, before tea.Program.Run), so even the newest content window
	// would delay the first frame on content-heavy projects. The rebuild is
	// deferred to initialRebuild(), a command run by Init(), so the first frame
	// paints immediately (empty) and the newest bounded window fills in a beat
	// later. Nothing counts the full history — there is no total to compute.
	return m
}

// initialRebuild performs the one-time authoritative bounded content rebuild off
// the synchronous launch path. Every source it touches is bounded before any
// body is read: the newest fs.RecentMessageLimit() mailbox entries, the newest
// contentWindow() canonical event entries, plus current auxiliary sources,
// merged chronologically. Nothing scans the full mailbox or full history, and
// no exact total is computed — the page is painted from the window alone.
// The one exception is the explicit LINGTAI_TUI_MESSAGE_LIMIT=0 opt-in, which
// resolves to an unlimited window and asks for exactly that full load.
// Running this as a tea.Cmd keeps the first frame instant. It returns a
// mailRefreshMsg carrying command-local mail and session caches; the live model
// installs and persists them only after accepting the message's generation, so
// a late rebuild cannot mutate a newer activation.
func (m MailModel) initialRebuild() tea.Msg {
	if m.beforeRebuild != nil {
		m.beforeRebuild()
	}
	// Refresh mail cache before session rebuild so mail entries are included.
	// The bounded variant reads at most fs.RecentMessageLimit() message bodies
	// regardless of how much mail the mailbox holds.
	cache := m.cache.RefreshRecent(fs.RecentMessageLimit())
	// Always rebuild from authoritative sources on launch. Keep both the in-memory
	// snapshot and its session.jsonl write command-local until Update accepts this
	// generation; stale work must have no effect on the installed cache.
	sessionCache := fs.NewSessionCache(m.humanDir, filepath.Dir(m.baseDir), fs.MainAggregateWriter)
	sessionCache.RebuildFromSourcesWindowedInMemory(cache, m.humanAddr, m.orchestrator, m.orchDisplayName(), m.contentWindow())
	m.cache = cache
	// Tag the resulting refresh as the initial one so the handler can clear the
	// loading banner. Only this rebuild flips initialLoading off; periodic ticks
	// produce untagged mailRefreshMsg values and never re-show the banner.
	msg := m.refreshMail()
	if rm, ok := msg.(mailRefreshMsg); ok {
		rm.initial = true
		rm.generation = m.generation
		rm.sessionCache = sessionCache
		return rm
	}
	return msg
}

// contentWindow is the number of newest canonical event entries any rebuild
// ingests. A positive resolved limit bounds the configured mail_page_size; zero
// is the explicit unlimited sentinel understood by the fs session rebuild API,
// so Main loads the complete event history while mail_page_size still controls
// the initial visible batch and each Ctrl+U reveal batch.
func (m MailModel) contentWindow() int {
	if fs.RecentMessageLimit() == 0 {
		return 0
	}
	window := fs.ClampToRecentMessageLimit(m.pageSize)
	if window < 1 {
		window = 1
	}
	return window
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
	// Direct View suppresses Main's history banners and home telemetry, so its
	// viewport must reclaim exactly those rows while leaving Main state intact.
	_, direct := m.currentDirectTarget()
	bannerLines := 0
	telemetryRows := 0
	if !direct {
		bannerLines = m.bannerLineCount()
		if m.hasHomeTelemetry() {
			telemetryRows++
		}
		if m.hasHomeAsyncStats() {
			telemetryRows++
		}
	}
	// Size the palette before consuming LineCount. Mail owns the child rectangle
	// and reserves its fixed chrome plus one mandatory transcript row; Palette
	// owns which cursor-following command rows fit in the remaining allowance.
	paletteMaxHeight := m.height - 2 - bannerLines - 1 - mailFooterHeight(0, inputLines, telemetryRows)
	m.palette.SetSize(m.width, paletteMaxHeight)
	paletteLines := 0
	if m.input.IsPaletteActive() {
		paletteLines = m.palette.LineCount()
	}
	if inputLines == m.lastInputLines && paletteLines == m.lastPaletteLines && bannerLines == m.lastBannerLines && telemetryRows == m.lastTelemetryRows {
		return false
	}
	m.lastInputLines = inputLines
	m.lastPaletteLines = paletteLines
	m.lastBannerLines = bannerLines
	m.lastTelemetryRows = telemetryRows
	// Layout: header(2) + topBanner(0-1) + viewport + bottomBanner(0-1) + footer.
	// The footer block (sep + palette + input + optional telemetry + status) is
	// sized by mailFooterHeight so View() and this height budget stay in lockstep
	// — the telemetry row added by PR #441 must be reserved here or it pushes the
	// bottom status bar (the "ctrl+o to expand" hint) off-screen.
	footerHeight := mailFooterHeight(paletteLines, inputLines, telemetryRows)
	vpHeight := m.height - 2 - bannerLines - footerHeight
	if vpHeight < 1 {
		vpHeight = 1
	}
	if direct {
		atBottom := m.directChat.viewport.AtBottom()
		offset := m.directChat.viewport.YOffset()
		m.directChat.viewport.SetHeight(vpHeight)
		if atBottom {
			m.directChat.viewport.GotoBottom()
		} else {
			m.directChat.viewport.SetYOffset(offset)
		}
	} else {
		m.viewport.SetHeight(vpHeight)
	}
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
	bottomBannerLines := 0
	if _, direct := m.currentDirectTarget(); !direct {
		if m.initialLoading {
			topBannerLines = 1
		}
		if m.loadedExtra > 0 {
			bottomBannerLines = 1
		}
	}
	viewportHeight := m.viewport.Height()
	if _, direct := m.currentDirectTarget(); direct {
		viewportHeight = m.directChat.viewport.Height()
	}
	start = 2 + topBannerLines + viewportHeight + bottomBannerLines + 1 + paletteLines
	end = start + m.input.LineCount() + 1 // input rows plus border line
	return start, end
}

// Cursor returns the visible composer cursor in Mail child coordinates.
func (m MailModel) Cursor() *tea.Cursor {
	if !m.ready || m.showEditorWarn || m.agentSelector.selectorOpen ||
		m.copyMode || m.agentRail.focused || !m.input.Focused() {
		return nil
	}
	cursor := m.input.Cursor()
	if cursor == nil {
		return nil
	}
	projected := *cursor
	start, _ := m.inputRegionBounds()
	projected.Y += start
	return &projected
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
// The top banner is only the one-time loading line; there is no older-history
// banner because no total-message count is ever computed.
func (m *MailModel) bannerLineCount() int {
	n := 0
	if m.initialLoading {
		n++ // top banner
	}
	if m.loadedExtra > 0 {
		n++ // bottom banner (reserved when expanded)
	}
	return n
}

// cacheIsWindowed reports whether the installed session cache holds only the
// newest window of canonical events (older entries stayed on disk and are never
// loaded). buildMessages uses it to keep the rendered stream one contiguous
// tail instead of crossing the gap left by the excluded older events.
func (m *MailModel) cacheIsWindowed() bool {
	return m.sessionCache != nil && !m.sessionCache.Complete()
}

// visibleMessages returns the tail of m.messages limited by pageSize plus any
// Ctrl+U reveal, and never more than the resolved fs.RecentMessageLimit() cap.
// m.messages is already trimmed to that cap by buildMessages, so this is the
// second, render-side half of the same invariant.
func (m *MailModel) visibleMessages() []ChatMessage {
	limit := fs.ClampToRecentMessageLimit(m.pageSize + m.loadedExtra)
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

// issueRefreshRequest assigns the one monotonic refresh request serial on the
// serialized Update loop, then returns the detached prepared-refresh command
// bound to the issuing model state. At most one periodic command may be in
// flight: a timer or explicit refresh that arrives while the current snapshot is
// still preparing becomes one pending demand. The matching owner completion
// launches that one follow-up immediately instead of dropping it or overlapping
// filesystem work.
func (m MailModel) issueRefreshRequest() (MailModel, tea.Cmd) {
	if m.mailRefreshInFlight {
		m.mailRefreshPending = true
		return m, nil
	}
	m.refreshRequestSerial = nextAcceptedSnapshotSerial(m.refreshRequestSerial)
	m.mailRefreshInFlight = true
	m.mailRefreshInFlightSerial = m.refreshRequestSerial
	return m, m.refreshMail
}

const networkActivityTTL = time.Second

func (m MailModel) fetchNetworkActivity() tea.Cmd {
	return func() tea.Msg {
		activity, err := fs.ComputeNetworkActivity(m.baseDir)
		return networkActivityMsg{generation: m.generation, activity: activity, ok: err == nil}
	}
}

// maybeScheduleNetworkActivity owns the only recurring project-activity read.
// Its cached snapshot, in-flight gate, and TTL keep Mail's one-second poll from
// re-entering a slow filesystem observation.
func (m *MailModel) maybeScheduleNetworkActivity(now time.Time) tea.Cmd {
	if m.baseDir == "" || m.networkActivityInFlight {
		return nil
	}
	if !m.networkActivityLastFetch.IsZero() && now.Sub(m.networkActivityLastFetch) < networkActivityTTL {
		return nil
	}
	m.networkActivityInFlight = true
	return m.fetchNetworkActivity()
}

func (m *MailModel) applyNetworkActivity(msg networkActivityMsg, now time.Time) {
	m.networkActivityInFlight = false
	m.networkActivityLastFetch = now
	if msg.ok {
		m.networkActivity = msg.activity
	}
}

func resolveAgentLifecycle(directory string) (bool, agentLifecycleState, fs.AgentNode) {
	if directory == "" {
		return false, agentLifecycleState{}, fs.AgentNode{}
	}

	heartbeat := fs.ReadHeartbeat(directory, fs.AgentAliveThresholdSec())
	alive := heartbeat.Fresh
	node, _ := fs.ReadAgent(directory)
	lifecycle := agentLifecycleState{state: strings.ToLower(strings.TrimSpace(node.State))}

	// A manifest-declared suspension is a real lifecycle transition. Keep it
	// visible regardless of heartbeat age rather than letting a stale observation
	// manufacture or erase that state.
	if alive || strings.EqualFold(lifecycle.state, "SUSPENDED") {
		return alive, lifecycle, node
	}
	if fs.HasRefreshTaken(directory) {
		lifecycle.state = "refreshing"
		return alive, lifecycle, node
	}

	// Only a parseable timestamp has an elapsed gap to present. Missing or
	// malformed heartbeats retain the manifest state instead of being guessed as
	// suspended. The normal threshold is stale; the 120-second presentation cutoff
	// is the only heartbeat-gap path that renders suspended.
	if heartbeat.Exists && heartbeat.Error == "" {
		if heartbeat.AgeSeconds >= heartbeatSuspendedGapSec {
			lifecycle.state = "suspended"
		} else {
			lifecycle.state = "stale"
			lifecycle.staleSeconds = int(heartbeat.AgeSeconds)
		}
	}
	return alive, lifecycle, node
}

func resolveDirectLifecycleStates(rows []agentSelectorRow) map[string]agentLifecycleState {
	states := make(map[string]agentLifecycleState, len(rows))
	for _, row := range rows {
		if row.Main {
			continue
		}
		key := fs.DirectThreadKey(row.Target)
		if key == "" {
			continue
		}
		_, lifecycle, _ := resolveAgentLifecycle(row.Target.Directory)
		states[key] = lifecycle
	}
	return states
}

func (m MailModel) refreshMail() tea.Msg {
	// Every value below is prepared on this tea.Cmd invocation, before Bubble
	// Tea receives the completion: the incremental cache refresh, the deep
	// accepted snapshot clone, canonical selector-row discovery, immutable
	// direct publication, and target lifecycle states. Update only validates
	// and installs these
	// detached results; it performs no manifest, unread, or clone work.
	//
	// The refresh is bounded: new mail still lands every tick (that is what keeps
	// the live conversation current), but the retained snapshot never grows past
	// the newest fs.RecentMessageLimit() entries, so neither the first frame nor
	// a steady-state tick pays for the whole mailbox.
	cache := m.cache.RefreshRecent(fs.RecentMessageLimit())
	acceptedSnapshot := newAcceptedMailSnapshot(cache)
	selectorRows := discoverAgentSelectorRows(m.baseDir)
	directPublication := fs.NewDirectMailPublication(
		m.humanAddr,
		directTargetsForRows(selectorRows),
		acceptedSnapshot.cache.Messages,
	)

	alive, lifecycle, orchestrator := resolveAgentLifecycle(m.orchestrator)
	directStates := resolveDirectLifecycleStates(selectorRows)
	lastApiCallAt, _ := fs.LastApiCallAt(m.orchestrator, time.Now())
	directLastApiCall := make(map[string]time.Time, len(directStates))
	for _, row := range selectorRows {
		if row.Main {
			continue
		}
		key := fs.DirectThreadKey(row.Target)
		if key == "" {
			continue
		}
		if lp, ok := fs.LastApiCallAt(row.Target.Directory, time.Now()); ok {
			directLastApiCall[key] = lp
		}
	}
	orchName := m.orchName
	orchNickname := ""
	if orchestrator.AgentName != "" {
		orchName = orchestrator.AgentName
	}
	orchNickname = orchestrator.Nickname
	return mailRefreshMsg{
		generation:           m.generation,
		refreshRequestSerial: m.refreshRequestSerial,
		cache:                cache,
		prepared:             true,
		acceptedSnapshot:     acceptedSnapshot,
		selectorRows:         selectorRows,
		directPublication:    directPublication,
		directStates:         directStates,
		lastApiCallAt:        lastApiCallAt,
		directLastApiCall:    directLastApiCall,
		alive:                alive,
		state:                lifecycle.state,
		staleSeconds:         lifecycle.staleSeconds,
		orchName:             orchName,
		orchNickname:         orchNickname,
	}
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
// Mail is projected exactly once from Main's accepted mailbox snapshot instead
// of the live producer or derived session entries, so unaccepted refresh work
// cannot leak into rendering or older-history reconstruction.
func (m *MailModel) buildMessages() {
	mailCache := m.acceptedSnapshot.cacheCopy(m.humanDir)
	// Ingest only the accepted mailbox publication into session.jsonl.
	m.sessionCache.Refresh(mailCache, m.humanAddr, m.orchestrator, m.orchDisplayName())

	// Build filtered view from the session cache.
	allEntries := m.sessionCache.Entries()
	chatMsgs := make([]ChatMessage, 0, len(allEntries))

	currentApiCallID := ""
	derivedApiCallSeq := 0
	m.auxiliaryMessages = 0
	firstLoadedEvent := -1
	if m.cacheIsWindowed() {
		for i, entry := range allEntries {
			if entry.Type != "mail" && !(entry.Type == "insight" && entry.Source != "") {
				firstLoadedEvent = i
				break
			}
		}
	}
	for entryIndex, e := range allEntries {
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
		// Session mail is a derived copy of MailCache. The accepted mailbox
		// snapshot below is the sole display source for mail; skipping this copy prevents
		// duplicate rendering while leaving every non-mail event path unchanged.
		if e.Type == "mail" {
			continue
		}
		if !m.shouldShow(e) {
			continue
		}
		// Mail and inquiry sources carry their own bounded window; event content
		// carries the contentWindow() one. While the event stream is windowed,
		// withhold auxiliary entries older than the oldest loaded event so the
		// rendered slice remains one chronological tail rather than crossing a gap.
		isEventEntry := e.Type != "mail" && !(e.Type == "insight" && e.Source != "")
		if !isEventEntry {
			m.auxiliaryMessages++
			if firstLoadedEvent >= 0 && entryIndex < firstLoadedEvent {
				continue
			}
		}
		cm := sessionEntryToChatMessage(e, m.humanAddr)
		chatMsgs = append(chatMsgs, cm)
	}
	for _, msg := range mailCache.Messages {
		chatMsgs = append(chatMsgs, mailMessageToChatMessage(msg, m.humanAddr, m.orchDisplayName()))
		m.auxiliaryMessages++
	}
	sort.SliceStable(chatMsgs, func(i, j int) bool {
		return chatMessageBefore(chatMsgs[i], chatMsgs[j])
	})

	// The one hard cap, applied to the merged stream: the page holds only the
	// newest fs.RecentMessageLimit() entries. Each source is already bounded
	// before its bodies are read, so this trim is the single place the combined
	// mail + event window becomes observable in the loaded snapshot. An
	// unlimited window clamps to the length itself and trims nothing.
	if kept := fs.ClampToRecentMessageLimit(len(chatMsgs)); kept < len(chatMsgs) {
		chatMsgs = chatMsgs[len(chatMsgs)-kept:]
	}

	// Restore dismissed state for insights.
	for i := range chatMsgs {
		if chatMsgs[i].Type == "insight" && m.dismissedInsights[chatMsgs[i].Timestamp] {
			chatMsgs[i].Dismissed = true
		}
	}
	m.messages = chatMsgs
}

func chatMessageBefore(a, b ChatMessage) bool {
	at, aErr := time.Parse(time.RFC3339Nano, a.Timestamp)
	bt, bErr := time.Parse(time.RFC3339Nano, b.Timestamp)
	if aErr == nil && bErr == nil {
		if !at.Equal(bt) {
			return at.Before(bt)
		}
	} else if a.Timestamp != b.Timestamp {
		return a.Timestamp < b.Timestamp
	}
	if a.Type != b.Type {
		return a.Type < b.Type
	}
	if a.From != b.From {
		return a.From < b.From
	}
	if a.Body != b.Body {
		return a.Body < b.Body
	}
	return a.Subject < b.Subject
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
		Reasoning:   e.Reasoning,
		TokenUsage:  e.TokenUsage,
		Summary:     e.Summary,
	}
	if e.Type == "mail" {
		cm.IsFromMe = e.From == "human"
		cm.IsFromOrch = !cm.IsFromMe
	}
	return cm
}

// mailMessageToChatMessage preserves the existing Mail presentation while
// changing only its source from the derived SessionCache entry to MailCache.
func mailMessageToChatMessage(msg fs.MailMessage, humanAddr, orchName string) ChatMessage {
	from := msg.From
	if i := strings.LastIndex(from, "/"); i >= 0 {
		from = from[i+1:]
	}
	if msg.From == humanAddr || from == "human" {
		from = "human"
	} else if nick, ok := msg.Identity["nickname"].(string); ok && nick != "" {
		from = nick
	} else if name, ok := msg.Identity["agent_name"].(string); ok && name != "" {
		from = name
	}

	to := orchName
	if fmt.Sprintf("%v", msg.To) == humanAddr {
		to = "human"
	}
	isFromMe := from == "human"
	return ChatMessage{
		From:        from,
		To:          to,
		Subject:     msg.Subject,
		Body:        msg.Message,
		Timestamp:   msg.ReceivedAt,
		IsFromMe:    isFromMe,
		IsFromOrch:  !isFromMe,
		Type:        "mail",
		Attachments: msg.Attachments,
		Delivered:   msg.Delivered,
	}
}

func (m *MailModel) advancePollEpoch() {
	m.pollEpoch++
	if m.pollEpoch == 0 {
		m.pollEpoch = 1
	}
}

func (m MailModel) pollLoopCmds() (tea.Cmd, tea.Cmd) {
	return tickEvery(m.pollRate, m.generation, m.pollEpoch), pulseTick(m.generation, m.pollEpoch)
}

func (m *MailModel) restartPollLoop() (tea.Cmd, tea.Cmd) {
	m.advancePollEpoch()
	return m.pollLoopCmds()
}

func (m MailModel) Init() tea.Cmd {
	tickCmd, pulseCmd := m.pollLoopCmds()
	return tea.Batch(
		m.input.Init(),
		// initialRebuild does the one-time authoritative session rebuild off the
		// synchronous launch path (see initialRebuild and NewMailModel). It
		// returns a mailRefreshMsg, so the standard refresh handler builds the
		// view once history is loaded. The periodic tick below then keeps it
		// current via the incremental Refresh path.
		m.initialRebuild,
		tickCmd,
		pulseCmd,
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
		if _, direct := m.currentDirectTarget(); direct {
			return m.updateDirectScroll(msg)
		}
		// Forward scroll wheel events outside the input box to Main's chat viewport.
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
			m.viewport = viewport.New()
			m.viewport.SetWidth(msg.Width)
			m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
			m.ready = true
			m.lastInputLines = -1
			m.syncViewportHeight()
		} else if _, direct := m.currentDirectTarget(); direct {
			if m.viewport.Width() != msg.Width {
				m.directChat.mainViewportDirty = true
			}
			m.lastInputLines = -1 // force active direct height recalculation
			m.syncViewportHeight()
			// Direct content is already hard-wrapped at the Mail child width. A real
			// width change republishes the bounded page once; height-only changes
			// update the stored viewport above without render/SetContent work.
			if m.directChat.renderWidth != msg.Width {
				m.publishDirectViewport(false)
			}
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
		m = m.clampAgentRail()
		return m, nil

	case mailRefreshMsg:
		// Accept the activation before touching any model/cache state. In
		// particular, stale initial rebuilds carry detached SessionCaches that must
		// never replace or persist over the current generation.
		if msg.generation != m.generation {
			return m, nil
		}
		var refreshFollowupCmd tea.Cmd
		if msg.prepared && msg.refreshRequestSerial != 0 && msg.refreshRequestSerial == m.mailRefreshInFlightSerial {
			m.mailRefreshInFlight = false
			m.mailRefreshInFlightSerial = 0
			if m.mailRefreshPending {
				m.mailRefreshPending = false
				m, refreshFollowupCmd = m.issueRefreshRequest()
			}
		}
		// One monotonic request serial, newest-accepted-completion gate: a
		// completion older than the newest accepted request installs no mailbox
		// cache, accepted snapshot, selector rows, publication, or unread work,
		// and schedules no side effect. Serial 0 marks status-only fabricated
		// internal messages and never installs direct state. The one-shot
		// initial rebuild is the single split acceptance: its bounded
		// Main session result below stays generation-gated exactly as before,
		// while its stale mailbox/direct payload is equally rejected.
		staleRequest := msg.refreshRequestSerial != 0 &&
			msg.refreshRequestSerial <= m.acceptedRefreshRequestSerial
		if staleRequest && !msg.initial {
			return m, refreshFollowupCmd
		}
		if !staleRequest && msg.refreshRequestSerial != 0 {
			m.acceptedRefreshRequestSerial = msg.refreshRequestSerial
		}
		acceptedPrepared := !staleRequest &&
			msg.refreshRequestSerial != 0 &&
			msg.prepared &&
			msg.acceptedSnapshot.ready &&
			msg.directPublication != nil
		if acceptedPrepared {
			// The detached command is read-only. Launch this best-effort
			// network/cache refresh only after the acceptance gates; it remains
			// off the Update path.
			go fs.UpdateHumanLocation(m.humanDir)
		}
		var persistCmd tea.Cmd
		if msg.sessionCache != nil {
			m.sessionCache = msg.sessionCache
			// A superseding first frame resets the revealed-extra window; the fresh
			// cache defines what is loaded. No count task is started here or
			// anywhere else — the window is the whole story.
			m.loadedExtra = 0
			generation := msg.generation
			sessionCache := msg.sessionCache
			persistCmd = func() tea.Msg {
				return mailPersistMsg{generation: generation, sessionCache: sessionCache}
			}
		}
		if msg.initial {
			// The deferred initial rebuild has landed — history is now built, so
			// drop the loading banner. Periodic refreshes leave this untouched.
			m.initialLoading = false
		}
		var directVisibilityCmd tea.Cmd
		var directUnreadCmd tea.Cmd
		if !staleRequest {
			if acceptedPrepared {
				// One matching prepared payload installs atomically: mailbox
				// cache, deep accepted snapshot, canonical selector rows, target
				// lifecycle states, and immutable direct publication all belong
				// to this request.
				m.cache = msg.cache
				m.acceptedSnapshot = msg.acceptedSnapshot
				m = m.installSelectorRows(msg.selectorRows)
				m = m.installDirectLifecycleStates(msg.directStates, msg.directLastApiCall)
				m.directPublication = msg.directPublication
				m = m.clampAgentRail()
				m, directVisibilityCmd = m.publishAcceptedDirectSnapshot()
				m = m.recomputeAgentRailUnread()
				m.directUnreadSyncPending = true
				m, directUnreadCmd = m.maybeStartDirectUnreadSync()
			}
			m.orchAlive = msg.alive
			m.orchState = msg.state
			m.orchStaleSeconds = msg.staleSeconds
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
				m.activeSince = msg.lastApiCallAt
				if m.activeSince.IsZero() {
					m.activeSince = time.Now()
				}
			} else if isActive && !msg.lastApiCallAt.IsZero() && !msg.lastApiCallAt.Equal(m.activeSince) {
				// Still active but the kernel reports a different last API call —
				// adopt it so "active N sec" measures seconds since the last tool
				// call (matches the kernel taskcard footer semantics).
				m.activeSince = msg.lastApiCallAt
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
		}
		m.buildMessages()
		// Track /btw inquiry lifecycle
		if !staleRequest && m.orchestrator != "" {
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
			if _, direct := m.currentDirectTarget(); direct {
				// The accepted direct publication above owns any bounded direct
				// repaint. Keep Main's viewport byte-for-byte dormant until return.
				m.directChat.mainViewportDirty = true
			} else {
				atBottom := m.viewport.AtBottom()
				m.syncViewportHeight()
				m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
				if atBottom {
					m.viewport.GotoBottom()
				}
			}
		}
		// Let Bubble Tea paint the accepted history before the derived-cache write.
		// The command itself performs no I/O; mailPersistMsg re-enters Update for a
		// second generation/cache-identity gate and serialized persistence.
		if persistCmd != nil {
			return m, tea.Batch(refreshFollowupCmd, persistCmd, directVisibilityCmd, directUnreadCmd)
		}
		// Kick off the first background telemetry fetch as soon as a refresh has
		// landed (including ordinary refreshes), so the row can appear without
		// waiting a full poll tick. Initial rebuilds schedule it after persistence.
		if cmd := m.maybeScheduleHomeTelemetry(time.Now()); cmd != nil {
			return m, tea.Batch(refreshFollowupCmd, cmd, directVisibilityCmd, directUnreadCmd)
		}
		if directUnreadCmd != nil {
			return m, tea.Batch(refreshFollowupCmd, directVisibilityCmd, directUnreadCmd)
		}
		return m, tea.Batch(refreshFollowupCmd, directVisibilityCmd)

	case mailPersistMsg:
		// Persist only the cache still installed for this activation. This runs on
		// the serialized Update path after the accepted history has been painted,
		// so no stale or concurrent writer can overtake a newer generation.
		if msg.generation != m.generation || msg.sessionCache == nil || msg.sessionCache != m.sessionCache {
			return m, nil
		}
		msg.sessionCache.Persist()
		if cmd := m.maybeScheduleHomeTelemetry(time.Now()); cmd != nil {
			return m, cmd
		}
		return m, nil

	case pulseTickMsg:
		if msg.generation != m.generation || msg.pollEpoch != m.pollEpoch {
			return m, nil
		}
		if strings.EqualFold(m.orchState, "ACTIVE") || strings.EqualFold(m.activeRecipientLifecycle().state, "ACTIVE") {
			m.pulseTick++
		}
		return m, pulseTick(m.generation, msg.pollEpoch)

	case tickMsg:
		if msg.generation != m.generation || msg.pollEpoch != m.pollEpoch {
			return m, nil
		}
		// Steady-state driver: alongside the incremental mail refresh, schedule a
		// background telemetry fetch (debounced by in-flight + TTL). All telemetry
		// I/O funnels through maybeScheduleHomeTelemetry — the UI path never gathers.
		var refreshCmd tea.Cmd
		m, refreshCmd = m.issueRefreshRequest()
		cmds = append(cmds, refreshCmd, tickEvery(m.pollRate, m.generation, msg.pollEpoch))
		if cmd := m.maybeScheduleNetworkActivity(time.Now()); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.maybeScheduleHomeTelemetry(time.Now()); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if cmd := m.maybeScheduleHomeAsyncStats(time.Now()); cmd != nil {
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case networkActivityMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		m.applyNetworkActivity(msg, time.Now())
		return m, nil

	case homeTelemetryMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		// A background telemetry fetch completed. Land the snapshot; only re-sync
		// the viewport height when the row's visibility flipped (data ⇄ no-data),
		// so ordinary numeric updates don't thrash the layout.
		if m.applyHomeTelemetry(msg.t, time.Now()) && m.ready {
			if _, direct := m.currentDirectTarget(); direct {
				m.directChat.mainViewportDirty = true
			} else {
				atBottom := m.viewport.AtBottom()
				m.syncViewportHeight()
				m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
				if atBottom {
					m.viewport.GotoBottom()
				}
			}
		}
		return m, nil

	case homeAsyncStatsMsg:
		if msg.generation != m.generation {
			return m, nil
		}
		// A background async-stats fetch completed. Land the snapshot; re-sync the
		// viewport height only when the row visibility flipped, mirroring telemetry.
		if m.applyHomeAsyncStats(msg.t, time.Now()) && m.ready {
			if _, direct := m.currentDirectTarget(); direct {
				m.directChat.mainViewportDirty = true
			} else {
				atBottom := m.viewport.AtBottom()
				m.syncViewportHeight()
				m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
				if atBottom {
					m.viewport.GotoBottom()
				}
			}
			cmds = append(cmds, m.maybeScheduleHomeAsyncStats(time.Now()))
		}
	case SendMsg:
		var text string
		fromPending := false
		if m.pendingMessage != "" {
			fromPending = true
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
		if target, ok := m.currentDirectTarget(); ok {
			if err := fs.WriteMail(target.Directory, m.humanDir, m.humanAddr, target.Address, "", text); err != nil {
				if fromPending {
					m.pendingMessage = text
				}
				m.input.SetValue(text)
				m.AddSystemMessage(i18n.TF("mail.send_failed", err))
				return m, nil
			}
			m.input.Reset()
			m.syncViewportHeight()
			return m.issueRefreshRequest()
		}
		if m.orchestrator != "" {
			if err := fs.WriteMail(m.orchestrator, m.humanDir, m.humanAddr, m.orchAddr, "", text); err != nil {
				if fromPending {
					m.pendingMessage = text
				}
				m.input.SetValue(text)
				m.AddSystemMessage(i18n.TF("mail.send_failed", err))
				return m, nil
			}
			// Human sent a real message — allow new insight after next idle
			os.Remove(filepath.Join(m.baseDir, ".tui-asset", ".insight.done"))
			m.input.Reset()
			m.syncViewportHeight()
			return m.issueRefreshRequest()
		}
		return m, nil

	case directVisibilityMsg:
		return m.handleDirectVisibility(msg)

	case directUnreadResultMsg:
		var cmd tea.Cmd
		m, cmd = m.handleDirectUnreadResult(msg)
		m = m.recomputeAgentRailUnread()
		return m, cmd

	case OpenEditorMsg:
		// Show editor intro page before launching. The command that emits this
		// message is asynchronous, so an intervening Tab may have focused the
		// conversation rail; the warning must take sole surface ownership.
		m = m.blurAgentRail()
		m.showEditorWarn = true
		m.editorWarnText = msg.Text
		return m, nil

	case EditorDoneMsg:
		if msg.Generation != m.generation {
			return m, nil
		}
		// Launch-context affinity: a completion may install text/pending, show
		// hints, or schedule refresh work only in the exact Main/direct context
		// captured at launch. A direct completion must match the current
		// validated target's project root, stable thread key, and direct
		// generation; a Main context accepts only an untagged completion.
		if target, ok := m.currentDirectTarget(); ok {
			if msg.ProjectRoot != target.ProjectDirectory ||
				msg.DirectThreadKey != m.directChat.threadKey ||
				msg.DirectGeneration != m.directChat.generation {
				return m, nil
			}
		} else if msg.ProjectRoot != "" || msg.DirectThreadKey != "" || msg.DirectGeneration != 0 {
			return m, nil
		}
		m.pendingMessage = msg.Text
		m.input.SetValue(msg.Text)
		m.syncViewportHeight()
		m.maybeShowEditorHint()
		// Refresh viewport and force a full repaint after the terminal returns from
		// the external editor; editors such as vim can leave the alt screen visually
		// stale until Bubble Tea draws a clean frame.
		var refreshCmd tea.Cmd
		m, refreshCmd = m.issueRefreshRequest()
		return m, tea.Batch(refreshCmd, tea.ClearScreen)

	case PaletteSelectMsg:
		m.input.Reset()
		m.syncViewportHeight()
		// Forward to app
		return m, func() tea.Msg { return PaletteSelectMsg{Command: msg.Command} }

	case tea.KeyPressMsg:
		// The /agents overlay owns keys while open, before any other surface.
		if m.agentSelector.selectorOpen {
			return m.updateAgentSelector(msg)
		}
		// Editor warning overlay — Enter proceeds, Esc cancels
		if m.showEditorWarn {
			switch msg.String() {
			case "enter":
				m.showEditorWarn = false
				return m, m.launchEditor(m.editorWarnText)
			case "esc", "ctrl+c":
				m.showEditorWarn = false
				return m, m.currentDirectVisibilityCmd()
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

		if msg.String() == "ctrl+e" {
			// Open the warning in this serialized Mail update before any path can
			// forward the key to InputModel's context-free asynchronous editor
			// command. The exact draft therefore cannot outlive its Mail route.
			m = m.blurAgentRail()
			m.showEditorWarn = true
			m.editorWarnText = m.input.Value()
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
				// The closed palette no longer obscures the direct transcript;
				// re-emit a fresh exact coordinate instead of replaying the one
				// rejected while obscured.
				return m, m.currentDirectVisibilityCmd()
			default:
				// Forward typing to input, then update palette filter
				var cmd tea.Cmd
				m.input, cmd = m.input.Update(msg)
				m.maybeShowEditorHint()
				// Extract filter from input (text after "/")
				val := m.input.Value()
				if len(val) > 1 {
					m.palette.SetFilter(val[1:])
				} else {
					m.palette.SetFilter("")
				}
				m.syncViewportHeight()
				return m, cmd
			}
		}

		// A current direct conversation owns its own scroll surface; these keys
		// navigate the direct viewport instead of Main history or the composer.
		if _, direct := m.currentDirectTarget(); direct {
			switch msg.String() {
			case "pgup", "pgdown", "home", "end", "ctrl+u", "ctrl+d", "ctrl+end":
				return m.updateDirectScroll(msg)
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
			return m.issueRefreshRequest()
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
			// Rebuild the filtered message slice immediately so both content and the
			// full-history older count switch to the new verbosity in the same frame.
			m.buildMessages()
			if m.ready {
				if _, direct := m.currentDirectTarget(); direct {
					m.directChat.mainViewportDirty = true
				} else {
					m.syncViewportHeight()
					m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
					m.viewport.GotoBottom()
				}
				return m, nil
			}
			return m.issueRefreshRequest()

		case "ctrl+u":
			if m.ready && m.viewport.AtTop() {
				// Reveal already-loaded older messages above the render window
				// (cheap, synchronous, no I/O). There is no disk-backed older page:
				// the loaded window IS the history, so Ctrl+U can never pull the
				// view past the fs.RecentMessageLimit() entries already in memory.
				if len(m.visibleMessages()) < len(m.messages) {
					m.loadedExtra += m.pageSize
					m.syncViewportHeight()
					m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
					return m, nil
				}
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
					if _, direct := m.currentDirectTarget(); direct {
						m.directChat.mainViewportDirty = true
					} else {
						m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
						m.viewport.GotoBottom()
					}
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
		if m.syncViewportHeight() && m.viewport.AtBottom() {
			m.viewport.GotoBottom()
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
				// Ctrl+O level 1 shows only the first 150 chars of thinking
				// blocks as a compact preview; level 2 shows the full body.
				if msg.Type == "thinking" && m.verbose == verboseThinking {
					body = truncateToolBody(body, thinkingPreviewLimit)
				}
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
			if msg.Type == "tool_call" && msg.Reasoning != "" {
				reasoning := lipgloss.NewStyle().Width(wrapWidth).Render("[Reasoning] " + msg.Reasoning)
				for _, line := range strings.Split(reasoning, "\n") {
					b.WriteString(thinkingStyle.Render("  "+RuneBullet+" "+line) + "\n")
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
			// Mail is projected from the same accepted mailbox snapshot in every layer,
			// so keep its row renderer identical when Ctrl+O adds event history around it.
			ts := ""
			if msg.Timestamp != "" {
				if t, err := time.Parse(time.RFC3339Nano, msg.Timestamp); err == nil {
					ts = StyleFaint.Render(" " + t.Local().Format("2006-01-02 15:04 MST"))
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

func (m MailModel) installDirectLifecycleStates(states map[string]agentLifecycleState, lastApiCall map[string]time.Time) MailModel {
	now := time.Now()
	next := make(map[string]directAgentLifecycle, len(states))
	for key, lifecycle := range states {
		status := m.directLifecycles[key]
		if strings.EqualFold(lifecycle.state, "ACTIVE") {
			if !strings.EqualFold(status.state, "ACTIVE") || status.activeSince.IsZero() {
				status.activeSince = lastApiCall[key]
				if status.activeSince.IsZero() {
					status.activeSince = now
				}
			} else if lp, ok := lastApiCall[key]; ok && !lp.IsZero() && !lp.Equal(status.activeSince) {
				// Still active but the target reports a different last progress —
				// adopt it (matches kernel taskcard footer semantics).
				status.activeSince = lp
			}
		} else {
			status.activeSince = time.Time{}
		}
		status.state = lifecycle.state
		status.staleSeconds = lifecycle.staleSeconds
		next[key] = status
	}
	m.directLifecycles = next
	return m
}

func (m MailModel) activeRecipientLifecycle() directAgentLifecycle {
	if target, ok := m.currentDirectTarget(); ok {
		return m.directLifecycles[fs.DirectThreadKey(target)]
	}
	return directAgentLifecycle{state: m.orchState, activeSince: m.activeSince, staleSeconds: m.orchStaleSeconds}
}

// stateGlyphFor returns the leading glyph for an agent-state badge. ACTIVE uses
// the rotating spinner frame so the badge visibly animates in normal mode;
// every other state gets a distinct static glyph (color carries the rest of
// the distinction via StateColor).
func (m MailModel) stateGlyphFor(state string) string {
	switch strings.ToUpper(state) {
	case "ACTIVE":
		return spinnerFrames[m.pulseTick%len(spinnerFrames)]
	case "ASLEEP":
		return "◌"
	case "SUSPENDED":
		return "○"
	case "STALE":
		return "◉"
	case "REFRESHING":
		return "⟳"
	default: // IDLE, STUCK, unknown
		return "◉"
	}
}

func (m MailModel) stateGlyph() string {
	return m.stateGlyphFor(m.orchState)
}

func activeElapsedFor(state string, activeSince time.Time) string {
	if !strings.EqualFold(state, "ACTIVE") || activeSince.IsZero() {
		return ""
	}
	d := time.Since(activeSince)
	return fmt.Sprintf(" %ds", int(d.Seconds()))
}

func staleElapsedFor(state string, staleSeconds int) string {
	if !strings.EqualFold(state, "STALE") || staleSeconds < 0 {
		return ""
	}
	return fmt.Sprintf(" %ds", staleSeconds)
}

// activeElapsed returns a short " 12s" / " 240s" suffix while Main is ACTIVE,
// or "" otherwise — the "how long has it been working" signal, always in
// whole seconds so the value matches the Telegram Task Card footer's
// "active (N s)" (Jason 2026-08-16).
func (m MailModel) activeElapsed() string {
	return activeElapsedFor(m.orchState, m.activeSince)
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
	generation := m.generation
	// Capture the launch context: a direct launch tags the completion with the
	// validated target's coordinates, a Main launch leaves them empty/zero.
	var projectRoot, directThreadKey string
	var directGeneration uint64
	if target, ok := m.currentDirectTarget(); ok {
		projectRoot = target.ProjectDirectory
		directThreadKey = m.directChat.threadKey
		directGeneration = m.directChat.generation
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
		return EditorDoneMsg{
			Text:             string(content),
			Generation:       generation,
			ProjectRoot:      projectRoot,
			DirectThreadKey:  directThreadKey,
			DirectGeneration: directGeneration,
		}
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

func (m MailModel) ordinaryHeaderVisible() bool {
	return m.ready && !m.showEditorWarn && !m.agentSelector.selectorOpen
}

func (m MailModel) View() string {
	return m.view(false)
}

func (m MailModel) view(showAgentRailExpandControl bool) string {
	if m.showEditorWarn {
		return m.viewEditorWarn()
	}
	if !m.ready {
		return "\n  " + i18n.T("app.loading")
	}
	_, direct := m.currentDirectTarget()

	// Build header: left = app title, center = thinking quote, right = agent [state]
	brand := i18n.T("app.brand")
	titleLeft := StyleTitle.Render("  " + brand)
	if showAgentRailExpandControl {
		titleLeft = StyleSubtle.Render(agentRailExpandControl) + titleLeft
	}
	if m.visitExitHint {
		titleLeft += " " + StyleSubtle.Render(i18n.T("mail.visit_exit_hint"))
	}

	// Resolve the one visible recipient and render its state through Main's
	// existing badge language. Direct selection changes the subject, not the UI.
	recipientLifecycle := m.activeRecipientLifecycle()
	stateKey := recipientLifecycle.state
	if stateKey == "" {
		stateKey = "unknown"
	}
	stateLabel := i18n.T("state." + stateKey)
	stateStyle := lipgloss.NewStyle().Foreground(StateColor(strings.ToUpper(stateKey)))
	orchNameStyle := lipgloss.NewStyle().Foreground(ColorText).Bold(true)
	titleRightBase := orchNameStyle.Render(m.activeRecipientLabel())
	titleRightBase += " " + stateStyle.Render("◉ "+stateLabel)

	// Thinking indicator: fixed quote per ACTIVE session, pulsing color + spinners
	titleCenter := ""
	if !direct && strings.EqualFold(m.orchState, "ACTIVE") {
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
	if badge := m.networkActivityBadge(); !direct && badge != "" {
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
	toLabel := StyleFaint.Render("Email To: ") + lipgloss.NewStyle().Foreground(ColorAgent).Render(m.activeRecipientLabel())
	indicator := stateStyle.Render(m.stateGlyphFor(recipientLifecycle.state) + " " + stateLabel + activeElapsedFor(recipientLifecycle.state, recipientLifecycle.activeSince) + staleElapsedFor(recipientLifecycle.state, recipientLifecycle.staleSeconds))
	toLabel += "  " + indicator + " "
	sepWidth := m.width - lipgloss.Width(toLabel)
	if sepWidth < 0 {
		sepWidth = 0
	}
	sep := toLabel + strings.Repeat("\u2500", sepWidth)
	inputSection := m.input.View()
	if m.input.IsPaletteActive() {
		if paletteView := m.palette.View(); paletteView != "" {
			inputSection = paletteView + "\n" + inputSection
		}
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
	} else if !direct && (m.inquiryState == "sent" || m.inquiryState == "taken") {
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
	// Gate on hasHomeTelemetry() (which carries the homeTelemetryLoaded guard and
	// the selected expression) so View and syncViewportHeight share the exact same
	// visibility predicate and can never disagree about whether the row occupies a
	// line. It is true exactly when formatHomeTelemetry returns a row.
	if !direct && m.hasHomeTelemetry() {
		footer += formatHomeTelemetry(m.homeTelemetry, m.width, m.homeTelemetryDisplay) + "\n"
	}
	if !direct && m.hasHomeAsyncStats() {
		if async := formatHomeAsyncStats(m.homeAsyncStats, m.width); async != "" {
			footer += async + "\n"
		}
	}
	footer += statusBar

	// Main history banners never belong to the strict current direct projection.
	// Top banner: the one-time loading line while the deferred initial session
	// rebuild is still pending. There is no older-history banner — the page never
	// computes how much history exists, so it has no total to announce.
	topBanner := ""
	bottomBanner := ""
	if !direct {
		if m.initialLoading {
			loadingText := i18n.T("mail.initial_loading")
			topBanner = StyleFaint.Render(centerText(loadingText, m.width)) + "\n"
		}
		// Bottom banner: "▼ ctrl+d to collapse to recent"
		if m.loadedExtra > 0 {
			bannerText := i18n.T("mail.collapse")
			bottomBanner = StyleFaint.Render(centerText(bannerText, m.width)) + "\n"
		}
	}

	// The open /agents overlay replaces the frame; a validated direct selection
	// composes only its accepted projection through a viewport value copy,
	// preserving Main's rich viewport content and scroll for return to Main.
	if m.agentSelector.selectorOpen {
		return m.renderAgentSelector()
	}
	return header + "\n" + topBanner + PaintViewportBG(m.activeViewportView(), m.width) + "\n" + bottomBanner + footer
}
