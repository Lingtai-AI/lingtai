package tui

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const goalRequestSource = "goal.request"

func writeGoalRequestNotification(agentDir, humanRequest string, now time.Time) (string, error) {
	if agentDir == "" {
		return "", fmt.Errorf("no current agent is selected")
	}
	if now.IsZero() {
		now = time.Now()
	}
	notifDir := filepath.Join(agentDir, ".notification")
	if err := os.MkdirAll(notifDir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(notifDir, "system.json")

	payload := readSystemNotificationPayload(path)
	events := readSystemNotificationEvents(payload)

	eventID := fmt.Sprintf("evt_%x_%s", now.UnixMilli(), randomHex(2))
	refID := fmt.Sprintf("goal.request:%x", now.UnixMilli())
	at := now.UTC().Format("2006-01-02T15:04:05Z")
	events = append(events, map[string]any{
		"event_id": eventID,
		"source":   goalRequestSource,
		"ref_id":   refID,
		"body":     buildGoalRequestBody(humanRequest),
		"at":       at,
	})
	if len(events) > 20 {
		events = events[len(events)-20:]
	}

	data, _ := payload["data"].(map[string]any)
	if data == nil {
		data = map[string]any{}
	}
	data["events"] = events
	payload["data"] = data
	payload["header"] = fmt.Sprintf("%d system notification%s", len(events), pluralS(len(events)))
	payload["icon"] = "🔔"
	payload["priority"] = "normal"
	payload["published_at"] = at
	payload["instructions"] = "System events are multiplexed in data.events. For source=goal.request, read the goal manual under system-manual, then guide the human to define a goal before writing .notification/goal.json. Dismiss this request with system(action=\"dismiss\", channel=\"system\", ref_id=\"<ref_id>\") after handling it."

	if err := writeJSONFile(path, payload); err != nil {
		return "", err
	}
	return eventID, nil
}

func readSystemNotificationPayload(path string) map[string]any {
	payload := map[string]any{}
	data, err := os.ReadFile(path)
	if err != nil {
		return payload
	}
	_ = json.Unmarshal(data, &payload)
	return payload
}

func readSystemNotificationEvents(payload map[string]any) []any {
	data, _ := payload["data"].(map[string]any)
	rawEvents, _ := data["events"].([]any)
	if len(rawEvents) == 0 {
		return nil
	}
	events := make([]any, 0, len(rawEvents))
	for _, ev := range rawEvents {
		if ev == nil {
			continue
		}
		events = append(events, ev)
	}
	return events
}

func buildGoalRequestBody(humanRequest string) string {
	request := strings.TrimSpace(humanRequest)
	var b strings.Builder
	b.WriteString("Human wants to set or revise an active goal. Read the goal manual under system-manual before acting. Guide the human to create a goal by clarifying objective, success criteria, optional reminder_delay_seconds, and any constraints. Explain clearly that canceling a goal requires deleting .notification/goal.json or marking data.status inactive/cancelled/done; dismissing a goal.reminder only hides that reminder and does not cancel the goal. Do not create or overwrite .notification/goal.json until the human confirms the goal details.")
	if request != "" {
		b.WriteString(" Human request: ")
		b.WriteString(request)
	} else {
		b.WriteString(" No inline goal text was provided; ask the human what goal they want to create.")
	}
	return b.String()
}

func writeJSONFile(path string, payload any) error {
	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	tmp, err := os.CreateTemp(filepath.Dir(path), ".system.json.tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%x", time.Now().UnixNano())
	}
	return hex.EncodeToString(buf)
}

func pluralS(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}
