package migrate

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// migrateAddonsToMCP rewrites legacy in-process addon declarations into
// the new MCP-server activation form.
//
// Before:
//
//	{"addons": {"imap": {"config": ".secrets/imap.json"}}}
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
// Portal mirrors the TUI implementation so init.json files migrate
// regardless of which binary runs first. Helper logic (venv path resolution,
// .env parsing) is duplicated here rather than imported from tui/internal/
// to keep the two binaries layering-clean.
//
// Idempotent. Errors per-file are logged and don't abort the run.
func migrateAddonsToMCP(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read .lingtai dir: %w", err)
	}

	globalDir := globalLingtaiTUIDir()

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

type addonSpec struct {
	module     string
	envVarName string
	defaultRel string
}

var addonSpecs = map[string]addonSpec{
	"imap":     {module: "lingtai_imap", envVarName: "LINGTAI_IMAP_CONFIG", defaultRel: ".secrets/imap.json"},
	"telegram": {module: "lingtai_telegram", envVarName: "LINGTAI_TELEGRAM_CONFIG", defaultRel: ".secrets/telegram.json"},
	"feishu":   {module: "lingtai_feishu", envVarName: "LINGTAI_FEISHU_CONFIG", defaultRel: ".secrets/feishu.json"},
	"wechat":   {module: "lingtai_wechat", envVarName: "LINGTAI_WECHAT_CONFIG", defaultRel: ".secrets/wechat/config.json"},
}

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
		doc["addons"] = []interface{}{}
		writeJSONIfChanged(initPath, doc, data)
		return
	}

	envMap, _ := loadEnvFile(envFilePath(doc, agentDir))
	venvPython := resolveVenvPython(doc, globalDir)

	addonsList := make([]interface{}, 0, len(addonsDict))
	mcpEntries, _ := doc["mcp"].(map[string]interface{})
	if mcpEntries == nil {
		mcpEntries = map[string]interface{}{}
	}

	for addonName, addonCfgRaw := range addonsDict {
		spec, known := addonSpecs[addonName]
		if !known {
			fmt.Fprintf(os.Stderr,
				"m028: %s — unknown addon %q, skipping\n", initPath, addonName)
			continue
		}
		addonCfg, ok := addonCfgRaw.(map[string]interface{})
		if !ok {
			fmt.Fprintf(os.Stderr,
				"m028: %s — addon %q has non-dict config (%T), skipping\n",
				initPath, addonName, addonCfgRaw)
			continue
		}

		configRel, err := resolveOrMaterializeAddonConfig(
			addonName, addonCfg, agentDir, spec.defaultRel,
		)
		if err != nil {
			fmt.Fprintf(os.Stderr,
				"m028: %s — addon %q: %v (skipping)\n",
				initPath, addonName, err)
			continue
		}

		if err := resolveEnvFieldsInJSONFile(
			absoluteUnder(agentDir, configRel), envMap,
		); err != nil {
			fmt.Fprintf(os.Stderr,
				"m028: %s — addon %q: env resolution warning: %v\n",
				initPath, addonName, err)
		}

		addonsList = append(addonsList, addonName)

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

// globalLingtaiTUIDir mirrors the TUI helper. Returns "" if home is
// unresolvable; callers fall back to a literal "python" command.
func globalLingtaiTUIDir() string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return ""
	}
	return filepath.Join(home, ".lingtai-tui")
}

func venvPythonPath(venvDir string) string {
	if runtime.GOOS == "windows" {
		return filepath.Join(venvDir, "Scripts", "python.exe")
	}
	return filepath.Join(venvDir, "bin", "python")
}

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

func resolveVenvPython(doc map[string]interface{}, globalDir string) string {
	if vp, ok := doc["venv_path"].(string); ok && vp != "" {
		return venvPythonPath(vp)
	}
	if globalDir == "" {
		return "python"
	}
	return venvPythonPath(filepath.Join(globalDir, "runtime", "venv"))
}

func resolveOrMaterializeAddonConfig(
	addonName string,
	addonCfg map[string]interface{},
	agentDir string,
	defaultRel string,
) (string, error) {
	if cfgPath, ok := addonCfg["config"].(string); ok && cfgPath != "" {
		return cfgPath, nil
	}
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

func resolveEnvFieldsInJSONFile(path string, envMap map[string]string) error {
	if path == "" {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
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
		val, found := os.LookupEnv(envVarName)
		if !found {
			val, found = envMap[envVarName]
		}
		if found {
			d[baseKey] = val
		}
		delete(d, ek)
		changed = true
	}
	return changed
}

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
		v = strings.Trim(v, "'\"")
		out[k] = v
	}
	return out, sc.Err()
}

func absoluteUnder(base, p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

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
