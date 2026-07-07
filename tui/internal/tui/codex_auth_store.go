package tui

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// Codex OAuth credential storage.
//
// Historically the TUI stored exactly one Codex OAuth token bundle at
// ~/.lingtai-tui/codex-auth.json (the "legacy" file). To support multiple
// ChatGPT accounts, additional accounts live as separate token files under
// ~/.lingtai-tui/codex-auth/<slug>.json. A Codex preset binds to a specific
// account through its non-secret manifest.llm.codex_auth_path field; a preset
// with no such field falls back to the legacy file.
//
// Everything here treats the token JSON as a secret: contents are parsed only
// to read non-secret display metadata (label/email) and to confirm a non-empty
// refresh_token. Nothing in this file logs or prints token material.

// legacyCodexAuthFile is the historical single-account filename, kept as the
// default/fallback target for presets that declare no codex_auth_path.
const legacyCodexAuthFile = "codex-auth.json"

// codexAuthSubdir is the directory (under globalDir) holding the additional
// per-account token files.
const codexAuthSubdir = "codex-auth"

// legacyCodexAuthPath returns the absolute path of the legacy single-account
// token file (~/.lingtai-tui/codex-auth.json).
func legacyCodexAuthPath(globalDir string) string {
	return filepath.Join(globalDir, legacyCodexAuthFile)
}

// codexAuthDir returns the directory holding additional per-account token
// files (~/.lingtai-tui/codex-auth/).
func codexAuthDir(globalDir string) string {
	return filepath.Join(globalDir, codexAuthSubdir)
}

// codexAccount describes one stored Codex OAuth credential for display and
// selection. Path is the absolute on-disk location; Ref is the home-shortened
// string written into a preset's manifest.llm.codex_auth_path. Label is the
// user-editable non-secret display name stored alongside the token. Email is
// the account email when the token JWT carried one ("" otherwise). Legacy
// marks the single historical ~/.lingtai-tui/codex-auth.json file. Valid
// reports whether the file parses with a non-empty refresh_token.
type codexAccount struct {
	Path   string
	Ref    string
	Label  string
	Email  string
	Legacy bool
	Valid  bool
}

// DisplayName returns a stable, human-friendly identifier for the account that
// never exposes secret material. Prefers the user label, then email, then the
// file stem (slug) for per-account files, and a fixed label for the legacy file.
func (a codexAccount) DisplayName() string {
	if strings.TrimSpace(a.Label) != "" {
		return strings.TrimSpace(a.Label)
	}
	if a.Email != "" {
		return a.Email
	}
	if a.Legacy {
		return "default"
	}
	return strings.TrimSuffix(filepath.Base(a.Path), filepath.Ext(a.Path))
}

// readCodexTokenFile parses a token file and returns the bundle. It is the
// single low-level reader; callers above it decide what (if anything) to
// surface. Returns ok=false on any read/parse error or an empty refresh_token.
func readCodexTokenFile(path string) (CodexTokens, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CodexTokens{}, false
	}
	var tokens CodexTokens
	if json.Unmarshal(data, &tokens) != nil {
		return CodexTokens{}, false
	}
	if strings.TrimSpace(tokens.Email) == "" {
		tokens.Email = extractEmailFromJWT(tokens.AccessToken)
	}
	tokens.Label = strings.TrimSpace(tokens.Label)
	return tokens, strings.TrimSpace(tokens.RefreshToken) != ""
}

// saveCodexCredentialLabel rewrites only the non-secret display label inside a
// token bundle, preserving OAuth tokens, email, and any unknown/future JSON
// fields. An empty label clears the JSON key. The token file remains 0600.
func saveCodexCredentialLabel(path, label string) (CodexTokens, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return CodexTokens{}, err
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return CodexTokens{}, err
	}
	if raw == nil {
		return CodexTokens{}, errors.New("codex auth token JSON must be an object")
	}

	label = strings.TrimSpace(label)
	if label == "" {
		delete(raw, "label")
	} else {
		encodedLabel, err := json.Marshal(label)
		if err != nil {
			return CodexTokens{}, err
		}
		raw["label"] = encodedLabel
	}

	out, err := json.MarshalIndent(raw, "", "  ")
	if err != nil {
		return CodexTokens{}, err
	}
	if err := os.WriteFile(path, out, 0o600); err != nil {
		return CodexTokens{}, err
	}
	tokens, _ := readCodexTokenFile(path)
	tokens.Label = label
	return tokens, nil
}

