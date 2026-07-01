package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// MigrateLegacyLanguage moves Language from config.json to tui_config.json if needed.
func MigrateLegacyLanguage(globalDir string) {
	cfg, err := LoadConfig(globalDir)
	if err != nil || cfg.Language == "" {
		return
	}
	tc := LoadTUIConfig(globalDir)
	if tc.Language == "en" || tc.Language == "" {
		// Only migrate if tui_config hasn't been explicitly set
		tcPath := filepath.Join(globalDir, "tui_config.json")
		if _, err := os.Stat(tcPath); os.IsNotExist(err) {
			tc.Language = cfg.Language
			SaveTUIConfig(globalDir, tc)
		}
	}
}

// GlobalDirName is the name of the global config directory under $HOME.
const GlobalDirName = ".lingtai-tui"

type Config struct {
	// Keys maps **env-var name** → key value, e.g. {"MINIMAX_API_KEY": "xxx"}.
	// Each preset declares which env var holds its key via
	// manifest.llm.api_key_env, and the TUI writes that exact name into
	// ~/.lingtai-tui/.env. This lets one provider serve multiple presets
	// with different keys (e.g. a personal vs work minimax account
	// stored under MINIMAX_API_KEY and MINIMAX_WORK_KEY).
	//
	// Legacy entries keyed by provider name (lowercase) get translated
	// to the canonical env var name on read via migrateLegacyProviderKeys.
	Keys     map[string]string `json:"keys,omitempty"`
	Language string            `json:"language,omitempty"` // deprecated: use TUIConfig.Language
}

// legacyProviderEnvVars is the *one-shot migration* lookup that
// translates pre-2026-04 Config.Keys entries (keyed by provider name)
// to canonical env var names. New writes always go directly to the
// env var name from the preset's api_key_env field — never through
// this map. Do not extend; new providers should not appear here.
var legacyProviderEnvVars = map[string]string{
	"minimax":    "MINIMAX_API_KEY",
	"zhipu":      "ZHIPU_API_KEY",
	"mimo":       "MIMO_API_KEY",
	"deepseek":   "DEEPSEEK_API_KEY",
	"openrouter": "OPENROUTER_API_KEY",
}

// migrateLegacyProviderKeys rewrites entries keyed by lowercase
// provider names (the pre-refactor shape) to their canonical env var
// name. Called from LoadConfig so callers always see the new shape.
func migrateLegacyProviderKeys(cfg *Config) {
	if cfg.Keys == nil {
		return
	}
	for provider, envKey := range legacyProviderEnvVars {
		val, hasLegacy := cfg.Keys[provider]
		if !hasLegacy {
			continue
		}
		// Don't clobber an explicit env-var-keyed entry that already exists.
		if _, hasNew := cfg.Keys[envKey]; !hasNew {
			cfg.Keys[envKey] = val
		}
		delete(cfg.Keys, provider)
	}
}

// TUIConfig holds global TUI preferences at ~/.lingtai-tui/tui_config.json.
type TUIConfig struct {
	Language     string `json:"language"`
	MailPageSize int    `json:"mail_page_size"`
	Theme        string `json:"theme,omitempty"` // theme name: "ink-dark" (default), etc.
	Insights     bool   `json:"insights"`
	// ToolCallTruncate is the max number of characters shown per tool_call /
	// tool_result line in the transcript. 0 (the default) means no truncation —
	// full content is shown. A positive value caps each tool line and the
	// renderer appends a "… (+N chars)" indicator. Stored as omitempty so the
	// untruncated default leaves no key in tui_config.json.
	ToolCallTruncate int `json:"tool_call_truncate,omitempty"`
	// AutoRefreshOff disables the 1s auto-refresh of reloadable views. Auto
	// refresh is ON by default, so this is stored as an inverse flag with
	// omitempty: the default (enabled) leaves no key in tui_config.json, and
	// only an explicit opt-out writes "auto_refresh_off": true. Read via
	// AutoRefreshEnabled() rather than this field directly.
	AutoRefreshOff bool `json:"auto_refresh_off,omitempty"`
}

// AutoRefreshEnabled reports whether reloadable views should auto-refresh on
// the 1s tick. Auto refresh is the default; only an explicit opt-out
// (auto_refresh_off=true) turns it off.
func (tc TUIConfig) AutoRefreshEnabled() bool { return !tc.AutoRefreshOff }

// DefaultTUIConfig returns sensible defaults.
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Language:     "en",
		MailPageSize: 200,
		Insights:     false,
	}
}

