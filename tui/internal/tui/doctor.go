package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"charm.land/bubbles/v2/viewport"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/migrate"
	"github.com/anthropics/lingtai-tui/internal/preset"
	"github.com/anthropics/lingtai-tui/internal/sqlitelog"
)

// tuiVersion is set once at startup by main via SetTUIVersion.
// Used by /doctor for the version-skew check.
var tuiVersion = "dev"

// SetTUIVersion records the running TUI binary version for doctor diagnostics.
func SetTUIVersion(v string) {
	if v != "" {
		tuiVersion = v
	}
}

// doctorResultMsg is sent when the async diagnostic completes.
type doctorResultMsg struct {
	Lines []doctorLine
}

type doctorLine struct {
	Text    string
	OK      bool // true = green check, false = red cross (ignored if Warn or Hint)
	Warn    bool // true = amber indicator (neutral info, e.g. version drift)
	Hint    bool // true = suggestion line (indented, dimmed)
	Section bool // true = section header (un-indented, bold, with a leading blank line)
}

// DoctorModel is the /doctor dedicated view. The diagnostic output can run far
// taller than the terminal (runtime + portal + kernel + LLM + per-agent checks),
// so the body lives in a scrollable viewport rather than a flat string dump.
type DoctorModel struct {
	orchDir   string
	globalDir string
	lines     []doctorLine
	loading   bool
	width     int
	height    int

	viewport viewport.Model
	ready    bool // viewport initialized (after first WindowSizeMsg)
}

func NewDoctorModel(orchDir, globalDir string) DoctorModel {
	return DoctorModel{orchDir: orchDir, globalDir: globalDir, loading: true}
}

// doctorHeaderLines: title row + separator + trailing blank line.
const doctorHeaderLines = 3

// doctorFooterLines: separator + hint line.
const doctorFooterLines = 2

func (m DoctorModel) Init() tea.Cmd {
	return m.runDoctorCmd()
}

// runDoctorCmd returns the command that runs the diagnostic. Shared by Init()
// (initial load) and the ctrl+r refresh handler.
func (m DoctorModel) runDoctorCmd() tea.Cmd {
	orchDir := m.orchDir
	globalDir := m.globalDir
	return func() tea.Msg {
		return runDoctor(orchDir, globalDir)
	}
}

func (m DoctorModel) Update(msg tea.Msg) (DoctorModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		vpHeight := m.height - doctorHeaderLines - doctorFooterLines
		if vpHeight < 1 {
			vpHeight = 1
		}
		if !m.ready {
			m.viewport = viewport.New()
			m.ready = true
		}
		m.viewport.SetWidth(m.width)
		m.viewport.SetHeight(vpHeight)
		m.syncViewport()

	case doctorResultMsg:
		m.lines = msg.Lines
		m.loading = false
		// A fresh diagnostic re-runs from the top; reset scroll so the user
		// reads from the first check rather than wherever they last were.
		if m.ready {
			m.viewport.GotoTop()
		}
		m.syncViewport()

	case tea.MouseWheelMsg:
		if m.ready {
			var cmd tea.Cmd
			m.viewport, cmd = m.viewport.Update(msg)
			return m, cmd
		}

	case tea.KeyPressMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return ViewChangeMsg{View: "mail"} }
		case "ctrl+r":
			// Re-run the full diagnostic from scratch.
			m.loading = true
			return m, m.runDoctorCmd()
		default:
			// Forward navigation keys (up/down/pgup/pgdn/home/end) to the
			// viewport so the diagnostic output is scrollable.
			if m.ready {
				var cmd tea.Cmd
				m.viewport, cmd = m.viewport.Update(msg)
				return m, cmd
			}
		}
	}
	return m, nil
}

// syncViewport re-renders the diagnostic body into the viewport. Called after a
// resize and whenever the line set changes so the scrollable content stays in
// step with both the data and the current width.
func (m *DoctorModel) syncViewport() {
	if !m.ready {
		return
	}
	m.viewport.SetContent(m.renderBody())
}