// codexAuthPathValid reports whether the token file at the given absolute path
// parses and carries a non-empty refresh_token. No secret is returned.
func codexAuthPathValid(path string) bool {
	_, ok := readCodexTokenFile(path)
	return ok
}

// resolveCodexAuthPath turns a preset's manifest.llm.codex_auth_path value into
// an absolute path. An empty ref falls back to the legacy single-account file.
// "~/"-prefixed and absolute refs are honored; a bare filename is resolved
// under globalDir so a relative value still lands in the TUI-owned tree.
func resolveCodexAuthPath(globalDir, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return legacyCodexAuthPath(globalDir)
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
	return filepath.Join(globalDir, ref)
}

// codexAuthRefForPath returns the home-shortened string to write into a
// preset's manifest.llm.codex_auth_path for the given absolute path. Paths
// under the user's home become "~/..."; others are returned unchanged. The
// legacy file deliberately maps to "" so legacy-bound presets keep their
// implicit fallback (no field churn on existing presets).
func codexAuthRefForPath(globalDir, absPath string) string {
	if absPath == "" || absPath == legacyCodexAuthPath(globalDir) {
		return ""
	}
	if home, err := os.UserHomeDir(); err == nil {
		if rel, err := filepath.Rel(home, absPath); err == nil && !strings.HasPrefix(rel, "..") {
			return "~/" + filepath.ToSlash(rel)
		}
	}
	return absPath
}

// codexAccountSlug derives a filesystem-safe slug for a new account file from
// the account email (local part) or, when no email is known, from a caller-
// supplied fallback. Only [a-z0-9-] survive; everything else collapses to "-".
func codexAccountSlug(email, fallback string) string {
	base := email
	if at := strings.IndexByte(base, '@'); at > 0 {
		base = base[:at]
	}
	if strings.TrimSpace(base) == "" {
		base = fallback
	}
	var b strings.Builder
	for _, r := range strings.ToLower(base) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	slug := strings.Trim(b.String(), "-")
	if slug == "" {
		slug = "account"
	}
	return slug
}

// newCodexAuthPath returns a fresh, non-colliding absolute path under
// codexAuthDir for a new account with the given email. The slug derives from
// the email; numeric suffixes break collisions with existing files. The
// directory is NOT created here — the caller creates it at write time.
func newCodexAuthPath(globalDir, email string) string {
	dir := codexAuthDir(globalDir)
	slug := codexAccountSlug(email, "codex")
	candidate := filepath.Join(dir, slug+".json")
	if _, err := os.Stat(candidate); os.IsNotExist(err) {
		return candidate
	}
	for n := 2; ; n++ {
		candidate = filepath.Join(dir, slug+"-"+itoa(n)+".json")
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
}

// itoa is a tiny dependency-free int→string for slug suffixes.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// listCodexAccounts enumerates every stored Codex OAuth account: the legacy
// file first (if present), then the per-account files sorted by label. Only
// files that parse with a non-empty refresh_token are reported as Valid, but
// malformed files are still surfaced (Valid=false) so the user can see and
// re-auth them. Returns nil when nothing is stored.
func listCodexAccounts(globalDir string) []codexAccount {
	var out []codexAccount

	if legacy := legacyCodexAuthPath(globalDir); fileExists(legacy) {
		tok, ok := readCodexTokenFile(legacy)
		out = append(out, codexAccount{
			Path:   legacy,
			Ref:    "", // legacy maps to the implicit-fallback empty ref
			Label:  tok.Label,
			Email:  tok.Email,
			Legacy: true,
			Valid:  ok,
		})
	}

	dir := codexAuthDir(globalDir)
	if entries, err := os.ReadDir(dir); err == nil {
		var extra []codexAccount
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			p := filepath.Join(dir, e.Name())
			tok, ok := readCodexTokenFile(p)
			extra = append(extra, codexAccount{
				Path:  p,
				Ref:   codexAuthRefForPath(globalDir, p),
				Label: tok.Label,
				Email: tok.Email,
				Valid: ok,
			})
		}
		sort.Slice(extra, func(i, j int) bool {
			return extra[i].DisplayName() < extra[j].DisplayName()
		})
		out = append(out, extra...)
	}

	return out
}

