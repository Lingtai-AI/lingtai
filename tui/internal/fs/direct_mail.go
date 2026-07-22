package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// DirectTarget identifies one inventory target without coupling target identity
// to unread persistence or any future async acceptance store. ProjectDirectory
// is the caller's canonical project directory; Directory is the target's
// canonical working directory; AgentID is its durable manifest identity; and
// Address is its current project-local network route.
type DirectTarget struct {
	ProjectDirectory string
	Directory        string
	AgentID          string
	Address          string
}

// DirectThreadKey returns the target's stable project-Agent identity. It does
// not include the target's current directory or address, so routing changes do
// not split unread or selection state. Incomplete targets have no durable key.
func DirectThreadKey(target DirectTarget) string {
	agentID := strings.TrimSpace(target.AgentID)
	if target.ProjectDirectory == "" || agentID == "" {
		return ""
	}
	sum := sha256.Sum256([]byte("direct-thread\x00" + target.ProjectDirectory + "\x00" + agentID))
	return hex.EncodeToString(sum[:])
}

// AddressFingerprint returns a stable fingerprint for one serialized route,
// not a durable Agent identity. Whitespace surrounding an address is ignored.
func AddressFingerprint(address string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(address)))
	return hex.EncodeToString(sum[:])
}

// NormalizeMailEndpoints converts the mailbox schema's string-or-list endpoint
// value into one trimmed, order-preserving, deduplicated list. It is lenient for
// topology aggregation: malformed, empty, and duplicate entries are discarded.
// Direct-thread membership deliberately uses strictMailRecipient instead.
func NormalizeMailEndpoints(value interface{}) []string {
	var raw []string
	switch endpoints := value.(type) {
	case string:
		raw = []string{endpoints}
	case []string:
		raw = endpoints
	case []interface{}:
		raw = make([]string, 0, len(endpoints))
		for _, endpoint := range endpoints {
			if text, ok := endpoint.(string); ok {
				raw = append(raw, text)
			}
		}
	default:
		return nil
	}

	seen := make(map[string]struct{}, len(raw))
	result := make([]string, 0, len(raw))
	for _, endpoint := range raw {
		endpoint = strings.TrimSpace(endpoint)
		if endpoint == "" {
			continue
		}
		if _, exists := seen[endpoint]; exists {
			continue
		}
		seen[endpoint] = struct{}{}
		result = append(result, endpoint)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

// strictMailRecipient returns the one valid recipient only when the raw To
// envelope itself is a scalar or one-element string list. It rejects malformed,
// empty, duplicate, or otherwise multi-entry envelopes rather than normalizing
// them into a direct-looking singleton.
func strictMailRecipient(value interface{}) (string, bool) {
	var recipient string
	switch endpoints := value.(type) {
	case string:
		recipient = endpoints
	case []string:
		if len(endpoints) != 1 {
			return "", false
		}
		recipient = endpoints[0]
	case []interface{}:
		if len(endpoints) != 1 {
			return "", false
		}
		text, ok := endpoints[0].(string)
		if !ok {
			return "", false
		}
		recipient = text
	default:
		return "", false
	}

	recipient = strings.TrimSpace(recipient)
	return recipient, recipient != ""
}

// suppliedAgentIDMatches reports whether an incoming message's optional Agent
// identity is compatible with the selected target. Legacy messages without the
// field may fall back to the exact current address; any supplied invalid,
// unverifiable, or mismatching value fails closed.
func suppliedAgentIDMatches(identity map[string]interface{}, targetAgentID string) bool {
	raw, supplied := identity["agent_id"]
	if !supplied {
		return true
	}
	messageAgentID, ok := raw.(string)
	if !ok {
		return false
	}
	messageAgentID = strings.TrimSpace(messageAgentID)
	targetAgentID = strings.TrimSpace(targetAgentID)
	return messageAgentID != "" && targetAgentID != "" && messageAgentID == targetAgentID
}

// IsDirectMail reports whether msg belongs to the strict human-target thread.
// Direct mail has exactly one raw primary recipient and no CC participants.
// Incoming mail must match the target's current address and any supplied
// identity.agent_id; group, malformed, copied, or contradictory mail fails closed.
func IsDirectMail(msg MailMessage, humanAddress string, target DirectTarget) bool {
	humanAddress = strings.TrimSpace(humanAddress)
	targetAddress := strings.TrimSpace(target.Address)
	from := strings.TrimSpace(msg.From)
	if humanAddress == "" || targetAddress == "" || humanAddress == targetAddress || len(msg.CC) != 0 {
		return false
	}
	to, valid := strictMailRecipient(msg.To)
	if !valid {
		return false
	}
	if from == humanAddress {
		return to == targetAddress
	}
	return from == targetAddress && to == humanAddress && suppliedAgentIDMatches(msg.Identity, target.AgentID)
}
