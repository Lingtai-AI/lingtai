package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// Item-4 (/mcp raw addon config) redaction coverage.
//
// The panel is a read-only render boundary: it must mask credential VALUES
// whatever their JSON type, keep env-var references and ordinary prose visible,
// and fail closed instead of echoing the original bytes. Values below are
// synthetic so an assertion can prove the exact bytes never reached the screen.

const (
	syntheticIMAPSecretA = "SYNTH-imap-pw-6f1c9a"
	syntheticIMAPSecretB = "SYNTH-imap-pw-reloaded-4d20b8"
	syntheticBotToken    = "SYNTH-bot-token-2b7e4d"
	syntheticAppSecret   = "SYNTH-app-secret-91ac53"
	syntheticNestedJunk  = "SYNTH-nested-field-8c07f2"
)

// --- Real-schema positives: the shapes the shipped IMAP/Telegram/Feishu
// configs actually use, including the nested accounts[] array that m028
// resolves into plaintext. ---

func TestAddonRedactorMasksRealSchemaSecrets(t *testing.T) {
	config := `{
	  "allowed_senders": ["boss@example.com"],
	  "email_address": "agent@example.com",
	  "accounts": [
	    {"email_address": "a@example.com", "email_password": "` + syntheticIMAPSecretA + `"},
	    {"email_address": "b@example.com", "email_password": "` + syntheticIMAPSecretA + `", "bot_token": "` + syntheticBotToken + `", "app_secret": "` + syntheticAppSecret + `"}
	  ]
	}`

	got := redactAddonJSON(config)

	for _, secret := range []string{syntheticIMAPSecretA, syntheticBotToken, syntheticAppSecret} {
		if strings.Contains(got, secret) {
			t.Errorf("rendered config still contains secret %q:\n%s", secret, got)
		}
	}
	// Safe structure must survive: the keys still read, the non-secret values
	// they sit next to are untouched.
	for _, keep := range []string{"email_password", "bot_token", "app_secret", "allowed_senders", "boss@example.com", "agent@example.com", "a@example.com"} {
		if !strings.Contains(got, keep) {
			t.Errorf("rendered config lost safe content %q:\n%s", keep, got)
		}
	}
}

// --- Naming matrix: every separator style and credential concept the review
// requires. ---

func TestAddonRedactorKeyNamingMatrix(t *testing.T) {
	credentialKeys := []string{
		"token", "tokens", "botToken", "bot_token", "bot-token", "BOT_TOKEN", "botToken2",
		"access_token", "refreshToken", "sessionToken", "authToken",
		"secret", "clientSecret", "client_secret", "app_secret", "appSecret",
		"password", "passwd", "email_password", "emailPassword", "Password",
		"credential", "credentials", "Credentials",
		"api_key", "apiKey", "API_KEY", "APIKey", "privateKey", "private_key", "session_key",
		"authorization", "Authorization", "cookie", "cookies", "session",
	}
	for _, key := range credentialKeys {
		t.Run("masked/"+key, func(t *testing.T) {
			got := redactAddonJSON(`{"` + key + `": "SYNTH-value-not-a-real-secret"}`)
			if !strings.Contains(got, addonRedactedValue) {
				t.Errorf("key %q must be masked, got:\n%s", key, got)
			}
			if strings.Contains(got, "SYNTH-value-not-a-real-secret") {
				t.Errorf("key %q leaked its value:\n%s", key, got)
			}
		})
	}

	// `credentials` is an object; its whole value is replaced, so unknown
	// nested fields are never exposed by name or value.
	t.Run("masked/credentials-object", func(t *testing.T) {
		got := redactAddonJSON(`{"credentials": {"nested_field": "` + syntheticNestedJunk + `"}}`)
		if strings.Contains(got, syntheticNestedJunk) || strings.Contains(got, "nested_field") {
			t.Errorf("an object-valued credential key must be replaced wholesale, got:\n%s", got)
		}
	})
}

// --- Type matrix: a sensitive key masks its whole value whatever the type, and
// non-sensitive containers are still walked. ---

func TestAddonRedactorMasksEveryValueType(t *testing.T) {
	cases := []struct {
		name       string
		rawValue   string
		mustAbsent string
	}{
		{"string", `"SYNTH-type-string"`, "SYNTH-type-string"},
		{"number", `8675309`, "8675309"},
		{"bool", `true`, ""},
		{"null", `null`, ""},
		{"array", `["SYNTH-type-array",412]`, "SYNTH-type-array"},
		{"object", `{"inner":"SYNTH-type-object"}`, "SYNTH-type-object"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := redactAddonJSON(`{"bot_token": ` + tc.rawValue + `}`)
			if !strings.Contains(got, `"bot_token": "`+addonRedactedValue+`"`) {
				t.Errorf("bot_token (%s) should render as the mask marker, got:\n%s", tc.name, got)
			}
			if tc.mustAbsent != "" && strings.Contains(got, tc.mustAbsent) {
				t.Errorf("bot_token (%s) leaked %q:\n%s", tc.name, tc.mustAbsent, got)
			}
		})
	}

	t.Run("walks-non-sensitive-containers", func(t *testing.T) {
		got := redactAddonJSON(`{"outer": {"accounts": [{"note": "visible prose", "app_secret": "` + syntheticAppSecret + `"}]}}`)
		if strings.Contains(got, syntheticAppSecret) {
			t.Errorf("a credential nested under non-sensitive containers must still be masked:\n%s", got)
		}
		if !strings.Contains(got, "visible prose") {
			t.Errorf("non-sensitive nested content must survive:\n%s", got)
		}
	})
}

