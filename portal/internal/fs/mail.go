package fs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// newMailboxID builds a sortable, human-scannable mailbox id. The format
// (`YYYYMMDDTHHMMSS-xxxx`, 20 chars, UTC) matches the kernel helper
// `_new_mailbox_id` in lingtai-kernel `src/lingtai/kernel/services/mail.py`
// so records read by either binary retain the same mailbox identity shape.
// The 4-hex suffix is drawn from a UUID4.
var mailboxIDSource = func() string {
	ts := time.Now().UTC().Format("20060102T150405")
	suffix := uuid.New().String()[:4]
	return ts + "-" + suffix
}

func newMailboxID() string {
	return mailboxIDSource()
}

func ReadInbox(dir string) ([]MailMessage, error) {
	return readMailFolder(filepath.Join(dir, "mailbox", "inbox"))
}

func ReadArchive(dir string) ([]MailMessage, error) {
	return readMailFolder(filepath.Join(dir, "mailbox", "archive"))
}

func readMailFolder(folder string) ([]MailMessage, error) {
	entries, err := os.ReadDir(folder)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read folder: %w", err)
	}
	var messages []MailMessage
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		msgPath := filepath.Join(folder, entry.Name(), "message.json")
		data, err := os.ReadFile(msgPath)
		if err != nil {
			continue
		}
		var msg MailMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}
