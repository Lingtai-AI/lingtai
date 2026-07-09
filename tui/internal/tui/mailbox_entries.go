package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// mailMessage is the union of internal mailbox and IMAP message formats.
// Uses interface{} for polymorphic fields to avoid unmarshal failures
// when a field's shape doesn't match expectations (e.g. attachments
// may be strings, objects, or absent depending on the source).
type mailMessage struct {
	// Common
	From    string `json:"from"`
	Subject string `json:"subject"`
	Message string `json:"message"`

	// Internal mailbox
	To         interface{} `json:"to"`          // string or []string
	ReceivedAt string      `json:"received_at"` // inbox
	SentAt     string      `json:"sent_at"`     // sent/
	Type       string      `json:"type"`

	// IMAP
	EmailID     string      `json:"email_id"`
	FromAddress string      `json:"from_address"`
	Date        string      `json:"date"`
	Attachments interface{} `json:"attachments"` // []mailAttachment or []string or nil
	Files       interface{} `json:"files"`       // some messages use "files" instead
}

type mailAttachment struct {
	Filename    string `json:"filename"`
	ContentType string `json:"content_type"`
	Size        int    `json:"size"`
	Path        string `json:"path"`
}

// parseAttachments extracts attachments from the polymorphic Attachments/Files fields.
func parseAttachments(raw interface{}) []mailAttachment {
	if raw == nil {
		return nil
	}
	arr, ok := raw.([]interface{})
	if !ok {
		return nil
	}
	var result []mailAttachment
	for _, item := range arr {
		switch v := item.(type) {
		case map[string]interface{}:
			att := mailAttachment{}
			if s, ok := v["filename"].(string); ok {
				att.Filename = s
			}
			if s, ok := v["content_type"].(string); ok {
				att.ContentType = s
			}
			if n, ok := v["size"].(float64); ok {
				att.Size = int(n)
			}
			if s, ok := v["path"].(string); ok {
				att.Path = s
			}
			if att.Filename != "" || att.Path != "" {
				result = append(result, att)
			}
		case string:
			// Plain file path
			result = append(result, mailAttachment{
				Filename: filepath.Base(v),
				Path:     v,
			})
		}
	}
	return result
}

// parsedMail is a normalized representation for display.
type parsedMail struct {
	From        string
	To          string
	Subject     string
	Body        string
	Time        time.Time
	Attachments []mailAttachment
	Source      string // "inbox", "sent", "imap:account"
}

// buildMailboxEntries scans the inbox of the given agent (or human) directory
// and returns MarkdownEntry items for the viewer. agentDir must be the full
// path to a directory containing a mailbox/inbox subdirectory (e.g.
// .lingtai/human or .lingtai/<agent>).
func buildMailboxEntries(agentDir string) []MarkdownEntry {
	var mails []parsedMail

	inbox := filepath.Join(agentDir, "mailbox", "inbox")
	mails = append(mails, scanInternalMailbox(inbox, "inbox")...)

	sent := filepath.Join(agentDir, "mailbox", "sent")
	mails = append(mails, scanInternalMailbox(sent, "sent")...)

	archive := filepath.Join(agentDir, "mailbox", "archive")
	mails = append(mails, scanInternalMailbox(archive, "archive")...)

	// Sort by time descending (newest first)
	sort.Slice(mails, func(i, j int) bool {
		return mails[i].Time.After(mails[j].Time)
	})

	// Group by source
	groups := map[string][]parsedMail{}
	var groupOrder []string
	for _, m := range mails {
		if _, seen := groups[m.Source]; !seen {
			groupOrder = append(groupOrder, m.Source)
		}
		groups[m.Source] = append(groups[m.Source], m)
	}

	// Build entries
	var result []MarkdownEntry
	for _, group := range groupOrder {
		for _, m := range groups[group] {
			// Build label: "MM-DD <subject-or-fallback> 📎"
			dateStr := ""
			if !m.Time.IsZero() {
				dateStr = m.Time.Local().Format("01-02") + " "
			}
			attIcon := ""
			if len(m.Attachments) > 0 {
				attIcon = " 📎"
			}
			subject := strings.TrimSpace(m.Subject)
			// Treat bare reply prefixes as "no subject" so 5 successive
			// replies to a naked thread don't all collapse to "Re: ".
			if isDegenerateSubject(subject) {
				subject = ""
			}
			displaySubject := subject
			if displaySubject == "" {
				displaySubject = bodyPreview(m.Body)
			}
			labelTail := displaySubject
			if labelTail == "" {
				labelTail = m.From
			}
			label := truncate(dateStr+labelTail, 33-runeLen(attIcon)) + attIcon

			// Build right-panel content
			var md strings.Builder
			if subject != "" {
				md.WriteString("# " + subject + "\n\n")
			}
			md.WriteString(fmt.Sprintf("**From:** %s  \n", m.From))
			if m.To != "" {
				md.WriteString(fmt.Sprintf("**To:** %s  \n", m.To))
			}
			if !m.Time.IsZero() {
				md.WriteString(fmt.Sprintf("**Date:** %s\n", m.Time.Local().Format("2006-01-02 15:04 MST")))
			}
			md.WriteString("\n---\n\n")
			md.WriteString(m.Body)

			// Render attachments
			if len(m.Attachments) > 0 {
				md.WriteString("\n\n---\n\n## Attachments\n\n")
				for _, att := range m.Attachments {
					md.WriteString(fmt.Sprintf("### 📎 %s\n", att.Filename))
					md.WriteString(fmt.Sprintf("*%s · %s*\n\n", att.ContentType, formatSize(att.Size)))

					// Inline text-based attachments
					if isTextAttachment(att.ContentType, att.Filename) && att.Path != "" {
						data, err := os.ReadFile(att.Path)
						if err == nil {
							md.WriteString("```\n" + string(data) + "\n```\n\n")
						} else {
							md.WriteString(fmt.Sprintf("*(file not found: %s)*\n\n", att.Path))
						}
					} else {
						md.WriteString(fmt.Sprintf("Path: `%s`\n\n", att.Path))
					}
				}
			}

			groupLabel := group
			switch group {
			case "inbox":
				groupLabel = "Inbox"
			case "sent":
				groupLabel = "Sent"
			case "archive":
				groupLabel = "Archive"
			default:
				if strings.HasPrefix(group, "imap:") {
					groupLabel = "✉ " + group[5:]
				}
			}

			result = append(result, MarkdownEntry{
				Label:   label,
				Group:   groupLabel,
				Content: md.String(),
			})
		}
	}

	return result
}

