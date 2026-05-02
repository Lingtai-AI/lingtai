// internal/fs/session.go — append-only session log and in-memory cache.
package fs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SessionEntry is the JSON-serializable entry stored in session.jsonl.
type SessionEntry struct {
	Ts          string   `json:"ts"`
	Type        string   `json:"type"`
	From        string   `json:"from,omitempty"`
	To          string   `json:"to,omitempty"`
	Subject     string   `json:"subject,omitempty"`
	Body        string   `json:"body"`
	Question    string   `json:"question,omitempty"`
	Attachments []string `json:"attachments,omitempty"`
	Source      string   `json:"source,omitempty"` // "human", "insight" — for inquiry entries
	FireID      string   `json:"fire_id,omitempty"` // soul_flow fires — used to look up voices in soul_flow.jsonl

	// Delivered is a transient field propagated from MailMessage.Delivered.
	// Only meaningful for Type == "mail". Not persisted to session.jsonl.
	Delivered bool `json:"-"`
}

// SessionCache is an append-only cache backed by session.jsonl.
// It incrementally tails three data sources and appends new entries.
type SessionCache struct {
	path        string          // human/logs/session.jsonl
	entries     []SessionEntry  // in-memory mirror of all entries
	lastMailTs  string          // highest mail ReceivedAt ingested (watermark for live-session dedup)
	eventsOff   int64           // byte offset in events.jsonl
	inquiryOff  int64           // byte offset in soul_inquiry.jsonl
	soulFlowOff int64           // byte offset in soul_flow.jsonl (voice index source)
	projectPath string          // absolute path of the project directory (parent of .lingtai/)
	lastHour    time.Time       // hour (truncated) of the most recent entry
	briefBase   string          // base dir for brief output (default: ~/.lingtai-tui)
	rebuilding  bool            // true during RebuildFromSources — suppress file writes

	// soulVoices indexes voices by fire_id, populated by tailing
	// soul_flow.jsonl. Used to inflate soul_flow SessionEntry bodies that
	// couldn't be rendered from events.jsonl alone — older fires (logged
	// before the inline-voices change in kernel commit 549c78d) only have
	// fire_id+sources in events.jsonl; the actual voice text lives here.
	soulVoices map[string][]soulVoiceRecord
}

// soulVoiceRecord is one parsed voice entry from soul_flow.jsonl,
// indexed by fire_id for body inflation.
type soulVoiceRecord struct {
	Source string
	Voice  string
}

// NewSessionCache opens (or creates) session.jsonl. Call RebuildFromSources
// after construction to populate the cache from authoritative sources.
func NewSessionCache(humanDir string, projectPath string) *SessionCache {
	logsDir := filepath.Join(humanDir, "logs")
	os.MkdirAll(logsDir, 0o755)
	path := filepath.Join(logsDir, "session.jsonl")

	home, _ := os.UserHomeDir()
	sc := &SessionCache{
		path:        path,
		projectPath: projectPath,
		briefBase:   filepath.Join(home, ".lingtai-tui"),
		soulVoices:  make(map[string][]soulVoiceRecord),
	}
	return sc
}


// RebuildFromSources reads all three data sources from scratch, merges and
// sorts them chronologically, writes session.jsonl, and sets offsets to EOF
// so subsequent Refresh calls only append new entries.
func (sc *SessionCache) RebuildFromSources(cache MailCache, humanAddr, orchDir, orchName string) {
	// Clear any prior state and suppress file writes during ingest
	// (we'll write the sorted result in one shot at the end).
	sc.entries = nil
	sc.eventsOff = 0
	sc.inquiryOff = 0
	sc.soulFlowOff = 0
	sc.soulVoices = make(map[string][]soulVoiceRecord)
	sc.rebuilding = true

	// Ingest everything from offset 0.
	sc.IngestMail(cache, humanAddr, orchDir, orchName)
	sc.IngestEvents(orchDir)
	sc.IngestInquiries(orchDir)

	sc.rebuilding = false

	// Sort by unix timestamp.
	sort.SliceStable(sc.entries, func(i, j int) bool {
		return tsToUnix(sc.entries[i].Ts) < tsToUnix(sc.entries[j].Ts)
	})

	// Write sorted session.jsonl in one shot.
	sc.rewriteFile()

	// Set offsets to EOF so Refresh only tails new entries.
	if orchDir != "" {
		sc.eventsOff = fileSize(filepath.Join(orchDir, "logs", "events.jsonl"))
		sc.inquiryOff = fileSize(filepath.Join(orchDir, "logs", "soul_inquiry.jsonl"))
		sc.soulFlowOff = fileSize(filepath.Join(orchDir, "logs", "soul_flow.jsonl"))
	}

	// Regenerate history markdown files for the secretary.
	if sc.projectPath != "" {
		hash := ProjectHash(sc.projectPath)
		histDir := briefHistoryDir(sc.briefBase, hash)
		DumpAllHours(sc.entries, histDir)
	}

	// Set lastHour from the final entry.
	if len(sc.entries) > 0 {
		if t, err := time.Parse(time.RFC3339Nano, sc.entries[len(sc.entries)-1].Ts); err == nil {
			sc.lastHour = t.Truncate(time.Hour)
		}
	}

	// Set mail watermark to the max ReceivedAt so live-session IngestMail
	// calls only accept strictly-newer mail.
	sc.lastMailTs = ""
	for _, e := range sc.entries {
		if e.Type == "mail" && e.Ts > sc.lastMailTs {
			sc.lastMailTs = e.Ts
		}
	}
}

