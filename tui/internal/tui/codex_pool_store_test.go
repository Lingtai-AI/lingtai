package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCodexPoolPath_DefaultUnderGlobalDir verifies the pool file lands directly
// under globalDir when LINGTAI_TUI_DIR is unset.
func TestCodexPoolPath_DefaultUnderGlobalDir(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	got := codexPoolPath(dir)
	want := filepath.Join(dir, codexPoolFileName)
	if got != want {
		t.Fatalf("pool path = %q, want %q", got, want)
	}
}

// TestCodexPoolPath_HonorsEnvOverride verifies LINGTAI_TUI_DIR wins over
// globalDir, matching the kernel reader's precedence.
func TestCodexPoolPath_HonorsEnvOverride(t *testing.T) {
	dir := t.TempDir()
	override := t.TempDir()
	t.Setenv("LINGTAI_TUI_DIR", override)
	got := codexPoolPath(dir)
	want := filepath.Join(override, codexPoolFileName)
	if got != want {
		t.Fatalf("pool path with env override = %q, want %q", got, want)
	}
}

// TestCodexPoolRefForPath_RelativeRefs verifies the legacy file serializes as
// "codex-auth.json" and per-account files as "codex-auth/<slug>.json" — stable
// TUI-dir-relative refs, never absolute and never token contents.
func TestCodexPoolRefForPath_RelativeRefs(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()

	legacy := legacyCodexAuthPath(dir)
	if ref := codexPoolRefForPath(dir, legacy); ref != "codex-auth.json" {
		t.Errorf("legacy pool ref = %q, want %q", ref, "codex-auth.json")
	}

	perAccount := filepath.Join(dir, codexAuthSubdir, "work.json")
	if ref := codexPoolRefForPath(dir, perAccount); ref != "codex-auth/work.json" {
		t.Errorf("per-account pool ref = %q, want %q", ref, "codex-auth/work.json")
	}
}

// TestCodexPoolRefRoundTrip verifies resolveCodexPoolRef inverts
// codexPoolRefForPath for both the legacy and per-account cases.
func TestCodexPoolRefRoundTrip(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	for _, abs := range []string{
		legacyCodexAuthPath(dir),
		filepath.Join(dir, codexAuthSubdir, "work.json"),
	} {
		ref := codexPoolRefForPath(dir, abs)
		if got := resolveCodexPoolRef(dir, ref); got != abs {
			t.Errorf("round-trip for %q: ref=%q resolved back to %q", abs, ref, got)
		}
	}
}

// TestSaveCodexPool_WritesOnlyRefsAndWeights is the core secrecy guarantee: the
// pool file holds version + relative refs + integer weights and NOTHING that
// looks like token material, even when the referenced token files contain
// secrets.
func TestSaveCodexPool_WritesOnlyRefsAndWeights(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()

	// Seed real-shaped (fake) token files so their contents exist on disk; the
	// pool file must not pick any of it up.
	writeStubCodexToken(t, legacyCodexAuthPath(dir), "primary@example.com")
	writeStubCodexToken(t, filepath.Join(dir, codexAuthSubdir, "work.json"), "work@example.com")

	pool := codexPool{
		Accounts: []codexPoolAccount{
			{Path: codexPoolRefForPath(dir, legacyCodexAuthPath(dir)), Weight: 1},
			{Path: codexPoolRefForPath(dir, filepath.Join(dir, codexAuthSubdir, "work.json")), Weight: 2},
		},
	}
	if err := saveCodexPool(dir, pool); err != nil {
		t.Fatalf("saveCodexPool: %v", err)
	}

	raw, err := os.ReadFile(codexPoolPath(dir))
	if err != nil {
		t.Fatalf("read pool file: %v", err)
	}
	body := string(raw)

	// No token material may appear.
	for _, secret := range []string{"stub-access", "stub-refresh", "access_token", "refresh_token"} {
		if strings.Contains(body, secret) {
			t.Errorf("pool file leaked secret-shaped content %q; body=%s", secret, body)
		}
	}
	// Refs must be the stable relative form, not absolute paths.
	if strings.Contains(body, dir) {
		t.Errorf("pool file contains an absolute path; body=%s", body)
	}
	if !strings.Contains(body, "codex-auth.json") || !strings.Contains(body, "codex-auth/work.json") {
		t.Errorf("pool file missing expected relative refs; body=%s", body)
	}

	// Version is stamped.
	var reloaded codexPool
	if err := json.Unmarshal(raw, &reloaded); err != nil {
		t.Fatalf("reparse pool: %v", err)
	}
	if reloaded.Version != codexPoolVersion {
		t.Errorf("version = %d, want %d", reloaded.Version, codexPoolVersion)
	}
	if len(reloaded.Accounts) != 2 {
		t.Fatalf("accounts = %d, want 2", len(reloaded.Accounts))
	}
}

