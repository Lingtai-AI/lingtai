package preset

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/process"
)

//go:embed all:covenant
var covenantFS embed.FS

//go:embed all:principle
var principleFS embed.FS

// Procedures are not localized — a single procedures.md lives at the root.
// ProceduresPath() checks for a lang-specific override first (<lang>/procedures.md),
// then falls back to the root file. To add a localized version in the future,
// create procedures/<lang>/procedures.md here and it will take precedence.
//
//go:embed all:procedures
var proceduresFS embed.FS

//go:embed all:templates
var templatesFS embed.FS

//go:embed all:recipe_assets
var recipeAssetsFS embed.FS

// Note: no `all:` prefix — Go's default embed semantics exclude leading-dot
// and leading-underscore files/dirs, which keeps `.pytest_cache/` and
// `__pycache__/` out of the binary even when they exist on disk at build
// time. Skills MUST NOT ship legitimately-dot-prefixed content; if they
// need to, switch back to `all:skills` and add a runtime filter (or use a
// distinct subdir name).
//
//go:embed skills
var skillsFS embed.FS

// Preset is a reusable agent bundle. Templates ship with the TUI and
// live under ~/.lingtai-tui/presets/templates/ (regenerated on every
// launch, never user-edited). User-saved variants live under
// ~/.lingtai-tui/presets/saved/. The two directories are the only
// thing distinguishing a template from a user preset — there is no
// in-band marker.
//
// Description is a structured object with a required `summary` and an
// optional `tier` (cost/quality ladder, "1".."5"). Authors may add
// arbitrary extra keys (gains/loses/recommended_for/...); they round-trip
// through `Description.Extra`.
type Preset struct {
	Name        string                 `json:"name"`
	Description PresetDescription      `json:"description"`
	Manifest    map[string]interface{} `json:"manifest"`

	// Source is set by List/Load to record where the preset was read
	// from on disk. Runtime-only — never marshaled. Callers use this
	// instead of name-matching to ask "is this a template?".
	Source PresetSource `json:"-"`
}

// PresetSource records which directory a preset was read from. The
// directory IS the answer to "is this a template?" — no in-band marker,
// no name list to maintain.
type PresetSource int

const (
	// SourceUnknown is the zero value; in-memory presets that were never
	// loaded from disk have this. Treat as "saved" for safety so a
	// hand-built preset can't accidentally claim template status.
	SourceUnknown PresetSource = iota
	// SourceTemplate means the preset lives under presets/templates/.
	// Read-only from the TUI's perspective: the user edits a template
	// to materialize a saved variant; the template itself is rewritten
	// from embedded data on every launch.
	SourceTemplate
	// SourceSaved means the preset lives under presets/saved/. User-
	// owned; never touched by Bootstrap/RefreshTemplates.
	SourceSaved
)

// PresetDescription is the structured commentary block on a preset. The
// kernel requires a non-empty summary; tier is optional but when present
// must be one of "1".."5".
//
// Extra holds any author-authored keys beyond summary/tier that the
// kernel surfaces verbatim to the agent. They round-trip through marshal
// so editing a preset in the TUI doesn't drop extra prose.
type PresetDescription struct {
	Summary string
	Tier    string
	Extra   map[string]interface{}
}

// MarshalJSON flattens Summary, Tier, and Extra into a single JSON object.
// Summary is always emitted (even when empty) because the kernel requires
// the key. Tier is omitted when empty. Extra keys are emitted last; they
// don't override Summary or Tier.
func (d PresetDescription) MarshalJSON() ([]byte, error) {
	out := make(map[string]interface{}, 2+len(d.Extra))
	for k, v := range d.Extra {
		if k == "summary" || k == "tier" {
			continue
		}
		out[k] = v
	}
	out["summary"] = d.Summary
	if d.Tier != "" {
		out["tier"] = d.Tier
	}
	return json.Marshal(out)
}

// UnmarshalJSON accepts the structured object form. A bare-string
// description (legacy on-disk shape) is wrapped as {summary: "<str>"}
// so older files load without forcing a migration pass on every read.
func (d *PresetDescription) UnmarshalJSON(data []byte) error {
	// String form: {"description": "..."} — wrap it.
	var asString string
	if err := json.Unmarshal(data, &asString); err == nil {
		d.Summary = asString
		d.Tier = ""
		d.Extra = nil
		return nil
	}
	var asMap map[string]interface{}
	if err := DecodeJSONUseNumber(data, &asMap); err != nil {
		return err
	}
	if v, ok := asMap["summary"].(string); ok {
		d.Summary = v
	}
	if v, ok := asMap["tier"].(string); ok {
		d.Tier = v
	}
	delete(asMap, "summary")
	delete(asMap, "tier")
	if len(asMap) > 0 {
		d.Extra = asMap
	} else {
		d.Extra = nil
	}
	return nil
}

// PresetsDir returns the parent directory ~/.lingtai-tui/presets/.
// The TUI only writes to its templates/ and saved/ subdirectories;
// PresetsDir itself stays around for the kernel-side migration meta
// file (kept at the parent to survive template re-extraction).
func PresetsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, config.GlobalDirName, "presets")
}

// TemplatesDir returns ~/.lingtai-tui/presets/templates/. The TUI
// regenerates these wholesale on every launch from embedded data —
// users should never edit files here directly.
func TemplatesDir() string {
	return filepath.Join(PresetsDir(), "templates")
}

// SavedDir returns ~/.lingtai-tui/presets/saved/. User territory:
// every Save() lands here, Bootstrap/Seed never touch it.
func SavedDir() string {
	return filepath.Join(PresetsDir(), "saved")
}

// listFromDir reads every *.json file from a single preset directory
// and stamps each result with the given source. Internal helper for
// List(); centralizes the parse-and-skip-malformed logic so the two
// directory walks can't drift.
func listFromDir(dir string, src PresetSource) ([]Preset, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Preset
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if e.Name() == "_kernel_meta.json" {
			continue
		}
		path := filepath.Join(dir, e.Name())
		p, err := loadFromPath(path)
		if err != nil {
			if errors.Is(err, ErrCapabilityConflict) {
				return nil, err
			}
			continue
		}
		p.Source = src
		out = append(out, p)
	}
	return out, nil
}

// List returns saved presets first (alphabetical), then templates in
// BuiltinPresets() order (any stray template file not in that list sorts
// last, alphabetically). Each preset carries a Source field recording which
// directory it came from — callers should prefer p.Source over name-matching
// when asking "is this a template?".
func List() ([]Preset, error) {
	saved, err := listFromDir(SavedDir(), SourceSaved)
	if err != nil {
		return nil, fmt.Errorf("list saved presets: %w", err)
	}
	templates, err := listFromDir(TemplatesDir(), SourceTemplate)
	if err != nil {
		return nil, fmt.Errorf("list template presets: %w", err)
	}

	sort.Slice(saved, func(i, j int) bool {
		return saved[i].Name < saved[j].Name
	})
	templateOrder := map[string]int{}
	for i, p := range BuiltinPresets() {
		templateOrder[p.Name] = i
	}
	rank := func(name string) int {
		if i, ok := templateOrder[name]; ok {
			return i
		}
		return len(templateOrder)
	}
	sort.SliceStable(templates, func(i, j int) bool {
		ri, rj := rank(templates[i].Name), rank(templates[j].Name)
		if ri != rj {
			return ri < rj
		}
		return templates[i].Name < templates[j].Name
	})

	return append(saved, templates...), nil
}