// rewriteFile overwrites session.jsonl with the current in-memory entries.
func (sc *SessionCache) rewriteFile() {
	f, err := os.Create(sc.path)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range sc.entries {
		_ = enc.Encode(e)
	}
}

func (sc *SessionCache) append(entries ...SessionEntry) {
	if len(entries) == 0 {
		return
	}

	sc.entries = append(sc.entries, entries...)

	// During RebuildFromSources, skip file writes and hour dumps —
	// we'll write the sorted result in one shot at the end.
	if sc.rebuilding {
		return
	}

	// Check for hour boundary crossings.
	for _, e := range entries {
		t := ParseSessionTs(e.Ts)
		if t.IsZero() {
			continue
		}
		entryHour := t.Truncate(time.Hour)
		if !sc.lastHour.IsZero() && entryHour.After(sc.lastHour) {
			sc.dumpHours(sc.lastHour, entryHour)
		}
		sc.lastHour = entryHour
	}

	// Append to file.
	f, err := os.OpenFile(sc.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetEscapeHTML(false)
	for _, e := range entries {
		_ = enc.Encode(e)
	}
}

// dumpHours dumps all completed hours from fromHour up to (but not including) toHour.
// Capped at 24 hours to avoid pathological dumps when the TUI resumes after a long gap
// (e.g., upgrading from a version without the dump feature).
func (sc *SessionCache) dumpHours(fromHour, toHour time.Time) {
	if sc.projectPath == "" {
		return
	}
	hash := ProjectHash(sc.projectPath)
	histDir := briefHistoryDir(sc.briefBase, hash)

	// Cap: only dump the most recent 24 hours to avoid spike on long gaps.
	earliest := toHour.Add(-24 * time.Hour)
	if fromHour.Before(earliest) {
		fromHour = earliest
	}

	for h := fromHour; h.Before(toHour); h = h.Add(time.Hour) {
		// Collect entries for this hour.
		var hourEntries []SessionEntry
		for _, e := range sc.entries {
			t, err := time.Parse(time.RFC3339, e.Ts)
			if err != nil {
				continue
			}
			if t.Truncate(time.Hour).Equal(h) {
				hourEntries = append(hourEntries, e)
			}
		}
		dumpCompletedHour(hourEntries, h, histDir)
	}
}

// Entries returns all entries in the cache.
func (sc *SessionCache) Entries() []SessionEntry {
	return sc.entries
}

// Len returns the total number of entries.
func (sc *SessionCache) Len() int {
	return len(sc.entries)
}

// ---------------------------------------------------------------------------
// Mail ingestion
// ---------------------------------------------------------------------------

// IngestMail appends new mail messages to the session log.
// humanAddr is the human's mail address (to determine IsFromMe).
// orchName is the orchestrator's display name.
func (sc *SessionCache) IngestMail(cache MailCache, humanAddr, orchDir, orchName string) {
	var newEntries []SessionEntry
	for _, msg := range cache.Messages {
		// Skip mail at or below the watermark — already ingested either in this
		// session or in a prior rebuild. During RebuildFromSources the watermark
		// is empty so this admits every mail.
		if !sc.rebuilding && msg.ReceivedAt <= sc.lastMailTs {
			continue
		}

		from := resolveMailFrom(msg, humanAddr)
		to := resolveMailTo(msg, humanAddr, orchName)

		newEntries = append(newEntries, SessionEntry{
			Ts:          msg.ReceivedAt,
			Type:        "mail",
			From:        from,
			To:          to,
			Subject:     msg.Subject,
			Body:        msg.Message,
			Attachments: msg.Attachments,
			Delivered:   msg.Delivered,
		})

		// Advance watermark. During rebuild we set it in one shot at the end
		// (see RebuildFromSources), so only track during live-session appends.
		if !sc.rebuilding && msg.ReceivedAt > sc.lastMailTs {
			sc.lastMailTs = msg.ReceivedAt
		}
	}
	sc.append(newEntries...)
}

func resolveMailFrom(msg MailMessage, humanAddr string) string {
	parts := splitLast(msg.From, "/")
	if msg.From == humanAddr || parts == "human" {
		return "human"
	}
	if nick, ok := msg.Identity["nickname"].(string); ok && nick != "" {
		return nick
	}
	if name, ok := msg.Identity["agent_name"].(string); ok && name != "" {
		return name
	}
	return parts
}

func resolveMailTo(msg MailMessage, humanAddr, orchName string) string {
	to := fmt.Sprintf("%v", msg.To)
	if to == humanAddr {
		return "human"
	}
	return orchName
}

func splitLast(s, sep string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if string(s[i]) == sep {
			return s[i+1:]
		}
	}
	return s
}

