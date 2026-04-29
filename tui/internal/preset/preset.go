package preset

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/config"
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

//go:embed all:soul
var soulFS embed.FS

//go:embed all:recipe_assets
var recipeAssetsFS embed.FS

//go:embed all:skills
var skillsFS embed.FS

// Preset is a reusable agent template stored at ~/.lingtai-tui/presets/.
//
// Description is a structured object with a required `summary` and an
// optional `tier` (cost/quality ladder, "1".."5"). Authors may add
// arbitrary extra keys (gains/loses/recommended_for/...); they round-trip
// through `Description.Extra`.
type Preset struct {
	Name        string                 `json:"name"`
	Description PresetDescription      `json:"description"`
	Manifest    map[string]interface{} `json:"manifest"`
}

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
	if err := json.Unmarshal(data, &asMap); err != nil {
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

// PresetsDir returns ~/.lingtai-tui/presets/.
func PresetsDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, config.GlobalDirName, "presets")
}

// List returns all presets from the presets directory.
func List() ([]Preset, error) {
	dir := PresetsDir()
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read presets dir: %w", err)
	}
	var presets []Preset
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		// Skip the kernel-side migration meta file — it sits in the same
		// directory as preset files but is not itself a preset.
		if e.Name() == "_kernel_meta.json" {
			continue
		}
		p, err := Load(e.Name()[:len(e.Name())-5]) // strip .json
		if err != nil {
			continue
		}
		presets = append(presets, p)
	}
	// Saved presets first (alphabetically), then builtins (minimax first)
	sort.Slice(presets, func(i, j int) bool {
		bi, bj := IsBuiltin(presets[i].Name), IsBuiltin(presets[j].Name)
		if bi != bj {
			return !bi // saved (non-builtin) before builtin
		}
		if bi { // both builtin: minimax → zhipu → mimo → deepseek → openrouter → codex → custom
			order := map[string]int{"minimax": 0, "zhipu": 1, "mimo": 2, "deepseek": 3, "openrouter": 4, "codex": 5, "custom": 6}
			return order[presets[i].Name] < order[presets[j].Name]
		}
		return presets[i].Name < presets[j].Name
	})
	return presets, nil
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

// Load reads a single preset by name.
func Load(name string) (Preset, error) {
	path := filepath.Join(PresetsDir(), name+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return Preset{}, fmt.Errorf("read preset %s: %w", name, err)
	}
	var p Preset
	if err := json.Unmarshal(data, &p); err != nil {
		return Preset{}, fmt.Errorf("parse preset %s: %w", name, err)
	}
	return p, nil
}

// validTiers mirrors the kernel-side TIER_VALUES in lingtai/presets.py.
var validTiers = map[string]bool{"1": true, "2": true, "3": true, "4": true, "5": true}