// HasAny returns true if at least one preset exists.
func HasAny() bool {
	presets, _ := List()
	return len(presets) > 0
}

// First returns the first available preset, or an empty Preset if none exist.
func First() Preset {
	presets, _ := List()
	if len(presets) > 0 {
		return presets[0]
	}
	return Preset{Manifest: map[string]interface{}{}}
}

// Load reads a single preset by name. Looks in saved/ first, then
// templates/ — a saved preset with the same name as a template wins
// (the user's variant overrides). Returns the loaded preset with
// Source populated.
//
// A directory is only skipped when its file is genuinely absent
// (errors.Is(..., fs.ErrNotExist) through loadFromPath's %w wrapping).
// Any other error — invalid JSON, permission/read failure, a path that is a
// directory — is
// returned to the caller with the underlying cause preserved instead of
// being collapsed into a generic "preset not found", so a broken file is
// distinguishable from a missing one (issue #483). Only when BOTH the
// saved and template files are absent do we return the not-found error.
func Load(name string) (Preset, error) {
	for _, attempt := range []struct {
		dir string
		src PresetSource
	}{
		{SavedDir(), SourceSaved},
		{TemplatesDir(), SourceTemplate},
	} {
		path := filepath.Join(attempt.dir, name+".json")
		p, err := loadFromPath(path)
		if err == nil {
			p.Source = attempt.src
			return p, nil
		}
		// Genuinely missing here — fall through to the next directory.
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		// A real failure (bad JSON, unreadable file, path is a directory):
		// surface it rather than masking it as not-found.
		return Preset{}, fmt.Errorf("load preset %q: %w", name, err)
	}
	return Preset{}, fmt.Errorf("preset not found: %s", name)
}

// loadFromPath reads + parses a preset file. Internal helper.
func loadFromPath(path string) (Preset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, fmt.Errorf("read preset %s: %w", path, err)
	}
	var p Preset
	if err := DecodeJSONUseNumber(data, &p); err != nil {
		return Preset{}, fmt.Errorf("parse preset %s: %w", path, err)
	}
	p.NormalizeLegacyContextLimit()
	if err := p.NormalizeLegacyCapabilities(); err != nil {
		return Preset{}, fmt.Errorf("canonicalize preset capabilities: %w", err)
	}
	return p, nil
}

// NormalizeLegacyContextLimit accepts the old saved-preset shape where
// context_limit lived at manifest.context_limit. The canonical location is
// manifest.llm.context_limit; if both locations are present, keep the
// canonical llm value and drop the legacy root key.
func (p *Preset) NormalizeLegacyContextLimit() {
	if p == nil || p.Manifest == nil {
		return
	}
	rootCtx, hasRoot := p.Manifest["context_limit"]
	if hasRoot {
		delete(p.Manifest, "context_limit")
	}
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	if llm == nil {
		return
	}

	// Keep the legacy float64 representation for ordinary context limits, but
	// retain exact JSON integers outside float64's safe integer range. The
	// Project exchange must compare the selected snapshot with actual disk data.
	if canonical, hasCanonical := llm["context_limit"]; hasCanonical {
		if number, ok := canonical.(json.Number); ok {
			if value, err := strconv.ParseFloat(string(number), 64); err == nil && value > -9007199254740992 && value < 9007199254740992 {
				llm["context_limit"] = value
			}
		}
		return
	}
	if !hasRoot {
		return
	}
	if number, ok := rootCtx.(json.Number); ok {
		if value, err := strconv.ParseFloat(string(number), 64); err == nil && value > -9007199254740992 && value < 9007199254740992 {
			rootCtx = value
		}
	}
	llm["context_limit"] = rootCtx
}

// validTiers mirrors the kernel-side TIER_VALUES in lingtai/presets.py.
var validTiers = map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true}

// Validate returns the list of rule violations for this preset. Mirrors the
// kernel's load_preset validation gauntlet so the editor refuses to save
// anything the kernel will refuse to load. Empty slice = passes.
func (p Preset) Validate() []error {
	p.NormalizeLegacyContextLimit()
	var errs []error
	if err := p.NormalizeLegacyCapabilities(); err != nil {
		errs = append(errs, err)
	}
	if p.Description.Summary == "" {
		errs = append(errs, fmt.Errorf("description.summary must be non-empty"))
	}
	if p.Description.Tier != "" && !validTiers[p.Description.Tier] {
		errs = append(errs, fmt.Errorf("description.tier must be one of 1..5 (got %q)", p.Description.Tier))
	}
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	if llm == nil {
		errs = append(errs, fmt.Errorf("manifest.llm must be an object"))
	} else {
		if s, _ := llm["provider"].(string); s == "" {
			errs = append(errs, fmt.Errorf("manifest.llm.provider must be non-empty"))
		}
		if s, _ := llm["model"].(string); s == "" {
			errs = append(errs, fmt.Errorf("manifest.llm.model must be non-empty"))
		}
		// base_url is optional for every family: the openai and anthropic
		// families fall back to their official endpoints when it is empty,
		// and codex/claude-code own their own routes.
		if v, ok := llm["context_limit"]; ok && v != nil {
			// JSON unmarshals numbers as float64; accept int-valued floats.
			switch n := v.(type) {
			case float64:
				if n != float64(int(n)) || n <= 0 {
					errs = append(errs, fmt.Errorf("manifest.llm.context_limit must be a positive integer"))
				}
			case int:
				if n <= 0 {
					errs = append(errs, fmt.Errorf("manifest.llm.context_limit must be a positive integer"))
				}
			case json.Number:
				parsed, err := strconv.ParseInt(string(n), 10, 64)
				if err != nil || parsed <= 0 {
					errs = append(errs, fmt.Errorf("manifest.llm.context_limit must be a positive integer"))
				}
			default:
				errs = append(errs, fmt.Errorf("manifest.llm.context_limit must be a positive integer"))
			}
		}
	}
	if _, hasRootCtx := p.Manifest["context_limit"]; hasRootCtx {
		errs = append(errs, fmt.Errorf("context_limit must live inside manifest.llm, not at manifest root"))
	}
	if caps, ok := p.Manifest["capabilities"]; ok {
		if _, isMap := caps.(map[string]interface{}); !isMap {
			errs = append(errs, fmt.Errorf("manifest.capabilities must be an object"))
		}
	}
	return errs
}

// ValidateSafeName rejects any name that isn't a single, non-empty path
// segment safe to use as a directory name or filename stem beneath an
// owning directory. It rejects blank values, "." and "..", and any path
// separator — both "/" and "\" — so escapes are blocked regardless of the
// platform the binary runs on (POSIX treats "\" as a literal filename
// character; Windows treats it as a separator). A name that passes is
// guaranteed to stay a direct child of whatever base directory it is
// joined to (issue #849).
func ValidateSafeName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("must not be blank")
	}
	if name == "." || name == ".." {
		return fmt.Errorf("must not be %q", name)
	}
	if strings.ContainsAny(name, "/\\") {
		return fmt.Errorf("must not contain a path separator")
	}
	return nil
}

