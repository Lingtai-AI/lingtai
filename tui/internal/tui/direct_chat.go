package tui

import (
	"path/filepath"
	"strings"
	"time"

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
	scrollOffset           int
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
	m.directChat = directChatState{
		target:                 target,
		threadKey:              threadKey,
		generation:             generation,
		acceptedSnapshotSerial: m.acceptedSnapshotSerial,
		projection:             projectDirectMessages(m.humanAddr, target, accepted),
		mainInput:              m.directChat.mainInput,
		mainPendingMessage:     m.directChat.mainPendingMessage,
		mainComposeStored:      m.directChat.mainComposeStored,
	}
	m.agentSelector.selectedThreadKey = threadKey
	m.lastInputLines = -1
	m.syncViewportHeight()
	m.directChat.scrollOffset = m.directScrollBottom()
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

	accepted := m.acceptedSnapshot.messagesForUnread(m.humanDir)
	m.directChat.projection = projectDirectMessages(m.humanAddr, m.directChat.target, accepted)
	m.directChat.acceptedSnapshotSerial = m.acceptedSnapshotSerial
	m.directChat.scrollOffset = m.directScrollBottom()
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

// activeViewportView renders the selected direct projection through a value
// copy of Main's viewport. The copy reads the one current direct offset,
// leaving Main's rich content and viewport position untouched for return to
// Main.
func (m MailModel) activeViewportView() string {
	if _, ok := m.currentDirectTarget(); !ok {
		return m.viewportWithChatTailHint()
	}
	directViewport := m.viewport
	directViewport.SetContent(m.renderMessages(m.directChat.projection))
	directViewport.SetYOffset(m.directChat.scrollOffset)
	return directViewport.View()
}

// directScrollBottom establishes the fresh current projection at its tail. It
// is used only when a direct projection is newly published, never by View.
func (m MailModel) directScrollBottom() int {
	directViewport := m.viewport
	directViewport.SetContent(m.renderMessages(m.directChat.projection))
	directViewport.GotoBottom()
	return directViewport.YOffset()
}

// updateDirectScroll navigates a transient direct viewport and persists only
// its scalar current offset. Main's viewport, rich messages, and history state
// stay completely untouched.
func (m MailModel) updateDirectScroll(msg tea.Msg) (MailModel, tea.Cmd) {
	if _, ok := m.currentDirectTarget(); !ok || !m.ready {
		return m, nil
	}
	directViewport := m.viewport
	directViewport.SetContent(m.renderMessages(m.directChat.projection))
	directViewport.SetYOffset(m.directChat.scrollOffset)
	if key, ok := msg.(tea.KeyPressMsg); ok {
		switch key.String() {
		case "home":
			directViewport.GotoTop()
		case "end", "ctrl+end":
			directViewport.GotoBottom()
		default:
			var cmd tea.Cmd
			directViewport, cmd = directViewport.Update(msg)
			m.directChat.scrollOffset = directViewport.YOffset()
			return m, cmd
		}
	} else {
		var cmd tea.Cmd
		directViewport, cmd = directViewport.Update(msg)
		m.directChat.scrollOffset = directViewport.YOffset()
		return m, cmd
	}
	m.directChat.scrollOffset = directViewport.YOffset()
	return m, nil
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

// projectDirectMessages is the owner-neutral direct-mail projection seam. It
// only filters the accepted detached snapshot and converts its matching mail;
// it owns no state, lifecycle, or I/O behavior.
func projectDirectMessages(humanAddr string, target fs.DirectTarget, accepted []fs.MailMessage) []ChatMessage {
	projected := make([]ChatMessage, 0, len(accepted))
	for _, message := range accepted {
		if fs.IsDirectMail(message, humanAddr, target) {
			projected = append(projected, mailMessageToChatMessage(message, humanAddr, target.Address))
		}
	}
	return projected
}