// TestLoadCodexPool_MissingFileIsEmpty verifies a missing pool file is not an
// error — it yields an empty (versioned) pool so "no pool yet" reads cleanly.
func TestLoadCodexPool_MissingFileIsEmpty(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	pool, err := loadCodexPool(dir)
	if err != nil {
		t.Fatalf("missing pool file must not error; got %v", err)
	}
	if len(pool.Accounts) != 0 {
		t.Errorf("missing pool should have no accounts; got %d", len(pool.Accounts))
	}
	if pool.Version != codexPoolVersion {
		t.Errorf("missing pool version = %d, want %d", pool.Version, codexPoolVersion)
	}
}

// TestLoadCodexPool_MalformedFileErrors verifies a corrupt pool file surfaces an
// error rather than silently resetting the user's weights.
func TestLoadCodexPool_MalformedFileErrors(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	if err := os.WriteFile(codexPoolPath(dir), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed malformed pool: %v", err)
	}
	if _, err := loadCodexPool(dir); err == nil {
		t.Fatal("malformed pool file should return a parse error")
	}
}

// TestCodexPoolMembership_ReportsRealState verifies the truthful membership
// lookup: an account recorded in the pool reports inPool=true with its stored
// weight (including an explicit 0 = disabled-in-pool); an account absent from
// the pool reports inPool=false so the UI can render "not in pool" instead of
// inventing a default weight.
func TestCodexPoolMembership_ReportsRealState(t *testing.T) {
	weights := map[string]int{
		"/x/recorded.json":  3,
		"/x/explicit0.json": 0,
	}
	if in, w := codexPoolMembership(weights, "/x/recorded.json"); !in || w != 3 {
		t.Errorf("recorded account: inPool=%v weight=%d, want true/3", in, w)
	}
	if in, w := codexPoolMembership(weights, "/x/explicit0.json"); !in || w != 0 {
		t.Errorf("disabled-in-pool account: inPool=%v weight=%d, want true/0", in, w)
	}
	if in, _ := codexPoolMembership(weights, "/x/absent.json"); in {
		t.Error("an account absent from the pool must report inPool=false")
	}
}