// Validate returns the list of rule violations for this preset. Mirrors the
// kernel's load_preset validation gauntlet so the editor refuses to save
// anything the kernel will refuse to load. Empty slice = passes.
func (p Preset) Validate() []error {
	var errs []error
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

// Save writes a preset to the presets directory.
func Save(p Preset) error {
	dir := PresetsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create presets dir: %w", err)
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
		json.Unmarshal(data, &manifest)
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

// Delete removes a preset file.
func Delete(name string) error {
	path := filepath.Join(PresetsDir(), name+".json")
	return os.Remove(path)
}

// EnsureDefaults creates all built-in presets if no presets exist.
func EnsureDefault() error {
	presets, _ := List()
	if len(presets) > 0 {
		return nil
	}
	for _, p := range BuiltinPresets() {
		if err := Save(p); err != nil {
			return err
		}
	}
	return nil
}

// SeedMissingBuiltins writes any built-in preset whose <name>.json file does
// not yet exist in the presets directory. Unlike EnsureDefault, this runs
// even when the user already has presets — it's how new built-ins ship to
// existing installs without clobbering user-saved variants (e.g. zhipu_cn).
// A user who has explicitly deleted a built-in will see it reappear; if that
// becomes a concern we can add a "deleted" marker file later.
func SeedMissingBuiltins() error {
	dir := PresetsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create presets dir: %w", err)
	}
	for _, p := range BuiltinPresets() {
		path := filepath.Join(dir, p.Name+".json")
		if _, err := os.Stat(path); err == nil {
			continue // already exists
		}
		if err := Save(p); err != nil {
			return err
		}
	}
	return nil
}

// BuiltinPresets returns the built-in presets.
func BuiltinPresets() []Preset {
	return []Preset{
		minimaxPreset(),
		zhipuPreset(),
		mimoPreset(),
		deepseekPreset(),
		openrouterPreset(),
		codexPreset(),
		customPreset(),
	}
}

// builtinNames is the set of built-in preset names.
var builtinNames = map[string]bool{
	"minimax":     true,
	"zhipu":       true,
	"mimo":        true,
	"deepseek":    true,
	"openrouter":  true,
	"codex":       true,
	"codex_oauth": true,
	"custom":      true,
}

// IsBuiltin returns true if name matches a built-in preset template.
func IsBuiltin(name string) bool {
	return builtinNames[name]
}

// SavedCount returns the number of non-builtin (saved) presets in the list.
func SavedCount(presets []Preset) int {
	n := 0
	for _, p := range presets {
		if !IsBuiltin(p.Name) {
			n++
		}
	}
	return n
}

func e() map[string]interface{} { return map[string]interface{}{} }

// libraryDefault returns the default library capability config — two Tier 1
// paths: the network-shared library (resolved relative to the agent dir)
// and the TUI's per-user utilities directory. Users can edit init.json to
// add or remove paths; init.json is the ground truth and the capability
// reads it on every setup.
func libraryDefault() map[string]interface{} {
	return map[string]interface{}{
		"paths": []interface{}{
			"../.library_shared",
			"~/.lingtai-tui/utilities",
		},
	}
}

func minimaxPreset() Preset {
	mm := map[string]interface{}{"provider": "minimax", "api_key_env": "MINIMAX_API_KEY"}
	return Preset{
		Name:        "minimax",
		Description: PresetDescription{Summary: "MiniMax M2.7 — full multimodal capabilities"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "minimax", "model": "MiniMax-M2.7-highspeed",
				"api_key": nil, "api_key_env": "MINIMAX_API_KEY", "base_url": nil,
			},
			"capabilities": map[string]interface{}{
				"file": e(), "email": e(), "bash": map[string]interface{}{"yolo": true},
				"web_search": mm, "psyche": e(), "codex": e(),
				"vision": mm, "avatar": e(), "daemon": e(),
				"library": libraryDefault(),
			},
			"admin": map[string]interface{}{"karma": true},
			"streaming": false,
		},
	}
}

func zhipuPreset() Preset {
	zp := map[string]interface{}{"provider": "zhipu", "api_key_env": "ZHIPU_API_KEY"}
	return Preset{
		Name:        "zhipu",
		Description: PresetDescription{Summary: "Zhipu GLM Coding Plan — OpenAI-compatible"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "zhipu", "model": "GLM-5.1",
				"api_key": nil, "api_key_env": "ZHIPU_API_KEY",
				"base_url": nil, "api_compat": "openai",
			},
			"capabilities": map[string]interface{}{
				"file": e(), "email": e(), "bash": map[string]interface{}{"yolo": true},
				"web_search": zp, "psyche": e(), "codex": e(),
				"vision": zp,
				"avatar": e(), "daemon": e(),
				"library": libraryDefault(),
			},
			"admin":     map[string]interface{}{"karma": true},
			"streaming": false,
		},
	}
}

func mimoPreset() Preset {
	// mimo-v2.5 is the sweet spot: 1M context, vision-capable, supports tool
	// calls and thinking mode. Cheaper-but-text-only siblings (mimo-v2.5-pro,
	// mimo-v2-flash) are documented in the xiaomi-mimo skill — users clone
	// this preset to switch. Among the models the TUI exposes (v2.5, v2.5-pro,
	// v2-flash), only v2.5 supports vision; pro/flash will 400 on image input.
	// Vision uses the first-class MiMoVisionService (kernel: services/vision/mimo.py).
	mp := map[string]interface{}{
		"provider":    "mimo",
		"api_key_env": "XIAOMI_API_KEY",
		"model":       "mimo-v2.5",
	}
	return Preset{
		Name:        "mimo",
		Description: PresetDescription{Summary: "Xiaomi MiMo V2.5 — OpenAI-compatible, 1M context, vision + tools"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "mimo", "model": "mimo-v2.5",
				"api_key": nil, "api_key_env": "XIAOMI_API_KEY",
				"base_url": "https://api.xiaomimimo.com/v1", "api_compat": "openai",
			},
			"capabilities": map[string]interface{}{
				"file": e(), "email": e(), "bash": map[string]interface{}{"yolo": true},
				"web_search": map[string]interface{}{"provider": "duckduckgo"},
				"psyche":     e(), "codex": e(),
				"vision":     mp,
				"avatar":     e(), "daemon": e(),
				"library":    libraryDefault(),
			},
			"admin":     map[string]interface{}{"karma": true},
			"streaming": false,
		},
	}
}

