package fs

import (
	"bufio"
	"encoding/json"
	"fmt"
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

// Sampling grid and reconstruction budgets. Hostile or corrupt timestamps in
// events.jsonl or mailbox records must not be able to expand the reconstructed
// tape into millions of frames, so every timestamp is validated against an
// explicit plausibility window and the derived range is bounded by explicit
// span and frame-count budgets before any frame allocation begins.
const (
	// intervalMs is the activity-driven sampling grid (3s, matching the live
	// recorder) and maxGapMs the longest quiet stretch between heartbeat
	// samples (one frame per minute during long gaps).
	intervalMs int64 = 3000
	maxGapMs   int64 = 60 * 1000

	// minPlausibleTs / maxPlausibleTs bound accepted unix-seconds timestamps
	// (2001-09-09 .. year ~33658). Non-finite values and values outside this
	// window are treated as corrupt input and reject the reconstruction.
	minPlausibleTs = 1e9
	maxPlausibleTs = 1e12

	// maxReconstructSpanMs caps the wall-clock span of a reconstructed tape.
	// 5 years (365.25 days each) is far beyond any real project history and
	// keeps the worst-case minute-gap frame count within the frame budget.
	maxReconstructSpanMs int64 = 157788000000 // 5 * 365.25 * 86400 * 1000

	// maxReconstructFrames caps the total frames a single reconstruction may
	// allocate. The worst case is estimated cheaply from the span and the
	// number of activity samples before any frames are built.
	maxReconstructFrames = 3000000

	// maxEventLineBytes bounds a single events.jsonl line; oversized lines
	// fail the scan and surface as a reconstruction error instead of being
	// silently ignored.
	maxEventLineBytes = 4 << 20 // 4 MiB
)

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
		events, err := readEventsJSONL(a.WorkingDir)
		if err != nil {
			return nil, fmt.Errorf("read events for %s: %w", a.WorkingDir, err)
		}
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

	// 4. Determine time range.
	// minTs uses ALL events so an agent visible from heartbeats appears at its
	// real first-seen time. maxTs uses only mutation-causing events (state
	// changes + mail), so heartbeats during long-idle tails don't push the
	// timeline up to "now" with nothing happening.
	//
	// Every timestamp must be finite and inside the plausibility window: a
	// corrupt or hostile timestamp must reject the rebuild rather than being
	// allowed to expand the tape.
	minTs := math.MaxFloat64
	maxTs := 0.0
	for _, e := range allEvents {
		if !plausibleTimestamp(e.Ts) {
			return nil, fmt.Errorf("rejecting reconstruction: event timestamp %v is non-finite or implausible (window [%v, %v])", e.Ts, minPlausibleTs, maxPlausibleTs)
		}
		if e.Ts < minTs {
			minTs = e.Ts
		}
		if e.Type == "agent_state" && e.Ts > maxTs {
			maxTs = e.Ts
		}
	}
	for _, m := range allMail {
		if !plausibleTimestamp(m.ts) {
			return nil, fmt.Errorf("rejecting reconstruction: mail timestamp %v is non-finite or implausible (window [%v, %v])", m.ts, minPlausibleTs, maxPlausibleTs)
		}
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
	// No mutation-causing events at all → use minTs so we still emit at least
	// one frame (initial visibility snapshot).
	if maxTs == 0.0 {
		maxTs = minTs
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

	// 5. Build frames using activity-driven sampling.
	//
	// Sample points (snapped to a 3s grid for cache friendliness):
	//   - The first frame (startMs aligned).
	//   - Every agent_state event timestamp.
	//   - Every mail timestamp.
	//   - Every firstEventTs (so an agent appears the moment it becomes visible,
	//     even if its first event is a heartbeat).
	//   - One "heartbeat" sample per maxGapMs during long idle stretches.
	//   - The final frame (endMs aligned, +1 interval if needed).
	//
	// This produces O(events + mail + duration/maxGapMs) frames instead of
	// O(duration/3s). For a 2-week-old project with a few hundred events that
	// drops frame count by roughly 100x.
	startMs := int64(minTs * 1000)
	endMs := int64(maxTs * 1000)

	// Align start down (floor) and end up (ceil) so the final frame's
	// effective time is at or after the latest mutation event. Snapping the
	// end down can place all events into a single bucket whose tSec is
	// earlier than the actual event timestamps, leaving mail unprocessed.
	startMs = (startMs / intervalMs) * intervalMs
	endMs = ((endMs + intervalMs - 1) / intervalMs) * intervalMs
	if endMs < startMs {
		endMs = startMs
	}

	// Explicit span budget: long gaps add one sample per minute, so an
	// unbounded range is the primary frame-amplification vector. Reject the
	// reconstruction before any frame allocation begins.
	spanMs := endMs - startMs
	if spanMs > maxReconstructSpanMs {
		return nil, fmt.Errorf("rejecting reconstruction: range %d ms exceeds maximum span %d ms", spanMs, maxReconstructSpanMs)
	}

	// Build the sorted set of activity-driven sample timestamps. Each
	// interesting event is sampled at the next 3s tick (ceil) so the sample
	// occurs strictly at-or-after the event, ensuring the event-cursor
	// advances when we visit that sample.
	sampleSet := make(map[int64]struct{})
	snapUp := func(tMs int64) int64 { return ((tMs + intervalMs - 1) / intervalMs) * intervalMs }
	snapDown := func(tMs int64) int64 { return (tMs / intervalMs) * intervalMs }
	sampleSet[startMs] = struct{}{}
	sampleSet[endMs] = struct{}{}
	for _, e := range allEvents {
		if e.Type == "agent_state" {
			sampleSet[snapUp(int64(e.Ts*1000))] = struct{}{}
		}
	}
	for _, m := range allMail {
		sampleSet[snapUp(int64(m.ts*1000))] = struct{}{}
	}
	for _, ft := range firstEventTs {
		sampleSet[snapUp(int64(ft*1000))] = struct{}{}
	}

	// Explicit frame-count budget, estimated before the gap-fill materializes
	// anything: at most one heartbeat sample per maxGapMs across the span plus
	// one sample per activity point. Reject before any large slice allocation.
	if worst := reconstructionWorstCaseFrames(spanMs, len(sampleSet)); worst > maxReconstructFrames {
		return nil, fmt.Errorf("rejecting reconstruction: worst-case %d frames exceeds frame budget %d", worst, maxReconstructFrames)
	}

	// Add heartbeat samples during long gaps so the scrubber feels alive.
	sorted := make([]int64, 0, len(sampleSet))
	for t := range sampleSet {
		if t < startMs || t > endMs {
			continue
		}
		sorted = append(sorted, t)
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i] < sorted[j] })
	if len(sorted) == 0 || sorted[0] != startMs {
		sorted = append([]int64{startMs}, sorted...)
	}
	gapFilled := make([]int64, 0, len(sorted))
	for i, t := range sorted {
		gapFilled = append(gapFilled, t)
		if i+1 >= len(sorted) {
			continue
		}
		next := sorted[i+1]
		for h := t + maxGapMs; h < next; h += maxGapMs {
			gapFilled = append(gapFilled, snapDown(h))
		}
	}
	sorted = gapFilled

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

	for _, t := range sorted {
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
// A missing file is not an error (an agent without a log contributes no
// events); a scan failure (including an oversized line) is surfaced so the
// reconstruction cannot silently proceed on truncated input.
func readEventsJSONL(agentDir string) ([]eventRecord, error) {
	path := filepath.Join(agentDir, "logs", "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var events []eventRecord
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), maxEventLineBytes)
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
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan %s: %w", path, err)
	}
	return events, nil
}

// plausibleTimestamp reports whether a unix-seconds timestamp is finite and
// inside the explicit plausibility window. Values outside the window cannot
// be real project timestamps and would otherwise be free to expand the
// reconstructed tape.
func plausibleTimestamp(ts float64) bool {
	if math.IsNaN(ts) || math.IsInf(ts, 0) {
		return false
	}
	return ts >= minPlausibleTs && ts <= maxPlausibleTs
}

// reconstructionWorstCaseFrames upper-bounds the frames activity-driven
// sampling can produce for a span: at most one heartbeat sample per maxGapMs
// across the span, plus one sample per activity point, plus the aligned
// start/end anchors. It is a cheap pre-allocation estimate used to enforce
// the frame-count budget.
func reconstructionWorstCaseFrames(spanMs int64, activitySamples int) int64 {
	gapFrames := int64(0)
	if spanMs > 0 {
		gapFrames = (spanMs + maxGapMs - 1) / maxGapMs
	}
	return gapFrames + int64(activitySamples) + 2
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
