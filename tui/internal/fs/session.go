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

	"github.com/anthropics/lingtai-tui/i18n"
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
	TokenUsage  *TokenUsage       `json:"token_usage,omitempty"` // llm_response entries — per-round token scalars (input/output/cached)
	Summary     *AprioriSummary   `json:"summary,omitempty"`     // apriori_summary entries — the model-visible summary=true result that replaced the raw payload

	// Delivered is a transient field propagated from MailMessage.Delivered.
	// Only meaningful for Type == "mail". Not persisted to session.jsonl.
	Delivered bool `json:"-"`
}

// NotificationMeta carries the kernel's per-injection vital signs.
// Shape mirrors lingtai.kernel.meta_block.build_meta plus the monotonic
// injection_seq stamped in BaseAgent._inject_notification_pair. All fields
// are optional — the kernel emits sentinel values (-1, "") when the
// underlying state hasn't been computed yet, and older events.jsonl rows
// pre-dating issue #40 carry no meta at all.
type NotificationMeta struct {
	CurrentTime  string                   `json:"current_time,omitempty"`
	Context      *NotificationMetaContext `json:"context,omitempty"`
	InjectionSeq int                      `json:"injection_seq,omitempty"`
}

type NotificationMetaContext struct {
	SystemTokens  int     `json:"system_tokens,omitempty"`
	HistoryTokens int     `json:"history_tokens,omitempty"`
	Usage         float64 `json:"usage,omitempty"`
}

// TokenUsage carries the per-round token scalars logged on an llm_response
// event: input_tokens, output_tokens, cached_tokens (plus the estimated flag
// the kernel sets when a provider returned no usage and the count was derived).
// Input is the true total input (raw + cache_read + cache_write, normalised per
// adapter), so the cache-miss complement is Input-Cached and the cache rate is
// Cached/Input — same semantics as the token_ledger.jsonl row for the same
// api_call_id (see tui/internal/fs/agent.go LedgerEntry). The TUI renders these
// four derived numbers as a compact footer at the bottom of the ctrl+o API call
// group; the noisy `_meta` envelope hidden by PR #440 is never read here.
type TokenUsage struct {
	Input     int64 `json:"input"`
	Output    int64 `json:"output"`
	Cached    int64 `json:"cached"`
	Estimated bool  `json:"estimated,omitempty"`
}

// AprioriSummary carries the kernel's `summary=true` (a-priori) tool-result
// summary — the model-VISIBLE artifact that replaced the raw tool payload
// before it entered the agent's context (kernel PR #586,
// `lingtai_apriori_tool_result_summary`). The raw result is still logged
// verbatim in the preceding `tool_result` event and is the canonical record;
// this struct is the compressed thing the agent actually saw, so the TUI can
// render it as a distinct labelled block right after the raw result.
//
// The kernel surfaces this in two shapes the TUI reads:
//   - the success/cap/error artifact dict nested in a `tool_result` event's
//     `result` map (detected by `artifact == lingtai_apriori_tool_result_summary`); or
//   - an `apriori_summary_generated` / `apriori_summary_cap_refused` /
//     `apriori_summary_failed` / `apriori_summary_empty` /
//     `apriori_summary_no_summarizer` lifecycle event keyed by `tool_call_id`.
//     The generated success event carries the summary text inline via
//     `generated_summary` (kernel #3833); older logs may lack it.
//
// Kind is the `summary_kind` ("apriori_generated", "apriori_cap_refused",
// "apriori_error") or the lifecycle event name for the text-less path. Text is
// the `generated_summary` (success) or the human-readable `message`/`error`
// (cap/error). All count fields are 0 when the source did not carry them.
type AprioriSummary struct {
	Kind                 string `json:"kind,omitempty"`                   // summary_kind or lifecycle event name
	ToolCallID           string `json:"tool_call_id,omitempty"`           // correlates with the preceding raw tool_result
	ToolName             string `json:"tool_name,omitempty"`              // tool whose result was summarized
	Text                 string `json:"text,omitempty"`                   // generated_summary, or refusal/error message
	OriginalVisibleChars int    `json:"original_visible_chars,omitempty"` // size of the raw payload that was replaced
	SummaryChars         int    `json:"summary_chars,omitempty"`          // size of the generated summary (0 on cap/error)
	Unavailable          bool   `json:"unavailable,omitempty"`            // cap-refusal or fail-closed error (no summary text)
}

