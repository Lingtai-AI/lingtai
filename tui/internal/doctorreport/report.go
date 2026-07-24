// Package doctorreport writes a redacted, GitHub-ready diagnostic bundle from a
// completed /doctor run. It is the single owner of redaction for doctor output
// so that future report surfaces route through one conservative filter rather
// than re-implementing ad-hoc secret stripping.
//
// Scope note: this package intentionally does NOT collect raw event logs,
// prompts, tool inputs/outputs, command text, or any events.jsonl tail. The
// bundle is built solely from the visible doctor result lines plus a small set
// of safe metadata the doctor already surfaces (provider/model/base host /
// compatibility flag and whether an API key is present — never the key itself).
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

	redactionMarker = "[REDACTED]"
)

// Severity mirrors the visible doctor line kinds so the saved report keeps the
// same OK / warn / fail / hint / info framing the user saw on screen.
type Severity string

const (
	SeverityOK   Severity = "ok"
	SeverityWarn Severity = "warn"
	SeverityFail Severity = "fail"
	SeverityHint Severity = "hint"
	SeverityInfo Severity = "info"
)

// Line is one visible diagnostic line carried into the report.
type Line struct {
	Severity Severity `json:"severity"`
	Text     string   `json:"text"`
}

// LLMConfig carries only the safe, support-useful slice of the agent's LLM
// configuration. The API key value is never carried — APIKeyPresent records
// only whether one resolved, and APIKeyEnv names the env var (not its value).
type LLMConfig struct {
	Provider      string `json:"provider,omitempty"`
	Model         string `json:"model,omitempty"`
	BaseHost      string `json:"base_host,omitempty"`
	APICompat     string `json:"api_compat,omitempty"`
	APIKeyEnv     string `json:"api_key_env,omitempty"`
	APIKeyPresent bool   `json:"api_key_present"`
}

// Draft is the in-memory capture of a finished /doctor run. The TUI builds one
// when the diagnostic completes and hands it to Write when the user presses the
// save key — Write is never allowed to re-run the diagnostic.
type Draft struct {
	GeneratedAt time.Time `json:"generated_at"`
	AgentName   string    `json:"agent_name,omitempty"`
	Lines       []Line    `json:"lines"`
	LLM         LLMConfig `json:"llm"`
}

type metadataFile struct {
	SchemaVersion string           `json:"schema_version"`
	GeneratedAt   string           `json:"generated_at_utc"`
	AgentName     string           `json:"agent_name,omitempty"`
	LineCounts    map[Severity]int `json:"line_counts"`
	LLM           LLMConfig        `json:"llm"`
}

type redactionFile struct {
	SchemaVersion string   `json:"schema_version"`
	Applied       bool     `json:"applied"`
	Marker        string   `json:"marker"`
	Rules         []string `json:"rules"`
	Note          string   `json:"note"`
}

// Write redacts the draft and writes the report bundle into dir, which the
// caller has already made unique (see the TUI's exclusive-create path). The
// bundle is three files — report.md, metadata.json, redaction.json. dir is
// created 0700 and every file 0600 so reports stay private to the user.
//
// Write performs no diagnostics, no network calls, and reads no event logs.
func Write(dir string, draft Draft) error {
	if dir == "" {
		return errors.New("report directory is required")
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	safe := redactDraft(draft)

	writes := map[string][]byte{
		"report.md":      []byte(renderMarkdown(safe)),
		"metadata.json":  mustJSON(metadataFor(safe)),
		"redaction.json": mustJSON(redactionInfo()),
	}
	for name, data := range writes {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			return err
		}
	}
	return nil
}

func metadataFor(draft Draft) metadataFile {
	counts := make(map[Severity]int)
	for _, line := range draft.Lines {
		sev := line.Severity
		if sev == "" {
			sev = SeverityInfo
		}
		counts[sev]++
	}
	return metadataFile{
		SchemaVersion: MetadataSchemaVersion,
		GeneratedAt:   generatedAt(draft).Format(time.RFC3339),
		AgentName:     draft.AgentName,
		LineCounts:    counts,
		LLM:           draft.LLM,
	}
}

func redactionInfo() redactionFile {
	return redactionFile{
		SchemaVersion: RedactionSchemaVersion,
		Applied:       true,
		Marker:        redactionMarker,
		Rules: []string{
			"bearer_tokens",
			"secret_like_assignments",
			"json_secret_fields",
			"url_credentials",
			"home_usernames",
			"known_api_key_shapes",
			"telegram_bot_tokens",
		},
		Note: "Built only from visible /doctor result lines and safe metadata. " +
			"No raw event logs, prompts, tool I/O, command text, or API key values are collected.",
	}
}