// Save writes a preset to the saved/ directory. Save NEVER writes to
// templates/ — that's owned by Bootstrap. Callers that want to seed
// a template must use writeTemplate (preset package internal).
func Save(p Preset) error {
	// The preset name becomes the filename stem under SavedDir(); enforce
	// the owning-layer containment invariant here so every caller — current
	// and future — is protected (issue #849).
	if err := ValidateSafeName(p.Name); err != nil {
		return fmt.Errorf("invalid preset name %q: %w", p.Name, err)
	}
	if err := p.NormalizeLegacyCapabilities(); err != nil {
		return fmt.Errorf("canonicalize preset capabilities: %w", err)
	}
	dir := SavedDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create saved presets dir: %w", err)
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal preset: %w", err)
	}
	path := filepath.Join(dir, p.Name+".json")
	return os.WriteFile(path, data, 0o644)
}

// Clone creates a deep copy of a preset with a new name.
// The original preset is not modified.
func Clone(src Preset, newName string) Preset {
	// Deep copy via JSON round-trip to avoid shared map references
	manifest := make(map[string]interface{})
	if data, err := json.Marshal(src.Manifest); err == nil {
		_ = DecodeJSONUseNumber(data, &manifest)
	}
	desc := src.Description
	if src.Description.Extra != nil {
		desc.Extra = make(map[string]interface{}, len(src.Description.Extra))
		for k, v := range src.Description.Extra {
			desc.Extra[k] = v
		}
	}
	return Preset{
		Name:        newName,
		Description: desc,
		Manifest:    manifest,
	}
}

// Delete removes a saved preset. Templates are immutable from the
// user's perspective; deleting them via the TUI is a no-op (the next
// Bootstrap re-extracts the file anyway). Returns an error only when
// a saved file existed and the unlink failed.
func Delete(name string) error {
	// The name becomes the filename stem under SavedDir(); enforce the
	// owning-layer containment invariant here so Delete can never target a
	// path outside saved/ regardless of caller (issue #849).
	if err := ValidateSafeName(name); err != nil {
		return fmt.Errorf("invalid preset name %q: %w", name, err)
	}
	path := filepath.Join(SavedDir(), name+".json")
	err := os.Remove(path)
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// RefreshTemplates rewrites templates/ from BuiltinPresets() wholesale.
// Called from Bootstrap on every TUI launch. Deletes any *.json file
// in templates/ that's no longer in BuiltinPresets() so a TUI upgrade
// that retires a template (e.g. an obsolete provider) propagates
// cleanly. Saved presets in saved/ are never touched.
func RefreshTemplates() error {
	dir := TemplatesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create templates dir: %w", err)
	}
	want := map[string]bool{}
	for _, p := range BuiltinPresets() {
		want[p.Name+".json"] = true
		data, err := json.MarshalIndent(p, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal template %s: %w", p.Name, err)
		}
		if err := os.WriteFile(filepath.Join(dir, p.Name+".json"), data, 0o644); err != nil {
			return fmt.Errorf("write template %s: %w", p.Name, err)
		}
	}
	// Prune retired templates.
	if entries, err := os.ReadDir(dir); err == nil {
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
				continue
			}
			if !want[e.Name()] {
				_ = os.Remove(filepath.Join(dir, e.Name()))
			}
		}
	}
	return nil
}

// Provider family names. These four are the only manifest.llm.provider
// values the kernel accepts; every other vendor is reached through the openai
// or anthropic family pointed at that vendor's compatible endpoint (or an
// external pool such as sub2api / subs-pool).
const (
	// ProviderOpenAI is any OpenAI-compatible endpoint (Chat Completions or
	// Responses, selected by manifest.llm.wire_api).
	ProviderOpenAI = "openai"
	// ProviderAnthropic is any Anthropic Messages-compatible endpoint.
	ProviderAnthropic = "anthropic"
	// ProviderCodex is the ChatGPT-OAuth Codex backend.
	ProviderCodex = "codex"
	// ProviderClaudeCode is the local Claude Code CLI login (shown as
	// "claude-p" in the TUI).
	ProviderClaudeCode = "claude-code"
)

// Official endpoints the two API-key families fall back to when a preset
// leaves manifest.llm.base_url empty. The kernel applies the same defaults;
// the TUI only needs them to probe an endpoint (/doctor, credentials page).
const (
	OpenAIDefaultBaseURL    = "https://api.openai.com/v1"
	AnthropicDefaultBaseURL = "https://api.anthropic.com"
)

// Wire formats an openai-family preset can select through
// manifest.llm.wire_api. Chat Completions is the default.
const (
	WireAPIChatCompletions = "chat_completions"
	WireAPIResponses       = "responses"
)

// DefaultAPIKeyEnv returns the api_key_env slot a fresh preset of the given
// provider family declares: OPENAI_API_KEY / ANTHROPIC_API_KEY for the two
// API-key families, "" for the OAuth/CLI families and anything else.
func DefaultAPIKeyEnv(provider string) string {
	switch provider {
	case ProviderOpenAI:
		return "OPENAI_API_KEY"
	case ProviderAnthropic:
		return "ANTHROPIC_API_KEY"
	}
	return ""
}

// DefaultBaseURL returns the official endpoint an API-key family uses when
// base_url is empty, or "" for providers that have no such fallback.
func DefaultBaseURL(provider string) string {
	switch provider {
	case ProviderOpenAI:
		return OpenAIDefaultBaseURL
	case ProviderAnthropic:
		return AnthropicDefaultBaseURL
	}
	return ""
}

// BuiltinPresets returns the built-in templates, one per provider family.
// Order is the picker order: codex first (the first-run default), then the
// Claude Code CLI login, then the two bring-your-own-endpoint families.
func BuiltinPresets() []Preset {
	return []Preset{
		codexPreset(),
		claudePreset(),
		openaiPreset(),
		anthropicPreset(),
	}
}

// IsBuiltin reports whether `name` matches a TUI-shipped template.
// Prefer IsTemplate(p) when you have a loaded Preset — that uses the
// directory-of-origin and is robust against a user saving a preset
// under a name that happens to match a template. (The frozen m030
// migration keeps its own historical name list; it does not call this.)
func IsBuiltin(name string) bool {
	for _, p := range BuiltinPresets() {
		if p.Name == name {
			return true
		}
	}
	return false
}

// IsTemplate reports whether the given preset was loaded from the
// templates/ directory. Use this in preference to IsBuiltin(p.Name)
// for any loaded preset — it's the canonical "is this read-only?"
// answer.
func IsTemplate(p Preset) bool {
	return p.Source == SourceTemplate
}

// RefFor returns the home-shortened on-disk path string this preset
// gets recorded as in init.json's manifest.preset.{default,active,
// allowed}. Templates resolve under presets/templates/, saved under
// presets/saved/. Presets without a Source (in-memory only, e.g.
// tests) fall back to the IsBuiltin name list.
func RefFor(p Preset) string {
	if p.Name == "" {
		return ""
	}
	subdir := "saved"
	switch p.Source {
	case SourceTemplate:
		subdir = "templates"
	case SourceSaved:
		subdir = "saved"
	default:
		if IsBuiltin(p.Name) {
			subdir = "templates"
		}
	}
	return "~/.lingtai-tui/presets/" + subdir + "/" + p.Name + ".json"
}