// SessionCache is an append-only cache backed by session.jsonl.
// It incrementally tails three data sources and appends new entries.
//
// Concurrency: every public entry point that touches mutable state
// (RebuildFromSources, RebuildFromSourcesInMemory, Persist, Refresh, IngestMail,
// Entries, Len) holds mu. The unexported helpers (ingestMail, IngestEvents,
// IngestInquiries, append, rewriteFile) assume the caller already holds mu and
// never lock themselves — that keeps the lock non-reentrant and avoids deadlock.
// The TUI's deferred initial rebuild uses a command-local SessionCache and only
// installs and persists it after accepting the command's generation.
type SessionCache struct {
	mu                  sync.Mutex     // guards all fields below; see type doc for the locking discipline
	path                string         // human/logs/session.jsonl
	entries             []SessionEntry // in-memory mirror of all entries
	lastMailTs          string         // highest mail ReceivedAt ingested (watermark for live-session dedup)
	eventsOff           int64          // byte offset in events.jsonl
	inquiryOff          int64          // byte offset in soul_inquiry.jsonl
	soulFlowOff         int64          // byte offset in soul_flow.jsonl (voice index source)
	projectPath         string         // absolute path of the project directory (parent of .lingtai/)
	lastHour            time.Time      // hour (truncated) of the most recent entry
	rebuilding          bool           // true during RebuildFromSources — suppress file writes
	afterRebuildIngest  func()         // optional deterministic test hook after authoritative reads
	afterSQLiteCoverage func()         // optional deterministic test hook after SQLite coverage capture

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

// NewSessionCache constructs an in-memory cache without touching the filesystem.
// Parent directories are created only by accepted persistence/append writes.
func NewSessionCache(humanDir string, projectPath string) *SessionCache {
	return &SessionCache{
		path:        filepath.Join(humanDir, "logs", "session.jsonl"),
		projectPath: projectPath,
		soulVoices:  make(map[string][]soulVoiceRecord),
	}
}

// RebuildFromSources reads all data sources from scratch, merges and sorts them
// chronologically, writes session.jsonl, and retains each source's last complete
// consumed-record boundary for subsequent Refresh calls.
func (sc *SessionCache) RebuildFromSources(cache MailCache, humanAddr, orchDir, orchName string) {
	sc.rebuildFromSources(cache, humanAddr, orchDir, orchName, true)
}

// RebuildFromSourcesInMemory performs the same authoritative rebuild without
// writing session.jsonl. It is used for command-local work whose generation must
// be accepted before it can affect the installed cache or its persisted mirror.
func (sc *SessionCache) RebuildFromSourcesInMemory(cache MailCache, humanAddr, orchDir, orchName string) {
	sc.rebuildFromSources(cache, humanAddr, orchDir, orchName, false)
}

func (sc *SessionCache) rebuildFromSources(cache MailCache, humanAddr, orchDir, orchName string, persist bool) {
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
	if sc.afterRebuildIngest != nil {
		sc.afterRebuildIngest()
	}

	sc.rebuilding = false

	// Sort by unix timestamp.
	sort.SliceStable(sc.entries, func(i, j int) bool {
		return tsToUnix(sc.entries[i].Ts) < tsToUnix(sc.entries[j].Ts)
	})

	// Write sorted session.jsonl in one shot only for an already-accepted cache.
	// Deferred commands build in memory and call Persist after generation checks.
	if persist {
		sc.rewriteFile()
	}

	// Each ingestion path leaves its offset at the last complete record it
	// actually consumed. Never replace those parser-proven boundaries with a later
	// raw file size: a trailing partial line or concurrent append must be retried by
	// the next Refresh.

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

// Persist writes the accepted in-memory snapshot to session.jsonl.
func (sc *SessionCache) Persist() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.rewriteFile()
}

// rewriteFile overwrites session.jsonl with the current in-memory entries.
// The caller must hold sc.mu.
func (sc *SessionCache) rewriteFile() {
	if err := os.MkdirAll(filepath.Dir(sc.path), 0o755); err != nil {
		return
	}
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

	// Append to file. Construction is pure, so the first accepted append owns
	// creation of the derived cache's parent directory.
	if err := os.MkdirAll(filepath.Dir(sc.path), 0o755); err != nil {
		return
	}
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
	// Resume immediately after the highest-offset record proven present in this
	// SQLite snapshot. Coverage and streaming use the same canonical source and
	// captured MaxOffset horizon; later index rows belong to the next JSONL Refresh.
	eventsPath := coverage.SourceFile
	eventsBoundary := indexedJSONLBoundary(eventsPath, coverage.MaxOffset, coverage.FileSize)
	if eventsBoundary <= 0 {
		return false
	}
	if sc.afterSQLiteCoverage != nil {
		sc.afterSQLiteCoverage()
	}
	sc.ingestSoulFlowVoices(orchDir)
	newEntries := make([]SessionEntry, 0)
	if !coverage.StartsAtBeginning() {
		prefixEntries, _ := sc.tailJSONLRange(eventsPath, 0, coverage.MinOffset, parseEvent)
		newEntries = append(newEntries, prefixEntries...)
	}
	seenRows := 0
	err = sqlitelog.StreamSessionEvents(orchDir, coverage, func(row sqlitelog.SessionEventRow) error {
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
	sc.eventsOff = eventsBoundary
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

// indexedJSONLBoundary returns the byte immediately after the complete JSONL
// record beginning at sourceOffset, bounded by the file-size snapshot used for
// SQLite coverage. That record is the highest one proven represented by the
// index; later bytes remain authoritative JSONL work for Refresh.
func indexedJSONLBoundary(path string, sourceOffset, snapshotEnd int64) int64 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return 0
	}
	if snapshotEnd > info.Size() {
		snapshotEnd = info.Size()
	}
	if sourceOffset < 0 || sourceOffset >= snapshotEnd {
		return 0
	}
	if _, err := f.Seek(sourceOffset, io.SeekStart); err != nil {
		return 0
	}
	const chunkSize int64 = 4096
	buf := make([]byte, chunkSize)
	pos := sourceOffset
	for pos < snapshotEnd {
		want := snapshotEnd - pos
		if want > chunkSize {
			want = chunkSize
		}
		n, readErr := f.Read(buf[:want])
		if n == 0 {
			return 0
		}
		if idx := bytes.IndexByte(buf[:n], '\n'); idx >= 0 {
			return pos + int64(idx) + 1
		}
		pos += int64(n)
		if readErr != nil {
			return 0
		}
	}
	return 0
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

	// Promote the kernel's a-priori summary (`summary=true`) lifecycle events
	// into a single first-class "apriori_summary" SessionEntry. The kernel logs
	// the raw tool_result FIRST and this lifecycle event immediately after, so
	// in stream order the summary entry already lands right after its raw result
	// — the renderer keys the visual association on tool_call_id. The generated
	// success event carries the char counts and (kernel #3833) the summary text
	// via `generated_summary`; older logs omit the text and the renderer falls
	// back to a metadata-only block in that case.
	summaryLifecycle := ""
	switch eventType {
	case "apriori_summary_generated":
		summaryLifecycle = "apriori_generated"
		eventType = "apriori_summary"
	case "apriori_summary_cap_refused":
		summaryLifecycle = "apriori_cap_refused"
		eventType = "apriori_summary"
	case "apriori_summary_failed", "apriori_summary_empty", "apriori_summary_no_summarizer":
		summaryLifecycle = "apriori_error"
		eventType = "apriori_summary"
	}

	switch eventType {
	case "thinking", "diary", "text_input", "text_output", "tool_call", "tool_result", "llm_call", "llm_response", "insight", "soul_flow", "notification", "aed", "apriori_summary":
		// ok
	default:
		return nil
	}

	// apriori_summary is the only session type whose body is allowed to be empty
	// (cap/error lifecycle events, and pre-#3833 logs, carry only counts and no
	// summary text). Build it here and return early so the shared empty-text
	// guard below does not drop it.
	if eventType == "apriori_summary" {
		return parseAprioriSummaryEvent(raw, summaryLifecycle)
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
		// renderer can show context %, current time, and injection_seq
		// alongside the source list. Older events
		// pre-dating the kernel emitter change carry no meta key — the
		// nil pointer signals "render without footer."
		if rawMeta, ok := raw["meta"].(map[string]interface{}); ok {
			meta := &NotificationMeta{}
			if ct, ok := rawMeta["current_time"].(string); ok {
				meta.CurrentTime = ct
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
	if eventType == "tool_result" {
		// Defensive: if the model-visible result IS the a-priori summary
		// artifact (a deployment where the visible/summary payload, not the
		// raw, was logged to this event), attach it so the renderer can append
		// the labelled summary section right after the raw block. The common
		// production shape logs the raw here and emits a separate
		// apriori_summary lifecycle event; both are handled.
		if s := aprioriSummaryFromArtifact(raw["result"]); s != nil {
			if s.ToolCallID == "" {
				s.ToolCallID, _ = raw["tool_call_id"].(string)
			}
			if s.ToolName == "" {
				s.ToolName, _ = raw["tool_name"].(string)
			}
			e.Summary = s
		}
	}
	if eventType == "llm_response" {
		// llm_response carries the per-round token scalars directly on the
		// event. We extract only these numbers (never the `_meta` envelope) so
		// the TUI can render a compact usage footer at the bottom of the API
		// call group. Missing/zero fields are fine — the renderer drops empty
		// fragments. Older events that predate the usage emitter carry no
		// token fields; intField returns 0 and the renderer shows nothing.
		input := intField(raw, "input_tokens")
		output := intField(raw, "output_tokens")
		cached := intField(raw, "cached_tokens")
		estimated, _ := raw["estimated"].(bool)
		if input != 0 || output != 0 || cached != 0 {
			e.TokenUsage = &TokenUsage{
				Input:     input,
				Output:    output,
				Cached:    cached,
				Estimated: estimated,
			}
		}
	}

	return e
}

// aprioriSummaryMarker is the kernel artifact tag stamped on every
// `summary=true` (a-priori) tool-result replacement/refusal/error dict
// (kernel `tool_result_summary.APRIORI_SUMMARY_MARKER`).
const aprioriSummaryMarker = "lingtai_apriori_tool_result_summary"

// aprioriSummaryFromArtifact builds an AprioriSummary from the kernel artifact
// dict (the model-visible `lingtai_apriori_tool_result_summary` payload) when
// `result` is that artifact, else returns nil. Used both for the artifact-in-
// result shape and as a defensive detector on tool_result events.
func aprioriSummaryFromArtifact(result interface{}) *AprioriSummary {
	m, ok := result.(map[string]interface{})
	if !ok {
		return nil
	}
	if marker, _ := m["artifact"].(string); marker != aprioriSummaryMarker {
		return nil
	}
	s := &AprioriSummary{}
	s.Kind, _ = m["summary_kind"].(string)
	s.ToolCallID, _ = m["tool_call_id"].(string)
	s.ToolName, _ = m["tool_name"].(string)
	s.OriginalVisibleChars = int(intField(m, "original_visible_chars"))
	s.SummaryChars = int(intField(m, "summary_chars"))
	if text, _ := m["generated_summary"].(string); text != "" {
		s.Text = text
	} else if msg, _ := m["message"].(string); msg != "" {
		// cap-refusal / fail-closed error: no generated text, surface the
		// kernel's human-readable explanation instead.
		s.Text = msg
	}
	// status == "summary_unavailable" marks the cap-refusal and error variants;
	// the generated success variant has no status field.
	if status, _ := m["status"].(string); status == "summary_unavailable" {
		s.Unavailable = true
	}
	return s
}

// parseAprioriSummaryEvent builds an apriori_summary SessionEntry from a kernel
// `apriori_summary_*` lifecycle event. These events carry the char counts and
// tool identity, and (as of kernel #3833) the generated summary text on the
// success path via `generated_summary`. Older logs predate that field, so Text
// stays empty for them and the renderer falls back to a metadata-only block.
func parseAprioriSummaryEvent(raw map[string]interface{}, kind string) *SessionEntry {
	ts := ""
	if tsFloat, ok := raw["ts"].(float64); ok {
		ts = time.Unix(int64(tsFloat), 0).UTC().Format(time.RFC3339)
	}
	s := &AprioriSummary{Kind: kind}
	s.ToolCallID, _ = raw["tool_call_id"].(string)
	s.ToolName, _ = raw["tool_name"].(string)
	s.OriginalVisibleChars = int(intField(raw, "original_visible_chars"))
	s.SummaryChars = int(intField(raw, "summary_chars"))
	// The generated success path carries the model-visible summary text inline
	// (kernel #3833). Older logs omit it; Text stays empty and the renderer
	// shows the metadata-only fallback note.
	if text, _ := raw["generated_summary"].(string); text != "" {
		s.Text = text
	}
	if kind == "apriori_cap_refused" || kind == "apriori_error" {
		s.Unavailable = true
	}
	e := &SessionEntry{Ts: ts, Type: "apriori_summary", Summary: s}
	if apiCallID, ok := raw["api_call_id"].(string); ok {
		e.ApiCallID = apiCallID
	}
	return e
}

// intField reads a numeric event field as int64. JSON numbers decode to
// float64 through encoding/json, so this tolerates both float64 and any
// integer types that may appear from other decoders.
func intField(raw map[string]interface{}, key string) int64 {
	switch v := raw[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	}
	return 0
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
	if result == nil {
		return strings.Join(lines, "\n")
	}

	// Build the result body once. The former format-to-string, prefix, and final
	// strings.Join sequence copied large tool results several times during every
	// full history rebuild. Keep the exact MarshalIndent output while writing its
	// bytes directly into the final builder.
	var resultText string
	var resultJSON []byte
	switch v := result.(type) {
	case string:
		resultText = v
	default:
		data, err := json.MarshalIndent(v, "", "  ")
		if err == nil {
			resultJSON = data
		} else {
			resultText = fmt.Sprint(v)
		}
	}
	if resultText == "" && len(resultJSON) == 0 {
		return strings.Join(lines, "\n")
	}

	prefix := strings.Join(lines, "\n")
	var body strings.Builder
	body.Grow(len(prefix) + len(resultText) + len(resultJSON) + len("\nresult: "))
	body.WriteString(prefix)
	body.WriteString("\nresult: ")
	if len(resultJSON) > 0 {
		body.Write(resultJSON)
	} else {
		body.WriteString(resultText)
	}
	return body.String()
}

// appendToolResultMetaBlocks decides whether a tool_result carries any of the
// synthesized `_meta` envelope blocks (`_tool`, `_runtime.state`,
// `_runtime.guidance`, `notifications`, `_notification_guidance`). The blocks
// themselves are too noisy for the ctrl+o chat replay, so instead of expanding
// them inline we emit a single short hint pointing the user at `/notification`,
// where the full canonical `_meta.*` envelope is paged on demand. The raw
// metadata is still carried in the underlying event and stripped out of the
// `result:` body by displayToolResultValue, so nothing is lost — only hidden.
func appendToolResultMetaBlocks(lines []string, entry map[string]interface{}, resultMap map[string]interface{}) []string {
	hasMeta := false

	toolBlock := map[string]interface{}{}
	copyIfPresent(toolBlock, "id", entry, "tool_call_id")
	copyIfPresent(toolBlock, "trace_id", entry, "tool_trace_id")
	copyIfPresent(toolBlock, "name", entry, "tool_name")
	copyIfPresent(toolBlock, "timestamp", entry, "ts")
	copyIfPresent(toolBlock, "status", entry, "status")
	copyIfPresent(toolBlock, "elapsed_ms", entry, "elapsed_ms")
	if len(toolBlock) > 0 {
		hasMeta = true
	}

	runtimeStateRendered := false
	runtimeGuidanceRendered := false
	if runtimeMap := firstMapValue(entry, resultMap, "_runtime"); runtimeMap != nil {
		if _, ok := runtimeMap["state"]; ok {
			hasMeta = true
			runtimeStateRendered = true
		}
		if _, ok := runtimeMap["guidance"]; ok {
			hasMeta = true
			runtimeGuidanceRendered = true
		}
	}
	if resultMap != nil && !runtimeStateRendered {
		if _, ok := resultMap["_runtime_pending"]; ok {
			hasMeta = true
		}
	}
	if guidance := firstValue(entry, resultMap, "_runtime_guidance"); guidance != nil && !runtimeGuidanceRendered {
		hasMeta = true
	}
	if notifications := firstValue(entry, resultMap, "notifications"); notifications != nil {
		hasMeta = true
	}
	if guidance := firstValue(entry, resultMap, "_notification_guidance"); guidance != nil {
		hasMeta = true
	}

	if hasMeta {
		lines = append(lines, i18n.T("mail.meta_hidden_hint"))
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
