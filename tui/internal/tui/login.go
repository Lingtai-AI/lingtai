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
		display := "OAuth"
		if acct.Email != "" {
			display = "OAuth — " + acct.Email
		} else if !acct.Legacy {
			display = "OAuth — " + acct.Label()
		}
		m.entries = append(m.entries, loginEntry{
			Provider:    "codex",
			Display:     display,
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

	// 5. Derive which Codex account is currently active (all saved Codex
	// presets agree on it) so its row can be marked. Empty when mixed/none.
	if p, ok := activeCodexAuthPath(globalDir); ok {
		m.activeCodexPath = p
	}

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

	if e.IsOAuth {
		return checkCodexOAuthHealth(e, mk)
	}
	if e.BaseURL == "" || e.Key == "" {
		return mk(loginInvalid, "no endpoint")
	}

	url := strings.TrimRight(e.BaseURL, "/") + "/models"
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

// refreshCodexTokensForLoginHealth is a test seam for the /setup credentials
// Codex health check. Production uses the same refreshCodexTokens implementation
// as startup validation; tests override it so no real OAuth/token endpoint is hit.
var refreshCodexTokensForLoginHealth = refreshCodexTokens

// checkCodexOAuthHealth treats the refresh-token grant as the source of truth.
// A short-lived access_token can expire while the TUI is open; probing
// /codex/models with that stale access token makes /setup credentials claim
// "invalid credentials" even though the runtime can still refresh and run.
// Only a malformed token file or refresh-token revocation should be shown as a
// re-login condition; transient refresh failures are health errors, not proof
// that OAuth is invalid.
func checkCodexOAuthHealth(e loginEntry, mk func(loginStatus, string) loginHealthMsg) loginHealthMsg {
	if e.CodexPath == "" {
		return mk(loginInvalid, "no token file")
	}
	tokens, ok := readCodexTokenFile(e.CodexPath)
	if !ok {
		return mk(loginInvalid, "invalid credentials")
	}

	const refreshBufferSeconds = 300
	if tokens.AccessToken != "" && tokens.ExpiresAt > time.Now().Unix()+refreshBufferSeconds {
		return mk(loginValid, "")
	}

	fresh, err := refreshCodexTokensForLoginHealth(tokens.RefreshToken, tokens)
	if err != nil {
		if errors.Is(err, ErrCodexAuthRevoked) {
			return mk(loginInvalid, "invalid credentials")
		}
		return mk(loginError, "refresh unavailable")
	}
	data, err := json.MarshalIndent(fresh, "", "  ")
	if err != nil {
		return mk(loginError, "save failed")
	}
	tmpPath := e.CodexPath + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0o600); err != nil {
		return mk(loginError, "save failed")
	}
	if err := os.Rename(tmpPath, e.CodexPath); err != nil {
		os.Remove(tmpPath)
		return mk(loginError, "save failed")
	}
	return mk(loginValid, "")
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

		// Update the matching account entry by path, or add a new one.
		isLegacy := target == legacy
		display := "OAuth"
		if msg.Tokens.Email != "" {
			display = "OAuth — " + msg.Tokens.Email
		}
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

		// Re-run health check for the affected account.
		for idx := range m.entries {
			if m.entries[idx].Provider == "codex" && m.entries[idx].CodexPath == target {
				e := m.entries[idx]
				return m, func() tea.Msg { return checkHealth(e) }
			}
		}

	case tea.PasteMsg:
		if m.reenteringKey {
			var cmd tea.Cmd
			m.keyInput, cmd = m.keyInput.Update(msg)
			return m, cmd
		}

	case tea.KeyPressMsg:
		if m.reenteringKey {
			return m.updateKeyReentry(msg)
		}
		return m.updateNormal(msg)
	}

	return m, nil
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
	// account may still be loginChecking, and transient refresh/network errors
	// should not force a needless re-login if the token file still carries a
	// refresh_token. Only loginInvalid is a hard block.
	valid := entry.Status == loginValid
	if !valid && entry.Status != loginInvalid {
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
		return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
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
		// Enter sets active, r re-auths, Del/d removes (see updateNormal).
		footerHint = i18n.T("login.codex_entry_hint") + "  [Esc] back"
	default:
		footerHint = "[Enter] " + i18n.T("login.reauth") + "  [Del] " + i18n.T("login.remove_hint") + "  [Esc] back"
	}
	b.WriteString(StyleFaint.Render("  "+footerHint) + "\n")

	return b.String()
}
