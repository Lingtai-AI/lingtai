package preset

import (
	"io/fs"
	"strings"
	"testing"
)

func TestTutorialCommunicationExplainsLICCAndCustomCommands(t *testing.T) {
	data, err := fs.ReadFile(skillsFS, "skills/lingtai-tutorial-guide/reference/communication/SKILL.md")
	if err != nil {
		t.Fatalf("read communication tutorial: %v", err)
	}
	body := string(data)
	for _, needle := range []string{
		"LICC bridge mental model",
		"Telegram Bot API",
		"LICC inbox event",
		"Agent-level custom commands",
		"conversation convention",
		"bypass permissions",
	} {
		if !strings.Contains(body, needle) {
			t.Fatalf("communication tutorial missing %q", needle)
		}
	}
}
