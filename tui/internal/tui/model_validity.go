package tui

import (
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/i18n"
)

// modelValidityStatus is the setup-flow gate on the preset editor's Save
// action. A real provider call must run for the exact current (provider,
// model, credential) tuple. Deterministic failures block Save; retryable
// provider failures may save with a warning and are re-probed later.
// See commit() in preset_editor.go.
type modelValidityStatus int

const (
	validityUnknown modelValidityStatus = iota
	validityChecking
	validityValid
	validityRetryable
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
			// Codex is handled by checkCodexModelValidityCmd, which must
			// use the selected token path and the Responses endpoint. Keep
			// this compatibility path for callers that do not have a
			// preset/global-dir context; it is not used by the editor.
			return modelValidityResultMsg{Generation: gen, Status: validityValid}
		}
		status, detail := probeLLM(provider, model, apiKey, baseURL, apiCompat)
		safeDetail := sanitizeModelValidityDetail(detail, apiKey)
		switch status {
		case probeOK:
			return modelValidityResultMsg{Generation: gen, Status: validityValid}
		case probeRateLimit, probeOverloaded:
			return modelValidityResultMsg{Generation: gen, Status: validityRetryable, Detail: probeStatusDetail(status, safeDetail)}
		default:
			return modelValidityResultMsg{Generation: gen, Status: validityInvalid, Detail: probeStatusDetail(status, safeDetail)}
		}
	}
}

// checkCodexModelValidityCmd performs the Codex-specific real-call gate. A
// token-file parse or /codex/models response is deliberately insufficient:
// the selected model must answer on the same Responses endpoint and account
// path the runtime will use. Pool presets try only their actual positive,
// valid candidates; an ineligible non-empty pool therefore fails closed.
func checkCodexModelValidityCmd(gen uint64, provider, model, baseURL, globalDir, authRef string) tea.Cmd {
	return func() tea.Msg {
		status, detail := probeCodexModel(provider, model, baseURL, globalDir, authRef)
		safeDetail := sanitizeModelValidityDetail(detail, "")
		switch status {
		case probeOK:
			return modelValidityResultMsg{Generation: gen, Status: validityValid}
		case probeRateLimit, probeOverloaded:
			return modelValidityResultMsg{Generation: gen, Status: validityRetryable, Detail: probeStatusDetail(status, safeDetail)}
		default:
			return modelValidityResultMsg{Generation: gen, Status: validityInvalid, Detail: probeStatusDetail(status, safeDetail)}
		}
	}
}

// probeStatusDetail renders a probeStatus into a short, safe-to-display
// message. Retryable provider responses keep their sanitized evidence so
// Save can explain what the live test returned.
func probeStatusDetail(status probeStatus, detail string) string {
	switch status {
	case probeAuthError:
		return i18n.T("preset_editor.model_validity_auth_error")
	case probeRateLimit:
		if detail != "" {
			return detail
		}
		return i18n.T("preset_editor.model_validity_rate_limited")
	case probeOverloaded:
		if detail != "" {
			return detail
		}
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

// sanitizeModelValidityDetail keeps provider evidence useful without ever
// surfacing the credential used by the live probe. It also normalizes
// whitespace and caps the warning so a provider cannot flood the TUI.
func sanitizeModelValidityDetail(detail, apiKey string) string {
	if apiKey != "" {
		detail = strings.ReplaceAll(detail, apiKey, "[redacted]")
	}
	detail = strings.Join(strings.Fields(detail), " ")
	const maxRunes = 240
	runes := []rune(detail)
	if len(runes) > maxRunes {
		detail = string(runes[:maxRunes]) + "…"
	}
	return detail
}
