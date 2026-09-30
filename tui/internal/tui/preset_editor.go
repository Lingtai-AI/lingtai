package tui

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"charm.land/bubbles/v2/textarea"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// PresetEditorCommitMsg fires when the editor's working copy passes validation
// and the user completes a save action (normal save, clone-name Enter, or the
// Ctrl+E expert overwrite). Hosts (firstrun, /setup, library) decide what to do
// next — typically: persist via preset.Save, then advance their own state. The
// editor itself does NOT save to disk.
// Preset.Source is always SourceSaved so RefFor names the saved/ file those
// hosts create, including an expert overwrite that keeps a built-in name.
//
// APIKey carries the new key value the user typed in the editor, when
// they actually changed it. Empty means "unchanged — keep whatever's
// already in ~/.lingtai-tui/.env". The host writes this into Config.Keys
// using the preset's api_key_env name as the key.
type PresetEditorCommitMsg struct {
	Preset    preset.Preset
	APIKey    string
	APIKeySet bool   // true when the user typed/changed a value in this session
	Warning   string // localized, sanitized availability warning; never a credential
}

// PresetEditorCancelMsg fires on Esc (and after the dirty-prompt
// confirms discard). Hosts return to whichever screen they came from.
type PresetEditorCancelMsg struct{}

// editorField identifies a row in the form.
type editorField int

const (
	feName editorField = iota
	feSummary
	feTier
	feGains
	feLoses
	feProvider
	feModel
	feServiceTier
	feThinking
	feWireAPI
	feResponsesTransport
	feBaseURL
	feAPIKey
	feStreaming
	feKarma
	feNirvana
	feSave
)

// editorFieldOrder is the rendering order of fields. The cursor walks
// this slice; section headers render between transitions. Capabilities
// are not cursor-navigable or editable fields — every capability the
// kernel can grant an agent is always included, and the Capabilities
// section renders as a fixed informational list below (see formRows'
// capabilityRows), not as form rows.
var editorFieldOrder = []editorField{
	feName, feSummary, feTier, feGains, feLoses,
	feProvider, feModel, feServiceTier, feThinking, feWireAPI, feResponsesTransport, feBaseURL, feAPIKey,
	feSave,
}

// saveFieldIndex is the cursor position of the [Save] button row. Tab
// jumps here from anywhere in the form so paste-and-save is two
// keystrokes away regardless of which field the user is editing.
var saveFieldIndex = len(editorFieldOrder) - 1

type editorMode int

const (
	emBrowse      editorMode = iota // navigating field list
	emInline                        // textinput active for the focused field
	emClonePrompt                   // built-in: prompt for new name on semantic edit
	emDirtyPrompt                   // legacy "discard? y/N" — kept for compat
	emExitPrompt                    // three-way exit on Esc: save / discard / cancel
)

// editorProviders is the provider cycle on the editor's provider row: the four
// provider families the kernel accepts. Every other vendor is reached through
// openai or anthropic pointed at that vendor's compatible endpoint.
var editorProviders = []string{
	preset.ProviderOpenAI,
	preset.ProviderAnthropic,
	preset.ProviderCodex,
	preset.ProviderClaudeCode,
}

// providerModels maps a provider to the curated model lineup the editor cycles
// through with ←/→ on the model row. Only the Codex OAuth route carries a
// catalog: the openai and anthropic families point at arbitrary endpoints, so
// their model is free text, and claude-code has no model row at all (Claude
// Code runs its own default model).
//
// CURATION RULE (tui/CONTRACT.md, "Model list curation"): every family
// listed here ships only its LATEST TWO GENERATIONS. A third-newest
// generation is removed in the same change that adds a new one. Variants
// within one generation (-sol/-terra/-luna) are not separate generations and
// all stay. See tui/internal/tui/SKILL.md for the source list and the rest of
// the inclusion checklist.
var providerModels = map[string][]string{
	// Codex: ChatGPT-OAuth-only models served by chatgpt.com/backend-api/codex.
	// Keep gpt-5.6-sol first to match the TUI default; the other named GPT-5.6
	// routes remain selectable when the endpoint/account enables them. See
	// SKILL.md next to this file for the canonical source list and why each
	// model is included or excluded (e.g. pro-only variants can 4xx).
	//
	// GPT-6 Astra is documented but not proven available on every authenticated
	// OAuth route, so keep the proven gpt-5.6-sol default first. gpt-5.5 is
	// retired from this latest-two curation. Saved presets are never rewritten.
	preset.ProviderCodex: {"gpt-5.6-sol", "gpt-6-astra", "gpt-5.6-terra", "gpt-5.6-luna"},
}

// modelOptions is the single catalog lookup used by every model picker
// surface. A provider without a curated catalog returns nil (free text).
func modelOptions(provider string) []string {
	return providerModels[provider]
}

// isCuratedModel reports whether model is one of any provider's curated
// catalog ids. A curated id belongs to its own route (a Codex OAuth model),
// so it is cleared when the user switches to a free-text provider family
// instead of being sent to an endpoint that does not serve it.
func isCuratedModel(model string) bool {
	for _, models := range providerModels {
		for _, candidate := range models {
			if candidate == model {
				return true
			}
		}
	}
	return false
}

// serviceTierOptions is the normal/fast vocabulary for the providers that
// accept a service tier (openai and codex; fast is sent as priority). The TUI
// does not decide whether a given endpoint honors it.
var serviceTierOptions = []string{"normal", "fast"}

var codexThinkingOptions = []string{"low", "medium", "high", "xhigh"}

// levelThinkingOptions is the reasoning-effort ladder the openai and
// anthropic families offer: the kernel's canonical THINKING_LEVELS tuple
// (lingtai/kernel/config.py) plus a leading "default" pseudo-option.
// "default" is NOT a payload value — the kernel treats an omitted
// manifest.llm.thinking as its own default, so selecting it deletes the key.
// Anthropic maps these levels to a thinking budget; the openai family passes
// them through as reasoning effort on either wire.
var levelThinkingOptions = []string{"default", "none", "minimal", "low", "medium", "high", "xhigh"}

// wireAPIOptions are the openai-family wire formats. Chat Completions is the
// default and is written explicitly so the manifest does not depend on a
// kernel-side default.
var wireAPIOptions = []string{preset.WireAPIChatCompletions, preset.WireAPIResponses}

var responsesTransportOptions = []string{"http", "websocket"}

const presetEditorFieldLabelWidth = 18

// PresetEditorModel is a single-page preset editor. Hosted by the
// firstrun/setup wizard and the library screen via embedding.
type PresetEditorModel struct {
	original preset.Preset // pristine copy for dirty diff + cancel
	working  preset.Preset // mutates as user edits

	// isBuiltin is set by the host. When true, semantic edits (llm.*
	// or capabilities.*) trigger a clone-first prompt on save so the
	// upstream built-in stays pristine and TUI upgrades can refresh it.
	isBuiltin bool

	cursor int // index into editorFieldOrder
	mode   editorMode

	// Inline textarea, reused for whichever field is being edited.
	// Textarea (not textinput) so paste from the system clipboard works
	// reliably — Bubble Tea's textinput drops characters on multi-byte
	// pastes. The editor intercepts Enter at the page level (see
	// updateInline) so multi-line behavior never surfaces.
	input textarea.Model

	// cloneNameInput captures the new preset name during the clone-first
	// prompt overlay.
	cloneNameInput textinput.Model

	// Display
	width, height int
	lang          string // "en"/"zh"/"wen" — drives tier label rendering
	scrollOffset  int    // first rendered form row; keeps focused field visible in short terminals

	// showJSON controls whether the right-hand JSON preview pane renders.
	// Hidden by default — the form is the source of truth and the JSON
	// dump usually just adds noise. Toggle with Ctrl+D for raw inspection.
	showJSON bool

	// savedCursor remembers where Tab jumped from so Shift+Tab can
	// return there. -1 when Tab hasn't been used (Shift+Tab is then a
	// no-op).
	savedCursor int

	// globalDir is ~/.lingtai-tui — the directory codex-auth.json lives
	// in. Passed by hosts so the editor can write the OAuth token bundle
	// when the user authenticates a codex preset's API-key row. May be
	// empty when no global dir is available (tests); in that case the
	// codex-OAuth branch falls back to inline edit.
	globalDir string

	// API key state. existingKeys is the host's Config.Keys snapshot
	// (env-var-name → value), used to prefill the api_key field when a
	// matching env var is already populated. apiKey is the live edit
	// buffer; apiKeySet flips true only when the user explicitly edits
	// the row (so an untouched masked key remains unchanged on commit,
	// while a pasted replacement is written by the host).
	existingKeys map[string]string
	apiKey       string
	apiKeySet    bool

	// Local Claude CLI login state for the claude-code auth row, filled
	// asynchronously by probeClaudeLoginCmd (Init, or a provider switch into
	// claude-code). A stored setup-token takes precedence, so the probe only
	// runs while no token is in the buffer.
	claudeLogin        claudeCodeAuthInfo
	claudeLoginKnown   bool
	claudeLoginPending bool

	// Status
	saveErr string

	// statusMsg is a transient, non-error footer message (e.g. "Imported
	// Codex CLI credential"). It replaces the browse hint until the next
	// keypress; renderFooter prefers it over saveErr-free hints.
	statusMsg string
}

