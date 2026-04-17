package postman

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// ExtractRecipientPath extracts the local filesystem path from an absolute address.
func ExtractRecipientPath(addr string) (string, bool) {
	_, path, ok := fs.ParseAddress(addr)
	return path, ok
}

// DeliverLocal writes a message to the agent's inbox at the given path.
// If the path doesn't exist, the message is silently dropped.
func DeliverLocal(agentDir, msgID string, data []byte) error {
	inboxBase := filepath.Join(agentDir, "mailbox", "inbox")
	if _, err := os.Stat(inboxBase); os.IsNotExist(err) {
		return nil
	}

	msgDir := filepath.Join(inboxBase, msgID)
	if err := os.MkdirAll(msgDir, 0o755); err != nil {
		return fmt.Errorf("create inbox dir: %w", err)
	}
	if err := os.WriteFile(filepath.Join(msgDir, "message.json"), data, 0o644); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

// HandleDatagram processes a single incoming UDP datagram.
func HandleDatagram(data []byte) error {
	payload, err := Decode(data)
	if err != nil {
		return fmt.Errorf("decode: %w", err)
	}

	var envelope struct {
		MailboxID string      `json:"_mailbox_id"`
		ID        string      `json:"id"`
		To        interface{} `json:"to"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		return fmt.Errorf("parse envelope: %w", err)
	}

	msgID := envelope.MailboxID
	if msgID == "" {
		msgID = envelope.ID
	}
	if msgID == "" {
		return fmt.Errorf("message has no id")
	}

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
		path, ok := ExtractRecipientPath(addr)
		if !ok {
			continue
		}
		if err := DeliverLocal(path, msgID, payload); err != nil {
			fmt.Printf("postman: deliver to %s failed: %v\n", path, err)
		}
	}
	return nil
}

// ListenUDP starts listening for incoming LTPM datagrams.
// Blocks until stop channel is closed.
func ListenUDP(port int, stop <-chan struct{}) error {
	addr := &net.UDPAddr{Port: port}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("listen udp :%d: %w", port, err)
	}
	defer conn.Close()

	go func() {
		<-stop
		conn.Close()
	}()

	buf := make([]byte, 65536)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-stop:
				return nil
			default:
				return fmt.Errorf("read udp: %w", err)
			}
		}
		datagram := make([]byte, n)
		copy(datagram, buf[:n])
		go func(d []byte) {
			if err := HandleDatagram(d); err != nil {
				fmt.Printf("postman: datagram error: %v\n", err)
			}
		}(datagram)
	}
}
