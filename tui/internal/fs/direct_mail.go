package fs

import "strings"

// NormalizeMailEndpoints converts the mailbox schema's string-or-list endpoint
// value into one deduplicated list. CC is deliberately not part of this input.
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

// IsDirectMail reports whether msg belongs to the strict human-target thread.
// Only From and To participate; CC never creates membership.
func IsDirectMail(msg MailMessage, humanAddress, targetAddress string) bool {
	humanAddress = strings.TrimSpace(humanAddress)
	targetAddress = strings.TrimSpace(targetAddress)
	from := strings.TrimSpace(msg.From)
	if humanAddress == "" || targetAddress == "" {
		return false
	}
	to := NormalizeMailEndpoints(msg.To)
	if from == humanAddress {
		return endpointListContains(to, targetAddress)
	}
	return from == targetAddress && endpointListContains(to, humanAddress)
}

func endpointListContains(endpoints []string, address string) bool {
	for _, endpoint := range endpoints {
		if endpoint == address {
			return true
		}
	}
	return false
}
