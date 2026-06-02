package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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
	StatusLine   string `json:"status_line,omitempty"`
}

const (
	StatusLineOff     = "off"
	StatusLineCompact = "compact"
	StatusLineDefault = "default"
	StatusLineFull    = "full"
)

var validStatusLineModes = map[string]bool{
	StatusLineOff:     true,
	StatusLineCompact: true,
	StatusLineDefault: true,
	StatusLineFull:    true,
}

// DefaultTUIConfig returns sensible defaults.
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Language:     "en",
		MailPageSize: 100,
		Insights:     false,
		StatusLine:   StatusLineDefault,
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
		tc.MailPageSize = 100 // migrate old values below minimum
	}
	if !validStatusLineModes[tc.StatusLine] {
		tc.StatusLine = StatusLineDefault
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

// WriteEnvFile writes API keys from config to ~/.lingtai-tui/.env.
// Each Config.Keys entry maps directly to a `<env-var-name>=<value>`
// line — the env var name comes from each preset's manifest.llm.
// api_key_env field, written by the TUI's key-paste flow. No
// provider-to-env-var translation: that misled the architecture
// because a single provider can serve multiple presets with distinct
// keys.
//
// This file is loaded by agents at boot via env_file in init.json.
func WriteEnvFile(globalDir string, cfg Config) error {
	var lines []string
	for envName, val := range cfg.Keys {
		if envName == "" || val == "" {
			continue
		}
		lines = append(lines, envName+"="+val)
	}
	path := EnvFilePath(globalDir)
	return os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600)
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
