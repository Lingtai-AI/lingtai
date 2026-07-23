// internal/fs/session.go — append-only session log and in-memory cache.
package fs

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
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

// SessionHistoryStats is count-only metadata for renderable canonical event
// history. Mail and inquiry entries are loaded separately and are not included.
type SessionHistoryStats struct {
	Detailed int // renderable non-insight event entries (verboseThinking/Extended)
	Insights int // renderable event insight entries (when insights are enabled)
}

type sessionHistoryCountPlan struct {
	identity string
	path     string
	upper    int64
}

// SessionPersistenceRole separates history completeness from permission to
// write the shared human/logs/session.jsonl aggregate.
type SessionPersistenceRole uint8

const (
	NoPersist SessionPersistenceRole = iota
	MainAggregateWriter
)

// SessionCache is an append-only cache backed by session.jsonl.
// It incrementally tails three data sources and appends new entries.
//
// Concurrency: every public entry point that touches mutable state
// (RebuildFromSources, RebuildFromSourcesInMemory, Persist, Refresh, IngestMail,
// Entries, Len, HistoryStats) holds mu. The unexported helpers (ingestMail, IngestEvents,
// IngestInquiries, append, rewriteFile) assume the caller already holds mu and
// never lock themselves — that keeps the lock non-reentrant and avoids deadlock.
// The TUI's deferred initial rebuild uses a command-local SessionCache and only
// installs and persists it after accepting the command's generation.
type SessionCache struct {
	mu                 sync.Mutex              // guards all fields below; see type doc for the locking discipline
	path               string                  // human/logs/session.jsonl
	entries            []SessionEntry          // in-memory mirror of loaded entries
	historyStats       SessionHistoryStats     // accepted exact count plus incremental EOF additions
	historyCountPlan   sessionHistoryCountPlan // canonical source/horizon captured by the bounded content rebuild
	lastMailTs         string                  // highest mail ReceivedAt ingested (watermark for live-session dedup)
	eventsOff          int64                   // byte offset in events.jsonl
	inquiryOff         int64                   // byte offset in soul_inquiry.jsonl
	soulFlowOff        int64                   // byte offset in soul_flow.jsonl (voice index source)
	projectPath        string                  // absolute path of the project directory (parent of .lingtai/)
	lastHour           time.Time               // hour (truncated) of the most recent entry
	rebuilding         bool                    // true during RebuildFromSources — suppress file writes
	complete           bool                    // true when the cache holds the full history; false after a windowed rebuild truncated older events
	afterRebuildIngest func()                  // optional deterministic test hook after authoritative reads
	persistenceRole    SessionPersistenceRole  // only MainAggregateWriter may change session.jsonl

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
func NewSessionCache(humanDir string, projectPath string, persistenceRole SessionPersistenceRole) *SessionCache {
	return &SessionCache{
		path:            filepath.Join(humanDir, "logs", "session.jsonl"),
		projectPath:     projectPath,
		soulVoices:      make(map[string][]soulVoiceRecord),
		persistenceRole: persistenceRole,
		// Complete-from-zero: a fresh cache holds nothing, which trivially IS the
		// full (empty) history. A MainAggregateWriter cache followed by full Refresh
		// + Persist (no windowed rebuild) may therefore still write session.jsonl,
		// matching the pre-windowing completeness contract. NoPersist remains
		// memory-only regardless. Only a windowed rebuild that PROVES it truncated
		// older events flips this false; full rebuilds keep it true.
		complete: true,
	}
}

// RebuildFromSources reads all data sources from scratch, merges and sorts them
// chronologically, requests a rewrite through the role-enforcing primitive, and
// retains each source's last complete consumed-record boundary for Refresh.
func (sc *SessionCache) RebuildFromSources(cache MailCache, humanAddr, orchDir, orchName string) {
	sc.rebuildFromSources(cache, humanAddr, orchDir, orchName, true, 0)
}

// RebuildFromSourcesInMemory performs the same authoritative rebuild without
// writing session.jsonl. It is used for command-local work whose generation must
// be accepted before it can affect the installed cache or its persisted mirror.
func (sc *SessionCache) RebuildFromSourcesInMemory(cache MailCache, humanAddr, orchDir, orchName string) {
	sc.rebuildFromSources(cache, humanAddr, orchDir, orchName, false, 0)
}

// RebuildFromSourcesWindowedInMemory is the bounded first-content variant: it
// ingests only the newest `window` session events by reading authoritative JSONL
// backward from EOF, while still loading all mail and inquiries,
// then merges and sorts chronologically.
// It never writes session.jsonl. When the window truncates older events the cache
// is left partial (Complete() == false), so the caller must NOT persist it as if
// complete. A window <= 0 is a full, complete rebuild.
//
// Correctness: even when the event stream is windowed, eventsOff is advanced to
// the true EOF complete-record boundary, so a later Refresh resumes from EOF and
// never re-ingests the excluded older window. The rebuild captures a canonical
// source/horizon count plan but does not execute it; ExactHistoryStats performs
// that metadata-only work asynchronously without caching excluded bodies.
func (sc *SessionCache) RebuildFromSourcesWindowedInMemory(cache MailCache, humanAddr, orchDir, orchName string, window int) {
	sc.rebuildFromSources(cache, humanAddr, orchDir, orchName, false, window)
}

// Complete reports whether the cache currently holds the full history. A windowed
// rebuild that truncated older events leaves this false; a full rebuild, or a
// windowed rebuild whose window covered the entire history, leaves it true.
func (sc *SessionCache) Complete() bool {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.complete
}

func (sc *SessionCache) rebuildFromSources(cache MailCache, humanAddr, orchDir, orchName string, persist bool, window int) {
	sc.mu.Lock()
	defer sc.mu.Unlock()

	// Clear any prior state and suppress file writes during ingest
	// (we'll write the sorted result in one shot at the end).
	sc.entries = nil
	sc.historyStats = SessionHistoryStats{}
	sc.historyCountPlan = sessionHistoryCountPlan{}
	sc.eventsOff = 0
	sc.inquiryOff = 0
	sc.soulFlowOff = 0
	sc.soulVoices = make(map[string][]soulVoiceRecord)
	sc.rebuilding = true
	// Assume complete until a positive-window canonical JSONL scan proves that
	// older session events remain outside the requested content window.
	sc.complete = true

	// Ingest everything (or, for a windowed rebuild, the newest `window` events)
	// from offset 0. Uses the unlocked helpers — we already hold sc.mu and the
	// helpers must not re-lock (non-reentrant mutex).
	sc.ingestMail(cache, humanAddr, orchDir, orchName)
	// events.jsonl is authoritative. The SQLite log is an additive index and its
	// endpoint range cannot prove that interior source offsets are continuous.
	// Windowed reads therefore scan canonical JSONL backward, while full rebuilds
	// consume it forward. Both paths remain body-bounded for the requested window
	// and can prove completeness without trusting a sparse index.
	if !(window > 0 && sc.ingestEventsFromJSONLWindowed(orchDir, window)) {
		sc.IngestEvents(orchDir)
	}
	if sc.historyCountPlan.identity == "" {
		eventsPath, _ := filepath.Abs(filepath.Join(orchDir, "logs", "events.jsonl"))
		eventsPath = filepath.Clean(eventsPath)
		sc.historyCountPlan = sessionHistoryCountPlan{
			identity: fmt.Sprintf("jsonl:%s:%d", eventsPath, sc.eventsOff),
			path:     eventsPath,
			upper:    sc.eventsOff,
		}
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

// Persist requests that the accepted in-memory snapshot replace session.jsonl.
// Completeness is a truncation-safety guard, not authorization: rewriteFile
// separately permits only MainAggregateWriter. A partial cache is a no-op so it
// cannot replace the operator's complete derived replay file.
func (sc *SessionCache) Persist() {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	if !sc.complete {
		return
	}
	sc.rewriteFile()
}

// rewriteFile overwrites session.jsonl with the current in-memory entries.
// The caller must hold sc.mu.
func (sc *SessionCache) rewriteFile() {
	if sc.persistenceRole != MainAggregateWriter {
		return
	}
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

	// Rebuilds write a proven-complete sorted snapshot in one shot. A bounded
	// cache is intentionally incomplete, so its incremental EOF additions must
	// remain memory-only rather than create or extend a misleading session.jsonl.
	if sc.rebuilding || !sc.complete {
		return
	}
	if sc.persistenceRole != MainAggregateWriter {
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

// HistoryStats returns count-only metadata for the full canonical event history,
// including entries whose bodies are outside a partial content window.
func (sc *SessionCache) HistoryStats() SessionHistoryStats {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.historyStats
}

// HistoryCountIdentity identifies the canonical event source and captured horizon
// whose exact metadata count may be computed asynchronously.
func (sc *SessionCache) HistoryCountIdentity() string {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	return sc.historyCountPlan.identity
}

// ExactHistoryStats performs the full-history metadata work for the source/horizon
// captured by the bounded content rebuild. Callers run this asynchronously and
// identity-gate the result; content bodies outside the cache are never retained.
func (sc *SessionCache) ExactHistoryStats() (SessionHistoryStats, string, error) {
	sc.mu.Lock()
	plan := sc.historyCountPlan
	sc.mu.Unlock()
	if plan.identity == "" {
		return SessionHistoryStats{}, "", fmt.Errorf("session history count plan unavailable")
	}
	stats, err := countSessionEventMetadataRange(plan.path, plan.upper)
	return stats, plan.identity, err
}

// SetHistoryStats installs an already identity-gated exact count on a rebuilt
// content cache so later EOF Refresh calls can increment it without recounting.
func (sc *SessionCache) SetHistoryStats(stats SessionHistoryStats) {
	sc.mu.Lock()
	defer sc.mu.Unlock()
	sc.historyStats = stats
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
		sc.addEventHistoryEntry(newEntries[i])
	}
	// Inflate any pre-existing entries (those parsed in earlier polls
	// before their voices landed in soul_flow.jsonl, e.g. on initial
	// rebuild from sources or a later kernel write).
	for i := range sc.entries {
		sc.maybeInflateSoulFlow(&sc.entries[i])
	}
	sc.append(newEntries...)
}

type sessionJSONLWindowLine struct {
	start int64
	end   int64
}

// readPreviousJSONLLine retains only a fixed prefix for compatibility with the
// range finder callers; metadata classification itself structurally scans the
// entire range via readSessionEventMetadataRange.
const sessionCountPrefixLimit = 128 * 1024

func lastCompleteJSONLOffset(f *os.File) int64 {
	info, err := f.Stat()
	if err != nil || info.Size() == 0 {
		return 0
	}
	size := info.Size()
	var one [1]byte
	if _, err := f.ReadAt(one[:], size-1); err == nil && one[0] == '\n' {
		return size
	}
	for pos := size - 1; pos >= 0; {
		start := pos - 64*1024 + 1
		if start < 0 {
			start = 0
		}
		buf := make([]byte, pos-start+1)
		if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
			return 0
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			return start + int64(i) + 1
		}
		if start == 0 {
			return 0
		}
		pos = start - 1
	}
	return 0
}

func readPreviousJSONLLine(f *os.File, end int64) (sessionJSONLWindowLine, []byte, bool, bool) {
	if end <= 0 {
		return sessionJSONLWindowLine{}, nil, false, false
	}
	contentEnd := end
	var one [1]byte
	if _, err := f.ReadAt(one[:], end-1); err != nil {
		return sessionJSONLWindowLine{}, nil, false, false
	}
	if one[0] == '\n' {
		contentEnd--
	}
	start := int64(0)
	for pos := contentEnd - 1; pos >= 0; {
		blockStart := pos - 64*1024 + 1
		if blockStart < 0 {
			blockStart = 0
		}
		buf := make([]byte, pos-blockStart+1)
		if _, err := f.ReadAt(buf, blockStart); err != nil && err != io.EOF {
			return sessionJSONLWindowLine{}, nil, false, false
		}
		if i := bytes.LastIndexByte(buf, '\n'); i >= 0 {
			start = blockStart + int64(i) + 1
			break
		}
		if blockStart == 0 {
			break
		}
		pos = blockStart - 1
	}
	lineLen := contentEnd - start
	readLen := lineLen
	if readLen > sessionCountPrefixLimit {
		readLen = sessionCountPrefixLimit
	}
	line := make([]byte, readLen)
	if len(line) > 0 {
		if _, err := f.ReadAt(line, start); err != nil && err != io.EOF {
			return sessionJSONLWindowLine{}, nil, false, false
		}
	}
	return sessionJSONLWindowLine{start: start, end: end}, bytes.TrimSpace(line), lineLen > readLen, true
}

// ingestEventsFromJSONLWindowed reads backward from the parser-proven EOF and
// materializes only the newest requested session records. Older prefix bodies are
// never parsed on the content path. If the cut lands inside a legacy API group,
// only its nearest hidden llm_response boundary is retained.
func (sc *SessionCache) ingestEventsFromJSONLWindowed(orchDir string, window int) bool {
	if orchDir == "" || window <= 0 {
		return false
	}
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	f, err := os.Open(eventsPath)
	if err != nil {
		return false
	}
	defer f.Close()

	completeOff := lastCompleteJSONLOffset(f)
	if completeOff == 0 {
		info, statErr := f.Stat()
		if statErr != nil || info.Size() != 0 {
			return false
		}
		sc.eventsOff = 0
		abs, _ := filepath.Abs(eventsPath)
		sc.historyCountPlan = sessionHistoryCountPlan{identity: fmt.Sprintf("jsonl:%s:%d", filepath.Clean(abs), 0), path: filepath.Clean(abs), upper: 0}
		return true
	}

	selectedNewest := make([]sessionJSONLWindowLine, 0, window)
	scanEnd := completeOff
	for scanEnd > 0 && len(selectedNewest) < window {
		rng, _, _, ok := readPreviousJSONLLine(f, scanEnd)
		if !ok {
			return false
		}
		scanEnd = rng.start
		meta, valid := readSessionEventMetadataRange(f, rng.start, rng.end-rng.start)
		if valid && isSessionEventMetadataContent(meta) {
			selectedNewest = append(selectedNewest, rng)
		}
	}
	selected := make([]sessionJSONLWindowLine, len(selectedNewest))
	for i := range selectedNewest {
		selected[len(selectedNewest)-1-i] = selectedNewest[i]
	}
	parseRanges := func(ranges []sessionJSONLWindowLine) ([]SessionEntry, bool) {
		entries := make([]SessionEntry, 0, len(ranges))
		for _, rng := range ranges {
			line := make([]byte, rng.end-rng.start)
			n, err := f.ReadAt(line, rng.start)
			if err != nil && err != io.EOF || n != len(line) {
				return nil, false
			}
			if entry := parseEvent(bytes.TrimRight(line, "\r\n")); entry != nil {
				entries = append(entries, *entry)
			}
		}
		return entries, true
	}
	newEntries, ok := parseRanges(selected)
	if !ok {
		return false
	}
	if len(newEntries) > 0 && needsGroupBackExtension(newEntries[0]) && len(selected) > 0 {
		for boundaryEnd := selected[0].start; boundaryEnd > 0; {
			rng, _, _, ok := readPreviousJSONLLine(f, boundaryEnd)
			if !ok {
				return false
			}
			boundaryEnd = rng.start
			meta, valid := readSessionEventMetadataRange(f, rng.start, rng.end-rng.start)
			if !valid {
				continue
			}
			if meta.Type == "llm_call" {
				break
			}
			if meta.Type == "llm_response" {
				boundary, ok := parseRanges([]sessionJSONLWindowLine{rng})
				if !ok {
					return false
				}
				if len(boundary) == 1 {
					boundary[0].TokenUsage = nil // grouping marker only; never an extra visible body
					newEntries = append(boundary, newEntries...)
				}
				break
			}
		}
	}

	sc.ingestSoulFlowVoices(orchDir)
	for i := range newEntries {
		sc.maybeInflateSoulFlow(&newEntries[i])
	}
	for i := range sc.entries {
		sc.maybeInflateSoulFlow(&sc.entries[i])
	}
	if scanEnd > 0 {
		sc.complete = false
	}
	abs, _ := filepath.Abs(eventsPath)
	abs = filepath.Clean(abs)
	sc.historyCountPlan = sessionHistoryCountPlan{identity: fmt.Sprintf("jsonl:%s:%d", abs, completeOff), path: abs, upper: completeOff}
	sc.append(newEntries...)
	sc.eventsOff = completeOff
	return true
}

// ingestEventsFromSQLite is retained for the opt-in diagnostic probe. It declines
// SQLite replay because endpoint coverage cannot establish interior continuity.
func (sc *SessionCache) ingestEventsFromSQLite(orchDir string) bool {
	return sc.ingestEventsFromSQLiteWindowed(orchDir, 0)
}

// ingestEventsFromSQLiteWindowed is the single gate for the disabled optimization.
// Keeping the rejection here prevents any caller from mistaking sparse endpoint
// coverage for complete canonical history.
func (sc *SessionCache) ingestEventsFromSQLiteWindowed(orchDir string, window int) bool {
	// QueryEventsIndexCoverage proves identity and endpoints, not interior
	// continuity. Decline this optimization until the index can supply a sound
	// continuity proof; callers fall back to canonical JSONL, whose backward scan
	// remains bounded to the requested content window.
	return false
}

func (sc *SessionCache) addEventHistoryEntry(e SessionEntry) {
	switch e.Type {
	case "insight":
		sc.historyStats.Insights++
	case "llm_call":
		// Hidden grouping reset, never a rendered Mail entry.
	case "llm_response":
		if e.TokenUsage != nil {
			sc.historyStats.Detailed++
		}
	default:
		sc.historyStats.Detailed++
	}
}

type nonEmptyJSONString bool

type nonZeroJSONNumber bool

type sessionEventCountMetadata struct {
	Type         string
	Text         nonEmptyJSONString
	InputTokens  nonZeroJSONNumber
	OutputTokens nonZeroJSONNumber
	CachedTokens nonZeroJSONNumber
}

// metadataJSONScanner validates and walks one JSON value without retaining
// unrelated values. In particular, arbitrarily large nested strings are consumed
// byte-by-byte rather than materialized merely to discover a later top-level key.
type metadataJSONScanner struct {
	r *bufio.Reader
}

func (s *metadataJSONScanner) readNonSpace() (byte, error) {
	for {
		b, err := s.r.ReadByte()
		if err != nil {
			return 0, err
		}
		switch b {
		case ' ', '\t', '\r', '\n':
			continue
		default:
			return b, nil
		}
	}
}

func (s *metadataJSONScanner) expectBytes(want string) bool {
	for i := 0; i < len(want); i++ {
		b, err := s.r.ReadByte()
		if err != nil || b != want[i] {
			return false
		}
	}
	return true
}

// scanString consumes a JSON string after its opening quote. Captured includes
// quotes and is suitable for bounded json.Unmarshal; once limit is exceeded, scanning
// continues without growing memory. nonEmpty reflects the decoded string's
// emptiness (any valid byte or escape between quotes decodes to content).
func (s *metadataJSONScanner) scanString(limit int) (captured string, nonEmpty, overflow, ok bool) {
	buf := make([]byte, 0, min(limit, 64))
	if limit > 0 {
		buf = append(buf, '"')
	}
	appendByte := func(b byte) {
		if limit <= 0 || overflow {
			return
		}
		if len(buf) >= limit {
			overflow = true
			buf = nil
			return
		}
		buf = append(buf, b)
	}
	for {
		b, err := s.r.ReadByte()
		if err != nil {
			return "", false, overflow, false
		}
		if b == '"' {
			appendByte(b)
			if limit > 0 && !overflow {
				captured = string(buf)
			}
			return captured, nonEmpty, overflow, true
		}
		if b < 0x20 {
			return "", false, overflow, false
		}
		nonEmpty = true
		appendByte(b)
		if b != '\\' {
			continue
		}
		esc, err := s.r.ReadByte()
		if err != nil {
			return "", false, overflow, false
		}
		appendByte(esc)
		switch esc {
		case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
		case 'u':
			for i := 0; i < 4; i++ {
				h, err := s.r.ReadByte()
				if err != nil || !((h >= '0' && h <= '9') || (h >= 'a' && h <= 'f') || (h >= 'A' && h <= 'F')) {
					return "", false, overflow, false
				}
				appendByte(h)
			}
		default:
			return "", false, overflow, false
		}
	}
}

func isJSONNumberByte(b byte) bool {
	return b == '-' || b == '+' || b == '.' || b == 'e' || b == 'E' || b >= '0' && b <= '9'
}

func validJSONNumber(number string) bool {
	if number == "" {
		return false
	}
	i := 0
	if number[i] == '-' {
		i++
		if i == len(number) {
			return false
		}
	}
	if number[i] == '0' {
		i++
	} else if number[i] >= '1' && number[i] <= '9' {
		for i < len(number) && number[i] >= '0' && number[i] <= '9' {
			i++
		}
	} else {
		return false
	}
	if i < len(number) && number[i] == '.' {
		i++
		start := i
		for i < len(number) && number[i] >= '0' && number[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	if i < len(number) && (number[i] == 'e' || number[i] == 'E') {
		i++
		if i < len(number) && (number[i] == '+' || number[i] == '-') {
			i++
		}
		start := i
		for i < len(number) && number[i] >= '0' && number[i] <= '9' {
			i++
		}
		if i == start {
			return false
		}
	}
	return i == len(number)
}

func (s *metadataJSONScanner) scanNumber(first byte, capture bool) (string, bool) {
	const maxNumberBytes = 1024
	buf := make([]byte, 0, 32)
	overflow := false
	add := func(b byte) {
		if !capture || overflow {
			return
		}
		if len(buf) == maxNumberBytes {
			overflow = true
			buf = nil
			return
		}
		buf = append(buf, b)
	}
	add(first)
	for {
		peek, err := s.r.Peek(1)
		if err != nil || !isJSONNumberByte(peek[0]) {
			break
		}
		b, _ := s.r.ReadByte()
		add(b)
	}
	if overflow {
		return "", false
	}
	if !capture {
		return "", true
	}
	number := string(buf)
	return number, validJSONNumber(number)
}

func (s *metadataJSONScanner) skipValue(first byte, depth int) bool {
	switch first {
	case '"':
		_, _, _, ok := s.scanString(0)
		return ok
	case '{':
		if depth >= 10000 {
			return false
		}
		b, err := s.readNonSpace()
		if err != nil {
			return false
		}
		if b == '}' {
			return true
		}
		for {
			if b != '"' {
				return false
			}
			if _, _, _, ok := s.scanString(0); !ok {
				return false
			}
			b, err = s.readNonSpace()
			if err != nil || b != ':' {
				return false
			}
			b, err = s.readNonSpace()
			if err != nil || !s.skipValue(b, depth+1) {
				return false
			}
			b, err = s.readNonSpace()
			if err != nil {
				return false
			}
			if b == '}' {
				return true
			}
			if b != ',' {
				return false
			}
			b, err = s.readNonSpace()
			if err != nil {
				return false
			}
		}
	case '[':
		if depth >= 10000 {
			return false
		}
		b, err := s.readNonSpace()
		if err != nil {
			return false
		}
		if b == ']' {
			return true
		}
		for {
			if !s.skipValue(b, depth+1) {
				return false
			}
			b, err = s.readNonSpace()
			if err != nil {
				return false
			}
			if b == ']' {
				return true
			}
			if b != ',' {
				return false
			}
			b, err = s.readNonSpace()
			if err != nil {
				return false
			}
		}
	case 't':
		return s.expectBytes("rue")
	case 'f':
		return s.expectBytes("alse")
	case 'n':
		return s.expectBytes("ull")
	default:
		if first != '-' && (first < '0' || first > '9') {
			return false
		}
		number, ok := s.scanNumber(first, true)
		if !ok {
			return false
		}
		// encoding/json decodes every untyped JSON number as float64, even in
		// unrelated fields. Match its range check so an overflowing decoy value
		// cannot make metadata accept a record the canonical parser rejects.
		_, err := strconv.ParseFloat(number, 64)
		return err == nil
	}
}

func (s *metadataJSONScanner) scanMetadata() (sessionEventCountMetadata, bool) {
	var meta sessionEventCountMetadata
	first, err := s.readNonSpace()
	if err != nil || first != '{' {
		return meta, false
	}
	b, err := s.readNonSpace()
	if err != nil {
		return meta, false
	}
	if b == '}' {
		return meta, s.onlyWhitespaceRemains()
	}
	for {
		if b != '"' {
			return meta, false
		}
		rawKey, _, overflow, ok := s.scanString(256)
		if !ok {
			return meta, false
		}
		key := ""
		if !overflow {
			if err = json.Unmarshal([]byte(rawKey), &key); err != nil {
				return meta, false
			}
		}
		b, err = s.readNonSpace()
		if err != nil || b != ':' {
			return meta, false
		}
		b, err = s.readNonSpace()
		if err != nil {
			return meta, false
		}
		switch key {
		case "type":
			meta.Type = ""
			if b == '"' {
				raw, _, overflow, ok := s.scanString(256)
				if !ok {
					return meta, false
				}
				if !overflow {
					if err = json.Unmarshal([]byte(raw), &meta.Type); err != nil {
						return meta, false
					}
				}
			} else if !s.skipValue(b, 1) {
				return meta, false
			}
		case "text":
			meta.Text = false
			if b == '"' {
				_, nonEmpty, _, ok := s.scanString(0)
				if !ok {
					return meta, false
				}
				meta.Text = nonEmptyJSONString(nonEmpty)
			} else if !s.skipValue(b, 1) {
				return meta, false
			}
		case "input_tokens", "output_tokens", "cached_tokens":
			var dst *nonZeroJSONNumber
			switch key {
			case "input_tokens":
				dst = &meta.InputTokens
			case "output_tokens":
				dst = &meta.OutputTokens
			default:
				dst = &meta.CachedTokens
			}
			*dst = false
			if b == '-' || b >= '0' && b <= '9' {
				number, ok := s.scanNumber(b, true)
				if !ok {
					return meta, false
				}
				value, err := strconv.ParseFloat(number, 64)
				if err != nil {
					return meta, false
				}
				*dst = nonZeroJSONNumber(int64(value) != 0)
			} else if !s.skipValue(b, 1) {
				return meta, false
			}
		default:
			if !s.skipValue(b, 1) {
				return meta, false
			}
		}
		b, err = s.readNonSpace()
		if err != nil {
			return meta, false
		}
		if b == '}' {
			return meta, s.onlyWhitespaceRemains()
		}
		if b != ',' {
			return meta, false
		}
		b, err = s.readNonSpace()
		if err != nil {
			return meta, false
		}
	}
}

func (s *metadataJSONScanner) onlyWhitespaceRemains() bool {
	for {
		b, err := s.r.ReadByte()
		if err == io.EOF {
			return true
		}
		if err != nil {
			return false
		}
		if b != ' ' && b != '\t' && b != '\r' && b != '\n' {
			return false
		}
	}
}

func readSessionEventMetadata(r io.Reader) (sessionEventCountMetadata, bool) {
	s := metadataJSONScanner{r: bufio.NewReaderSize(r, 32*1024)}
	return s.scanMetadata()
}

func canonicalSessionEventMetadata(data []byte) (sessionEventCountMetadata, bool) {
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return sessionEventCountMetadata{}, false
	}
	var meta sessionEventCountMetadata
	meta.Type, _ = raw["type"].(string)
	if text, ok := raw["text"].(string); ok {
		meta.Text = nonEmptyJSONString(text != "")
	}
	for key, dst := range map[string]*nonZeroJSONNumber{
		"input_tokens":  &meta.InputTokens,
		"output_tokens": &meta.OutputTokens,
		"cached_tokens": &meta.CachedTokens,
	} {
		if value, ok := raw[key].(float64); ok {
			*dst = nonZeroJSONNumber(int64(value) != 0)
		}
	}
	return meta, true
}

func readSessionEventMetadataRange(f *os.File, start, length int64) (sessionEventCountMetadata, bool) {
	if length < 0 {
		return sessionEventCountMetadata{}, false
	}
	if meta, ok := readSessionEventMetadata(io.NewSectionReader(f, start, length)); ok {
		return meta, true
	}
	// The streaming fast path deliberately caps captured key/type/number
	// lexemes. Fall back to the canonical decoder for the one physical record
	// whenever that bounded parser declines it. This preserves exact
	// encoding/json acceptance, duplicate-key, and float64 number semantics
	// without ever retaining more than one historical record at a time.
	bufferLen := int(length)
	if int64(bufferLen) != length {
		return sessionEventCountMetadata{}, false
	}
	data := make([]byte, bufferLen)
	if n, err := f.ReadAt(data, start); n != len(data) || err != nil {
		return sessionEventCountMetadata{}, false
	}
	return canonicalSessionEventMetadata(data)
}

// countSessionEventMetadataRange scans [0, upper) without retaining historical
// bodies. Physical records are discovered with a fixed-size reader and then
// structurally scanned from the file, so memory is bounded even when relevant
// top-level fields follow multi-megabyte nested or scalar payloads.
func countSessionEventMetadataRange(path string, upper int64) (SessionHistoryStats, error) {
	var out SessionHistoryStats
	if upper <= 0 {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()
	reader := bufio.NewReaderSize(io.LimitReader(f, upper), 64*1024)
	var recordStart, consumed int64
	for {
		fragment, readErr := reader.ReadSlice('\n')
		consumed += int64(len(fragment))
		if len(fragment) > 0 && fragment[len(fragment)-1] == '\n' {
			if meta, ok := readSessionEventMetadataRange(f, recordStart, consumed-recordStart); ok {
				entryStats := statsForSessionEventMetadata(meta)
				out.Detailed += entryStats.Detailed
				out.Insights += entryStats.Insights
			}
			recordStart = consumed
		}
		if readErr == bufio.ErrBufferFull {
			continue
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return out, readErr
		}
	}
	return out, nil
}

// isSessionEventMetadataContent mirrors parseEventMap's entry membership for a
// bounded content window. Hidden legacy grouping carriers still consume slots:
// buildMessages needs llm_call/llm_response even when they add no visible count.
func isSessionEventMetadataContent(meta sessionEventCountMetadata) bool {
	if meta.Type == "llm_call" || meta.Type == "llm_response" {
		return true
	}
	stats := statsForSessionEventMetadata(meta)
	return stats.Detailed > 0 || stats.Insights > 0
}

func statsForSessionEventMetadata(meta sessionEventCountMetadata) SessionHistoryStats {
	switch meta.Type {
	case "thinking", "diary", "text_input", "text_output":
		if bool(meta.Text) {
			return SessionHistoryStats{Detailed: 1}
		}
	case "insight":
		if bool(meta.Text) {
			return SessionHistoryStats{Insights: 1}
		}
	case "llm_call":
		return SessionHistoryStats{}
	case "llm_response":
		if bool(meta.InputTokens) || bool(meta.OutputTokens) || bool(meta.CachedTokens) {
			return SessionHistoryStats{Detailed: 1}
		}
	case "tool_call", "tool_result", "soul_flow", "notification", "aed", "apriori_summary",
		"consultation_fire", "notification_pair_injected", "aed_attempt", "aed_exhausted", "aed_timeout", "apriori_summary_generated",
		"apriori_summary_cap_refused", "apriori_summary_failed", "apriori_summary_empty",
		"apriori_summary_no_summarizer":
		return SessionHistoryStats{Detailed: 1}
	}
	return SessionHistoryStats{}
}

// needsGroupBackExtension reports whether the oldest loaded windowed entry is a
// legacy api-grouped entry with no explicit api_call_id — the case where a
// window that cut off the group's hidden llm_response header must reach back to
// that header so grouping (and the separator suppression it drives) survives.
// Entries that already carry an explicit api_call_id, and non-grouped types,
// need no back-extension.
func needsGroupBackExtension(e SessionEntry) bool {
	if e.ApiCallID != "" {
		return false
	}
	switch e.Type {
	case "thinking", "diary", "text_input", "text_output", "tool_call", "tool_result":
		return true
	}
	return false
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