// isSyntheticPreset reports whether p is the in-memory-only "Keep current
// preset" sentinel that NewSetupModeModel builds from an existing init.json.
// It has no backing file on disk; RefFor would otherwise derive a non-existent
// path like ~/.lingtai-tui/presets/saved/keep_current.json.
//
// Be deliberately narrow here: SourceUnknown also appears on hand-built presets
// in tests or callers that intentionally want RefFor's saved/ fallback.
func isSyntheticPreset(p Preset) bool {
	return p.Source == SourceUnknown && p.Name == "keep_current"
}

// ResolvedRef is a single entry in ResolveRefs's output. It captures
// everything a UI surface (the kanban Presets section in particular)
// needs to render an at-a-glance health check for a preset path
// recorded in manifest.preset.{default,active,allowed}.
// CredentialFamily is the narrow set of provider-owned authentication
// families understood by the TUI. Keep this classifier here so every UI
// surface consumes the manifest provider rather than reimplementing alias
// lists or inferring auth from filenames.
type CredentialFamily string

const (
	CredentialFamilyOther       CredentialFamily = "other"
	CredentialFamilyCodexSingle CredentialFamily = "codex_single"
	CredentialFamilyClaudeCLI   CredentialFamily = "claude_cli"
)

// ClassifyCredentialFamily classifies only exact manifest.llm.provider values.
// Unknown values, including path/filename-like strings, remain Other.
func ClassifyCredentialFamily(provider string) CredentialFamily {
	switch provider {
	case "codex", "codex_oauth":
		return CredentialFamilyCodexSingle
	case "claude-code", "claude_code", "claude-agent-sdk", "claude_agent_sdk":
		return CredentialFamilyClaudeCLI
	default:
		return CredentialFamilyOther
	}
}

// UsesLoginCredential reports whether provider authenticates through a local
// CLI/OAuth login (Claude Code CLI, Codex OAuth) rather than an API key in
// .env. It is a pure classification of the manifest value — it does not probe
// whether the login is currently valid.
func UsesLoginCredential(provider string) bool {
	return ClassifyCredentialFamily(provider) != CredentialFamilyOther
}

// ResolvedRef is a credential-aware view of one preset reference.
type ResolvedRef struct {
	// Ref is the original input string (e.g. "~/.lingtai-tui/presets/templates/codex.json").
	Ref string
	// Name is the preset's filename stem (e.g. "codex"). Empty when Ref
	// is malformed.
	Name string
	// Source is SourceTemplate when the resolved path lives under a
	// /templates/ segment, SourceSaved when it lives under /saved/,
	// SourceUnknown otherwise (legacy flat layout, custom user path).
	Source PresetSource
	// Exists reports whether the path exists.
	Exists bool
	// ManifestValid reports that the existing path was successfully loaded.
	// A false value is deliberately not interpreted as a legacy Codex preset.
	ManifestValid bool
	// Provider and Family come only from the loaded manifest provider.
	Provider string
	Family   CredentialFamily
	// HasKey reports whether the preset's credential is actually
	// configured. For a preset with a non-empty api_key_env, this is true
	// only when that env var has a value in the passed existingKeys map.
	// For a codex preset (provider "codex", which uses ChatGPT OAuth and
	// declares no api_key_env), this is true only when OAuth is configured
	// (see AuthState.CodexOAuthConfigured). For a Claude preset
	// (provider "claude-code"/"claude_code", which authenticates through the
	// local Claude Code CLI login and declares no api_key_env),
	// this is true only when the CLI reports a logged-in session (see
	// AuthState.ClaudeCodeAuthConfigured). A preset with an empty
	// api_key_env that is not one of those OAuth/CLI providers has no
	// configured credential, so this is false. Only meaningful when Exists
	// is true.
	HasKey bool
	// CodexAuthRef is the codex preset's manifest.llm.codex_auth_path value
	// (verbatim, possibly ""). Empty with the field omitted or literal "" means
	// the preset accepts any usable stored Codex account (legacy file or any
	// per-account file); empty from a present wrong-type/whitespace value means
	// the preset fails closed (see ResolvePresetWithAuth). Only set for codex
	// presets; "" for all others. Lets a UI surface which account a codex
	// preset is bound to without re-reading the preset file.
	CodexAuthRef string
}

// AuthState carries machine-level credential facts the credential guard
// cannot derive from a preset file alone: Codex OAuth token state and the
// Claude Code CLI session.
type AuthState struct {
	// CodexOAuthConfigured is the caller-provided fallback signal for a codex
	// preset that declares no manifest.llm.codex_auth_path when CodexAuthDir is
	// empty. Callers that know the token store directory should set CodexAuthDir
	// so the preset package can inspect legacy and per-account token files.
	CodexOAuthConfigured bool

	// CodexAuthDir is the directory (typically ~/.lingtai-tui) that
	// per-account codex_auth_path values resolve against when they are
	// relative or "~/"-prefixed. When set, explicit manifest.llm.codex_auth_path
	// values validate exactly that bound token file, so multiple Codex accounts
	// are judged independently. An empty codex_auth_path means "default Codex
	// credentials" and is valid when either the legacy token or any per-account
	// token under codex-auth/ is usable. Empty falls back to CodexOAuthConfigured.
	CodexAuthDir string

	// ClaudeCodeAuthConfigured is true when the local Claude Code CLI
	// (`claude`) is installed and reports a logged-in session. The
	// claude-code provider authenticates through that existing CLI login
	// (no per-request API key, no separate token stored by the TUI), so a
	// Claude preset is credential-valid only when this is
	// true. Computed by the caller (see tui.claudeCodeAuthConfigured) and
	// passed in to avoid the preset→tui import cycle.
	ClaudeCodeAuthConfigured bool
}

// codexTokenFileValid reports whether the Codex OAuth token file at the
// resolved path parses and carries a non-empty refresh_token. Mirrors the
// tui package's readCodexTokenFile check but lives here to avoid the
// preset→tui import cycle. Token material is never returned or logged.
func codexTokenFileValid(path string) bool {
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var tok struct {
		RefreshToken string `json:"refresh_token"`
	}
	return json.Unmarshal(data, &tok) == nil && strings.TrimSpace(tok.RefreshToken) != ""
}

func anyCodexTokenFileValid(authDir string) bool {
	if codexTokenFileValid(filepath.Join(authDir, "codex-auth.json")) {
		return true
	}
	entries, err := os.ReadDir(filepath.Join(authDir, "codex-auth"))
	if err != nil {
		return false
	}
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		if codexTokenFileValid(filepath.Join(authDir, "codex-auth", e.Name())) {
			return true
		}
	}
	return false
}

// resolveCodexAuthRef expands a preset's manifest.llm.codex_auth_path against
// authDir. Empty ref → the legacy ~/.lingtai-tui/codex-auth.json fallback.
// "~/"-prefixed and absolute refs are honored; a bare relative value resolves
// under authDir so it still lands in the TUI-owned tree.
func resolveCodexAuthRef(authDir, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return filepath.Join(authDir, "codex-auth.json")
	}
	if strings.HasPrefix(ref, "~/") || ref == "~" {
		return expandUserPath(ref)
	}
	if filepath.IsAbs(ref) {
		return ref
	}
	return filepath.Join(authDir, ref)
}