func deepseekPreset() Preset {
	return Preset{
		Name:        "deepseek",
		Description: PresetDescription{Summary: "DeepSeek V4 — OpenAI-compatible, 1M context window, tool calls"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "deepseek", "model": "deepseek-v4-flash",
				"api_key": nil, "api_key_env": "DEEPSEEK_API_KEY",
				"base_url": "https://api.deepseek.com", "api_compat": "openai",
			},
			// DeepSeek's public API is text-only — no media generation. For
			// audio analysis (transcription, music critique), use the `listen`
			// skill; for media creation, register the MiniMax-Media MCP server
			// via the `lingtai-mcp` skill.
			"capabilities": map[string]interface{}{
				"file": e(), "email": e(), "bash": map[string]interface{}{"yolo": true},
				"web_search": map[string]interface{}{"provider": "duckduckgo"},
				"psyche": e(), "codex": e(),
				"avatar": e(), "daemon": e(),
				"library": libraryDefault(),
			},
			"admin":     map[string]interface{}{"karma": true},
			"streaming": false,
		},
	}
}

func openrouterPreset() Preset {
	return Preset{
		Name:        "openrouter",
		Description: PresetDescription{Summary: "OpenRouter — gateway to DeepSeek, GLM, Qwen, MiniMax, Kimi, Claude, ..."},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "openrouter", "model": "z-ai/glm-5.1",
				"api_key": nil, "api_key_env": "OPENROUTER_API_KEY",
				"base_url": nil,
			},
			// OpenRouter is a text-only /chat/completions gateway — no media
			// generation. For audio analysis use the `listen` skill; for
			// media creation register a provider's MCP server via `lingtai-mcp`.
			"capabilities": map[string]interface{}{
				"file": e(), "email": e(), "bash": map[string]interface{}{"yolo": true},
				"web_search": map[string]interface{}{"provider": "duckduckgo"},
				"psyche": e(), "codex": e(),
				"avatar": e(), "daemon": e(),
				"library": libraryDefault(),
			},
			"admin":     map[string]interface{}{"karma": true},
			"streaming": false,
		},
	}
}

func codexPreset() Preset {
	cx := map[string]interface{}{"provider": "codex", "api_key_env": ""}
	return Preset{
		Name:        "codex",
		Description: PresetDescription{Summary: "ChatGPT account — vision + web search + tools"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "codex", "model": "gpt-5.4",
				"api_key": nil, "api_key_env": "",
				"base_url": "https://chatgpt.com/backend-api",
			},
			"capabilities": map[string]interface{}{
				"file": e(), "email": e(), "bash": map[string]interface{}{"yolo": true},
				"web_search": cx, "psyche": e(), "codex": e(),
				"vision": cx,
				"avatar": e(), "daemon": e(),
				"library": libraryDefault(),
			},
			"admin":     map[string]interface{}{"karma": true},
			"streaming": false,
		},
	}
}

func customPreset() Preset {
	return Preset{
		Name:        "custom",
		Description: PresetDescription{Summary: "OpenAI-compatible API — full capabilities"},
		Manifest: map[string]interface{}{
			"llm": map[string]interface{}{
				"provider": "custom", "model": "",
				"api_key": nil, "api_key_env": "LLM_API_KEY", "base_url": nil,
			},
			"capabilities": map[string]interface{}{
				"file": e(), "email": e(), "bash": map[string]interface{}{"yolo": true},
				"web_search": e(), "psyche": e(), "codex": e(),
				"avatar": e(), "daemon": e(),
				"library": libraryDefault(),
			},
			"admin": map[string]interface{}{"karma": true},
			"streaming": false,
		},
	}
}

// PrinciplePath returns the absolute path to the principle file for a language.
func PrinciplePath(globalDir, lang string) string {
	return filepath.Join(globalDir, "principle", lang, "principle.md")
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
	populate(globalDir, soulFS, "soul")
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
	if err := EnsureDefault(); err != nil {
		return err
	}
	// Also seed any built-ins that were added after the user's first install.
	// Idempotent: only writes files that don't exist.
	return SeedMissingBuiltins()
}

