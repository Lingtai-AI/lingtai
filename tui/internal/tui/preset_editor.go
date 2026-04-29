package tui

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// PresetEditorCommitMsg fires when the editor's working copy passes
// validation and the user pressed Ctrl+S. Hosts (firstrun, /setup,
// library) decide what to do next — typically: persist via preset.Save,
// then advance their own state. The editor itself does NOT save to disk.
type PresetEditorCommitMsg struct {
	Preset preset.Preset
}

// PresetEditorCancelMsg fires on Esc (and after the dirty-prompt
// confirms discard). Hosts return to whichever screen they came from.
type PresetEditorCancelMsg struct{}

// editorField identifies a row in the form.
type editorField int

const (
	feSummary editorField = iota
	feTier
	feGains
	feLoses
	feProvider
	feModel
	feAPICompat
	feBaseURL
	feAPIKeyEnv
	feContextLimit
	feCapabilities
	feStreaming
	feKarma
)

// editorFieldOrder is the rendering order of fields. The cursor walks
// this slice; section headers render between transitions.
var editorFieldOrder = []editorField{
	feSummary, feTier, feGains, feLoses,
	feProvider, feModel, feAPICompat, feBaseURL, feAPIKeyEnv, feContextLimit,
	feCapabilities,
	feStreaming, feKarma,
}

type editorMode int

const (
	emBrowse       editorMode = iota // navigating field list
	emInline                         // textinput active for the focused field
	emCapabilities                   // capability-edit modal
	emCapInline                      // inline edit of a capability subfield (e.g. yolo, paths)
	emClonePrompt                    // built-in: prompt for new name on semantic edit
	emDirtyPrompt                    // "discard changes? y/N"
)

// capabilityProviderOptions enumerates the multi-provider capabilities
// the editor knows about. Order matters — tab cycles through this list
// in declaration order. "inherit" means "use the main LLM's provider"
// via the kernel's expand_inherit logic.
var capabilityProviderOptions = map[string][]string{
	"web_search": {"duckduckgo", "minimax", "zhipu", "codex", "inherit"},
	"vision":     {"inherit", "minimax", "zhipu", "mimo", "codex"},
}

// editorCapabilities is the canonical capability list shown in the
// sub-modal. Mirrors AllCapabilities from presets.go but kept local so
// the editor doesn't quietly absorb additions to AllCapabilities that
// haven't been thought about for the per-preset baseline view.
var editorCapabilities = []string{
	"file", "email", "bash", "web_search", "psyche", "codex",
	"vision", "avatar", "daemon", "library",
}

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

	// Inline textinput, reused for whichever field is being edited.
	input textinput.Model

	// cloneNameInput captures the new preset name during the clone-first
	// prompt overlay.
	cloneNameInput textinput.Model

	// Capability sub-modal state. capCursor is the row index in the
	// capability list. capSubField is "yolo" or "paths" while inline-
	// editing a capability's nested config; "" otherwise.
	capCursor   int
	capSubField string

	// Display
	width, height int
	lang          string // "en"/"zh"/"wen" — drives tier label rendering

	// Status
	saveErr string
}

// NewPresetEditorModel builds an editor against a working copy of `p`.
// The model never mutates `p`; the host receives the modified version
// via PresetEditorCommitMsg. isBuiltin gates the clone-first prompt on
// semantic edits — pass preset.IsBuiltin(p.Name).
func NewPresetEditorModel(p preset.Preset, lang string) PresetEditorModel {
	return NewPresetEditorModelWithBuiltinFlag(p, lang, preset.IsBuiltin(p.Name))
}

// NewPresetEditorModelWithBuiltinFlag is the explicit-flag variant for
// callers that want to override built-in protection (e.g. tests, or
// a future "fork built-in" flow that has already cloned upstream).
func NewPresetEditorModelWithBuiltinFlag(p preset.Preset, lang string, isBuiltin bool) PresetEditorModel {
	ti := textinput.New()
	ti.CharLimit = 256
	ti.SetWidth(40)
	cn := textinput.New()
	cn.CharLimit = 64
	cn.SetWidth(30)
	return PresetEditorModel{
		original:       clonePresetForEditor(p),
		working:        clonePresetForEditor(p),
		isBuiltin:      isBuiltin,
		cursor:         0,
		mode:           emBrowse,
		input:          ti,
		cloneNameInput: cn,
		lang:           lang,
	}
}

