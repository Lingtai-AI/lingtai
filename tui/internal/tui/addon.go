package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// AddonSavedMsg is sent when the MCP control panel is dismissed.
type AddonSavedMsg struct{}

// AddonModel is the /mcp control panel — a read-only view of each MCP bridge's
// configuration and status. The Go type keeps the historical Addon* naming, but
// the slash-command is /mcp only: PR #204 retired /addon and
// TestDefaultCommandsDoesNotKeepAddonAlias guards against it returning.
//
// Each MCP server (IMAP, Telegram, Feishu, WeChat) is configured by a file at
// {lingtaiDir}/.addons/{name}/config.json, a project-level shared location
// (one config file per MCP, multi-account via the accounts array).
type AddonModel struct {
	lingtaiDir string // <project>/.lingtai/ directory
	width      int
	height     int
	// addonConfigs maps addon name → JSON file content (or "" if missing/unreadable)
	addonConfigs map[string]string
	// addonErrors maps addon name → error message (e.g. "not found", "parse error")
	addonErrors map[string]string
}

// NewAddonModel constructs the /mcp control panel. lingtaiDir is the project's
// .lingtai/ directory (parent of all agent dirs). Each MCP bridge's config
// lives at lingtaiDir/.addons/<name>/config.json.
func NewAddonModel(lingtaiDir string) AddonModel {
	configs, errs := readAddonConfigs(lingtaiDir)
	return AddonModel{
		lingtaiDir:   lingtaiDir,
		addonConfigs: configs,
		addonErrors:  errs,
	}
}

func (m AddonModel) Init() tea.Cmd { return nil }

func (m AddonModel) Update(msg tea.Msg) (AddonModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return AddonSavedMsg{} }
		case "ctrl+r":
			m.addonConfigs, m.addonErrors = readAddonConfigs(m.lingtaiDir)
			return m, nil
		}
	}
	return m, nil
}

