package fs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
)

// DaemonLedgerEntry is a single per-call token_ledger.jsonl line from a
// daemon run directory, tagged with the daemon's identity so the kanban
// detail view can show which daemon each call belongs to. The embedded
// LedgerEntry carries the token/model/endpoint fields; RunID/Handle/State
// come from the run's daemon.json (or the run directory name when the
// identity card is missing).
type DaemonLedgerEntry struct {
	LedgerEntry
	RunID  string // daemons/<run_id> directory name
	Handle string // daemon.json "handle" (e.g. "em-1"); empty when no card
	State  string // daemon.json "state" (running/done/failed/...); empty when no card
}

// daemonCard is the typed subset of daemon.json fields the daemon-ledger
// path needs: identity for tagging and attribution, plus the backend/preset
// fields for fallback provider derivation when a run has no per-call ledger.
// CLITokens and Tokens carry the typed token snapshot so DaemonLedgerSummary
// reads daemon.json exactly once per run.
type daemonCard struct {
	Handle         string                  `json:"handle"`
	RunID          string                  `json:"run_id"`
	State          string                  `json:"state"`
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

// DaemonLedgerSummary is the single fs traversal that returns both
// provider/backend-aggregated token totals and the most-recent tagged
// per-call rows across all daemon run directories.  The pre-existing
// DaemonRecentLedger wrapper returns only the recent-rows half; the kanban
// detail pane calls DaemonLedgerSummary once and fans out both results.
//
// Per-run ledgers are authoritative; when a run has no valid per-call
// ledger entries (CLI backends), the function falls back to daemon.json
// cli_tokens → tokens snapshots and attributes them with
// daemonFallbackProvider (preset_provider → non-lingtai backend →
// preset_model/model derivation → raw backend/model → "daemon").
func DaemonLedgerSummary(agentDir string, recentN int) (map[string]TokenTotals, []DaemonLedgerEntry) {
	daemonDir := filepath.Join(agentDir, "daemons")
	dirEntries, err := os.ReadDir(daemonDir)
	if err != nil {
		return nil, nil
	}

	byProvider := map[string]TokenTotals{}
	var all []DaemonLedgerEntry

	for _, de := range dirEntries {
		if !de.IsDir() {
			continue
		}
		runID := de.Name()
		runDir := filepath.Join(daemonDir, runID)

		cardPath := filepath.Join(runDir, "daemon.json")
		card := readDaemonCard(cardPath)
		if card.RunID == "" {
			card.RunID = runID
		}

		ledgerPath := filepath.Join(runDir, "logs", "token_ledger.jsonl")
		entries := readLedgerEntries(ledgerPath)

		if len(entries) > 0 {
			for _, e := range entries {
				provider := DeriveLedgerProvider(e.Endpoint, e.Model)
				t := byProvider[provider]
				t.Input += e.Input
				t.Output += e.Output
				t.Thinking += e.Thinking
				t.Cached += e.Cached
				t.APICalls++
				byProvider[provider] = t

				all = append(all, DaemonLedgerEntry{
					LedgerEntry: e,
					RunID:       card.RunID,
					Handle:      card.Handle,
					State:       card.State,
				})
			}
			continue
		}

		// No valid ledger entries — fall back to the non-zero cli_tokens
		// snapshot, then legacy tokens, from the already-parsed card.
		var fbInput, fbOutput, fbThinking, fbCached, fbCalls int64
		if cli := card.CLITokens; cli != nil &&
			(cli.Input+cli.Output+cli.Thinking+cli.Cached != 0 || cli.Calls != 0) {
			fbInput = cli.Input
			fbOutput = cli.Output
			fbThinking = cli.Thinking
			fbCached = cli.Cached
			fbCalls = cli.Calls
		} else if legacy := card.Tokens; legacy != nil {
			fbInput = legacy.Input
			fbOutput = legacy.Output
			fbThinking = legacy.Thinking
			fbCached = legacy.Cached
			// Legacy tokens block has no calls field.
		}
		if fbInput+fbOutput+fbThinking+fbCached == 0 && fbCalls == 0 {
			continue
		}
		provider := daemonFallbackProvider(card)
		t := byProvider[provider]
		t.Input += fbInput
		t.Output += fbOutput
		t.Thinking += fbThinking
		t.Cached += fbCached
		t.APICalls += fbCalls
		byProvider[provider] = t
	}

	// Global newest-first sort by ts.
	sort.SliceStable(all, func(i, j int) bool {
		return all[i].TS > all[j].TS
	})

	if recentN > 0 && len(all) > recentN {
		all = all[:recentN]
	}
	return byProvider, all
}

// DaemonRecentLedger returns the most-recent recentN daemon per-call ledger
// entries across all daemon run directories, newest first.  Convenience
// wrapper around DaemonLedgerSummary.
func DaemonRecentLedger(agentDir string, recentN int) []DaemonLedgerEntry {
	_, recent := DaemonLedgerSummary(agentDir, recentN)
	return recent
}

// readDaemonCard reads the typed subset of daemon.json.  Missing or
// malformed files yield a zero card — the caller fills run_id from the
// directory name.
func readDaemonCard(path string) daemonCard {
	var card daemonCard
	data, err := os.ReadFile(path)
	if err != nil {
		return card
	}
	json.Unmarshal(data, &card)
	return card
}

// readLedgerEntries parses every well-formed line of a token_ledger.jsonl
// file into LedgerEntry values (file order preserved).  Missing file or
// malformed lines are skipped silently.
func readLedgerEntries(path string) []LedgerEntry {
	var out []LedgerEntry
	_ = forEachJSONLLine(path, func(line []byte) {
		var e LedgerEntry
		if err := json.Unmarshal(line, &e); err != nil {
			return
		}
		out = append(out, e)
	})
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