// hasAnyCodexAccount reports whether at least one Codex OAuth credential
// (legacy or per-account) is stored and valid.
func hasAnyCodexAccount(globalDir string) bool {
	for _, a := range listCodexAccounts(globalDir) {
		if a.Valid {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// Active Codex account: apply one chosen account to all SAVED Codex presets.
//
// "Active" is a TUI convenience, not load balancing (that's lingtai-api-pool):
// the TUI binds every user-owned Codex preset to one chosen ChatGPT account by
// rewriting each preset's non-secret manifest.llm.codex_auth_path. Templates are
// never touched (RefreshTemplates owns them and would overwrite edits anyway);
// only presets under presets/saved/ are user territory. The preset package must
// not import tui, so these helpers live here (they need codexAuthRefForPath) and
// call preset.List/preset.Save.
// ---------------------------------------------------------------------------

// presetLLM returns the manifest.llm map for a preset, or nil when absent.
func presetLLM(p preset.Preset) map[string]interface{} {
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	return llm
}

// isSavedCodexPreset reports whether p is a user-owned (saved/) preset bound to
// the codex provider — the only presets the active-account apply rewrites.
func isSavedCodexPreset(p preset.Preset) bool {
	if p.Source != preset.SourceSaved {
		return false
	}
	llm := presetLLM(p)
	if llm == nil {
		return false
	}
	prov, _ := llm["provider"].(string)
	return prov == "codex"
}

// presetCodexAuthRef returns the preset's current manifest.llm.codex_auth_path
// (trimmed), or "" when unset — the legacy/default fallback.
func presetCodexAuthRef(p preset.Preset) string {
	llm := presetLLM(p)
	if llm == nil {
		return ""
	}
	ref, _ := llm["codex_auth_path"].(string)
	return strings.TrimSpace(ref)
}

// setPresetCodexAuthRef writes ref into the preset's manifest.llm.codex_auth_path.
// An empty ref DELETES the key so a legacy/default binding serializes clean and
// matches the shipped codex template (which carries no codex_auth_path). Reports
// whether the stored value actually changed, so callers can skip a no-op Save.
func setPresetCodexAuthRef(p *preset.Preset, ref string) bool {
	llm := presetLLM(*p)
	if llm == nil {
		// A codex preset always has an llm map in practice; guard anyway.
		llm = map[string]interface{}{}
		if p.Manifest == nil {
			p.Manifest = map[string]interface{}{}
		}
		p.Manifest["llm"] = llm
	}
	cur, has := llm["codex_auth_path"]
	curStr, _ := cur.(string)
	if ref == "" {
		if !has {
			return false
		}
		delete(llm, "codex_auth_path")
		return true
	}
	if has && curStr == ref {
		return false
	}
	llm["codex_auth_path"] = ref
	return true
}

// applyActiveCodexAccount applies absPath to every SAVED Codex preset's
// codex_auth_path, and returns how many presets are now bound to that account.
// The legacy account maps to "" (key removed) so saved presets fall back to the
// legacy file. Templates and non-Codex presets are left untouched. A preset
// already pointing at the chosen account is counted but not rewritten on disk
// (no-op Save avoided). The first Save error aborts and is returned.
func applyActiveCodexAccount(globalDir, absPath string) (int, error) {
	ref := codexAuthRefForPath(globalDir, absPath)
	presets, err := preset.List()
	if err != nil {
		return 0, err
	}
	applied := 0
	for _, p := range presets {
		if !isSavedCodexPreset(p) {
			continue
		}
		applied++ // this preset is now bound to the active account
		if !setPresetCodexAuthRef(&p, ref) {
			continue // already on the active account; nothing to write
		}
		if err := preset.Save(p); err != nil {
			return applied, err
		}
	}
	return applied, nil
}

// activeCodexAuthPath derives the currently-active Codex account from the saved
// Codex presets: when they ALL reference the same account it returns that
// account's absolute token-file path with ok=true; when they disagree or none
// exist it returns ("", false). Used to mark the active row in the credentials
// list. Comparison is on the resolved absolute path so "~/"-prefixed, absolute,
// and empty(=legacy) refs that name the same file collapse together.
func activeCodexAuthPath(globalDir string) (string, bool) {
	presets, err := preset.List()
	if err != nil {
		return "", false
	}
	var resolved string
	seen := false
	for _, p := range presets {
		if !isSavedCodexPreset(p) {
			continue
		}
		path := resolveCodexAuthPath(globalDir, presetCodexAuthRef(p))
		if !seen {
			resolved = path
			seen = true
			continue
		}
		if path != resolved {
			return "", false // mixed bindings — no single active account
		}
	}
	if !seen {
		return "", false
	}
	return resolved, true
}