func (m AddonModel) View() string {
	var b strings.Builder

	// Title bar
	titleText := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent).Render(i18n.T("welcome.title"))
	titleBar := titleText + " " + StyleAccent.Render(RuneBullet) + " " + StyleTitle.Render(i18n.T("mcp.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("mcp.back"))
	padding := m.width - lipgloss.Width(titleBar) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(titleBar + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(titleBar + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")

	// Description
	b.WriteString(StyleSubtle.Render("  "+i18n.T("mcp.readonly_desc")) + "\n\n")

	// Addon list
	for _, name := range AllAddons {
		label := strings.ToUpper(name[:1]) + name[1:]
		configPath := addonConfigRelPath(name)
		b.WriteString("  " + StyleTitle.Render(label) + StyleFaint.Render("  "+configPath) + "\n")

		if errMsg, bad := m.addonErrors[name]; bad {
			b.WriteString("    " + StyleFaint.Render(errMsg) + "\n\n")
			continue
		}

		content, ok := m.addonConfigs[name]
		if !ok || content == "" {
			b.WriteString("    " + StyleFaint.Render(i18n.T("mcp.not_configured")) + "\n\n")
			continue
		}

		// Render through the render-boundary redactor. The panel is
		// scrollback-prone, so a credential must never reach the screen —
		// not on first paint and not after ctrl+r reload.
		pretty := redactAddonJSON(content)
		for _, line := range strings.Split(strings.TrimRight(pretty, "\n"), "\n") {
			b.WriteString("    " + line + "\n")
		}
		b.WriteString("\n")
	}

	// Footer
	b.WriteString(strings.Repeat("─", m.width) + "\n")
	b.WriteString(StyleFaint.Render("  "+i18n.T("mcp.footer_hint")) + "\n")

	return b.String()
}

// addonConfigRelPath returns the canonical path (relative to project root) for
// an addon's config file. This is the only place the convention is defined —
// all other code uses this helper.
func addonConfigRelPath(addon string) string {
	return filepath.Join(".lingtai", ".addons", addon, "config.json")
}

// AddonConfigPath returns the absolute path to an addon's config file, given
// the project's .lingtai/ directory. Exported for use by other packages.
func AddonConfigPath(lingtaiDir, addon string) string {
	return filepath.Join(lingtaiDir, ".addons", addon, "config.json")
}

// readAddonConfigs reads {lingtaiDir}/.addons/{addon}/config.json for each
// known addon. Returns (configs, errors): configs holds addon→JSON-content
// for successful reads, errors holds addon→error-message for files that
// exist but couldn't be parsed. Addons with no file at all appear in neither map.
func readAddonConfigs(lingtaiDir string) (map[string]string, map[string]string) {
	configs := make(map[string]string)
	errs := make(map[string]string)
	if lingtaiDir == "" {
		return configs, errs
	}

	for _, addon := range AllAddons {
		configPath := AddonConfigPath(lingtaiDir, addon)
		data, err := os.ReadFile(configPath)
		if err != nil {
			// File missing or unreadable — not an error, just "not configured"
			continue
		}
		// Validate it parses as JSON; if not, report as an error
		var probe any
		if jerr := json.Unmarshal(data, &probe); jerr != nil {
			errs[addon] = i18n.TF("mcp.parse_error", jerr.Error())
			continue
		}
		configs[addon] = string(data)
	}
	return configs, errs
}

// addonRedactedValue replaces every value whose key is classified as a
// credential. It is a whole-value mask on purpose: this panel is read in a
// terminal scrollback that cannot be retracted, so no first/last characters
// survive (unlike maskKey/maskAPIKey, which are for one-shot entry fields).
// The spelling matches doctorreport's existing redactionMarker.
const addonRedactedValue = "[REDACTED]"

// addonUndecodableJSON is rendered instead of the config body when the JSON
// cannot be decoded or re-encoded. It is constant by design: this boundary is
// fail-closed and must never fall back to the original bytes.
const addonUndecodableJSON = "[REDACTED: config could not be decoded]"

// redactAddonJSON renders an addon config for the /mcp panel: parse, mask every
// credential value, re-encode. It never returns its input — a decode or encode
// failure yields addonUndecodableJSON. readAddonConfigs already rejects
// undecodable files before they reach the model, so the decode branch is the
// second line of defence, not the first.
func redactAddonJSON(data string) string {
	var v any
	if err := json.Unmarshal([]byte(data), &v); err != nil {
		return addonUndecodableJSON
	}
	return marshalRedactedJSON(v)
}

// marshalRedactedJSON masks v and re-encodes it. Split out so the fail-closed
// encode branch is directly testable: a tree decoded from JSON always encodes,
// so in production this is defence in depth.
func marshalRedactedJSON(v any) string {
	out, err := json.MarshalIndent(redactAddonValue(v), "", "  ")
	if err != nil {
		return addonUndecodableJSON
	}
	return string(out)
}

// redactAddonValue walks a decoded JSON value and returns an equivalent tree in
// which every value under a credential-classified key is addonRedactedValue.
//
// The whole value is replaced regardless of its type — object and array values
// are never descended into, because a key named "credentials" says nothing
// about the field names nested below it. Non-sensitive containers are walked,
// so a credential at any depth is still masked.
func redactAddonValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if addonKeyIsCredential(k) {
				out[k] = addonRedactedValue
				continue
			}
			out[k] = redactAddonValue(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = redactAddonValue(val)
		}
		return out
	default:
		return v
	}
}

// addonCredentialTerminals holds the single words that make a key a credential
// when they are its last word. Last-word matching (rather than substring
// matching) is what keeps token_limit and tokens_used visible.
//
// The concatenated lowercase spellings are listed explicitly because they have
// no separator or case boundary to split on.
var addonCredentialTerminals = map[string]bool{
	"token": true, "tokens": true,
	"secret": true, "secrets": true,
	"password": true, "passwords": true, "passwd": true,
	"credential": true, "credentials": true,
	"cookie": true, "cookies": true,
	"session":       true,
	"authorization": true, "bearer": true,
	// Unseparated spellings that tokenisation cannot split.
	"apikey": true, "privatekey": true, "secretkey": true,
	"clientsecret": true, "appsecret": true, "emailpassword": true,
	"bottoken": true, "authtoken": true, "sessiontoken": true, "sessionid": true,
	"accesstoken": true, "refreshtoken": true,
}

// addonKeyQualifiers are the words that make a following "key" a credential.
// Bare "key" is deliberately not a credential — it is far too common a name for
// non-secret material — so "key" only matches when qualified. "public" is
// absent on purpose: a public key is publishable.
var addonKeyQualifiers = map[string]bool{
	"api": true, "private": true, "secret": true, "session": true,
	"access": true, "refresh": true, "signing": true, "encryption": true,
	"master": true, "auth": true,
}

// addonCredentialPhrases are ordered word sequences that are credentials even
// though their last word is not a credential terminal: a session identifier is
// a bearer credential, "id" is not.
var addonCredentialPhrases = [][]string{
	{"session", "id"},
	{"session", "ids"},
}

// addonEnvReferenceWord is the last word that marks a field as an
// environment-variable reference rather than a secret. The bundled templates
// use email_password_env / bot_token_env / app_secret_env, and m028 resolves
// exactly those into a sibling base key. A reference names a variable; the
// value it resolves to is the secret, and the base key stays masked.
const addonEnvReferenceWord = "env"

// addonKeyIsCredential reports whether a JSON key names credential material.
// It classifies keys only — never values — so a note that merely mentions
// "token" is left alone.
func addonKeyIsCredential(key string) bool {
	words := splitAddonKey(key)
	if len(words) == 0 {
		return false
	}
	last := words[len(words)-1]
	if last == addonEnvReferenceWord {
		return false
	}
	if addonCredentialTerminals[last] {
		return true
	}
	if (last == "key" || last == "keys") && addonAnyQualifier(words[:len(words)-1]) {
		return true
	}
	return addonMatchesPhrase(words)
}

func addonAnyQualifier(words []string) bool {
	for _, w := range words {
		if addonKeyQualifiers[w] {
			return true
		}
	}
	return false
}

func addonMatchesPhrase(words []string) bool {
	for _, phrase := range addonCredentialPhrases {
		if len(words) < len(phrase) {
			continue
		}
		tail := words[len(words)-len(phrase):]
		matched := true
		for i := range phrase {
			if tail[i] != phrase[i] {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}
	return false
}

// splitAddonKey splits a JSON key into lowercase words at the boundaries the
// bundled configs actually use: snake_case, kebab-case, dots, spaces, digits,
// and lower-to-upper camelCase transitions. An upper-case run stays one word,
// so APIKey reads as "apikey" (matched as a terminal) instead of "a","p","i".
func splitAddonKey(key string) []string {
	var words []string
	var cur []rune
	runes := []rune(key)
	flush := func() {
		if len(cur) > 0 {
			words = append(words, strings.ToLower(string(cur)))
			cur = cur[:0]
		}
	}
	for i, r := range runes {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ' ' || unicode.IsDigit(r):
			flush()
		case unicode.IsUpper(r):
			if i > 0 && !unicode.IsUpper(runes[i-1]) {
				flush()
			}
			cur = append(cur, r)
		default:
			cur = append(cur, r)
		}
	}
	flush()
	return words
}