func (m DoctorModel) View() string {
	// Header: title row + separator + a trailing blank line (doctorHeaderLines).
	title := StyleTitle.Render(i18n.T("app.title")) + " " +
		StyleAccent.Render(RuneBullet) + " " +
		StyleTitle.Render(i18n.T("doctor.title"))
	escHint := StyleAccent.Render("[esc] ") + StyleSubtle.Render(i18n.T("manage.back"))
	padding := m.width - lipgloss.Width(title) - lipgloss.Width(escHint) - 1
	var titleRow string
	if padding > 0 {
		titleRow = title + strings.Repeat(" ", padding) + escHint
	} else {
		titleRow = title + "  " + escHint
	}
	header := titleRow + "\n" + strings.Repeat("─", m.width) + "\n"

	if m.loading {
		return header + "\n  " + i18n.T("doctor.checking") + "\n"
	}

	// Body lives in the viewport once it has been sized; before the first
	// WindowSizeMsg (e.g. a unit test calling View directly) fall back to the
	// raw body so output is still meaningful.
	body := m.renderBody()
	if m.ready {
		body = m.viewport.View()
	}

	// Footer: separator + hint line (doctorFooterLines). Surface the scroll
	// affordance only when there is more below the fold, so a short report
	// doesn't advertise a control that does nothing.
	hint := "  [esc] " + i18n.T("manage.back") + " " + RuneBullet +
		" [ctrl+r] " + i18n.T("props.ctrl_r_reload")
	if m.ready && !m.viewport.AtBottom() {
		hint += " " + RuneBullet + " ↑↓ " + i18n.T("doctor.scroll")
	}
	footer := strings.Repeat("─", m.width) + "\n" + StyleFaint.Render(hint)

	return header + "\n" + PaintViewportBG(body, m.width) + "\n" + footer
}

// renderBody formats the diagnostic lines into the scrollable region. Section
// headers are un-indented and bold with a leading blank line so the runtime /
// kernel / LLM / per-agent groups read as distinct blocks; status, warning,
// and hint lines keep the two-space indent that nests them under their section.
func (m DoctorModel) renderBody() string {
	var b strings.Builder
	for i, line := range m.lines {
		switch {
		case line.Section:
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(StyleTitle.Render(line.Text) + "\n")
		case line.Hint:
			b.WriteString("  " + StyleAccent.Render(line.Text) + "\n")
		case line.Warn:
			b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorStuck).Render(line.Text) + "\n")
		case line.OK:
			b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorAgent).Render(line.Text) + "\n")
		default:
			b.WriteString("  " + lipgloss.NewStyle().Foreground(ColorSuspended).Render(line.Text) + "\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// --- Diagnostic logic ---

// runDoctor performs the /doctor diagnostic and returns a doctorResultMsg.
func runDoctor(orchDir, globalDir string) doctorResultMsg {
	var lines []doctorLine

	// Phase -1: forced bootstrap/runtime update. Keep this first so /doctor can
	// repair stale binaries or Python packages before running the traditional
	// health checks below. Failures are surfaced but do not short-circuit the
	// rest of the diagnostic.
	lines = append(lines, doctorLine{Text: i18n.T("doctor.section_runtime"), Section: true})
	updateReport := config.RunDoctorUpdate(globalDir, config.DoctorOptions{
		CurrentTUIVersion: tuiVersion,
		ForceTUI:          true,
		ForcePython:       true,
		QuietEnsureVenv:   true,
	})
	for _, line := range updateReport.Lines {
		lines = append(lines, doctorLineFromConfig(line))
	}
	if err := preset.Bootstrap(globalDir); err != nil {
		lines = append(lines, doctorLine{
			Text: fmt.Sprintf("✗ Bootstrap assets refresh failed: %v", err),
		})
	} else {
		preset.PopulateBundledLibrary("", globalDir)
		ExportCommandsJSON(globalDir)
		lines = append(lines, doctorLine{
			Text: "✓ Bootstrap assets, utility skills, and commands.json refreshed", OK: true,
		})
	}

	// Phase 0: check lingtai-portal on PATH
	lines = append(lines, doctorLine{Text: i18n.T("doctor.section_portal"), Section: true})
	if _, err := exec.LookPath("lingtai-portal"); err == nil {
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.portal_ok"), OK: true,
		})
	} else {
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.portal_missing"),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_portal"), Hint: true,
		})
	}

	// Phase 0.5: kernel health — these are the checks that catch "TUI upgraded
	// but Python kernel is old/broken/missing", which is the most common cause
	// of a post-upgrade regression (especially for CN users hitting mirror
	// flakiness during install).
	lines = append(lines, doctorLine{Text: i18n.T("doctor.section_kernel"), Section: true})
	kernelOK := checkKernelHealth(orchDir, globalDir, &lines)
	_ = kernelOK // intentionally not short-circuiting: LLM probe is still useful info

	// Phase 1: read events.jsonl for recent errors
	lines = append(lines, doctorLine{Text: i18n.T("doctor.section_llm"), Section: true})
	lastErr := findLastAPIError(orchDir)
	if lastErr != "" {
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.last_error", lastErr),
		})
	}

	// Phase 2: read init.json to get LLM config
	provider, model, apiKey, baseURL, apiCompat, err := readLLMConfig(orchDir)
	if err != nil {
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.config_error", err),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_refresh"), Hint: true,
		})
		return doctorResultMsg{Lines: lines}
	}

	// Phase 2.5: check base_url for providers that require it
	if baseURL == "" {
		if regions, ok := preset.ProviderRegionURLs[provider]; ok && len(regions) > 0 {
			lines = append(lines, doctorLine{
				Text: i18n.TF("doctor.llm_no_base_url", provider),
			})
			lines = append(lines, doctorLine{
				Text: i18n.T("doctor.suggest_base_url"), Hint: true,
			})
		}
	}

	// Phase 3: live API check
	status, detail := probeLLM(provider, model, apiKey, baseURL, apiCompat)

	switch status {
	case probeOK:
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.llm_ok", provider, model), OK: true,
		})
		if lastErr != "" {
			lines = append(lines, doctorLine{
				Text: i18n.T("doctor.suggest_revive"), Hint: true,
			})
		} else {
			lines = append(lines, doctorLine{
				Text: i18n.T("doctor.healthy"), OK: true,
			})
		}
	case probeAuthError:
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.llm_auth", detail),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_setup"), Hint: true,
		})
	case probeRateLimit:
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.llm_rate", detail),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_wait"), Hint: true,
		})
	case probeOverloaded:
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.llm_overloaded", detail),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_wait"), Hint: true,
		})
	case probeNetworkError:
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.llm_network", detail),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_network"), Hint: true,
		})
	case probeNoKey:
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.llm_no_key"),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_setup"), Hint: true,
		})
	case probeOAuth:
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.llm_oauth"), Warn: true,
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_oauth"), Hint: true,
		})
	case probeEmptyResponse:
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.llm_empty_response", detail),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_proxy_check"), Hint: true,
		})
	default:
		lines = append(lines, doctorLine{
			Text: i18n.TF("doctor.llm_unknown", detail),
		})
		lines = append(lines, doctorLine{
			Text: i18n.T("doctor.suggest_refresh"), Hint: true,
		})
	}

	// Phase 4: delegate per-agent local state diagnostics to the kernel
	// intrinsic lingtai-doctor script. This keeps the TUI focused on framing
	// and runtime bootstrap while the kernel-owned skill carries reusable
	// agent/MCP/log/notification checks.
	lines = append(lines, doctorLine{Text: i18n.T("doctor.section_agent"), Section: true})
	lines = append(lines, runKernelDoctorIntrinsic(orchDir, globalDir)...)

	return doctorResultMsg{Lines: lines}
}

