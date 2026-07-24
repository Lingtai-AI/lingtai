package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const directUnreadStateVersion = 1

// DirectUnreadStore owns one project's durable direct-thread unread cursors.
type DirectUnreadStore struct {
	mu               sync.RWMutex
	projectDirectory string
	humanAddress     string
	statePath        string
	state            directUnreadState
}

type directUnreadState struct {
	Version int                           `json:"version"`
	Threads map[string]directUnreadThread `json:"threads"`
}

type directUnreadThread struct {
	AgentID         string   `json:"agent_id"`
	ReceivedAt      string   `json:"received_at"`
	IDsAtReceivedAt []string `json:"ids_at_received_at"`
}

type directUnreadCursor struct {
	receivedAt time.Time
	ids        []string
}

type directUnreadMessage struct {
	id string
	at time.Time
}

var directUnreadPathLocks sync.Map // canonical state path -> *sync.Mutex

func directUnreadPathMutex(path string) *sync.Mutex {
	key, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		key = filepath.Clean(path)
	}
	lock, _ := directUnreadPathLocks.LoadOrStore(key, &sync.Mutex{})
	return lock.(*sync.Mutex)
}

// Direct-unread transactions always acquire locks in this order: canonical
// path mutex, stable sibling OS lock, then store.mu. They release in reverse.
func withDirectUnreadTransaction(path string, transaction func() error) (err error) {
	processLock := directUnreadPathMutex(path)
	processLock.Lock()
	defer processLock.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create direct unread state parent: %w", err)
	}
	lockPath := path + ".lock"
	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open direct unread transaction lock: %w", err)
	}
	if err := lockFileExclusive(file); err != nil {
		_ = file.Close()
		return fmt.Errorf("lock direct unread transaction: %w", err)
	}
	defer func() {
		if unlockErr := unlockFile(file); err == nil && unlockErr != nil {
			err = fmt.Errorf("unlock direct unread transaction: %w", unlockErr)
		}
		if closeErr := file.Close(); err == nil && closeErr != nil {
			err = fmt.Errorf("close direct unread transaction lock: %w", closeErr)
		}
	}()
	return transaction()
}

// refreshedDirectUnreadState chooses a valid on-disk snapshot as the mutation
// baseline. Recoverable absent/malformed/unsupported state keeps valid memory
// and requests a repair write; all other read errors fail closed.
func refreshedDirectUnreadState(path string, memory directUnreadState) (directUnreadState, bool, error) {
	disk, recoverable, err := readDirectUnreadState(path)
	if err != nil {
		return directUnreadState{}, false, err
	}
	if recoverable || disk.Threads == nil {
		return cloneDirectUnreadState(memory), true, nil
	}
	return cloneDirectUnreadState(disk), false, nil
}

// OpenDirectUnreadStore opens version-1 state and baselines any missing,
// corrupt, unsupported, or target-invalid cursor from accepted direct mail.
func OpenDirectUnreadStore(projectDirectory, humanAddress string, targets []DirectTarget, accepted []MailMessage) (*DirectUnreadStore, error) {
	if strings.TrimSpace(projectDirectory) == "" || strings.TrimSpace(humanAddress) == "" {
		return nil, fmt.Errorf("direct unread project directory or human address is empty")
	}
	if err := validateDirectUnreadTargets(projectDirectory, targets); err != nil {
		return nil, err
	}

	path := filepath.Join(projectDirectory, ".lingtai", ".tui-asset", "direct-unread.json")
	var opened directUnreadState
	if err := withDirectUnreadTransaction(path, func() error {
		state, fresh, err := readDirectUnreadState(path)
		if err != nil {
			return err
		}
		changed := fresh
		if state.Threads == nil {
			state.Threads, changed = make(map[string]directUnreadThread), true
		}
		next := cloneDirectUnreadState(state)
		needsBaseline := make([]DirectTarget, 0, len(targets))
		for _, target := range targets {
			key := DirectThreadKey(target)
			thread, exists := next.Threads[key]
			_, valid := directUnreadCursorForThread(thread)
			if !exists || thread.AgentID != target.AgentID || !valid {
				needsBaseline = append(needsBaseline, target)
			}
		}
		if len(needsBaseline) != 0 {
			baselines, err := directUnreadBaselines(needsBaseline, accepted, humanAddress)
			if err != nil {
				return err
			}
			for _, target := range needsBaseline {
				next.Threads[DirectThreadKey(target)] = directUnreadThreadForCursor(target.AgentID, baselines[DirectThreadKey(target)])
			}
			changed = true
		}
		if changed {
			if err := saveDirectUnreadState(path, next); err != nil {
				return err
			}
		}
		opened = cloneDirectUnreadState(next)
		return nil
	}); err != nil {
		return nil, err
	}
	return &DirectUnreadStore{projectDirectory: projectDirectory, humanAddress: humanAddress, statePath: path, state: opened}, nil
}