func (m PresetEditorModel) Init() tea.Cmd { return nil }

func (m PresetEditorModel) Update(msg tea.Msg) (PresetEditorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case tea.KeyMsg:
		switch m.mode {
		case emInline:
			return m.updateInline(msg)
		case emCapabilities:
			return m.updateCapabilities(msg)
		case emCapInline:
			return m.updateCapInline(msg)
		case emClonePrompt:
			return m.updateClonePrompt(msg)
		case emDirtyPrompt:
			return m.updateDirtyPrompt(msg)
		default:
			return m.updateBrowse(msg)
		}
	}
	return m, nil
}

// ───────────────────────────────────────────────────────────────────────────
// Update — browse mode (cursor over field rows)
// ───────────────────────────────────────────────────────────────────────────

func (m PresetEditorModel) updateBrowse(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.isDirty() {
			m.mode = emDirtyPrompt
			return m, nil
		}
		return m, func() tea.Msg { return PresetEditorCancelMsg{} }
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
		return m, nil
	case "down", "j":
		if m.cursor < len(editorFieldOrder)-1 {
			m.cursor++
		}
		return m, nil
	case "left", "h":
		// Cycle backwards on enum fields.
		m.cycleFocused(-1)
		return m, nil
	case "right", "l", "tab":
		m.cycleFocused(+1)
		return m, nil
	case " ":
		m.toggleFocused()
		return m, nil
	case "c":
		// Fast path to capability modal regardless of cursor position.
		m.openCapabilities()
		return m, nil
	case "enter":
		return m.openInline()
	case "ctrl+s":
		return m.commit()
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

// ───────────────────────────────────────────────────────────────────────────
// Field-level mutation
// ───────────────────────────────────────────────────────────────────────────

func (m *PresetEditorModel) openInline() (PresetEditorModel, tea.Cmd) {
	f := editorFieldOrder[m.cursor]
	switch f {
	case feSummary, feGains, feLoses, feModel, feBaseURL, feAPIKeyEnv, feContextLimit:
		m.input.SetValue(m.fieldString(f))
		m.input.CursorEnd()
		m.input.Focus()
		m.mode = emInline
	case feTier:
		// Tier is an enum — Enter cycles like ←/→. No picker overlay.
		m.cycleFocused(+1)
	case feCapabilities:
		m.openCapabilities()
	case feProvider, feAPICompat:
		// Enums — Enter cycles forward (same as Right). Lets the user
		// stay on the keyboard's "advance" key.
		m.cycleFocused(+1)
	case feStreaming, feKarma:
		m.toggleFocused()
	}
	return *m, nil
}

func (m *PresetEditorModel) openCapabilities() {
	m.capCursor = 0
	m.capSubField = ""
	m.mode = emCapabilities
}

// updateCapabilities handles the capability modal's main list.
func (m PresetEditorModel) updateCapabilities(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = emBrowse
		return m, nil
	case "up", "k":
		if m.capCursor > 0 {
			m.capCursor--
		}
		return m, nil
	case "down", "j":
		if m.capCursor < len(editorCapabilities)-1 {
			m.capCursor++
		}
		return m, nil
	case " ", "space":
		m.toggleCapability(editorCapabilities[m.capCursor])
		return m, nil
	case "tab", "right", "l":
		m.cycleCapProvider(editorCapabilities[m.capCursor], +1)
		return m, nil
	case "shift+tab", "left", "h":
		m.cycleCapProvider(editorCapabilities[m.capCursor], -1)
		return m, nil
	case "enter":
		// On rows that have a nested config (bash.yolo, library.paths),
		// drop into a single-line inline edit. Other rows: enter is a
		// no-op (use space to toggle, tab to cycle providers).
		name := editorCapabilities[m.capCursor]
		switch name {
		case "bash":
			// Toggle yolo via Enter as a one-keystroke shortcut.
			caps := m.capsMap()
			cfg := capCfgMap(caps, "bash")
			cfg["yolo"] = !asBool(cfg["yolo"])
			caps["bash"] = cfg
		case "library":
			// Open inline editor with comma-joined paths.
			caps := m.capsMap()
			cfg := capCfgMap(caps, "library")
			paths := pathsFromConfig(cfg)
			m.input.SetValue(strings.Join(paths, ","))
			m.input.CursorEnd()
			m.input.Focus()
			m.capSubField = "paths"
			m.mode = emCapInline
		}
		return m, nil
	}
	return m, nil
}