func doctorLineFromConfig(line config.DoctorLine) doctorLine {
	prefix := "•"
	converted := doctorLine{Warn: true}
	switch line.Severity {
	case config.DoctorOK:
		prefix = "✓"
		converted.OK = true
		converted.Warn = false
	case config.DoctorFail:
		prefix = "✗"
		converted.Warn = false
	case config.DoctorWarn:
		prefix = "!"
	case config.DoctorInfo:
		prefix = "•"
	}
	converted.Text = prefix + " " + line.Text
	return converted
}

// --- Event log scanning ---

type logEvent struct {
	Type  string `json:"type"`
	Error string `json:"error"`
}

// findLastAPIError scans events.jsonl for the most recent aed_attempt,
// aed_exhausted, or refresh_init_error event and returns the error string.
func findLastAPIError(orchDir string) string {
	if rows, err := sqlitelog.QueryErrorEvents(orchDir); err == nil && len(rows) > 0 {
		return rows[0].Error
	}

	logPath := filepath.Join(orchDir, "logs", "events.jsonl")
	f, err := os.Open(logPath)
	if err != nil {
		return ""
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var lastError string
	for scanner.Scan() {
		var ev logEvent
		if err := json.Unmarshal(scanner.Bytes(), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "aed_attempt", "aed_exhausted", "refresh_init_error":
			if ev.Error != "" {
				lastError = ev.Error
			}
		}
	}
	return lastError
}

// --- Init.json / env resolution ---

// readLLMConfig pulls the agent's LLM configuration from init.json. The
// apiCompat return value carries manifest.llm.api_compat ("", "openai",
// or "anthropic") — required by probeLLM to pick the right auth scheme
// when provider="custom" points at a third-party gateway. Without this,
// any anthropic-compatible custom endpoint (e.g. JoyCode's local proxy
// on 127.0.0.1:34891) gets probed with `Authorization: Bearer <key>`,
// silently falls into the 404 path that's mapped to probeOK, and masks
// the real connectivity state.
func readLLMConfig(orchDir string) (provider, model, apiKey, baseURL, apiCompat string, err error) {
	initPath := filepath.Join(orchDir, "init.json")
	data, err := os.ReadFile(initPath)
	if err != nil {
		return "", "", "", "", "", fmt.Errorf("cannot read init.json")
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return "", "", "", "", "", fmt.Errorf("invalid init.json")
	}

	manifest, _ := raw["manifest"].(map[string]interface{})
	if manifest == nil {
		return "", "", "", "", "", fmt.Errorf("no manifest in init.json")
	}

	llm, _ := manifest["llm"].(map[string]interface{})
	if llm == nil {
		return "", "", "", "", "", fmt.Errorf("no manifest.llm in init.json")
	}

	provider, _ = llm["provider"].(string)
	model, _ = llm["model"].(string)
	apiKey, _ = llm["api_key"].(string)
	baseURL, _ = llm["base_url"].(string)
	apiCompat, _ = llm["api_compat"].(string)

	if apiKey == "" {
		apiKeyEnv, _ := llm["api_key_env"].(string)
		if apiKeyEnv != "" {
			envFile, _ := raw["env_file"].(string)
			apiKey = lookupEnvKey(envFile, orchDir, apiKeyEnv)
		}
	}

	return provider, model, apiKey, baseURL, apiCompat, nil
}

// lookupEnvKey resolves an environment variable name, checking os.Environ first,
// then parsing the .env file without mutating the process environment.
func lookupEnvKey(envFile, workingDir, envVarName string) string {
	if val, ok := os.LookupEnv(envVarName); ok {
		return val
	}
	if envFile == "" {
		return ""
	}

	p := envFile
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, p[2:])
	}
	if !filepath.IsAbs(p) {
		p = filepath.Join(workingDir, p)
	}

	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		if strings.TrimSpace(key) == envVarName {
			return strings.Trim(strings.TrimSpace(val), "'\"")
		}
	}
	return ""
}

