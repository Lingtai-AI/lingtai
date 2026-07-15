package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// helper: write init.json under <lingtaiDir>/<agent>/init.json
func writeM028Init(t *testing.T, lingtaiDir, agent string, doc map[string]interface{}) string {
	t.Helper()
	agentDir := filepath.Join(lingtaiDir, agent)
	if err := os.MkdirAll(agentDir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func readM028Init(t *testing.T, p string) map[string]interface{} {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	return doc
}

// ---------------------------------------------------------------------------
// Conversion: imap with config-file ref
// ---------------------------------------------------------------------------

func TestM028ConvertsImapWithConfigRef(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons": map[string]interface{}{
			"imap": map[string]interface{}{
				"config": ".secrets/imap.json",
			},
		},
	})

	// Pre-create the addon's config file so the env-resolution pass has
	// something to walk (no *_env keys, so it stays untouched).
	agentDir := filepath.Dir(initPath)
	if err := os.MkdirAll(filepath.Join(agentDir, ".secrets"), 0o700); err != nil {
		t.Fatal(err)
	}
	imapCfg := map[string]interface{}{
		"accounts": []interface{}{
			map[string]interface{}{
				"email_address":  "alice@example.com",
				"email_password": "plaintext-pw",
			},
		},
	}
	body, _ := json.MarshalIndent(imapCfg, "", "  ")
	if err := os.WriteFile(filepath.Join(agentDir, ".secrets", "imap.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	got := readM028Init(t, initPath)

	addons, ok := got["addons"].([]interface{})
	if !ok {
		t.Fatalf("addons not a list: %T", got["addons"])
	}
	if len(addons) != 1 || addons[0] != "imap" {
		t.Errorf("addons = %v, want [\"imap\"]", addons)
	}

	mcp, ok := got["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcp not a dict: %T", got["mcp"])
	}
	imap, ok := mcp["imap"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcp.imap not a dict: %T", mcp["imap"])
	}
	if imap["type"] != "stdio" {
		t.Errorf("mcp.imap.type = %v, want stdio", imap["type"])
	}
	args, _ := imap["args"].([]interface{})
	if len(args) != 2 || args[0] != "-m" || args[1] != "lingtai.mcp_servers.imap" {
		t.Errorf("mcp.imap.args = %v, want [\"-m\", \"lingtai.mcp_servers.imap\"]", args)
	}
	env, _ := imap["env"].(map[string]interface{})
	if env["LINGTAI_IMAP_CONFIG"] != ".secrets/imap.json" {
		t.Errorf("env.LINGTAI_IMAP_CONFIG = %v, want \".secrets/imap.json\"", env["LINGTAI_IMAP_CONFIG"])
	}
	if cmd, _ := imap["command"].(string); cmd == "" {
		t.Error("mcp.imap.command is empty")
	}
}

// ---------------------------------------------------------------------------
// Conversion: inline addon config (no "config" key) materializes a sidecar
// ---------------------------------------------------------------------------

func TestM028MaterializesInlineConfig(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons": map[string]interface{}{
			"telegram": map[string]interface{}{
				"accounts": []interface{}{
					map[string]interface{}{
						"alias":     "mybot",
						"bot_token": "1234:abc",
					},
				},
			},
		},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	// The migration should have written .secrets/telegram.json with the
	// inline kwargs, then pointed the activation entry at it.
	agentDir := filepath.Dir(initPath)
	cfgPath := filepath.Join(agentDir, ".secrets", "telegram.json")
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("expected sidecar at %s: %v", cfgPath, err)
	}
	data, _ := os.ReadFile(cfgPath)
	var cfg map[string]interface{}
	json.Unmarshal(data, &cfg)
	accs, _ := cfg["accounts"].([]interface{})
	if len(accs) != 1 {
		t.Errorf("expected 1 account in materialized sidecar, got %d", len(accs))
	}

	got := readM028Init(t, initPath)
	mcp := got["mcp"].(map[string]interface{})
	tg := mcp["telegram"].(map[string]interface{})
	env := tg["env"].(map[string]interface{})
	if env["LINGTAI_TELEGRAM_CONFIG"] != ".secrets/telegram.json" {
		t.Errorf("env.LINGTAI_TELEGRAM_CONFIG = %v, want \".secrets/telegram.json\"",
			env["LINGTAI_TELEGRAM_CONFIG"])
	}
}

// ---------------------------------------------------------------------------
// Conversion: *_env resolution from env_file at migration time
// ---------------------------------------------------------------------------

