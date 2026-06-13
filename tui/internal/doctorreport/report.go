package doctorreport

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const (
	ReportSchemaVersion    = "lingtai.doctor.report.v1"
	MetadataSchemaVersion  = "lingtai.doctor.metadata.v1"
	RedactionSchemaVersion = "lingtai.doctor.redaction.v1"

	maxLogTailLines = 20
)

type Severity string

const (
	SeverityOK   Severity = "ok"
	SeverityWarn Severity = "warn"
	SeverityFail Severity = "fail"
	SeverityHint Severity = "hint"
	SeverityInfo Severity = "info"
)

type Line struct {
	Severity Severity `json:"severity"`
	Text     string   `json:"text"`
}

type LLMConfig struct {
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	BaseHost      string `json:"base_host,omitempty"`
	APICompat     string `json:"api_compat,omitempty"`
	APIKeyEnv     string `json:"api_key_env,omitempty"`
	APIKeyPresent bool   `json:"api_key_present"`
}

type Draft struct {
	GeneratedAt time.Time `json:"generated_at"`
	AgentName   string    `json:"agent_name,omitempty"`
	Lines       []Line    `json:"lines"`
	LLM         LLMConfig `json:"llm"`
	LogTail     []string  `json:"log_tail,omitempty"`
}

type metadataFile struct {
	SchemaVersion string           `json:"schema_version"`
	GeneratedAt   string           `json:"generated_at_utc"`
	AgentName     string           `json:"agent_name,omitempty"`
	LineCounts    map[Severity]int `json:"line_counts"`
	LLM           LLMConfig        `json:"llm"`
	HasLogTail    bool             `json:"has_log_tail"`
	LogTailLines  int              `json:"log_tail_lines,omitempty"`
}

type redactionFile struct {
	SchemaVersion string   `json:"schema_version"`
	Applied       bool     `json:"applied"`
	Marker        string   `json:"marker"`
	Rules         []string `json:"rules"`
}

func Write(dir string, draft Draft) error {
	if dir == "" {
		return errors.New("report directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	redactor := newRedactor()
	safeDraft := redactor.draft(draft)
	logTail := redactor.logTail(draft.LogTail)

	writes := map[string][]byte{
		"report.md":      []byte(renderMarkdown(safeDraft)),
		"metadata.json":  mustJSON(metadataFor(safeDraft, len(logTail))),
		"redaction.json": mustJSON(redactionFor()),
	}
	if len(logTail) > 0 {
		writes["logs.tail.redacted.jsonl"] = []byte(strings.Join(logTail, "\n") + "\n")
	}

	for name, data := range writes {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func metadataFor(draft Draft, logTailLines int) metadataFile {
	counts := make(map[Severity]int)
	for _, line := range draft.Lines {
		severity := line.Severity
		if severity == "" {
			severity = SeverityInfo
		}
		counts[severity]++
	}
	return metadataFile{
		SchemaVersion: MetadataSchemaVersion,
		GeneratedAt:   generatedAt(draft).Format(time.RFC3339),
		AgentName:     draft.AgentName,
		LineCounts:    counts,
		LLM:           draft.LLM,
		HasLogTail:    logTailLines > 0,
		LogTailLines:  logTailLines,
	}
}

func redactionFor() redactionFile {
	return redactionFile{
		SchemaVersion: RedactionSchemaVersion,
		Applied:       true,
		Marker:        "[REDACTED]",
		Rules: []string{
			"bearer_tokens",
			"secret_like_keys",
			"url_credentials",
			"home_usernames",
			"known_api_key_shapes",
		},
	}
}

func renderMarkdown(draft Draft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# LingTai Doctor Report\n\n")
	fmt.Fprintf(&b, "schema_version: %s\n", ReportSchemaVersion)
	fmt.Fprintf(&b, "generated_at_utc: %s\n", generatedAt(draft).Format(time.RFC3339))
	if draft.AgentName != "" {
		fmt.Fprintf(&b, "agent_name: %s\n", draft.AgentName)
	}
	fmt.Fprintf(&b, "\n## LLM Configuration\n\n")
	writeField(&b, "provider", draft.LLM.Provider)
	writeField(&b, "model", draft.LLM.Model)
	writeField(&b, "base_host", draft.LLM.BaseHost)
	writeField(&b, "api_compat", draft.LLM.APICompat)
	writeField(&b, "api_key_env", draft.LLM.APIKeyEnv)
	fmt.Fprintf(&b, "- api_key_present: %t\n", draft.LLM.APIKeyPresent)

	fmt.Fprintf(&b, "\n## Findings\n\n")
	for _, line := range draft.Lines {
		severity := line.Severity
		if severity == "" {
			severity = SeverityInfo
		}
		fmt.Fprintf(&b, "- [%s] %s\n", severity, line.Text)
	}
	return b.String()
}

func writeField(b *strings.Builder, key, value string) {
	if value == "" {
		return
	}
	fmt.Fprintf(b, "- %s: %s\n", key, value)
}

func generatedAt(draft Draft) time.Time {
	if draft.GeneratedAt.IsZero() {
		return time.Now().UTC()
	}
	return draft.GeneratedAt.UTC()
}

func mustJSON(v any) []byte {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		panic(err)
	}
	return append(data, '\n')
}

type redactor struct{}

func newRedactor() redactor {
	return redactor{}
}

func (r redactor) draft(draft Draft) Draft {
	draft.AgentName = r.text(draft.AgentName)
	draft.LLM.Provider = r.text(draft.LLM.Provider)
	draft.LLM.Model = r.text(draft.LLM.Model)
	draft.LLM.BaseHost = r.text(draft.LLM.BaseHost)
	draft.LLM.APICompat = r.text(draft.LLM.APICompat)
	draft.LLM.APIKeyEnv = r.text(draft.LLM.APIKeyEnv)
	for i := range draft.Lines {
		draft.Lines[i].Text = r.text(draft.Lines[i].Text)
	}
	draft.LogTail = nil
	return draft
}

func (r redactor) logTail(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	if len(lines) > maxLogTailLines {
		lines = lines[len(lines)-maxLogTailLines:]
	}
	redacted := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		redacted = append(redacted, r.logLine(line))
	}
	return redacted
}

func (r redactor) logLine(line string) string {
	var raw any
	if err := json.Unmarshal([]byte(line), &raw); err == nil {
		data, err := json.Marshal(r.jsonValue(raw))
		if err == nil {
			return string(data)
		}
	}
	return r.text(line)
}

func (r redactor) jsonValue(v any) any {
	switch typed := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(typed))
		for key, value := range typed {
			if secretLikeKey(key) {
				out[key] = "[REDACTED]"
				continue
			}
			out[key] = r.jsonValue(value)
		}
		return out
	case []any:
		out := make([]any, len(typed))
		for i, value := range typed {
			out[i] = r.jsonValue(value)
		}
		return out
	case string:
		return r.text(typed)
	default:
		return typed
	}
}