// updateCapInline handles the inline edit of a capability sub-field
// (currently only library.paths). Enter commits, esc abandons.
func (m PresetEditorModel) updateCapInline(msg tea.KeyMsg) (PresetEditorModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.mode = emCapabilities
		m.capSubField = ""
		m.input.Blur()
		return m, nil
	case "enter":
		switch m.capSubField {
		case "paths":
			caps := m.capsMap()
			cfg := capCfgMap(caps, "library")
			parts := strings.Split(m.input.Value(), ",")
			cleaned := make([]interface{}, 0, len(parts))
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					cleaned = append(cleaned, p)
				}
			}
			cfg["paths"] = cleaned
			caps["library"] = cfg
		}
		m.mode = emCapabilities
		m.capSubField = ""
		m.input.Blur()
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// toggleCapability flips a capability on/off in the working manifest.
// Enabling synthesizes a sensible default config; disabling deletes the
// entry. Provider preferences are preserved across off→on cycles via
// the existing entry shape.
func (m *PresetEditorModel) toggleCapability(name string) {
	caps := m.capsMap()
	if _, on := caps[name]; on {
		delete(caps, name)
		return
	}
	// Synthesize a reasonable default config.
	cfg := map[string]interface{}{}
	switch name {
	case "bash":
		cfg["yolo"] = false
	case "library":
		cfg["paths"] = []interface{}{"../.library_shared", "~/.lingtai-tui/utilities"}
	case "web_search":
		cfg["provider"] = "duckduckgo"
	case "vision":
		cfg["provider"] = "inherit"
	}
	caps[name] = cfg
}

// cycleCapProvider rotates the provider field on a multi-provider capability.
// No-op on caps that aren't enabled or don't have a provider list.
func (m *PresetEditorModel) cycleCapProvider(name string, dir int) {
	opts, ok := capabilityProviderOptions[name]
	if !ok {
		return
	}
	caps := m.capsMap()
	cfg, on := caps[name].(map[string]interface{})
	if !on {
		return
	}
	cur, _ := cfg["provider"].(string)
	cfg["provider"] = cycleString(opts, cur, dir)
	caps[name] = cfg
}

// capsMap returns manifest.capabilities, allocating it if missing.
func (m *PresetEditorModel) capsMap() map[string]interface{} {
	caps, _ := m.working.Manifest["capabilities"].(map[string]interface{})
	if caps == nil {
		caps = map[string]interface{}{}
		m.working.Manifest["capabilities"] = caps
	}
	return caps
}

// capCfgMap returns the config map for a single capability inside caps,
// allocating it if the existing value is nil/missing/empty.
func capCfgMap(caps map[string]interface{}, name string) map[string]interface{} {
	cfg, _ := caps[name].(map[string]interface{})
	if cfg == nil {
		cfg = map[string]interface{}{}
	}
	return cfg
}

