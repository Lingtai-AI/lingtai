package fs

import (
	"container/heap"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// DaemonLedgerEntry is a single per-call token_ledger.jsonl line from a
// daemon run directory, tagged with the daemon's identity so the kanban
// detail view can show which daemon each call belongs to. The embedded
// LedgerEntry carries the token/model/endpoint fields; RunID/Handle/State
// come from the run's daemon.json (or the run directory name when the
// identity card is missing).
type DaemonLedgerEntry struct {
	LedgerEntry
	RunID   string // daemons/<run_id> directory name
	Handle  string // daemon.json "handle" (e.g. "em-1"); empty when no card
	State   string // daemon.json "state" (running/done/failed/...); empty when no card
	Backend string `json:"-"` // daemon.json "backend"; in-memory display dimension only
}

// daemonCard is the typed subset of daemon.json fields the daemon-ledger
// path needs: identity for tagging and attribution, plus the backend/preset
// fields for fallback provider derivation when a run has no per-call ledger.
// CLITokens and Tokens carry the typed token snapshot so a card is read only
// once whenever its file signature changes.
type daemonCard struct {
	Handle         string                  `json:"handle"`
	RunID          string                  `json:"run_id"`
	State          string                  `json:"state"`
	FinishedAt     json.RawMessage         `json:"finished_at"`
	Backend        string                  `json:"backend"`
	Model          string                  `json:"model"`
	PresetProvider string                  `json:"preset_provider"`
	PresetModel    string                  `json:"preset_model"`
	CLITokens      *daemonTokenBlock       `json:"cli_tokens"`
	Tokens         *daemonLegacyTokenBlock `json:"tokens"`
}

// daemonTokenBlock is the typed cli_tokens sub-object in daemon.json.
type daemonTokenBlock struct {
	Input    int64 `json:"input"`
	Output   int64 `json:"output"`
	Thinking int64 `json:"thinking"`
	Cached   int64 `json:"cached"`
	Calls    int64 `json:"calls"`
}

// daemonLegacyTokenBlock is the typed legacy tokens sub-object (no calls
// field — pre-cli_tokens era).
type daemonLegacyTokenBlock struct {
	Input    int64 `json:"input"`
	Output   int64 `json:"output"`
	Thinking int64 `json:"thinking"`
	Cached   int64 `json:"cached"`
}

// DaemonDetailSnapshot is the bounded daemon data used by Ctrl+D. Token totals
// and recent rows cover the newest ScannedRuns run directories, while
// Counts.Total is the cheap directory-entry total across all run directories.
// Running/Queued/Done/Failed are classified only inside the recent window; the
// UI intentionally renders Running and Total and labels token totals with the
// window size.
type DaemonDetailSnapshot struct {
	ByProvider   map[string]TokenTotals
	Recent       []DaemonLedgerEntry
	Counts       DaemonCounts
	ScannedRuns  int
	TotalRuns    int
	TerminalRuns int // total minus recent/observed-live running and queued runs
}

type daemonFileStamp struct {
	exists  bool
	size    int64
	modTime int64
}

type daemonRunRef struct {
	name    string
	modTime int64
}

type daemonLedgerFileSummary struct {
	byProvider map[string]TokenTotals
	recent     []LedgerEntry // append order; bounded to the newest recentLimit rows
	validRows  int
}

type daemonRunMemo struct {
	cardStamp      daemonFileStamp
	ledgerStamp    daemonFileStamp
	heartbeatStamp daemonFileStamp
	recentLimit    int
	card           daemonCard
	ledger         daemonLedgerFileSummary
}

type daemonDirectoryMemo struct {
	dirStamp daemonFileStamp
	runs     []daemonRunRef
	byRun    map[string]*daemonRunMemo
	liveRuns map[string]bool // once observed live, always rechecked until terminal
}

var daemonDetailCache = struct {
	sync.Mutex
	byDir map[string]*daemonDirectoryMemo
}{byDir: map[string]*daemonDirectoryMemo{}}

// ReadDaemonDetailSnapshot reads only the newest recentRunN run directories
// (all runs when recentRunN <= 0), retains at most recentCallN globally newest
// ledger rows, and memoizes each selected card/ledger by size+mtime.
//
// The daemons/ directory listing is reused while its signature is unchanged.
// Cached run-directory mtimes are still statted so an atomically replaced file
// or live state transition can reorder the recent window without requiring a
// parent-directory change. For selected runs, daemon.json, token_ledger.jsonl,
// and .heartbeat are statted on every call. A heartbeat change forces a card
// and ledger reread, preventing a live daemon from being hidden by the memo even
// on a filesystem with coarse content mtimes.
func ReadDaemonDetailSnapshot(agentDir string, recentRunN, recentCallN int) DaemonDetailSnapshot {
	daemonDir := filepath.Join(agentDir, "daemons")
	key := filepath.Clean(daemonDir)

	daemonDetailCache.Lock()
	defer daemonDetailCache.Unlock()

	memo := daemonDetailCache.byDir[key]
	if memo == nil {
		memo = &daemonDirectoryMemo{
			byRun:    map[string]*daemonRunMemo{},
			liveRuns: map[string]bool{},
		}
		daemonDetailCache.byDir[key] = memo
	}
	if !refreshDaemonRunList(daemonDir, memo) {
		delete(daemonDetailCache.byDir, key)
		return DaemonDetailSnapshot{}
	}

	runLimit := len(memo.runs)
	if recentRunN > 0 && runLimit > recentRunN {
		runLimit = recentRunN
	}
	runs := append([]daemonRunRef(nil), memo.runs[:runLimit]...)
	selected := make(map[string]bool, len(runs)+len(memo.liveRuns))
	for _, run := range runs {
		selected[run.name] = true
	}
	// A run observed as live never ages out of the window. It remains on the
	// heartbeat/card/ledger fast-refresh path until its card becomes terminal.
	for _, run := range memo.runs[runLimit:] {
		if memo.liveRuns[run.name] && !selected[run.name] {
			runs = append(runs, run)
			selected[run.name] = true
		}
	}

	result := DaemonDetailSnapshot{
		ByProvider:  map[string]TokenTotals{},
		ScannedRuns: len(runs),
		TotalRuns:   len(memo.runs),
	}
	// The fast total counts run directories from the single cached ReadDir,
	// rather than opening every historical daemon.json.
	result.Counts.Total = len(memo.runs)

	var recentHeap daemonEntryHeap
	var unbounded []DaemonLedgerEntry
	for _, run := range runs {
		selected[run.name] = true
		runDir := filepath.Join(daemonDir, run.name)
		runMemo := refreshDaemonRunMemo(runDir, recentCallN, memo.byRun[run.name])
		memo.byRun[run.name] = runMemo

		card := runMemo.card
		if card.RunID == "" {
			card.RunID = run.name
		}
		addDaemonDetailState(&result.Counts, card)
		state := daemonStateFile{State: card.State, FinishedAt: card.FinishedAt}
		bucket := stateBucket(card.State)
		if (bucket == "running" && isRunningDaemonState(state)) || bucket == "queued" {
			memo.liveRuns[run.name] = true
		} else {
			delete(memo.liveRuns, run.name)
		}

		if runMemo.ledger.validRows > 0 {
			addProviderTotals(result.ByProvider, runMemo.ledger.byProvider)
			for _, entry := range runMemo.ledger.recent {
				tagged := DaemonLedgerEntry{
					LedgerEntry: entry,
					RunID:       card.RunID,
					Handle:      card.Handle,
					State:       card.State,
					Backend:     card.Backend,
				}
				if recentCallN <= 0 {
					unbounded = append(unbounded, tagged)
				} else {
					pushRecentDaemonEntry(&recentHeap, tagged, recentCallN)
				}
			}
			continue
		}
		addDaemonFallbackTotals(result.ByProvider, card)
	}

	// Keep the process cache bounded to the currently selected run window.
	for name := range memo.byRun {
		if !selected[name] {
			delete(memo.byRun, name)
		}
	}

	result.TerminalRuns = result.TotalRuns - result.Counts.Running - result.Counts.Queued
	if result.TerminalRuns < 0 {
		result.TerminalRuns = 0
	}
	if recentCallN <= 0 {
		sort.SliceStable(unbounded, func(i, j int) bool { return unbounded[i].TS > unbounded[j].TS })
		result.Recent = unbounded
	} else {
		result.Recent = drainRecentDaemonEntries(&recentHeap)
	}
	return result
}

// DaemonLedgerSummary returns provider/backend token totals and recent tagged
// rows from the newest recentN run directories. recentN <= 0 preserves the
// legacy all-runs behavior for non-UI callers; the detail UI uses a separate
// run limit and call-row limit through ReadDaemonDetailSnapshot.
func DaemonLedgerSummary(agentDir string, recentN int) (map[string]TokenTotals, []DaemonLedgerEntry) {
	snapshot := ReadDaemonDetailSnapshot(agentDir, recentN, recentN)
	return snapshot.ByProvider, snapshot.Recent
}

// DaemonRecentLedger returns the most-recent recentN daemon per-call ledger
// entries across the newest recentN run directories, newest first.
func DaemonRecentLedger(agentDir string, recentN int) []DaemonLedgerEntry {
	_, recent := DaemonLedgerSummary(agentDir, recentN)
	return recent
}

func daemonPathStamp(path string) daemonFileStamp {
	info, err := os.Stat(path)
	if err != nil {
		return daemonFileStamp{}
	}
	return daemonFileStamp{exists: true, size: info.Size(), modTime: info.ModTime().UnixNano()}
}

// refreshDaemonRunList avoids ReadDir on an unchanged directory. It still
// compares every cached run-directory mtime, which is metadata-only O(D) work
// and is what makes recent-by-mtime ordering responsive to run transitions.
func refreshDaemonRunList(daemonDir string, memo *daemonDirectoryMemo) bool {
	dirStamp := daemonPathStamp(daemonDir)
	if !dirStamp.exists {
		return false
	}

	needReadDir := !memo.dirStamp.exists || memo.dirStamp != dirStamp
	changed := false
	if !needReadDir {
		for i := range memo.runs {
			stamp := daemonPathStamp(filepath.Join(daemonDir, memo.runs[i].name))
			if !stamp.exists {
				needReadDir = true
				break
			}
			if memo.runs[i].modTime != stamp.modTime {
				memo.runs[i].modTime = stamp.modTime
				changed = true
			}
		}
	}

	if needReadDir {
		entries, err := os.ReadDir(daemonDir)
		if err != nil {
			return false
		}
		runs := make([]daemonRunRef, 0, len(entries))
		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}
			info, err := entry.Info()
			if err != nil {
				continue
			}
			runs = append(runs, daemonRunRef{name: entry.Name(), modTime: info.ModTime().UnixNano()})
		}
		memo.runs = runs
		memo.dirStamp = dirStamp
		changed = true
	}

	if changed {
		sort.SliceStable(memo.runs, func(i, j int) bool {
			if memo.runs[i].modTime != memo.runs[j].modTime {
				return memo.runs[i].modTime > memo.runs[j].modTime
			}
			return memo.runs[i].name > memo.runs[j].name
		})
	}
	return true
}