// NewPresetEditorModel builds an editor against a working copy of `p`.
// The model never mutates `p`; the host receives the modified version
// via PresetEditorCommitMsg. isBuiltin gates the clone-first prompt on
// semantic edits — derived from IsTemplate(p), which uses the preset's
// on-disk Source rather than its name (so a user-saved preset whose
// name happens to match a template is correctly treated as editable).
//
// existingKeys is Config.Keys (env-var-name → value). For user-owned
// presets, the editor uses it to display an already-saved key as masked.
// Templates intentionally start with a blank key buffer so creating a new
// preset never inherits the provider's old shared env slot by accident.
func NewPresetEditorModel(p preset.Preset, lang string, existingKeys map[string]string, globalDir string) PresetEditorModel {
	return NewPresetEditorModelWithBuiltinFlag(p, lang, existingKeys, globalDir, preset.IsTemplate(p))
}

// NewPresetEditorModelWithBuiltinFlag is the explicit-flag variant for
// callers that want to override built-in protection (e.g. tests, or
// a future "fork built-in" flow that has already cloned upstream).
func NewPresetEditorModelWithBuiltinFlag(p preset.Preset, lang string, existingKeys map[string]string, globalDir string, isBuiltin bool) PresetEditorModel {
	// Normalize legacy capability aliases before cloning so the form and its
	// eventual write path expose only the canonical shell key. Conflicts are
	// retained for Validate/commit to reject, but the error is visible now.
	normalizationErr := ""
	if err := p.NormalizeLegacyCapabilities(); err != nil {
		normalizationErr = err.Error()
	}
	// Inline editor uses textarea — paste from the system clipboard
	// works reliably (textinput drops chars on multi-byte pastes).
	// We render only one row; updateInline intercepts Enter and the
	// keymap's InsertNewline binding is cleared, so multi-line
	// semantics never surface. Styles match the rest of the TUI
	// (themedTextareaStyles); the default textarea ships with dark
	// focus colors that clash with the lipgloss palette.
	ta := textarea.New()
	ta.CharLimit = 512
	ta.SetWidth(50)
	ta.SetHeight(1)
	ta.ShowLineNumbers = false
	ta.Prompt = ""
	ta.KeyMap.InsertNewline.SetKeys() // no newlines — single line
	ta.SetStyles(themedTextareaStyles())
	cn := textinput.New()
	cn.CharLimit = 64
	cn.SetWidth(30)
	// For saved/user-owned presets, prefill the api_key buffer if the
	// declared env slot already holds a value; this lets the row render as
	// masked and preserves the key when untouched. For templates, keep the
	// buffer empty: editing a template creates a new preset, and that new
	// preset must not silently inherit an old provider-wide key.
	//
	// Claude is the exception: every Claude preset shares the one
	// CLAUDE_CODE_OAUTH_TOKEN slot (a setup-token belongs to the account),
	// so a Claude template shows — and keeps — the stored token.
	apiKey := ""
	if llm, ok := p.Manifest["llm"].(map[string]interface{}); ok {
		if !isBuiltin || preset.ClaudeCodeFamily(asString(llm["provider"])) {
			if envName := preset.APIKeyEnvName(llm); envName != "" {
				apiKey = existingKeys[envName]
			}
		}
	}
	return PresetEditorModel{
		original:       clonePresetForEditor(p),
		working:        clonePresetForEditor(p),
		isBuiltin:      isBuiltin,
		cursor:         0,
		savedCursor:    -1,
		mode:           emBrowse,
		input:          ta,
		cloneNameInput: cn,
		lang:           lang,
		existingKeys:   existingKeys,
		globalDir:      globalDir,
		apiKey:         apiKey,
		saveErr:        normalizationErr,
	}
}

// Init starts the local Claude login probe for a claude-code preset with no
// stored token; every other preset needs no startup work.
func (m PresetEditorModel) Init() tea.Cmd {
	if m.needsClaudeLoginProbe() {
		return probeClaudeLoginCmd()
	}
	return nil
}

