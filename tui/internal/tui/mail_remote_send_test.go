package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestMailModelRemoteSendFailurePreservesRetryState(t *testing.T) {
	const remoteAddr = "[2001:db8::1]:/remote/.lingtai/orch"
	const text = "keep this message for retry"

	for _, tc := range []struct {
		name   string
		editor bool
	}{
		{name: "ordinary input"},
		{name: "editor pending", editor: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			humanDir := filepath.Join(root, "human")
			orchDir := filepath.Join(root, "orch")
			baseDir := filepath.Join(root, "project")
			if err := os.MkdirAll(filepath.Join(baseDir, ".tui-asset"), 0o755); err != nil {
				t.Fatal(err)
			}
			marker := filepath.Join(baseDir, ".tui-asset", ".insight.done")
			if err := os.WriteFile(marker, []byte("preserve"), 0o644); err != nil {
				t.Fatal(err)
			}
			manifest, err := json.Marshal(map[string]interface{}{
				"agent_name": "orch",
				"address":    remoteAddr,
				"admin":      map[string]interface{}{},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(orchDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(orchDir, ".agent.json"), manifest, 0o644); err != nil {
				t.Fatal(err)
			}

			m := NewMailModel(humanDir, "human", baseDir, orchDir, "orch", 20, "", "en", false, 0)
			if m.orchAddr != remoteAddr {
				t.Fatalf("orchAddr = %q, want %q", m.orchAddr, remoteAddr)
			}
			bindMailModelForAsyncTest(t, &m, 1)
			if tc.editor {
				var editorCmd tea.Cmd
				m, editorCmd = m.Update(EditorDoneMsg{envelope: captureAsync(asyncEditorDone, m.asyncCurrent()), Text: text})
				if editorCmd == nil {
					t.Fatal("editor completion did not schedule its repaint/refresh")
				}
				if m.pendingMessage != text || m.input.Value() != text {
					t.Fatalf("editor completion did not retain pending text: pending=%q input=%q", m.pendingMessage, m.input.Value())
				}
			} else {
				m.input.SetValue(text)
			}

			got, cmd := m.Update(SendMsg{})
			if cmd != nil {
				t.Fatalf("failed remote send returned success/refresh command %T", cmd)
			}
			if !strings.Contains(got.statusFlash, "unsupported remote mail address") {
				t.Fatalf("status flash = %q, want explicit remote-send error", got.statusFlash)
			}
			if !got.statusExpiry.After(time.Now()) {
				t.Fatalf("status expiry = %v, want future expiry", got.statusExpiry)
			}
			if got.input.Value() != text {
				t.Fatalf("input = %q, want retryable text %q", got.input.Value(), text)
			}
			if tc.editor && got.pendingMessage != text {
				t.Fatalf("pending message = %q, want retryable editor text %q", got.pendingMessage, text)
			}
			if got.acceptedSnapshot != nil && got.acceptedSnapshot.cache.Messages != nil {
				t.Fatalf("mail cache changed on failed send: %#v", got.acceptedSnapshot.cache.Messages)
			}
			if _, err := os.Stat(marker); err != nil {
				t.Fatalf("insight marker was not preserved: %v", err)
			}
			for _, path := range []string{
				filepath.Join(humanDir, "mailbox", "outbox"),
				filepath.Join(humanDir, "mailbox", "sent"),
				filepath.Join(orchDir, "mailbox", "inbox"),
				filepath.Join(orchDir, "mailbox", "sent"),
			} {
				if _, err := os.Stat(path); !os.IsNotExist(err) {
					t.Fatalf("failed remote send changed mailbox tree at %s (stat error: %v)", path, err)
				}
			}
		})
	}
}