// TestSetCodexPoolWeight_LazyCreateAndUpdate verifies the lazy-write policy: the
// pool file is created on first edit, updated in place on subsequent edits, and
// unrelated accounts survive.
func TestSetCodexPoolWeight_LazyCreateAndUpdate(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	poolFile := codexPoolPath(dir)

	if _, err := os.Stat(poolFile); !os.IsNotExist(err) {
		t.Fatalf("precondition: pool file should not exist yet; stat err=%v", err)
	}

	work := filepath.Join(dir, codexAuthSubdir, "work.json")
	home := filepath.Join(dir, codexAuthSubdir, "home.json")

	// First edit creates the file.
	if err := setCodexPoolWeight(dir, work, 2); err != nil {
		t.Fatalf("set work weight: %v", err)
	}
	if _, err := os.Stat(poolFile); err != nil {
		t.Fatalf("pool file should be created on first edit; stat err=%v", err)
	}

	// Second account is added, not replacing the first.
	if err := setCodexPoolWeight(dir, home, 5); err != nil {
		t.Fatalf("set home weight: %v", err)
	}
	// Update the first in place.
	if err := setCodexPoolWeight(dir, work, 4); err != nil {
		t.Fatalf("update work weight: %v", err)
	}

	weights := codexPoolWeights(dir)
	if weights[work] != 4 {
		t.Errorf("work weight = %d, want 4", weights[work])
	}
	if weights[home] != 5 {
		t.Errorf("home weight = %d, want 5", weights[home])
	}
	if len(weights) != 2 {
		t.Errorf("expected exactly 2 pool accounts; got %d (%v)", len(weights), weights)
	}

	// The stored refs must be the relative form.
	pool, err := loadCodexPool(dir)
	if err != nil {
		t.Fatalf("reload pool: %v", err)
	}
	for _, acct := range pool.Accounts {
		if filepath.IsAbs(acct.Path) || strings.HasPrefix(acct.Path, "~") {
			t.Errorf("pool ref should be TUI-dir-relative; got %q", acct.Path)
		}
	}
}

// TestSetCodexPoolWeight_ClampsNegative verifies weights never go below 0.
func TestSetCodexPoolWeight_ClampsNegative(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	work := filepath.Join(dir, codexAuthSubdir, "work.json")
	if err := setCodexPoolWeight(dir, work, -3); err != nil {
		t.Fatalf("set negative weight: %v", err)
	}
	if w := codexPoolWeights(dir)[work]; w != 0 {
		t.Errorf("negative weight should clamp to 0; got %d", w)
	}
}

// TestSetCodexPoolWeight_DedupesSameAbsToken guards N2: a pool that already
// holds two entries resolving to the SAME absolute token (e.g. one relative ref
// and one absolute ref left behind after LINGTAI_TUI_DIR changed between edits)
// must collapse to a single account on the next weight edit — not update one and
// leave the stale duplicate distorting the effective weight.
func TestSetCodexPoolWeight_DedupesSameAbsToken(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LINGTAI_TUI_DIR", dir)

	work := filepath.Join(dir, codexAuthSubdir, "work.json")
	other := filepath.Join(dir, codexAuthSubdir, "home.json")

	// Seed a pool with a duplicate for `work` (relative + absolute refs both
	// resolving to `work`) plus an unrelated account. Weights differ so we can
	// tell which survived.
	seeded := codexPool{
		Version: codexPoolVersion,
		Accounts: []codexPoolAccount{
			{Path: "codex-auth/work.json", Weight: 2}, // relative → work
			{Path: other, Weight: 7},                  // unrelated (absolute)
			{Path: work, Weight: 9},                   // absolute → work (duplicate)
		},
	}
	if err := saveCodexPool(dir, seeded); err != nil {
		t.Fatalf("seed pool: %v", err)
	}

	// Precondition: two entries currently resolve to `work`.
	pre, err := loadCodexPool(dir)
	if err != nil {
		t.Fatalf("load seeded pool: %v", err)
	}
	dupes := 0
	for _, a := range pre.Accounts {
		if resolveCodexPoolRef(dir, a.Path) == work {
			dupes++
		}
	}
	if dupes != 2 {
		t.Fatalf("precondition: expected 2 duplicate entries for work; got %d", dupes)
	}

	// Edit the weight for `work` — should collapse the duplicates into one.
	if err := setCodexPoolWeight(dir, work, 5); err != nil {
		t.Fatalf("set work weight: %v", err)
	}

	pool, err := loadCodexPool(dir)
	if err != nil {
		t.Fatalf("reload pool: %v", err)
	}
	matches := 0
	for _, a := range pool.Accounts {
		if resolveCodexPoolRef(dir, a.Path) == work {
			matches++
			if a.Weight != 5 {
				t.Errorf("deduped work weight = %d, want 5", a.Weight)
			}
		}
	}
	if matches != 1 {
		t.Fatalf("expected exactly 1 entry for work after dedup; got %d (%#v)", matches, pool.Accounts)
	}
	// The unrelated account must survive untouched.
	if w := codexPoolWeights(dir)[other]; w != 7 {
		t.Errorf("unrelated account weight = %d, want 7 (preserved)", w)
	}
	// codexPoolWeights (map keyed by abs path) must agree.
	if w := codexPoolWeights(dir)[work]; w != 5 {
		t.Errorf("resolved work weight = %d, want 5", w)
	}
}

