package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Codex pool (load-balancing) storage.
//
// The kernel's `codex-pool` provider reads a NON-SECRET pool file that lists the
// Codex OAuth token files to load-balance across, each with an integer weight.
// The pool file is the single source of truth for load balancing — presets do
// NOT encode weights, and enabling the pool never rewrites saved presets. This
// file owns loading/saving that pool file; the token files themselves stay put
// (legacy ~/.lingtai-tui/codex-auth.json and per-account
// ~/.lingtai-tui/codex-auth/<slug>.json) and are never read here.
//
// Contract (must match the kernel):
//
//	Default path:
//	  $LINGTAI_TUI_DIR/codex-auth-pool.json  when LINGTAI_TUI_DIR is set;
//	  otherwise ~/.lingtai-tui/codex-auth-pool.json.
//
//	Schema (v1, flat):
//	  {"version": 1, "accounts": [{"path": "codex-auth.json", "weight": 1}, ...]}
//
//	Schema (v2, model-classified — hand-authored):
//	  {"version": 2, "models": {"<exact model>": [{"path": ..., "weight": ...}, ...], ...}}
//
//	Rules:
//	  - `path` is TUI-dir-relative for token files under the TUI dir; the legacy
//	    file serializes as "codex-auth.json", per-account as
//	    "codex-auth/<slug>.json". Files outside the TUI dir fall back to a
//	    "~/"-prefixed or absolute ref.
//	  - Store only paths/refs and integer weights — NEVER token contents.
//	  - Weight 0 means the account is disabled (present but not balanced onto).
//	  - A `models` map classifies the pool by EXACT model string: the kernel
//	    selects accounts only inside the current model's category and ignores
//	    `accounts` entirely when `models` is present. PRESENCE of the dict is
//	    what classifies — an empty `{}` still classifies (every model then
//	    falls back to the legacy token); `models: null` is not a dict and
//	    leaves the pool flat. The TUI has no category editor yet — it
//	    round-trips `models` losslessly, stamps version 2, and REFUSES flat
//	    +/-/0 weight edits on a classified pool (errCodexPoolModelClassified)
//	    so it can never destroy a hand-authored classification or write an
//	    entry the kernel would ignore.
//
// Nothing here reads, logs, or writes token material.

// codexPoolFileName is the non-secret pool file's basename, shared with the
// kernel's reader.
const codexPoolFileName = "codex-auth-pool.json"

// codexPoolVersion is the schema version written into a flat (v1) pool file.
const codexPoolVersion = 1

// codexPoolVersionModels is the schema version stamped when the pool is
// model-classified (carries a top-level `models` map — even an empty one).
const codexPoolVersionModels = 2

// errCodexPoolModelClassified refuses flat weight edits on a model-classified
// pool: the kernel ignores `accounts` when `models` is present, so the edit
// would silently do nothing at runtime, and rewriting the file risks mangling
// the hand-authored classification. The file must be edited by hand until a
// category-aware editor exists.
var errCodexPoolModelClassified = errors.New(
	"codex-auth-pool.json is model-classified (has a models map); flat weight edits are disabled — edit the file by hand")

// codexPoolAccount is one balanced account: a stable ref to its token file plus
// an integer weight. Weight 0 disables the account without dropping it.
type codexPoolAccount struct {
	Path    string `json:"path"`
	Weight  int    `json:"weight"`
	Enabled *bool  `json:"enabled,omitempty"`
}

// codexPool is the on-disk pool file shape. Models (v2) classifies accounts by
// EXACT model string; when present it is the single source of truth and the
// flat Accounts list is ignored by the kernel. The kernel keys on the dict's
// PRESENCE, not its size — `models: {}` classifies, `models: null` does not —
// so Models is a pointer: nil means the key was absent or null (flat v1),
// non-nil means a dict was present (v2), even an empty one. That presence bit
// plus keeping both fields typed is what makes load→save lossless for
// hand-authored v2 files.
type codexPool struct {
	Version  int                            `json:"version"`
	Accounts []codexPoolAccount             `json:"accounts"`
	Models   *map[string][]codexPoolAccount `json:"models,omitempty"`
}