// ResolveRefs expands and inspects a list of preset path strings. For
// each ref, returns the directory-of-origin (templates/saved), whether
// the file exists, and whether its declared api_key_env has a value in
// existingKeys. Used by the kanban's Presets section to render an
// at-a-glance health check for an agent's preset surface.
//
// Ref strings are accepted in the same forms the kernel accepts:
// absolute, ~/-prefixed, or relative to the caller's working dir
// (relative paths are resolved against $PWD — pass absolute or
// home-relative for predictable behavior).
//
// existingKeys is the env-var-name → value map (typically
// Config.Keys). Pass nil when no key store is available; HasKey will
// be false for any preset that declares an api_key_env.
//
// ResolveRefs assumes NO OAuth is configured (the conservative default):
// a codex preset resolves to HasKey=false under this entry point. Callers
// that make credential-sensitive validity decisions should use
// ResolveRefsWithAuth and pass the real OAuth state.
func ResolveRefs(refs []string, existingKeys map[string]string) []ResolvedRef {
	return ResolveRefsWithAuth(refs, existingKeys, AuthState{})
}

// ResolveRefsWithAuth is ResolveRefs plus machine-level credential state
// (auth), so the codex-OAuth case can be judged correctly: a codex preset
// is valid only when auth.CodexOAuthConfigured is true. See ResolveRefs for
// the ref-string and existingKeys contracts.
func ResolveRefsWithAuth(refs []string, existingKeys map[string]string, auth AuthState) []ResolvedRef {
	out := make([]ResolvedRef, 0, len(refs))
	for _, ref := range refs {
		out = append(out, resolveOneRef(ref, existingKeys, auth))
	}
	return out
}

// ResolvePresetWithAuth applies the same credential-family rules as
// ResolveRefsWithAuth to an already-loaded in-memory preset. UI flows that
// already own a Preset should use this helper instead of duplicating provider
// and credential checks.
func ResolvePresetWithAuth(p Preset, existingKeys map[string]string, auth AuthState) ResolvedRef {
	r := ResolvedRef{
		Name:          p.Name,
		Source:        p.Source,
		Family:        CredentialFamilyOther,
		Exists:        true,
		ManifestValid: true,
	}
	llm, ok := p.Manifest["llm"].(map[string]interface{})
	if !ok || llm == nil {
		r.ManifestValid = false
		return r
	}
	r.Provider, _ = llm["provider"].(string)
	r.Family = ClassifyCredentialFamily(r.Provider)
	apiKeyEnv, _ := llm["api_key_env"].(string)
	codexAuthRaw, codexAuthPresent := llm["codex_auth_path"]
	r.CodexAuthRef, _ = codexAuthRaw.(string) // "" for absent or wrong-type values
	setAuth := func(ok bool) { r.HasKey = ok }
	switch r.Family {
	case CredentialFamilyCodexSingle:
		codexAuthStr, codexAuthIsString := codexAuthRaw.(string)
		switch {
		case codexAuthPresent && !codexAuthIsString:
			// Present but wrong-type explicit binding: fail closed.
			setAuth(false)
		case codexAuthPresent && codexAuthIsString && strings.TrimSpace(codexAuthStr) == "" && codexAuthStr != "":
			// Whitespace-only explicit value: fail closed (canonical empty is
			// an omitted field or literal "").
			setAuth(false)
		case codexAuthPresent && codexAuthIsString && strings.TrimSpace(codexAuthStr) != "":
			// Explicit nonempty binding: never the aggregate bool. With a known
			// dir, resolve exactly; without one, only absolute/"~" refs are
			// checkable, and any other relative ref fails closed.
			ref := strings.TrimSpace(codexAuthStr)
			if auth.CodexAuthDir != "" {
				setAuth(codexTokenFileValid(resolveCodexAuthRef(auth.CodexAuthDir, ref)))
			} else if filepath.IsAbs(ref) || strings.HasPrefix(ref, "~/") || ref == "~" {
				setAuth(codexTokenFileValid(resolveCodexAuthRef("", ref)))
			} else {
				setAuth(false)
			}
		default:
			// Unbound (field omitted or canonical empty): accept any usable
			// stored account when a dir is known, else the caller-provided
			// aggregate signal.
			if auth.CodexAuthDir != "" {
				setAuth(anyCodexTokenFileValid(auth.CodexAuthDir))
			} else {
				setAuth(auth.CodexOAuthConfigured)
			}
		}
	case CredentialFamilyClaudeCLI:
		setAuth(auth.ClaudeCodeAuthConfigured)
	default:
		if apiKeyEnv != "" {
			setAuth(existingKeys[apiKeyEnv] != "")
		}
	}
	return r
}

func resolveOneRef(ref string, existingKeys map[string]string, auth AuthState) ResolvedRef {
	r := ResolvedRef{Ref: ref, Family: CredentialFamilyOther}
	if ref == "" {
		return r
	}
	abs := expandUserPath(ref)
	r.Name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	switch {
	case strings.Contains(abs, string(filepath.Separator)+"templates"+string(filepath.Separator)):
		r.Source = SourceTemplate
	case strings.Contains(abs, string(filepath.Separator)+"saved"+string(filepath.Separator)):
		r.Source = SourceSaved
	default:
		r.Source = SourceUnknown
	}
	if _, err := os.Stat(abs); err != nil {
		return r
	}
	r.Exists = true
	p, err := loadFromPath(abs)
	if err != nil {
		// Do not infer a provider from the basename/path. In particular, a
		// malformed or unreadable `codex.json` is not allowed to fail open
		// to the legacy Codex credential.
		return r
	}
	r = ResolvePresetWithAuth(p, existingKeys, auth)
	r.Ref = ref
	r.Name = strings.TrimSuffix(filepath.Base(abs), filepath.Ext(abs))
	r.Source = func() PresetSource {
		switch {
		case strings.Contains(abs, string(filepath.Separator)+"templates"+string(filepath.Separator)):
			return SourceTemplate
		case strings.Contains(abs, string(filepath.Separator)+"saved"+string(filepath.Separator)):
			return SourceSaved
		default:
			return SourceUnknown
		}
	}()
	return r
}

