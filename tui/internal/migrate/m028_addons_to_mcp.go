package migrate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/anthropics/lingtai-tui/internal/config"
)

// migrateAddonsToMCP rewrites legacy in-process addon declarations into
// the new MCP-server activation form.
//
// Before (legacy):
//
//	{
//	  "addons": {
//	    "imap": {"config": ".secrets/imap.json"}
//	  }
//	}
//
// After:
//
//	{
//	  "addons": ["imap"],
//	  "mcp": {
//	    "imap": {
//	      "type": "stdio",
//	      "command": "/Users/.../runtime/venv/bin/python",
//	      "args": ["-m", "lingtai_imap"],
//	      "env": {"LINGTAI_IMAP_CONFIG": ".secrets/imap.json"}
//	    }
//	  }
//	}
//
// What changes:
//  1. addons becomes a list of names (the kernel mcp capability decompresses
//     each name from mcp_catalog.json into the per-agent registry).
//  2. mcp gets a sibling activation entry per addon, pointing at the MCP
//     subprocess (python -m lingtai_<name>) with the corresponding env var
//     pointing at the addon's config file.
//
// The migration also resolves *_env indirection inside the addon's config
// file at migration time, since the new MCPs require plaintext config (the
// kernel-side _resolve_env_fields helper goes away with the legacy addons
// tree). For inline addon configs (no "config" key), the migration writes
// out a fresh .secrets/<name>.json from the inline values and points the
// activation entry at it.
//
// Idempotent: if addons is already a list, the migration is a no-op.
// Errors on individual files are logged to stderr and don't abort the run.
func migrateAddonsToMCP(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	globalDir := globalTUIDir()

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		name := entry.Name()
		if name == "" || name[0] == '.' || name == "human" {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, name)
		initPath := filepath.Join(agentDir, "init.json")
		convertAddonsInInitFile(initPath, agentDir, globalDir)
	}
	return nil
}

// addonSpec captures the per-addon plumbing needed to produce a new
// mcp.<name> activation entry.
type addonSpec struct {
	module     string // python -m <module>
	envVarName string // env var the MCP reads for its config path
	defaultRel string // default config-file relative path under the agent dir
}

var addonSpecs = map[string]addonSpec{
	"imap":     {module: "lingtai_imap", envVarName: "LINGTAI_IMAP_CONFIG", defaultRel: ".secrets/imap.json"},
	"telegram": {module: "lingtai_telegram", envVarName: "LINGTAI_TELEGRAM_CONFIG", defaultRel: ".secrets/telegram.json"},
	"feishu":   {module: "lingtai_feishu", envVarName: "LINGTAI_FEISHU_CONFIG", defaultRel: ".secrets/feishu.json"},
	"wechat":   {module: "lingtai_wechat", envVarName: "LINGTAI_WECHAT_CONFIG", defaultRel: ".secrets/wechat/config.json"},
}