func (m PresetEditorModel) Update(msg tea.Msg) (PresetEditorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case claudeLoginStatusMsg:
		m.claudeLogin = msg.Info
		m.claudeLoginKnown = true
		m.claudeLoginPending = false
		return m, nil

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.ensureFocusedVisible()
		return m, nil

	case tea.MouseWheelMsg:
		if m.mode == emBrowse {
			mouse := msg.Mouse()
			switch mouse.Button {
			case tea.MouseWheelUp:
				m.moveCursor(-3)
			case tea.MouseWheelDown:
				m.moveCursor(3)
			}
			return m, nil
		}

	case tea.KeyMsg:
		switch m.mode {
		case emInline:
			return m.updateInline(msg)
		case emClonePrompt:
			return m.updateClonePrompt(msg)
		case emDirtyPrompt:
			return m.updateDirtyPrompt(msg)
		case emExitPrompt:
			return m.updateExitPrompt(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	// Forward non-KeyMsg events (notably tea.PasteMsg from bracketed-paste
	// mode) to the active text widget. Without this, pasting into the
	// inline editor or the clone-name overlay silently drops the blob —
	// bubbletea v2 delivers paste as a separate msg type, not a KeyMsg.
	switch m.mode {
	case emInline:
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	case emClonePrompt:
		var cmd tea.Cmd
		m.cloneNameInput, cmd = m.cloneNameInput.Update(msg)
		return m, cmd
	}
	return m, nil
}

// ───────────────────────────────────────────────────────────────────────────
// Update — browse mode (cursor over field rows)
// ───────────────────────────────────────────────────────────────────────────

func (m PresetEditorModel) updateBrowse(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	// Transient footer message: visible for the frame that set it, cleared by
	// the next keypress (the import handler below re-sets it after this line).
	m.statusMsg = ""
	switch msg.String() {
	case "esc":
		// Clean editor (no edits made) → close immediately. Confirming
		// an exit when there's nothing to lose is the source of the
		// "I just glanced at this and Esc trapped me" complaint.
		// Dirty editor → show the three-way prompt so the user picks
		// save / discard / cancel intentionally.
		if !m.hasSemanticEdits() && !m.apiKeySet {
			return m, func() tea.Msg { return PresetEditorCancelMsg{} }
		}
		m.mode = emExitPrompt
		return m, nil
	case "up", "k":
		m.moveCursor(-1)
		return m, nil
	case "down", "j":
		m.moveCursor(1)
		return m, nil
	case "pgup":
		m.moveCursor(-m.visibleFormRows())
		return m, nil
	case "pgdown":
		m.moveCursor(m.visibleFormRows())
		return m, nil
	case "home":
		m.cursor = 0
		m.ensureFocusedVisible()
		return m, nil
	case "end":
		m.cursor = saveFieldIndex
		m.ensureFocusedVisible()
		return m, nil
	case "left", "h":
		// Cycle backwards on enum fields.
		m.cycleFocused(-1)
		return m, m.claudeLoginProbeCmd()
	case "right", "l":
		m.cycleFocused(+1)
		return m, m.claudeLoginProbeCmd()
	case "tab":
		// Jump straight to the Save button. Press Enter there to
		// commit (or Tab again to cycle back to the previous field).
		// Shift+Tab returns to the previously-focused field.
		m.savedCursor = m.cursor
		m.cursor = saveFieldIndex
		m.ensureFocusedVisible()
		return m, nil
	case "shift+tab":
		// Restore the cursor to wherever Tab jumped from. If we
		// haven't tabbed-to-save yet, no-op.
		if m.cursor == saveFieldIndex && m.savedCursor >= 0 && m.savedCursor < len(editorFieldOrder) {
			m.cursor = m.savedCursor
			m.ensureFocusedVisible()
		}
		return m, nil
	case "enter":
		updated, cmd := m.openInline()
		if probe := updated.claudeLoginProbeCmd(); probe != nil {
			return updated, tea.Batch(cmd, probe)
		}
		return updated, cmd
	case "ctrl+s":
		return m.commit()
	case "ctrl+d":
		// Toggle the JSON preview pane. Raw inspection for power users
		// who want to see the on-disk shape; hidden by default to keep
		// the form uncluttered.
		m.showJSON = !m.showJSON
		return m, nil
	case "i":
		// One-click import of the Codex CLI's own credential (`codex
		// login` writes ~/.codex/auth.json) into the TUI store, bound to
		// this preset's API-key row. Only meaningful on the codex
		// API-key row while the bound account is invalid (the footer
		// hint codex.import_cli_hint advertises it only when a valid CLI
		// file exists). Errors — no CLI credential, already imported,
		// target exists — surface in the footer via saveErr.
		f := editorFieldOrder[m.cursor]
		if f == feAPIKey && preset.ClassifyCredentialFamily(asString(m.llmMap()["provider"])) == preset.CredentialFamilyCodexSingle && m.globalDir != "" {
			if _, valid := m.codexBoundAccountLabel(); !valid {
				ref, label, err := importCodexCLIAuth(m.globalDir)
				if err != nil {
					m.saveErr = err.Error()
				} else {
					m.setCodexAuthRef(ref)
					m.statusMsg = fmt.Sprintf(i18n.T("codex.import_cli_done"), label)
				}
			}
		}
		return m, nil
	}
	return m, nil
}

// updateInline routes keys to the active textinput. Enter commits the
// edit into the working copy; Esc abandons the edit.
func (m PresetEditorModel) updateInline(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = emBrowse
		m.input.Blur()
		return m, nil
	case "enter":
		m.applyInline(m.input.Value())
		m.mode = emBrowse
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m PresetEditorModel) updateDirtyPrompt(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	switch msg.String() {
	case "y", "Y":
		return m, func() tea.Msg { return PresetEditorCancelMsg{} }
	default:
		// Anything else returns to browse without discarding.
		m.mode = emBrowse
		return m, nil
	}
}

// updateExitPrompt is the three-way "save / discard / cancel" overlay
// triggered by Esc when the editor has unsaved changes. Enter (the
// visible default) and `s` save and exit. `d` discards changes and
// exits. Esc and `c` cancel back to browse.
//
// The mapping deliberately makes Esc safe: a user who hit Esc by
// mistake and double-presses won't accidentally discard their edits.
// The destructive choice (discard) requires the explicit `d` key.
func (m PresetEditorModel) updateExitPrompt(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	switch msg.String() {
	case "enter", "s", "S":
		m.mode = emBrowse
		updated, cmd := m.commit()
		return updated, cmd
	case "d", "D":
		return m, func() tea.Msg { return PresetEditorCancelMsg{} }
	default:
		// esc/c/n/anything else → return to browse, no exit.
		m.mode = emBrowse
		return m, nil
	}
}

// ───────────────────────────────────────────────────────────────────────────
// Field-level mutation
// ───────────────────────────────────────────────────────────────────────────

func (m *PresetEditorModel) openInline() (PresetEditorModel, tea.Cmd) {
	f := editorFieldOrder[m.cursor]
	switch f {
	case feName, feSummary, feGains, feLoses, feBaseURL:
		// base_url is plain free text for every family: empty means the
		// family's official endpoint (openai/anthropic) or the provider's
		// own route (codex/claude-code).
		m.input.SetValue(m.fieldString(f))
		m.input.CursorEnd()
		m.input.Focus()
		m.mode = emInline
	case feAPIKey:
		// Codex binds an OAuth account, not a typed key: keep its row
		// visibly read-only so it cannot suggest that typing here changes
		// the bound account. Every other family — including claude-code,
		// whose row takes a `claude setup-token` token — pastes a secret.
		if preset.ClassifyCredentialFamily(asString(m.llmMap()["provider"])) == preset.CredentialFamilyCodexSingle {
			m.saveErr = i18n.T("preset_editor.api_key_codex_readonly")
			return *m, nil
		}
		// Edit the live key buffer, not the env-var-name. We start
		// blank rather than prefilling the existing value so the user
		// can paste a new key without first deleting the masked
		// placeholder. apiKeySet flips on commit if they typed anything.
		m.input.SetValue("")
		m.input.CursorEnd()
		m.input.Focus()
		m.mode = emInline
	case feModel:
		provider := asString(m.llmMap()["provider"])
		if models := modelOptions(provider); len(models) > 0 {
			m.cycleFocused(+1)
		} else {
			m.input.SetValue(m.fieldString(f))
			m.input.CursorEnd()
			m.input.Focus()
			m.mode = emInline
		}
	case feServiceTier:
		if m.hasServiceTier() {
			m.cycleFocused(+1)
		}
	case feThinking:
		if m.hasThinking() {
			m.cycleFocused(+1)
		}
	case feTier:
		// Tier is an enum — Enter cycles like ←/→. No picker overlay.
		m.cycleFocused(+1)
	case feProvider, feWireAPI, feResponsesTransport:
		// Enums — Enter cycles forward (same as Right). Lets the user
		// stay on the keyboard's "advance" key.
		m.cycleFocused(+1)
	case feSave:
		updated, cmd := m.commit()
		return updated, cmd
	}
	return *m, nil
}

// applyInline writes the textinput's current value into the working
// copy, with light coercion for numeric fields.
func (m *PresetEditorModel) applyInline(val string) {
	val = strings.TrimSpace(val)
	f := editorFieldOrder[m.cursor]
	llm := m.llmMap()
	switch f {
	case feName:
		// Empty name is silently ignored — name is required to save and
		// the validator will catch a bad write later. Spaces collapse to
		// underscores so the on-disk filename is shell-safe.
		if val != "" {
			m.working.Name = strings.ReplaceAll(val, " ", "_")
		}
	case feSummary:
		m.working.Description.Summary = val
	case feGains:
		m.setExtra("gains", val)
	case feLoses:
		m.setExtra("loses", val)
	case feModel:
		llm["model"] = val
	case feBaseURL:
		if val == "" {
			llm["base_url"] = nil
		} else {
			llm["base_url"] = val
		}
	case feAPIKey:
		// Store the raw key in the editor's buffer; the manifest
		// itself only holds api_key_env (the slot name), assigned at
		// commit time by the host's stampAutoEnvVar helper. Opening the
		// blank replacement editor and pressing Enter without typing is
		// a no-op, not a clear; key clearing needs an explicit future UI.
		if val == "" {
			return
		}
		m.apiKey = val
		m.apiKeySet = true
	}
}

func (m PresetEditorModel) isCodexProvider() bool {
	return preset.ClassifyCredentialFamily(asString(m.llmMap()["provider"])) == preset.CredentialFamilyCodexSingle
}

func isCodexThinkingProvider(provider string) bool {
	return preset.ClassifyCredentialFamily(provider) == preset.CredentialFamilyCodexSingle
}

func (m PresetEditorModel) hasCodexThinking() bool {
	return isCodexThinkingProvider(asString(m.llmMap()["provider"]))
}

// isThinkingLevel reports whether v is one of the kernel's canonical
// THINKING_LEVELS payload values. "default" is deliberately absent: it is a
// UI-only pseudo-option meaning "omit manifest.llm.thinking".
func isThinkingLevel(v string) bool {
	switch v {
	case "none", "minimal", "low", "medium", "high", "xhigh":
		return true
	}
	return false
}

// llmHasLevelThinking reports whether an llm block takes the canonical
// THINKING_LEVELS ladder: the openai family (the level rides through as
// reasoning effort on either wire — wire_api does not gate it) and the
// anthropic family (the adapter turns the level into an extended-thinking
// budget). Codex keeps its own ladder and xhigh default; claude-code has a
// CLI-specific effort vocabulary the editor does not expose.
func llmHasLevelThinking(llm map[string]interface{}) bool {
	if llm == nil {
		return false
	}
	switch asString(llm["provider"]) {
	case preset.ProviderOpenAI, preset.ProviderAnthropic:
		return true
	}
	return false
}

func (m PresetEditorModel) hasLevelThinking() bool {
	return llmHasLevelThinking(m.llmMap())
}

func (m PresetEditorModel) hasThinking() bool {
	return m.hasCodexThinking() || m.hasLevelThinking()
}

// llmHasServiceTier reports whether an llm block accepts service_tier
// (normal/fast, where fast is sent as priority): the openai family and the
// Codex family. Other providers never surface or keep the field.
func llmHasServiceTier(llm map[string]interface{}) bool {
	provider := asString(llm["provider"])
	return provider == preset.ProviderOpenAI ||
		preset.ClassifyCredentialFamily(provider) == preset.CredentialFamilyCodexSingle
}

func (m PresetEditorModel) hasServiceTier() bool {
	return llmHasServiceTier(m.llmMap())
}

// isOpenAIFamily reports whether the working preset is the openai provider
// family — the only scope where the wire-format selector (wire_api) applies.
func (m PresetEditorModel) isOpenAIFamily() bool {
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	return asString(llm["provider"]) == preset.ProviderOpenAI
}

// isOpenAIResponses is the openai family on the Responses wire. It gates the
// transport selector (HTTP default, or WebSocket). The reasoning-effort
// selector is NOT gated on it — see llmHasLevelThinking.
func (m PresetEditorModel) isOpenAIResponses() bool {
	return m.isOpenAIFamily() && m.fieldString(feWireAPI) == preset.WireAPIResponses
}

// codexAccountRefs returns the selectable codex_auth_path values for the
// account picker on the feAPIKey row: "" (legacy/default account) first, then
// each per-account file's home-shortened ref. Order is stable so ←/→ cycling
// is predictable.
func (m PresetEditorModel) codexAccountRefs() []string {
	refs := []string{""} // "" == legacy/default account
	if m.globalDir == "" {
		return refs
	}
	for _, a := range listCodexAccounts(m.globalDir) {
		if a.Legacy {
			continue // the legacy file is already represented by ""
		}
		refs = append(refs, a.Ref)
	}
	return refs
}

// codexAuthRef returns the preset's bound manifest.llm.codex_auth_path ("" when
// unset / legacy fallback).
func (m PresetEditorModel) codexAuthRef() string {
	return asString(m.llmMap()["codex_auth_path"])
}

// setCodexAuthRef writes (or clears) manifest.llm.codex_auth_path. An empty ref
// removes the field so the preset falls back to the legacy account with no
// stray key in the JSON.
func (m *PresetEditorModel) setCodexAuthRef(ref string) {
	llm := m.llmMap()
	if preset.ClassifyCredentialFamily(asString(llm["provider"])) != preset.CredentialFamilyCodexSingle || strings.TrimSpace(ref) == "" {
		delete(llm, "codex_auth_path")
		return
	}
	llm["codex_auth_path"] = ref
}

// codexBoundAccountLabel returns a non-secret label for the account the preset
// is currently bound to, plus whether that account's token file is valid.
func (m PresetEditorModel) codexBoundAccountLabel() (string, bool) {
	ref := m.codexAuthRef()
	path := resolveCodexAuthPath(m.globalDir, ref)
	valid := codexAuthPathValid(path)
	if ref == "" {
		// Legacy/default: prefer the stored email for the label.
		if tok, ok := readCodexTokenFile(path); ok && tok.Email != "" {
			return tok.Email, valid
		}
		return i18n.T("codex.account_default"), valid
	}
	if tok, ok := readCodexTokenFile(path); ok && tok.Email != "" {
		return tok.Email, valid
	}
	return strings.TrimSuffix(filepath.Base(path), filepath.Ext(path)), valid
}

// codexCLIImportAvailable reports whether the codex API-key row can offer the
// one-click CLI import: the preset uses a codex single credential, globalDir
// is set, the currently-bound account is invalid, and a valid Codex CLI
// credential file (~/.codex/auth.json or $CODEX_HOME/auth.json) exists to
// import. Drives the footer hint; the 'i' handler additionally requires the
// row to be focused.
func (m PresetEditorModel) codexCLIImportAvailable() bool {
	if m.globalDir == "" {
		return false
	}
	if preset.ClassifyCredentialFamily(asString(m.llmMap()["provider"])) != preset.CredentialFamilyCodexSingle {
		return false
	}
	if _, valid := m.codexBoundAccountLabel(); valid {
		return false
	}
	_, ok := readCodexCLIAuthFile(codexCLIAuthPath())
	return ok
}
func (m PresetEditorModel) serviceTier() string {
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	if asString(llm["service_tier"]) == "fast" {
		return "fast"
	}
	return "normal"
}

func (m *PresetEditorModel) setServiceTier(tier string) {
	llm := m.llmMap()
	if tier != "fast" {
		delete(llm, "service_tier")
		return
	}
	llm["service_tier"] = "fast"
}

// codexDefaultThinking is the reasoning effort LingTai applies to Codex
// when a preset omits (or carries an invalid) llm.thinking. LingTai is the
// primary brain, so it runs Codex at maximum effort by default.
const codexDefaultThinking = "xhigh"

func (m PresetEditorModel) codexThinking() string {
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	switch asString(llm["thinking"]) {
	case "low", "medium", "high", "xhigh":
		return asString(llm["thinking"])
	default:
		return codexDefaultThinking
	}
}

func (m *PresetEditorModel) setCodexThinking(effort string) {
	llm := m.llmMap()
	if !isCodexThinkingProvider(asString(llm["provider"])) {
		delete(llm, "thinking")
		return
	}
	switch effort {
	case "low", "medium", "high", "xhigh":
		llm["thinking"] = effort
	default:
		// Absent/invalid resolves to the Codex default, persisted
		// explicitly so the running session actually receives xhigh.
		llm["thinking"] = codexDefaultThinking
	}
}

func (m PresetEditorModel) thinkingValue() string {
	if m.hasCodexThinking() {
		return m.codexThinking()
	}
	if m.hasLevelThinking() {
		if v := asString(m.llmMap()["thinking"]); isThinkingLevel(v) {
			return v
		}
		// Absent or invalid: the kernel's own default applies, which the
		// editor shows (and stores) as omission.
		return "default"
	}
	return ""
}

func (m PresetEditorModel) thinkingOptions() []string {
	if m.hasCodexThinking() {
		return codexThinkingOptions
	}
	if m.hasLevelThinking() {
		return levelThinkingOptions
	}
	return nil
}

func (m *PresetEditorModel) setThinking(effort string) {
	if m.hasCodexThinking() {
		m.setCodexThinking(effort)
		return
	}
	llm := m.llmMap()
	if !m.hasLevelThinking() || !isThinkingLevel(effort) {
		// Out of scope, "default", or an unknown value — all of which mean
		// "carry no thinking field".
		delete(llm, "thinking")
		return
	}
	llm["thinking"] = effort
}

// normalizeServiceTier keeps service_tier only where it applies (openai and
// codex) and only as "fast"; "normal" is the omission sentinel, and any other
// value — or any value on another provider — is dropped.
func normalizeServiceTier(manifest map[string]interface{}) {
	llm, _ := manifest["llm"].(map[string]interface{})
	if llm == nil {
		return
	}
	if llmHasServiceTier(llm) && asString(llm["service_tier"]) == "fast" {
		return
	}
	delete(llm, "service_tier")
}

func normalizeThinking(manifest map[string]interface{}) {
	llm, _ := manifest["llm"].(map[string]interface{})
	if llm == nil {
		return
	}
	if isCodexThinkingProvider(asString(llm["provider"])) {
		switch asString(llm["thinking"]) {
		case "low", "medium", "high", "xhigh":
			return
		default:
			// Codex with absent/invalid thinking is normalized to the default
			// so committed/cloned/generated presets explicitly carry it and the
			// running session receives xhigh rather than a UI-only fallback.
			llm["thinking"] = codexDefaultThinking
			return
		}
	}
	if llmHasLevelThinking(llm) {
		if isThinkingLevel(asString(llm["thinking"])) {
			return
		}
		// "default" is represented by omission for every non-Codex provider:
		// the Kernel's own main-session default then applies. No default is
		// forced here, so an absent field stays absent.
		delete(llm, "thinking")
		return
	}
	delete(llm, "thinking")
}

// normalizeWireAPI keeps llm.wire_api only on the openai family, where it is
// always written explicitly: "responses" stays, and anything else (absent, a
// legacy "auto", or an unknown value) becomes the chat_completions default.
// Every other provider drops the field.
func normalizeWireAPI(manifest map[string]interface{}) {
	llm, _ := manifest["llm"].(map[string]interface{})
	if llm == nil {
		return
	}
	if asString(llm["provider"]) != preset.ProviderOpenAI {
		delete(llm, "wire_api")
		return
	}
	if asString(llm["wire_api"]) != preset.WireAPIResponses {
		llm["wire_api"] = preset.WireAPIChatCompletions
	}
}

// normalizeResponsesTransport keeps HTTP as the omission/default and removes
// stale transport values outside the openai family's Responses wire.
func normalizeResponsesTransport(manifest map[string]interface{}) {
	llm, _ := manifest["llm"].(map[string]interface{})
	if llm == nil {
		return
	}
	if asString(llm["provider"]) == preset.ProviderOpenAI &&
		asString(llm["wire_api"]) == preset.WireAPIResponses {
		if asString(llm["responses_transport"]) != "websocket" {
			delete(llm, "responses_transport")
		}
		return
	}
	delete(llm, "responses_transport")
}

func normalizeLLMForCommit(manifest map[string]interface{}) {
	if llm, ok := manifest["llm"].(map[string]interface{}); ok {
		preset.StripRetiredLLMFields(llm)
	}
	normalizeServiceTier(manifest)
	normalizeThinking(manifest)
	normalizeWireAPI(manifest)
	normalizeResponsesTransport(manifest)
	normalizeClaudeCode(manifest)
}

// normalizeClaudeCode keeps a claude-code llm block to what the family uses:
// provider plus its setup-token slot. model and base_url are dropped (Claude
// Code runs its own default model on its own route); thinking, service_tier,
// wire_api, and codex_auth_path are already removed by the other
// normalizers.
func normalizeClaudeCode(manifest map[string]interface{}) {
	llm, _ := manifest["llm"].(map[string]interface{})
	if llm == nil || !preset.ClaudeCodeFamily(asString(llm["provider"])) {
		return
	}
	delete(llm, "model")
	delete(llm, "base_url")
	delete(llm, "codex_auth_path")
}

// setExtra writes into Description.Extra, allocating the map on first
// use. Empty string deletes the key.
func (m *PresetEditorModel) setExtra(key, val string) {
	if val == "" {
		delete(m.working.Description.Extra, key)
		if len(m.working.Description.Extra) == 0 {
			m.working.Description.Extra = nil
		}
		return
	}
	if m.working.Description.Extra == nil {
		m.working.Description.Extra = map[string]interface{}{}
	}
	m.working.Description.Extra[key] = val
}

// cycleFocused rotates enum fields by `dir` (+1 or -1).
func (m *PresetEditorModel) cycleFocused(dir int) {
	f := editorFieldOrder[m.cursor]
	switch f {
	case feProvider:
		oldProvider := m.fieldString(f)
		newProvider := cycleString(editorProviders, oldProvider, dir)
		if !containsString(editorProviders, oldProvider) {
			// A legacy saved provider (e.g. "custom") is not in the cycle:
			// enter it at its edge — first family going right, last going
			// left — so → converts it to openai.
			newProvider = editorProviders[0]
			if dir < 0 {
				newProvider = editorProviders[len(editorProviders)-1]
			}
		}
		if newProvider != oldProvider {
			m.switchProvider(oldProvider, newProvider)
		}
	case feModel:
		provider := asString(m.llmMap()["provider"])
		if models := modelOptions(provider); len(models) > 0 {
			next := cycleString(models, m.fieldString(f), dir)
			m.llmMap()["model"] = next
		}
	case feWireAPI:
		if !m.isOpenAIFamily() {
			return
		}
		m.llmMap()["wire_api"] = cycleString(wireAPIOptions, m.fieldString(f), dir)
		normalizeResponsesTransport(m.working.Manifest)
	case feResponsesTransport:
		next := cycleString(responsesTransportOptions, m.fieldString(f), dir)
		if next == "websocket" {
			m.llmMap()["responses_transport"] = next
		} else {
			delete(m.llmMap(), "responses_transport")
		}
	case feServiceTier:
		if m.hasServiceTier() {
			m.setServiceTier(cycleString(serviceTierOptions, m.serviceTier(), dir))
		}
	case feThinking:
		if m.hasThinking() {
			m.setThinking(cycleString(m.thinkingOptions(), m.thinkingValue(), dir))
		}
	case feAPIKey:
		// Codex account selector: bind the preset to the next/previous
		// stored account by cycling manifest.llm.codex_auth_path. "" is the
		// legacy/default account.
		if m.isCodexProvider() {
			refs := m.codexAccountRefs()
			if len(refs) > 1 {
				m.setCodexAuthRef(cycleString(refs, m.codexAuthRef(), dir))
			}
		}
	case feTier:
		// Cycle ""→1→2→3→4→5→"" with → and reverse with ←. tierValues
		// is ordered best-first ([5..1]) for the library's picker, so
		// reverse it here for the natural ascending sweep.
		opts := []string{"", "1", "2", "3", "4", "5"}
		m.working.Description.Tier = cycleString(opts, m.working.Description.Tier, dir)
	}
}

// switchProvider moves the working preset from one provider to another
// without leaking route-specific state across. The provider row is only
// offered for a legacy saved provider (see fieldVisible), so this is the
// path that converts such a preset into one of the four families:
//
//   - codex adopts its template's /codex route and clears api_key_env
//     (OAuth, no key slot).
//   - claude-code drops base_url, model, and thinking (Claude Code runs its
//     own default model and effort) and uses the shared
//     CLAUDE_CODE_OAUTH_TOKEN slot.
//   - openai/anthropic coming from codex or claude-code start from the
//     official endpoint (base_url nil) and the family's default key slot.
//   - openai/anthropic coming from each other (or from a legacy saved
//     provider being converted) keep the user's base_url and credential slot;
//     only the previous family's default slot name is swapped for the new
//     family's default, so OPENAI_API_KEY never follows a preset to anthropic.
//   - a curated model the new provider cannot serve is replaced by the first
//     catalog entry, or cleared for a free-text family (Save then requires an
//     explicit model); a user-typed model is kept.
//   - thinking restarts from the new family's default, and fields only one
//     family understands (wire_api, responses_transport, service_tier,
//     codex_auth_path) are normalized away.
func (m *PresetEditorModel) switchProvider(oldProvider, newProvider string) {
	llm := m.llmMap()
	llm["provider"] = newProvider

	oldRouteOwned := preset.ClassifyCredentialFamily(oldProvider) != preset.CredentialFamilyOther
	switch preset.ClassifyCredentialFamily(newProvider) {
	case preset.CredentialFamilyCodexSingle:
		llm["base_url"] = familyTemplateLLM(newProvider)["base_url"]
		llm["api_key_env"] = ""
	case preset.CredentialFamilyClaude:
		delete(llm, "base_url")
		delete(llm, "model")
		llm["api_key_env"] = preset.DefaultAPIKeyEnv(newProvider)
	default:
		env := asString(llm["api_key_env"])
		if oldRouteOwned {
			llm["base_url"] = nil
			env = ""
		}
		if env == "" || env == preset.DefaultAPIKeyEnv(oldProvider) {
			env = preset.DefaultAPIKeyEnv(newProvider)
		}
		llm["api_key_env"] = env
	}
	// The key buffer follows the new slot unless the user typed a key in
	// this session: a Claude setup-token must not display as an OpenAI key
	// (or the reverse).
	if !m.apiKeySet && (preset.ClaudeCodeFamily(oldProvider) || preset.ClaudeCodeFamily(newProvider)) {
		m.apiKey = m.existingKeys[preset.APIKeyEnvName(llm)]
	}

	currentModel := asString(llm["model"])
	if models := modelOptions(newProvider); len(models) > 0 {
		keep := false
		for _, mdl := range models {
			if mdl == currentModel {
				keep = true
				break
			}
		}
		if !keep {
			llm["model"] = models[0]
		}
	} else if isCuratedModel(currentModel) {
		llm["model"] = ""
	}

	delete(llm, "thinking")
	if preset.ClassifyCredentialFamily(newProvider) != preset.CredentialFamilyCodexSingle {
		delete(llm, "codex_auth_path")
	}
	normalizeServiceTier(m.working.Manifest)
	normalizeThinking(m.working.Manifest)
	normalizeWireAPI(m.working.Manifest)
	normalizeResponsesTransport(m.working.Manifest)
}

// familyTemplateLLM returns the manifest.llm block of the built-in template
// for provider, or an empty map when no template declares that provider.
func familyTemplateLLM(provider string) map[string]interface{} {
	for _, p := range preset.BuiltinPresets() {
		llm, _ := p.Manifest["llm"].(map[string]interface{})
		if asString(llm["provider"]) == provider {
			return llm
		}
	}
	return map[string]interface{}{}
}

func (m PresetEditorModel) commit() (PresetEditorModel, tea.Cmd) {
	if errs := m.working.Validate(); len(errs) > 0 {
		m.saveErr = localizedPresetValidationError(errs[0])
		return m, nil
	}
	m.saveErr = ""
	// Templates (built-ins) are starting points: the user picks one,
	// edits it, and saves. The save always materializes a *new* file
	// under an auto-generated name like `openai-1` so the template stays
	// pristine and the user gets a saved preset they own.
	//
	// If the user explicitly renamed the preset in the editor (Name
	// differs from the template's name), respect that name. Otherwise
	// gap-fill the next "<template>-N" slot.
	committed := clonePresetForEditor(m.working)
	// Kernel core capabilities (knowledge, skills, shell, avatar, daemon,
	// mcp, file group) are floor-injected by apply_core_defaults at
	// runtime, so we deliberately do NOT stamp them into the saved
	// manifest. That keeps preset JSON minimal and avoids implying these
	// are ordinary opt-ins.
	if m.isBuiltin && (m.hasSemanticEdits() || m.apiKeySet) {
		if committed.Name == m.original.Name {
			existing, _ := preset.List()
			names := make([]string, 0, len(existing))
			for _, p := range existing {
				names = append(names, p.Name)
			}
			if auto := preset.AutoSavedName(m.original.Name, names); auto != "" {
				committed.Name = auto
			}
		}
		// Clear the inherited api_key_env so the host's stampAutoEnvVar
		// allocates a fresh slot (PROVIDER_N_API_KEY) under the new name.
		// Without this, the user's pasted key would overwrite the
		// template's shared slot (e.g. OPENAI_API_KEY), polluting any
		// other preset that references it.
		if llm, ok := committed.Manifest["llm"].(map[string]interface{}); ok {
			delete(llm, "api_key_env")
		}
	}
	normalizeLLMForCommit(committed.Manifest)
	return m, m.commitCmd(committed)
}

func localizedPresetValidationError(err error) string {
	if err == nil {
		return ""
	}
	switch err.Error() {
	case "description.summary must be non-empty":
		return i18n.T("preset_editor.validation.description_summary_required")
	case "manifest.llm must be an object":
		return i18n.T("preset_editor.validation.llm_object_required")
	case "manifest.llm.provider must be non-empty":
		return i18n.T("preset_editor.validation.llm_provider_required")
	case "manifest.llm.model must be non-empty":
		return i18n.T("preset_editor.validation.llm_model_required")
	default:
		return err.Error()
	}
}

// commitCmd is the single PresetEditorCommitMsg constructor. Every host writes
// editor results through preset.Save, whose destination is presets/saved/, so
// the runtime-only Source on the committed object must identify that exact
// destination even when its name matches a built-in template.
func (m PresetEditorModel) commitCmd(committed preset.Preset) tea.Cmd {
	committed.Source = preset.SourceSaved
	return func() tea.Msg {
		return PresetEditorCommitMsg{Preset: committed, APIKey: m.apiKey, APIKeySet: m.apiKeySet}
	}
}

// hasSemanticEdits reports whether the user changed any field whose
// in-place edit on a built-in would silently mask a TUI upgrade. The
// definition of "semantic" is: anything except description.summary,
// description.tier, and description.Extra (gains/loses/etc.).
func (m PresetEditorModel) hasSemanticEdits() bool {
	if m.working.Name != m.original.Name {
		return true
	}
	wm, _ := json.Marshal(m.working.Manifest)
	om, _ := json.Marshal(m.original.Manifest)
	return string(wm) != string(om)
}

// updateClonePrompt handles the new-name textinput overlay shown to
// gate semantic edits on built-in presets.
func (m PresetEditorModel) updateClonePrompt(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = emBrowse
		m.cloneNameInput.Blur()
		return m, nil
	case "ctrl+e":
		// Expert override: skip clone, save in place under the original
		// built-in name. The user explicitly accepts that future TUI
		// upgrades won't refresh this preset.
		m.mode = emBrowse
		m.cloneNameInput.Blur()
		committed := clonePresetForEditor(m.working)
		normalizeLLMForCommit(committed.Manifest)
		return m, m.commitCmd(committed)
	case "enter":
		newName := strings.TrimSpace(m.cloneNameInput.Value())
		if newName == "" {
			m.saveErr = "name cannot be empty"
			return m, nil
		}
		// The name becomes a filename stem under presets/saved/ via
		// preset.Save; reject path forms up front so the user sees a
		// clear error instead of an escape attempt (issue #849).
		if err := preset.ValidateSafeName(newName); err != nil {
			m.saveErr = "invalid name: " + err.Error()
			return m, nil
		}
		if newName == m.original.Name {
			m.saveErr = "pick a different name (or press Ctrl+E to overwrite the built-in)"
			return m, nil
		}
		m.working.Name = newName
		m.mode = emBrowse
		m.cloneNameInput.Blur()
		committed := clonePresetForEditor(m.working)
		normalizeLLMForCommit(committed.Manifest)
		return m, m.commitCmd(committed)
	}
	var cmd tea.Cmd
	m.cloneNameInput, cmd = m.cloneNameInput.Update(msg)
	return m, cmd
}

// ───────────────────────────────────────────────────────────────────────────
// Read-side helpers
// ───────────────────────────────────────────────────────────────────────────

func (m PresetEditorModel) llmMap() map[string]interface{} {
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	if llm == nil {
		llm = map[string]interface{}{}
		m.working.Manifest["llm"] = llm
	}
	return llm
}

// originalLLM is the llm block the editor was opened with (never nil).
func (m PresetEditorModel) originalLLM() map[string]interface{} {
	llm, _ := m.original.Manifest["llm"].(map[string]interface{})
	if llm == nil {
		return map[string]interface{}{}
	}
	return llm
}

// isFamilyProvider reports whether provider is one of the four canonical
// families (as opposed to a legacy saved provider awaiting conversion).
func isFamilyProvider(provider string) bool {
	return containsString(editorProviders, provider)
}

// isClaudeCode reports whether the working preset is the claude-code family,
// whose LLM section is only the auth row.
func (m PresetEditorModel) isClaudeCode() bool {
	return preset.ClaudeCodeFamily(asString(m.llmMap()["provider"]))
}

// claudeTokenInBuffer reports whether a setup-token is stored (prefilled) or
// was pasted in this session.
func (m PresetEditorModel) claudeTokenInBuffer() bool {
	return strings.TrimSpace(m.apiKey) != ""
}

// needsClaudeLoginProbe: a claude-code preset with no token and no known (or
// pending) local-login result.
func (m PresetEditorModel) needsClaudeLoginProbe() bool {
	return m.isClaudeCode() && !m.claudeTokenInBuffer() && !m.claudeLoginKnown && !m.claudeLoginPending
}

// claudeLoginProbeCmd starts the local-login probe once when the working
// preset needs it (e.g. right after a provider switch into claude-code).
func (m *PresetEditorModel) claudeLoginProbeCmd() tea.Cmd {
	if !m.needsClaudeLoginProbe() {
		return nil
	}
	m.claudeLoginPending = true
	return probeClaudeLoginCmd()
}

// claudeAuthLabel renders the claude-code auth row: a stored/pasted
// setup-token wins, else the local Claude login, else "not configured".
func (m PresetEditorModel) claudeAuthLabel() string {
	env := preset.APIKeyEnvName(m.llmMap())
	if !m.claudeTokenInBuffer() && !m.claudeLoginKnown {
		return i18n.T("claude.auth_checking")
	}
	login := m.claudeLogin
	return resolveClaudeAuth(env, m.claudeTokenInBuffer(), func() claudeCodeAuthInfo { return login }).Label()
}

// fieldString returns the current display value for the given field.
func (m PresetEditorModel) fieldString(f editorField) string {
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	switch f {
	case feName:
		return m.working.Name
	case feSummary:
		return m.working.Description.Summary
	case feTier:
		return m.working.Description.Tier
	case feGains:
		v, _ := m.working.Description.Extra["gains"].(string)
		return v
	case feLoses:
		v, _ := m.working.Description.Extra["loses"].(string)
		return v
	case feProvider:
		s, _ := llm["provider"].(string)
		return s
	case feModel:
		s, _ := llm["model"].(string)
		return s
	case feServiceTier:
		return m.serviceTier()
	case feThinking:
		return m.thinkingValue()
	case feWireAPI:
		// Absent, legacy "auto", or unknown values all mean the Chat
		// Completions default (normalizeWireAPI writes it explicitly).
		if s, _ := llm["wire_api"].(string); s == preset.WireAPIResponses {
			return s
		}
		return preset.WireAPIChatCompletions
	case feResponsesTransport:
		if s, _ := llm["responses_transport"].(string); s == "websocket" {
			return s
		}
		return "http"
	case feBaseURL:
		s, _ := llm["base_url"].(string)
		return s
	case feAPIKey:
		// Codex uses an OAuth credential, not an API key. Show the bound
		// account (manifest.llm.codex_auth_path → resolved token file) and
		// its validity. When more than one account exists, ←/→ cycles the
		// binding (see isCyclable/cycleField). No secret is shown.
		family := preset.ClassifyCredentialFamily(asString(llm["provider"]))
		if family == preset.CredentialFamilyCodexSingle {
			if m.globalDir != "" {
				label, valid := m.codexBoundAccountLabel()
				if valid {
					return "✓ " + label
				}
				return "✗ " + label + " — " + i18n.T("codex.oauth_not_logged_in")
			}
			return i18n.T("codex.oauth_not_logged_in")
		}
		if family == preset.CredentialFamilyClaude {
			return m.claudeAuthLabel()
		}
		// Other providers display the existing key masked. The env-var name
		// is an internal detail; the user only needs to see whether a key is
		// set.
		return maskAPIKey(m.apiKey)
	}
	return ""
}

func (m PresetEditorModel) isDirty() bool {
	a, _ := json.Marshal(m.working)
	b, _ := json.Marshal(m.original)
	return string(a) != string(b)
}

// ───────────────────────────────────────────────────────────────────────────
// View
// ───────────────────────────────────────────────────────────────────────────

func (m PresetEditorModel) View() string {
	if m.width == 0 || m.height == 0 {
		return ""
	}

	bodyHeight := m.height - 4
	if bodyHeight < 3 {
		bodyHeight = 3
	}

	// Title bar.
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	title := titleStyle.Render(i18n.T("preset_editor.title") + ": " + m.working.Name)
	if label := tierLabel(m.working.Description.Tier, m.lang); label != "" {
		title += "  " + tierChipStyle(m.working.Description.Tier).Render(label)
	}

	// JSON preview is opt-in via Ctrl+D. When off (default), the form
	// claims the full width — clean & focused. When on AND wide enough,
	// split horizontally. Narrow terminals always show form-only.
	var body string
	if m.showJSON && m.width >= 100 {
		formW := m.width / 2
		previewW := m.width - formW - 1
		body = lipgloss.JoinHorizontal(lipgloss.Top,
			m.renderForm(formW, bodyHeight),
			" ",
			m.renderPreview(previewW, bodyHeight),
		)
	} else {
		body = m.renderForm(m.width, bodyHeight)
	}

	footer := m.renderFooter()
	full := lipgloss.JoinVertical(lipgloss.Left, title, body, footer)

	switch m.mode {
	case emClonePrompt:
		full = m.renderCloneOverlay(full)
	case emDirtyPrompt:
		full = m.renderDirtyOverlay(full)
	case emExitPrompt:
		full = m.renderExitOverlay(full)
	}
	return full
}

type presetEditorRow struct {
	text     string
	field    editorField
	hasField bool
}

func (m PresetEditorModel) renderForm(width, height int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("245")).
		Width(width).
		Height(height).
		Padding(0, 1)

	rows := m.formRows(width)
	visibleRows := formContentHeight(height)
	start := m.scrollOffset
	if start < 0 {
		start = 0
	}
	if maxStart := maxScrollStart(len(rows), visibleRows); start > maxStart {
		start = maxStart
	}
	end := start + visibleRows
	if end > len(rows) {
		end = len(rows)
	}

	contentWidth := formInnerWidth(width)
	visible := make([]string, 0, visibleRows)
	for _, row := range rows[start:end] {
		// Every semantic row must occupy exactly one terminal row.
		// Several row builders contain ANSI styling, and plain rune-count
		// truncation is not enough: lipgloss will wrap over-wide styled
		// strings inside the bordered box, making the final Save row fall
		// below the alt-screen viewport even when our semantic row slice
		// includes it. Clamp the rendered row at the box's inner display
		// width as a final safety net.
		visible = append(visible, ansi.Truncate(row.text, contentWidth, "…"))
	}

	return box.Render(strings.Join(visible, "\n"))
}

