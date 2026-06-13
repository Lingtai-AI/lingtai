package doctorreport

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestWriteCreatesMinimalArtifactsWithSchemaVersions(t *testing.T) {
	out := t.TempDir()
	draft := Draft{
		GeneratedAt: time.Date(2026, 6, 12, 18, 30, 0, 0, time.UTC),
		AgentName:   "agent-1",
		Lines: []Line{
			{Severity: SeverityOK, Text: "TUI version dev"},
			{Severity: SeverityWarn, Text: "heartbeat stale"},
			{Severity: SeverityFail, Text: "LLM auth failed"},
			{Severity: SeverityHint, Text: "refresh credentials"},
		},
		LLM: LLMConfig{
			Provider:      "custom",
			Model:         "claude-sonnet-4",
			BaseHost:      "api.example.com",
			APICompat:     "anthropic",
			APIKeyEnv:     "ANTHROPIC_API_KEY",
			APIKeyPresent: true,
		},
	}

	if err := Write(out, draft); err != nil {
		t.Fatalf("Write: %v", err)
	}

	gotEntries := readDirNames(t, out)
	wantEntries := []string{"metadata.json", "redaction.json", "report.md"}
	if !slices.Equal(gotEntries, wantEntries) {
		t.Fatalf("artifact set mismatch\ngot  %v\nwant %v", gotEntries, wantEntries)
	}

	var metadata map[string]any
	readJSONFile(t, filepath.Join(out, "metadata.json"), &metadata)
	if metadata["schema_version"] != MetadataSchemaVersion {
		t.Fatalf("metadata schema_version = %v, want %q", metadata["schema_version"], MetadataSchemaVersion)
	}
	if metadata["agent_name"] != "agent-1" {
		t.Fatalf("metadata agent_name = %v", metadata["agent_name"])
	}

	var redaction map[string]any
	readJSONFile(t, filepath.Join(out, "redaction.json"), &redaction)
	if redaction["schema_version"] != RedactionSchemaVersion {
		t.Fatalf("redaction schema_version = %v, want %q", redaction["schema_version"], RedactionSchemaVersion)
	}
	if redaction["applied"] != true {
		t.Fatalf("redaction applied = %v, want true", redaction["applied"])
	}
}

func TestWriteRedactsSecretsAcrossArtifacts(t *testing.T) {
	out := t.TempDir()
	rawAPIKey := "sk-test-rawapikey123456"
	bearerToken := "Bearer bearer-token-raw-123456"
	urlCredentials := "https://fixtureuser:url-password@example.com/v1"
	homePath := "/Users/fixtureuser/.lingtai/agents/agent-1"
	jsonSecret := "json-secret-raw-123456"
	refreshToken := "refresh-token-raw-123456"

	draft := Draft{
		GeneratedAt: time.Date(2026, 6, 12, 18, 30, 0, 0, time.UTC),
		AgentName:   "agent-1",
		Lines: []Line{
			{Severity: SeverityFail, Text: "api_key=" + rawAPIKey},
			{Severity: SeverityFail, Text: "authorization failed: " + bearerToken},
			{Severity: SeverityWarn, Text: "proxy url " + urlCredentials},
			{Severity: SeverityWarn, Text: "home path " + homePath},
			{Severity: SeverityFail, Text: `{"api_key":"` + jsonSecret + `","nested":{"refresh_token":"` + refreshToken + `"}}`},
		},
		LLM: LLMConfig{
			Provider:      "custom",
			Model:         "claude-sonnet-4",
			BaseHost:      "fixtureuser:url-password@example.com",
			APIKeyEnv:     "SECRET_ENV",
			APIKeyPresent: true,
		},
		LogTail: []string{
			`{"type":"aed_attempt","api_key":"` + jsonSecret + `","authorization":"` + bearerToken + `"}`,
		},
	}

	if err := Write(out, draft); err != nil {
		t.Fatalf("Write: %v", err)
	}

	all := readAllArtifacts(t, out)
	for _, raw := range []string{
		rawAPIKey,
		bearerToken,
		"bearer-token-raw-123456",
		"fixtureuser:url-password",
		"url-password",
		"/Users/fixtureuser",
		jsonSecret,
		refreshToken,
	} {
		if strings.Contains(all, raw) {
			t.Fatalf("artifact content leaked raw secret %q:\n%s", raw, all)
		}
	}
	if !strings.Contains(all, "[REDACTED]") {
		t.Fatalf("expected redaction marker in artifacts:\n%s", all)
	}
}

func TestWriteRedactsMalformedJSONLTailAndCaps(t *testing.T) {
	out := t.TempDir()
	logTail := make([]string, 0, maxLogTailLines+8)
	for i := 0; i < maxLogTailLines+6; i++ {
		logTail = append(logTail, `{"type":"debug","message":"noise"}`)
	}
	logTail = append(logTail,
		`{"type":"aed_attempt","api_key":"valid-json-secret-raw-123456","message":"failed"}`,
		`not-json api_key=malformed-json-secret-raw-123456 Authorization: Bearer malformed-bearer-token-123456 url=https://bob:pw@example.com/v1`,
	)
	draft := Draft{
		GeneratedAt: time.Date(2026, 6, 12, 18, 30, 0, 0, time.UTC),
		AgentName:   "agent-1",
		Lines:       []Line{{Severity: SeverityFail, Text: "LLM failed"}},
		LogTail:     logTail,
	}

	if err := Write(out, draft); err != nil {
		t.Fatalf("Write: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(out, "logs.tail.redacted.jsonl"))
	if err != nil {
		t.Fatalf("read redacted log tail: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > maxLogTailLines {
		t.Fatalf("log tail has %d lines, want at most %d", len(lines), maxLogTailLines)
	}
	validJSONLine := lines[len(lines)-2]
	if !json.Valid([]byte(validJSONLine)) {
		t.Fatalf("valid JSONL input should stay JSON after redaction: %q", validJSONLine)
	}
	all := string(data)
	for _, raw := range []string{
		"valid-json-secret-raw-123456",
		"malformed-json-secret-raw-123456",
		"malformed-bearer-token-123456",
		"bob:pw",
	} {
		if strings.Contains(all, raw) {
			t.Fatalf("redacted log tail leaked %q:\n%s", raw, all)
		}
	}
	if !strings.Contains(all, "[REDACTED]") {
		t.Fatalf("expected redaction marker in log tail:\n%s", all)
	}
}

func readDirNames(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir(%s): %v", dir, err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	return names
}

func readJSONFile(t *testing.T, path string, out any) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	if err := json.Unmarshal(data, out); err != nil {
		t.Fatalf("Unmarshal(%s): %v\n%s", path, err, data)
	}
}

func readAllArtifacts(t *testing.T, dir string) string {
	t.Helper()
	var b strings.Builder
	for _, name := range readDirNames(t, dir) {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("ReadFile(%s): %v", name, err)
		}
		b.Write(data)
		b.WriteByte('\n')
	}
	return b.String()
}