// seedModelClassifiedPool writes a hand-authored v2 (model-classified) pool
// file — the shape the kernel's codex-pool provider selects from by exact
// model — and returns its raw bytes for byte-preservation assertions.
func seedModelClassifiedPool(t *testing.T, globalDir string) []byte {
	t.Helper()
	raw := []byte(`{
  "version": 2,
  "models": {
    "gpt-5.6-sol": [
      {"path": "codex-auth/work.json", "weight": 3},
      {"path": "codex-auth.json", "weight": 1}
    ],
    "gpt-5.5": [
      {"path": "codex-auth/backup.json", "weight": 1}
    ]
  }
}`)
	if err := os.MkdirAll(filepath.Dir(codexPoolPath(globalDir)), 0o755); err != nil {
		t.Fatalf("mkdir pool dir: %v", err)
	}
	if err := os.WriteFile(codexPoolPath(globalDir), raw, 0o644); err != nil {
		t.Fatalf("seed v2 pool: %v", err)
	}
	return raw
}

// seedEmptyModelsClassifiedPool writes a hand-authored v2 pool whose `models`
// map is PRESENT BUT EMPTY, with a sibling flat `accounts` list. The kernel
// treats any top-level `models` dict — including `{}` — as classified and
// ignores the sibling `accounts` entirely, so the TUI must classify on
// presence, not non-emptiness. Returns the raw bytes for byte-preservation
// assertions.
func seedEmptyModelsClassifiedPool(t *testing.T, globalDir string) []byte {
	t.Helper()
	raw := []byte(`{
  "version": 2,
  "models": {},
  "accounts": [
    {"path": "codex-auth/work.json", "weight": 3}
  ]
}`)
	if err := os.MkdirAll(filepath.Dir(codexPoolPath(globalDir)), 0o755); err != nil {
		t.Fatalf("mkdir pool dir: %v", err)
	}
	if err := os.WriteFile(codexPoolPath(globalDir), raw, 0o644); err != nil {
		t.Fatalf("seed empty-models v2 pool: %v", err)
	}
	return raw
}

// TestLoadSaveCodexPool_PreservesEmptyModelsMap pins the presence semantics of
// an empty `models` map through load→save: the saved file must still carry
// `"models": {}` (a present, empty JSON object), stamp version 2, and keep the
// sibling flat accounts — never collapse to a flat v1 file, which would flip
// the kernel from "classified, everything falls back" to "flat, accounts win".
// Asserted on the raw JSON so the test is independent of the in-memory
// representation.
func TestLoadSaveCodexPool_PreservesEmptyModelsMap(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	seedEmptyModelsClassifiedPool(t, dir)

	pool, err := loadCodexPool(dir)
	if err != nil {
		t.Fatalf("load empty-models pool: %v", err)
	}
	if err := saveCodexPool(dir, pool); err != nil {
		t.Fatalf("save empty-models pool: %v", err)
	}

	raw, err := os.ReadFile(codexPoolPath(dir))
	if err != nil {
		t.Fatalf("read pool file back: %v", err)
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatalf("reparse saved pool: %v", err)
	}
	models, present := generic["models"]
	if !present {
		t.Fatalf("saved pool dropped the empty models map; body=%s", raw)
	}
	obj, isObj := models.(map[string]any)
	if !isObj {
		t.Fatalf("saved models is not a JSON object; got %T (%v)", models, models)
	}
	if len(obj) != 0 {
		t.Errorf("saved models should stay empty; got %v", obj)
	}
	if v, _ := generic["version"].(float64); int(v) != codexPoolVersionModels {
		t.Errorf("saved version = %v, want %d (models present ⇒ classified)", generic["version"], codexPoolVersionModels)
	}
	accounts, _ := generic["accounts"].([]any)
	if len(accounts) != 1 {
		t.Fatalf("sibling accounts list mangled; got %v", generic["accounts"])
	}
	if acct, _ := accounts[0].(map[string]any); acct["path"] != "codex-auth/work.json" || acct["weight"].(float64) != 3 {
		t.Errorf("sibling account mangled by round-trip: %v", accounts[0])
	}
	// The classification probe must agree after the round-trip.
	if classified, n := codexPoolModelInfo(dir); !classified || n != 0 {
		t.Errorf("round-tripped empty-models pool: classified=%v count=%d, want true/0", classified, n)
	}
}