// SyncTargets adds and baselines genuinely new stable keys; it never prunes.
func (s *DirectUnreadStore) SyncTargets(targets []DirectTarget, accepted []MailMessage) error {
	if s == nil {
		return fmt.Errorf("sync direct unread targets: nil store")
	}
	if err := validateDirectUnreadTargets(s.projectDirectory, targets); err != nil {
		return err
	}
	return withDirectUnreadTransaction(s.statePath, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		base, repair, err := refreshedDirectUnreadState(s.statePath, s.state)
		if err != nil {
			return err
		}
		newTargets := make([]DirectTarget, 0, len(targets))
		for _, target := range targets {
			key := DirectThreadKey(target)
			thread, exists := base.Threads[key]
			if !exists {
				newTargets = append(newTargets, target)
				continue
			}
			if thread.AgentID != target.AgentID {
				return fmt.Errorf("sync direct unread target %q: inconsistent agent ID", key)
			}
			if _, valid := directUnreadCursorForThread(thread); !valid {
				return fmt.Errorf("sync direct unread target %q: invalid stored boundary", key)
			}
		}
		next := cloneDirectUnreadState(base)
		if len(newTargets) != 0 {
			baselines, err := directUnreadBaselines(newTargets, accepted, s.humanAddress)
			if err != nil {
				return err
			}
			for _, target := range newTargets {
				next.Threads[DirectThreadKey(target)] = directUnreadThreadForCursor(target.AgentID, baselines[DirectThreadKey(target)])
			}
		}
		if repair || len(newTargets) != 0 {
			if err := saveDirectUnreadState(s.statePath, next); err != nil {
				return err
			}
		}
		s.state = cloneDirectUnreadState(next)
		return nil
	})
}