func TestM028ResolvesEnvFieldsFromEnvFile(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	envFilePath := filepath.Join(tmp, "secrets.env")
	if err := os.WriteFile(envFilePath, []byte("GMAIL_APP_PASS=resolved-secret\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"env_file": envFilePath, // absolute
		"addons": map[string]interface{}{
			"imap": map[string]interface{}{"config": ".secrets/imap.json"},
		},
	})

	agentDir := filepath.Dir(initPath)
	os.MkdirAll(filepath.Join(agentDir, ".secrets"), 0o700)
	imapCfg := map[string]interface{}{
		"accounts": []interface{}{
			map[string]interface{}{
				"email_address":      "alice@example.com",
				"email_password_env": "GMAIL_APP_PASS",
			},
		},
	}
	body, _ := json.MarshalIndent(imapCfg, "", "  ")
	os.WriteFile(filepath.Join(agentDir, ".secrets", "imap.json"), body, 0o600)

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	// The *_env field should be resolved to plaintext and the suffix dropped.
	cfgData, _ := os.ReadFile(filepath.Join(agentDir, ".secrets", "imap.json"))
	var cfg map[string]interface{}
	json.Unmarshal(cfgData, &cfg)
	acct := cfg["accounts"].([]interface{})[0].(map[string]interface{})
	if _, hasEnv := acct["email_password_env"]; hasEnv {
		t.Errorf("email_password_env should have been removed; full acct: %v", acct)
	}
	if acct["email_password"] != "resolved-secret" {
		t.Errorf("email_password = %v, want \"resolved-secret\"", acct["email_password"])
	}
}

// ---------------------------------------------------------------------------
// Idempotence: running the migration twice yields the same result
// ---------------------------------------------------------------------------

func TestM028Idempotent(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons": map[string]interface{}{
			"imap": map[string]interface{}{"config": ".secrets/imap.json"},
		},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(initPath)

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(initPath)

	if string(first) != string(second) {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", first, second)
	}
}

// ---------------------------------------------------------------------------
// Skip when already list-shape (new-shape init.json)
// ---------------------------------------------------------------------------

func TestM028NoOpOnListShape(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons":   []interface{}{"imap"},
		"mcp": map[string]interface{}{
			"imap": map[string]interface{}{
				"type": "stdio", "command": "/usr/bin/python", "args": []interface{}{"-m", "lingtai_imap"},
			},
		},
	})
	original, _ := os.ReadFile(initPath)

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	after, _ := os.ReadFile(initPath)
	if string(original) != string(after) {
		t.Errorf("file mutated despite list-shape addons:\nbefore:\n%s\nafter:\n%s", original, after)
	}
}

// ---------------------------------------------------------------------------
// Preserves user-set mcp.<name> entry (don't clobber)
// ---------------------------------------------------------------------------

func TestM028PreservesExistingMCPEntry(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	customCommand := "/opt/custom/python"
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons": map[string]interface{}{
			"imap": map[string]interface{}{"config": ".secrets/imap.json"},
		},
		"mcp": map[string]interface{}{
			// User pre-wired with a custom python.
			"imap": map[string]interface{}{
				"type":    "stdio",
				"command": customCommand,
				"args":    []interface{}{"-m", "lingtai_imap"},
			},
		},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	got := readM028Init(t, initPath)
	mcp := got["mcp"].(map[string]interface{})
	imap := mcp["imap"].(map[string]interface{})
	if imap["command"] != customCommand {
		t.Errorf("user's command was clobbered: got %v, want %s", imap["command"], customCommand)
	}
}

// ---------------------------------------------------------------------------
// Preserves unrelated init.json keys
// ---------------------------------------------------------------------------

func TestM028PreservesUnrelatedKeys(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest":           map[string]interface{}{"agent_name": "alice"},
		"covenant_file":      "/etc/covenant.md",
		"some_unrelated_key": "be helpful",
		"addons":             map[string]interface{}{"imap": map[string]interface{}{"config": ".secrets/imap.json"}},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	got := readM028Init(t, initPath)
	if got["covenant_file"] != "/etc/covenant.md" {
		t.Errorf("covenant_file lost: %v", got["covenant_file"])
	}
	if got["some_unrelated_key"] != "be helpful" {
		t.Errorf("some_unrelated_key lost: %v", got["some_unrelated_key"])
	}
	manifest := got["manifest"].(map[string]interface{})
	if manifest["agent_name"] != "alice" {
		t.Errorf("manifest.agent_name lost: %v", manifest["agent_name"])
	}
}

// ---------------------------------------------------------------------------
// Wechat: config + sibling credentials.json
// ---------------------------------------------------------------------------

func TestM028WechatPointsAtConfigJSON(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons": map[string]interface{}{
			"wechat": map[string]interface{}{
				"config": ".lingtai/.addons/wechat/config.json",
			},
		},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	got := readM028Init(t, initPath)
	mcp := got["mcp"].(map[string]interface{})
	wc := mcp["wechat"].(map[string]interface{})
	env := wc["env"].(map[string]interface{})
	want := ".lingtai/.addons/wechat/config.json"
	if env["LINGTAI_WECHAT_CONFIG"] != want {
		t.Errorf("env.LINGTAI_WECHAT_CONFIG = %v, want %s", env["LINGTAI_WECHAT_CONFIG"], want)
	}
	args, _ := wc["args"].([]interface{})
	if len(args) != 2 || args[0] != "-m" || args[1] != "lingtai.mcp_servers.wechat" {
		t.Errorf("wechat args = %v, want [\"-m\", \"lingtai.mcp_servers.wechat\"]", args)
	}
}

// ---------------------------------------------------------------------------
// Multiple addons in one init.json
// ---------------------------------------------------------------------------