// ---------------------------------------------------------------------------
// Events ingestion
// ---------------------------------------------------------------------------

// IngestEvents tails the orchestrator's events.jsonl from the last-read offset,
// converting new entries to SessionEntry. ALL event types are ingested — verbose
// filtering happens at render time.
//
// Also refreshes the soul_flow voice index BEFORE parsing events so
// fresh consultation_fire entries can be inflated with voice text on
// the same poll. Inflates the bodies of any soul_flow entries (new or
// already-cached) that came back as the fallback summary.
func (sc *SessionCache) IngestEvents(orchDir string) {
	if orchDir == "" {
		return
	}
	sc.ingestSoulFlowVoices(orchDir)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	newEntries, newOff := sc.tailJSONL(eventsPath, sc.eventsOff, parseEvent)
	sc.eventsOff = newOff
	for i := range newEntries {
		sc.maybeInflateSoulFlow(&newEntries[i])
	}
	// Inflate any pre-existing entries (those parsed in earlier polls
	// before their voices landed in soul_flow.jsonl, e.g. on initial
	// rebuild from sources or a later kernel write).
	for i := range sc.entries {
		sc.maybeInflateSoulFlow(&sc.entries[i])
	}
	sc.append(newEntries...)
}

// ingestSoulFlowVoices tails soul_flow.jsonl from the last-read offset
// and updates sc.soulVoices, the fire_id→[]voice map. Idempotent.
func (sc *SessionCache) ingestSoulFlowVoices(orchDir string) {
	path := filepath.Join(orchDir, "logs", "soul_flow.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return
	}
	if info.Size() < sc.soulFlowOff {
		// File truncated (e.g. agent reset) — restart from the beginning
		// and clear the index so we don't carry stale voices.
		sc.soulFlowOff = 0
		sc.soulVoices = make(map[string][]soulVoiceRecord)
	}
	if info.Size() == sc.soulFlowOff {
		return
	}
	if _, err := f.Seek(sc.soulFlowOff, io.SeekStart); err != nil {
		return
	}

	buf, err := io.ReadAll(f)
	if err != nil {
		return
	}
	// Only consume up to the last newline — trailing partial lines are
	// re-read on the next poll.
	last := bytes.LastIndexByte(buf, '\n')
	if last < 0 {
		return
	}
	consumed := buf[:last+1]
	for _, line := range bytes.Split(consumed, []byte{'\n'}) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		var rec map[string]interface{}
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		if k, _ := rec["kind"].(string); k != "voice" {
			continue
		}
		fireID, _ := rec["fire_id"].(string)
		if fireID == "" {
			continue
		}
		src, _ := rec["source"].(string)
		voice, _ := rec["voice"].(string)
		if voice == "" {
			continue
		}
		sc.soulVoices[fireID] = append(sc.soulVoices[fireID], soulVoiceRecord{
			Source: src,
			Voice:  voice,
		})
	}
	sc.soulFlowOff += int64(len(consumed))
}