func formInnerWidth(width int) int {
	// renderForm sets total Width(width), a border on both sides, and
	// horizontal padding of one cell on both sides. The text content must
	// fit within what remains or lipgloss wraps it into extra visual rows.
	inner := width - 4
	if inner < 1 {
		return 1
	}
	return inner
}

func (m PresetEditorModel) formRows(width int) []presetEditorRow {
	lbl := func(key string) string { return i18n.T("preset_editor.field_" + key) }
	row := func(f editorField, text string) presetEditorRow {
		return presetEditorRow{text: text, field: f, hasField: true}
	}
	plain := func(text string) presetEditorRow { return presetEditorRow{text: text} }

	var rows []presetEditorRow
	rows = append(rows, plain(m.sectionHeader(i18n.T("preset_editor.section_identity"))))
	// Name row renders the on-disk preset stem. Editable for non-builtins;
	// for builtins, the clone-first overlay still gates renames on save.
	rows = append(rows, row(feName, m.row(feName, lbl("name"), m.working.Name, width-4)))
	rows = append(rows, row(feSummary, m.row(feSummary, lbl("summary"), m.working.Description.Summary, width-4)))
	rows = append(rows, row(feTier, m.row(feTier, lbl("tier"), m.tierDisplay(), width-4)))
	rows = append(rows, row(feGains, m.row(feGains, lbl("gains"), asExtra(m.working.Description.Extra, "gains"), width-4)))
	rows = append(rows, row(feLoses, m.row(feLoses, lbl("loses"), asExtra(m.working.Description.Extra, "loses"), width-4)))
	rows = append(rows, plain(""))
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	// Only the chosen family's fields render. The family itself is named in
	// the section header; the four-family choice appears only to convert a
	// legacy saved provider (fieldVisible(feProvider)).
	llmHeader := i18n.T("preset_editor.section_llm")
	if provider := asString(llm["provider"]); isFamilyProvider(provider) {
		llmHeader += " · " + providerDisplayName(provider)
	}
	rows = append(rows, plain(m.sectionHeader(llmHeader)))
	if m.fieldVisible(feProvider) {
		rows = append(rows, row(feProvider, m.row(feProvider, lbl("provider"), asString(llm["provider"]), width-4)))
	}
	if m.fieldVisible(feModel) {
		rows = append(rows, row(feModel, m.row(feModel, lbl("model"), asString(llm["model"]), width-4)))
	}
	if m.fieldVisible(feServiceTier) {
		rows = append(rows, row(feServiceTier, m.row(feServiceTier, lbl("service_tier"), m.serviceTier(), width-4)))
	}
	if m.fieldVisible(feThinking) {
		rows = append(rows, row(feThinking, m.row(feThinking, lbl("thinking"), m.thinkingValue(), width-4)))
	}
	if m.fieldVisible(feWireAPI) {
		rows = append(rows, row(feWireAPI, m.row(feWireAPI, lbl("wire_api"), m.fieldString(feWireAPI), width-4)))
	}
	if m.fieldVisible(feResponsesTransport) {
		rows = append(rows, row(feResponsesTransport, m.row(feResponsesTransport, lbl("responses_transport"), m.fieldString(feResponsesTransport), width-4)))
	}
	if m.fieldVisible(feBaseURL) {
		rows = append(rows, row(feBaseURL, m.row(feBaseURL, lbl("base_url"), asString(llm["base_url"]), width-4)))
	}
	apiKeyLabel := lbl("api_key")
	if m.isClaudeCode() {
		apiKeyLabel = lbl("claude_auth")
	}
	rows = append(rows, row(feAPIKey, m.row(feAPIKey, apiKeyLabel, m.fieldString(feAPIKey), width-4)))
	rows = append(rows, plain(""))
	// Capabilities — every tool/subsystem the runtime can grant an agent,
	// including web_search and vision. All of them are always included:
	// there is no separate editable-capability concept, no checkbox, and
	// no provider control on this page that can remove or change one.
	// Customizing what an agent can do is done outside the preset editor
	// by asking the agent to explain init.json and hand-editing it there
	// (capabilitiesGuidanceRow below).
	rows = append(rows, plain(m.sectionHeader(i18n.T("preset_editor.section_capabilities"))))
	capabilityRows := []string{
		"email", "psyche", "system",
		"knowledge", "skills", "shell",
		"avatar", "daemon", "mcp", "file",
		"web_search", "vision",
	}
	for _, capName := range capabilityRows {
		rows = append(rows, plain(m.mandatoryCapRow(capName, width-4)))
	}
	rows = append(rows, plain(""))
	rows = append(rows, plain(m.capabilitiesGuidanceRow(width-4)))
	rows = append(rows, plain(""))
	rows = append(rows, row(feSave, m.renderSaveButton()))
	return rows
}