// codexPoolPath returns the absolute path of the pool file. LINGTAI_TUI_DIR
// wins when set (matching the kernel reader); otherwise the file lives directly
// under globalDir (~/.lingtai-tui). globalDir is only consulted as the fallback
// base, so the two readers agree on the location.
func codexPoolPath(globalDir string) string {
	if base := strings.TrimSpace(os.Getenv("LINGTAI_TUI_DIR")); base != "" {
		return filepath.Join(base, codexPoolFileName)
	}
	return filepath.Join(globalDir, codexPoolFileName)
}

// codexPoolBaseDir returns the directory the pool file lives in — the same base
// that relative `path` entries resolve against. LINGTAI_TUI_DIR wins when set,
// mirroring codexPoolPath, so refs written here round-trip through the kernel.
func codexPoolBaseDir(globalDir string) string {
	if base := strings.TrimSpace(os.Getenv("LINGTAI_TUI_DIR")); base != "" {
		return base
	}
	return globalDir
}

// codexPoolRefForPath maps an absolute token-file path to the stable ref stored
// in the pool file. Unlike codexAuthRefForPath (which maps the legacy file to ""
// to preserve preset fallback semantics), the pool wants an EXPLICIT, stable
// relative ref for every account:
//   - a token file under the TUI dir → its TUI-dir-relative path
//     ("codex-auth.json" for the legacy file, "codex-auth/<slug>.json" per-account);
//   - a file under the user's home but outside the TUI dir → "~/"-prefixed;
//   - anything else → the absolute path unchanged.
func codexPoolRefForPath(globalDir, absPath string) string {
	if absPath == "" {
		return ""
	}
	base := codexPoolBaseDir(globalDir)
	if rel, err := filepath.Rel(base, absPath); err == nil && rel != "" &&
		!strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		return filepath.ToSlash(rel)
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return absPath
}

// resolveCodexPoolRef expands a pool `path` entry to an absolute path — the
// inverse of codexPoolRefForPath. A bare relative ref resolves under the pool
// base dir (so "codex-auth.json" lands on the legacy file); "~/" and absolute
// refs are honored. Empty refs resolve to "" so callers can skip them.
func resolveCodexPoolRef(globalDir, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	if ref == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return ref
	}
	if strings.HasPrefix(ref, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, ref[2:])
		}
		return ref
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(codexPoolBaseDir(globalDir), ref)
}

// loadCodexPool reads the pool file. A missing file is NOT an error: it returns
// an empty pool (Version defaulted, no accounts) so callers can treat "no pool
// yet" and "empty pool" identically. A malformed file returns the parse error so
// the caller can surface it rather than silently clobbering the user's edits.
func loadCodexPool(globalDir string) (codexPool, error) {
	path := codexPoolPath(globalDir)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return codexPool{Version: codexPoolVersion}, nil
		}
		return codexPool{}, err
	}
	var raw struct {
		Version  int                         `json:"version"`
		Accounts json.RawMessage             `json:"accounts"`
		Models   *map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return codexPool{}, err
	}
	pool := codexPool{Version: raw.Version}
	var decodeErr error
	if len(raw.Accounts) > 0 && string(raw.Accounts) != "null" {
		pool.Accounts, decodeErr = decodeCodexPoolAccounts(raw.Accounts)
		if decodeErr != nil {
			return codexPool{}, decodeErr
		}
	}
	if raw.Models != nil {
		pool.Models = &map[string][]codexPoolAccount{}
		for model, entries := range *raw.Models {
			decoded, decodeErr := decodeCodexPoolAccounts(entries)
			if decodeErr != nil {
				continue
			}
			(*pool.Models)[model] = decoded
		}
	}
	if pool.Version == 0 {
		pool.Version = codexPoolVersion
	}
	return pool, nil
}

func decodeCodexPoolAccounts(raw json.RawMessage) ([]codexPoolAccount, error) {
	var entries []json.RawMessage
	if err := json.Unmarshal(raw, &entries); err != nil {
		return nil, err
	}
	accounts := make([]codexPoolAccount, 0, len(entries))
	for _, entry := range entries {
		var account codexPoolAccount
		if len(entry) == 0 || entry[0] != '{' || json.Unmarshal(entry, &account) != nil {
			continue
		}
		accounts = append(accounts, account)
	}
	return accounts, nil
}

