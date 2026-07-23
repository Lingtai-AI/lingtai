package tui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// ---------------------------------------------------------------------------
// Types
// ---------------------------------------------------------------------------

type loginStatus int

const (
	loginChecking loginStatus = iota
	loginValid
	loginInvalid
	loginError
)

type loginEntry struct {
	Provider string
	Display  string // masked key or "OAuth — email"
	Status   loginStatus
	Detail   string // error detail
	IsOAuth  bool
	BaseURL  string
	Key      string // raw key or access token
	// CodexPath is the absolute on-disk token file backing a Codex OAuth
	// entry (legacy ~/.lingtai-tui/codex-auth.json or a per-account file
	// under ~/.lingtai-tui/codex-auth/). Empty for non-codex entries. Each
	// codex account is its own entry, so multiple ChatGPT accounts coexist.
	CodexPath string
	// CodexLegacy marks the entry backed by the legacy single-account file.
	CodexLegacy bool
}

type loginHealthMsg struct {
	Provider string
	Status   loginStatus
	Detail   string
	// CodexPath disambiguates codex entries, which all share Provider
	// "codex" but back distinct accounts (distinct token files). Empty for
	// non-codex entries, which are matched by Provider alone.
	CodexPath string
}

// LoginModel shows saved credentials with live health checks. It is opened
// from Setup → Credentials; /login remains a compatibility shortcut into
// the same setup subpage.
type LoginModel struct {
	entries       []loginEntry
	cursor        int
	activePreset  string
	activeModel   string
	orchDir       string
	globalDir     string
	width, height int
	setupSubpage  bool
	message       string
	reenteringKey bool
	keyInput      textarea.Model
	editingLabel  bool
	labelInput    textarea.Model
	codexLogging  bool
	// codexCancel cancels an in-flight startOAuthFlow goroutine. Set
	// when codexLogging flips to true on Enter; cleared in
	// CodexOAuthDoneMsg or by an explicit Del cancel.
	codexCancel context.CancelFunc
	// codexLoginEpoch / deleteArmedIdx: same mechanics as in
	// FirstRunModel — epoch drops stale OAuth callbacks after cancel,
	// and the armed-index gates two-press Del so a stray keypress
	// can't wipe a credential. deleteArmedIdx == -1 means no arm.
	codexLoginEpoch uint64
	deleteArmedIdx  int
	// codexSession holds the active OAuth session for manual callback submission.
	codexSession *codexOAuthSession
	// codexAuthURL is set from CodexOAuthURLMsg; shown so remote-browser
	// users can copy-open the URL on another machine.
	codexAuthURL string
	// codexChoosingMethod shows the two-path Codex login chooser before any
	// network side effect: browser OAuth for same-machine use, or device code
	// for remote/headless use.
	codexChoosingMethod bool
	codexMethodCursor   int // 0=browser OAuth, 1=device code
	codexDeviceURL      string
	codexDeviceCode     string
	// codexLoginTargetPath is the token file the in-flight Codex login will
	// write to. An empty string means "add a new account" — the destination
	// path is derived from the authenticated email after tokens arrive. A
	// non-empty path means "re-authenticate this existing account" and the
	// tokens overwrite that file.
	codexLoginTargetPath string
	// activeCodexPath is the absolute token-file path of the Codex account all
	// saved Codex presets currently agree on (derived via activeCodexAuthPath).
	// Empty when saved Codex presets disagree or none exist. Used only to mark
	// the (active) row in the list; recomputed after an apply.
	activeCodexPath string
	// messageOK styles `message` as a success (active color) rather than the
	// default error red. Set when reporting a completed set-active apply; reset
	// implicitly whenever a new message is assigned through the error paths.
	messageOK bool
	// poolWeights maps a Codex account's absolute token-file path to its pool
	// weight recorded in ~/.lingtai-tui/codex-auth-pool.json. Populated in the
	// constructor and updated in place when the user edits a weight. A path
	// ABSENT from this map is not in the pool: the row renders "not in pool"
	// (never a phantom default), matching the kernel runtime, which has no
	// entry for it and falls back to the legacy token. Load-balancing
	// membership lives entirely in the pool file — editing weights here never
	// rewrites presets.
	poolWeights map[string]int
	// poolCorrupt is true when codex-auth-pool.json exists but fails to parse.
	// The credentials view then shows a warning that pool weights can't be read
	// (so accounts render as "not in pool") and that the bad file will NOT be
	// clobbered. A missing pool file is not corrupt and sets this false.
	poolCorrupt bool
	// poolModelClassified is true when codex-auth-pool.json carries a top-level
	// `models` map (v2, classified by exact model) — presence, not size: the
	// kernel treats even an empty `{}` as classified. The kernel then
	// ignores the flat accounts list, so rows render the classified state
	// instead of flat memberships, and the +/-/0 flat edits are refused with a
	// hand-edit hint (no category editor exists yet).
	poolModelClassified bool
	// poolModelCount is the number of model categories in a classified pool
	// (meaningful only when poolModelClassified). Shown in the explanatory note.
	poolModelCount int
}