func TestM028HandlesMultipleAddons(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	wantSpecs := map[string]struct {
		module string
		envVar string
		config string
	}{
		"imap":     {"lingtai.mcp_servers.imap", "LINGTAI_IMAP_CONFIG", ".secrets/imap.json"},
		"telegram": {"lingtai.mcp_servers.telegram", "LINGTAI_TELEGRAM_CONFIG", ".secrets/telegram.json"},
		"feishu":   {"lingtai.mcp_servers.feishu", "LINGTAI_FEISHU_CONFIG", ".secrets/feishu.json"},
		"wechat":   {"lingtai.mcp_servers.wechat", "LINGTAI_WECHAT_CONFIG", ".secrets/wechat/config.json"},
		"whatsapp": {"lingtai.mcp_servers.whatsapp", "LINGTAI_WHATSAPP_CONFIG", ".secrets/whatsapp.json"},
	}
	legacyAddons := map[string]interface{}{}
	for name, want := range wantSpecs {
		legacyAddons[name] = map[string]interface{}{"config": want.config}
	}
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons":   legacyAddons,
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	got := readM028Init(t, initPath)
	addons, ok := got["addons"].([]interface{})
	if !ok {
		t.Fatalf("addons not a list: %T", got["addons"])
	}
	names := map[string]bool{}
	for _, a := range addons {
		name, ok := a.(string)
		if !ok {
			t.Errorf("addon entry has type %T, want string", a)
			continue
		}
		names[name] = true
	}
	if len(names) != len(wantSpecs) {
		t.Errorf("addons list = %v, want exactly %d curated addons", addons, len(wantSpecs))
	}
	for name := range wantSpecs {
		if !names[name] {
			t.Errorf("missing addon %s in addons list (got %v)", name, addons)
		}
	}

	mcp, ok := got["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcp not a map: %T", got["mcp"])
	}
	for name, want := range wantSpecs {
		entry, ok := mcp[name].(map[string]interface{})
		if !ok {
			t.Errorf("mcp.%s missing or wrong type: %T", name, mcp[name])
			continue
		}
		args, _ := entry["args"].([]interface{})
		if len(args) != 2 || args[0] != "-m" || args[1] != want.module {
			t.Errorf("mcp.%s.args = %v, want [-m %s]", name, args, want.module)
		}
		env, _ := entry["env"].(map[string]interface{})
		if env[want.envVar] != want.config {
			t.Errorf("mcp.%s.env[%s] = %v, want %q", name, want.envVar, env[want.envVar], want.config)
		}
	}
}

// ---------------------------------------------------------------------------
// Unknown addon name is logged and skipped (doesn't appear in addons list)
// ---------------------------------------------------------------------------

func TestM028SkipsUnknownAddon(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons": map[string]interface{}{
			"imap":     map[string]interface{}{"config": ".secrets/imap.json"},
			"slackbot": map[string]interface{}{"config": ".secrets/slack.json"}, // not in catalog
		},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	got := readM028Init(t, initPath)
	addons, _ := got["addons"].([]interface{})
	for _, a := range addons {
		if a == "slackbot" {
			t.Errorf("slackbot should have been skipped (got %v)", addons)
		}
	}
	mcp, _ := got["mcp"].(map[string]interface{})
	if _, ok := mcp["slackbot"]; ok {
		t.Error("slackbot should not have an mcp entry")
	}
}

// ---------------------------------------------------------------------------
// Empty addons dict -> empty list
// ---------------------------------------------------------------------------

func TestM028EmptyAddonsBecomesEmptyList(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	initPath := writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons":   map[string]interface{}{},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	got := readM028Init(t, initPath)
	addons, ok := got["addons"].([]interface{})
	if !ok {
		t.Fatalf("addons not a list: %T", got["addons"])
	}
	if len(addons) != 0 {
		t.Errorf("expected empty list, got %v", addons)
	}
}

// ---------------------------------------------------------------------------
// Skip non-agent dirs (dotted, "human", etc.)
// ---------------------------------------------------------------------------

func TestM028SkipsHumanAndDottedDirs(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")

	// "human" and ".tmp" should be skipped.
	for _, name := range []string{"human", ".tmp"} {
		d := filepath.Join(lingtaiDir, name)
		os.MkdirAll(d, 0o755)
		os.WriteFile(filepath.Join(d, "init.json"), []byte(`{"addons": {"imap": {}}}`), 0o644)
	}
	// "alice" is a real agent and should be processed.
	writeM028Init(t, lingtaiDir, "alice", map[string]interface{}{
		"manifest": map[string]interface{}{"agent_name": "alice"},
		"addons":   map[string]interface{}{"imap": map[string]interface{}{"config": "x"}},
	})

	if err := migrateAddonsToMCP(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	for _, name := range []string{"human", ".tmp"} {
		raw, _ := os.ReadFile(filepath.Join(lingtaiDir, name, "init.json"))
		if !strings.Contains(string(raw), `"addons": {"imap": {}}`) {
			t.Errorf("%s/init.json was modified: %s", name, raw)
		}
	}
}