// TestSetCodexPoolWeight_RefusesEmptyModelsClassifiedPool guards the edit path
// for the empty-`models` shape: even with zero categories the pool is
// classified (the kernel ignores the sibling accounts), so a flat weight edit
// must be refused with errCodexPoolModelClassified and the file bytes — the
// empty map AND the sibling accounts — left exactly as the operator wrote
// them. The sibling accounts list is never treated as editable.
func TestSetCodexPoolWeight_RefusesEmptyModelsClassifiedPool(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	raw := seedEmptyModelsClassifiedPool(t, dir)

	work := filepath.Join(dir, codexAuthSubdir, "work.json")
	err := setCodexPoolWeight(dir, work, 2)
	if err == nil {
		t.Fatal("flat weight edit on an empty-models classified pool must be refused")
	}
	if !errors.Is(err, errCodexPoolModelClassified) {
		t.Errorf("refusal error = %v, want errCodexPoolModelClassified", err)
	}

	got, readErr := os.ReadFile(codexPoolPath(dir))
	if readErr != nil {
		t.Fatalf("read pool file back: %v", readErr)
	}
	if string(got) != string(raw) {
		t.Errorf("refused edit must leave the pool file byte-identical;\n got: %s\nwant: %s", got, raw)
	}
}

// TestLoadSaveCodexPool_RoundTripsModelsMap guards the v2 round-trip: a
// hand-authored model-classified pool file must survive load→save with its
// `models` map semantically intact and the version stamped 2 — never silently
// collapsed back to a flat v1 file (which would destroy the operator's
// classification the first time the TUI rewrites the file).
func TestLoadSaveCodexPool_RoundTripsModelsMap(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	seedModelClassifiedPool(t, dir)

	pool, err := loadCodexPool(dir)
	if err != nil {
		t.Fatalf("load v2 pool: %v", err)
	}
	if pool.Models == nil || len(*pool.Models) != 2 {
		t.Fatalf("loaded models want 2 categories; got %#v", pool.Models)
	}
	if err := saveCodexPool(dir, pool); err != nil {
		t.Fatalf("save v2 pool: %v", err)
	}

	reloaded, err := loadCodexPool(dir)
	if err != nil {
		t.Fatalf("reload v2 pool: %v", err)
	}
	if reloaded.Version != codexPoolVersionModels {
		t.Errorf("round-tripped version = %d, want %d", reloaded.Version, codexPoolVersionModels)
	}
	if reloaded.Models == nil {
		t.Fatal("round-tripped pool lost its models map")
	}
	sol := (*reloaded.Models)["gpt-5.6-sol"]
	if len(sol) != 2 || sol[0].Path != "codex-auth/work.json" || sol[0].Weight != 3 ||
		sol[1].Path != "codex-auth.json" || sol[1].Weight != 1 {
		t.Errorf("gpt-5.6-sol category mangled by round-trip: %#v", sol)
	}
	old := (*reloaded.Models)["gpt-5.5"]
	if len(old) != 1 || old[0].Path != "codex-auth/backup.json" || old[0].Weight != 1 {
		t.Errorf("gpt-5.5 category mangled by round-trip: %#v", old)
	}
}