func refreshDaemonRunMemo(runDir string, recentLimit int, memo *daemonRunMemo) *daemonRunMemo {
	if memo == nil {
		memo = &daemonRunMemo{}
	}

	heartbeatStamp := daemonPathStamp(filepath.Join(runDir, ".heartbeat"))
	liveChanged := memo.heartbeatStamp != heartbeatStamp
	cardPath := filepath.Join(runDir, "daemon.json")
	cardStamp := daemonPathStamp(cardPath)
	if liveChanged || memo.cardStamp != cardStamp {
		memo.card = readDaemonCard(cardPath)
		memo.cardStamp = cardStamp
	}

	ledgerPath := filepath.Join(runDir, "logs", "token_ledger.jsonl")
	ledgerStamp := daemonPathStamp(ledgerPath)
	if liveChanged || memo.ledgerStamp != ledgerStamp || memo.recentLimit != recentLimit {
		memo.ledger = readDaemonLedgerSummary(ledgerPath, recentLimit)
		memo.ledgerStamp = ledgerStamp
		memo.recentLimit = recentLimit
	}
	memo.heartbeatStamp = heartbeatStamp
	return memo
}

// readDaemonCard reads daemon.json; missing or malformed files yield a zero card.
func readDaemonCard(path string) daemonCard {
	var card daemonCard
	data, err := os.ReadFile(path)
	if err != nil {
		return card
	}
	if err := json.Unmarshal(data, &card); err != nil {
		return daemonCard{}
	}
	return card
}