// row renders a single field row with focus styling. When the row is
// in inline-edit mode (cursor here AND mode == emInline) the textinput
// renders in place of the value. The model row gets a special radio-
// strip render when the provider has a known model list, so all
// options are visible at once and ←/→ visibly moves the dot.
func (m PresetEditorModel) row(f editorField, key, value string, width int) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(presetEditorFieldLabelWidth)
	marker := "  "
	valStyle := lipgloss.NewStyle()
	focused := editorFieldOrder[m.cursor] == f
	if focused {
		marker = "▸ "
		valStyle = valStyle.Bold(true).Foreground(ColorAccent)
	}
	if m.mode == emInline && focused {
		return marker + keyStyle.Render(key) + m.input.View()
	}
	if f == feProvider {
		return marker + keyStyle.Render(key) + m.providerRadioStrip(focused, valStyle)
	}
	if f == feModel {
		if strip := m.modelRadioStrip(focused, valStyle); strip != "" {
			return marker + keyStyle.Render(key) + strip
		}
	}
	if f == feServiceTier {
		if strip := m.serviceTierRadioStrip(focused, valStyle); strip != "" {
			return marker + keyStyle.Render(key) + strip
		}
	}
	if f == feThinking {
		if strip := m.thinkingRadioStrip(focused, valStyle); strip != "" {
			return marker + keyStyle.Render(key) + strip
		}
	}
	if f == feWireAPI {
		return marker + keyStyle.Render(key) + m.wireAPIRadioStrip(focused, valStyle)
	}
	if f == feResponsesTransport {
		return marker + keyStyle.Render(key) + m.responsesTransportRadioStrip(focused, valStyle)
	}
	if f == feBaseURL && value == "" {
		// Empty base_url on an API-key family means the official endpoint;
		// say so instead of rendering a bare dash.
		if def := preset.DefaultBaseURL(asString(m.llmMap()["provider"])); def != "" {
			hint := fmt.Sprintf(i18n.T("preset_editor.base_url_official_default"), def)
			hint = truncate(hint, width-lipgloss.Width(marker)-presetEditorFieldLabelWidth)
			return marker + keyStyle.Render(key) + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(hint)
		}
	}
	if value == "" {
		value = "—"
	}
	if f != feTier {
		value = truncate(value, width-lipgloss.Width(marker)-presetEditorFieldLabelWidth)
	}
	return marker + keyStyle.Render(key) + valStyle.Render(value)
}

