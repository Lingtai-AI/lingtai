package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultCommandsIncludesGoal(t *testing.T) {
	cmd, ok := findCommand("goal")
	if !ok {
		t.Fatal("DefaultCommands() missing goal command")
	}
	if cmd.Description != "palette.goal" || cmd.Detail != "cmd.goal" {
		t.Fatalf("goal command keys = (%q, %q), want (palette.goal, cmd.goal)", cmd.Description, cmd.Detail)
	}
}

func TestWriteGoalRequestNotificationCreatesSystemEvent(t *testing.T) {
	agentDir := t.TempDir()
	now := time.Date(2026, 6, 10, 9, 0, 0, 0, time.UTC)

	eventID, err := writeGoalRequestNotification(agentDir, "finish the linked /goal PR", now)
	if err != nil {
		t.Fatalf("writeGoalRequestNotification returned error: %v", err)
	}
	if eventID == "" {
		t.Fatal("writeGoalRequestNotification returned empty event id")
	}

	payload := readGoalTestPayload(t, agentDir)
	events := goalTestEvents(t, payload)
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	event := events[0]
	if event["event_id"] != eventID {
		t.Fatalf("event_id = %q, want returned id %q", event["event_id"], eventID)
	}
	if event["source"] != goalRequestSource {
		t.Fatalf("source = %q, want %q", event["source"], goalRequestSource)
	}
	if refID, ok := event["ref_id"].(string); !ok || !strings.HasPrefix(refID, "goal.request:") {
		t.Fatalf("ref_id = %#v, want goal.request:<id>", event["ref_id"])
	}

	body, _ := event["body"].(string)
	for _, want := range []string{
		"goal manual",
		"finish the linked /goal PR",
		".notification/goal.json",
		"dismissing a goal.reminder only hides",
		"Do not create or overwrite .notification/goal.json until the human confirms",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("goal request body missing %q:\n%s", want, body)
		}
	}

	instructions, _ := payload["instructions"].(string)
	for _, want := range []string{
		"source=goal.request",
		"read the goal manual",
		"system(action=\"dismiss\", channel=\"system\", ref_id=\"<ref_id>\")",
	} {
		if !strings.Contains(instructions, want) {
			t.Fatalf("instructions missing %q:\n%s", want, instructions)
		}
	}
}

func TestWriteGoalRequestNotificationPreservesEventsAndCapsTwenty(t *testing.T) {
	agentDir := t.TempDir()
	notifDir := filepath.Join(agentDir, ".notification")
	if err := os.MkdirAll(notifDir, 0o755); err != nil {
		t.Fatal(err)
	}

	seedEvents := make([]map[string]any, 20)
	for i := range seedEvents {
		seedEvents[i] = map[string]any{
			"event_id": "seed",
			"source":   "daemon.done",
			"ref_id":   "old-" + string(rune('a'+i)),
			"body":     "old event",
		}
	}
	seed := map[string]any{
		"header": "20 system notifications",
		"data": map[string]any{
			"events": seedEvents,
			"other":  "preserved",
		},
		"instructions": "old instructions",
	}
	data, err := json.Marshal(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(notifDir, "system.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err = writeGoalRequestNotification(agentDir, "new goal", time.Date(2026, 6, 10, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("writeGoalRequestNotification returned error: %v", err)
	}

	payload := readGoalTestPayload(t, agentDir)
	events := goalTestEvents(t, payload)
	if len(events) != 20 {
		t.Fatalf("got %d events, want cap of 20", len(events))
	}
	if events[0]["ref_id"] != "old-b" {
		t.Fatalf("first preserved event ref_id = %q, want old-b after dropping oldest", events[0]["ref_id"])
	}
	last := events[len(events)-1]
	if last["source"] != goalRequestSource {
		t.Fatalf("last source = %q, want %q", last["source"], goalRequestSource)
	}
	dataMap, _ := payload["data"].(map[string]any)
	if dataMap["other"] != "preserved" {
		t.Fatalf("data.other = %q, want preserved", dataMap["other"])
	}
}

func readGoalTestPayload(t *testing.T, agentDir string) map[string]any {
	t.Helper()
	path := filepath.Join(agentDir, ".notification", "system.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading %s: %v", path, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(data, &payload); err != nil {
		t.Fatalf("unmarshal %s: %v", path, err)
	}
	return payload
}

func goalTestEvents(t *testing.T, payload map[string]any) []map[string]any {
	t.Helper()
	data, ok := payload["data"].(map[string]any)
	if !ok {
		t.Fatalf("payload.data missing or wrong type: %#v", payload["data"])
	}
	raw, ok := data["events"].([]any)
	if !ok {
		t.Fatalf("payload.data.events missing or wrong type: %#v", data["events"])
	}
	events := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		event, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("event has wrong type: %#v", item)
		}
		events = append(events, event)
	}
	return events
}