// NewSetupCredentialsModel opens the credential manager as a /setup subpage.
// The legacy /login command routes here too, so credential work lives under
// the setup surface while preserving the old shortcut.
//
// UNIFICATION CONTRACT (do not fork): credential and Codex-OAuth logic must
// NOT diverge between first-run setup and `/setup credentials`. All three
// entry points — `/setup credentials`, the legacy `/login` shortcut, and the
// first-run wizard's setup-mode Codex row (which emits
// ViewChangeMsg{View:"login"}, firstrun.go) — land on THIS constructor and the
// shared LoginModel. The first-run *wizard itself* (non-setup) manages only
// the primary/legacy account, but it reads and writes the SAME on-disk store
// (codex_auth_store.go: legacyCodexAuthPath + listCodexAccounts) and the SAME
// OAuth entrypoint (startOAuthFlow → buildAuthorizeURL). Adding a second
// account, account selection (prompt=login via codexForceLogin), and re-auth
// all live here. If you need new credential behavior, add it to this shared
// path — never re-implement it in firstrun.go. Guarded by
// TestSetupAndFirstRunShareCodexAccountStore.
func NewSetupCredentialsModel(orchDir, globalDir string) LoginModel {
	m := NewLoginModel(orchDir, globalDir)
	m.setupSubpage = true
	return m
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// codexAccountName returns a recognizable, non-secret account name so
// multiple Codex accounts can be told apart at a glance. The name prefers the
// account email; falls back to the per-account file slug; and, for the legacy
// single-account file with no stored email, uses the localized default-account
// label (matching PresetEditorModel.codexBoundAccountLabel). It never exposes
// token material — only the email/slug already derived for display. Kept in
// login.go (not codex_auth_store.go) so the store helper stays free of the i18n
// dependency.
func codexAccountName(acct codexAccount) string {
	name := strings.TrimSpace(acct.Label)
	if name != "" {
		return name
	}
	name = strings.TrimSpace(acct.Email)
	if name != "" {
		return name
	}
	if acct.Legacy {
		return i18n.T("codex.account_default")
	}
	// codexAccount.DisplayName() derives the file slug for a per-account file
	// (its own fallback for the legacy case is a plain-English "default",
	// which we deliberately override above with the localized label).
	return acct.DisplayName()
}

// codexAccountDisplay returns the credential-row display string for a Codex
// account: the fixed "OAuth" tag followed by the shared account name.
func codexAccountDisplay(acct codexAccount) string {
	return "OAuth — " + codexAccountName(acct)
}

// providerBaseURL returns the default API base URL for known providers.
func providerBaseURL(provider string) string {
	switch provider {
	case "minimax":
		return "https://api.minimaxi.com"
	case "zhipu":
		return "https://open.bigmodel.cn/api/coding/paas/v4"
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Constructor
// ---------------------------------------------------------------------------

// NewLoginModel builds a LoginModel populated from the orchestrator config
// and globally saved credentials.
func NewLoginModel(orchDir, globalDir string) LoginModel {
	m := LoginModel{
		orchDir:        orchDir,
		globalDir:      globalDir,
		deleteArmedIdx: -1,
	}

	// 1. Read orchestrator's active provider/model.
	provider, model, _, _, _, _ := readLLMConfig(orchDir)
	m.activePreset = provider
	m.activeModel = model

	// 2. Enumerate every stored Codex OAuth account — the legacy
	// single-account file plus any per-account files under codex-auth/.
	// Each becomes its own entry so multiple ChatGPT accounts coexist.
	for _, acct := range listCodexAccounts(globalDir) {
		tok, _ := readCodexTokenFile(acct.Path)
		m.entries = append(m.entries, loginEntry{
			Provider:    "codex",
			Display:     codexAccountDisplay(acct),
			Status:      loginChecking,
			IsOAuth:     true,
			BaseURL:     "https://chatgpt.com/backend-api",
			Key:         tok.AccessToken,
			CodexPath:   acct.Path,
			CodexLegacy: acct.Legacy,
		})
	}

	// 3. Read config.Keys for API-key-based providers.
	cfg, _ := config.LoadConfig(globalDir)
	for prov, key := range cfg.Keys {
		if key == "" || prov == "codex" {
			continue
		}
		base := providerBaseURL(prov)
		m.entries = append(m.entries, loginEntry{
			Provider: prov,
			Display:  maskKey(key),
			Status:   loginChecking,
			IsOAuth:  false,
			BaseURL:  base,
			Key:      key,
		})
	}

	// 4. Prepare textarea for key re-entry.
	ta := textarea.New()
	ta.SetHeight(1)
	ta.CharLimit = 512
	ta.Placeholder = "paste API key..."
	m.keyInput = ta
	li := textarea.New()
	li.SetHeight(1)
	li.CharLimit = 80
	li.Placeholder = i18n.T("login.codex_label_placeholder")
	m.labelInput = li

	// 5. Derive which Codex account is currently active (all saved Codex
	// presets agree on it) so its row can be marked. Empty when mixed/none.
	if p, ok := activeCodexAuthPath(globalDir); ok {
		m.activeCodexPath = p
	}

	// 6. Load the Codex pool weights so each account row can show its REAL pool
	// state. Keyed by absolute token-file path; an account absent from this map
	// is not in the pool and renders "not in pool" at render time. A
	// missing/broken pool file yields an empty map — every account then reads
	// as not-in-pool, which is exactly the truth.
	m.poolWeights = codexPoolWeights(globalDir)
	// A malformed (present-but-unparseable) pool file is surfaced as a warning
	// rather than silently degrading to "everything not in pool"; a missing file
	// stays quiet.
	m.poolCorrupt = codexPoolFileCorrupt(globalDir)
	// A model-classified (v2) pool replaces flat membership display entirely:
	// the kernel selects only inside the current model's category, so flat
	// weights would be a lie. Rows render the classified state instead and the
	// +/-/0 keys turn into a hand-edit hint.
	m.poolModelClassified, m.poolModelCount = codexPoolModelInfo(globalDir)

	return m
}

// ---------------------------------------------------------------------------
// Health check
// ---------------------------------------------------------------------------

func checkHealth(e loginEntry) loginHealthMsg {
	// base carries the discriminators (Provider + CodexPath) every return
	// path must echo so the Update handler can match the right entry — codex
	// entries share a Provider, so CodexPath is what distinguishes accounts.
	base := loginHealthMsg{Provider: e.Provider, CodexPath: e.CodexPath}
	mk := func(s loginStatus, detail string) loginHealthMsg {
		out := base
		out.Status = s
		out.Detail = detail
		return out
	}
	if e.BaseURL == "" || e.Key == "" {
		return mk(loginInvalid, "no endpoint")
	}

	var url string
	if e.IsOAuth {
		url = strings.TrimRight(e.BaseURL, "/") + "/codex/models?client_version=1.0.0"
	} else {
		url = strings.TrimRight(e.BaseURL, "/") + "/models"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return mk(loginError, "connection error")
	}
	req.Header.Set("Authorization", "Bearer "+e.Key)

	resp, err := client.Do(req)
	if err != nil {
		return mk(loginError, "connection error")
	}
	defer resp.Body.Close()
	io.ReadAll(io.LimitReader(resp.Body, 1024)) // drain body

	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return mk(loginValid, "")
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return mk(loginInvalid, "invalid credentials")
	default:
		return mk(loginError, fmt.Sprintf("HTTP %d", resp.StatusCode))
	}
}

// ---------------------------------------------------------------------------
// Bubble Tea interface
// ---------------------------------------------------------------------------

func (m LoginModel) Init() tea.Cmd {
	var cmds []tea.Cmd
	for _, e := range m.entries {
		entry := e // capture
		cmds = append(cmds, func() tea.Msg {
			return checkHealth(entry)
		})
	}
	return tea.Batch(cmds...)
}

func (m LoginModel) Update(msg tea.Msg) (LoginModel, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height

	case loginHealthMsg:
		for idx := range m.entries {
			if m.entries[idx].Provider != msg.Provider {
				continue
			}
			// Codex entries share a Provider but back distinct accounts;
			// match the specific token file so health lands on the right row.
			if msg.Provider == "codex" && m.entries[idx].CodexPath != msg.CodexPath {
				continue
			}
			m.entries[idx].Status = msg.Status
			m.entries[idx].Detail = msg.Detail
			break
		}

	case CodexOAuthURLMsg:
		// Non-terminal: browser listener is ready; store URL for display and keep listening.
		if msg.Epoch != m.codexLoginEpoch {
			return m, nil
		}
		m.codexAuthURL = msg.AuthURL
		return m, waitCodexOAuthMsg(m.codexSession)

	case CodexDeviceCodeMsg:
		if msg.Epoch != m.codexLoginEpoch {
			return m, nil
		}
		m.codexDeviceURL = msg.VerificationURL
		m.codexDeviceCode = msg.UserCode
		return m, waitCodexOAuthMsg(m.codexSession)

	case CodexOAuthDoneMsg:
		// Drop late callbacks from a cancelled session.
		if msg.Epoch != m.codexLoginEpoch {
			return m, nil
		}
		m.codexLogging = false
		m.codexCancel = nil
		m.codexSession = nil
		m.codexAuthURL = ""
		if msg.Err != nil {
			if errors.Is(msg.Err, ErrCodexAuthCancelled) {
				m.message = i18n.T("login.codex_cancelled")
			} else {
				m.message = "OAuth error: " + msg.Err.Error()
			}
			return m, nil
		}
		if msg.Tokens == nil {
			m.message = "OAuth returned no tokens"
			return m, nil
		}

		// Resolve the destination token file. A re-auth targets the
		// existing account's file (codexLoginTargetPath set when the user
		// pressed `r` on that account); an "add account" leaves the target
		// empty so we derive a fresh per-account path from the email — unless
		// no account exists yet, in which case the first account seeds the
		// legacy file so existing presets keep working without churn.
		target := m.codexLoginTargetPath
		legacy := filepath.Join(m.globalDir, legacyCodexAuthFile)
		if target == "" {
			if !fileExists(legacy) {
				target = legacy
			} else {
				target = newCodexAuthPath(m.globalDir, msg.Tokens.Email)
			}
		}
		m.codexLoginTargetPath = ""

		// Re-auth should refresh token material without discarding an existing
		// user label. New OAuth responses normally do not carry the local-only
		// label, so preserve it when the target file already exists.
		if strings.TrimSpace(msg.Tokens.Label) == "" && fileExists(target) {
			if existing, _ := readCodexTokenFile(target); strings.TrimSpace(existing.Label) != "" {
				msg.Tokens.Label = existing.Label
			}
		}

		// Token material is secret: written 0600, never logged.
		data, err := json.MarshalIndent(msg.Tokens, "", "  ")
		if err != nil {
			m.message = "failed to marshal tokens: " + err.Error()
			return m, nil
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			m.message = "failed to create credential dir: " + err.Error()
			return m, nil
		}
		if err := os.WriteFile(target, data, 0o600); err != nil {
			m.message = "failed to save Codex credential: " + err.Error()
			return m, nil
		}

		// Update the matching account entry by path, or add a new one. Build the
		// display name through the shared helper so a re-auth/add keeps the same
		// recognizable label the constructor renders (email → file slug →
		// localized default) instead of collapsing to a bare "OAuth" when the
		// fresh tokens carry no email.
		isLegacy := target == legacy
		display := codexAccountDisplay(codexAccount{
			Path:   target,
			Label:  msg.Tokens.Label,
			Email:  msg.Tokens.Email,
			Legacy: isLegacy,
		})
		found := false
		for idx := range m.entries {
			if m.entries[idx].Provider == "codex" && m.entries[idx].CodexPath == target {
				m.entries[idx].Display = display
				m.entries[idx].Key = msg.Tokens.AccessToken
				m.entries[idx].Status = loginChecking
				m.entries[idx].Detail = ""
				found = true
				break
			}
		}
		if !found {
			m.entries = append(m.entries, loginEntry{
				Provider:    "codex",
				Display:     display,
				Status:      loginChecking,
				IsOAuth:     true,
				BaseURL:     "https://chatgpt.com/backend-api",
				Key:         msg.Tokens.AccessToken,
				CodexPath:   target,
				CodexLegacy: isLegacy,
			})
		}
		m.message = m.codexOAuthImpactMessage()
		m.messageOK = true

		// Re-run health check for the affected account.
		for idx := range m.entries {
			if m.entries[idx].Provider == "codex" && m.entries[idx].CodexPath == target {
				e := m.entries[idx]
				return m, func() tea.Msg { return checkHealth(e) }
			}
		}

	case tea.PasteMsg:
		if m.editingLabel {
			var cmd tea.Cmd
			m.labelInput, cmd = m.labelInput.Update(msg)
			return m, cmd
		}
		if m.reenteringKey {
			var cmd tea.Cmd
			m.keyInput, cmd = m.keyInput.Update(msg)
			return m, cmd
		}

	case tea.KeyPressMsg:
		if m.editingLabel {
			return m.updateLabelEdit(msg)
		}
		if m.reenteringKey {
			return m.updateKeyReentry(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
}

// codexOAuthImpactMessage explains the shared-credential scope after OAuth.
// Saving OAuth never changes a preset or the active provider; the user must
// explicitly create/activate a Codex preset and refresh the running agent.
func (m LoginModel) codexOAuthImpactMessage() string {
	all, _ := preset.List()
	affected := 0
	for _, p := range all {
		llm, _ := p.Manifest["llm"].(map[string]interface{})
		provider, _ := llm["provider"].(string)
		if provider == "codex" || provider == "codex_oauth" || provider == "codex-pool" || provider == "codex_pool" {
			affected++
		}
	}
	if affected == 0 {
		return i18n.T("login.codex_oauth_saved_no_preset")
	}
	active := m.activePreset
	if active == "" {
		active = i18n.T("login.codex_oauth_no_active")
	}
	if m.activeModel != "" {
		active += " (" + m.activeModel + ")"
	}
	if m.activePreset != "codex" && m.activePreset != "codex_oauth" && m.activePreset != "codex-pool" && m.activePreset != "codex_pool" {
		return i18n.TF("login.codex_oauth_saved_inactive", affected, active)
	}
	return i18n.TF("login.codex_oauth_saved_active", affected, active)
}

func (m LoginModel) startCodexLogin(deviceCode bool) (LoginModel, tea.Cmd) {
	m.codexChoosingMethod = false
	m.codexLogging = true
	m.codexAuthURL = ""
	m.codexDeviceURL = ""
	m.codexDeviceCode = ""
	m.codexLoginEpoch++
	epoch := m.codexLoginEpoch
	ctx, cancel := context.WithCancel(context.Background())
	m.codexCancel = cancel
	if deviceCode {
		// Device-code login always shows the OpenAI login page on another
		// device, so there is no browser session to force past — the
		// forceLogin flag only applies to the browser path.
		m.codexSession = startDeviceAuthFlow(ctx, epoch)
	} else {
		m.codexSession = startOAuthFlow(ctx, epoch, m.codexForceLogin())
	}
	return m, waitCodexOAuthMsg(m.codexSession)
}

// setActiveCodexAccount makes the given Codex OAuth entry the active account and
// applies it to every saved Codex preset (rewriting their codex_auth_path). This
// is the common action — the user picks the ChatGPT account all their Codex work
// should run on. An INVALID account (token file unparseable / no refresh_token)
// is refused with a hint so a dead account can't be bound everywhere. The status
// message reports how many saved presets were bound (or that there were none).
// activeCodexPath is recomputed so the (active) marker tracks the new binding.
func (m LoginModel) setActiveCodexAccount(entry loginEntry) LoginModel {
	// Gate on validity: prefer the live health result, but a freshly-listed
	// account may still be loginChecking — fall back to parsing the token file.
	valid := entry.Status == loginValid
	if entry.Status == loginChecking {
		valid = codexAuthPathValid(entry.CodexPath)
	}
	if !valid {
		m.message = i18n.T("login.codex_invalid_account")
		m.messageOK = false
		return m
	}

	applied, err := applyActiveCodexAccount(m.globalDir, entry.CodexPath)
	if err != nil {
		m.message = fmt.Sprintf(i18n.T("login.codex_apply_failed"), err.Error())
		m.messageOK = false
		return m
	}
	if applied == 0 {
		m.message = i18n.T("login.codex_no_saved_presets")
	} else {
		m.message = fmt.Sprintf(i18n.T("login.codex_set_active"), applied)
	}
	// Neither outcome is an error; style both as confirmation feedback.
	m.messageOK = true
	// Recompute the active account from the now-rewritten saved presets so the
	// (active) marker reflects this selection immediately.
	if p, ok := activeCodexAuthPath(m.globalDir); ok {
		m.activeCodexPath = p
	} else {
		m.activeCodexPath = ""
	}
	return m
}

// codexEntryPoolPath returns the absolute token-file path a Codex entry's pool
// weight is keyed on. Falls back to the legacy file when CodexPath is empty
// (older entries), matching the delete path's resolution.
func (m *LoginModel) codexEntryPoolPath(entry loginEntry) string {
	if entry.CodexPath != "" {
		return entry.CodexPath
	}
	return legacyCodexAuthPath(m.globalDir)
}

// codexEntryMembership reports a Codex entry's real pool state: inPool is true
// only when the pool file records the account, and weight is its stored weight.
// The UI renders "not in pool" when inPool is false — never a phantom default —
// so the display matches the kernel runtime, which has no entry for such an
// account either.
func (m *LoginModel) codexEntryMembership(entry loginEntry) (inPool bool, weight int) {
	return codexPoolMembership(m.poolWeights, m.codexEntryPoolPath(entry))
}

// codexPoolAction names the three pool-weight keys so adjustCodexPoolWeight can
// apply the correct membership-aware semantics for each.
type codexPoolAction int

const (
	// poolIncrement (+/=): add an absent account at weight 1, or bump a present
	// account's weight by 1.
	poolIncrement codexPoolAction = iota
	// poolDecrement (-): reduce a present account's weight by 1 (clamped at 0).
	// An absent account stays OUT of the pool — decrement never adds it.
	poolDecrement
	// poolDisable (0): write an explicit weight-0 entry. This ADDS an absent
	// account to the pool as disabled (distinct from "not in pool"), matching
	// the existing "0 = disable" affordance.
	poolDisable
)

// adjustCodexPoolWeight applies a pool-weight key to the selected Codex account
// with membership-aware semantics (see codexPoolAction), persists the pool file
// (lazily created on the first write), and updates the in-memory weight map so
// the row re-renders immediately. Non-Codex rows and the virtual add row are
// ignored. This never touches presets: pool membership is the pool file's job,
// not the active-account binding.
func (m LoginModel) adjustCodexPoolWeight(action codexPoolAction) LoginModel {
	if m.cursor < 0 || m.cursor >= len(m.entries) || m.cursorOnVirtualRow() {
		return m
	}
	entry := m.entries[m.cursor]
	if !entry.IsOAuth {
		return m
	}
	if m.poolModelClassified {
		// Flat weight edits are meaningless on a model-classified pool (the
		// kernel ignores the accounts list) and rewriting the file could
		// destroy the hand-authored classification. Explain instead of
		// writing; setCodexPoolWeight refuses too, as the backstop.
		m.message = fmt.Sprintf(i18n.T("login.codex_pool_model_classified_note"), m.poolModelCount)
		m.messageOK = false
		return m
	}
	absPath := m.codexEntryPoolPath(entry)
	inPool, cur := m.codexEntryMembership(entry)

	newWeight := cur
	switch action {
	case poolIncrement:
		if !inPool {
			// Absent → join the pool at weight 1 (the "start balancing onto
			// this account" affordance). A present account bumps by 1.
			newWeight = 1
		} else {
			newWeight = cur + 1
		}
	case poolDecrement:
		if !inPool {
			// Absent stays absent — decrement must not silently enroll an
			// account the user hasn't opted into. Nothing to persist.
			m.message = i18n.T("login.codex_pool_absent_decrement")
			m.messageOK = false
			return m
		}
		newWeight = cur - 1
		if newWeight < 0 {
			newWeight = 0
		}
	case poolDisable:
		newWeight = 0
	}

	if err := setCodexPoolWeight(m.globalDir, absPath, newWeight); err != nil {
		m.message = fmt.Sprintf(i18n.T("login.codex_pool_save_failed"), err.Error())
		m.messageOK = false
		return m
	}
	if m.poolWeights == nil {
		m.poolWeights = map[string]int{}
	}
	m.poolWeights[absPath] = newWeight
	m.message = fmt.Sprintf(i18n.T("login.codex_pool_weight_set"), newWeight)
	m.messageOK = true
	return m
}

// codexForceLogin reports whether the in-flight browser OAuth must force the
// OpenAI login page (prompt=login) instead of reusing the existing ChatGPT
// session. True only when ADDING a new account (empty codexLoginTargetPath)
// while at least one Codex account already exists — that is the "Add another
// Codex account" path, where reusing the session would silently re-add the
// account already signed in. Re-authenticating an existing account (target
// set) and seeding the very first account (no existing entry) keep the
// default session-reuse behavior.
func (m *LoginModel) codexForceLogin() bool {
	return m.codexLoginTargetPath == "" && m.hasCodexOAuth()
}

func (m *LoginModel) entryByProvider(provider string) *loginEntry {
	for idx := range m.entries {
		if m.entries[idx].Provider == provider {
			return &m.entries[idx]
		}
	}
	return nil
}

// hasCodexOAuth returns true if the entry list already contains a Codex OAuth entry.
func (m *LoginModel) hasCodexOAuth() bool {
	return m.entryByProvider("codex") != nil
}

// virtualAddCodexRow returns true when the "Add Codex OAuth" virtual row
// should appear. It is always shown so the user can always add another
// ChatGPT account: with no account it adds the first credential; with one
// or more it adds an additional account (a separate token file under
// codex-auth/). Re-authenticating an existing account is a separate action
// (`r` on that account's entry). Previously this row hid itself once a
// Codex entry existed AND only one token file could exist; both limits are
// removed — a Codex login must always be reachable and accounts coexist.
func (m *LoginModel) virtualAddCodexRow() bool {
	return true
}

// cursorMax returns the maximum valid cursor index (entries + virtual row if present).
func (m *LoginModel) cursorMax() int {
	n := len(m.entries)
	if m.virtualAddCodexRow() {
		n++ // virtual "Add Codex OAuth" row
	}
	if n == 0 {
		return 0
	}
	return n - 1
}

// cursorOnVirtualRow returns true when the cursor sits on the virtual add-Codex row.
func (m *LoginModel) cursorOnVirtualRow() bool {
	return m.virtualAddCodexRow() && m.cursor == len(m.entries)
}

func (m LoginModel) updateNormal(msg tea.KeyPressMsg) (LoginModel, tea.Cmd) {
	if m.codexChoosingMethod {
		switch msg.String() {
		case "up", "k", "down", "j", "tab":
			if m.codexMethodCursor == 0 {
				m.codexMethodCursor = 1
			} else {
				m.codexMethodCursor = 0
			}
			return m, nil
		case "enter":
			return m.startCodexLogin(m.codexMethodCursor == 1)
		case "esc":
			m.codexChoosingMethod = false
			m.message = ""
			return m, nil
		}
	}

	// Any key other than a second delete trigger disarms the two-press
	// confirmation. "d", "delete", and "backspace" all arm/confirm; everything
	// else (including "r", now re-auth) clears the arm. Up/Down still need to
	// clear so cursor movement invalidates a stale arm.
	key := msg.String()
	// A fresh key interaction: drop the success styling from any prior status
	// so a stale "set active" green never bleeds onto a later error message.
	m.messageOK = false
	if m.deleteArmedIdx != -1 && key != "d" && key != "delete" && key != "backspace" {
		m.deleteArmedIdx = -1
		m.message = ""
	}

	switch key {
	case "esc":
		// Esc while a Codex login is mid-flight cancels that login and
		// stays on the credentials screen — it does NOT exit to home.
		// Returning to the credentials list (rather than mail) is the
		// fix for the reported UX bug where Esc in the OAuth flow dumped
		// the user back to the home page instead of the screen they came
		// from. Tearing down the goroutine releases the bound listener and
		// the epoch bump drops any late callback; every transient login
		// field is cleared so no stale spinner/URL/code lingers.
		if m.codexLogging {
			if m.codexCancel != nil {
				m.codexCancel()
				m.codexCancel = nil
			}
			m.codexLoginEpoch++
			m.codexLogging = false
			m.codexSession = nil
			m.codexAuthURL = ""
			m.codexDeviceURL = ""
			m.codexDeviceCode = ""
			m.message = i18n.T("login.codex_cancelled")
			return m, nil
		}
		// Idle Esc should back out one navigation level. The shared
		// credentials model is a /setup subpage, so return there instead
		// of dumping a user who entered from /setup back to chat. A bare
		// LoginModel keeps the historical mail fallback for tests/compat.
		backView := "mail"
		if m.setupSubpage {
			backView = "setup"
		}
		return m, func() tea.Msg { return ViewChangeMsg{View: backView} }
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < m.cursorMax() {
			m.cursor++
		}
	case "enter":
		// Virtual "Add Codex OAuth" row — open method chooser without network.
		if m.cursorOnVirtualRow() {
			// Add a NEW account: empty target → completion handler derives a
			// fresh per-account path (or seeds legacy when none exists yet).
			m.codexLoginTargetPath = ""
			m.codexChoosingMethod = true
			m.codexMethodCursor = 0
			m.message = ""
			return m, nil
		}
		if m.cursor >= 0 && m.cursor < len(m.entries) {
			entry := m.entries[m.cursor]
			if entry.IsOAuth {
				// Enter SETS THIS ACCOUNT ACTIVE and applies it to all saved
				// Codex presets (the common action). Re-auth moved to [r].
				return m.setActiveCodexAccount(entry), nil
			}
			// API key entry — show re-entry textarea.
			m.reenteringKey = true
			m.keyInput.Reset()
			m.keyInput.Focus()
			return m, nil
		}
	case "r":
		// Re-authenticate the selected Codex account: re-run OAuth and
		// overwrite that account's own token file. This is the action Enter
		// used to perform; Enter now sets the account active instead. A non-
		// OAuth (API-key) entry or the virtual add row ignore `r`.
		if m.cursor >= 0 && m.cursor < len(m.entries) && !m.cursorOnVirtualRow() {
			entry := m.entries[m.cursor]
			if entry.IsOAuth {
				m.codexLoginTargetPath = entry.CodexPath
				m.codexChoosingMethod = true
				m.codexMethodCursor = 0
				m.message = ""
			}
		}
		return m, nil
	case "l":
		if m.cursor >= 0 && m.cursor < len(m.entries) && !m.cursorOnVirtualRow() {
			entry := m.entries[m.cursor]
			if entry.Provider == "codex" && entry.IsOAuth {
				tok, _ := readCodexTokenFile(entry.CodexPath)
				m.editingLabel = true
				m.labelInput.Reset()
				m.labelInput.SetValue(tok.Label)
				m.labelInput.Focus()
				m.message = ""
			}
		}
		return m, nil
	case "+", "=":
		// Add an absent account to the pool at weight 1, or increase a present
		// account's load-balancing share. "=" is the unshifted "+" on most
		// layouts, so both map here. No preset is touched — this edits
		// ~/.lingtai-tui/codex-auth-pool.json.
		return m.adjustCodexPoolWeight(poolIncrement), nil
	case "-", "_":
		// Decrease a present account's pool weight, clamped at 0 (disabled).
		// An account NOT in the pool stays out. "_" is the shifted "-".
		return m.adjustCodexPoolWeight(poolDecrement), nil
	case "0":
		// Disable the selected Codex account in the pool (explicit weight 0)
		// without removing its credential.
		return m.adjustCodexPoolWeight(poolDisable), nil
	case "d", "delete", "backspace":
		// Remove credential. For an in-flight OAuth, Del cancels the
		// flow (matching the firstrun behavior). For a stored entry,
		// two presses are required to confirm. `d` joins Del/Backspace as a
		// letter-key logout shortcut; `r` is now re-auth, not delete.
		if m.codexLogging && m.codexCancel != nil {
			m.codexCancel()
			m.codexCancel = nil
			m.codexLoginEpoch++
			m.codexLogging = false
			m.codexSession = nil
			m.codexAuthURL = ""
			m.codexDeviceURL = ""
			m.codexDeviceCode = ""
			m.message = i18n.T("login.codex_cancelled")
			return m, nil
		}
		if m.cursor < 0 || m.cursor >= len(m.entries) || m.cursorOnVirtualRow() {
			return m, nil
		}
		if m.deleteArmedIdx != m.cursor {
			m.deleteArmedIdx = m.cursor
			m.message = i18n.T("login.delete_confirm")
			return m, nil
		}
		// Second press — actually delete.
		m.deleteArmedIdx = -1
		entry := m.entries[m.cursor]
		if entry.IsOAuth {
			// Remove just this account's token file. CodexPath is the
			// specific legacy or per-account file backing the entry, so
			// deleting one account never touches another.
			authPath := entry.CodexPath
			if authPath == "" {
				authPath = filepath.Join(m.globalDir, legacyCodexAuthFile)
			}
			if err := os.Remove(authPath); err != nil && !os.IsNotExist(err) {
				m.message = "failed to remove credential: " + err.Error()
				return m, nil
			}
		} else {
			cfg, err := config.LoadConfig(m.globalDir)
			if err != nil {
				m.message = "failed to load config: " + err.Error()
				return m, nil
			}
			if cfg.Keys != nil {
				delete(cfg.Keys, entry.Provider)
			}
			if err := config.SaveConfig(m.globalDir, cfg); err != nil {
				m.message = "failed to save config: " + err.Error()
				return m, nil
			}
		}
		// Remove from the in-memory slice and clamp cursor.
		m.entries = append(m.entries[:m.cursor], m.entries[m.cursor+1:]...)
		if m.cursor >= len(m.entries) {
			m.cursor = len(m.entries) - 1
		}
		if m.cursor < 0 {
			m.cursor = 0
		}
		m.message = i18n.T("login.deleted")
		return m, nil
	}
	return m, nil
}

func (m LoginModel) updateKeyReentry(msg tea.KeyPressMsg) (LoginModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.reenteringKey = false
		m.keyInput.Blur()
		return m, nil
	case "enter":
		newKey := strings.TrimSpace(m.keyInput.Value())
		if newKey == "" {
			m.reenteringKey = false
			m.keyInput.Blur()
			return m, nil
		}
		m.reenteringKey = false
		m.keyInput.Blur()

		// Save key to config.
		cfg, err := config.LoadConfig(m.globalDir)
		if err != nil {
			m.message = "failed to load config: " + err.Error()
			return m, nil
		}
		if cfg.Keys == nil {
			cfg.Keys = make(map[string]string)
		}
		provider := m.entries[m.cursor].Provider
		cfg.Keys[provider] = newKey
		if err := config.SaveConfig(m.globalDir, cfg); err != nil {
			m.message = "failed to save config: " + err.Error()
			return m, nil
		}

		// Update entry.
		m.entries[m.cursor].Key = newKey
		m.entries[m.cursor].Display = maskKey(newKey)
		m.entries[m.cursor].Status = loginChecking
		m.entries[m.cursor].Detail = ""

		// Fire health check.
		entry := m.entries[m.cursor]
		return m, func() tea.Msg { return checkHealth(entry) }
	default:
		var cmd tea.Cmd
		m.keyInput, cmd = m.keyInput.Update(msg)
		return m, cmd
	}
}

func (m LoginModel) updateLabelEdit(msg tea.KeyPressMsg) (LoginModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.editingLabel = false
		m.labelInput.Blur()
		return m, nil
	case "enter":
		if m.cursor < 0 || m.cursor >= len(m.entries) {
			m.editingLabel = false
			m.labelInput.Blur()
			return m, nil
		}
		entry := m.entries[m.cursor]
		label := strings.TrimSpace(m.labelInput.Value())
		tokens, err := saveCodexCredentialLabel(entry.CodexPath, label)
		m.editingLabel = false
		m.labelInput.Blur()
		if err != nil {
			m.message = fmt.Sprintf(i18n.T("login.codex_label_save_failed"), err.Error())
			m.messageOK = false
			return m, nil
		}
		m.entries[m.cursor].Display = codexAccountDisplay(codexAccount{
			Path:   entry.CodexPath,
			Label:  tokens.Label,
			Email:  tokens.Email,
			Legacy: entry.CodexLegacy,
		})
		if label == "" {
			m.message = i18n.T("login.codex_label_cleared")
		} else {
			m.message = i18n.T("login.codex_label_saved")
		}
		m.messageOK = true
		return m, nil
	default:
		var cmd tea.Cmd
		m.labelInput, cmd = m.labelInput.Update(msg)
		return m, cmd
	}
}

// ---------------------------------------------------------------------------
// View
// ---------------------------------------------------------------------------

func (m LoginModel) View() string {
	var b strings.Builder

	// Title bar: app.title • setup.credentials   [esc] back
	titleKey := "login.title"
	if m.setupSubpage {
		titleKey = "login.setup_title"
	}
	title := StyleTitle.Render(i18n.T("app.title")) + " " +
		StyleAccent.Render(RuneBullet) + " " +
		StyleTitle.Render(i18n.T(titleKey))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("manage.back"))
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(escHint) - 1
	if padding > 0 {
		b.WriteString(title + strings.Repeat(" ", padding) + escHint + "\n")
	} else {
		b.WriteString(title + "  " + escHint + "\n")
	}
	b.WriteString(strings.Repeat("─", m.width) + "\n\n")
	if m.setupSubpage {
		b.WriteString("  " + StyleFaint.Render(i18n.T("login.setup_note")) + "\n\n")
	}

	// Active provider line.
	if m.activePreset != "" {
		active := fmt.Sprintf("  Active: %s", m.activePreset)
		if m.activeModel != "" {
			active += fmt.Sprintf(" (%s)", m.activeModel)
		}
		b.WriteString(active + "\n\n")
	}

	// Entries.
	if len(m.entries) == 0 {
		b.WriteString("  " + StyleFaint.Render(i18n.T("login.no_credentials")) + "\n\n")
	} else {
		b.WriteString("  Saved credentials:\n\n")
		for idx, entry := range m.entries {
			cursor := "  "
			if idx == m.cursor {
				cursor = StyleAccent.Render("> ")
			}

			// Status icon.
			var icon string
			switch entry.Status {
			case loginChecking:
				icon = StyleSubtle.Render("⋯")
			case loginValid:
				icon = lipgloss.NewStyle().Foreground(ColorActive).Render("✓")
			case loginInvalid:
				icon = lipgloss.NewStyle().Foreground(ColorSuspended).Render("✗")
			case loginError:
				icon = lipgloss.NewStyle().Foreground(ColorStuck).Render("✗")
			}

			// Provider name padded to 10 chars.
			name := entry.Provider
			if len(name) < 10 {
				name += strings.Repeat(" ", 10-len(name))
			}

			line := fmt.Sprintf("%s %s %s %s", cursor, icon, name, entry.Display)
			// Mark the Codex account all saved Codex presets currently point at.
			if entry.Provider == "codex" && m.activeCodexPath != "" && entry.CodexPath == m.activeCodexPath {
				line += " " + lipgloss.NewStyle().Foreground(ColorActive).Render(i18n.T("login.codex_active_marker"))
			}
			// Show each Codex OAuth row's REAL pool state, keeping the display
			// honest about whether load balancing is active for it:
			//   - not recorded in the pool file → "not in pool" (the kernel
			//     runtime has no entry either and falls back to the legacy
			//     token, so we must not imply balancing is on);
			//   - recorded at weight 0          → "pool disabled";
			//   - recorded at weight N          → "pool weight: N".
			// The pool file lives at ~/.lingtai-tui/codex-auth-pool.json and is
			// edited with +/-/0 on the row; it is independent of the (active)
			// single-account binding above, which drives the plain codex preset.
			if entry.Provider == "codex" && entry.IsOAuth {
				var poolLabel string
				if m.poolModelClassified {
					// A model-classified (v2) pool has no flat per-account
					// state: the kernel picks inside the current model's
					// category, so any flat membership label would be a lie.
					// Say what the pool actually is.
					poolLabel = i18n.T("login.codex_pool_model_classified")
				} else {
					inPool, weight := m.codexEntryMembership(entry)
					switch {
					case !inPool:
						poolLabel = i18n.T("login.codex_pool_not_member")
					case weight <= 0:
						poolLabel = i18n.T("login.codex_pool_disabled")
					default:
						poolLabel = fmt.Sprintf(i18n.T("login.codex_pool_weight"), weight)
					}
				}
				line += " " + StyleFaint.Render(poolLabel)
			}
			if entry.Detail != "" {
				var detailStyle lipgloss.Style
				switch entry.Status {
				case loginInvalid:
					detailStyle = lipgloss.NewStyle().Foreground(ColorSuspended)
				case loginError:
					detailStyle = lipgloss.NewStyle().Foreground(ColorStuck)
				default:
					detailStyle = lipgloss.NewStyle().Foreground(ColorStuck)
				}
				line += "  " + detailStyle.Render("("+entry.Detail+")")
			}
			b.WriteString(line + "\n")
		}
	}

	// A malformed pool file is surfaced explicitly: without this, every account
	// silently reads as "not in pool" and the user has no idea their weights are
	// being ignored. The message also reassures that the bad file is preserved,
	// not overwritten. Rendered in the alert color and independent of the
	// hasCodexOAuth() gate so the warning shows even if no account row is present.
	if m.poolCorrupt {
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(i18n.T("login.codex_pool_corrupt")) + "\n")
	}

	// One-line explanation tying the two Codex affordances to their presets, so
	// users don't confuse the (active) single-account binding with pool weights.
	// Shown only when a Codex OAuth account exists (the only rows that carry
	// either affordance). Kept short to avoid footer/note bloat. On a
	// model-classified pool the flat-weight explanation is replaced by the
	// classified-pool note (category count + hand-edit hint), since the flat
	// affordance it describes is disabled.
	if m.hasCodexOAuth() {
		explain := i18n.T("login.codex_pool_explain")
		if m.poolModelClassified {
			explain = fmt.Sprintf(i18n.T("login.codex_pool_model_classified_note"), m.poolModelCount)
		}
		b.WriteString("\n  " + StyleFaint.Render(explain) + "\n")
	}

	// Virtual Codex OAuth row — always shown so a Codex login is always
	// reachable. It ADDS a new account: with no account it reads "Add Codex
	// OAuth"; with one or more it reads "Add another Codex account". To
	// re-authenticate an existing account the user presses `r` on that
	// account's own entry above (which targets that account's token file).
	if m.virtualAddCodexRow() {
		rowCursor := "  "
		if m.cursorOnVirtualRow() {
			rowCursor = StyleAccent.Render("> ")
		}
		rowKey := "login.codex_add_row"
		if m.hasCodexOAuth() {
			rowKey = "login.codex_add_another_row"
		}
		b.WriteString(rowCursor + lipgloss.NewStyle().Bold(true).Foreground(ColorAgent).Render(i18n.T(rowKey)) + "\n")
	}

	// Key re-entry area.
	if m.reenteringKey && m.cursor >= 0 && m.cursor < len(m.entries) {
		b.WriteString("\n  Enter new API key for " + m.entries[m.cursor].Provider + ":\n")
		b.WriteString("  " + m.keyInput.View() + "\n")
		b.WriteString("  " + StyleFaint.Render("[Enter] save  [Esc] cancel") + "\n")
	}

	if m.editingLabel && m.cursor >= 0 && m.cursor < len(m.entries) {
		b.WriteString("\n  " + fmt.Sprintf(i18n.T("login.codex_label_prompt"), m.entries[m.cursor].Display) + "\n")
		b.WriteString("  " + m.labelInput.View() + "\n")
		b.WriteString("  " + StyleFaint.Render(i18n.T("login.codex_label_hint")) + "\n")
	}

	// Codex login method chooser.
	if m.codexChoosingMethod {
		b.WriteString("\n  " + StyleAccent.Render(i18n.T("codex.method_title")) + "\n")
		labels := []string{i18n.T("codex.method_browser"), i18n.T("codex.method_device")}
		details := []string{i18n.T("codex.method_browser_detail"), i18n.T("codex.method_device_detail")}
		for i := range labels {
			cursor := "    "
			if i == m.codexMethodCursor {
				cursor = StyleAccent.Render("  > ")
			}
			b.WriteString(cursor + labels[i] + "\n")
			b.WriteString("      " + StyleFaint.Render(details[i]) + "\n")
		}
		b.WriteString("  " + StyleFaint.Render(i18n.T("codex.method_hint")) + "\n")
	}

	// Codex logging state.
	if m.codexLogging {
		b.WriteString("\n  " + StyleAccent.Render(i18n.T("codex.logging_in")) + "\n")
		if m.codexAuthURL != "" {
			b.WriteString("  " + StyleFaint.Render(i18n.T("codex.browser_auth_url_label")) + "\n")
			b.WriteString("  " + StyleAccent.Render(m.codexAuthURL) + "\n")
		}
		if m.codexDeviceURL != "" {
			b.WriteString("  " + StyleFaint.Render(i18n.T("codex.device_auth_url_label")) + "\n")
			b.WriteString("  " + StyleAccent.Render(m.codexDeviceURL) + "\n")
			b.WriteString("  " + StyleFaint.Render(i18n.T("codex.device_code_label")) + " " + StyleAccent.Render(m.codexDeviceCode) + "\n")
			b.WriteString("  " + StyleFaint.Render(i18n.T("codex.device_waiting_hint")) + "\n")
		}
	}

	// Transient message. Success outcomes (active account applied) render in the
	// active color; errors and hints keep the alert color.
	if m.message != "" {
		msgColor := ColorStuck
		if m.messageOK {
			msgColor = ColorActive
		}
		b.WriteString("\n  " + lipgloss.NewStyle().Foreground(msgColor).Render(m.message) + "\n")
	}

	// Bottom hints. Context-sensitive: a Codex OAuth row offers set-active /
	// re-auth / remove; the virtual add row and the empty list offer the add
	// affordance. A non-Codex API-key row keeps the generic remove hint.
	b.WriteString("\n" + strings.Repeat("─", m.width) + "\n")
	var footerHint string
	switch {
	case len(m.entries) == 0:
		footerHint = i18n.T("login.codex_add_hint") + "  [Esc] back"
	case m.cursorOnVirtualRow():
		footerHint = i18n.T("login.codex_add_hint") + "  [Esc] back"
	case m.cursor >= 0 && m.cursor < len(m.entries) && m.entries[m.cursor].IsOAuth:
		// Enter sets active, r re-auths, l edits label, +/-/0 edit pool weight,
		// Del/d removes (see updateNormal). The pool-weight hint is dropped on
		// a model-classified pool, where those keys only explain themselves.
		footerHint = i18n.T("login.codex_entry_hint") + "  " + i18n.T("login.codex_pool_hint") + "  [Esc] back"
		if m.poolModelClassified {
			footerHint = i18n.T("login.codex_entry_hint") + "  [Esc] back"
		}
	default:
		footerHint = "[Enter] " + i18n.T("login.reauth") + "  [Del] " + i18n.T("login.remove_hint") + "  [Esc] back"
	}
	b.WriteString(StyleFaint.Render("  "+footerHint) + "\n")

	return b.String()
}