// expandUserPath returns abs(`~/foo` → `$HOME/foo`), passing other forms
// through unchanged. Internal helper for ResolveRefs.
func expandUserPath(p string) string {
	if p == "~" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// AutoSavedName picks a fresh saved-preset name derived from a template,
// using the same gap-fill counter as AutoEnvVarName. Pattern is
// "<template>-<N>" where N is the lowest positive integer that doesn't
// collide with anything in `existing`. Used when the user saves an
// edited template preset — we never overwrite the template, we always
// branch off a saved copy.
//
// existing is the set of preset names currently on disk (from List()).
// Returns "" when template is empty.
func AutoSavedName(template string, existing []string) string {
	if template == "" {
		return ""
	}
	used := map[int]bool{}
	wantPrefix := template + "-"
	for _, name := range existing {
		if !strings.HasPrefix(name, wantPrefix) {
			continue
		}
		mid := strings.TrimPrefix(name, wantPrefix)
		n := 0
		for _, c := range mid {
			if c < '0' || c > '9' {
				n = -1
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 {
			used[n] = true
		}
	}
	for n := 1; ; n++ {
		if !used[n] {
			return fmt.Sprintf("%s-%d", template, n)
		}
	}
}

// SavedCount returns the number of saved presets (Source == SourceSaved)
// in the list. Falls back to the legacy IsBuiltin check for any preset
// whose Source wasn't populated (e.g. hand-built test fixtures).
func SavedCount(presets []Preset) int {
	n := 0
	for _, p := range presets {
		switch p.Source {
		case SourceSaved:
			n++
		case SourceUnknown:
			if !IsBuiltin(p.Name) {
				n++
			}
		}
	}
	return n
}

// skillsDefault returns the default skills capability config — two Tier 1
// paths: the network-shared skills shelf (resolved relative to the agent dir)
// and the TUI's per-user utilities directory. Users can edit init.json to
// add or remove paths; init.json is the ground truth and the capability
// reads it on every setup.
//
// `skills` itself is default-on in the kernel; this entry exists only to
// override the default kwargs (which carry no extra paths).
func skillsDefault() map[string]interface{} {
	return map[string]interface{}{
		"paths": []interface{}{
			"../.library_shared",
			"~/.lingtai-tui/utilities",
		},
	}
}

// familyCapabilities is the capability block shared by every template that
// can serve images and search: web_search uses the provider-agnostic
// DuckDuckGo engine, and vision inherits the agent's own LLM provider and
// credentials (the kernel expands "inherit" at boot). If the configured
// endpoint or model cannot take image input, the vision call fails at
// runtime — the TUI makes no per-vendor vision claim.
func familyCapabilities() map[string]interface{} {
	return map[string]interface{}{
		"web_search": map[string]interface{}{"provider": "duckduckgo"},
		"vision":     map[string]interface{}{"provider": "inherit"},
		"skills":     skillsDefault(),
	}
}

func codexPreset() Preset {
	return Preset{
		Name:        "codex",
		Description: PresetDescription{Summary: "ChatGPT account (Codex OAuth) — vision + web search + tools"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				// Keep gpt-5.6-sol as the default: gpt-6-astra is documented and
				// picker-visible, but exact authenticated OAuth-route availability
				// is not positive evidence for silently promoting it. The named
				// GPT-5.6 routes remain selectable where enabled. The complete
				// curated list lives in preset_editor.go; the bare gpt-5.6 alias is
				// intentionally omitted.
				"provider": ProviderCodex, "model": "gpt-5.6-sol",
				"api_key": nil, "api_key_env": "",
				"base_url": "https://chatgpt.com/backend-api/codex",
				// LingTai is the primary brain, so Codex runs at maximum
				// reasoning effort by default. Carried explicitly
				// here (not a UI-only fallback) so the running session and
				// generated init.json actually receive xhigh.
				"thinking": "xhigh",
			},
			"capabilities": familyCapabilities(),
		},
	}
}

func claudePreset() Preset {
	return Preset{
		Name:        "claude",
		Description: PresetDescription{Summary: "Claude Code / Claude Max — uses your local Claude CLI login (no API key)"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				// The kernel's canonical Claude Code provider invokes the local
				// `claude` CLI, whose print-mode backend is shown as "claude-p"
				// in the TUI. It reuses the CLI's OAuth login — no per-request
				// API key, and the TUI stores no Anthropic token of its own. So
				// api_key is nil and api_key_env is empty; credential validity
				// is judged by detecting an existing `claude` CLI login (see
				// AuthState.ClaudeCodeAuthConfigured). Default model is the CLI
				// alias "opus"; the editor also offers "fable", whose full ID
				// is `claude-fable-5-1` in current Claude Code.
				"provider": ProviderClaudeCode, "model": "opus",
				"api_key": nil, "api_key_env": "",
			},
			// Conservative capabilities: Claude Code is wired here as a completion
			// provider only. We do NOT route web_search or vision through it —
			// the CLI's own native tool surface is out of scope, and the
			// kernel's claude-code vision route only returns CLI guidance.
			// Keep the standard LingTai skills default so agents behave like
			// any other preset.
			"capabilities": map[string]interface{}{
				"skills": skillsDefault(),
			},
		},
	}
}

// openaiPreset is the OpenAI-compatible family template: the official
// OpenAI API when base_url is empty, or any compatible endpoint (a vendor's
// OpenAI-compatible API, a local server, or an account pool such as
// sub2api / subs-pool) when the user fills base_url in. wire_api selects
// Chat Completions (the default) or Responses. There is no universal model,
// so the model starts empty and must be typed before the preset can be saved.
func openaiPreset() Preset {
	return Preset{
		Name:        "openai",
		Description: PresetDescription{Summary: "OpenAI-compatible — official OpenAI API or any compatible endpoint (Chat Completions or Responses)"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": ProviderOpenAI, "model": "",
				"api_key": nil, "api_key_env": DefaultAPIKeyEnv(ProviderOpenAI),
				"base_url": nil, "wire_api": WireAPIChatCompletions,
			},
			"capabilities": familyCapabilities(),
		},
	}
}

// anthropicPreset is the Anthropic-compatible family template: the official
// Anthropic API when base_url is empty, or any Messages-compatible endpoint
// when the user fills base_url in. The model starts empty for the same
// reason as openaiPreset.
func anthropicPreset() Preset {
	return Preset{
		Name:        "anthropic",
		Description: PresetDescription{Summary: "Anthropic-compatible — official Anthropic API or any Messages-compatible endpoint"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": ProviderAnthropic, "model": "",
				"api_key": nil, "api_key_env": DefaultAPIKeyEnv(ProviderAnthropic),
				"base_url": nil,
			},
			"capabilities": familyCapabilities(),
		},
	}
}

// ProceduresPath returns the absolute path to the procedures file for a language.
// Checks the lang-specific path first, falls back to the root procedures.md.
func ProceduresPath(globalDir, lang string) string {
	p := filepath.Join(globalDir, "procedures", lang, "procedures.md")
	if _, err := os.Stat(p); err == nil {
		return p
	}
	return filepath.Join(globalDir, "procedures", "procedures.md")
}

// populate mirrors an embedded FS subtree to globalDir, skipping existing files.
func populate(globalDir string, fsys embed.FS, root string) {
	fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		target := filepath.Join(globalDir, root, rel)
		os.MkdirAll(filepath.Dir(target), 0o755)
		data, err := fsys.ReadFile(path)
		if err == nil {
			os.WriteFile(target, data, 0o644)
		}
		return nil
	})
}

// Bootstrap populates all embedded assets and default presets at ~/.lingtai-tui/.
func Bootstrap(globalDir string) error {
	populate(globalDir, covenantFS, "covenant")
	populate(globalDir, principleFS, "principle")
	populate(globalDir, proceduresFS, "procedures")
	populate(globalDir, templatesFS, "templates")
	populate(globalDir, recipeAssetsFS, "recipe_assets")
	// Rename recipe_assets -> recipes at the target path.
	// Unlike other populate() calls (which are merge-skip), recipes are
	// refreshed wholesale on every launch — the TUI manages this content,
	// users should not edit bundled recipe files.
	src := filepath.Join(globalDir, "recipe_assets")
	dst := filepath.Join(globalDir, "recipes")
	if _, err := os.Stat(src); err == nil {
		if err := os.RemoveAll(dst); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to remove old recipes dir: %v\n", err)
		}
		if err := os.Rename(src, dst); err != nil {
			fmt.Fprintf(os.Stderr, "warning: failed to rename recipe_assets to recipes: %v\n", err)
		}
	}
	// Templates are TUI-managed: rewritten wholesale on every launch
	// from embedded data, retired entries pruned. Saved presets in
	// presets/saved/ are user territory and never touched here.
	return RefreshTemplates()
}