// maybeInflateSoulFlow rewrites a soul_flow entry's body to include the
// actual voice text if the entry currently shows the fallback summary
// and the voice index has data for its fire_id. No-op for non-soul_flow
// entries or entries that already render with voices inline.
func (sc *SessionCache) maybeInflateSoulFlow(e *SessionEntry) {
	if e.Type != "soul_flow" {
		return
	}
	if e.FireID == "" {
		return
	}
	voices, ok := sc.soulVoices[e.FireID]
	if !ok || len(voices) == 0 {
		return
	}
	// Only overwrite if body is the fallback shape — preserve any body
	// already produced from inline voices in events.jsonl.
	if !strings.HasPrefix(e.Body, "(soul flow fired") {
		return
	}
	var lines []string
	for _, v := range voices {
		label := v.Source
		switch {
		case v.Source == "insights":
			label = "insights"
		case strings.HasPrefix(v.Source, "snapshot:"):
			label = "past self"
		}
		lines = append(lines, fmt.Sprintf("[%s] %s", label, v.Voice))
	}
	if len(lines) > 0 {
		e.Body = strings.Join(lines, "\n")
	}
}

// tailJSONL reads a JSONL file from the given byte offset, calls parseFn on each
// complete line (terminated by \n), and returns new SessionEntry values plus the
// updated offset. Lines without a trailing \n (partial writes at EOF) are NOT
// consumed — they will be retried on the next poll.
func (sc *SessionCache) tailJSONL(path string, offset int64, parseFn func([]byte) *SessionEntry) ([]SessionEntry, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset
	}
	defer f.Close()

	// Check if file was truncated (e.g. agent molt reset the log).
	info, err := f.Stat()
	if err != nil {
		return nil, offset
	}
	if info.Size() < offset {
		offset = 0 // file was truncated, restart from beginning
	}
	if info.Size() == offset {
		return nil, offset // nothing new
	}

	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset
	}

	// Read all new bytes from offset to current EOF.
	data, err := io.ReadAll(f)
	if err != nil {
		return nil, offset
	}

	var entries []SessionEntry
	consumed := int64(0)

	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			// No newline — partial line at EOF, do not consume.
			break
		}
		line := data[:idx]
		data = data[idx+1:]
		consumed += int64(idx) + 1

		// Strip \r for \r\n endings.
		line = bytes.TrimRight(line, "\r")
		if len(line) == 0 {
			continue
		}

		if e := parseFn(line); e != nil {
			entries = append(entries, *e)
		}
	}

	return entries, offset + consumed
}

func parseEvent(line []byte) *SessionEntry {
	var raw map[string]interface{}
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil
	}
	eventType, _ := raw["type"].(string)

	// Promote consultation_fire from a raw event to a first-class
	// "soul_flow" entry — the TUI gates this at verboseThinking (level 1)
	// so users see the agent's autonomous reflection at the same Ctrl+O
	// depth as diary/thinking, without needing the extended verbose level
	// that exposes every tool call.
	if eventType == "consultation_fire" {
		eventType = "soul_flow"
	}

	switch eventType {
	case "thinking", "diary", "text_input", "text_output", "tool_call", "tool_result", "insight", "soul_flow":
		// ok
	default:
		return nil
	}

	text := extractSessionEventText(raw, eventType)
	if text == "" {
		return nil
	}

	ts := ""
	if tsFloat, ok := raw["ts"].(float64); ok {
		ts = time.Unix(int64(tsFloat), 0).UTC().Format(time.RFC3339)
	}

	e := &SessionEntry{
		Ts:   ts,
		Type: eventType,
		Body: text,
	}

	if eventType == "insight" {
		if q, ok := raw["question"].(string); ok {
			e.Question = q
		}
	}
	if eventType == "soul_flow" {
		// Carry fire_id so the post-ingest inflater can look up voices
		// in soul_flow.jsonl when events.jsonl lacks the inline payload.
		if fid, ok := raw["fire_id"].(string); ok {
			e.FireID = fid
		}
	}

	return e
}

