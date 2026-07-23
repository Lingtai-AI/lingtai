package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const RailUnreadStateVersion = 1

type DirectTarget struct {
	Directory string
	Address   string
}

type unreadBoundary struct {
	Timestamp string   `json:"timestamp,omitempty"`
	IDs       []string `json:"ids,omitempty"`
}

type unreadTargetState struct {
	AddressFingerprint string         `json:"address_fingerprint"`
	LastSeen           unreadBoundary `json:"last_seen"`
}

type railUnreadState struct {
	Version int                          `json:"version"`
	Targets map[string]unreadTargetState `json:"targets"`
}

// RailUnreadStore owns the TUI's durable per-project direct-mail boundaries.
// It is intentionally not a mailbox scanner; callers supply accepted snapshots.
type RailUnreadStore struct {
	path  string
	state railUnreadState
}

func RailUnreadStatePath(projectDir string) string {
	return filepath.Join(projectDir, ".tui-asset", "rail-last-seen.json")
}

func AddressFingerprint(address string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(address)))
	return hex.EncodeToString(sum[:])
}

// OpenRailUnreadStore loads the versioned state. Missing, malformed, or
// unsupported state is replaced with an all-read baseline at snapshot.
func OpenRailUnreadStore(projectDir string, targets []DirectTarget, snapshot []MailMessage, humanAddress string) (*RailUnreadStore, error) {
	store := &RailUnreadStore{
		path: RailUnreadStatePath(projectDir),
		state: railUnreadState{
			Version: RailUnreadStateVersion,
			Targets: make(map[string]unreadTargetState),
		},
	}
	data, err := os.ReadFile(store.path)
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read rail unread state: %w", err)
	}
	valid := err == nil && json.Unmarshal(data, &store.state) == nil &&
		store.state.Version == RailUnreadStateVersion && store.state.Targets != nil
	if !valid {
		store.state = railUnreadState{Version: RailUnreadStateVersion, Targets: make(map[string]unreadTargetState)}
		store.baselineTargets(targets, snapshot, humanAddress)
		if err := store.write(); err != nil {
			return nil, err
		}
		return store, nil
	}
	if err := store.SyncTargets(targets, snapshot, humanAddress); err != nil {
		return nil, err
	}
	return store, nil
}

// SyncTargets drops disappeared directories and baselines new or address-changed
// identities. Existing matching identities retain their last-seen boundary.
func (s *RailUnreadStore) SyncTargets(targets []DirectTarget, snapshot []MailMessage, humanAddress string) error {
	current := make(map[string]DirectTarget, len(targets))
	for _, target := range targets {
		key := canonicalTargetDirectory(target.Directory)
		if key != "" {
			current[key] = target
		}
	}
	changed := false
	for key := range s.state.Targets {
		if _, exists := current[key]; !exists {
			delete(s.state.Targets, key)
			changed = true
		}
	}
	for key, target := range current {
		fingerprint := AddressFingerprint(target.Address)
		state, exists := s.state.Targets[key]
		if !exists || state.AddressFingerprint != fingerprint {
			s.state.Targets[key] = unreadTargetState{
				AddressFingerprint: fingerprint,
				LastSeen:           incomingBoundary(snapshot, humanAddress, target.Address),
			}
			changed = true
		}
	}
	if changed {
		return s.write()
	}
	return nil
}

func (s *RailUnreadStore) UnreadCount(target DirectTarget, snapshot []MailMessage, humanAddress string) int {
	state, ok := s.targetState(target)
	if !ok {
		return 0
	}
	boundaryTime := parseMailTime(state.LastSeen.Timestamp)
	seenAtBoundary := make(map[string]struct{}, len(state.LastSeen.IDs))
	for _, id := range state.LastSeen.IDs {
		seenAtBoundary[id] = struct{}{}
	}
	count := 0
	for _, msg := range snapshot {
		if !isIncomingDirectMail(msg, humanAddress, target.Address) {
			continue
		}
		messageTime := parseMailTime(msg.ReceivedAt)
		if messageTime.IsZero() {
			continue
		}
		if boundaryTime.IsZero() || messageTime.After(boundaryTime) {
			count++
			continue
		}
		if messageTime.Equal(boundaryTime) {
			if _, seen := seenAtBoundary[mailBoundaryID(msg)]; !seen {
				count++
			}
		}
	}
	return count
}