// PopulateBundledLibrary extracts the TUI's embedded bundled skills into a
// stable per-user location: <globalDir>/utilities/ (typically
// ~/.lingtai-tui/utilities/). Agents reach these by default via the
// skills.paths entry in their init.json, which points at the same path.
//
// Called on every TUI startup so utility skills stay in sync with the
// shipped binary. Directory is rewritten from scratch so a TUI upgrade
// that renames or removes a utility propagates cleanly.
//
// Per-agent .library/ is owned by the kernel library capability, not by the TUI.
func PopulateBundledLibrary(globalDir string) {
	utilitiesDir := filepath.Join(globalDir, "utilities")
	os.RemoveAll(utilitiesDir)
	os.MkdirAll(utilitiesDir, 0o755)

	fs.WalkDir(skillsFS, "skills", func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel("skills", path)
		target := filepath.Join(utilitiesDir, rel)
		os.MkdirAll(filepath.Dir(target), 0o755)
		data, err := skillsFS.ReadFile(path)
		if err == nil {
			os.WriteFile(target, data, 0o644)
		}
		return nil
	})
}

// BundledSkillNames returns the set of skill directory names that are shipped
// with the TUI binary (embedded in skillsFS). Use this to distinguish
// intrinsic skills from user-created or recipe-imported ones.
func BundledSkillNames() map[string]bool {
	names := make(map[string]bool)
	entries, err := fs.ReadDir(skillsFS, "skills")
	if err != nil {
		return names
	}
	for _, e := range entries {
		if e.IsDir() {
			names[e.Name()] = true
		}
	}
	return names
}

