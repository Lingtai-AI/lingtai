package fs

import (
	"fmt"
	"path/filepath"
	"reflect"
	"sort"
	"testing"
)

func TestSessionDirectThreadWindowBoundsEventsAndInquiries(t *testing.T) {
	root, humanDir, targetDir := newSessionTestDirs(t)
	var events string
	for i := 0; i < 5; i++ {
		events += sessionEventJSONL(float64(i+1), "text_output", fmt.Sprintf("event-%d", i))
	}
	writeSessionTestFile(t, filepath.Join(targetDir, "logs", "events.jsonl"), events)

	var inquiries string
	for i := 0; i < 4; i++ {
		inquiries += fmt.Sprintf(
			"{\"ts\":\"2026-07-14T00:00:0%dZ\",\"source\":\"human\",\"prompt\":\"question-%d\",\"voice\":\"inquiry-%d\"}\n",
			i, i, i,
		)
	}
	writeSessionTestFile(t, filepath.Join(targetDir, "logs", "soul_inquiry.jsonl"), inquiries)

	accepted := []MailMessage{
		{ID: "to-a", From: "human", To: "agent-a", Subject: "to A", Message: "mail-to-a", ReceivedAt: "2026-07-14T01:00:00Z", Delivered: true},
		{ID: "from-a", From: "agent-a", To: "human", Subject: "from A", Message: "mail-from-a", ReceivedAt: "2026-07-14T01:00:01Z", Delivered: true},
		{ID: "to-b", From: "human", To: "agent-b", Subject: "to B", Message: "mail-to-b", ReceivedAt: "2026-07-14T01:00:02Z", Delivered: true},
		{ID: "from-b", From: "agent-b", To: "human", Subject: "from B", Message: "mail-from-b", ReceivedAt: "2026-07-14T01:00:03Z", Delivered: true},
		{ID: "cc-a", From: "human", To: "agent-b", CC: []string{"agent-a"}, Subject: "CC A", Message: "mail-cc-only-a", ReceivedAt: "2026-07-14T01:00:04Z", Delivered: true},
	}

	session := NewSessionCache(humanDir, root, NoPersist)
	session.RebuildDirectThreadWindowedInMemory(
		accepted,
		"human",
		"agent-a",
		targetDir,
		"Agent A",
		2,
		2,
	)

	want := []string{
		"event-3",
		"event-4",
		"inquiry-2",
		"inquiry-3",
		"mail-from-a",
		"mail-to-a",
	}
	if got := sortedSessionBodies(session.Entries()); !reflect.DeepEqual(got, want) {
		t.Fatalf("direct bounded entries = %v, want %v", got, want)
	}
	if session.Complete() {
		t.Fatal("direct bounded session reported complete after both histories were truncated")
	}

	appendSessionTestFile(t, filepath.Join(targetDir, "logs", "events.jsonl"), sessionEventJSONL(6, "text_output", "event-5"))
	appendSessionTestFile(t, filepath.Join(targetDir, "logs", "soul_inquiry.jsonl"), "{\"ts\":\"2026-07-14T00:00:04Z\",\"source\":\"human\",\"prompt\":\"question-4\",\"voice\":\"inquiry-4\"}\n")
	session.Refresh(
		MailCache{Messages: []MailMessage{accepted[0], accepted[1]}},
		"human",
		targetDir,
		"Agent A",
	)

	want = []string{
		"event-3",
		"event-4",
		"event-5",
		"inquiry-2",
		"inquiry-3",
		"inquiry-4",
		"mail-from-a",
		"mail-to-a",
	}
	if got := sortedSessionBodies(session.Entries()); !reflect.DeepEqual(got, want) {
		t.Fatalf("direct refresh entries = %v, want %v; excluded history or mail was re-ingested", got, want)
	}
}

func sortedSessionBodies(entries []SessionEntry) []string {
	bodies := make([]string, 0, len(entries))
	for _, entry := range entries {
		bodies = append(bodies, entry.Body)
	}
	sort.Strings(bodies)
	return bodies
}