// convertAddonsInInitFile is the per-init.json workhorse. Atomic write,
// preserves all unrelated keys, leaves the file untouched on any error.
func convertAddonsInInitFile(initPath, agentDir, globalDir string) {
	data, err := os.ReadFile(initPath)
	if err != nil {
		return
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		fmt.Fprintf(os.Stderr, "m028: skipping %s — unparseable: %v\n", initPath, err)
		return
	}

	addonsRaw, ok := doc["addons"]
	if !ok {
		return
	}
	// Already in new shape (list)? No-op.
	if _, isList := addonsRaw.([]interface{}); isList {
		return
	}
	addonsDict, ok := addonsRaw.(map[string]interface{})
	if !ok {
		fmt.Fprintf(os.Stderr, "m028: skipping %s — addons field neither dict nor list (%T)\n",
			initPath, addonsRaw)
		return
	}
	if len(addonsDict) == 0 {
		// Empty addons dict — replace with empty list and bail.
		doc["addons"] = []interface{}{}
		writeJSONIfChanged(initPath, doc, data)
		return
	}

	// Resolve env file path for *_env substitution.
	envMap, _ := loadEnvFile(envFilePath(doc, agentDir))

	// Resolve venv python: per-init.json override, then global runtime venv.
	venvPython := resolveVenvPython(doc, globalDir)

	// Build the new addons list + mcp activations.
	addonsList := make([]interface{}, 0, len(addonsDict))
	mcpEntries, _ := doc["mcp"].(map[string]interface{})
	if mcpEntries == nil {
		mcpEntries = map[string]interface{}{}
	}

	for addonName, addonCfgRaw := range addonsDict {
		spec, known := addonSpecs[addonName]
		if !known {
			fmt.Fprintf(os.Stderr,
				"m028: %s — unknown addon %q, skipping (file unchanged for this entry)\n",
				initPath, addonName)
			continue
		}
		addonCfg, ok := addonCfgRaw.(map[string]interface{})
		if !ok {
			fmt.Fprintf(os.Stderr,
				"m028: %s — addon %q has non-dict config (%T), skipping\n",
				initPath, addonName, addonCfgRaw)
			continue
		}

		// Resolve the config file path. If "config" key exists, use it.
		// Otherwise, materialize inline kwargs into a new file at defaultRel.
		configRel, err := resolveOrMaterializeAddonConfig(
			addonName, addonCfg, agentDir, spec.defaultRel,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"m028: %s — addon %q: %v (skipping)\n",
				initPath, addonName, err)
			continue
		}

		// Resolve *_env fields inside the addon's config file in place.
		// Best-effort: a failure logs but doesn't block migration.
		if err := resolveEnvFieldsInJSONFile(
			absoluteUnder(agentDir, configRel), envMap,
		); err != nil {
			fmt.Fprintf(os.Stderr,
				"m028: %s — addon %q: env resolution warning: %v\n",
				initPath, addonName, err)
		}

		addonsList = append(addonsList, addonName)

		// Don't clobber a pre-existing mcp.<name> entry — the user may have
		// already wired the activation manually.
		if _, exists := mcpEntries[addonName]; !exists {
			mcpEntries[addonName] = map[string]interface{}{
				"type":    "stdio",
				"command": venvPython,
				"args":    []interface{}{"-m", spec.module},
				"env": map[string]interface{}{
					spec.envVarName: configRel,
				},
			}
		}
	}

	doc["addons"] = addonsList
	if len(mcpEntries) > 0 {
		doc["mcp"] = mcpEntries
	}

	writeJSONIfChanged(initPath, doc, data)
}

// envFilePath reads init.json's top-level env_file (resolved against the
// agent dir if relative). Returns "" when missing.
func envFilePath(doc map[string]interface{}, agentDir string) string {
	raw, ok := doc["env_file"].(string)
	if !ok || raw == "" {
		return ""
	}
	if filepath.IsAbs(raw) {
		return raw
	}
	return filepath.Join(agentDir, raw)
}

// resolveVenvPython returns the absolute python path the MCP subprocess
// will invoke. Honors init.json venv_path if present, otherwise falls back
// to the TUI's global runtime venv.
func resolveVenvPython(doc map[string]interface{}, globalDir string) string {
	if vp, ok := doc["venv_path"].(string); ok && vp != "" {
		return config.VenvPython(vp)
	}
	if globalDir == "" {
		// Last-ditch fallback so we never write an empty command.
		return "python"
	}
	return config.VenvPython(config.RuntimeVenvDir(globalDir))
}

// resolveOrMaterializeAddonConfig figures out where the addon's config
// file lives. If addonCfg has a "config" key, that path is used directly
// (returned in its original relative-or-absolute form). Otherwise the
// inline kwargs are written out to a fresh JSON file at defaultRel under
// agentDir, and that relative path is returned.
//
// Inline-materialization is what makes the migration work for legacy
// users who put their credentials directly in init.json instead of in a
// sidecar config file.
func resolveOrMaterializeAddonConfig(
	addonName string,
	addonCfg map[string]interface{},
	agentDir string,
	defaultRel string,
) (string, error) {
	if cfgPath, ok := addonCfg["config"].(string); ok && cfgPath != "" {
		return cfgPath, nil
	}

	// Inline kwargs — strip the "config" key (in case it's empty/null) and
	// write the rest to defaultRel.
	inline := map[string]interface{}{}
	for k, v := range addonCfg {
		if k == "config" {
			continue
		}
		inline[k] = v
	}
	if len(inline) == 0 {
		return "", fmt.Errorf("no config path and no inline kwargs to materialize")
	}

	target := filepath.Join(agentDir, defaultRel)
	if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", filepath.Dir(target), err)
	}
	if _, err := os.Stat(target); err == nil {
		// Don't overwrite an existing file. The user's pre-existing config wins.
		return defaultRel, nil
	}
	body, err := json.MarshalIndent(inline, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal inline kwargs: %w", err)
	}
	if err := os.WriteFile(target, body, 0o600); err != nil {
		return "", fmt.Errorf("write %s: %w", target, err)
	}
	return defaultRel, nil
}