// readDaemonLedgerSummary streams a ledger once. It aggregates while decoding
// and retains only a fixed-size ring of recent rows instead of materializing
// every historical LedgerEntry and truncating after a global sort.
func readDaemonLedgerSummary(path string, recentLimit int) daemonLedgerFileSummary {
	out := daemonLedgerFileSummary{byProvider: map[string]TokenTotals{}}
	var ring []LedgerEntry
	next := 0
	_ = forEachJSONLLine(path, func(line []byte) {
		var entry LedgerEntry
		if line[0] != '{' || json.Unmarshal(line, &entry) != nil {
			return
		}
		out.validRows++
		provider := DeriveLedgerProvider(entry.Endpoint, entry.Model)
		t := out.byProvider[provider]
		t.Input += entry.Input
		t.Output += entry.Output
		t.Thinking += entry.Thinking
		t.Cached += entry.Cached
		t.APICalls++
		out.byProvider[provider] = t

		if recentLimit <= 0 {
			ring = append(ring, entry)
			return
		}
		if len(ring) < recentLimit {
			ring = append(ring, entry)
			return
		}
		ring[next] = entry
		next = (next + 1) % recentLimit
	})
	if recentLimit > 0 && len(ring) == recentLimit && next != 0 {
		ordered := make([]LedgerEntry, 0, len(ring))
		ordered = append(ordered, ring[next:]...)
		ordered = append(ordered, ring[:next]...)
		ring = ordered
	}
	out.recent = ring
	return out
}