// mandatoryCapRow renders one capability row in the Capabilities section:
// a fixed, always-checked "[✓] name  description" line. Every capability
// listed in formRows' capabilityRows renders this way — there is no
// toggleable or provider-cyclable variant anymore; the row is purely
// informational.
func (m PresetEditorModel) mandatoryCapRow(name string, width int) string {
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	check := subtle.Render("[✓]")
	keyCol := subtle.Render(lipgloss.NewStyle().Width(15).Render(name))
	desc := i18n.T("firstrun.cap_desc." + name)
	desc = strings.ReplaceAll(desc, "\n", "  ")
	desc = truncate(desc, width-21)
	val := subtle.Render(desc)
	return "  " + check + " " + keyCol + val
}

// capabilitiesGuidanceRow renders the one-line explanation of how to
// customize an agent's capabilities now that this page offers no
// checkbox or provider control that can remove or change one: ask the
// agent to explain init.json, then hand-edit init.json directly.
func (m PresetEditorModel) capabilitiesGuidanceRow(width int) string {
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Italic(true)
	text := truncate(i18n.T("preset_editor.capabilities_guidance"), width)
	return "  " + subtle.Render(text)
}

// modelRadioStrip renders the model field as a horizontal radio strip
// (● selected ○ unselected) when the current provider+route has a known
// model lineup. Returns "" when there's no picker — caller falls back to the
// standard single-value render.
func (m PresetEditorModel) modelRadioStrip(focused bool, valStyle lipgloss.Style) string {
	models := modelOptions(asString(m.llmMap()["provider"]))
	if len(models) == 0 {
		return ""
	}
	return radioStrip(models, asString(m.llmMap()["model"]), focused, valStyle)
}