// PopulateBundledLibrary extracts the TUI's embedded bundled skills into a
// stable per-user location: <globalDir>/utilities/ (typically
// ~/.lingtai-tui/utilities/). Agents reach these by default via the
// library.paths entry in their init.json, which points at the same path.
//
// Called on every TUI startup so utility skills stay in sync with the
// shipped binary. Directory is rewritten from scratch so a TUI upgrade
// that renames or removes a utility propagates cleanly.
//
// The lingtaiDir argument is retained for compatibility with callers
// (main.go, launcher.go) and is currently unused. Per-agent .library/
// is now owned by the kernel library capability, not by the TUI.
func PopulateBundledLibrary(lingtaiDir, globalDir string) {
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

// CovenantPath returns the absolute path to the covenant file for a language.
func CovenantPath(globalDir, lang string) string {
	return filepath.Join(globalDir, "covenant", lang, "covenant.md")
}

// SoulFlowPath returns the absolute path to the soul flow file for a language.
func SoulFlowPath(globalDir, lang string) string {
	return filepath.Join(globalDir, "soul", lang, "soul-flow.md")
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

// defaultMCPSpec returns the canonical wiring for one of the four curated
// addons (imap / telegram / feishu / wechat) — the Python module to invoke,
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
		return "lingtai_imap", "LINGTAI_IMAP_CONFIG", filepath.Join(".secrets", "imap.json"), true
	case "telegram":
		return "lingtai_telegram", "LINGTAI_TELEGRAM_CONFIG", filepath.Join(".secrets", "telegram.json"), true
	case "feishu":
		return "lingtai_feishu", "LINGTAI_FEISHU_CONFIG", filepath.Join(".secrets", "feishu.json"), true
	case "wechat":
		return "lingtai_wechat", "LINGTAI_WECHAT_CONFIG", filepath.Join(".secrets", "wechat", "config.json"), true
	}
	return "", "", "", false
}

// DefaultPreset returns the first built-in preset (minimax).
func DefaultPreset() Preset {
	return minimaxPreset()
}

// AutoEnvVarName builds a deterministic api_key_env slot name for a
// preset, with a number suffix that gap-fills the lowest unused index.
//
// Shape: <PROVIDER>[_<REGION>]_<N>_API_KEY
//   - PROVIDER:   uppercased manifest.llm.provider
//   - REGION:     "CN" or "INTL" for minimax/zhipu (read from base_url);
//                 omitted for other providers
//   - N:          the lowest positive integer not already present in
//                 existingKeys (1-based). Reuses freed slots since the
//                 user said API keys rapidly rotate anyway.
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
	if region := regionSuffix(provider, llmString(llm, "base_url")); region != "" {
		prefix += "_" + region
	}
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
		// MINIMAX_PERSONAL_API_KEY (no number) or MINIMAX_PROD_v2_API_KEY.
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

// regionSuffix returns "CN" / "INTL" for providers with regional
// splits, "" for everything else. Mirrors the wizard's existing
// region-detection logic so a preset that says "minimaxi.com" gets
// the same CN suffix the wizard would have applied.
func regionSuffix(provider, baseURL string) string {
	switch provider {
	case "minimax":
		if strings.Contains(baseURL, "minimaxi.com") {
			return "CN"
		}
		return "INTL"
	case "zhipu":
		if strings.Contains(baseURL, "api.z.ai") {
			return "INTL"
		}
		return "CN"
	}
	return ""
}

// llmString is a tiny accessor that returns a string field from an
// llm map without panicking on missing keys or wrong types.
func llmString(llm map[string]interface{}, key string) string {
	v, _ := llm[key].(string)
	return v
}

// AgentOpts holds per-agent configuration values set at creation time.
type AgentOpts struct {
	Language      string   // "en", "zh", or "wen"
	Stamina       float64  // max uptime in seconds
	ContextLimit  int      // token budget
	SoulDelay     float64  // seconds between soul cycles
	MoltPressure  float64  // 0–1 ratio triggering molt
	MaxRpm        int      // API requests-per-minute cap (cooperative network gate); 0 disables
	Karma         bool     // lifecycle control over other agents
	Nirvana       bool     // permanent agent destruction
	CovenantFile   string   // path to covenant file
	PrincipleFile  string   // path to principle file
	ProceduresFile string   // path to procedures file
	BriefFile      string   // path to brief file (externally maintained by secretary)
	SoulFile       string   // path to soul flow file
	CommentFile   string   // path to comment file (optional)
	Addons        []string // addon names to auto-populate in init.json (e.g. ["imap", "telegram"])
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
		Language:     "en",
		Stamina:      36000,
		ContextLimit: 200000,
		SoulDelay:    999999,
		MoltPressure: 0.8,
		MaxRpm:       60,
		Karma:        true,
		Nirvana:      false,
	}
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