func addProviderTotals(dst, src map[string]TokenTotals) {
	for provider, value := range src {
		t := dst[provider]
		t.Input += value.Input
		t.Output += value.Output
		t.Thinking += value.Thinking
		t.Cached += value.Cached
		t.APICalls += value.APICalls
		dst[provider] = t
	}
}

func addDaemonFallbackTotals(dst map[string]TokenTotals, card daemonCard) {
	var input, output, thinking, cached, calls int64
	if cli := card.CLITokens; cli != nil &&
		(cli.Input+cli.Output+cli.Thinking+cli.Cached != 0 || cli.Calls != 0) {
		// cli_tokens keeps raw/non-cached input and cached input disjoint.
		input = cli.Input + cli.Cached
		output = cli.Output
		thinking = cli.Thinking
		cached = cli.Cached
		calls = cli.Calls
	} else if legacy := card.Tokens; legacy != nil {
		input = legacy.Input
		output = legacy.Output
		thinking = legacy.Thinking
		cached = legacy.Cached
	}
	if input+output+thinking+cached == 0 && calls == 0 {
		return
	}
	provider := daemonFallbackProvider(card)
	t := dst[provider]
	t.Input += input
	t.Output += output
	t.Thinking += thinking
	t.Cached += cached
	t.APICalls += calls
	dst[provider] = t
}

func addDaemonDetailState(counts *DaemonCounts, card daemonCard) {
	state := daemonStateFile{State: card.State, FinishedAt: card.FinishedAt}
	switch stateBucket(card.State) {
	case "running":
		if isRunningDaemonState(state) {
			counts.Running++
		}
	case "queued":
		counts.Queued++
	case "done":
		counts.Done++
	case "failed":
		counts.Failed++
	}
}

type daemonEntryHeap []DaemonLedgerEntry

func (h daemonEntryHeap) Len() int { return len(h) }
func (h daemonEntryHeap) Less(i, j int) bool {
	if h[i].TS != h[j].TS {
		return h[i].TS < h[j].TS
	}
	return h[i].RunID < h[j].RunID
}
func (h daemonEntryHeap) Swap(i, j int) { h[i], h[j] = h[j], h[i] }
func (h *daemonEntryHeap) Push(x any)   { *h = append(*h, x.(DaemonLedgerEntry)) }
func (h *daemonEntryHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[:n-1]
	return x
}

func pushRecentDaemonEntry(h *daemonEntryHeap, entry DaemonLedgerEntry, limit int) {
	if h.Len() < limit {
		heap.Push(h, entry)
		return
	}
	oldest := (*h)[0]
	if entry.TS < oldest.TS || (entry.TS == oldest.TS && entry.RunID <= oldest.RunID) {
		return
	}
	heap.Pop(h)
	heap.Push(h, entry)
}

func drainRecentDaemonEntries(h *daemonEntryHeap) []DaemonLedgerEntry {
	out := make([]DaemonLedgerEntry, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(DaemonLedgerEntry)
	}
	return out
}

// daemonFallbackProvider derives a provider/backend label for daemon runs
// that have no per-call token ledger, using the daemon.json identity card
// fields in precedence order:
//
//  1. non-empty preset_provider
//  2. non-empty non-"lingtai" backend (claude-p / codex / mimocode / ...)
//  3. DeriveLedgerProvider("", model) on preset_model, then model
//  4. non-empty backend or model as-is
//  5. "daemon"
func daemonFallbackProvider(card daemonCard) string {
	if card.PresetProvider != "" {
		return card.PresetProvider
	}
	if card.Backend != "" && card.Backend != "lingtai" {
		return card.Backend
	}
	model := card.PresetModel
	if model == "" {
		model = card.Model
	}
	if model != "" {
		p := DeriveLedgerProvider("", model)
		if p != "unknown" {
			return p
		}
	}
	if card.Backend != "" {
		return card.Backend
	}
	if card.Model != "" {
		return card.Model
	}
	return "daemon"
}
