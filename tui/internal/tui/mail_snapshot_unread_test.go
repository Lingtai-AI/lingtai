package tui

import (
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestAcceptedMailSnapshotMessagesForUnreadReadinessAndDetachment(t *testing.T) {
	producer := fs.MailCache{Messages: []fs.MailMessage{{
		MailboxID:   "mailbox-1",
		ID:          "legacy-1",
		From:        "project/agent-a",
		To:          []interface{}{"project/human", map[string]interface{}{"route": []interface{}{"original-route"}}},
		CC:          []string{"original-cc"},
		Attachments: []string{"original.txt"},
		Identity: map[string]interface{}{
			"agent_id": "agent-id-a",
			"nested": map[string]interface{}{
				"labels": []interface{}{"original-label"},
			},
		},
	}}}

	notReady := acceptedMailSnapshot{cache: producer}
	if got := notReady.messagesForUnread("/human"); len(got) != 0 {
		t.Fatalf("messagesForUnread before readiness = %#v, want empty", got)
	}

	accepted := newAcceptedMailSnapshot(producer)
	// The accepted snapshot must not retain references into the producer cache.
	producer.Messages[0].CC[0] = "producer-mutated-cc"
	producer.Messages[0].Attachments[0] = "producer-mutated.txt"
	producer.Messages[0].To.([]interface{})[1].(map[string]interface{})["route"].([]interface{})[0] = "producer-mutated-route"
	producer.Messages[0].Identity["nested"].(map[string]interface{})["labels"].([]interface{})[0] = "producer-mutated-label"

	got := accepted.messagesForUnread("/human")
	if len(got) != 1 {
		t.Fatalf("messagesForUnread after readiness len = %d, want 1", len(got))
	}
	if got[0].CC[0] != "original-cc" || got[0].Attachments[0] != "original.txt" ||
		got[0].To.([]interface{})[1].(map[string]interface{})["route"].([]interface{})[0] != "original-route" ||
		got[0].Identity["nested"].(map[string]interface{})["labels"].([]interface{})[0] != "original-label" {
		t.Fatalf("messagesForUnread observed later producer mutation: %#v", got[0])
	}

	// The returned messages must also be recursively detached from the accepted
	// snapshot and from a later unread accessor result.
	got[0].CC[0] = "returned-mutated-cc"
	got[0].Attachments[0] = "returned-mutated.txt"
	got[0].To.([]interface{})[1].(map[string]interface{})["route"].([]interface{})[0] = "returned-mutated-route"
	got[0].Identity["nested"].(map[string]interface{})["labels"].([]interface{})[0] = "returned-mutated-label"

	again := accepted.messagesForUnread("/human")
	if len(again) != 1 {
		t.Fatalf("second messagesForUnread len = %d, want 1", len(again))
	}
	if again[0].CC[0] != "original-cc" || again[0].Attachments[0] != "original.txt" ||
		again[0].To.([]interface{})[1].(map[string]interface{})["route"].([]interface{})[0] != "original-route" ||
		again[0].Identity["nested"].(map[string]interface{})["labels"].([]interface{})[0] != "original-label" {
		t.Fatalf("messagesForUnread returned an aliased message graph: %#v", again[0])
	}
}