// --- False-positive locks: keys and prose that merely resemble credentials
// stay visible. ---

func TestAddonRedactorKeepsFalsePositives(t *testing.T) {
	config := `{
	  "author": "Runyuan",
	  "monkey": "curious",
	  "keyboard_layout": "us",
	  "token_limit": 200000,
	  "tokens_used": 1234,
	  "note": "rotate the token before the secret expires",
	  "key": "not-a-credential-on-its-own"
	}`
	got := redactAddonJSON(config)

	for _, keep := range []string{"Runyuan", "curious", "us", "200000", "1234"} {
		if !strings.Contains(got, keep) {
			t.Errorf("false-positive lock %q was masked:\n%s", keep, got)
		}
	}
	// Prose is a value, never a key: it is classified by its key only.
	if !strings.Contains(got, "rotate the token before the secret expires") {
		t.Errorf("prose mentioning credential words must not be rewritten:\n%s", got)
	}
	if !strings.Contains(got, "not-a-credential-on-its-own") {
		t.Errorf("an unqualified bare `key` is not a credential:\n%s", got)
	}
}

// --- Reference locks: *_env / camel ...Env name a variable, not a secret, and
// the resolved base key of the same concept stays masked. ---

func TestAddonRedactorKeepsEnvReferences(t *testing.T) {
	config := `{
	  "email_password_env": "IMAP_PASSWORD",
	  "bot_token_env": "TELEGRAM_BOT_TOKEN",
	  "app_secret_env": "FEISHU_APP_SECRET",
	  "clientSecretEnv": "CLIENT_SECRET_VAR",
	  "email_password": "` + syntheticIMAPSecretA + `",
	  "bot_token": "` + syntheticBotToken + `",
	  "app_secret": "` + syntheticAppSecret + `",
	  "clientSecret": "` + syntheticNestedJunk + `"
	}`
	got := redactAddonJSON(config)

	for _, reference := range []string{"IMAP_PASSWORD", "TELEGRAM_BOT_TOKEN", "FEISHU_APP_SECRET", "CLIENT_SECRET_VAR"} {
		if !strings.Contains(got, reference) {
			t.Errorf("env reference %q must remain visible:\n%s", reference, got)
		}
	}
	for _, secret := range []string{syntheticIMAPSecretA, syntheticBotToken, syntheticAppSecret, syntheticNestedJunk} {
		if strings.Contains(got, secret) {
			t.Errorf("resolved base key leaked %q:\n%s", secret, got)
		}
	}
}

// --- Failure policy: fail closed, never echo the original bytes. ---

func TestAddonRedactorFailsClosed(t *testing.T) {
	t.Run("invalid-json", func(t *testing.T) {
		const invalid = `{"bot_token": "` + syntheticBotToken + `",,,`
		got := redactAddonJSON(invalid)
		if got != addonUndecodableJSON {
			t.Errorf("invalid JSON must render the constant fail-closed marker, got:\n%s", got)
		}
		if strings.Contains(got, syntheticBotToken) {
			t.Errorf("invalid JSON must not echo its bytes:\n%s", got)
		}
	})

	t.Run("truncated-json", func(t *testing.T) {
		got := redactAddonJSON(`{"app_secret": "` + syntheticAppSecret + `"`)
		if got != addonUndecodableJSON || strings.Contains(got, syntheticAppSecret) {
			t.Errorf("truncated JSON must fail closed, got:\n%s", got)
		}
	})

	// An encode failure is unreachable from a decoded JSON tree, so the branch
	// is driven directly: a channel value cannot be marshalled.
	t.Run("marshal-error", func(t *testing.T) {
		got := marshalRedactedJSON(map[string]any{"unencodable": make(chan int)})
		if got != addonUndecodableJSON {
			t.Errorf("marshal failure must render the constant fail-closed marker, got:\n%s", got)
		}
	})
}

// --- No partial reveal: the mask is whole-value, unlike maskKey/maskAPIKey. ---

func TestAddonRedactorNeverPartiallyReveals(t *testing.T) {
	secret := "SYNTH-abcdefghijklmnop"
	got := redactAddonJSON(`{"private_key": "` + secret + `"}`)

	if !strings.Contains(got, addonRedactedValue) {
		t.Fatalf("private_key must be masked, got:\n%s", got)
	}
	// No 2-character run of the secret may survive anywhere in the output.
	for i := 0; i+2 <= len(secret); i++ {
		if strings.Contains(got, secret[i:i+2]) {
			t.Fatalf("output leaked the character run %q of the secret:\n%s", secret[i:i+2], got)
		}
	}
}

// --- View integration: first paint and the ctrl+r reload path both go through
// the redactor. ---