func (m PresetEditorModel) serviceTierRadioStrip(focused bool, valStyle lipgloss.Style) string {
	current := m.serviceTier()
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	parts := make([]string, 0, len(serviceTierOptions))
	for _, tier := range serviceTierOptions {
		if tier == current {
			if focused {
				parts = append(parts, valStyle.Render("● "+tier))
			} else {
				parts = append(parts, "● "+tier)
			}
		} else {
			parts = append(parts, subtle.Render("○ "+tier))
		}
	}
	return strings.Join(parts, "  ")
}

func (m PresetEditorModel) thinkingRadioStrip(focused bool, valStyle lipgloss.Style) string {
	if !m.hasThinking() {
		return ""
	}
	current := m.thinkingValue()
	options := m.thinkingOptions()
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	parts := make([]string, 0, len(options))
	for _, effort := range options {
		if effort == current {
			if focused {
				parts = append(parts, valStyle.Render("● "+effort))
			} else {
				parts = append(parts, "● "+effort)
			}
		} else {
			parts = append(parts, subtle.Render("○ "+effort))
		}
	}
	separator := "  "
	if m.hasLevelThinking() {
		// Seven level choices still need to fit in the standard 80-column form.
		separator = " "
	}
	return strings.Join(parts, separator)
}

// radioStrip renders options as a horizontal radio strip (● selected,
// ○ unselected). A current value that is not one of the options leaves every
// dot hollow and is appended after the strip so the row never claims a value
// the preset does not hold.
func radioStrip(options []string, current string, focused bool, valStyle lipgloss.Style) string {
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	parts := make([]string, 0, len(options)+1)
	for _, option := range options {
		switch {
		case option == current && focused:
			parts = append(parts, valStyle.Render("● "+option))
		case option == current:
			parts = append(parts, "● "+option)
		default:
			parts = append(parts, subtle.Render("○ "+option))
		}
	}
	if current != "" && !containsString(options, current) {
		parts = append(parts, subtle.Render(current))
	}
	return strings.Join(parts, "  ")
}

// providerRadioStrip shows the four provider families side by side so the
// user can see every choice the ←/→ cycle offers. A legacy saved provider
// (not one of the four) is shown after the strip with no dot selected.
func (m PresetEditorModel) providerRadioStrip(focused bool, valStyle lipgloss.Style) string {
	return radioStrip(editorProviders, m.fieldString(feProvider), focused, valStyle)
}

// wireAPIRadioStrip renders both openai-family wire choices.
func (m PresetEditorModel) wireAPIRadioStrip(focused bool, valStyle lipgloss.Style) string {
	return radioStrip(wireAPIOptions, m.fieldString(feWireAPI), focused, valStyle)
}

// responsesTransportRadioStrip shows the default HTTP path and explicit
// WebSocket v2 opt-in side by side.
func (m PresetEditorModel) responsesTransportRadioStrip(focused bool, valStyle lipgloss.Style) string {
	return radioStrip(responsesTransportOptions, m.fieldString(feResponsesTransport), focused, valStyle)
}

// isCyclable reports whether a field accepts ←/→ to step through enum
// values. The model row is conditional on the current provider+route having
// a known model lineup; uncurated routes remain inline-edit-only.
func (m PresetEditorModel) isCyclable(f editorField) bool {
	switch f {
	case feProvider, feTier:
		return true
	case feServiceTier:
		return m.hasServiceTier()
	case feThinking:
		return m.hasThinking()
	case feWireAPI:
		return m.isOpenAIFamily()
	case feResponsesTransport:
		return m.isOpenAIResponses()
	case feAPIKey:
		// For codex, the "API key" row is an account selector: ←/→ binds the
		// preset to a different Codex OAuth account when more than one exists.
		return m.isCodexProvider() && len(m.codexAccountRefs()) > 1
	case feModel:
		return len(modelOptions(asString(m.llmMap()["provider"]))) > 0
	}
	return false
}

func (m PresetEditorModel) sectionHeader(label string) string {
	return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true).Render("── " + label + " ──")
}

func (m PresetEditorModel) tierDisplay() string {
	if m.working.Description.Tier == "" {
		return ""
	}
	return tierChipStyle(m.working.Description.Tier).Render(tierLabel(m.working.Description.Tier, m.lang))
}