// --- Live LLM probe ---

type probeStatus int

const (
	probeOK probeStatus = iota
	probeAuthError
	probeRateLimit
	probeOverloaded
	probeNetworkError
	probeNoKey
	probeUnknown
	// probeEmptyResponse: the endpoint accepted a real POST /v1/messages
	// (HTTP 200) but returned an empty content array and zero-token usage.
	// This is the canonical failure signature of an anthropic-compatible
	// reverse proxy (JoyCode's local 127.0.0.1:34891 proxy is the example
	// motivating this status) that received the request, didn't actually
	// forward to its upstream model, and replied with a structurally-valid
	// but empty Message envelope. GET /v1/models cannot detect this — the
	// proxy may not implement /v1/models at all (404 → existing probeOK
	// charity branch), or may return a benign 200 — so we run a real
	// minimal messages call as a second-stage probe.
	probeEmptyResponse
	// probeOAuth: provider uses OAuth/session-based auth (e.g. codex /
	// codex_oauth via ChatGPT subscription), not an API key. The doctor
	// cannot probe these from this process — the CLI subprocess owns the
	// credential. Surface as a Warn-level note rather than the bogus
	// "API key not set" alarm that probeNoKey would produce.
	probeOAuth
)

// oauthProviders enumerates LLM providers whose credentials live outside
// the LingTai config (no api_key / api_key_env). The doctor cannot probe
// these directly; the spawned CLI handles its own auth (e.g. `codex`
// reads ~/.codex/auth.json from a prior `codex login`).
var oauthProviders = map[string]bool{
	"codex":       true,
	"codex_oauth": true,
}

func probeLLM(provider, model, apiKey, baseURL, apiCompat string) (probeStatus, string) {
	if oauthProviders[provider] {
		return probeOAuth, ""
	}
	if apiKey == "" {
		return probeNoKey, ""
	}

	url, headers := providerProbeConfig(provider, apiKey, baseURL, apiCompat)
	if url == "" {
		return probeUnknown, "unknown provider: " + provider
	}

	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return probeNetworkError, err.Error()
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}

	resp, err := client.Do(req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, apiKey) {
			errMsg = "connection failed"
		}
		return probeNetworkError, errMsg
	}
	defer resp.Body.Close()
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	body := string(bodyBytes)

	var gotStatus probeStatus
	var gotDetail string
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		gotStatus, gotDetail = probeOK, ""
	case resp.StatusCode == 404 || resp.StatusCode == 405:
		// /v1/models not supported but server responded — connectivity and auth OK
		gotStatus, gotDetail = probeOK, ""
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return probeAuthError, fmt.Sprintf("%d %s", resp.StatusCode, extractErrorMessage(body))
	case resp.StatusCode == 429:
		return probeRateLimit, "429 rate limited"
	case resp.StatusCode == 529 || resp.StatusCode == 503:
		return probeOverloaded, fmt.Sprintf("%d overloaded", resp.StatusCode)
	default:
		return probeUnknown, fmt.Sprintf("%d %s", resp.StatusCode, extractErrorMessage(body))
	}

	// Stage 2: real-call second-stage probe. The /v1/models check above
	// tells us only that the host is reachable and credentials were not
	// rejected outright. It cannot tell us whether the actual generation
	// path (POST /v1/messages or POST /v1/chat/completions) forwards
	// requests to a working upstream model — and that is exactly what
	// fails for reverse-proxy gateways (e.g. JoyCode at 127.0.0.1:34891,
	// or acui.shop / opencode.ai zen routes) when they reply HTTP 200
	// with an empty envelope. So when the wire protocol is identifiable
	// we send one max_tokens=1 generation and inspect the envelope. Cost
	// is negligible (<$0.0001 on every commercial provider) and the
	// diagnostic value is high — without this the doctor's first-stage
	// probeOK silently masks the very failure mode the user is here to
	// debug.
	if gotStatus == probeOK {
		switch classifyWire(provider, apiCompat) {
		case wireAnthropic:
			if msgStatus, msgDetail := probeAnthropicMessages(model, apiKey, baseURL); msgStatus != probeOK {
				return msgStatus, msgDetail
			}
		case wireOpenAI:
			if msgStatus, msgDetail := probeOpenAICompletions(model, apiKey, baseURL); msgStatus != probeOK {
				return msgStatus, msgDetail
			}
		}
	}
	return gotStatus, gotDetail
}

