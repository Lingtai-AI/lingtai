package tui

import (
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// directChatState is the one current-only direct-view state owned by MailModel.
// There is exactly one; switching targets replaces it and returning to Main
// discards it. It is never cached or indexed per target.
type directChatState struct {
	target                 fs.DirectTarget
	threadKey              string
	generation             uint64
	acceptedSnapshotSerial uint64
	projection             []ChatMessage
	viewport               viewport.Model
	revealHorizon          int
	hasOlder               bool
	renderWidth            int
	mainViewportDirty      bool
	mainInput              InputModel
	mainPendingMessage     string
	mainComposeStored      bool
}

// directVisibilityMsg is the guarded, deferred visibility coordinate produced
// by direct activation and accepted republication. Selection alone never
// acknowledges visibility; the durable unread acknowledgement that consumes
// this exact coordinate lands behind its own visibility gate.
type directVisibilityMsg struct {
	projectRoot            string
	threadKey              string
	directGeneration       uint64
	acceptedSnapshotSerial uint64
}

// activateDirectTarget installs one valid current-project direct projection.
// The returned command defers visibility acknowledgement so selection alone
// never advances durable unread state.
func (m MailModel) activateDirectTarget(target fs.DirectTarget) (MailModel, tea.Cmd) {
	projectRoot, threadKey, ok := m.directTargetCoordinates(target)
	if !ok {
		return m, nil
	}

	generation := nextDirectGeneration(m.directChat.generation)
	accepted := m.acceptedSnapshot.messagesForUnread(m.humanDir)
	projection, hasOlder := projectDirectMessages(m.humanAddr, target, accepted, m.pageSize)
	directViewport := viewport.New()
	directViewport.SetWidth(m.width)
	directViewport.SetHeight(m.viewport.Height())
	m.directChat = directChatState{
		target:                 target,
		threadKey:              threadKey,
		generation:             generation,
		acceptedSnapshotSerial: m.acceptedSnapshotSerial,
		projection:             projection,
		viewport:               directViewport,
		revealHorizon:          m.pageSize,
		hasOlder:               hasOlder,
		mainViewportDirty:      m.directChat.mainViewportDirty,
		mainInput:              m.directChat.mainInput,
		mainPendingMessage:     m.directChat.mainPendingMessage,
		mainComposeStored:      m.directChat.mainComposeStored,
	}
	m.agentSelector.selectedThreadKey = threadKey
	m.lastInputLines = -1
	m.syncViewportHeight()
	m.publishDirectViewport(true)
	return m, deferredDirectVisibilityCmd(projectRoot, threadKey, generation, m.acceptedSnapshotSerial)
}

// installFreshDirectCompose moves the complete Main input aside without
// mutating it, then installs one newly allocated compose model for the current
// direct target. A target switch replaces that direct model instead of caching
// it by target.
func (m MailModel) installFreshDirectCompose() MailModel {
	if !m.directChat.mainComposeStored {
		m.directChat.mainInput = m.input
		m.directChat.mainPendingMessage = m.pendingMessage
		m.directChat.mainComposeStored = true
	}

	focused := m.input.Focused()
	width := m.input.width
	maxHeight := m.input.MaxHeight()
	directInput := NewInputModel(m.humanDir)
	directInput.SetWidth(width)
	directInput.SetMaxHeight(maxHeight)
	if focused {
		_ = directInput.Focus()
	} else {
		directInput.Blur()
	}
	m.input = directInput
	m.pendingMessage = ""
	return m
}

// restoreMainCompose returns the exact stored Main input and pending editor
// text. The active direct input is discarded; it is never retained per target.
func (m MailModel) restoreMainCompose() MailModel {
	if !m.directChat.mainComposeStored {
		return m
	}
	m.input = m.directChat.mainInput
	m.pendingMessage = m.directChat.mainPendingMessage
	if m.ready && (m.directChat.mainViewportDirty || m.viewport.Width() != m.width) {
		atBottom := m.viewport.AtBottom()
		offset := m.viewport.YOffset()
		m.viewport.SetWidth(m.width)
		m.viewport.SetContent(m.renderMessages(m.visibleMessages()))
		if atBottom {
			m.viewport.GotoBottom()
		} else {
			m.viewport.SetYOffset(offset)
		}
	}
	return m
}

func clearedDirectChat(previous directChatState) directChatState {
	// Keep the monotonic generation counter so Main -> same target cannot allow a
	// delayed coordinate from the previous direct activation to become current.
	return directChatState{generation: previous.generation}
}

// publishAcceptedDirectSnapshot advances the accepted publication coordinate,
// reconciles the current selection against the accepted safe rows, and
// republishes only a still-current direct selection. It never marks mail seen;
// the fresh deferred coordinate carries the new accepted serial instead.
func (m MailModel) publishAcceptedDirectSnapshot() (MailModel, tea.Cmd) {
	m.acceptedSnapshotSerial = nextAcceptedSnapshotSerial(m.acceptedSnapshotSerial)
	m = m.reconcileAcceptedDirectSelection()

	projectRoot, threadKey, ok := m.directTargetCoordinates(m.directChat.target)
	if !ok || m.directChat.threadKey != threadKey ||
		m.agentSelector.selectedThreadKey != threadKey ||
		m.directChat.generation == 0 {
		return m, nil
	}

	horizon := m.directChat.revealHorizon
	if horizon <= 0 {
		horizon = m.pageSize
	}
	accepted := m.acceptedSnapshot.messagesForUnread(m.humanDir)
	projection, hasOlder := projectDirectMessages(m.humanAddr, m.directChat.target, accepted, horizon)
	pageChanged := !reflect.DeepEqual(projection, m.directChat.projection)
	widthChanged := m.directChat.renderWidth != m.width
	m.directChat.projection = projection
	m.directChat.revealHorizon = horizon
	m.directChat.hasOlder = hasOlder
	m.directChat.acceptedSnapshotSerial = m.acceptedSnapshotSerial
	if pageChanged || widthChanged {
		m.publishDirectViewport(pageChanged)
	}
	return m, deferredDirectVisibilityCmd(projectRoot, threadKey, m.directChat.generation, m.acceptedSnapshotSerial)
}

// reconcileAcceptedDirectSelection rebinds a still-selected stable thread key
// to its exactly-one accepted safe row — the canonical rediscovered target may
// carry a new directory/address — before direct projection or send can use it.
// A selected key that is absent or ambiguous in the accepted rows fails closed
// to Main within the same accepted publication: selection and direct state are
// cleared, the exact stored Main compose returns, and the active viewport is
// recalculated so a stale direct send is impossible. The durable unread store
// is deliberately untouched.
func (m MailModel) reconcileAcceptedDirectSelection() MailModel {
	key := m.agentSelector.selectedThreadKey
	if key == "" && m.directChat.threadKey == "" {
		return m
	}
	if key != "" && key == m.directChat.threadKey {
		matches := 0
		var rebound fs.DirectTarget
		for _, row := range m.agentSelector.rows {
			if !row.Main && fs.DirectThreadKey(row.Target) == key {
				matches++
				rebound = row.Target
			}
		}
		if matches == 1 {
			m.directChat.target = rebound
			return m
		}
	}
	m.agentSelector.selectedThreadKey = ""
	m = m.restoreMainCompose()
	m.directChat = clearedDirectChat(m.directChat)
	m.lastInputLines = -1
	m.syncViewportHeight()
	return m
}

// syncAcceptedDirectUnread opens or synchronizes the one Mail-owned durable
// direct unread store from the accepted publication path only. It hands the
// store the FULL accepted snapshot so newly discovered targets baseline at the
// accepted boundary; it never counts for display, renders, or marks seen. The
// canonical selector rows remain the only target discovery.
func (m MailModel) syncAcceptedDirectUnread() MailModel {
	if strings.TrimSpace(m.baseDir) == "" {
		return m
	}
	projectRoot := filepath.Dir(filepath.Clean(m.baseDir))
	if projectRoot == "." {
		return m
	}
	targets := make([]fs.DirectTarget, 0, len(m.agentSelector.rows))
	for _, row := range m.agentSelector.rows {
		if !row.Main {
			targets = append(targets, row.Target)
		}
	}
	accepted := m.acceptedSnapshot.messagesForUnread(m.humanDir)
	store := m.directUnread
	var err error
	if store == nil || m.directUnreadProjectRoot != projectRoot {
		store, err = fs.OpenDirectUnreadStore(projectRoot, m.humanAddr, targets, accepted)
	} else {
		err = store.SyncTargets(targets, accepted)
	}
	if err != nil {
		m.statusFlash = i18n.T("agent_selector.unread_failed")
		m.statusExpiry = time.Now().Add(5 * time.Second)
		return m
	}
	m.directUnread = store
	m.directUnreadProjectRoot = projectRoot
	return m
}

// handleDirectVisibility accepts only an exact current coordinate — project
// root, strict thread key, direct generation, and accepted snapshot serial all
// matching the current selection — while Mail is ready and the direct
// transcript is unobscured. Only then may the durable store MarkSeen, from the
// FULL accepted snapshot. Everything else fails closed without durable changes.
func (m MailModel) handleDirectVisibility(msg directVisibilityMsg) MailModel {
	projectRoot, threadKey, ok := m.directTargetCoordinates(m.directChat.target)
	if !m.ready || m.directVisibilityObscured() || !ok ||
		msg.projectRoot == "" || msg.threadKey == "" || msg.directGeneration == 0 ||
		msg.acceptedSnapshotSerial == 0 || msg.projectRoot != projectRoot ||
		msg.threadKey != threadKey || msg.directGeneration != m.directChat.generation ||
		msg.acceptedSnapshotSerial != m.acceptedSnapshotSerial ||
		m.directChat.threadKey != threadKey ||
		m.directChat.acceptedSnapshotSerial != m.acceptedSnapshotSerial ||
		m.agentSelector.selectedThreadKey != threadKey || m.directUnread == nil {
		return m
	}
	accepted := m.acceptedSnapshot.messagesForUnread(m.humanDir)
	if err := m.directUnread.MarkSeen(m.directChat.target, accepted); err != nil {
		m.statusFlash = i18n.T("agent_selector.unread_failed")
		m.statusExpiry = time.Now().Add(5 * time.Second)
	}
	return m
}

// directVisibilityObscured names the Mail-owned surfaces that cover the direct
// transcript. An obscured coordinate is rejected, never queued; closing the
// obstruction emits a fresh current coordinate instead.
func (m MailModel) directVisibilityObscured() bool {
	return m.agentSelector.selectorOpen || m.showEditorWarn || m.input.IsPaletteActive()
}

// currentDirectVisibilityCmd creates a fresh coordinate only for the exact
// accepted current direct selection. Rejected coordinates are deliberately
// never replayed.
func (m MailModel) currentDirectVisibilityCmd() tea.Cmd {
	target, ok := m.currentDirectTarget()
	if !ok {
		return nil
	}
	return deferredDirectVisibilityCmd(target.ProjectDirectory, m.directChat.threadKey, m.directChat.generation, m.acceptedSnapshotSerial)
}

// directTargetCoordinates validates the selected target against the MailModel's
// current project root before it can affect direct state or a deferred
// visibility coordinate.
func (m MailModel) directTargetCoordinates(target fs.DirectTarget) (string, string, bool) {
	if strings.TrimSpace(m.baseDir) == "" ||
		strings.TrimSpace(target.ProjectDirectory) == "" ||
		strings.TrimSpace(target.Directory) == "" ||
		strings.TrimSpace(target.AgentID) == "" ||
		strings.TrimSpace(target.Address) == "" {
		return "", "", false
	}
	projectRoot := filepath.Dir(filepath.Clean(m.baseDir))
	if projectRoot == "." || target.ProjectDirectory != projectRoot {
		return "", "", false
	}
	threadKey := fs.DirectThreadKey(target)
	if threadKey == "" {
		return "", "", false
	}
	return projectRoot, threadKey, true
}

// currentDirectTarget validates that the one direct state still names the
// current MailModel project, canonical selection, and accepted publication. It
// is a read-only seam shared by rendering and sending; it never discovers
// agents or changes Main.
func (m MailModel) currentDirectTarget() (fs.DirectTarget, bool) {
	target := m.directChat.target
	_, threadKey, ok := m.directTargetCoordinates(target)
	if !ok || m.directChat.generation == 0 || m.directChat.threadKey != threadKey ||
		m.agentSelector.selectedThreadKey != threadKey ||
		m.directChat.acceptedSnapshotSerial != m.acceptedSnapshotSerial {
		return fs.DirectTarget{}, false
	}
	return target, true
}

// activeRecipientLabel returns the direct address only while the current direct
// selection validates. Main otherwise retains its existing recipient label.
func (m MailModel) activeRecipientLabel() string {
	if target, ok := m.currentDirectTarget(); ok {
		return target.Address
	}
	return m.orchDisplayName()
}

// publishDirectViewport is the only direct render/SetContent path. Content
// changes follow the established tail policy; width-only reflow preserves an
// existing bottom anchor and otherwise keeps the current scalar offset.
func (m *MailModel) publishDirectViewport(goToTail bool) {
	atBottom := m.directChat.viewport.AtBottom()
	offset := m.directChat.viewport.YOffset()
	m.directChat.viewport.SetWidth(m.width)
	m.directChat.viewport.SetContent(m.renderMessages(m.directChat.projection))
	m.directChat.renderWidth = m.width
	if goToTail || atBottom {
		m.directChat.viewport.GotoBottom()
	} else {
		m.directChat.viewport.SetYOffset(offset)
	}
}

// activeViewportView reads only the installed current direct viewport. Main
// keeps its own rich content and viewport position for return to Main.
func (m MailModel) activeViewportView() string {
	if _, ok := m.currentDirectTarget(); !ok {
		return m.viewportWithChatTailHint()
	}
	return m.directChat.viewport.View()
}

// revealOlderDirectPage synchronously expands only the current direct
// projection by one accepted-snapshot page. It owns no cache, job, or I/O path;
// Main history and every other target remain untouched. The caller's top anchor
// is restored after the single direct publish.
func (m MailModel) revealOlderDirectPage(target fs.DirectTarget) (MailModel, bool) {
	if !m.directChat.viewport.AtTop() || !m.directChat.hasOlder || m.pageSize <= 0 {
		return m, false
	}
	horizon := m.directChat.revealHorizon
	if horizon <= 0 {
		horizon = m.pageSize
	}
	nextHorizon := horizon + m.pageSize
	if nextHorizon <= horizon {
		return m, false
	}
	accepted := m.acceptedSnapshot.messagesForUnread(m.humanDir)
	projection, hasOlder := projectDirectMessages(m.humanAddr, target, accepted, nextHorizon)
	m.directChat.projection = projection
	m.directChat.revealHorizon = nextHorizon
	m.directChat.hasOlder = hasOlder
	m.publishDirectViewport(false)
	m.directChat.viewport.GotoTop()
	return m, true
}

// updateDirectScroll navigates the stored current direct viewport directly.
// Ordinary scrolling never renders or publishes content and never mutates
// Main's viewport. Ctrl+U at the top may explicitly reveal one accepted page.
func (m MailModel) updateDirectScroll(msg tea.Msg) (MailModel, tea.Cmd) {
	target, ok := m.currentDirectTarget()
	if !ok || !m.ready {
		return m, nil
	}
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "home":
			m.directChat.viewport.GotoTop()
			return m, nil
		case "end", "ctrl+end":
			m.directChat.viewport.GotoBottom()
			return m, nil
		case "ctrl+u":
			if revealed, ok := m.revealOlderDirectPage(target); ok {
				return revealed, nil
			}
		}
	}
	var cmd tea.Cmd
	m.directChat.viewport, cmd = m.directChat.viewport.Update(msg)
	return m, cmd
}

