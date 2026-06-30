package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
)

func TestSaveClipboardImageBytesWritesFile(t *testing.T) {
	dir := t.TempDir()
	data := []byte("fake-png")
	path, err := saveClipboardImageBytes(dir, data, time.Unix(123, 456))
	if err != nil {
		t.Fatalf("saveClipboardImageBytes returned error: %v", err)
	}
	if !filepath.IsAbs(path) {
		t.Fatalf("expected absolute path, got %q", path)
	}
	if got, want := filepath.Base(path), "lingtai-paste-123000000456.png"; got != want {
		t.Fatalf("filename = %q, want %q", got, want)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read saved file: %v", err)
	}
	if string(got) != string(data) {
		t.Fatalf("saved data = %q, want %q", got, data)
	}
}

func TestSaveClipboardImageBytesRejectsEmptyClipboard(t *testing.T) {
	_, err := saveClipboardImageBytes(t.TempDir(), nil, time.Now())
	if !errors.Is(err, errClipboardImageUnavailable) {
		t.Fatalf("error = %v, want errClipboardImageUnavailable", err)
	}
}

func TestInputAppendTextAddsNewLine(t *testing.T) {
	m := NewInputModel(t.TempDir())
	m.SetValue("describe this")
	m.AppendText(pastedImageReference("/tmp/shot.png"))
	if got, want := m.Value(), "describe this\n[pasted image: /tmp/shot.png]"; got != want {
		t.Fatalf("input value = %q, want %q", got, want)
	}
}

func TestCtrlVPastesClipboardImageReference(t *testing.T) {
	old := readClipboardImage
	readClipboardImage = func() ([]byte, error) { return []byte("fake-png"), nil }
	t.Cleanup(func() { readClipboardImage = old })

	humanDir := t.TempDir()
	m := MailModel{
		humanDir: humanDir,
		input:    NewInputModel(humanDir),
	}
	updated, cmd := m.Update(tea.KeyPressMsg{Code: 'v', Mod: tea.ModCtrl})
	if cmd != nil {
		t.Fatalf("ctrl+v returned unexpected command")
	}
	val := updated.input.Value()
	if !strings.Contains(val, "[pasted image: ") {
		t.Fatalf("input value %q does not contain pasted image reference", val)
	}
	path := strings.TrimSuffix(strings.TrimPrefix(val, "[pasted image: "), "]")
	if !strings.HasPrefix(path, filepath.Join(humanDir, "attachments", "pasted-images")) {
		t.Fatalf("pasted path %q not under human attachments dir", path)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("saved pasted image missing: %v", err)
	}
	if !strings.Contains(updated.statusFlash, path) {
		t.Fatalf("statusFlash %q does not mention path %q", updated.statusFlash, path)
	}
}