// TestSetCodexPoolWeight_RefusesModelClassifiedPool guards the flat-edit
// refusal: on a model-classified pool a +/-/0 weight edit must return
// errCodexPoolModelClassified and leave the file bytes untouched — a flat
// `accounts` entry would be ignored by the kernel (models wins), and any
// rewrite risks mangling the hand-authored classification.
func TestSetCodexPoolWeight_RefusesModelClassifiedPool(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	raw := seedModelClassifiedPool(t, dir)

	work := filepath.Join(dir, codexAuthSubdir, "work.json")
	err := setCodexPoolWeight(dir, work, 2)
	if err == nil {
		t.Fatal("flat weight edit on a model-classified pool must be refused")
	}
	if !errors.Is(err, errCodexPoolModelClassified) {
		t.Errorf("refusal error = %v, want errCodexPoolModelClassified", err)
	}

	got, readErr := os.ReadFile(codexPoolPath(dir))
	if readErr != nil {
		t.Fatalf("read pool file back: %v", readErr)
	}
	if string(got) != string(raw) {
		t.Errorf("refused edit must leave the pool file byte-identical;\n got: %s\nwant: %s", got, raw)
	}
}

// TestCodexPoolModelInfo covers the UI's classification probe: missing, flat,
// and malformed pools report unclassified; a v2 pool reports classified with
// its category count.
func TestCodexPoolModelInfo(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")

	missing := t.TempDir()
	if classified, n := codexPoolModelInfo(missing); classified || n != 0 {
		t.Errorf("missing pool: classified=%v count=%d, want false/0", classified, n)
	}

	flat := t.TempDir()
	if err := setCodexPoolWeight(flat, filepath.Join(flat, codexAuthSubdir, "w.json"), 1); err != nil {
		t.Fatalf("seed flat pool: %v", err)
	}
	if classified, n := codexPoolModelInfo(flat); classified || n != 0 {
		t.Errorf("flat pool: classified=%v count=%d, want false/0", classified, n)
	}

	bad := t.TempDir()
	if err := os.WriteFile(codexPoolPath(bad), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed malformed pool: %v", err)
	}
	if classified, n := codexPoolModelInfo(bad); classified || n != 0 {
		t.Errorf("malformed pool: classified=%v count=%d, want false/0", classified, n)
	}

	v2 := t.TempDir()
	seedModelClassifiedPool(t, v2)
	if classified, n := codexPoolModelInfo(v2); !classified || n != 2 {
		t.Errorf("v2 pool: classified=%v count=%d, want true/2", classified, n)
	}

	// PRESENCE, not non-emptiness, classifies: the kernel treats any top-level
	// `models` dict — even `{}` — as classified (isinstance(models, dict)).
	emptyModels := t.TempDir()
	seedEmptyModelsClassifiedPool(t, emptyModels)
	if classified, n := codexPoolModelInfo(emptyModels); !classified || n != 0 {
		t.Errorf("empty-models pool: classified=%v count=%d, want true/0", classified, n)
	}

	// `models: null` is NOT a dict — the kernel falls back to the flat
	// accounts list, so the TUI must report unclassified.
	nullModels := t.TempDir()
	nullRaw := []byte(`{"version": 1, "models": null, "accounts": [{"path": "codex-auth/w.json", "weight": 1}]}`)
	if err := os.WriteFile(codexPoolPath(nullModels), nullRaw, 0o644); err != nil {
		t.Fatalf("seed null-models pool: %v", err)
	}
	if classified, n := codexPoolModelInfo(nullModels); classified || n != 0 {
		t.Errorf("null-models pool: classified=%v count=%d, want false/0", classified, n)
	}
}

// TestCodexPoolFileCorrupt_ModelClassifiedIsNotCorrupt pins that a valid v2
// (model-classified) pool file parses cleanly — classification must never be
// mistaken for corruption.
func TestCodexPoolFileCorrupt_ModelClassifiedIsNotCorrupt(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	seedModelClassifiedPool(t, dir)
	if codexPoolFileCorrupt(dir) {
		t.Error("a valid model-classified pool file must not be reported corrupt")
	}
}