// ReadBundledSkillFile returns the contents of a file inside a bundled skill,
// read straight from the embedded skillsFS. The skill argument is the skill
// directory name (e.g. "lingtai-tui-help"); relPath is the path inside that
// skill, using forward slashes (e.g. "assets/slash-commands.en.md"). This lets
// in-binary callers (like the TUI /help view) render bundled skill assets
// without relying on the on-disk extraction in PopulateBundledLibrary.
func ReadBundledSkillFile(skill, relPath string) (string, error) {
	data, err := skillsFS.ReadFile("skills/" + skill + "/" + relPath)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// CreationCovenant reads the existing caller-selected covenant without writes.
// A cold home uses the same compiled resource that Bootstrap would extract.
func CreationCovenant(globalDir, lang, selectedPath string) (string, error) {
	path := selectedPath
	if path == "" {
		path = CovenantPath(globalDir, lang)
	}
	data, err := os.ReadFile(path)
	if selectedPath == "" && os.IsNotExist(err) {
		data, err = covenantFS.ReadFile("covenant/" + lang + "/covenant.md")
	}
	if err != nil || len(data) == 0 || !utf8.Valid(data) {
		return "", fmt.Errorf("creation covenant unavailable")
	}
	return string(data), nil
}

// CovenantPath returns the absolute path to the covenant file for a language.
func CovenantPath(globalDir, lang string) string {
	return filepath.Join(globalDir, "covenant", lang, "covenant.md")
}

// AddonConfigRelPath returns the path (relative to the project root) where an
// addon's config file should live. This is the one place the convention
// ".lingtai/.addons/<addon>/config.json" is encoded.
func AddonConfigRelPath(addon string) string {
	return filepath.Join(".lingtai", ".addons", addon, "config.json")
}

// AddonConfigPathFromAgent returns the path (relative to an agent's working
// directory, which is <project>/.lingtai/<agent>/) to an addon's config file.
// Used in init.json's "addons.<name>.config" field — the kernel resolves these
// paths against the agent's working_dir.
func AddonConfigPathFromAgent(addon string) string {
	return filepath.Join("..", ".addons", addon, "config.json")
}

// AddonSecretsPathFromAgent returns the path (relative to an agent's working
// directory) where an addon's config file lives under the admin-local
// .secrets/ convention introduced 2026-04-16. Used by first-creation seeding
// to prefer the new path when it exists on disk.
func AddonSecretsPathFromAgent(addon string) string {
	return filepath.Join(".secrets", addon+".json")
}

// defaultMCPSpec returns the canonical wiring for one of the curated
// addons (imap / telegram / feishu / wechat / whatsapp) — the Python module to invoke,
// the env-var name the MCP reads its config path from, and the config path
// (relative to the agent working dir) to point that env var at by default.
//
// Used by GenerateInitJSONWithOpts to seed init.json's mcp.<name> activation
// entries when the wizard selects an addon. supported=false for unknown
// names so the caller skips them silently rather than emitting a spec the
// kernel would reject.
//
// Note: this is the writer-side mirror of the migration's addonSpec table
// (m028). When you add a new curated addon, update both.
func defaultMCPSpec(name string) (module, envVar, configRel string, supported bool) {
	switch name {
	case "imap":
		return "lingtai.mcp_servers.imap", "LINGTAI_IMAP_CONFIG", filepath.Join(".secrets", "imap.json"), true
	case "telegram":
		return "lingtai.mcp_servers.telegram", "LINGTAI_TELEGRAM_CONFIG", filepath.Join(".secrets", "telegram.json"), true
	case "feishu":
		return "lingtai.mcp_servers.feishu", "LINGTAI_FEISHU_CONFIG", filepath.Join(".secrets", "feishu.json"), true
	case "wechat":
		return "lingtai.mcp_servers.wechat", "LINGTAI_WECHAT_CONFIG", filepath.Join(".secrets", "wechat", "config.json"), true
	case "whatsapp":
		return "lingtai.mcp_servers.whatsapp", "LINGTAI_WHATSAPP_CONFIG", filepath.Join(".secrets", "whatsapp.json"), true
	}
	return "", "", "", false
}

// DefaultMCPSpec is the exported form of defaultMCPSpec, so diagnostics
// (e.g. /doctor's addon-secrets check) can resolve the canonical config path
// without re-hardcoding the addon→path table (fable F4: the table must stay
// single-sourced).
func DefaultMCPSpec(name string) (module, envVar, configRel string, supported bool) {
	return defaultMCPSpec(name)
}

// AutoEnvVarName builds a deterministic api_key_env slot name for a
// preset, with a number suffix that gap-fills the lowest unused index.
//
// Shape: <PROVIDER>_<N>_API_KEY (e.g. OPENAI_1_API_KEY, ANTHROPIC_2_API_KEY)
//   - PROVIDER:   uppercased manifest.llm.provider
//   - N:          the lowest positive integer not already present in
//     existingKeys (1-based). Reuses freed slots since the
//     user said API keys rapidly rotate anyway.
//
// existingKeys is the env-var-keyed map from Config.Keys — caller
// passes it in so this stays a pure function (no I/O).
//
// Returns "" when the preset has no provider — caller falls back to
// whatever api_key_env the preset already declared.
func AutoEnvVarName(p Preset, existingKeys map[string]string) string {
	llm, _ := p.Manifest["llm"].(map[string]interface{})
	provider, _ := llm["provider"].(string)
	if provider == "" {
		return ""
	}
	prefix := strings.ToUpper(provider)
	// Find the lowest unused N. We scan existingKeys for entries that
	// match `<prefix>_<int>_API_KEY` and collect the integers.
	used := map[int]bool{}
	wantPrefix := prefix + "_"
	for name := range existingKeys {
		if !strings.HasPrefix(name, wantPrefix) || !strings.HasSuffix(name, "_API_KEY") {
			continue
		}
		mid := strings.TrimSuffix(strings.TrimPrefix(name, wantPrefix), "_API_KEY")
		// Only consider pure-integer suffixes — skip things like
		// OPENAI_PERSONAL_API_KEY (no number) or OPENAI_PROD_v2_API_KEY.
		n := 0
		for _, c := range mid {
			if c < '0' || c > '9' {
				n = -1
				break
			}
			n = n*10 + int(c-'0')
		}
		if n > 0 {
			used[n] = true
		}
	}
	for n := 1; ; n++ {
		if !used[n] {
			return fmt.Sprintf("%s_%d_API_KEY", prefix, n)
		}
	}
}

// AgentOpts holds per-agent configuration values set at creation time.
type AgentOpts struct {
	EnvFile        string   // public explicit fallback; existing kernel-owned value wins
	VenvPath       string   // public explicit fallback/interpreter selection
	Language       string   // "en", "zh", or "wen"
	ContextLimit   int      // token budget
	MaxRpm         int      // API requests-per-minute cap (cooperative network gate); 0 disables
	MaxAedAttempts int      // AED (auto-error-recovery) retry attempts per message turn before fallback/sleep
	Karma          bool     // lifecycle control over other agents
	Nirvana        bool     // permanent agent destruction
	CovenantFile   string   // path to covenant file
	CommentFile    string   // path to comment file (optional)
	Addons         []string // addon names to auto-populate in init.json (e.g. ["imap", "telegram"])
	// AllowedPresets lists the absolute (or ~-prefixed) paths of every
	// preset this agent is authorized to swap to at runtime. The default
	// preset is automatically included if missing. When empty, falls back
	// to a single-element list containing just the default preset.
	AllowedPresets []string
	// PreserveActivePreset, when true, leaves manifest.preset.active alone
	// and only updates manifest.preset.default to the chosen preset. Used
	// by /setup so a running agent doesn't get yanked mid-conversation —
	// the new choice takes effect on the next AED fallback or explicit
	// revert_preset call.
	PreserveActivePreset bool
}

// DefaultAgentOpts returns sensible defaults for agent creation.
func DefaultAgentOpts() AgentOpts {
	return AgentOpts{
		Language:       "en",
		ContextLimit:   500000,
		MaxRpm:         60,
		MaxAedAttempts: DefaultMaxAedAttempts,
		Karma:          true,
		Nirvana:        false,
	}
}

// AED max-attempts validation bounds. DefaultMaxAedAttempts is the TUI
// first-run/setup default for newly generated init.json manifests. Keep this
// default explicit so setup, tests, and generated init.json agree on the same
// AED retry count.
const (
	DefaultMaxAedAttempts = 5
	MinMaxAedAttempts     = 1
	MaxMaxAedAttempts     = 100
)

// ClampAedAttempts validates a user-supplied AED max-attempts value. A value of
// zero or below (the zero value, or empty/invalid input parsed to 0) falls back
// to DefaultMaxAedAttempts; anything above MaxMaxAedAttempts is clamped down to
// the ceiling. The result is always within [MinMaxAedAttempts, MaxMaxAedAttempts].
func ClampAedAttempts(n int) int {
	if n < MinMaxAedAttempts {
		return DefaultMaxAedAttempts
	}
	if n > MaxMaxAedAttempts {
		return MaxMaxAedAttempts
	}
	return n
}

// GenerateInitJSON creates a full init.json from a preset using default opts.
func GenerateInitJSON(p Preset, agentName, dirName, lingtaiDir, globalDir string) error {
	opts := DefaultAgentOpts()
	// Inherit language from preset if set
	if l, ok := p.Manifest["language"].(string); ok && l != "" {
		opts.Language = l
	}
	return GenerateInitJSONWithOpts(p, agentName, dirName, lingtaiDir, globalDir, opts)
}

// SyncCapabilityAPIKeyEnv propagates the LLM's api_key_env to any
// capability whose provider matches the LLM provider. This ensures
// capabilities that name the agent's own provider use the same resolved
// env var slot (e.g. "OPENAI_1_API_KEY") rather than a stale preset
// placeholder (e.g. "OPENAI_API_KEY"). Capabilities declared as
// "inherit" are expanded by the kernel and need no rewrite here.
func SyncCapabilityAPIKeyEnv(manifest map[string]interface{}) {
	llm, _ := manifest["llm"].(map[string]interface{})
	if llm == nil {
		return
	}
	llmProvider, _ := llm["provider"].(string)
	llmKeyEnv, _ := llm["api_key_env"].(string)
	if llmProvider == "" || llmKeyEnv == "" {
		return
	}
	caps, _ := manifest["capabilities"].(map[string]interface{})
	if caps == nil {
		return
	}
	for _, cfg := range caps {
		capMap, ok := cfg.(map[string]interface{})
		if !ok {
			continue
		}
		capProvider, _ := capMap["provider"].(string)
		if capProvider != llmProvider {
			continue
		}
		capMap["api_key_env"] = llmKeyEnv
	}
}

// retiredLLMFields are manifest.llm keys the kernel no longer reads. The TUI
// never writes them: the preset editor drops them on commit and init.json
// generation drops them from the copied llm block. Existing files are not
// rewritten just to remove them.
var retiredLLMFields = []string{"api_compat"}

// StripRetiredLLMFields deletes retired keys (see retiredLLMFields) from an
// llm map in place. A nil map is a no-op.
func StripRetiredLLMFields(llm map[string]interface{}) {
	for _, key := range retiredLLMFields {
		delete(llm, key)
	}
}

// stripObsoleteInitFields removes top-level init.json fields that the kernel
// treats as ignored legacy input. New writers and explicit read-modify-write
// paths must not carry them forward, because their presence triggers a
// deterministic boot-time nudge before the agent can do useful work.
func stripObsoleteInitFields(initJSON map[string]interface{}) {
	delete(initJSON, "principle_file")
	delete(initJSON, "procedures_file")
}

// GenerateInitJSONWithOpts is the existing caller adapter, not a Go merger.
// A persisted real preset reference is required; Keep-current is explicit.
func GenerateInitJSONWithOpts(p Preset, agentName, dirName, lingtaiDir, globalDir string, opts AgentOpts, python ...string) error {
	cmd := config.LingtaiCmd(globalDir)
	if len(python) > 0 {
		cmd = python[0]
	}
	_, err := configureWithOpts(p, agentName, dirName, lingtaiDir, globalDir, opts, cmd)
	return err
}

// PropagatePresetPolicy delegates authorization/reconciliation to Core.
// Empty allowed remains the wizard's deliberate no-op, not a reset policy.
func PropagatePresetPolicy(lingtaiDir, skipDir, defaultRef string, allowed []string, python ...string) (int, error) {
	if len(allowed) == 0 {
		return 0, nil
	}
	cmd := ""
	if len(python) > 0 {
		cmd = python[0]
	} else {
		globalDir, err := config.GlobalDirPath()
		if err != nil {
			return 0, fmt.Errorf("Project kernel interpreter unavailable")
		}
		cmd = config.LingtaiCmd(globalDir)
	}
	cmd = process.ProjectPython(filepath.Join(lingtaiDir, skipDir), cmd)
	result, err := process.PropagateProjectPolicy(context.Background(), cmd, filepath.Dir(lingtaiDir), skipDir, defaultRef, allowed)
	return result.Updated, err
}
