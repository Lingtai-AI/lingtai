package doctorreport

import (
	"encoding/json"
	"io/fs"
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
		GeneratedAt: time.Date(2026, 6, 26, 18, 30, 0, 0, time.UTC),
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

	got := readDirNames(t, out)
	want := []string{"metadata.json", "redaction.json", "report.md"}
	if !slices.Equal(got, want) {
		t.Fatalf("artifact set mismatch\ngot  %v\nwant %v", got, want)
	}

	// No log tail / events file is ever written.
	for _, name := range got {
		if strings.Contains(name, "log") || strings.Contains(name, "events") {
			t.Fatalf("unexpected log-like artifact %q (log tail capture is prohibited)", name)
		}
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

func TestWriteUsesPrivatePermissions(t *testing.T) {
	out := filepath.Join(t.TempDir(), "doctor-report")
	if err := Write(out, Draft{Lines: []Line{{Severity: SeverityOK, Text: "ok"}}}); err != nil {
		t.Fatalf("Write: %v", err)
	}

	dirInfo, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if perm := dirInfo.Mode().Perm(); perm != 0o700 {
		t.Fatalf("dir perm = %#o, want 0700", perm)
	}

	err = filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file %s perm = %#o, want 0600", path, perm)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
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
		GeneratedAt: time.Date(2026, 6, 26, 18, 30, 0, 0, time.UTC),
		AgentName:   "agent-1",
		Lines: []Line{
			{Severity: SeverityFail, Text: "api_key=" + rawAPIKey},
			{Severity: SeverityFail, Text: "authorization failed: " + bearerToken},
			{Severity: SeverityWarn, Text: "proxy url " + urlCredentials},
			{Severity: SeverityWarn, Text: "home path " + homePath},
			{Severity: SeverityFail, Text: `{"api_key":"` + jsonSecret + `","refresh_token":"` + refreshToken + `"}`},
		},
		LLM: LLMConfig{
			Provider:      "custom",
			Model:         "claude-sonnet-4",
			BaseHost:      "fixtureuser:url-password@example.com",
			APIKeyEnv:     "SECRET_ENV",
			APIKeyPresent: true,
		},
	}

	if err := Write(out, draft); err != nil {
		t.Fatalf("Write: %v", err)
	}

	all := readAllArtifacts(t, out)
	for _, raw := range []string{
		rawAPIKey,
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

func TestWriteDoesNotMutateCallerDraft(t *testing.T) {
	original := "api_key=sk-test-rawapikey123456"
	draft := Draft{Lines: []Line{{Severity: SeverityFail, Text: original}}}
	if err := Write(t.TempDir(), draft); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if draft.Lines[0].Text != original {
		t.Fatalf("Write mutated caller draft: %q", draft.Lines[0].Text)
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