// GenerateInitJSONWithOpts creates a full init.json from a preset with explicit agent options.
func GenerateInitJSONWithOpts(p Preset, agentName, dirName, lingtaiDir, globalDir string, opts AgentOpts) error {
	agentDir := filepath.Join(lingtaiDir, dirName)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		return fmt.Errorf("create agent dir: %w", err)
	}

	// Build manifest with opts
	manifest := make(map[string]interface{})
	manifest["agent_name"] = agentName
	lang := opts.Language
	if lang == "" {
		lang = "en"
	}
	manifest["language"] = lang
	if llm, ok := p.Manifest["llm"]; ok {
		manifest["llm"] = llm
	}
	if caps, ok := p.Manifest["capabilities"]; ok {
		manifest["capabilities"] = caps
	}
	manifest["admin"] = map[string]interface{}{
		"karma":   opts.Karma,
		"nirvana": opts.Nirvana,
	}
	manifest["soul"] = map[string]interface{}{"delay": opts.SoulDelay}
	manifest["stamina"] = opts.Stamina
	manifest["context_limit"] = opts.ContextLimit
	manifest["molt_pressure"] = opts.MoltPressure
	manifest["molt_prompt"] = ""
	manifest["max_turns"] = 100
	manifest["max_rpm"] = opts.MaxRpm
	manifest["streaming"] = false
	// Track which preset this agent was created from. The kernel reads this
	// at boot to materialize manifest.llm + manifest.capabilities from the
	// referenced preset file. As of the path-as-name redesign, the value is
	// the preset's full path (in ~/... shorthand for portability across
	// machines), not its filename stem. The agent passes this same string
	// to system(action='refresh', preset='<path>') to swap.
	// The 'default' field is used by AED auto-fallback to revert to the
	// original preset when the active one keeps failing.
	if p.Name != "" {
		presetRef := "~/.lingtai-tui/presets/" + p.Name + ".json"
		// Default behavior: both active and default point at the new
		// preset (the agent runs on the chosen preset immediately).
		// /setup mode (PreserveActivePreset=true) only updates default,
		// so the running agent keeps its current preset until an AED
		// fallback or explicit revert_preset takes effect.
		activeRef := presetRef
		if opts.PreserveActivePreset {
			existingInitPath := filepath.Join(agentDir, "init.json")
			if data, err := os.ReadFile(existingInitPath); err == nil {
				var existing map[string]interface{}
				if json.Unmarshal(data, &existing) == nil {
					if mn, ok := existing["manifest"].(map[string]interface{}); ok {
						if pre, ok := mn["preset"].(map[string]interface{}); ok {
							if cur, ok := pre["active"].(string); ok && cur != "" {
								activeRef = cur
							}
						}
					}
				}
			}
		}
		manifest["preset"] = map[string]interface{}{
			"active":  activeRef,
			"default": presetRef,
		}
	}

	// Resolve file paths — use opts if set, fallback to language defaults
	covenantFile := opts.CovenantFile
	if covenantFile == "" {
		covenantFile = CovenantPath(globalDir, lang)
	}
	principleFile := opts.PrincipleFile
	if principleFile == "" {
		principleFile = PrinciplePath(globalDir, lang)
	}
	proceduresFile := opts.ProceduresFile
	if proceduresFile == "" {
		proceduresFile = ProceduresPath(globalDir, lang)
	}
	soulFile := opts.SoulFile
	if soulFile == "" {
		soulFile = SoulFlowPath(globalDir, lang)
	}

	// Load existing init.json addons + mcp fields so we preserve them across
	// regens. Critical for /setup: when the user changes non-addon settings,
	// existing addon registrations and MCP activations must not be dropped.
	// User edits always win over opts.Addons — opts only seeds the fields
	// on first creation.
	//
	// Reads both shapes for back-compat with init.json files written by older
	// TUIs (pre-v0.7.3 wrote a dict; new TUIs write a list). Both shapes get
	// converted to the new list-of-names form before re-writing, so the on-
	// disk file is normalized on the next refresh.
	var existingAddonsList []interface{}
	var existingMCP map[string]interface{}
	existingInitPath := filepath.Join(agentDir, "init.json")
	if existingData, err := os.ReadFile(existingInitPath); err == nil {
		var existing map[string]interface{}
		if json.Unmarshal(existingData, &existing) == nil {
			switch v := existing["addons"].(type) {
			case []interface{}:
				existingAddonsList = v
			case map[string]interface{}:
				// Legacy dict shape — extract just the names.
				for name := range v {
					existingAddonsList = append(existingAddonsList, name)
				}
			}
			if mcp, ok := existing["mcp"].(map[string]interface{}); ok && len(mcp) > 0 {
				existingMCP = mcp
			}
		}
	}

	initJSON := map[string]interface{}{
		"manifest":         manifest,
		"covenant_file":    covenantFile,
		"principle_file":   principleFile,
		"procedures_file":  proceduresFile,
		"soul_file":        soulFile,
		"env_file":       config.EnvFilePath(globalDir),
		"venv_path":      filepath.Join(globalDir, "runtime", "venv"),
		"pad":            "",
		"prompt":         "",
	}

	// Decide which addons to wire.
	//
	// Precedence:
	//   1. Pre-existing addons:[...] in init.json (preserved verbatim — user
	//      edits win).
	//   2. Otherwise, opts.Addons from the caller (the wizard's selection).
	//
	// The list is normalized to the new shape (list of curated MCP names).
	// The kernel's `mcp` capability decompresses each name from the catalog
	// into the per-agent mcp_registry.jsonl on boot.
	var addonsList []interface{}
	if existingAddonsList != nil {
		addonsList = existingAddonsList
	} else {
		for _, name := range opts.Addons {
			addonsList = append(addonsList, name)
		}
	}
	if addonsList != nil {
		initJSON["addons"] = addonsList
	}

	// Build the mcp activation map for any addon name in the list. Each entry
	// points at the local venv python (where `pip install lingtai` placed the
	// MCP packages) running `python -m lingtai_<name>` with the canonical
	// LINGTAI_<NAME>_CONFIG env var set to the .secrets/<name>.json convention.
	//
	// Pre-existing mcp.<name> entries take precedence — humans who customized
	// the spec (e.g., switched to a different Python or added env vars) keep
	// their settings.
	if len(addonsList) > 0 {
		venvPython := config.VenvPython(filepath.Join(globalDir, "runtime", "venv"))
		mcpField := make(map[string]interface{})
		for k, v := range existingMCP {
			mcpField[k] = v
		}
		for _, raw := range addonsList {
			name, ok := raw.(string)
			if !ok || name == "" {
				continue
			}
			if _, exists := mcpField[name]; exists {
				continue // user-set entry wins
			}
			module, envVar, configRel, supported := defaultMCPSpec(name)
			if !supported {
				continue // unknown name — let the kernel surface the warning
			}
			mcpField[name] = map[string]interface{}{
				"type":    "stdio",
				"command": venvPython,
				"args":    []interface{}{"-m", module},
				"env":     map[string]interface{}{envVar: configRel},
			}
		}
		if len(mcpField) > 0 {
			initJSON["mcp"] = mcpField
		}
	}

	// Comment file — only if user specified one
	if opts.CommentFile != "" {
		initJSON["comment_file"] = opts.CommentFile
	}

	// Brief file — externally maintained by the secretary agent.
	// Only set for admin agents (karma=true); avatars don't need it.
	if opts.BriefFile != "" && opts.Karma {
		initJSON["brief_file"] = opts.BriefFile
	}

	data, err := json.MarshalIndent(initJSON, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal init.json: %w", err)
	}

	initPath := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(initPath, data, 0o644); err != nil {
		return fmt.Errorf("write init.json: %w", err)
	}

	// Also create .agent.json manifest for the agent
	agentManifest := map[string]interface{}{
		"agent_name": agentName,
		"address":    filepath.Base(agentDir),
		"state":      "",
		"admin": map[string]interface{}{
			"karma":   opts.Karma,
			"nirvana": opts.Nirvana,
		},
	}

	// Create mailbox structure
	for _, sub := range []string{
		"mailbox/inbox",
		"mailbox/sent",
		"mailbox/archive",
	} {
		os.MkdirAll(filepath.Join(agentDir, sub), 0o755)
	}

	mdata, _ := json.MarshalIndent(agentManifest, "", "  ")
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), mdata, 0o644)

	return nil
}