func (r redactor) text(s string) string {
	if s == "" {
		return ""
	}
	s = redactURLCredentials(s)
	s = redactHomeUsernames(s)
	s = bearerTokenRe.ReplaceAllString(s, "Bearer [REDACTED]")
	s = jsonSecretFieldRe.ReplaceAllString(s, `${1}"[REDACTED]"`)
	s = assignmentSecretRe.ReplaceAllString(s, `${1}=[REDACTED]`)
	s = apiKeyShapeRe.ReplaceAllString(s, "[REDACTED]")
	return s
}

func secretLikeKey(key string) bool {
	normalized := strings.ToLower(strings.ReplaceAll(strings.ReplaceAll(key, "-", "_"), " ", "_"))
	for _, marker := range []string{
		"api_key",
		"apikey",
		"token",
		"secret",
		"password",
		"authorization",
		"credential",
	} {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	return false
}

func redactURLCredentials(s string) string {
	s = urlCredentialRe.ReplaceAllString(s, `${1}[REDACTED]@`)
	return hostCredentialRe.ReplaceAllString(s, `[REDACTED]@`)
}

func redactHomeUsernames(s string) string {
	s = macHomeRe.ReplaceAllString(s, "/Users/[REDACTED]")
	return linuxHomeRe.ReplaceAllString(s, "/home/[REDACTED]")
}

var (
	urlCredentialRe   = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/@:]+:[^\s/@]+@`)
	hostCredentialRe  = regexp.MustCompile(`\b[^\s/@:]+:[^\s/@]+@`)
	macHomeRe         = regexp.MustCompile(`/Users/[A-Za-z0-9._-]+`)
	linuxHomeRe       = regexp.MustCompile(`/home/[A-Za-z0-9._-]+`)
	bearerTokenRe     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	jsonSecretFieldRe = regexp.MustCompile(
		`(?i)((?:"(?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|token|secret|password|authorization|credential)"|(?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|token|secret|password|authorization|credential))\s*:\s*)("[^"]*"|[^,\s}]+)`,
	)
	assignmentSecretRe = regexp.MustCompile(
		`(?i)\b((?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|token|secret|password|authorization|credential))\s*=\s*("[^"]*"|'[^']*'|[^\s,}]+)`,
	)
	apiKeyShapeRe = regexp.MustCompile(`\b(?:sk|sk-ant|sk-proj)-[A-Za-z0-9_-]{8,}\b`)
)