func deferredDirectVisibilityCmd(projectRoot, threadKey string, directGeneration, acceptedSnapshotSerial uint64) tea.Cmd {
	return func() tea.Msg {
		return directVisibilityMsg{
			projectRoot:            projectRoot,
			threadKey:              threadKey,
			directGeneration:       directGeneration,
			acceptedSnapshotSerial: acceptedSnapshotSerial,
		}
	}
}

func nextDirectGeneration(current uint64) uint64 {
	next := current + 1
	if next == 0 {
		return 1
	}
	return next
}

func nextAcceptedSnapshotSerial(current uint64) uint64 {
	next := current + 1
	if next == 0 {
		return 1
	}
	return next
}

// projectDirectMessages is the owner-neutral bounded direct-mail projection
// seam. It walks the accepted chronological snapshot newest-first, examines at
// most horizon+1 strict matches to derive hasOlder, converts only the retained
// horizon, then restores chronological order. It owns no I/O behavior and never
// truncates the accepted snapshot used by selector/unread/MarkSeen.
func projectDirectMessages(humanAddr string, target fs.DirectTarget, accepted []fs.MailMessage, horizon int) ([]ChatMessage, bool) {
	if horizon < 1 {
		return nil, false
	}
	capacity := min(horizon, len(accepted))
	projected := make([]ChatMessage, 0, capacity)
	hasOlder := false
	for index := len(accepted) - 1; index >= 0; index-- {
		message := accepted[index]
		if !fs.IsDirectMail(message, humanAddr, target) {
			continue
		}
		if len(projected) == horizon {
			hasOlder = true
			break
		}
		projected = append(projected, mailMessageToChatMessage(message, humanAddr, target.Address))
	}
	for left, right := 0, len(projected)-1; left < right; left, right = left+1, right-1 {
		projected[left], projected[right] = projected[right], projected[left]
	}
	return projected, hasOlder
}