func extractSessionEventText(entry map[string]interface{}, eventType string) string {
	switch eventType {
	case "thinking", "diary", "text_output", "text_input", "insight":
		text, _ := entry["text"].(string)
		return text
	case "soul_flow":
		// Render each voice with a short attribution. A "snapshot:..." source
		// is rendered as "past self"; "insights" stays as-is. Empty voice
		// strings are dropped (the kernel side already filters them).
		voices, _ := entry["voices"].([]interface{})
		var lines []string
		for _, v := range voices {
			vm, ok := v.(map[string]interface{})
			if !ok {
				continue
			}
			src, _ := vm["source"].(string)
			text, _ := vm["voice"].(string)
			if text == "" {
				continue
			}
			label := src
			switch {
			case src == "insights":
				label = "insights"
			case len(src) > len("snapshot:") && src[:len("snapshot:")] == "snapshot:":
				label = "past self"
			}
			lines = append(lines, fmt.Sprintf("[%s] %s", label, text))
		}
		if len(lines) == 0 {
			// Fall back to a one-line summary if voices payload is missing
			// (older event records, persistence error, or empty fire).
			count, _ := entry["count"].(float64)
			return fmt.Sprintf("(soul flow fired — %d voice(s))", int(count))
		}
		return strings.Join(lines, "\n")
	case "tool_call":
		name, _ := entry["tool_name"].(string)
		args, _ := entry["tool_args"].(string)
		if args == "" {
			if argsMap, ok := entry["tool_args"].(map[string]interface{}); ok {
				data, _ := json.Marshal(argsMap)
				args = string(data)
			}
		}
		if len(args) > 200 {
			args = args[:200] + "..."
		}
		return fmt.Sprintf("%s(%s)", name, args)
	case "tool_result":
		name, _ := entry["tool_name"].(string)
		status, _ := entry["status"].(string)
		elapsed := ""
		if ms, ok := entry["elapsed_ms"].(float64); ok {
			elapsed = fmt.Sprintf(" %dms", int(ms))
		}
		return fmt.Sprintf("%s → %s%s", name, status, elapsed)
	}
	return ""
}

// ---------------------------------------------------------------------------
// Inquiry ingestion
// ---------------------------------------------------------------------------

// IngestInquiries tails the orchestrator's soul_inquiry.jsonl from the last-read
// offset. Only human and insight-sourced inquiries are ingested.
func (sc *SessionCache) IngestInquiries(orchDir string) {
	if orchDir == "" {
		return
	}
	inquiryPath := filepath.Join(orchDir, "logs", "soul_inquiry.jsonl")
	newEntries, newOff := sc.tailJSONL(inquiryPath, sc.inquiryOff, parseInquiry)
	sc.inquiryOff = newOff
	sc.append(newEntries...)
}

func parseInquiry(line []byte) *SessionEntry {
	var raw map[string]interface{}
	if err := json.Unmarshal(line, &raw); err != nil {
		return nil
	}
	source, _ := raw["source"].(string)
	if source != "human" && source != "insight" {
		return nil
	}
	voice, _ := raw["voice"].(string)
	if voice == "" {
		return nil
	}
	ts, _ := raw["ts"].(string)

	e := &SessionEntry{
		Ts:     ts,
		Type:   "insight",
		Body:   voice,
		Source: source,
	}
	if source == "human" {
		e.Question, _ = raw["prompt"].(string)
	}
	return e
}

// ---------------------------------------------------------------------------
// Refresh + offset helpers
// ---------------------------------------------------------------------------

// Refresh polls all three data sources and appends new entries to the session log.
func (sc *SessionCache) Refresh(cache MailCache, humanAddr, orchDir, orchName string) {
	sc.IngestMail(cache, humanAddr, orchDir, orchName)
	sc.IngestEvents(orchDir)
	sc.IngestInquiries(orchDir)
}

// tsToUnix converts a session timestamp string to Unix seconds (float64).
// Handles both RFC3339Nano ("...T07:08:26.1279Z") and RFC3339 ("...T07:08:26Z").
func tsToUnix(s string) float64 {
	t := ParseSessionTs(s)
	if t.IsZero() {
		return 0
	}
	return float64(t.UnixNano()) / 1e9
}

// ParseSessionTs parses a session entry timestamp, trying RFC3339Nano first
// (handles fractional seconds from mail) then RFC3339 (whole seconds from events).
func ParseSessionTs(s string) time.Time {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Time{}
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
