package tui

import (
	"os"
	"strings"
	"testing"
)

// TestDirectConversationCleanupHasSinglePublicationOwner keeps superseded
// compatibility state and owner-neutral projection seams from returning after
// the accepted publication, unread store, selector, and target contracts exist.
func TestDirectConversationCleanupHasSinglePublicationOwner(t *testing.T) {
	for file, forbidden := range map[string][]string{
		"agent_rail.go": {
			"if index < 0 || index >= len(m.agentSelector.rows)",
		},
		"app.go": {
			"targetName = filepath.Base(targetDir)",
		},
		"direct_chat.go": {
			"msg.opSerial == 0 ||",
			"if m.directPublication != nil {",
			"func projectDirectMessages(",
		},
		"mail.go": {
			"directPrepared",
		},
	} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		for _, marker := range forbidden {
			if strings.Contains(string(source), marker) {
				t.Errorf("%s still contains superseded direct-state bridge %q", file, marker)
			}
		}
	}
}