// UnreadCount counts unique accepted incoming direct messages after the cursor.
func (s *DirectUnreadStore) UnreadCount(target DirectTarget, accepted []MailMessage) (int, error) {
	if s == nil {
		return 0, fmt.Errorf("count direct unread: nil store")
	}
	if err := validateDirectUnreadTarget(s.projectDirectory, target); err != nil {
		return 0, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	thread, err := s.threadForTargetLocked(target)
	if err != nil {
		return 0, err
	}
	cursor, valid := directUnreadCursorForThread(thread)
	if !valid {
		return 0, fmt.Errorf("count direct unread target %q: invalid stored boundary", DirectThreadKey(target))
	}
	messages, err := resolveDirectUnreadMessages(accepted, s.humanAddress, target)
	if err != nil {
		return 0, err
	}
	seen := make(map[string]struct{}, len(cursor.ids))
	for _, id := range cursor.ids {
		seen[id] = struct{}{}
	}
	count := 0
	for _, message := range messages {
		if message.at.After(cursor.receivedAt) || (message.at.Equal(cursor.receivedAt) && !containsDirectUnreadID(seen, message.id)) {
			count++
		}
	}
	return count, nil
}

// MarkSeen advances a target cursor monotonically from accepted direct mail.
func (s *DirectUnreadStore) MarkSeen(target DirectTarget, accepted []MailMessage) error {
	if s == nil {
		return fmt.Errorf("mark direct unread seen: nil store")
	}
	if err := validateDirectUnreadTarget(s.projectDirectory, target); err != nil {
		return err
	}
	messages, err := resolveDirectUnreadMessages(accepted, s.humanAddress, target)
	if err != nil {
		return err
	}
	candidate := directUnreadCursorForMessages(messages)

	return withDirectUnreadTransaction(s.statePath, func() error {
		s.mu.Lock()
		defer s.mu.Unlock()

		base, repair, err := refreshedDirectUnreadState(s.statePath, s.state)
		if err != nil {
			return err
		}
		key := DirectThreadKey(target)
		thread, exists := base.Threads[key]
		if !exists {
			return fmt.Errorf("direct unread target %q is not synchronized", key)
		}
		if thread.AgentID != target.AgentID {
			return fmt.Errorf("direct unread target %q has inconsistent agent ID", key)
		}
		current, valid := directUnreadCursorForThread(thread)
		if !valid {
			return fmt.Errorf("mark direct unread target %q: invalid stored boundary", key)
		}
		nextCursor, changed := advanceDirectUnreadCursor(current, candidate)
		next := cloneDirectUnreadState(base)
		if changed {
			next.Threads[key] = directUnreadThreadForCursor(target.AgentID, nextCursor)
		}
		if repair || changed {
			if err := saveDirectUnreadState(s.statePath, next); err != nil {
				return err
			}
		}
		s.state = cloneDirectUnreadState(next)
		return nil
	})
}

func validateDirectUnreadTargets(projectDirectory string, targets []DirectTarget) error {
	keys := make(map[string]struct{}, len(targets))
	for _, target := range targets {
		if err := validateDirectUnreadTarget(projectDirectory, target); err != nil {
			return err
		}
		key := DirectThreadKey(target)
		if _, duplicate := keys[key]; duplicate {
			return fmt.Errorf("direct unread targets contain duplicate key %q", key)
		}
		keys[key] = struct{}{}
	}
	return nil
}

func validateDirectUnreadTarget(projectDirectory string, target DirectTarget) error {
	if target.ProjectDirectory != projectDirectory {
		return fmt.Errorf("direct unread target project %q does not match store project %q", target.ProjectDirectory, projectDirectory)
	}
	if DirectThreadKey(target) == "" {
		return fmt.Errorf("direct unread target has no stable project-Agent key")
	}
	return nil
}

func readDirectUnreadState(path string) (directUnreadState, bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return newDirectUnreadState(), true, nil
		}
		return directUnreadState{}, false, fmt.Errorf("read direct unread state: %w", err)
	}
	var state directUnreadState
	if json.Unmarshal(data, &state) != nil || state.Version != directUnreadStateVersion {
		return newDirectUnreadState(), true, nil
	}
	return state, false, nil
}

func newDirectUnreadState() directUnreadState {
	return directUnreadState{Version: directUnreadStateVersion, Threads: make(map[string]directUnreadThread)}
}

func saveDirectUnreadState(path string, state directUnreadState) error {
	data, err := json.MarshalIndent(cloneDirectUnreadState(state), "", "  ")
	if err != nil {
		return fmt.Errorf("marshal direct unread state: %w", err)
	}
	data = append(data, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create direct unread state parent: %w", err)
	}
	if err := writeJSONAtomic(path, data); err != nil {
		return fmt.Errorf("write direct unread state: %w", err)
	}
	return nil
}

func cloneDirectUnreadState(state directUnreadState) directUnreadState {
	out := directUnreadState{Version: state.Version}
	if state.Threads == nil {
		return out
	}
	out.Threads = make(map[string]directUnreadThread, len(state.Threads))
	for key, thread := range state.Threads {
		out.Threads[key] = directUnreadThread{AgentID: thread.AgentID, ReceivedAt: thread.ReceivedAt, IDsAtReceivedAt: cloneDirectUnreadIDs(thread.IDsAtReceivedAt)}
	}
	return out
}

func cloneDirectUnreadIDs(ids []string) []string {
	if ids == nil {
		return nil
	}
	return append([]string{}, ids...)
}

func directUnreadCursorForThread(thread directUnreadThread) (directUnreadCursor, bool) {
	if thread.IDsAtReceivedAt == nil {
		return directUnreadCursor{}, false
	}
	receivedAt, err := time.Parse(time.RFC3339Nano, thread.ReceivedAt)
	if err != nil || (len(thread.IDsAtReceivedAt) == 0 && !receivedAt.IsZero()) {
		return directUnreadCursor{}, false
	}
	for index, id := range thread.IDsAtReceivedAt {
		if strings.TrimSpace(id) == "" || (index > 0 && thread.IDsAtReceivedAt[index-1] >= id) {
			return directUnreadCursor{}, false
		}
	}
	return directUnreadCursor{receivedAt: receivedAt, ids: cloneDirectUnreadIDs(thread.IDsAtReceivedAt)}, true
}

