package tui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
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

// buildMailboxEntries scans the mailbox of the given agent (or human) directory
// and returns MarkdownEntry items for the viewer. agentDir must be the full
// path to a directory containing a mailbox/inbox subdirectory (e.g.
// .lingtai/human or .lingtai/<agent>).
//
// Only the newest fs.RecentMessageLimit messages are loaded. That window is the
// page: entries outside it are never read, never rendered into markdown, and
// therefore never searched. No total-message count is computed anywhere.
func buildMailboxEntries(agentDir string) []MarkdownEntry {
	return buildMailboxEntriesWindow(agentDir, fs.RecentMessageLimit)
}

// buildMailboxEntriesWindow is buildMailboxEntries with an explicit window, so
// the cap is exercisable in a test without seeding a cap-sized mailbox.
func buildMailboxEntriesWindow(agentDir string, limit int) []MarkdownEntry {
	mailbox := filepath.Join(agentDir, "mailbox")
	mails := scanRecentInternalMailboxes(limit, []mailboxScanTarget{
		{dir: filepath.Join(mailbox, "inbox"), source: "inbox"},
		{dir: filepath.Join(mailbox, "sent"), source: "sent"},
		{dir: filepath.Join(mailbox, "archive"), source: "archive"},
	})

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

// mailboxScanTarget is one mailbox folder to display, plus the group label its
// messages are filed under in the viewer.
type mailboxScanTarget struct {
	dir    string
	source string
}

// mailboxLeaf is one candidate message: the folder it lives in and its mailbox
// id. The display window is settled over these — names only — before any body
// is opened.
type mailboxLeaf struct {
	target mailboxScanTarget
	id     string
}

// scanRecentInternalMailboxes loads the newest `limit` messages across every
// target folder. It is the only mailbox reader on the /mailbox path, and it is
// bounded before the expensive half: directory listings pick the window by
// mailbox id, and only the surviving ids have their message.json read and
// parsed. A mailbox holding a hundred thousand messages therefore costs one
// ReadDir per folder plus `limit` body reads, not one read per message ever
// received.
//
// Trimming per folder and then globally is exact rather than approximate: a
// folder can contribute at most `limit` entries to the merged newest-`limit`,
// so dropping its own older ids first cannot remove anything the global window
// would have kept. A limit <= 0 loads every message.
func scanRecentInternalMailboxes(limit int, targets []mailboxScanTarget) []parsedMail {
	var leaves []mailboxLeaf
	for _, target := range targets {
		for _, id := range fs.RecentMailboxIDs(target.dir, limit) {
			leaves = append(leaves, mailboxLeaf{target: target, id: id})
		}
	}
	// Order by mailbox id, which is a sortable UTC stamp, so the merged window
	// is chronological across folders. Leaves are kept per folder rather than
	// deduplicated by id: the same id in inbox/ and sent/ is a self-addressed
	// message and owns a row in both groups.
	sort.SliceStable(leaves, func(i, j int) bool { return leaves[i].id < leaves[j].id })
	if limit > 0 && len(leaves) > limit {
		leaves = leaves[len(leaves)-limit:]
	}

	mails := make([]parsedMail, 0, len(leaves))
	for _, leaf := range leaves {
		if mail, ok := readInternalMail(leaf.target, leaf.id); ok {
			mails = append(mails, mail)
		}
	}
	return mails
}

// readInternalMail reads and normalizes one mailbox leaf for display. A missing
// or malformed message.json reports false and is skipped, matching the previous
// scan's silent-skip behavior.
func readInternalMail(target mailboxScanTarget, id string) (parsedMail, bool) {
	data, err := os.ReadFile(filepath.Join(target.dir, id, "message.json"))
	if err != nil {
		return parsedMail{}, false
	}
	var msg mailMessage
	if json.Unmarshal(data, &msg) != nil {
		return parsedMail{}, false
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

	return parsedMail{
		From:        msg.From,
		To:          to,
		Subject:     msg.Subject,
		Body:        msg.Message,
		Time:        t,
		Attachments: atts,
		Source:      target.source,
	}, true
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