// LoadTUIConfig reads ~/.lingtai-tui/tui_config.json.
func LoadTUIConfig(globalDir string) TUIConfig {
	data, err := os.ReadFile(filepath.Join(globalDir, "tui_config.json"))
	if err != nil {
		return DefaultTUIConfig()
	}
	var tc TUIConfig
	if err := json.Unmarshal(data, &tc); err != nil {
		return DefaultTUIConfig()
	}
	if tc.Language == "" {
		tc.Language = "en"
	}
	if tc.MailPageSize > 0 && tc.MailPageSize < 100 {
		tc.MailPageSize = 200 // migrate old values below minimum
	}
	// Insights defaults to false when absent from JSON.
	// No override needed — zero value of bool is false.
	return tc
}

// SaveTUIConfig writes ~/.lingtai-tui/tui_config.json.
func SaveTUIConfig(globalDir string, tc TUIConfig) error {
	data, err := json.MarshalIndent(tc, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(globalDir, "tui_config.json"), data, 0o644)
}

func GlobalDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, GlobalDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func LoadConfig(dir string) (Config, error) {
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if os.IsNotExist(err) {
		return Config{}, nil
	}
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	migrateLegacyProviderKeys(&cfg)
	return cfg, nil
}

func SaveConfig(dir string, cfg Config) error {
	os.MkdirAll(dir, 0o755)
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		return err
	}
	return WriteEnvFile(dir, cfg)
}

// EnvFilePath returns the path to the global .env file.
func EnvFilePath(globalDir string) string {
	return filepath.Join(globalDir, ".env")
}

// SoulFlowEnabledEnvVar is the env var the kernel reads to decide
// whether soul flow (proactive autonomous action on idle) is enabled.
// It is opt-in: unset/empty/other = disabled. The kernel treats
// {1,true,yes,on} (case-insensitive) as enabled. See kernel
// flow.py:SOUL_FLOW_ENABLED_ENV. The TUI surfaces this as a wizard
// toggle and persists it into ~/.lingtai-tui/.env via SetEnvVar so the
// agent inherits it at boot through env_file.
const SoulFlowEnabledEnvVar = "LINGTAI_SOUL_FLOW_ENABLED"

// parseEnvKey reports whether a raw .env line is a `KEY=VALUE`
// assignment and, if so, returns its key. Comment lines (leading `#`)
// and blank lines are reported as non-assignments so callers preserve
// them verbatim.
func parseEnvKey(line string) (key string, isAssignment bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	eq := strings.IndexByte(line, '=')
	if eq <= 0 {
		return "", false
	}
	return strings.TrimSpace(line[:eq]), true
}

// readEnvLines reads the .env file into a slice of raw lines. A missing
// file yields an empty slice with no error, so callers can treat "no
// file" and "empty file" identically. A trailing empty element caused
// by a final newline is dropped.
func readEnvLines(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	content := strings.TrimRight(string(data), "\n")
	if content == "" {
		return nil, nil
	}
	return strings.Split(content, "\n"), nil
}

// writeEnvLines writes lines back to the .env file with a single
// trailing newline and 0600 permissions. When the file already exists
// its existing permission bits are preserved rather than reset to 0600,
// so a user who tightened them keeps their choice.
func writeEnvLines(path string, lines []string) error {
	perm := os.FileMode(0o600)
	if info, err := os.Stat(path); err == nil {
		perm = info.Mode().Perm()
	}
	// Ensure the parent (global) dir exists — GenerateInitJSONWithOpts may
	// write the .env before the wizard's GlobalDir() MkdirAll runs (and
	// tests point globalDir at a not-yet-created temp path).
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := strings.Join(lines, "\n")
	if body != "" {
		body += "\n"
	}
	return os.WriteFile(path, []byte(body), perm)
}

// WriteEnvFile writes API keys from config to ~/.lingtai-tui/.env while
// preserving any unmanaged lines already present in the file (comments,
// blank lines, and env vars not owned by Config.Keys — most notably
// LINGTAI_SOUL_FLOW_ENABLED, which the wizard writes separately via
// SetEnvVar).
//
// Each Config.Keys entry maps directly to a `<env-var-name>=<value>`
// line — the env var name comes from each preset's manifest.llm.
// api_key_env field, written by the TUI's key-paste flow. No
// provider-to-env-var translation: that misled the architecture
// because a single provider can serve multiple presets with distinct
// keys.
//
// Managed keys already present in the file are updated in place;
// managed keys absent from the file are appended; unmanaged lines are
// left untouched. This replaces the previous clobber-everything
// behavior so a manually- or separately-populated .env survives an API
// key save.
//
// This file is loaded by agents at boot via env_file in init.json.
func WriteEnvFile(globalDir string, cfg Config) error {
	path := EnvFilePath(globalDir)
	existing, err := readEnvLines(path)
	if err != nil {
		return err
	}

	// Collect the managed keys we intend to write (skipping empties).
	managed := make(map[string]string, len(cfg.Keys))
	for envName, val := range cfg.Keys {
		if envName == "" || val == "" {
			continue
		}
		managed[envName] = val
	}

	var out []string
	seen := map[string]bool{}
	for _, line := range existing {
		key, isAssign := parseEnvKey(line)
		if !isAssign {
			out = append(out, line) // comment / blank — preserve verbatim
			continue
		}
		if val, isManaged := managed[key]; isManaged {
			out = append(out, key+"="+val) // update in place
			seen[key] = true
			continue
		}
		out = append(out, line) // unmanaged assignment — preserve
	}

	// Append managed keys that weren't already present. Sort for a
	// deterministic on-disk order (map iteration is random in Go).
	var missing []string
	for envName := range managed {
		if !seen[envName] {
			missing = append(missing, envName)
		}
	}
	sort.Strings(missing)
	for _, envName := range missing {
		out = append(out, envName+"="+managed[envName])
	}

	return writeEnvLines(path, out)
}