// saveCodexPool writes the pool file (version stamped, parent created). The file
// is non-secret — it holds only refs and weights — so it is written 0644, unlike
// the 0600 token files. Callers build the accounts list from
// codexPoolRefForPath so only stable relative refs land on disk. A pool whose
// `models` map was present (even empty) is stamped version 2 (model-classified)
// and the map is written back — dropping an empty `{}` would flip the kernel
// from classified to flat. Flat pools stay version 1, byte-identical to today.
func saveCodexPool(globalDir string, pool codexPool) error {
	pool.Version = codexPoolVersion
	if pool.Models != nil {
		pool.Version = codexPoolVersionModels
	}
	if pool.Accounts == nil {
		pool.Accounts = []codexPoolAccount{}
	}
	path := codexPoolPath(globalDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pool, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// codexPoolWeights returns a map from resolved ABSOLUTE token-file path to the
// weight recorded in the pool file's FLAT accounts list. Entries whose ref
// can't be resolved are skipped. Used by the credentials UI to look up each
// Codex account row's weight without re-parsing the pool per row. A missing
// pool file yields an empty map (callers apply the default-weight policy on
// top). On a model-classified pool the kernel ignores the flat list, so UI
// callers must gate on codexPoolModelInfo before rendering these weights.
func codexPoolWeights(globalDir string) map[string]int {
	pool, err := loadCodexPool(globalDir)
	if err != nil {
		return map[string]int{}
	}
	out := make(map[string]int, len(pool.Accounts))
	for _, acct := range pool.Accounts {
		abs := resolveCodexPoolRef(globalDir, acct.Path)
		if abs == "" {
			continue
		}
		out[abs] = acct.Weight
	}
	return out
}

// codexPoolAccountsEligible reports whether one account is selectable by the
// kernel: positive weight plus a valid token file.
func codexPoolAccountsEligible(globalDir string, accounts []codexPoolAccount) bool {
	if len(codexPoolAccountsRepresentable(accounts)) == 0 {
		return codexPoolLegacyFallbackValid(globalDir)
	}
	for _, acct := range accounts {
		if acct.Enabled != nil && !*acct.Enabled || acct.Weight <= 0 || strings.TrimSpace(acct.Path) == "" {
			continue
		}
		abs := resolveCodexPoolRef(globalDir, acct.Path)
		if abs != "" && codexAuthPathValid(abs) {
			return true
		}
	}
	return false
}

func codexPoolAccountsRepresentable(accounts []codexPoolAccount) []codexPoolAccount {
	eligible := make([]codexPoolAccount, 0, len(accounts))
	for _, acct := range accounts {
		if (acct.Enabled == nil || *acct.Enabled) && acct.Weight > 0 && strings.TrimSpace(acct.Path) != "" {
			eligible = append(eligible, acct)
		}
	}
	return eligible
}

func codexPoolLegacyFallbackValid(globalDir string) bool {
	return codexAuthPathValid(filepath.Join(codexPoolBaseDir(globalDir), "codex-auth.json"))
}

// codexPoolEligible mirrors the runtime distinction between an empty pool and
// a nonempty pool whose members are unusable. Only an absent/empty applicable
// category may use the valid legacy fallback; a nonempty unusable category
// remains ineligible and fails closed.
func codexPoolEligible(globalDir, model string) bool {
	poolPath := codexPoolPath(globalDir)
	payload, err := os.Stat(poolPath)
	if os.IsNotExist(err) {
		return codexPoolLegacyFallbackValid(globalDir)
	}
	if err != nil || payload.IsDir() {
		return false
	}
	p, err := loadCodexPool(globalDir)
	if err != nil {
		return false
	}
	if p.Models == nil {
		if len(p.Accounts) == 0 {
			return codexPoolLegacyFallbackValid(globalDir)
		}
		return codexPoolAccountsEligible(globalDir, p.Accounts)
	}
	if model == "" {
		return false
	}
	accounts, ok := (*p.Models)[model]
	if !ok || len(accounts) == 0 {
		return codexPoolLegacyFallbackValid(globalDir)
	}
	return codexPoolAccountsEligible(globalDir, accounts)
}

// codexPoolEligibilityFacts returns the same facts for the preset health
// renderer. A non-nil classified map preserves the pool's classification bit.
func codexPoolEligibilityFacts(globalDir string) (flat bool, classified map[string]bool, fallback bool) {
	poolPath := codexPoolPath(globalDir)
	if _, err := os.Stat(poolPath); os.IsNotExist(err) {
		return codexPoolEligible(globalDir, ""), nil, codexPoolLegacyFallbackValid(globalDir)
	} else if err != nil {
		return false, nil, false
	}
	p, err := loadCodexPool(globalDir)
	if err != nil {
		return false, nil, false
	}
	if p.Models == nil {
		return codexPoolEligible(globalDir, ""), nil, false
	}
	classified = make(map[string]bool, len(*p.Models))
	for model := range *p.Models {
		classified[model] = codexPoolEligible(globalDir, model)
	}
	return false, classified, codexPoolLegacyFallbackValid(globalDir)
}

// codexPoolModelInfo reports whether the pool file is model-classified (has a
// top-level `models` map — presence, not size, is the classification bit, so
// an empty `{}` reports true/0) and how many model categories it holds.
// A missing or malformed file reports false/0 — corruption is surfaced
// separately by codexPoolFileCorrupt. The credentials UI uses this to render
// the classified state instead of flat memberships the kernel would not honor.
func codexPoolModelInfo(globalDir string) (classified bool, modelCount int) {
	pool, err := loadCodexPool(globalDir)
	if err != nil || pool.Models == nil {
		return false, 0
	}
	return true, len(*pool.Models)
}

// codexPoolFileCorrupt reports whether the pool file exists but fails to parse.
// A missing file is NOT corrupt (returns false) — that is the normal "no pool
// yet" state. Used by the credentials UI to warn the user that a malformed pool
// file is being ignored for display (and won't be overwritten), instead of
// silently rendering every account as "not in pool".
func codexPoolFileCorrupt(globalDir string) bool {
	if _, err := os.Stat(codexPoolPath(globalDir)); err != nil {
		return false // missing (or unstattable) — not a parse-corruption case
	}
	_, err := loadCodexPool(globalDir)
	return err != nil
}

// codexPoolMembership reports an account's real pool state for absPath: inPool
// is true only when the pool file actually records the account, and weight is
// its stored weight (meaningful only when inPool). An account absent from the
// pool file is NOT in the pool — the UI must say so rather than inventing a
// default weight, because the kernel runtime has no such entry either and would
// fall back to the legacy single token. This truthfulness is the whole point of
// GLM's display/runtime-mismatch fix: never render a phantom "pool weight: 1"
// for an account the pool doesn't contain.
func codexPoolMembership(weights map[string]int, absPath string) (inPool bool, weight int) {
	w, ok := weights[absPath]
	return ok, w
}

// setCodexPoolWeight records weight for the token file at absPath and persists
// the pool file, creating it on first edit (the lazy-write policy). The account
// is added if absent, updated in place if present. Other accounts and their
// weights are preserved in their original order.
//
// Entries are matched by RESOLVED ABSOLUTE token path, not by ref string, so a
// weight edit lands on the right account even when the stored ref used a
// different style (relative vs "~/"/absolute) — e.g. because LINGTAI_TUI_DIR
// changed between edits. To stay robust against a pool that already contains
// more than one entry resolving to the same token (a duplicate produced before
// this fix), the save collapses all such entries into a single account: the
// first occurrence keeps its slot but adopts the current output ref style
// (codexPoolRefForPath) and the new weight; any later duplicates are dropped.
// The result always holds at most one account per resolved absolute token.
func setCodexPoolWeight(globalDir, absPath string, weight int) error {
	if weight < 0 {
		weight = 0
	}
	pool, err := loadCodexPool(globalDir)
	if err != nil {
		// A malformed pool file must not be silently overwritten — surface it.
		return err
	}
	if pool.Models != nil {
		// A model-classified pool (any `models` dict, even empty) has no flat
		// weights to edit: refuse before touching anything so the file bytes
		// stay exactly as the operator wrote them.
		return errCodexPoolModelClassified
	}

	ref := codexPoolRefForPath(globalDir, absPath)
	deduped := make([]codexPoolAccount, 0, len(pool.Accounts))
	applied := false
	for _, acct := range pool.Accounts {
		if resolveCodexPoolRef(globalDir, acct.Path) == absPath {
			if applied {
				// A second (or later) entry for the same token — drop it so the
				// pool holds a single account for this abs path.
				continue
			}
			// First match: normalize its ref style and set the new weight.
			deduped = append(deduped, codexPoolAccount{Path: ref, Weight: weight})
			applied = true
			continue
		}
		deduped = append(deduped, acct)
	}
	if !applied {
		deduped = append(deduped, codexPoolAccount{Path: ref, Weight: weight})
	}
	pool.Accounts = deduped
	return saveCodexPool(globalDir, pool)
}