// wireProtocol classifies a provider/api_compat pair into the wire
// protocol used for the actual generation call. The classification is
// what determines which second-stage envelope-inspection probe runs.
type wireProtocol int

const (
	wireUnknown wireProtocol = iota
	wireAnthropic
	wireOpenAI
)

func classifyWire(provider, apiCompat string) wireProtocol {
	switch provider {
	case "anthropic", "minimax":
		return wireAnthropic
	case "openai", "openrouter", "deepseek", "kimi", "mimo", "zhipu", "codex", "nvidia":
		return wireOpenAI
	case "custom":
		switch apiCompat {
		case "anthropic":
			return wireAnthropic
		case "openai":
			return wireOpenAI
		default:
			return wireUnknown
		}
	default:
		switch apiCompat {
		case "anthropic":
			return wireAnthropic
		case "openai":
			return wireOpenAI
		default:
			return wireUnknown
		}
	}
}

// probeAnthropicMessages issues a single max_tokens=1 POST /v1/messages
// and verifies the response envelope is non-empty. Returns probeOK when
// the server returns a real Message with at least one content block (or
// non-zero output_tokens); returns probeEmptyResponse when HTTP 200 is
// paired with content:[] and zero-token usage — the canonical failure
// signature of a reverse-proxy gateway that received the call but did
// not actually forward it (JoyCode's local proxy is the motivating case).
// Any non-200 / network failure is mapped to the same status taxonomy as
// probeLLM's first-stage check so the doctor UI stays consistent.
func probeAnthropicMessages(model, apiKey, baseURL string) (probeStatus, string) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.anthropic.com"
	}
	url := base + "/v1/messages"

	probeModel := model
	if probeModel == "" {
		// Best-effort default; the gateway will surface a 400 if it dislikes
		// the model name, which is itself a useful diagnostic outcome.
		probeModel = "claude-3-5-haiku-20241022"
	}

	payload := map[string]interface{}{
		"model":      probeModel,
		"max_tokens": 1,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hi"},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return probeUnknown, err.Error()
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return probeNetworkError, err.Error()
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")
	// Some gateways (notably proxies fronting Bearer-style upstreams) will
	// also accept Authorization; sending both is harmless and improves
	// compatibility with reverse proxies that forward only one.
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, apiKey) {
			errMsg = "connection failed"
		}
		return probeNetworkError, errMsg
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	respBody := string(respBytes)

	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return probeAuthError, fmt.Sprintf("messages %d %s", resp.StatusCode, extractErrorMessage(respBody))
	case resp.StatusCode == 429:
		return probeRateLimit, "messages 429 rate limited"
	case resp.StatusCode == 529 || resp.StatusCode == 503:
		return probeOverloaded, fmt.Sprintf("messages %d overloaded", resp.StatusCode)
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// Continue to envelope inspection below.
	default:
		return probeUnknown, fmt.Sprintf("messages %d %s", resp.StatusCode, extractErrorMessage(respBody))
	}

	var env struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
		StopReason string `json:"stop_reason"`
	}
	if err := json.Unmarshal(respBytes, &env); err != nil {
		preview := respBody
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		return probeUnknown, "messages 200 but response not anthropic-shape: " + preview
	}

	hasContent := false
	for _, c := range env.Content {
		if strings.TrimSpace(c.Text) != "" || c.Type != "" {
			hasContent = true
			break
		}
	}
	usageZero := env.Usage.InputTokens == 0 && env.Usage.OutputTokens == 0

	if !hasContent && usageZero {
		// JoyCode-style failure: gateway accepted the call, replied 200,
		// but the envelope shows nothing was actually forwarded upstream.
		return probeEmptyResponse, fmt.Sprintf(
			"HTTP 200, content=[], usage 0/0 (model=%s); proxy/gateway likely did not forward to upstream",
			probeModel,
		)
	}
	return probeOK, ""
}