func scanInternalMailbox(dir, source string) []parsedMail {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var mails []parsedMail
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		msgPath := filepath.Join(dir, entry.Name(), "message.json")
		data, err := os.ReadFile(msgPath)
		if err != nil {
			continue
		}
		var msg mailMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		stamp := msg.ReceivedAt
		if stamp == "" {
			stamp = msg.SentAt
		}
		t, _ := time.Parse(time.RFC3339, stamp)

		to := ""
		switch v := msg.To.(type) {
		case string:
			to = v
		case []interface{}:
			parts := make([]string, 0, len(v))
			for _, x := range v {
				if s, ok := x.(string); ok {
					parts = append(parts, s)
				}
			}
			to = strings.Join(parts, ", ")
		}

		atts := parseAttachments(msg.Attachments)
		if len(atts) == 0 {
			atts = parseAttachments(msg.Files)
		}

		mails = append(mails, parsedMail{
			From:        msg.From,
			To:          to,
			Subject:     msg.Subject,
			Body:        msg.Message,
			Time:        t,
			Attachments: atts,
			Source:      source,
		})
	}
	return mails
}

func scanImapMailbox(dir, account string) []parsedMail {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var mails []parsedMail
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		msgPath := filepath.Join(dir, entry.Name(), "message.json")
		data, err := os.ReadFile(msgPath)
		if err != nil {
			continue
		}
		var msg mailMessage
		if json.Unmarshal(data, &msg) != nil {
			continue
		}

		from := msg.From
		if msg.FromAddress != "" && msg.FromAddress != msg.From {
			from = msg.From + " <" + msg.FromAddress + ">"
		}

		// Parse IMAP date (RFC 2822 style)
		t, _ := time.Parse("Mon, 02 Jan 2006 15:04:05 -0700", msg.Date)

		atts := parseAttachments(msg.Attachments)
		if len(atts) == 0 {
			atts = parseAttachments(msg.Files)
		}

		mails = append(mails, parsedMail{
			From:        from,
			To:          account,
			Subject:     msg.Subject,
			Body:        msg.Message,
			Time:        t,
			Attachments: atts,
			Source:      "imap:" + account,
		})
	}
	return mails
}

func isTextAttachment(contentType, filename string) bool {
	if strings.HasPrefix(contentType, "text/") {
		return true
	}
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".md", ".txt", ".json", ".csv", ".log", ".py", ".go", ".js", ".yaml", ".yml", ".toml", ".xml", ".html":
		return true
	}
	return false
}

func formatSize(bytes int) string {
	if bytes < 1024 {
		return fmt.Sprintf("%d B", bytes)
	}
	if bytes < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(bytes)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
}

// isDegenerateSubject reports whether a subject is just a reply/forward
// prefix with no real content (e.g. "Re:", "Re: ", "RE:", "Fwd:"). A naked
// thread (original subject empty) propagates "Re: " on every reply, which
// makes inbox rows indistinguishable.
func isDegenerateSubject(s string) bool {
	t := strings.TrimSpace(s)
	if t == "" {
		return true
	}
	low := strings.ToLower(t)
	low = strings.TrimSuffix(low, ":")
	low = strings.TrimSpace(low)
	switch low {
	case "re", "fwd", "fw":
		return true
	}
	return false
}

// bodyPreview returns the first non-empty line of body, collapsed to a
// single line for use as a fallback inbox label.
func bodyPreview(body string) string {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(raw)
		if line != "" {
			return line
		}
	}
	return ""
}

// runeLen counts runes in s (mirrors len() but on glyphs, not bytes).
func runeLen(s string) int { return len([]rune(s)) }