func writeAddonConfig(t *testing.T, lingtaiDir, addon, body string) {
	t.Helper()
	path := AddonConfigPath(lingtaiDir, addon)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir addon dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write addon config: %v", err)
	}
}

func TestAddonViewRedactsOnFirstPaintAndReload(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")

	writeAddonConfig(t, lingtaiDir, "imap", `{
	  "email_address": "agent@example.com",
	  "accounts": [{"email_password": "`+syntheticIMAPSecretA+`"}]
	}`)
	writeAddonConfig(t, lingtaiDir, "telegram", `{"bot_token": "`+syntheticBotToken+`"}`)
	writeAddonConfig(t, lingtaiDir, "feishu", `{"app_secret": "`+syntheticAppSecret+`"}`)

	m := NewAddonModel(lingtaiDir)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	first := m.View()
	for _, secret := range []string{syntheticIMAPSecretA, syntheticBotToken, syntheticAppSecret} {
		if strings.Contains(first, secret) {
			t.Errorf("first paint leaked %q:\n%s", secret, first)
		}
	}
	for _, safe := range []string{"email_address", "agent@example.com", "accounts", addonRedactedValue} {
		if !strings.Contains(first, safe) {
			t.Errorf("first paint lost safe structure %q:\n%s", safe, first)
		}
	}

	// Reload path: ctrl+r rereads the files, and the new secret must be masked
	// just like the first one.
	writeAddonConfig(t, lingtaiDir, "imap", `{
	  "email_address": "agent@example.com",
	  "accounts": [{"email_password": "`+syntheticIMAPSecretB+`", "bot_token": "`+syntheticBotToken+`"}]
	}`)
	m, _ = m.Update(ctrlR())

	second := m.View()
	for _, secret := range []string{syntheticIMAPSecretA, syntheticIMAPSecretB, syntheticBotToken} {
		if strings.Contains(second, secret) {
			t.Errorf("reload paint leaked %q:\n%s", secret, second)
		}
	}
	if !strings.Contains(second, "agent@example.com") {
		t.Errorf("reload paint lost safe structure:\n%s", second)
	}
}

// A config file that cannot be decoded is reported by readAddonConfigs and
// never reaches the render boundary, so the panel shows the parse error rather
// than any of the file's bytes.
func TestAddonViewShowsParseErrorInsteadOfUndecodableBytes(t *testing.T) {
	lingtaiDir := filepath.Join(t.TempDir(), ".lingtai")
	writeAddonConfig(t, lingtaiDir, "imap", `{"bot_token": "`+syntheticBotToken+`",`)

	m := NewAddonModel(lingtaiDir)
	m, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	got := m.View()
	if strings.Contains(got, syntheticBotToken) {
		t.Errorf("undecodable config leaked its bytes:\n%s", got)
	}
}

// --- Classifier unit coverage: last-word semantics are what keep the
// false-positive locks above true, so pin them directly. ---

func TestAddonKeyIsCredentialClassifier(t *testing.T) {
	cases := map[string]bool{
		"bot_token":          true,
		"botToken":           true,
		"authorization":      true,
		"cookie":             true,
		"session":            true,
		"session_id":         true,
		"sessionId":          true,
		"sessionid":          true,
		"privateKey":         true,
		"credentials":        true,
		"email_password":     true,
		"app_secret":         true,
		"apiKey":             true,
		"token_limit":        false,
		"tokens_used":        false,
		"author":             false,
		"monkey":             false,
		"keyboard_layout":    false,
		"bot_token_env":      false,
		"app_secret_env":     false,
		"email_password_env": false,
		"clientSecretEnv":    false,
		"key":                false,
		"public_key":         false,
		"allowed_senders":    false,
		"email_address":      false,
		"imap_host":          false,
		"poll_interval":      false,
		"":                   false,
	}
	for key, want := range cases {
		if got := addonKeyIsCredential(key); got != want {
			t.Errorf("addonKeyIsCredential(%q) = %v, want %v", key, got, want)
		}
	}
}

func TestSplitAddonKeyWordBoundaries(t *testing.T) {
	cases := []struct {
		key  string
		want []string
	}{
		{"bot_token", []string{"bot", "token"}},
		{"bot-token", []string{"bot", "token"}},
		{"botToken", []string{"bot", "token"}},
		{"BOT_TOKEN", []string{"bot", "token"}},
		{"APIKey", []string{"apikey"}},
		{"emailPassword", []string{"email", "password"}},
		{"token_limit", []string{"token", "limit"}},
		{"keyboard_layout", []string{"keyboard", "layout"}},
		{"allowlist.entries", []string{"allowlist", "entries"}},
	}
	for _, tc := range cases {
		got := splitAddonKey(tc.key)
		if len(got) != len(tc.want) {
			t.Errorf("splitAddonKey(%q) = %v, want %v", tc.key, got, tc.want)
			continue
		}
		for i := range tc.want {
			if got[i] != tc.want[i] {
				t.Errorf("splitAddonKey(%q) = %v, want %v", tc.key, got, tc.want)
				break
			}
		}
	}
}