func renderMarkdown(draft Draft) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# LingTai Doctor Report\n\n")
	fmt.Fprintf(&b, "- schema_version: %s\n", ReportSchemaVersion)
	fmt.Fprintf(&b, "- generated_at_utc: %s\n", generatedAt(draft).Format(time.RFC3339))
	if draft.AgentName != "" {
		fmt.Fprintf(&b, "- agent: %s\n", draft.AgentName)
	}

	fmt.Fprintf(&b, "\n## LLM configuration\n\n")
	writeField(&b, "provider", draft.LLM.Provider)
	writeField(&b, "model", draft.LLM.Model)
	writeField(&b, "base_host", draft.LLM.BaseHost)
	writeField(&b, "api_compat", draft.LLM.APICompat)
	writeField(&b, "api_key_env", draft.LLM.APIKeyEnv)
	fmt.Fprintf(&b, "- api_key_present: %t\n", draft.LLM.APIKeyPresent)

	fmt.Fprintf(&b, "\n## Findings\n\n")
	for _, line := range draft.Lines {
		sev := line.Severity
		if sev == "" {
			sev = SeverityInfo
		}
		fmt.Fprintf(&b, "- [%s] %s\n", sev, line.Text)
	}

	fmt.Fprintf(&b, "\n---\n\n")
	fmt.Fprintf(&b, "_Redacted before saving (%s). See redaction.json for the rules applied._\n", redactionMarker)
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

// --- Redaction ---

// redactDraft returns a copy of draft with secret-shaped substrings stripped
// from every free-text field. It does not mutate the caller's draft.
func redactDraft(draft Draft) Draft {
	draft.AgentName = redactText(draft.AgentName)
	draft.LLM.Provider = redactText(draft.LLM.Provider)
	draft.LLM.Model = redactText(draft.LLM.Model)
	draft.LLM.BaseHost = redactText(draft.LLM.BaseHost)
	draft.LLM.APICompat = redactText(draft.LLM.APICompat)
	draft.LLM.APIKeyEnv = redactText(draft.LLM.APIKeyEnv)

	lines := make([]Line, len(draft.Lines))
	for i, line := range draft.Lines {
		line.Text = redactText(line.Text)
		lines[i] = line
	}
	draft.Lines = lines
	return draft
}

// redactText applies the conservative secret filters to a single string.
func redactText(s string) string {
	if s == "" {
		return ""
	}
	s = redactURLCredentials(s)
	s = redactHomeUsernames(s)
	s = bearerTokenRe.ReplaceAllString(s, "Bearer "+redactionMarker)
	s = jsonSecretFieldRe.ReplaceAllString(s, `${1}"`+redactionMarker+`"`)
	s = assignmentSecretRe.ReplaceAllString(s, `${1}=`+redactionMarker)
	s = apiKeyShapeRe.ReplaceAllString(s, redactionMarker)
	s = telegramBotURLTokenRe.ReplaceAllString(s, `${1}`+redactionMarker+`${3}`)
	s = telegramBotTokenRe.ReplaceAllString(s, `${1}`+redactionMarker)
	return s
}

func redactURLCredentials(s string) string {
	s = urlCredentialRe.ReplaceAllString(s, `${1}`+redactionMarker+`@`)
	return hostCredentialRe.ReplaceAllString(s, redactionMarker+"@")
}

func redactHomeUsernames(s string) string {
	s = macHomeRe.ReplaceAllString(s, "/Users/"+redactionMarker)
	return linuxHomeRe.ReplaceAllString(s, "/home/"+redactionMarker)
}

var (
	urlCredentialRe   = regexp.MustCompile(`(?i)(\b[a-z][a-z0-9+.-]*://)[^\s/@:]+:[^\s/@]+@`)
	hostCredentialRe  = regexp.MustCompile(`\b[^\s/@:]+:[^\s/@]+@`)
	macHomeRe         = regexp.MustCompile(`/Users/[A-Za-z0-9._-]+`)
	linuxHomeRe       = regexp.MustCompile(`/home/[A-Za-z0-9._-]+`)
	bearerTokenRe     = regexp.MustCompile(`(?i)\bBearer\s+[A-Za-z0-9._~+/=-]+`)
	jsonSecretFieldRe = regexp.MustCompile(
		`(?i)("(?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|token|secret|password|authorization|credential)"\s*:\s*)("[^"]*")`,
	)
	assignmentSecretRe = regexp.MustCompile(
		`(?i)\b((?:api[_-]?key|apikey|access[_-]?token|refresh[_-]?token|token|secret|password|authorization|credential))\s*=\s*("[^"]*"|'[^']*'|[^\s,}]+)`,
	)
	apiKeyShapeRe = regexp.MustCompile(`\b(?:sk-ant|sk-proj|sk)-[A-Za-z0-9_-]{8,}\b`)
	// Canonical Telegram Bot API and file-download paths place the token directly
	// after the literal "/bot", so redact that narrow path shape before applying
	// the generic leading-boundary rule below.
	telegramBotURLTokenRe = regexp.MustCompile(`(?i)(/bot)([0-9]{6,12}:[A-Za-z0-9_-]{30,})(/|$)`)
	// Telegram bot tokens are digits (6 to 12) followed by a colon and at least
	// 30 token characters. A leading boundary keeps ordinary embedded text intact
	// without consuming the delimiter that may introduce a following token.
	telegramBotTokenRe = regexp.MustCompile(`(^|[^A-Za-z0-9_-])([0-9]{6,12}:[A-Za-z0-9_-]{30,})`)
)
