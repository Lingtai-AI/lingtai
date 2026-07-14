package tui

import (
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// ProjectDraft is the single in-memory holder for every choice made while
// creating a NEW project through the no-project launcher. Before the atomic
// rename, its project-local choices may be materialized only inside the owned
// sibling staging directory; no final .lingtai path or global config, preset,
// credential, or registry state is persisted. See Invariant 3/4 of the launcher
// design (reports/design/lingtai-tui-no-project-launcher-2026-07-14). Until
// confirmation, ProjectDraft is the only source of truth for theme/language,
// credential material, preset edits, agent options, and recipe selection.
//
// Secret hygiene: DraftAPIKey and DraftCodexTokens hold credential material
// that must never appear in a String()/Sprintf("%+v", ...)/error-wrapping
// context. ProjectDraft deliberately has NO String()/GoString method — the
// default struct formatting on %v/%+v would print field values directly, so
// callers must never format a whole ProjectDraft for logs or error text.
// Format individual non-secret fields (Theme, Language, AgentName, ...)
// instead. secretString wraps the two credential fields so an accidental
// %v/%s on the wrapper itself prints "<redacted>" rather than the value.
type ProjectDraft struct {
	// ProjectRoot is the directory (cwd at launcher entry) the new project
	// will be created in. Never mutated before commit.
	ProjectRoot string

	// Theme/Language mirror config.TUIConfig fields the Welcome step would
	// otherwise save immediately via config.SaveTUIConfig. Empty means
	// "use the existing global config value / default" at commit time.
	Theme    string
	Language string

	// DraftAPIKeyEnv/DraftAPIKey hold only the pending provider API key for
	// the preset actually shown on the Review step before it is written to
	// ~/.lingtai-tui/config.json + .env. FirstRunModel keeps navigation-time
	// edits in a per-api_key_env in-memory map and copies only the reviewed
	// preset's slot here on every enterReviewStep call. DraftAPIKey is wrapped
	// in secretString so it never leaks through wrapper formatting.
	DraftAPIKeyEnv string
	DraftAPIKey    secretString

	// DraftPreset is the preset the new agent will use — either a copy of
	// whatever the picker's cursor currently points at, or (when
	// DraftPresetDirty is true) the user's own edited copy. Re-resolved
	// fresh from the cursor every time the Review step is (re-)entered
	// (see enterReviewStep), so navigating away from an edited preset to a
	// different, unedited one before Review correctly drops the stale
	// edit rather than silently finalizing it.
	//
	// DraftPresetDirty gates a REAL disk write, and that write happens
	// strictly AFTER the atomic rename, not during staging: preset.Save
	// only runs in the finalizer's post-commit phase (runPostCommit,
	// PhasePostCommitConfig), once the project is already valid and
	// published. This is deliberate — an earlier version called
	// preset.Save during the pre-rename staging/build phase, which left an
	// orphaned real global preset file behind if a LATER pre-rename phase
	// then failed (the project was never created, but the preset file
	// would have survived cleanup, since cleanup only ever removes the
	// staging directory). The staged init.json can safely reference this
	// preset's path before the file exists on disk — see RunProjectCreate's
	// PhaseApplyPreset comment for why that's not a validity problem.
	DraftPreset      *preset.Preset
	DraftPresetDirty bool // true once the user has actually edited/committed a preset in the wizard

	// DraftCodexTokens holds a completed Codex OAuth token bundle held
	// in memory instead of being written to codex-auth.json immediately.
	// Marshaled JSON bytes, wrapped so the raw bearer/refresh tokens never
	// print via %v/%s. Nil means "no Codex login performed in this draft".
	DraftCodexTokens secretBytes

	// AgentName/AgentDirName are the chosen orchestrator identity.
	AgentName    string
	AgentDirName string

	// AgentOpts carries the runtime configuration page's values (context
	// limit, soul delay, karma, addons, allowed presets, ...) exactly as
	// preset.AgentOpts already models them for GenerateInitJSONWithOpts.
	AgentOpts preset.AgentOpts

	// RecipeName/RecipeCustomDir select which recipe bundle to apply.
	RecipeName      string
	RecipeCustomDir string

	// ExistingKeys is a PRESENCE-ONLY mirror of FirstRunModel.existingKeys'
	// key set — it never carries real secret values, by TYPE, not just by
	// current call-site discipline. FirstRunModel.existingKeys (unexported,
	// real values) is the only place actual API keys live during a draft
	// session; it is consulted directly for prefill/masking within that
	// same live model instance and is never itself exported onto
	// ProjectDraft. This map exists only because
	// preset.AutoEnvVarName/stampAutoEnvVar need to know which api_key_env
	// NAMES are already taken (to pick the next free
	// "<PROVIDER>_<N>_API_KEY" slot) — they scan existingKeys' keys, never
	// its values, so key presence is sufficient and correct.
	//
	// keyPresence is a distinct type (not plain map[string]string)
	// specifically so a FUTURE bug that assigns a real value into this map
	// still cannot leak it through %v/%+v/%#v/error-wrapping — its values
	// are keyPresenceValue, a wrapper with the same redacting
	// String()/GoString() contract as secretString. A prior version used a
	// plain map[string]string and relied entirely on every call site
	// remembering to redact before assigning; that is exactly the kind of
	// call-site discipline that broke once already (the three firstrun.go
	// sync points used to alias the live real-value map directly).
	ExistingKeys keyPresence
}

// keyPresence is ProjectDraft.ExistingKeys' type: a map whose values can
// never format as anything but the redacted sentinel, regardless of what a
// future caller assigns into it. AutoEnvVarName/stampAutoEnvVar only range
// over the keys, so the value's content is irrelevant to their behavior —
// making the value type itself secret-safe is a strictly stronger guarantee
// than trusting every future write site to redact before assigning.
type keyPresence map[string]keyPresenceValue

// keyPresenceValue is a zero-information placeholder — it carries no
// payload at all (not even a redacted copy of anything), so there is
// nothing for %v/%#v to ever expose.
type keyPresenceValue struct{}

func (keyPresenceValue) String() string   { return "<redacted>" }
func (keyPresenceValue) GoString() string { return "<redacted>" }

// redactedKeyPresence returns the key SET of keys (values discarded
// entirely, not merely masked) as a keyPresence map — used to populate
// ProjectDraft.ExistingKeys without ever aliasing or copying real API key
// material onto the draft. Safe to call with a nil map.
func redactedKeyPresence(keys map[string]string) keyPresence {
	out := make(keyPresence, len(keys))
	for k := range keys {
		out[k] = keyPresenceValue{}
	}
	return out
}

// NewProjectDraft returns an empty draft rooted at projectRoot.
func NewProjectDraft(projectRoot string) *ProjectDraft {
	return &ProjectDraft{
		ProjectRoot:  projectRoot,
		AgentOpts:    preset.DefaultAgentOpts(),
		ExistingKeys: keyPresence{},
	}
}

// keyNames returns the key set of a keyPresence map as a plain
// map[string]string suffix-compatible shape for
// preset.AutoEnvVarName/stampAutoEnvVar, which only range over keys and
// never read values — so any placeholder value is fine here, never a real
// secret (there isn't one to put: keyPresenceValue carries no payload).
func (kp keyPresence) keyNames() map[string]string {
	out := make(map[string]string, len(kp))
	for k := range kp {
		out[k] = ""
	}
	return out
}

// secretString is a string wrapper that never reveals its value through
// default formatting (%v, %s, %+v, %#v) or accidental logging. Call Reveal()
// explicitly (and only at the point of use — e.g. writing the credential to
// disk during the finalizer's build phase) to get the underlying value.
//
// Both String() and GoString() are required: Go's fmt package only invokes
// the fmt.Stringer interface (String()) for %v/%s/%+v verbs — %#v instead
// looks for fmt.GoStringer (GoString()) and, absent that, falls back to
// printing the underlying named type's value directly (e.g.
// `tui.secretString("the actual secret")`), bypassing String() entirely.
// A prior version of this type had only String() and leaked the raw value
// through %#v — this is exactly the gap a parent review found.
type secretString string

func (secretString) String() string   { return "<redacted>" }
func (secretString) GoString() string { return `"<redacted>"` }
func (s secretString) Reveal() string { return string(s) }
func (s secretString) Empty() bool    { return s == "" }

// secretBytes is the []byte analogue of secretString, used for marshaled
// JSON token bundles (Codex OAuth) that must never appear in logs/errors.
// Same %#v/GoStringer caveat as secretString applies here.
type secretBytes []byte

func (secretBytes) String() string   { return "<redacted>" }
func (secretBytes) GoString() string { return `[]byte("<redacted>")` }
func (s secretBytes) Reveal() []byte { return []byte(s) }
func (s secretBytes) Empty() bool    { return len(s) == 0 }

// applyToConfig merges the draft's theme/language into a config.TUIConfig,
// used by the finalizer's post-commit save phase. Fields left empty in the
// draft do not override the passed-in config (so a draft that never
// touched theme/language leaves the caller's existing/default values).
func (d *ProjectDraft) applyToConfig(tc config.TUIConfig) config.TUIConfig {
	if d.Theme != "" {
		tc.Theme = d.Theme
	}
	if d.Language != "" {
		tc.Language = d.Language
	}
	return tc
}