// pathsFromConfig coerces config["paths"] to []string, accepting both
// []interface{} (post-JSON) and []string (post-Go-construction) shapes.
func pathsFromConfig(cfg map[string]interface{}) []string {
	switch v := cfg["paths"].(type) {
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, p := range v {
			if s, ok := p.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

// applyInline writes the textinput's current value into the working
// copy, with light coercion for numeric fields.
func (m *PresetEditorModel) applyInline(val string) {
	val = strings.TrimSpace(val)
	f := editorFieldOrder[m.cursor]
	llm := m.llmMap()
	switch f {
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
	case feAPIKeyEnv:
		llm["api_key_env"] = val
	case feContextLimit:
		if val == "" {
			delete(llm, "context_limit")
		} else if n, err := strconv.Atoi(val); err == nil && n > 0 {
			llm["context_limit"] = n
		}
		// Invalid input: silently keep the previous value. The
		// validation footer will already complain if the existing
		// value is wrong; we don't want a typo to clobber a good one.
	}
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
		// Order matches the builtin presets (preset.go BuiltinPresets).
		// Keep this in sync when adding a new provider/builtin.
		opts := []string{"minimax", "zhipu", "mimo", "deepseek", "openrouter", "codex", "custom"}
		m.llmMap()["provider"] = cycleString(opts, m.fieldString(f), dir)
	case feAPICompat:
		opts := []string{"", "openai", "anthropic"}
		m.llmMap()["api_compat"] = cycleString(opts, m.fieldString(f), dir)
	case feTier:
		// Cycle ""→1→2→3→4→5→"" with → and reverse with ←. tierValues
		// is ordered best-first ([5..1]) for the library's picker, so
		// reverse it here for the natural ascending sweep.
		opts := []string{"", "1", "2", "3", "4", "5"}
		m.working.Description.Tier = cycleString(opts, m.working.Description.Tier, dir)
	}
}

// toggleFocused flips bool fields.
func (m *PresetEditorModel) toggleFocused() {
	f := editorFieldOrder[m.cursor]
	switch f {
	case feStreaming:
		m.working.Manifest["streaming"] = !asBool(m.working.Manifest["streaming"])
	case feKarma:
		admin, _ := m.working.Manifest["admin"].(map[string]interface{})
		if admin == nil {
			admin = map[string]interface{}{}
			m.working.Manifest["admin"] = admin
		}
		admin["karma"] = !asBool(admin["karma"])
	}
}

func (m PresetEditorModel) commit() (PresetEditorModel, tea.Cmd) {
	if errs := m.working.Validate(); len(errs) > 0 {
		m.saveErr = errs[0].Error()
		return m, nil
	}
	// Built-in protection: if the user changed any semantic field
	// (llm.*, capabilities.*, or name) on a built-in preset, gate the
	// save behind a clone-first prompt. Editorial-only edits (summary,
	// tier, gains, loses) save in place — they're user annotations
	// that should outlive a TUI upgrade.
	if m.isBuiltin && m.hasSemanticEdits() {
		m.cloneNameInput.SetValue(m.working.Name + "_custom")
		m.cloneNameInput.CursorEnd()
		m.cloneNameInput.Focus()
		m.mode = emClonePrompt
		return m, nil
	}
	committed := clonePresetForEditor(m.working)
	return m, func() tea.Msg { return PresetEditorCommitMsg{Preset: committed} }
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
		return m, func() tea.Msg { return PresetEditorCommitMsg{Preset: committed} }
	case "enter":
		newName := strings.TrimSpace(m.cloneNameInput.Value())
		if newName == "" {
			m.saveErr = "name cannot be empty"
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
		return m, func() tea.Msg { return PresetEditorCommitMsg{Preset: committed} }
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

// fieldString returns the current display value for the given field.
func (m PresetEditorModel) fieldString(f editorField) string {
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	switch f {
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
	case feAPICompat:
		s, _ := llm["api_compat"].(string)
		return s
	case feBaseURL:
		s, _ := llm["base_url"].(string)
		return s
	case feAPIKeyEnv:
		s, _ := llm["api_key_env"].(string)
		return s
	case feContextLimit:
		switch v := llm["context_limit"].(type) {
		case float64:
			return strconv.Itoa(int(v))
		case int:
			return strconv.Itoa(v)
		}
		return ""
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

	bodyHeight := m.height - 3
	if bodyHeight < 6 {
		bodyHeight = 6
	}

	// Title bar.
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	title := titleStyle.Render(i18n.T("preset_editor.title") + ": " + m.working.Name)
	if label := tierLabel(m.working.Description.Tier, m.lang); label != "" {
		title += "  " + tierChipStyle(m.working.Description.Tier).Render(label)
	}

	// Two-column body when wide enough; single column otherwise.
	var body string
	if m.width >= 100 {
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
	case emCapabilities, emCapInline:
		full = m.renderCapOverlay(full)
	case emClonePrompt:
		full = m.renderCloneOverlay(full)
	case emDirtyPrompt:
		full = m.renderDirtyOverlay(full)
	}
	return full
}

func (m PresetEditorModel) renderForm(width, height int) string {
	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("245")).
		Width(width).
		Height(height).
		Padding(0, 1)

	lbl := func(key string) string { return i18n.T("preset_editor.field_" + key) }

	var rows []string
	rows = append(rows, m.sectionHeader(i18n.T("preset_editor.section_identity")))
	rows = append(rows, m.row(feSummary, lbl("summary"), m.working.Description.Summary, width-4))
	rows = append(rows, m.row(feTier, lbl("tier"), m.tierDisplay(), width-4))
	rows = append(rows, m.row(feGains, lbl("gains"), asExtra(m.working.Description.Extra, "gains"), width-4))
	rows = append(rows, m.row(feLoses, lbl("loses"), asExtra(m.working.Description.Extra, "loses"), width-4))
	rows = append(rows, "")
	rows = append(rows, m.sectionHeader(i18n.T("preset_editor.section_llm")))
	llm, _ := m.working.Manifest["llm"].(map[string]interface{})
	rows = append(rows, m.row(feProvider, lbl("provider"), asString(llm["provider"]), width-4))
	rows = append(rows, m.row(feModel, lbl("model"), asString(llm["model"]), width-4))
	rows = append(rows, m.row(feAPICompat, lbl("api_compat"), asString(llm["api_compat"]), width-4))
	rows = append(rows, m.row(feBaseURL, lbl("base_url"), asString(llm["base_url"]), width-4))
	rows = append(rows, m.row(feAPIKeyEnv, lbl("api_key_env"), asString(llm["api_key_env"]), width-4))
	rows = append(rows, m.row(feContextLimit, lbl("context_limit"), m.fieldString(feContextLimit), width-4))
	rows = append(rows, "")
	rows = append(rows, m.sectionHeader(i18n.T("preset_editor.section_capabilities")))
	rows = append(rows, m.row(feCapabilities, lbl("edit"), m.capabilitiesSummary(), width-4))
	rows = append(rows, "")
	rows = append(rows, m.sectionHeader(i18n.T("preset_editor.section_runtime")))
	streaming := asBool(m.working.Manifest["streaming"])
	rows = append(rows, m.row(feStreaming, lbl("streaming"), boolLabel(streaming), width-4))
	karma := false
	if admin, ok := m.working.Manifest["admin"].(map[string]interface{}); ok {
		karma = asBool(admin["karma"])
	}
	rows = append(rows, m.row(feKarma, lbl("karma"), boolLabel(karma), width-4))

	return box.Render(strings.Join(rows, "\n"))
}

// row renders a single field row with focus styling. When the row is
// in inline-edit mode (cursor here AND mode == emInline) the textinput
// renders in place of the value.
func (m PresetEditorModel) row(f editorField, key, value string, width int) string {
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Width(15)
	marker := "  "
	valStyle := lipgloss.NewStyle()
	if editorFieldOrder[m.cursor] == f {
		marker = "▸ "
		valStyle = valStyle.Bold(true).Foreground(ColorAccent)
	}
	if m.mode == emInline && editorFieldOrder[m.cursor] == f {
		return marker + keyStyle.Render(key) + m.input.View()
	}
	if value == "" {
		value = "—"
	}
	return marker + keyStyle.Render(key) + valStyle.Render(value)
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

// capabilitiesSummary renders the capability set as a count plus the
// sorted name list. Press Enter on this row to open the capability
// modal for full editing.
func (m PresetEditorModel) capabilitiesSummary() string {
	caps, _ := m.working.Manifest["capabilities"].(map[string]interface{})
	if len(caps) == 0 {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("245")).Render(i18n.T("preset_editor.caps_none"))
	}
	names := make([]string, 0, len(caps))
	for k := range caps {
		names = append(names, k)
	}
	sort.Strings(names)
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	return subtle.Render(fmt.Sprintf("(%d)  %s", len(caps), strings.Join(names, ", ")))
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

func (m PresetEditorModel) renderFooter() string {
	hintStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	if m.saveErr != "" {
		return lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Render("  " + m.saveErr)
	}
	switch m.mode {
	case emInline:
		return hintStyle.Render("  " + i18n.T("preset_editor.hint_inline"))
	case emDirtyPrompt:
		return hintStyle.Render("  " + i18n.T("preset_editor.hint_dirty"))
	}
	return hintStyle.Render("  " + i18n.T("preset_editor.hint_browse"))
}

func (m PresetEditorModel) renderCapOverlay(_ string) string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	cursorStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	subtle := lipgloss.NewStyle().Foreground(lipgloss.Color("245"))

	caps := m.capsMap()

	var rows []string
	rows = append(rows, titleStyle.Render(i18n.T("preset_editor.cap_picker_title")))
	rows = append(rows, "")

	for i, name := range editorCapabilities {
		cfg, on := caps[name].(map[string]interface{})
		marker := "  "
		nameStyle := lipgloss.NewStyle()
		if i == m.capCursor {
			marker = "▸ "
			nameStyle = cursorStyle
		}
		check := "[ ]"
		if on {
			check = "[✓]"
		}

		// Inline meta render (provider, yolo, paths preview).
		var meta string
		switch name {
		case "bash":
			if on {
				if asBool(cfg["yolo"]) {
					meta = "  yolo:on"
				} else {
					meta = "  yolo:off"
				}
			}
		case "library":
			if on {
				ps := pathsFromConfig(cfg)
				if len(ps) == 0 {
					meta = "  (no paths)"
				} else {
					meta = "  " + strings.Join(ps, ", ")
				}
			}
		default:
			if _, multi := capabilityProviderOptions[name]; multi && on {
				prov, _ := cfg["provider"].(string)
				if prov == "" {
					prov = "inherit"
				}
				meta = "  prov:" + prov
			}
		}
		row := marker + check + " " + nameStyle.Render(name) + subtle.Render(meta)
		rows = append(rows, row)
	}

	// Inline edit field for library.paths
	if m.mode == emCapInline && m.capSubField == "paths" {
		rows = append(rows, "")
		rows = append(rows, subtle.Render("paths (comma-separated):"))
		rows = append(rows, "  "+m.input.View())
	}

	rows = append(rows, "")
	switch m.mode {
	case emCapInline:
		rows = append(rows, subtle.Render(i18n.T("preset_editor.cap_inline_hint")))
	default:
		rows = append(rows, subtle.Render(i18n.T("preset_editor.cap_hint")))
	}

	box := lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(ColorAccent).
		Padding(1, 2).
		Render(strings.Join(rows, "\n"))
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
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

// ───────────────────────────────────────────────────────────────────────────
// Private helpers
// ───────────────────────────────────────────────────────────────────────────

// clonePresetForEditor deep-copies a Preset via JSON round-trip so the
// editor's working copy doesn't share map references with the caller.
// preset.Clone changes the Name; we want everything preserved.
func clonePresetForEditor(p preset.Preset) preset.Preset {
	data, err := json.Marshal(p)
	if err != nil {
		return p
	}
	var out preset.Preset
	if err := json.Unmarshal(data, &out); err != nil {
		return p
	}
	return out
}

func asBool(v interface{}) bool {
	b, _ := v.(bool)
	return b
}

func boolLabel(b bool) string {
	if b {
		return i18n.T("preset_editor.bool_on")
	}
	return i18n.T("preset_editor.bool_off")
}

func asExtra(extra map[string]interface{}, key string) string {
	if extra == nil {
		return ""
	}
	s, _ := extra[key].(string)
	return s
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
