package fs

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// TapeFrame is a single timestamped snapshot of the network topology.
type TapeFrame struct {
	T   int64   `json:"t"`
	Net Network `json:"net"`
}

// eventRecord is one line from logs/events.jsonl.
type eventRecord struct {
	Type      string  `json:"type"`
	Ts        float64 `json:"ts"`
	Address   string  `json:"address"`
	AgentName string  `json:"agent_name"`
	Old       string  `json:"old"`
	New       string  `json:"new"`
}

// timestampedMail pairs a mail message with its parsed unix timestamp.
type timestampedMail struct {
	msg MailMessage
	ts  float64 // unix seconds
}

// ReconstructTape scans agent directories under baseDir, reads events.jsonl
// and mailbox contents, and reconstructs the full topology tape as a sequence
// of TapeFrame snapshots at 3-second intervals.
func ReconstructTape(baseDir string) ([]TapeFrame, error) {
	// 1. Discover all agents
	agents, err := DiscoverAgents(baseDir)
	if err != nil {
		return nil, err
	}
	if len(agents) == 0 {
		return nil, nil
	}

	// Normalize states to uppercase
	for i := range agents {
		agents[i].State = strings.ToUpper(agents[i].State)
	}

	// 2. Read all events across all agents
	var allEvents []eventRecord
	for _, a := range agents {
		events := readEventsJSONL(a.WorkingDir)
		allEvents = append(allEvents, events...)
	}

	// 3. Read all mail across all agents (inbox + archive)
	var allMail []timestampedMail
	for _, a := range agents {
		inbox, _ := readMailFolder(filepath.Join(a.WorkingDir, "mailbox", "inbox"))
		archive, _ := readMailFolder(filepath.Join(a.WorkingDir, "mailbox", "archive"))
		for _, msg := range append(inbox, archive...) {
			ts := mailTimestamp(msg)
			if ts > 0 {
				allMail = append(allMail, timestampedMail{msg: msg, ts: ts})
			}
		}
	}

	// 4. Determine time range
	minTs := math.MaxFloat64
	maxTs := 0.0
	for _, e := range allEvents {
		if e.Ts < minTs {
			minTs = e.Ts
		}
		if e.Ts > maxTs {
			maxTs = e.Ts
		}
	}
	for _, m := range allMail {
		if m.ts < minTs {
			minTs = m.ts
		}
		if m.ts > maxTs {
			maxTs = m.ts
		}
	}

	// No events and no mail → no frames
	if minTs == math.MaxFloat64 {
		return nil, nil
	}

	// Sort events by timestamp for replay
	sort.Slice(allEvents, func(i, j int) bool {
		return allEvents[i].Ts < allEvents[j].Ts
	})

	// Sort mail by timestamp
	sort.Slice(allMail, func(i, j int) bool {
		return allMail[i].ts < allMail[j].ts
	})

	// Determine first-event time per agent (for visibility: agent appears when its first event <= t)
	firstEventTs := make(map[string]float64)
	for _, e := range allEvents {
		if e.Address == "" {
			continue
		}
		resolved := ResolveAddress(e.Address, baseDir)
		if _, ok := firstEventTs[resolved]; !ok {
			firstEventTs[resolved] = e.Ts
		}
	}
	// Agents with mail but no events: use their first mail timestamp
	for _, m := range allMail {
		from := m.msg.From
		if from != "" {
			resolved := ResolveAddress(from, baseDir)
			if _, ok := firstEventTs[resolved]; !ok {
				firstEventTs[resolved] = m.ts
			}
		}
	}

	// Pre-read static edges (avatar + contact) once — they don't change per-frame
	var avatarEdges []AvatarEdge
	var contactEdges []ContactEdge
	for _, a := range agents {
		edges, _ := ReadLedger(a.WorkingDir)
		avatarEdges = append(avatarEdges, edges...)
		contactEdges = append(contactEdges, ReadContacts(a.WorkingDir)...)
	}
	if avatarEdges == nil {
		avatarEdges = []AvatarEdge{}
	}
	if contactEdges == nil {
		contactEdges = []ContactEdge{}
	}
	// Relativize static edges so they match AgentNode.Address format
	for i := range avatarEdges {
		avatarEdges[i].Parent = RelativizeAddress(avatarEdges[i].Parent, baseDir)
		avatarEdges[i].Child = RelativizeAddress(avatarEdges[i].Child, baseDir)
	}
	for i := range contactEdges {
		contactEdges[i].Owner = RelativizeAddress(contactEdges[i].Owner, baseDir)
		contactEdges[i].Target = RelativizeAddress(contactEdges[i].Target, baseDir)
	}

	// 5. Build frames at 3-second intervals
	const intervalMs int64 = 3000
	startMs := int64(minTs * 1000)
	endMs := int64(maxTs * 1000)

	// Align start to interval boundary (floor)
	startMs = (startMs / intervalMs) * intervalMs

	type edgeKey struct{ sender, recipient string }
	type edgeCounts struct{ direct, cc, bcc int }

	var frames []TapeFrame
	eventIdx := 0
	mailIdx := 0

	// Track agent states via replay. All agents start as ASLEEP.
	agentState := make(map[string]string)
	for _, a := range agents {
		if !a.IsHuman {
			agentState[a.WorkingDir] = "ASLEEP"
		}
	}

	// Cumulative mail counts — updated incrementally each frame
	mailCounts := make(map[edgeKey]*edgeCounts)

	ensure := func(k edgeKey) *edgeCounts {
		if c, ok := mailCounts[k]; ok {
			return c
		}
		c := &edgeCounts{}
		mailCounts[k] = c
		return c
	}

	for t := startMs; t <= endMs; t += intervalMs {
		tSec := float64(t) / 1000.0

		// Advance events up to this time
		for eventIdx < len(allEvents) && allEvents[eventIdx].Ts <= tSec {
			ev := allEvents[eventIdx]
			if ev.Type == "agent_state" && ev.Address != "" {
				resolved := ResolveAddress(ev.Address, baseDir)
				agentState[resolved] = strings.ToUpper(ev.New)
			}
			eventIdx++
		}

		// Advance mail frontier — only process new messages
		for mailIdx < len(allMail) && allMail[mailIdx].ts <= tSec {
			msg := allMail[mailIdx].msg
			from := ResolveAddress(msg.From, baseDir)
			recipients := resolveRecipients(msg.To)
			for _, r := range recipients {
				ensure(edgeKey{from, ResolveAddress(r, baseDir)}).direct++
			}
			for _, r := range msg.CC {
				ensure(edgeKey{from, ResolveAddress(r, baseDir)}).cc++
			}
			for _, r := range msg.BCC {
				ensure(edgeKey{from, ResolveAddress(r, baseDir)}).bcc++
			}
			mailIdx++
		}

		// Snapshot current cumulative mail counts into edges (relativized)
		var mailEdges []MailEdge
		for k, c := range mailCounts {
			mailEdges = append(mailEdges, MailEdge{
				Sender:    RelativizeAddress(k.sender, baseDir),
				Recipient: RelativizeAddress(k.recipient, baseDir),
				Count:     c.direct + c.cc + c.bcc,
				Direct:    c.direct,
				CC:        c.cc,
				BCC:       c.bcc,
			})
		}
		if mailEdges == nil {
			mailEdges = []MailEdge{}
		}

		// Build node list: human always present, agents visible after their first event
		var nodes []AgentNode
		for _, a := range agents {
			if a.IsHuman {
				node := a
				node.Alive = true
				node.State = "ACTIVE"
				nodes = append(nodes, node)
				continue
			}
			// Agent visible only if its first event is <= t
			ft, hasFirst := firstEventTs[a.WorkingDir]
			if !hasFirst || ft > tSec {
				continue
			}
			node := a
			if state, ok := agentState[a.WorkingDir]; ok {
				node.State = state
			} else {
				node.State = "ASLEEP"
			}
			node.Alive = node.State == "ACTIVE" || node.State == "IDLE"
			nodes = append(nodes, node)
		}
		if nodes == nil {
			nodes = []AgentNode{}
		}

		stats := computeStats(nodes, mailEdges)

		frames = append(frames, TapeFrame{
			T: t,
			Net: Network{
				Nodes:        nodes,
				AvatarEdges:  avatarEdges,
				ContactEdges: contactEdges,
				MailEdges:    mailEdges,
				Stats:        stats,
			},
		})
	}

	return frames, nil
}

// readEventsJSONL reads logs/events.jsonl and returns parsed event records
// for event types we care about: agent_state, heartbeat_start, refresh_start.
func readEventsJSONL(agentDir string) []eventRecord {
	path := filepath.Join(agentDir, "logs", "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()

	var events []eventRecord
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var ev eventRecord
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "agent_state", "heartbeat_start", "refresh_start":
			events = append(events, ev)
		}
	}
	return events
}

// mailTimestamp extracts the best timestamp from a mail message as unix seconds.
// Prefers SentAt, falls back to ReceivedAt.
func mailTimestamp(msg MailMessage) float64 {
	for _, raw := range []string{msg.SentAt, msg.ReceivedAt} {
		if raw == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, raw)
		if err != nil {
			t, err = time.Parse(time.RFC3339Nano, raw)
		}
		if err == nil {
			return float64(t.UnixMilli()) / 1000.0
		}
	}
	return 0
}
