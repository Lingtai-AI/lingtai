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
}

// DefaultTUIConfig returns sensible defaults.
func DefaultTUIConfig() TUIConfig {
	return TUIConfig{
		Language:     "en",
		MailPageSize: 100,
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
		tc.MailPageSize = 100 // migrate old values below minimum
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


