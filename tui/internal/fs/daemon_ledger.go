package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"
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
// once per run.
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

// DaemonDetailSnapshot is the daemon data shown by Ctrl+D. Token totals and
// recent rows cover the newest ScannedRuns run directories, while
// Counts.Total is the cheap directory-entry total across all run directories.
// Running/Queued/Done/Failed are classified only inside the scanned window; the
// UI intentionally renders Running and Total and labels token totals with the
// window size.
type DaemonDetailSnapshot struct {
	ByProvider   map[string]TokenTotals
	Recent       []DaemonLedgerEntry
	Counts       DaemonCounts
	ScannedRuns  int
	TotalRuns    int
	TerminalRuns int // total minus scanned running and queued runs
	// Kanban-only recent-dispatch evidence. Legacy directory-based callers
	// leave these zero-valued.
	DispatchRead BoundedReadStats
	TokenReads   []BoundedReadStats
	RunIDs       []string
	WindowState  string // available, empty, recent, partial, malformed, or unavailable
}

// ReadDaemonDetailSnapshot reads only the newest recentRunN run directories
// (all runs when recentRunN <= 0) and keeps at most recentCallN globally
// newest ledger rows. It is a direct, stateless read: one ReadDir ordered by
// run-directory mtime, then daemon.json + token_ledger.jsonl per selected run.
// No caching or live-run retention — Ctrl+D is a point-in-time diagnostic
// refreshed on open, explicit Ctrl+R, or agent change.
func ReadDaemonDetailSnapshot(agentDir string, recentRunN, recentCallN int) DaemonDetailSnapshot {
	daemonDir := filepath.Join(agentDir, "daemons")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		return DaemonDetailSnapshot{ByProvider: map[string]TokenTotals{}}
	}

	// Order run directories by mtime, newest first, so the scan opens only the
	// most recently active runs.
	type daemonRunRef struct {
		name    string
		modTime int64
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
	sort.SliceStable(runs, func(i, j int) bool {
		if runs[i].modTime != runs[j].modTime {
			return runs[i].modTime > runs[j].modTime
		}
		return runs[i].name > runs[j].name
	})
	totalRuns := len(runs)
	if recentRunN > 0 && len(runs) > recentRunN {
		runs = runs[:recentRunN]
	}

	result := DaemonDetailSnapshot{
		ByProvider:  map[string]TokenTotals{},
		ScannedRuns: len(runs),
		TotalRuns:   totalRuns,
	}
	result.Counts.Total = totalRuns

	var all []DaemonLedgerEntry
	for _, run := range runs {
		runDir := filepath.Join(daemonDir, run.name)
		card := readDaemonCard(filepath.Join(runDir, "daemon.json"))
		if card.RunID == "" {
			card.RunID = run.name
		}
		addDaemonDetailState(&result.Counts, card)

		ledgerEntries := readLedgerEntries(filepath.Join(runDir, "logs", "token_ledger.jsonl"))
		if len(ledgerEntries) > 0 {
			for _, e := range ledgerEntries {
				provider := DeriveLedgerProvider(e.Endpoint, e.Model)
				t := result.ByProvider[provider]
				t.Input += e.Input
				t.Output += e.Output
				t.Thinking += e.Thinking
				t.Cached += e.Cached
				t.APICalls++
				result.ByProvider[provider] = t

				all = append(all, DaemonLedgerEntry{
					LedgerEntry: e,
					RunID:       card.RunID,
					Handle:      card.Handle,
					State:       card.State,
					Backend:     card.Backend,
				})
			}
			continue
		}
		addDaemonFallbackTotals(result.ByProvider, card)
	}

	// Global newest-first sort by ts, then keep the newest recentCallN rows.
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].TS > all[j].TS
	})
	if recentCallN > 0 && len(all) > recentCallN {
		all = all[:recentCallN]
	}
	result.Recent = all

	result.TerminalRuns = result.TotalRuns - result.Counts.Running - result.Counts.Queued
	if result.TerminalRuns < 0 {
		result.TerminalRuns = 0
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

// DaemonRecentLedgerSummary aggregates token usage and API-call counts across
// only the daemon runs whose run directory was modified within daemonListWindow
// (10 minutes), mirroring CountDaemons' recency gate. The mail async-work row
// and any other per-second consumer call this so token ledgers are never opened
// for stale historical runs: one ReadDir yields the directory metadata, and only
// in-window runs get their daemon.json / token_ledger.jsonl read. Per-run totals
// follow ReadDaemonDetailSnapshot: the per-call ledger when present, else the
// daemon.json cli_tokens/legacy snapshot (CLI backends write no ledger).
func DaemonRecentLedgerSummary(agentDir string) TokenTotals {
	daemonDir := filepath.Join(agentDir, "daemons")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		return TokenTotals{}
	}

	now := time.Now()
	var totals TokenTotals
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			continue
		}
		if !withinDaemonListWindow(info.ModTime(), now) {
			continue
		}
		runDir := filepath.Join(daemonDir, entry.Name())
		card := readDaemonCard(filepath.Join(runDir, "daemon.json"))
		if ledger := readLedgerEntries(filepath.Join(runDir, "logs", "token_ledger.jsonl")); len(ledger) > 0 {
			for _, e := range ledger {
				totals.Input += e.Input
				totals.Output += e.Output
				totals.Thinking += e.Thinking
				totals.Cached += e.Cached
				totals.APICalls++
			}
			continue
		}
		byProvider := map[string]TokenTotals{}
		addDaemonFallbackTotals(byProvider, card)
		for _, t := range byProvider {
			totals.Input += t.Input
			totals.Output += t.Output
			totals.Thinking += t.Thinking
			totals.Cached += t.Cached
			totals.APICalls += t.APICalls
		}
	}
	return totals
}

// RecentLingtaiDaemonModels returns the raw daemon.json model strings for
// backend="lingtai" runs in the same fresh daemonListWindow as CountDaemons and
// the Telegram-aligned home async row. It deliberately reads only daemon cards:
// historical run directories are skipped before opening their files, non-lingtai
// backends are excluded, and no model is derived from presets or token ledgers.
func RecentLingtaiDaemonModels(agentDir string) []string {
	daemonDir := filepath.Join(agentDir, "daemons")
	entries, err := os.ReadDir(daemonDir)
	if err != nil {
		return nil
	}

	now := time.Now()
	var models []string
	for _, entry := range entries {
		var cardPath string
		if entry.IsDir() {
			cardPath = filepath.Join(daemonDir, entry.Name(), "daemon.json")
		} else if entry.Name() == "daemon.json" {
			cardPath = filepath.Join(daemonDir, entry.Name())
		} else {
			continue
		}
		info, err := entry.Info()
		if err != nil || !withinDaemonListWindow(info.ModTime(), now) {
			continue
		}
		card := readDaemonCard(cardPath)
		if card.Backend == "lingtai" && card.Model != "" {
			models = append(models, card.Model)
		}
	}
	return models
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

// readLedgerEntries parses every well-formed object line of a token_ledger.jsonl
// file into LedgerEntry values (file order preserved). Missing file or
// malformed lines are skipped silently.
func readLedgerEntries(path string) []LedgerEntry {
	var out []LedgerEntry
	_ = forEachJSONLLine(path, func(line []byte) {
		var e LedgerEntry
		if line[0] != '{' || json.Unmarshal(line, &e) != nil {
			return
		}
		out = append(out, e)
	})
	return out
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