// probeOpenAICompletions is the OpenAI-protocol counterpart of
// probeAnthropicMessages. It POSTs a single max_tokens=1 chat completion
// to base_url + /v1/chat/completions (or just /chat/completions when the
// base_url already ends in /v1) and inspects choices[].message.content
// plus usage.completion_tokens. Returns probeEmptyResponse when the
// response is HTTP 200 but the envelope is empty — the canonical failure
// signature of OpenAI-compatible reverse proxies (acui.shop and the
// opencode.ai zen routes are the motivating cases) that 200-but-nop the
// generation. See probeAnthropicMessages for the broader rationale.
func probeOpenAICompletions(model, apiKey, baseURL string) (probeStatus, string) {
	base := strings.TrimRight(baseURL, "/")
	if base == "" {
		base = "https://api.openai.com/v1"
	}
	// Smart endpoint join: many OpenAI-compatible base_urls already end in
	// /v1, others don't. Don't double-stack.
	url := base + "/chat/completions"
	if !strings.HasSuffix(base, "/v1") && !strings.Contains(base, "/v1/") {
		url = base + "/v1/chat/completions"
	}

	probeModel := model
	if probeModel == "" {
		probeModel = "gpt-4o-mini"
	}

	payload := map[string]interface{}{
		"model": probeModel,
		"messages": []map[string]interface{}{
			{"role": "user", "content": "hi"},
		},
		"max_tokens": 1,
		"stream":     false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return probeUnknown, err.Error()
	}

	client := &http.Client{Timeout: 15 * time.Second}
	req, err := http.NewRequest("POST", url, bytes.NewReader(body))
	if err != nil {
		return probeNetworkError, err.Error()
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("Authorization", "Bearer "+apiKey)

	resp, err := client.Do(req)
	if err != nil {
		errMsg := err.Error()
		if strings.Contains(errMsg, apiKey) {
			errMsg = "connection failed"
		}
		return probeNetworkError, errMsg
	}
	defer resp.Body.Close()
	respBytes, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
	respBody := string(respBytes)

	switch {
	case resp.StatusCode == 401 || resp.StatusCode == 403:
		return probeAuthError, fmt.Sprintf("chat/completions %d %s", resp.StatusCode, extractErrorMessage(respBody))
	case resp.StatusCode == 429:
		return probeRateLimit, "chat/completions 429 rate limited"
	case resp.StatusCode == 529 || resp.StatusCode == 503:
		return probeOverloaded, fmt.Sprintf("chat/completions %d overloaded", resp.StatusCode)
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		// Continue to envelope inspection below.
	default:
		return probeUnknown, fmt.Sprintf("chat/completions %d %s", resp.StatusCode, extractErrorMessage(respBody))
	}

	var env struct {
		Choices []struct {
			Message struct {
				Content   string        `json:"content"`
				ToolCalls []interface{} `json:"tool_calls"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(respBytes, &env); err != nil {
		preview := respBody
		if len(preview) > 100 {
			preview = preview[:100] + "..."
		}
		return probeUnknown, "chat/completions 200 but response not openai-shape: " + preview
	}

	hasContent := false
	for _, c := range env.Choices {
		if strings.TrimSpace(c.Message.Content) != "" || len(c.Message.ToolCalls) > 0 {
			hasContent = true
			break
		}
	}
	usageZero := env.Usage.PromptTokens == 0 && env.Usage.CompletionTokens == 0

	if !hasContent && usageZero {
		// acui.shop / opencode.ai-style failure: proxy returned 200 but
		// nothing was actually generated upstream.
		return probeEmptyResponse, fmt.Sprintf(
			"HTTP 200, choices=[] (no content / no tool_calls), usage 0/0 (model=%s); proxy/gateway likely did not forward to upstream",
			probeModel,
		)
	}
	return probeOK, ""
}

// providerProbeConfig returns the URL and headers for a GET /v1/models
// (or provider-equivalent) probe. apiCompat carries manifest.llm.api_compat
// and is consulted whenever a provider entry is itself protocol-agnostic
// (i.e. "custom" or unknown providers backed by a user-supplied baseURL):
// the auth scheme then follows the wire protocol the user declared, not
// a hardcoded default.
//
// Without this, anthropic-compatible third-party gateways — JoyCode's
// local proxy at 127.0.0.1:34891 being a representative case — get hit
// with `Authorization: Bearer <key>`, fall through to a 404 on /v1/models
// (which the probe charitably maps to OK), and the user sees a green
// /doctor while the actual agent fails to talk to the gateway.
func providerProbeConfig(provider, apiKey, baseURL, apiCompat string) (string, map[string]string) {
	switch provider {
	case "anthropic":
		base := "https://api.anthropic.com"
		if baseURL != "" {
			base = strings.TrimRight(baseURL, "/")
		}
		return base + "/v1/models", map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}
	case "openai":
		base := "https://api.openai.com"
		if baseURL != "" {
			base = strings.TrimRight(baseURL, "/")
		}
		return base + "/v1/models", map[string]string{
			"Authorization": "Bearer " + apiKey,
		}
	case "gemini":
		return "https://generativelanguage.googleapis.com/v1beta/models", map[string]string{
			"x-goog-api-key": apiKey,
		}
	case "minimax":
		base := "https://api.minimax.io/anthropic"
		if baseURL != "" {
			base = strings.TrimRight(baseURL, "/")
		}
		return base + "/v1/models", map[string]string{
			"x-api-key":         apiKey,
			"anthropic-version": "2023-06-01",
		}
	case "zhipu":
		base := "https://open.bigmodel.cn/api/coding/paas/v4"
		if baseURL != "" {
			base = strings.TrimRight(baseURL, "/")
		}
		return base + "/models", map[string]string{
			"Authorization": "Bearer " + apiKey,
		}
	case "custom":
		if baseURL == "" {
			return "", nil
		}
		base := strings.TrimRight(baseURL, "/")
		if apiCompat == "anthropic" {
			return base + "/v1/models", map[string]string{
				"x-api-key":         apiKey,
				"anthropic-version": "2023-06-01",
			}
		}
		return base + "/v1/models", map[string]string{
			"Authorization": "Bearer " + apiKey,
		}
	default:
		if baseURL != "" {
			base := strings.TrimRight(baseURL, "/")
			if apiCompat == "anthropic" {
				return base + "/v1/models", map[string]string{
					"x-api-key":         apiKey,
					"anthropic-version": "2023-06-01",
				}
			}
			return base + "/v1/models", map[string]string{
				"Authorization": "Bearer " + apiKey,
			}
		}
		return "", nil
	}
}

// --- Kernel health checks ---

// checkKernelHealth runs K1–K6 and appends findings to lines.
// Returns true if every hard check passed. Soft warnings (version drift) do
// not affect the return value but still surface as Warn lines.
func checkKernelHealth(orchDir, globalDir string, lines *[]doctorLine) bool {
	allOK := true

	// K1. TUI binary version (always shown, informational)
	*lines = append(*lines, doctorLine{
		Text: i18n.TF("doctor.tui_version", tuiVersion), OK: true,
	})

	// K2. Which Python the TUI will use for agents.
	python := config.LingtaiCmd(globalDir)
	venvPython := config.VenvPython(config.RuntimeVenvDir(globalDir))
	usingVenv := python == venvPython
	if _, err := os.Stat(python); err != nil {
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.python_missing", python),
		})
		*lines = append(*lines, doctorLine{
			Text: i18n.T("doctor.suggest_venv"), Hint: true,
		})
		return false // downstream checks need a working python
	}
	if usingVenv {
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.python_venv", python), OK: true,
		})
	} else {
		// Fallback PATH python — works but means the TUI's managed venv is missing.
		// Not a hard failure (dev installs hit this path), but worth surfacing.
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.python_fallback", python), Warn: true,
		})
	}

	// K3. lingtai package importable, and capture version string.
	kernelVersion, importErr := probeKernelImport(python)
	if importErr != "" {
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.kernel_import_fail", importErr),
		})
		*lines = append(*lines, doctorLine{
			Text: i18n.T("doctor.suggest_reinstall_kernel"), Hint: true,
		})
		return false // can't proceed with further kernel checks
	}
	*lines = append(*lines, doctorLine{
		Text: i18n.TF("doctor.kernel_version", kernelVersion), OK: true,
	})

	// Note: the TUI binary and the Python kernel (`lingtai` on PyPI) ship
	// from separate repos with independent version numbers — they are NOT
	// meant to track each other. An earlier version of /doctor warned on
	// mismatch; that check was wrong and has been removed. Users see both
	// versions via K1 and K3 above and can compare manually if relevant.

	// K5. `python -m lingtai --help` exits 0 (catches broken entry points,
	// missing CLI deps like click/typer, etc. that `import lingtai` alone misses).
	if err, stderr := probeKernelCLI(python); err != nil {
		detail := strings.TrimSpace(stderr)
		if detail == "" {
			detail = err.Error()
		}
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.kernel_cli_fail", detail),
		})
		*lines = append(*lines, doctorLine{
			Text: i18n.T("doctor.suggest_force_reinstall"), Hint: true,
		})
		allOK = false
	}

	// K6. Migration version in .lingtai/meta.json vs this binary's CurrentVersion.
	// orchDir is <projectRoot>/.lingtai/<orchName>, so .lingtai/ is its parent.
	lingtaiDir := filepath.Dir(orchDir)
	projectVersion, metaErr := readMetaVersion(lingtaiDir)
	switch {
	case metaErr != nil:
		// meta.json missing or unreadable — surface but don't fail the suite,
		// since a project being opened for the first time legitimately has none.
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.meta_unreadable", metaErr.Error()), Warn: true,
		})
	case projectVersion == migrate.CurrentVersion:
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.migration_ok", projectVersion), OK: true,
		})
	case projectVersion > migrate.CurrentVersion:
		// User downgraded the TUI. Data format is ahead of the binary.
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.migration_ahead", projectVersion, migrate.CurrentVersion),
		})
		*lines = append(*lines, doctorLine{
			Text: i18n.T("doctor.suggest_upgrade_tui"), Hint: true,
		})
		allOK = false
	default:
		// projectVersion < CurrentVersion — migrations should have run at startup,
		// so hitting this means a silent migration failure or a stale state.
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.migration_behind", projectVersion, migrate.CurrentVersion),
		})
		*lines = append(*lines, doctorLine{
			Text: i18n.T("doctor.suggest_restart_tui"), Hint: true,
		})
		allOK = false
	}

	// K7. Orchestrator heartbeat. Uses the canonical 3.0s liveness threshold
	// from app.go / mail.go / nirvana.go. No remediation suggestion — a stale
	// heartbeat can mean ASLEEP, SUSPENDED, crashed, or just lagging; the
	// main view is the authoritative place for state and recovery actions.
	age, hbErr := readHeartbeatAge(orchDir)
	switch {
	case hbErr != nil && os.IsNotExist(hbErr):
		*lines = append(*lines, doctorLine{
			Text: i18n.T("doctor.heartbeat_missing"), Warn: true,
		})
	case hbErr != nil:
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.heartbeat_unreadable", hbErr.Error()), Warn: true,
		})
	case age < 3*time.Second:
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.heartbeat_fresh", formatHeartbeatAge(age)), OK: true,
		})
	default:
		*lines = append(*lines, doctorLine{
			Text: i18n.TF("doctor.heartbeat_stale", formatHeartbeatAge(age)), Warn: true,
		})
	}

	return allOK
}

// readHeartbeatAge reads the orchestrator's .agent.heartbeat file and returns
// how long ago it was written. Mirrors fs.IsAlive's parsing but returns the
// age directly so /doctor can display it. Errors on missing or malformed
// heartbeat files are returned verbatim; the caller distinguishes IsNotExist
// from parse failures.
func readHeartbeatAge(orchDir string) (time.Duration, error) {
	path := filepath.Join(orchDir, ".agent.heartbeat")
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	ts, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return 0, err
	}
	return time.Since(time.Unix(int64(ts), 0)), nil
}

// formatHeartbeatAge renders a duration in the terse unit-appropriate form
// humans expect in diagnostic output: sub-second as "just now", single-digit
// seconds as "Ns ago", minutes as "Nm ago", hours as "Nh ago". Longer than
// a day just shows "Nd ago".
func formatHeartbeatAge(d time.Duration) string {
	switch {
	case d < time.Second:
		return i18n.T("doctor.heartbeat_just_now")
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// probeKernelImport runs `python -c "import lingtai; print(lingtai.__version__)"`
// capturing stderr on failure so we can surface the real ImportError
// (not just "exit status 1"). Returns (version, "") on success, or ("", errMsg).
func probeKernelImport(python string) (string, string) {
	cmd := exec.Command(python, "-c", "import lingtai; print(lingtai.__version__)")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		detail := strings.TrimSpace(stderr.String())
		if detail == "" {
			detail = err.Error()
		}
		// Collapse multi-line tracebacks to the last meaningful line
		// (usually "ModuleNotFoundError: ..." or "ImportError: ...").
		if idx := strings.LastIndex(detail, "\n"); idx >= 0 {
			detail = strings.TrimSpace(detail[idx+1:])
		}
		return "", detail
	}
	return strings.TrimSpace(stdout.String()), ""
}

// probeKernelCLI runs `python -m lingtai --help` and reports failure with captured stderr.
func probeKernelCLI(python string) (error, string) {
	cmd := exec.Command(python, "-m", "lingtai", "--help")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return err, stderr.String()
	}
	return nil, ""
}

// readMetaVersion reads .lingtai/meta.json and returns the version field.
// Returns (0, nil) if the file doesn't exist; (0, err) on parse failure.
func readMetaVersion(lingtaiDir string) (int, error) {
	data, err := os.ReadFile(filepath.Join(lingtaiDir, "meta.json"))
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	var meta struct {
		Version int `json:"version"`
	}
	if err := json.Unmarshal(data, &meta); err != nil {
		return 0, err
	}
	return meta.Version, nil
}

func extractErrorMessage(body string) string {
	var obj map[string]interface{}
	if err := json.Unmarshal([]byte(body), &obj); err != nil {
		if len(body) > 100 {
			return body[:100] + "..."
		}
		return body
	}
	if errObj, ok := obj["error"].(map[string]interface{}); ok {
		if msg, ok := errObj["message"].(string); ok {
			return msg
		}
	}
	if errStr, ok := obj["error"].(string); ok {
		return errStr
	}
	if msg, ok := obj["message"].(string); ok {
		return msg
	}
	if len(body) > 100 {
		return body[:100] + "..."
	}
	return body
}