// renderPreview is the right-hand pane: live JSON + validation status.
func (m PresetEditorModel) renderPreview(width, height int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("245")).
		Width(width).
		Height(height).
		Padding(0, 1)

	js, _ := json.MarshalIndent(m.working, "", "  ")
	preview := string(js)
	// Truncate overly long previews — the form is the source of truth,
	// the preview is for orientation. Width-trim happens via lipgloss.
	maxLines := height - 8
	if maxLines < 4 {
		maxLines = 4
	}
	lines := strings.Split(preview, "\n")
	if len(lines) > maxLines {
		lines = append(lines[:maxLines], "  …")
	}
	preview = strings.Join(lines, "\n")

	var b strings.Builder
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true).Render("── JSON ──"))
	b.WriteString("\n")
	b.WriteString(preview)
	b.WriteString("\n\n")
	b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Bold(true).Render("── " + i18n.T("preset_editor.validation") + " ──"))
	b.WriteString("\n")
	if errs := m.working.Validate(); len(errs) == 0 {
		b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color("84")).Render("✓ " + i18n.T("preset_editor.valid")))
	} else {
		errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
		for _, e := range errs {
			b.WriteString(errStyle.Render("✗ "+e.Error()) + "\n")
		}
	}
	return box.Render(b.String())
}

func (m *PresetEditorModel) moveCursor(delta int) {
	if delta == 0 {
		m.normalizeCursor()
		m.ensureFocusedVisible()
		return
	}
	step := 1
	if delta < 0 {
		step = -1
		delta = -delta
	}
	for i := 0; i < delta; i++ {
		next := m.cursor + step
		for next >= 0 && next <= saveFieldIndex && !m.fieldVisible(editorFieldOrder[next]) {
			next += step
		}
		if next < 0 || next > saveFieldIndex {
			break
		}
		m.cursor = next
	}
	m.normalizeCursor()
	m.ensureFocusedVisible()
}

func (m *PresetEditorModel) ensureFocusedVisible() {
	m.normalizeCursor()
	if m.width == 0 || m.height == 0 {
		return
	}
	rows := m.formRows(m.width)
	focused := m.focusedRowIndex(rows)
	if focused < 0 {
		return
	}
	visibleRows := m.visibleFormRows()
	if visibleRows < 1 {
		visibleRows = 1
	}
	if focused < m.scrollOffset {
		m.scrollOffset = focused
	} else if focused >= m.scrollOffset+visibleRows {
		m.scrollOffset = focused - visibleRows + 1
	}
	if maxStart := maxScrollStart(len(rows), visibleRows); m.scrollOffset > maxStart {
		m.scrollOffset = maxStart
	}
	if m.scrollOffset < 0 {
		m.scrollOffset = 0
	}
}

func (m PresetEditorModel) fieldVisible(f editorField) bool {
	switch f {
	case feProvider:
		// The family is chosen when the preset is created (by its template),
		// so the four-family choice is only offered to convert a legacy saved
		// provider. Keyed on the original provider so a conversion can still
		// cycle through every family before Save.
		return !isFamilyProvider(asString(m.originalLLM()["provider"]))
	case feModel, feBaseURL:
		// claude-code is auth-only: no model, no endpoint.
		return !m.isClaudeCode()
	case feServiceTier:
		return m.hasServiceTier()
	case feThinking:
		return m.hasThinking()
	case feWireAPI:
		return m.isOpenAIFamily()
	case feResponsesTransport:
		return m.isOpenAIResponses()
	default:
		return true
	}
}

func (m *PresetEditorModel) normalizeCursor() {
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor > saveFieldIndex {
		m.cursor = saveFieldIndex
	}
	if m.fieldVisible(editorFieldOrder[m.cursor]) {
		return
	}
	for i := m.cursor + 1; i <= saveFieldIndex; i++ {
		if m.fieldVisible(editorFieldOrder[i]) {
			m.cursor = i
			return
		}
	}
	for i := m.cursor - 1; i >= 0; i-- {
		if m.fieldVisible(editorFieldOrder[i]) {
			m.cursor = i
			return
		}
	}
}

func (m PresetEditorModel) focusedRowIndex(rows []presetEditorRow) int {
	if m.cursor < 0 || m.cursor >= len(editorFieldOrder) {
		return -1
	}
	focusedField := editorFieldOrder[m.cursor]
	for i, row := range rows {
		if row.hasField && row.field == focusedField {
			return i
		}
	}
	return -1
}

func (m PresetEditorModel) visibleFormRows() int {
	return formContentHeight(m.height - 4)
}

func formContentHeight(boxHeight int) int {
	// renderForm applies a rounded border and no vertical padding, so the
	// interior content is the requested box height minus top/bottom border.
	rows := boxHeight - 2
	if rows < 1 {
		return 1
	}
	return rows
}

func maxScrollStart(rowCount, visibleRows int) int {
	if visibleRows < 1 {
		visibleRows = 1
	}
	if rowCount <= visibleRows {
		return 0
	}
	return rowCount - visibleRows
}

func (m PresetEditorModel) renderFooter() string {
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	if m.saveErr != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  " + m.saveErr)
	}
	claudeAuthFocused := m.isClaudeCode() && editorFieldOrder[m.cursor] == feAPIKey
	switch m.mode {
	case emInline:
		if claudeAuthFocused {
			return hintStyle.Render("  " + i18n.T("claude.setup_token_hint") + "  ·  " + i18n.T("preset_editor.hint_inline"))
		}
		return hintStyle.Render("  " + i18n.T("preset_editor.hint_inline"))
	case emDirtyPrompt:
		return hintStyle.Render("  " + i18n.T("preset_editor.hint_dirty"))
	case emExitPrompt:
		return hintStyle.Render("  " + i18n.T("preset_editor.hint_exit"))
	}
	hint := i18n.T("preset_editor.hint_browse")
	if m.statusMsg != "" {
		// Transient feedback (e.g. a completed CLI credential import)
		// replaces the generic browse hint until the next keypress.
		hint = m.statusMsg
	} else if claudeAuthFocused {
		// The claude-code auth row takes a `claude setup-token` token.
		hint = "[Enter] " + i18n.T("claude.setup_token_hint") + "  ·  " + hint
	} else if m.codexCLIImportAvailable() {
		// A `codex login` credential exists while this preset's bound
		// account is invalid: advertise the one-click import on the
		// codex API-key row.
		hint += "  " + i18n.T("codex.import_cli_hint")
	}
	return hintStyle.Render("  " + hint)
}

func (m PresetEditorModel) renderCloneOverlay(_ string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	body := titleStyle.Render(i18n.T("preset_editor.clone_title")) + "\n\n" +
		i18n.T("preset_editor.clone_explain") + "\n\n" +
		subtle.Render("name: ") + m.cloneNameInput.View() + "\n\n" +
		subtle.Render(i18n.T("preset_editor.clone_hint"))
	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m PresetEditorModel) renderDirtyOverlay(_ string) string {
	style := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(i18n.T("preset_editor.dirty_prompt") + "\n\n" +
			lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render("[y] "+i18n.T("preset_editor.discard")+
				"   [n/Esc] "+i18n.T("preset_editor.cancel_discard")))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, style)
}

func (m PresetEditorModel) renderExitOverlay(_ string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	body := titleStyle.Render(i18n.T("preset_editor.exit_title")) + "\n\n" +
		subtle.Render(i18n.T("preset_editor.exit_hint"))
	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(lipgloss.Color("214")).
		Padding(1, 2).
		Render(body)
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// renderSaveButton emits the save row at the bottom of the form. When
// the cursor is on it, the row pops in accent color; Enter triggers
// commit. Acts like a button users can find by tabbing down.
func (m PresetEditorModel) renderSaveButton() string {
	focused := editorFieldOrder[m.cursor] == feSave
	label := "[ " + i18n.T("preset_editor.save_button") + " ]"
	if focused {
		return "▸ " + lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorAccent).
			Render(label)
	}
	return "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(label)
}

// ───────────────────────────────────────────────────────────────────────────
// Private helpers
// ───────────────────────────────────────────────────────────────────────────

// clonePresetForEditor deep-copies a Preset via JSON round-trip so the
// editor's working copy doesn't share map references with the caller.
// preset.Clone changes the Name; we preserve every serialized field here.
// Source is runtime-only and is deliberately re-established by commitCmd.
func clonePresetForEditor(p preset.Preset) preset.Preset {
	data, err := json.Marshal(p)
	if err != nil {
		return p
	}
	var out preset.Preset
	if err := preset.DecodeJSONUseNumber(data, &out); err != nil {
		return p
	}
	return out
}

func asBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

func asExtra(extra map[string]interface{}, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
}

// maskAPIKey returns a display form for an API key — the last 4 chars
// preceded by ••• padding, or the i18n placeholder when empty. We never
// show the full key on screen; pasting a new value triggers a fresh
// edit which then masks again on commit.
func maskAPIKey(key string) string {
	if key == "" {
		return i18n.T("preset_editor.api_key_unset")
	}
	if len(key) <= 4 {
		return strings.Repeat("•", len(key))
	}
	return "••••••••" + key[len(key)-4:]
}

// containsString reports whether v is one of opts.
func containsString(opts []string, v string) bool {
	for _, o := range opts {
		if o == v {
			return true
		}
	}
	return false
}

// cycleString rotates `cur` through `opts` by `dir` steps. Unknown
// values land at index 0 on +1, last index on -1.
func cycleString(opts []string, cur string, dir int) string {
	idx := 0
	for i, v := range opts {
		if v == cur {
			idx = i
			break
		}
	}
	idx = (idx + dir + len(opts)) % len(opts)
	return opts[idx]
}