// SetEnvVar performs a merge-preserving upsert of a single env var in
// ~/.lingtai-tui/.env. It reads the file, sets (or, when value is "",
// removes) exactly the named key, and rewrites — leaving comments,
// blank lines, unrelated keys, and file permissions untouched. Used for
// vars the TUI owns outside Config.Keys, such as LINGTAI_SOUL_FLOW_ENABLED.
//
// A missing file is treated as empty; removing a key that isn't present
// is a no-op that still normalizes the file (harmless).
func SetEnvVar(globalDir, key, value string) error {
	if key == "" {
		return nil
	}
	path := EnvFilePath(globalDir)
	existing, err := readEnvLines(path)
	if err != nil {
		return err
	}

	var out []string
	replaced := false
	removedExisting := false
	for _, line := range existing {
		k, isAssign := parseEnvKey(line)
		if !isAssign || k != key {
			out = append(out, line)
			continue
		}
		// Matched the target key.
		if value == "" {
			removedExisting = true
			continue // remove: drop the line entirely
		}
		if !replaced {
			out = append(out, key+"="+value)
			replaced = true
		}
		// Any further duplicate lines for this key are dropped.
	}
	if value != "" && !replaced {
		out = append(out, key+"="+value)
	}
	// Removing a key that was never present (and no file to normalize) is a
	// pure no-op: don't create an empty .env just to "unset" nothing. This
	// keeps a default-disabled agent from materializing a spurious empty
	// .env when none existed.
	if value == "" && !removedExisting {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return nil
		}
	}
	return writeEnvLines(path, out)
}

// EnvVarTruthy reports whether the given .env value is one the kernel
// treats as enabled: 1/true/yes/on, case-insensitive. Empty/absent/other
// is false. Mirrors kernel flow.py truthy parsing.
func EnvVarTruthy(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// SoulFlowEnabled reports whether ~/.lingtai-tui/.env currently opts into
// soul flow (LINGTAI_SOUL_FLOW_ENABLED set to a truthy value). Used to
// seed the wizard toggle from the existing on-disk state. A missing file
// or absent key means disabled — matching the kernel default.
func SoulFlowEnabled(globalDir string) bool {
	return SoulFlowEnabledInEnvFile(EnvFilePath(globalDir))
}

// SoulFlowEnabledInEnvFile is like SoulFlowEnabled but reads an explicit
// .env path — used by /kanban/props to reflect the specific env_file an
// agent's init.json points at (usually the global .env, but honoring an
// override). A missing file or absent/false key means disabled.
func SoulFlowEnabledInEnvFile(envPath string) bool {
	lines, err := readEnvLines(envPath)
	if err != nil {
		return false
	}
	for _, line := range lines {
		k, isAssign := parseEnvKey(line)
		if !isAssign || k != SoulFlowEnabledEnvVar {
			continue
		}
		eq := strings.IndexByte(line, '=')
		return EnvVarTruthy(line[eq+1:])
	}
	return false
}

// EnsureConfigPersisted creates a minimal empty config.json if and
// only if the file does not already exist. This is purely a setup-
// complete sentinel for main.go's first-run heuristic (which checks
// config.json existence), needed because OAuth / no-key presets like
// codex skip stepPresetKey entirely — so keyDoNext's SaveConfig is
// never called and config.json is never created, causing the
// recovery wizard to re-trigger on every launch.
//
// Implementation deliberately avoids SaveConfig because SaveConfig
// also rewrites .env, which a user may have populated manually with
// values that should not be clobbered. We also don't read the file
// first — if it exists (in any state, including malformed or user-
// edited), we leave it alone. We have no business modifying its
// content; we only need the file to exist as a marker.
//
// Errors are intentionally swallowed: this runs as a side-effect
// after successful wizard completion, where a sentinel-write error
// should not block the launch path.
func EnsureConfigPersisted(globalDir string) {
	configPath := filepath.Join(globalDir, "config.json")
	if _, err := os.Stat(configPath); err == nil {
		return // file already exists in some form — don't touch it
	}
	if err := os.MkdirAll(globalDir, 0o755); err != nil {
		return
	}
	_ = os.WriteFile(configPath, []byte("{}\n"), 0o644)
}