// MarkSeen advances exactly to the supplied accepted snapshot boundary. The
// target must already match the identity accepted by the latest SyncTargets.
func (s *RailUnreadStore) MarkSeen(target DirectTarget, snapshot []MailMessage, humanAddress string) error {
	key := canonicalTargetDirectory(target.Directory)
	if key == "" {
		return fmt.Errorf("direct target directory is empty")
	}
	fingerprint := AddressFingerprint(target.Address)
	state, exists := s.state.Targets[key]
	if !exists {
		return fmt.Errorf("direct target is not synchronized")
	}
	if state.AddressFingerprint != fingerprint {
		return fmt.Errorf("direct target identity changed; synchronize targets before marking seen")
	}
	s.state.Targets[key] = unreadTargetState{
		AddressFingerprint: fingerprint,
		LastSeen:           incomingBoundary(snapshot, humanAddress, target.Address),
	}
	return s.write()
}

func (s *RailUnreadStore) targetState(target DirectTarget) (unreadTargetState, bool) {
	state, ok := s.state.Targets[canonicalTargetDirectory(target.Directory)]
	if !ok || state.AddressFingerprint != AddressFingerprint(target.Address) {
		return unreadTargetState{}, false
	}
	return state, true
}

func (s *RailUnreadStore) baselineTargets(targets []DirectTarget, snapshot []MailMessage, humanAddress string) {
	for _, target := range targets {
		key := canonicalTargetDirectory(target.Directory)
		if key == "" {
			continue
		}
		s.state.Targets[key] = unreadTargetState{
			AddressFingerprint: AddressFingerprint(target.Address),
			LastSeen:           incomingBoundary(snapshot, humanAddress, target.Address),
		}
	}
}

func (s *RailUnreadStore) write() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create rail unread directory: %w", err)
	}
	data, err := json.MarshalIndent(s.state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal rail unread state: %w", err)
	}
	data = append(data, '\n')
	if err := writeJSONAtomic(s.path, data); err != nil {
		return fmt.Errorf("write rail unread state: %w", err)
	}
	return nil
}

func incomingBoundary(snapshot []MailMessage, humanAddress, targetAddress string) unreadBoundary {
	var maximum time.Time
	ids := make(map[string]struct{})
	timestamp := ""
	for _, msg := range snapshot {
		if !isIncomingDirectMail(msg, humanAddress, targetAddress) {
			continue
		}
		messageTime := parseMailTime(msg.ReceivedAt)
		if messageTime.IsZero() {
			continue
		}
		switch {
		case maximum.IsZero() || messageTime.After(maximum):
			maximum = messageTime
			timestamp = msg.ReceivedAt
			ids = map[string]struct{}{mailBoundaryID(msg): {}}
		case messageTime.Equal(maximum):
			ids[mailBoundaryID(msg)] = struct{}{}
		}
	}
	result := unreadBoundary{Timestamp: timestamp}
	for id := range ids {
		if id != "" {
			result.IDs = append(result.IDs, id)
		}
	}
	sort.Strings(result.IDs)
	return result
}

func isIncomingDirectMail(msg MailMessage, humanAddress, targetAddress string) bool {
	return strings.TrimSpace(msg.From) == strings.TrimSpace(targetAddress) &&
		IsDirectMail(msg, humanAddress, targetAddress)
}

func mailBoundaryID(msg MailMessage) string {
	if msg.MailboxID != "" {
		return msg.MailboxID
	}
	return msg.ID
}

func parseMailTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func canonicalTargetDirectory(directory string) string {
	directory = strings.TrimSpace(directory)
	if directory == "" {
		return ""
	}
	absolute, err := filepath.Abs(directory)
	if err != nil {
		return filepath.Clean(directory)
	}
	return filepath.Clean(absolute)
}
