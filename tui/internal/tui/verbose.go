package tui

import (
	"bufio"
	"encoding/json"
	"os"
	"time"
)

// ReadEvents reads events.jsonl and returns thinking/diary entries as ChatMessages.
func ReadEvents(eventsPath string) []ChatMessage {
	f, err := os.Open(eventsPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var events []ChatMessage
	scanner := bufio.NewScanner(f)
	// Increase buffer size for long lines
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var entry map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		eventType, _ := entry["type"].(string)
		if eventType != "thinking" && eventType != "diary" {
			continue
		}

		text, _ := entry["text"].(string)
		if text == "" {
			continue
		}

		// Convert Unix timestamp to RFC3339
		ts := ""
		if tsFloat, ok := entry["ts"].(float64); ok {
			ts = time.Unix(int64(tsFloat), 0).UTC().Format(time.RFC3339)
		}

		events = append(events, ChatMessage{
			Body:      text,
			Timestamp: ts,
			Type:      eventType,
		})
	}

	return events
}