// resolveEnvFieldsInJSONFile walks a JSON file and replaces *_env fields
// with their plaintext values from envMap. Best-effort — silently skips
// fields whose env vars are absent (the new MCP will surface that error
// at runtime if the value is actually needed).
//
// Handles both top-level fields and per-account dicts inside an "accounts"
// array (matching the legacy lingtai.config_resolve._resolve_env_fields
// behavior). Atomic write, preserves untouched fields.
func resolveEnvFieldsInJSONFile(path string, envMap map[string]string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // nothing to resolve if the file isn't there
		}
		return err
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}

	changed := resolveEnvFieldsInDict(doc, envMap)
	if accountsRaw, ok := doc["accounts"].([]interface{}); ok {
		for i, acctRaw := range accountsRaw {
			if acct, ok := acctRaw.(map[string]interface{}); ok {
				if resolveEnvFieldsInDict(acct, envMap) {
					changed = true
					accountsRaw[i] = acct
				}
			}
		}
	}

	if !changed {
		return nil
	}
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal %s: %w", path, err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, updated, 0o600); err != nil {
		return fmt.Errorf("write tmp %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

// resolveEnvFieldsInDict mutates a single dict, replacing every key
// ending in "_env" with its base form. Returns true if any change was made.
func resolveEnvFieldsInDict(d map[string]interface{}, envMap map[string]string) bool {
	envKeys := []string{}
	for k := range d {
		if strings.HasSuffix(k, "_env") {
			envKeys = append(envKeys, k)
		}
	}
	if len(envKeys) == 0 {
		return false
	}
	changed := false
	for _, ek := range envKeys {
		baseKey := strings.TrimSuffix(ek, "_env")
		envVarName, ok := d[ek].(string)
		if !ok {
			continue
		}
		// Try host environment first (matches the legacy resolution order).
		val, found := os.LookupEnv(envVarName)
		if !found {
			val, found = envMap[envVarName]
		}
		if found {
			d[baseKey] = val
		}
		// Always drop the *_env key (whether resolved or not), to leave the
		// config in plaintext-only form. If the value couldn't be resolved,
		// the new MCP will fail at runtime with a clear "missing field" error,
		// which is the correct signal for the user to fix.
		delete(d, ek)
		changed = true
	}
	return changed
}

// loadEnvFile parses a simple KEY=VALUE .env file. Skips comments and blanks.
// Returns nil envMap on missing file (callers treat as no override).
func loadEnvFile(path string) (map[string]string, error) {
	out := map[string]string{}
	if path == "" {
		return out, nil
	}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		k := strings.TrimSpace(line[:eq])
		v := strings.TrimSpace(line[eq+1:])
		// Strip optional surrounding quotes.
		v = strings.Trim(v, "'\"")
		out[k] = v
	}
	return out, sc.Err()
}

// absoluteUnder makes a path absolute by joining it under base if relative.
func absoluteUnder(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// writeJSONIfChanged compares marshaled output to original bytes and skips
// the write if nothing changed (cheap defense against gratuitous mtime bumps).
// On change, writes atomically.
func writeJSONIfChanged(path string, doc map[string]interface{}, original []byte) {
	updated, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "m028: marshal failed for %s: %v\n", path, err)
		return
	}
	if string(updated) == string(original) {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, updated, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "m028: write tmp failed for %s: %v\n", path, err)
		return
	}
	if err := os.Rename(tmp, path); err != nil {
		fmt.Fprintf(os.Stderr, "m028: rename failed for %s: %v\n", path, err)
		_ = os.Remove(tmp)
	}
}