// TestCodexPoolFileCorrupt guards N5's detection helper: a malformed pool file
// reports corrupt=true, a missing one reports false, and a valid one reports
// false.
func TestCodexPoolFileCorrupt(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")

	missing := t.TempDir()
	if codexPoolFileCorrupt(missing) {
		t.Error("a missing pool file must not be reported corrupt")
	}

	bad := t.TempDir()
	if err := os.WriteFile(codexPoolPath(bad), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("seed malformed pool: %v", err)
	}
	if !codexPoolFileCorrupt(bad) {
		t.Error("a malformed pool file must be reported corrupt")
	}

	good := t.TempDir()
	if err := setCodexPoolWeight(good, filepath.Join(good, codexAuthSubdir, "w.json"), 1); err != nil {
		t.Fatalf("seed valid pool: %v", err)
	}
	if codexPoolFileCorrupt(good) {
		t.Error("a valid pool file must not be reported corrupt")
	}
}

func TestCodexPoolEligibilityFacts_FallbackAndFailClosed(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")

	// No pool is an empty runtime snapshot and may use a valid legacy token.
	empty := t.TempDir()
	legacy := filepath.Join(empty, "codex-auth.json")
	writeStubCodexToken(t, legacy, "legacy@example.com")
	if !codexPoolEligible(empty, "gpt-5.6-sol") {
		t.Fatal("missing pool with valid legacy token should be fallback-ready")
	}
	flat, classified, fallback := codexPoolEligibilityFacts(empty)
	if !flat || classified != nil || !fallback {
		t.Fatalf("missing pool facts = (%v, %v, %v), want true/nil/true", flat, classified, fallback)
	}

	// A nonempty pool whose only member is unusable must not silently route to
	// the legacy account.
	nonempty := t.TempDir()
	writeStubCodexToken(t, filepath.Join(nonempty, "codex-auth.json"), "legacy@example.com")
	raw := []byte(`{"version":1,"accounts":[{"path":"codex-auth/missing.json","weight":1}]}`)
	if err := os.WriteFile(codexPoolPath(nonempty), raw, 0o644); err != nil {
		t.Fatalf("write pool: %v", err)
	}
	if codexPoolEligible(nonempty, "gpt-5.6-sol") {
		t.Fatal("nonempty unusable pool must remain ineligible")
	}

	// An empty classified category is fallback-ready, while a nonempty
	// unusable category remains ineligible.
	classifiedDir := t.TempDir()
	writeStubCodexToken(t, filepath.Join(classifiedDir, "codex-auth.json"), "legacy@example.com")
	classifiedRaw := []byte(`{"version":2,"models":{"gpt-5.6-sol":[],"gpt-5.5":[{"path":"codex-auth/missing.json","weight":1}]}}`)
	if err := os.WriteFile(codexPoolPath(classifiedDir), classifiedRaw, 0o644); err != nil {
		t.Fatalf("write classified pool: %v", err)
	}
	if !codexPoolEligible(classifiedDir, "gpt-5.6-sol") {
		t.Fatal("empty classified category should be fallback-ready")
	}
	if codexPoolEligible(classifiedDir, "gpt-5.5") {
		t.Fatal("nonempty unusable classified category must remain ineligible")
	}
}

func TestCodexPoolEligibility_DroppedEntriesUseLegacyFallback(t *testing.T) {
	t.Setenv("LINGTAI_TUI_DIR", "")
	dir := t.TempDir()
	writeStubCodexToken(t, legacyCodexAuthPath(dir), "legacy@example.com")
	raw := []byte(`{"version":1,"accounts":[0,{"path":"","weight":1},{"path":"codex-auth/disabled.json","weight":1,"enabled":false},{"path":"codex-auth/zero.json","weight":0}]}`)
	if err := os.WriteFile(codexPoolPath(dir), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if !codexPoolEligible(dir, "gpt-test") {
		t.Fatal("all dropped pool entries should be validated-empty and use legacy fallback")
	}
}