func directUnreadThreadForCursor(agentID string, cursor directUnreadCursor) directUnreadThread {
	return directUnreadThread{AgentID: agentID, ReceivedAt: cursor.receivedAt.UTC().Format(time.RFC3339Nano), IDsAtReceivedAt: cloneDirectUnreadIDs(cursor.ids)}
}

func directUnreadBaselines(targets []DirectTarget, accepted []MailMessage, humanAddress string) (map[string]directUnreadCursor, error) {
	baselines := make(map[string]directUnreadCursor, len(targets))
	for _, target := range targets {
		messages, err := resolveDirectUnreadMessages(accepted, humanAddress, target)
		if err != nil {
			return nil, err
		}
		baselines[DirectThreadKey(target)] = directUnreadCursorForMessages(messages)
	}
	return baselines, nil
}

func resolveDirectUnreadMessages(accepted []MailMessage, humanAddress string, target DirectTarget) ([]directUnreadMessage, error) {
	byID := make(map[string]time.Time)
	messages := make([]directUnreadMessage, 0, len(accepted))
	for _, msg := range accepted {
		if !IsDirectMail(msg, humanAddress, target) || strings.TrimSpace(msg.From) != strings.TrimSpace(target.Address) {
			continue
		}
		receivedAt, err := time.Parse(time.RFC3339Nano, msg.ReceivedAt)
		if err != nil {
			return nil, fmt.Errorf("resolve direct unread message: invalid received_at %q: %w", msg.ReceivedAt, err)
		}
		id := msg.MailboxID
		if strings.TrimSpace(id) == "" {
			id = msg.ID
		}
		if strings.TrimSpace(id) == "" {
			return nil, fmt.Errorf("resolve direct unread message: missing stable message ID")
		}
		if prior, exists := byID[id]; exists {
			if !prior.Equal(receivedAt) {
				return nil, fmt.Errorf("resolve direct unread message: stable message ID %q has conflicting received_at", id)
			}
			continue
		}
		byID[id] = receivedAt
		messages = append(messages, directUnreadMessage{id: id, at: receivedAt})
	}
	return messages, nil
}

func directUnreadCursorForMessages(messages []directUnreadMessage) directUnreadCursor {
	max, ids := time.Time{}, make(map[string]struct{})
	for _, message := range messages {
		if message.at.After(max) {
			max, ids = message.at, map[string]struct{}{message.id: {}}
		} else if message.at.Equal(max) {
			ids[message.id] = struct{}{}
		}
	}
	atMax := make([]string, 0, len(ids))
	for id := range ids {
		atMax = append(atMax, id)
	}
	sort.Strings(atMax)
	return directUnreadCursor{receivedAt: max, ids: atMax}
}

func advanceDirectUnreadCursor(current, candidate directUnreadCursor) (directUnreadCursor, bool) {
	if candidate.receivedAt.After(current.receivedAt) {
		return candidate, true
	}
	if candidate.receivedAt.Before(current.receivedAt) {
		return current, false
	}
	ids := make(map[string]struct{}, len(current.ids)+len(candidate.ids))
	for _, id := range current.ids {
		ids[id] = struct{}{}
	}
	for _, id := range candidate.ids {
		ids[id] = struct{}{}
	}
	merged := make([]string, 0, len(ids))
	for id := range ids {
		merged = append(merged, id)
	}
	sort.Strings(merged)
	if len(merged) == len(current.ids) {
		return current, false
	}
	return directUnreadCursor{receivedAt: current.receivedAt, ids: merged}, true
}

func containsDirectUnreadID(ids map[string]struct{}, id string) bool {
	_, found := ids[id]
	return found
}

func (s *DirectUnreadStore) threadForTargetLocked(target DirectTarget) (directUnreadThread, error) {
	key := DirectThreadKey(target)
	thread, exists := s.state.Threads[key]
	if !exists {
		return directUnreadThread{}, fmt.Errorf("direct unread target %q is not synchronized", key)
	}
	if thread.AgentID != target.AgentID {
		return directUnreadThread{}, fmt.Errorf("direct unread target %q has inconsistent agent ID", key)
	}
	return thread, nil
}
