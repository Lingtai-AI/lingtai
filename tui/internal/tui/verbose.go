package tui

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"time"
)

// thinkingTypes are shown in ctrl+o level 1: text input/output + soul
var thinkingTypes = map[string]bool{
	"thinking":    true,
	"diary":       true,
	"text_input":  true,
	"text_output": true,
}

// extendedTypes are shown in ctrl+o level 2: + pure tool calling info
var extendedTypes = map[string]bool{
	"thinking":    true,
	"diary":       true,
	"text_input":  true,
	"text_output": true,
	"tool_call":   true,
	"tool_result": true,
}

// ReadEvents reads events.jsonl and returns entries as ChatMessages.
// If extended is true, includes tool_call, tool_result, text_input, text_output.
func ReadEvents(eventsPath string, extended bool) []ChatMessage {
	f, err := os.Open(eventsPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	allowed := thinkingTypes
	if extended {
		allowed = extendedTypes
	}

	var events []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var entry map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}

		eventType, _ := entry["type"].(string)
		if !allowed[eventType] {
			continue
		}

		// Extract text — different fields for different types
		text := extractEventText(entry, eventType)
		if text == "" {
			continue
		}

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

// ReadInsightEvents reads only "insight" events from events.jsonl.
func ReadInsightEvents(eventsPath string) []ChatMessage {
	f, err := os.Open(eventsPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var events []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var entry map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		if eventType, _ := entry["type"].(string); eventType != "insight" {
			continue
		}
		text, _ := entry["text"].(string)
		if text == "" {
			continue
		}
		ts := ""
		if tsFloat, ok := entry["ts"].(float64); ok {
			ts = time.Unix(int64(tsFloat), 0).UTC().Format(time.RFC3339)
		}
		// Skip /btw results — these are now read from soul_inquiry.jsonl
		// via ReadSoulInquiries to avoid duplication.
		if q, _ := entry["question"].(string); q != "" {
			continue
		}
		events = append(events, ChatMessage{
			Body:      text,
			Timestamp: ts,
			Type:      "insight",
		})
	}
	return events
}

// ReadSoulInquiries reads soul_inquiry.jsonl and returns ChatMessages for
// human-sourced inquiries (from /btw). These are displayed independently of
// the events.jsonl insight path so results survive even if the insight event
// was lost due to a crash.
func ReadSoulInquiries(logPath string) []ChatMessage {
	f, err := os.Open(logPath)
	if err != nil {
		return nil
	}
	defer f.Close()

	var msgs []ChatMessage
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		var entry map[string]interface{}
		if err := json.Unmarshal(scanner.Bytes(), &entry); err != nil {
			continue
		}
		source, _ := entry["source"].(string)
		if source != "human" {
			continue
		}
		voice, _ := entry["voice"].(string)
		if voice == "" {
			continue
		}
		ts, _ := entry["ts"].(string)
		prompt, _ := entry["prompt"].(string)
		msgs = append(msgs, ChatMessage{
			Body:      voice,
			Timestamp: ts,
			Type:      "insight",
			Question:  prompt,
		})
	}
	return msgs
}

func extractEventText(entry map[string]interface{}, eventType string) string {
	switch eventType {
	case "thinking", "diary", "text_output", "text_input":
		text, _ := entry["text"].(string)
		return text
	case "tool_call":
		name, _ := entry["tool_name"].(string)
		args, _ := entry["tool_args"].(string)
		if args == "" {
			if argsMap, ok := entry["tool_args"].(map[string]interface{}); ok {
				data, _ := json.Marshal(argsMap)
				args = string(data)
			}
		}
		if len(args) > 200 {
			args = args[:200] + "..."
		}
		return fmt.Sprintf("%s(%s)", name, args)
	case "tool_result":
		name, _ := entry["tool_name"].(string)
		status, _ := entry["status"].(string)
		elapsed := ""
		if ms, ok := entry["elapsed_ms"].(float64); ok {
			elapsed = fmt.Sprintf(" %dms", int(ms))
		}
		return fmt.Sprintf("%s → %s%s", name, status, elapsed)
	}
	return ""
}
