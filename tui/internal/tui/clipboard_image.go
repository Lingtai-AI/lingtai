package tui

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"golang.design/x/clipboard"

	"github.com/anthropics/lingtai-tui/i18n"
)

var errClipboardImageUnavailable = errors.New("clipboard does not contain an image")

var (
	clipboardInitOnce  sync.Once
	clipboardInitErr   error
	readClipboardImage = defaultReadClipboardImage
)

func defaultReadClipboardImage() ([]byte, error) {
	clipboardInitOnce.Do(func() {
		clipboardInitErr = clipboard.Init()
	})
	if clipboardInitErr != nil {
		return nil, clipboardInitErr
	}
	return clipboard.Read(clipboard.FmtImage), nil
}

func saveClipboardImageFromClipboard(dir string) (string, error) {
	data, err := readClipboardImage()
	if err != nil {
		return "", err
	}
	return saveClipboardImageBytes(dir, data, time.Now())
}

func saveClipboardImageBytes(dir string, data []byte, now time.Time) (string, error) {
	if len(data) == 0 {
		return "", errClipboardImageUnavailable
	}
	if dir == "" {
		return "", errors.New("clipboard image directory is empty")
	}
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(absDir, 0o700); err != nil {
		return "", err
	}
	name := fmt.Sprintf("lingtai-paste-%d.png", now.UnixNano())
	path := filepath.Join(absDir, name)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return "", err
	}
	return path, nil
}

func pastedImageReference(path string) string {
	return fmt.Sprintf("[pasted image: %s]", path)
}

func (m *MailModel) pasteClipboardImageFromSystem() {
	path, err := saveClipboardImageFromClipboard(filepath.Join(m.humanDir, "attachments", "pasted-images"))
	if err != nil {
		m.statusFlash = fmt.Sprintf(i18n.T("mail.image_paste_failed"), err)
		m.statusExpiry = time.Now().Add(5 * time.Second)
		return
	}
	m.input.AppendText(pastedImageReference(path))
	m.syncViewportHeight()
	m.statusFlash = fmt.Sprintf(i18n.T("mail.image_paste_saved"), path)
	m.statusExpiry = time.Now().Add(5 * time.Second)
}
