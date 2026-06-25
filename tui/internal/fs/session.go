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
	"sync"
	"time"

	"github.com/anthropics/lingtai-tui/internal/sqlitelog"
)

// SessionEntry is the JSON-serializable entry stored in session.jsonl.
type SessionEntry struct {
	Ts          string            `json:"ts"`
	Type        string            `json:"type"`
	From        string            `json:"from,omitempty"`
	To          string            `json:"to,omitempty"`
	Subject     string            `json:"subject,omitempty"`
	Body        string            `json:"body"`
	Question    string            `json:"question,omitempty"`
	Attachments []string          `json:"attachments,omitempty"`
	Source      string            `json:"source,omitempty"`      // "human", "insight" — for inquiry entries
	FireID      string            `json:"fire_id,omitempty"`     // soul_flow fires — used to look up voices in soul_flow.jsonl
	Sources     []string          `json:"sources,omitempty"`     // notification entries — list of source keys (email, soul, system, ...)
	Meta        *NotificationMeta `json:"meta,omitempty"`        // notification entries — vital signs at injection time (kernel build_meta + injection_seq)
	ApiCallID   string            `json:"api_call_id,omitempty"` // llm/tool entries — one LLM API round-trip grouping id

	// Delivered is a transient field propagated from MailMessage.Delivered.
	// Only meaningful for Type == "mail". Not persisted to session.jsonl.
	Delivered bool `json:"-"`
}

// NotificationMeta carries the kernel's per-injection vital signs.
// Shape mirrors lingtai_kernel.meta_block.build_meta plus the monotonic
// injection_seq stamped in BaseAgent._inject_notification_pair. All fields
// are optional — the kernel emits sentinel values (-1, "") when the
// underlying state hasn't been computed yet, and older events.jsonl rows
// pre-dating issue #40 carry no meta at all.
type NotificationMeta struct {
	CurrentTime        string                   `json:"current_time,omitempty"`
	Context            *NotificationMetaContext `json:"context,omitempty"`
	StaminaLeftSeconds float64                  `json:"stamina_left_seconds,omitempty"`
	InjectionSeq       int                      `json:"injection_seq,omitempty"`
}

type NotificationMetaContext struct {
	SystemTokens  int     `json:"system_tokens,omitempty"`
	HistoryTokens int     `json:"history_tokens,omitempty"`
	Usage         float64 `json:"usage,omitempty"`
}

// SessionCache is an append-only cache backed by session.jsonl.
// It incrementally tails three data sources and appends new entries.
//
// Concurrency: the launch path defers the authoritative RebuildFromSources
// into a Bubble Tea Cmd goroutine while the periodic mail tick keeps calling
// Refresh on the main goroutine (see internal/tui/mail.go). Both mutate the
// same *SessionCache, so every public entry point that touches mutable state
// (RebuildFromSources, Refresh, IngestMail, Entries, Len) holds mu. The
// unexported helpers (ingestMail, IngestEvents, IngestInquiries, append,
// rewriteFile) assume the caller already holds mu and never lock themselves —
// that keeps the lock non-reentrant and avoids deadlock.
type SessionCache struct {
	mu          sync.Mutex     // guards all fields below; see type doc for the locking discipline
	path        string         // human/logs/session.jsonl
	entries     []SessionEntry // in-memory mirror of all entries
	lastMailTs  string         // highest mail ReceivedAt ingested (watermark for live-session dedup)
	eventsOff   int64          // byte offset in events.jsonl
	inquiryOff  int64          // byte offset in soul_inquiry.jsonl
	soulFlowOff int64          // byte offset in soul_flow.jsonl (voice index source)
	projectPath string         // absolute path of the project directory (parent of .lingtai/)
	lastHour    time.Time      // hour (truncated) of the most recent entry
	rebuilding  bool           // true during RebuildFromSources — suppress file writes

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

	sc := &SessionCache{
		path:        path,
		projectPath: projectPath,
		soulVoices:  make(map[string][]soulVoiceRecord),
	}
	return sc
}

