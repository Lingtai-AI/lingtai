// E2E sanity test: drives migrateAddonsToMCP against a fixture with both
// addons styles + *_env resolution + env_file. Kept separate from the unit
// suite so its absolute /tmp paths can be skimmed easily during review.
//
// Run with: go test ./internal/migrate -run TestM028E2EFixture -v
package migrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestM028E2EFixture(t *testing.T) {
	tmp := t.TempDir()
	lingtaiDir := filepath.Join(tmp, ".lingtai")
	agentDir := filepath.Join(lingtaiDir, "alice")
	secretsDir := filepath.Join(agentDir, ".secrets")

	if err := os.MkdirAll(secretsDir, 0o700); err != nil {
		t.Fatal(err)
	}

	envFilePath := filepath.Join(tmp, ".env")
	if err := os.WriteFile(envFilePath, []byte("GMAIL_APP_PASS=top-secret-app-pw\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	// imap sidecar with *_env indirection
	imapCfg := map[string]interface{}{
		"accounts": []interface{}{
			map[string]interface{}{
				"email_address":      "alice@gmail.com",
				"email_password_env": "GMAIL_APP_PASS",
			},
		},
	}
	body, _ := json.MarshalIndent(imapCfg, "", "  ")
	if err := os.WriteFile(filepath.Join(secretsDir, "imap.json"), body, 0o600); err != nil {
		t.Fatal(err)
	}

	// init.json with imap (config-ref) + telegram (inline)
	init := map[string]interface{}{
		"manifest":      map[string]interface{}{"agent_name": "alice"},
		"env_file":      envFilePath,
		"covenant_file": "/some/path",
		"addons": map[string]interface{}{
			"imap": map[string]interface{}{"config": ".secrets/imap.json"},
			"telegram": map[string]interface{}{
				"accounts": []interface{}{
					map[string]interface{}{
						"alias":     "mybot",
						"bot_token": "1:abc",
					},
				},
			},
		},
	}
	initBody, _ := json.MarshalIndent(init, "", "  ")
	initPath := filepath.Join(agentDir, "init.json")
	if err := os.WriteFile(initPath, initBody, 0o644); err != nil {
		t.Fatal(err)
	}

	// Drive the registry-aware Run() so we exercise the version bump too.
	if err := Run(lingtaiDir); err != nil {
		t.Fatal(err)
	}

	// Verify init.json shape
	updatedInit, _ := os.ReadFile(initPath)
	var got map[string]interface{}
	if err := json.Unmarshal(updatedInit, &got); err != nil {
		t.Fatal(err)
	}
	if _, ok := got["addons"].([]interface{}); !ok {
		t.Errorf("addons not converted to list: %T (%v)", got["addons"], got["addons"])
	}
	if got["covenant_file"] != "/some/path" {
		t.Error("covenant_file lost")
	}
	mcp, ok := got["mcp"].(map[string]interface{})
	if !ok {
		t.Fatalf("mcp section missing or wrong type: %T", got["mcp"])
	}
	if _, ok := mcp["imap"]; !ok {
		t.Error("mcp.imap missing")
	}
	if _, ok := mcp["telegram"]; !ok {
		t.Error("mcp.telegram missing")
	}

	// Verify *_env was resolved from envFilePath
	resolvedImap, _ := os.ReadFile(filepath.Join(secretsDir, "imap.json"))
	var rImap map[string]interface{}
	json.Unmarshal(resolvedImap, &rImap)
	acct := rImap["accounts"].([]interface{})[0].(map[string]interface{})
	if _, hasEnv := acct["email_password_env"]; hasEnv {
		t.Error("email_password_env should have been resolved + dropped")
	}
	if acct["email_password"] != "top-secret-app-pw" {
		t.Errorf("email_password = %v, want top-secret-app-pw", acct["email_password"])
	}

	// Verify telegram inline kwargs were materialized to .secrets/telegram.json
	tgPath := filepath.Join(secretsDir, "telegram.json")
	if _, err := os.Stat(tgPath); err != nil {
		t.Errorf("telegram sidecar not materialized: %v", err)
	}

	// Verify version bumped to CurrentVersion
	metaData, _ := os.ReadFile(filepath.Join(lingtaiDir, "meta.json"))
	var meta map[string]interface{}
	json.Unmarshal(metaData, &meta)
	if int(meta["version"].(float64)) != CurrentVersion {
		t.Errorf("version = %v, want %d", meta["version"], CurrentVersion)
	}
}
