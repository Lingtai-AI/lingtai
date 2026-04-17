package postman

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// OutboxItem represents a queued message ready for remote delivery.
type OutboxItem struct {
	ID       string // UUID directory name
	PeerAddr string // IPv6 address of the remote postman
	Data     []byte // raw message.json content
	AgentDir string // the agent whose outbox this came from
}

// ScanOutbox reads an agent's mailbox/outbox/ and returns items with remote addresses.
func ScanOutbox(agentDir string) []OutboxItem {
	outboxDir := filepath.Join(agentDir, "mailbox", "outbox")
	entries, err := os.ReadDir(outboxDir)
	if err != nil {
		return nil
	}

	var items []OutboxItem
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		msgPath := filepath.Join(outboxDir, e.Name(), "message.json")
		data, err := os.ReadFile(msgPath)
		if err != nil {
			continue
		}

		var envelope struct {
			MailboxID string      `json:"_mailbox_id"`
			ID        string      `json:"id"`
			To        interface{} `json:"to"`
		}
		if json.Unmarshal(data, &envelope) != nil {
			continue
		}

		msgID := envelope.MailboxID
		if msgID == "" {
			msgID = envelope.ID
		}

		var peerAddr string
		var recipients []string
		switch v := envelope.To.(type) {
		case string:
			recipients = []string{v}
		case []interface{}:
			for _, item := range v {
				if s, ok := item.(string); ok {
					recipients = append(recipients, s)
				}
			}
		}
		for _, addr := range recipients {
			if fs.IsRemoteAddress(addr) {
				host, _, ok := fs.ParseAddress(addr)
				if ok {
					peerAddr = host
					break
				}
			}
		}

		if peerAddr == "" {
			continue
		}

		items = append(items, OutboxItem{
			ID:       e.Name(), // use directory name for cleanup, not message ID
			PeerAddr: peerAddr,
			Data:     data,
			AgentDir: agentDir,
		})
	}
	return items
}

// CleanOutboxItem removes a delivered message from the outbox.
func CleanOutboxItem(agentDir, msgID string) {
	dir := filepath.Join(agentDir, "mailbox", "outbox", msgID)
	os.RemoveAll(dir)
}

// SendUDP sends an encoded datagram to a remote postman.
func SendUDP(peerAddr string, port int, payload []byte) error {
	encoded, err := Encode(payload)
	if err != nil {
		return fmt.Errorf("encode: %w", err)
	}

	addr := &net.UDPAddr{
		IP:   net.ParseIP(peerAddr),
		Port: port,
	}
	if addr.IP == nil {
		return fmt.Errorf("invalid peer address: %q", peerAddr)
	}

	conn, err := net.DialUDP("udp", nil, addr)
	if err != nil {
		return fmt.Errorf("dial %s:%d: %w", peerAddr, port, err)
	}
	defer conn.Close()

	if _, err := conn.Write(encoded); err != nil {
		return fmt.Errorf("send: %w", err)
	}
	return nil
}
