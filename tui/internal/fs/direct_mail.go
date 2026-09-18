package fs

import (
	"crypto/sha256"
	"encoding/hex"
	"path/filepath"
	"strings"
)

// DirectTarget identifies one inventory target without coupling target identity
// to unread persistence or any future async acceptance store. ProjectDirectory
// is the caller's canonical project directory; Directory is the target's
// canonical working directory, which is also the absolute spelling of its
// route; AgentID is its durable manifest identity; and Address is its current
// project-local network route.
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
	agentID := target.AgentID
	if target.ProjectDirectory == "" || strings.TrimSpace(agentID) == "" {
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

// directEndpoint is one direct-thread participant's project-aware route: the
// bare address its manifest publishes and, when the project layout supplies it,
// the canonical absolute agent directory that a kernel envelope such as
// email.reply may record instead. These are the only two spellings that name the
// participant; a shared final path component, another agent's directory, a path
// outside the project, or a nested path never does.
type directEndpoint struct {
	address   string
	directory string
}

// directSpelling trims one raw endpoint and lexically cleans it when absolute.
// It is the only normalization applied before routes are compared, so bare
// addresses stay literal.
func directSpelling(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if filepath.IsAbs(endpoint) {
		return filepath.Clean(endpoint)
	}
	return endpoint
}

// directTargetEndpoint is the selected target's route: its manifest address and
// its own working directory. A blank or relative Directory has no absolute
// spelling.
func directTargetEndpoint(target DirectTarget) directEndpoint {
	endpoint := directEndpoint{address: directSpelling(target.Address)}
	if directory := directSpelling(target.Directory); filepath.IsAbs(directory) {
		endpoint.directory = directory
	}
	return endpoint
}

// directHumanEndpoint is the human's route inside the target's project: the
// human address and the directory it names beside the agents under
// <project>/.lingtai. Without an absolute ProjectDirectory only the literal
// address names the human.
func directHumanEndpoint(humanAddress string, target DirectTarget) directEndpoint {
	endpoint := directEndpoint{address: directSpelling(humanAddress)}
	project := directSpelling(target.ProjectDirectory)
	if endpoint.address != "" && !filepath.IsAbs(endpoint.address) && filepath.IsAbs(project) {
		endpoint.directory = filepath.Join(project, ".lingtai", endpoint.address)
	}
	return endpoint
}

// names reports whether raw is one of the participant's two spellings.
func (e directEndpoint) names(raw string) bool {
	spelling := directSpelling(raw)
	return spelling != "" && (spelling == e.address || spelling == e.directory)
}

// suppliedAgentIDMatches reports whether an incoming message's optional Agent
// identity is compatible with the selected target. Legacy messages without the
// field may fall back to route equality; any supplied invalid, unverifiable, or
// mismatching value fails closed.
func suppliedAgentIDMatches(identity map[string]interface{}, targetAgentID string) bool {
	raw, supplied := identity["agent_id"]
	if !supplied {
		return true
	}
	messageAgentID, ok := raw.(string)
	if !ok {
		return false
	}
	return strings.TrimSpace(messageAgentID) != "" &&
		strings.TrimSpace(targetAgentID) != "" &&
		messageAgentID == targetAgentID
}

// IsDirectMail reports whether msg belongs to the strict human-target thread.
// Direct mail has exactly one raw primary recipient and no CC participants.
// Each side must name the human or the target by its bare address or its
// canonical same-project absolute directory (see directEndpoint), and incoming
// mail must match any supplied identity.agent_id literally; group, malformed,
// copied, cross-project, or contradictory mail fails closed.
func IsDirectMail(msg MailMessage, humanAddress string, target DirectTarget) bool {
	human := directHumanEndpoint(humanAddress, target)
	agent := directTargetEndpoint(target)
	// The human and the target must share no spelling: NewDirectMailPublication
	// relies on that to tell inbound mail from outbound.
	if human.address == "" || agent.address == "" || human.names(agent.address) || human.names(agent.directory) || len(msg.CC) != 0 {
		return false
	}
	to, valid := strictMailRecipient(msg.To)
	if !valid {
		return false
	}
	if human.names(msg.From) {
		return agent.names(to)
	}
	return agent.names(msg.From) && human.names(to) && suppliedAgentIDMatches(msg.Identity, target.AgentID)
}
