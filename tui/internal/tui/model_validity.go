package tui

import (
	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// modelValidityStatus is the setup-flow gate on the preset editor's Save
// action: the exact current (provider, model, credential) tuple must
// resolve to validityValid — a real, successful provider call, not a
// non-empty-string check — before the editor will emit
// PresetEditorCommitMsg. See commit() in preset_editor.go.
type modelValidityStatus int

const (
	validityUnknown modelValidityStatus = iota
	validityChecking
	validityValid
	validityInvalid
)

// modelValidityResultMsg carries the outcome of one async validity check.
// Generation must match the editor's current modelValidityGen or the
// result is stale (the user changed provider/model/credential while the
// check was in flight) and is discarded.
type modelValidityResultMsg struct {
	Generation uint64
	Status     modelValidityStatus
	Detail     string
}

// checkModelValidityCmd issues one real provider call via doctor.go's
// probeLLM and reports the outcome tagged with gen. It never blocks the
// Bubble Tea event loop — probeLLM runs inside the returned tea.Cmd's
// closure, which Bubble Tea executes on its own goroutine.
func checkModelValidityCmd(gen uint64, provider, model, apiKey, baseURL, apiCompat string) tea.Cmd {
	return func() tea.Msg {
		if oauthProviders[provider] {
			// OAuth providers (codex/codex_oauth) authenticate via a
			// token file the kernel subprocess owns, not an API key this
			// process holds. doctor.go treats these as unprobeable from
			// here (probeOAuth), not invalid; mirror that so a codex
			// preset with a bound account is treated as valid without a
			// bogus "no key" failure.
			return modelValidityResultMsg{Generation: gen, Status: validityValid}
		}
		status, detail := probeLLM(provider, model, apiKey, baseURL, apiCompat)
		if status == probeOK {
			return modelValidityResultMsg{Generation: gen, Status: validityValid}
		}
		return modelValidityResultMsg{Generation: gen, Status: validityInvalid, Detail: probeStatusDetail(status, detail)}
	}
}

// probeStatusDetail renders a probeStatus into a short, safe-to-display
// message. probeLLM already strips the API key out of network-error
// text; this only adds a stable label per status so the UI never shows a
// raw provider error body verbatim beyond what probeLLM already limited
// and sanitized.
func probeStatusDetail(status probeStatus, detail string) string {
	switch status {
	case probeAuthError:
		return i18n.T("preset_editor.model_validity_auth_error")
	case probeRateLimit:
		return i18n.T("preset_editor.model_validity_rate_limited")
	case probeOverloaded:
		return i18n.T("preset_editor.model_validity_overloaded")
	case probeNetworkError:
		return i18n.T("preset_editor.model_validity_network_error")
	case probeNoKey:
		return i18n.T("preset_editor.model_validity_no_key")
	case probeEmptyResponse:
		return i18n.T("preset_editor.model_validity_empty_response")
	default:
		if detail == "" {
			return i18n.T("preset_editor.model_validity_unknown_error")
		}
		return detail
	}
}