// RebuildFromSources reads all three data sources from scratch, merges and
// sorts them chronologically, writes session.jsonl, and sets offsets to EOF
// so subsequent Refresh calls only append new entries.
func (sc *SessionCache) RebuildFromSources(cache MailCache, humanAddr, orchDir, orchName string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Clear any prior state and suppress file writes during ingest
	// (we'll write the sorted result in one shot at the end).
	sc.entries = nil
	sc.eventsOff = 0
	sc.inquiryOff = 0
	sc.soulFlowOff = 0
	sc.soulVoices = make(map[string][]soulVoiceRecord)
	sc.rebuilding = true

	// Ingest everything from offset 0. Uses the unlocked helpers — we already
	// hold sc.mu and the helpers must not re-lock (non-reentrant mutex).
	sc.ingestMail(cache, humanAddr, orchDir, orchName)
	if !sc.ingestEventsFromSQLite(orchDir) {
		sc.IngestEvents(orchDir)
	}
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

	// During RebuildFromSources, skip file writes —
	// we'll write the sorted result in one shot at the end.
	if sc.rebuilding {
		return
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

// Entries returns a snapshot copy of all entries in the cache. The copy is
// deliberate: callers (e.g. buildMessages) iterate the result after this
// returns, possibly while a concurrent RebuildFromSources mutates the backing
// slice. Returning the live slice would race; the copy is shallow (entry
// strings share storage) so it is cheap relative to the rebuild it guards.
func (sc *SessionCache) Entries() []SessionEntry {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	out := make([]SessionEntry, len(sc.entries))
	copy(out, sc.entries)
	return out
}

// Len returns the total number of entries.
func (sc *SessionCache) Len() int {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return len(sc.entries)
}

// ---------------------------------------------------------------------------
// Mail ingestion
// ---------------------------------------------------------------------------

// IngestMail appends new mail messages to the session log.
// humanAddr is the human's mail address (to determine IsFromMe).
// orchName is the orchestrator's display name.
//
// Public entry point: acquires the cache lock. RebuildFromSources/Refresh,
// which already hold the lock, call the unlocked ingestMail directly.
func (sc *SessionCache) IngestMail(cache MailCache, humanAddr, orchDir, orchName string) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.ingestMail(cache, humanAddr, orchDir, orchName)
}

// ingestMail is the unlocked body of IngestMail. The caller must hold sc.mu.
func (sc *SessionCache) ingestMail(cache MailCache, humanAddr, orchDir, orchName string) {
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
// converting new entries to SessionEntry. The parser keeps every
// session-recognized event type; verbose filtering happens at render time.
//
// Also refreshes the soul_flow voice index BEFORE parsing events so
// fresh consultation_fire entries can be inflated with voice text on
// the same poll. Inflates the bodies of any soul_flow entries (new or
// already-cached) that came back as the fallback summary.
//
// Lock: the caller must hold sc.mu. This is only reached from the locked
// RebuildFromSources/Refresh paths; it does not lock itself (non-reentrant).
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

func (sc *SessionCache) ingestEventsFromSQLite(orchDir string) bool {
	if orchDir == "" {
		return false
	}
	coverage, err := sqlitelog.QueryEventsIndexCoverage(orchDir)
	if err != nil || !coverage.HasRows() || !coverage.TailNearEOF() {
		return false
	}
	sc.ingestSoulFlowVoices(orchDir)
	newEntries := make([]SessionEntry, 0)
	if !coverage.StartsAtBeginning() {
		eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
		prefixEntries, _ := sc.tailJSONLRange(eventsPath, 0, coverage.MinOffset, parseEvent)
		newEntries = append(newEntries, prefixEntries...)
	}
	seenRows := 0
	err = sqlitelog.StreamSessionEvents(orchDir, func(row sqlitelog.SessionEventRow) error {
		seenRows++
		if entry := parseSQLiteEvent(row); entry != nil {
			newEntries = append(newEntries, *entry)
		}
		return nil
	})
	if err != nil {
		return false
	}
	if seenRows == 0 && fileSize(filepath.Join(orchDir, "logs", "events.jsonl")) > 0 {
		return false
	}
	for i := range newEntries {
		sc.maybeInflateSoulFlow(&newEntries[i])
	}
	for i := range sc.entries {
		sc.maybeInflateSoulFlow(&sc.entries[i])
	}
	sc.append(newEntries...)
	return true
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
func (sc *SessionCache) tailJSONLRange(path string, offset, end int64, parseFn func([]byte) *SessionEntry) ([]SessionEntry, int64) {
	f, err := os.Open(path)
	if err != nil {
		return nil, offset
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, offset
	}
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if end < offset {
		end = offset
	}
	if end > info.Size() {
		end = info.Size()
	}
	if end == offset {
		return nil, offset
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return nil, offset
	}
	data, err := io.ReadAll(io.LimitReader(f, end-offset))
	if err != nil {
		return nil, offset
	}
	var entries []SessionEntry
	consumed := int64(0)
	for len(data) > 0 {
		idx := bytes.IndexByte(data, '\n')
		if idx < 0 {
			break
		}
		line := data[:idx]
		data = data[idx+1:]
		consumed += int64(idx) + 1
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
	return parseEventMap(raw)
}

func parseSQLiteEvent(row sqlitelog.SessionEventRow) *SessionEntry {
	var raw map[string]interface{}
	if row.FieldsJSON != "" {
		if err := json.Unmarshal([]byte(row.FieldsJSON), &raw); err != nil {
			return nil
		}
	}
	if raw == nil {
		raw = make(map[string]interface{})
	}
	raw["type"] = row.Type
	raw["ts"] = row.TS
	return parseEventMap(raw)
}

func parseEventMap(raw map[string]interface{}) *SessionEntry {
	eventType, _ := raw["type"].(string)

	// Promote consultation_fire from a raw event to a first-class
	// "soul_flow" entry — the TUI gates this at verboseThinking (level 1)
	// so users see the agent's autonomous reflection at the same Ctrl+O
	// depth as diary/thinking, without needing the extended verbose level
	// that exposes every tool call.
	if eventType == "consultation_fire" {
		eventType = "soul_flow"
	}
	// Promote notification_pair_injected (kernel notification-sync wire
	// rewire) into a first-class "notification" SessionEntry so the
	// mail view can render it at the same Ctrl+O depth as soul_flow.
	if eventType == "notification_pair_injected" {
		eventType = "notification"
	}
	// Promote the three AED (agent error-recovery) events into a single
	// "aed" SessionEntry type. Subtype ("attempt" | "exhausted" | "timeout")
	// is captured below in the Source field so the renderer can vary
	// wording without juggling three nearly-identical render cases.
	aedSubtype := ""
	switch eventType {
	case "aed_attempt":
		aedSubtype = "attempt"
		eventType = "aed"
	case "aed_exhausted":
		aedSubtype = "exhausted"
		eventType = "aed"
	case "aed_timeout":
		aedSubtype = "timeout"
		eventType = "aed"
	}

	switch eventType {
	case "thinking", "diary", "text_input", "text_output", "tool_call", "tool_result", "llm_call", "llm_response", "insight", "soul_flow", "notification", "aed":
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
	if apiCallID, ok := raw["api_call_id"].(string); ok {
		e.ApiCallID = apiCallID
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
	if eventType == "notification" {
		// Carry the per-source list so the renderer can emit one
		// separated section per source even when the kernel summary
		// string is missing (older events) or not parseable.
		if rawSources, ok := raw["sources"].([]interface{}); ok {
			for _, s := range rawSources {
				if str, ok := s.(string); ok && str != "" {
					e.Sources = append(e.Sources, str)
				}
			}
		}
		// Issue #40: surface the kernel's build_meta vital signs so the
		// renderer can show context %, stamina remaining, current time,
		// and injection_seq alongside the source list. Older events
		// pre-dating the kernel emitter change carry no meta key — the
		// nil pointer signals "render without footer."
		if rawMeta, ok := raw["meta"].(map[string]interface{}); ok {
			meta := &NotificationMeta{}
			if ct, ok := rawMeta["current_time"].(string); ok {
				meta.CurrentTime = ct
			}
			if sls, ok := rawMeta["stamina_left_seconds"].(float64); ok {
				meta.StaminaLeftSeconds = sls
			}
			if seq, ok := rawMeta["injection_seq"].(float64); ok {
				meta.InjectionSeq = int(seq)
			}
			if rawCtx, ok := rawMeta["context"].(map[string]interface{}); ok {
				ctx := &NotificationMetaContext{}
				if st, ok := rawCtx["system_tokens"].(float64); ok {
					ctx.SystemTokens = int(st)
				}
				if ht, ok := rawCtx["history_tokens"].(float64); ok {
					ctx.HistoryTokens = int(ht)
				}
				if u, ok := rawCtx["usage"].(float64); ok {
					ctx.Usage = u
				}
				meta.Context = ctx
			}
			e.Meta = meta
		}
	}
	if eventType == "aed" {
		e.Source = aedSubtype
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
	case "llm_call":
		if model, ok := entry["model"].(string); ok && model != "" {
			return "llm call " + model
		}
		return "llm call"
	case "llm_response":
		return "llm response"
	case "notification":
		// Prefer the kernel-logged summary string when present (it
		// already carries per-source counts in human-readable form).
		// Older events lack `summary` — fall back to a sources list.
		if summary, ok := entry["summary"].(string); ok && summary != "" {
			return summary
		}
		rawSources, _ := entry["sources"].([]interface{})
		var srcs []string
		for _, s := range rawSources {
			if str, ok := s.(string); ok && str != "" {
				srcs = append(srcs, str)
			}
		}
		if len(srcs) == 0 {
			return "(notification rewire)"
		}
		return fmt.Sprintf("notifications: %s", strings.Join(srcs, ", "))
	case "aed":
		// Recover the original subtype from the untouched raw["type"].
		// Wording differs per subtype: attempts include the attempt index
		// and the LLM-side error description, exhausted reports the final
		// attempt count, timeout reports elapsed seconds.
		origType, _ := entry["type"].(string)
		switch origType {
		case "aed_attempt":
			attempt, _ := entry["attempt"].(float64)
			errMsg, _ := entry["error"].(string)
			if errMsg == "" {
				errMsg = "(no error description)"
			}
			return fmt.Sprintf("attempt %d — %s", int(attempt), errMsg)
		case "aed_exhausted":
			attempts, _ := entry["attempts"].(float64)
			errMsg, _ := entry["error"].(string)
			if errMsg == "" {
				errMsg = "(no error description)"
			}
			return fmt.Sprintf("exhausted after %d attempt(s) — %s", int(attempts), errMsg)
		case "aed_timeout":
			seconds, _ := entry["seconds"].(float64)
			return fmt.Sprintf("recovery wait timed out after %.1fs", seconds)
		}
		return "(aed event)"
	case "tool_call":
		name, _ := entry["tool_name"].(string)
		args, _ := entry["tool_args"].(string)
		if args == "" {
			if argsMap, ok := entry["tool_args"].(map[string]interface{}); ok {
				data, _ := json.Marshal(argsMap)
				args = string(data)
			}
		}
		// Carry the full args verbatim. Truncation (if any) is applied at
		// render time per the user's tool_call_truncate setting; the default
		// is no truncation, so this path keeps full content.
		return fmt.Sprintf("%s(%s)", name, args)
	case "tool_result":
		return formatToolResultEvent(entry)
	}
	return ""
}

func formatToolResultEvent(entry map[string]interface{}) string {
	name, _ := entry["tool_name"].(string)
	status, _ := entry["status"].(string)
	elapsed := ""
	if ms, ok := entry["elapsed_ms"].(float64); ok {
		elapsed = fmt.Sprintf(" %dms", int(ms))
	}

	lines := []string{fmt.Sprintf("%s → %s%s", name, status, elapsed)}
	result, hasResult := entry["result"]
	resultMap, _ := result.(map[string]interface{})

	if resultMap != nil {
		if toolErr, ok := resultMap["tool_error"].(map[string]interface{}); ok {
			lines = append(lines, formatToolErrorSummary(toolErr)...)
		}
	}
	lines = appendToolResultMetaBlocks(lines, entry, resultMap)

	if !hasResult {
		return strings.Join(lines, "\n")
	}
	result = displayToolResultValue(result)
	if result != nil {
		if resultText := formatToolResultValue(result); resultText != "" {
			// Keep the parsed session entry complete. Mail-view verbosity and the
			// user's tool_truncate preference apply render-time truncation.
			lines = append(lines, "result: "+resultText)
		}
	}
	return strings.Join(lines, "\n")
}

func appendToolResultMetaBlocks(lines []string, entry map[string]interface{}, resultMap map[string]interface{}) []string {
	toolBlock := map[string]interface{}{}
	copyIfPresent(toolBlock, "id", entry, "tool_call_id")
	copyIfPresent(toolBlock, "trace_id", entry, "tool_trace_id")
	copyIfPresent(toolBlock, "name", entry, "tool_name")
	copyIfPresent(toolBlock, "timestamp", entry, "ts")
	copyIfPresent(toolBlock, "status", entry, "status")
	copyIfPresent(toolBlock, "elapsed_ms", entry, "elapsed_ms")
	if len(toolBlock) > 0 {
		lines = appendMetadataBlock(lines, "_tool", toolBlock)
	}

	runtimeStateRendered := false
	runtimeGuidanceRendered := false
	if runtimeMap := firstMapValue(entry, resultMap, "_runtime"); runtimeMap != nil {
		if state, ok := runtimeMap["state"]; ok {
			lines = appendMetadataBlock(lines, "_runtime.state", state)
			runtimeStateRendered = true
		}
		if guidance, ok := runtimeMap["guidance"]; ok {
			lines = appendMetadataBlock(lines, "_runtime.guidance", guidance)
			runtimeGuidanceRendered = true
		}
	}
	if resultMap != nil && !runtimeStateRendered {
		if pending, ok := resultMap["_runtime_pending"]; ok {
			lines = appendMetadataBlock(lines, "_runtime.state", pending)
		}
	}
	if guidance := firstValue(entry, resultMap, "_runtime_guidance"); guidance != nil && !runtimeGuidanceRendered {
		lines = appendMetadataBlock(lines, "_runtime.guidance", guidance)
	}
	if notifications := firstValue(entry, resultMap, "notifications"); notifications != nil {
		lines = appendMetadataBlock(lines, "notifications", notifications)
	}
	if guidance := firstValue(entry, resultMap, "_notification_guidance"); guidance != nil {
		lines = appendMetadataBlock(lines, "_notification_guidance", guidance)
	}
	return lines
}

func displayToolResultValue(result interface{}) interface{} {
	resultMap, ok := result.(map[string]interface{})
	if !ok {
		return result
	}
	cleaned := make(map[string]interface{}, len(resultMap))
	for k, v := range resultMap {
		switch k {
		case "_runtime_pending", "_runtime", "_runtime_guidance", "notifications", "_notification_guidance", "_tool":
			continue
		default:
			cleaned[k] = v
		}
	}
	if len(cleaned) == 0 {
		return nil
	}
	return cleaned
}

func copyIfPresent(dst map[string]interface{}, dstKey string, src map[string]interface{}, srcKey string) {
	if value, ok := src[srcKey]; ok && value != nil {
		dst[dstKey] = value
	}
}

func firstMapValue(primary map[string]interface{}, secondary map[string]interface{}, key string) map[string]interface{} {
	if value, ok := primary[key].(map[string]interface{}); ok {
		return value
	}
	if secondary != nil {
		if value, ok := secondary[key].(map[string]interface{}); ok {
			return value
		}
	}
	return nil
}

func firstValue(primary map[string]interface{}, secondary map[string]interface{}, key string) interface{} {
	if value, ok := primary[key]; ok {
		return value
	}
	if secondary != nil {
		if value, ok := secondary[key]; ok {
			return value
		}
	}
	return nil
}

func appendMetadataBlock(lines []string, name string, value interface{}) []string {
	text := formatToolResultValue(value)
	if text == "" {
		return lines
	}
	lines = append(lines, name+":")
	for _, line := range strings.Split(text, "\n") {
		lines = append(lines, "  "+line)
	}
	return lines
}

func formatToolErrorSummary(toolErr map[string]interface{}) []string {
	var lines []string
	if reason, ok := toolErr["reason"].(string); ok && reason != "" {
		lines = append(lines, "tool_error: "+reason)
	} else if summary, ok := toolErr["summary"].(string); ok && summary != "" {
		lines = append(lines, "tool_error: "+summary)
	}
	if argKeys := stringifyToolResultList(toolErr["arg_keys"]); len(argKeys) > 0 {
		lines = append(lines, "arg_keys: "+strings.Join(argKeys, ", "))
	}
	if guidance := stringifyToolResultList(toolErr["guidance"]); len(guidance) > 0 {
		lines = append(lines, "guidance:")
		for _, item := range guidance {
			lines = append(lines, "- "+item)
		}
	}
	return lines
}

func stringifyToolResultList(value interface{}) []string {
	items, ok := value.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		switch v := item.(type) {
		case string:
			if v != "" {
				out = append(out, v)
			}
		default:
			out = append(out, fmt.Sprint(v))
		}
	}
	return out
}

func formatToolResultValue(value interface{}) string {
	switch v := value.(type) {
	case nil:
		return "null"
	case string:
		return v
	default:
		data, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}

// ---------------------------------------------------------------------------
// Inquiry ingestion
// ---------------------------------------------------------------------------

// IngestInquiries tails the orchestrator's soul_inquiry.jsonl from the last-read
// offset. Only human and insight-sourced inquiries are ingested.
//
// Lock: the caller must hold sc.mu (reached only from RebuildFromSources/Refresh).
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
	sc.mu.Lock()
	defer sc.mu.Unlock()
	// Unlocked helpers — we already hold sc.mu (non-reentrant).
	sc.ingestMail(cache, humanAddr, orchDir, orchName)
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
