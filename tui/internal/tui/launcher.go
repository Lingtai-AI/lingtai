package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/preset"
)

// LauncherDecisionKind is the typed outcome of the no-project launcher —
// see the design doc's "typed result, not boolean flags" guidance.
type LauncherDecisionKind uint8

const (
	DecisionCancel LauncherDecisionKind = iota
	DecisionOpenExisting
	DecisionCreate
)

// LauncherResult is what main.go receives when the launcher root model
// exits. ProjectRoot is set for both successful decisions; Draft is set for
// DecisionCreate (already staged/committed by RunProjectCreate before this
// result is produced — see LauncherRootModel.Update's ProjectDraftConfirmedMsg
// handling). DecisionCancel means the user backed out entirely: Esc/q/Ctrl+C at
// Welcome or Choose, q/Ctrl+C on Picker or Staging, and Ctrl+C during Create;
// zero filesystem writes occurred.
type LauncherResult struct {
	Kind        LauncherDecisionKind
	ProjectRoot string
	Draft       *ProjectDraft
	// CreateResult carries the finalizer's outcome for DecisionCreate so
	// main.go can decide how to proceed (construct App normally on full
	// success, or show a retry/incomplete banner on a post-commit
	// failure that still left a valid project).
	CreateResult *CreateResult
}

// LauncherDoneMsg is emitted by the launcher root model when it has reached
// a terminal decision. main.go inspects the final root model's Result() after
// the handoff program quits; the message remains for programmatic/test
// callers that want to observe the decision without needing that wrapper.
type LauncherDoneMsg struct {
	Result LauncherResult
}

// launcherView is the launcher's own tiny navigation graph:
//
//	Welcome ⇄ Choose ⇄ (Picker | Staging | Create)
//
// Welcome is always first (Jason's redesign direction: a no-project user
// meets LingTai through the SAME welcome visual language as first-run —
// brand, language, theme — BEFORE being asked to decide anything).
// Choose is the explicit create-here / open-existing decision. Picker is
// the existing project-level "open existing" view. Staging is the
// unfinished-creation recovery screen (its own view, no longer borrowing
// the picker's identity). Create hosts the draft-purpose FirstRunModel.
type launcherView int

const (
	launcherViewWelcome launcherView = iota
	launcherViewChoose
	launcherViewPicker
	launcherViewStaging
	launcherViewCreate
)

// launcherLangs mirrors the first-run welcome selector's order and labels —
// the launcher prelude IS the welcome page for the no-project flow, so the
// two must never diverge.
var launcherLangs = []string{"en", "zh", "wen"}

// LauncherRootModel is the pre-App root Bubble Tea model for the no-project
// case (design doc Invariant 2/6): it owns ONLY view state, the embedded
// ProjectsModel used by Open Existing, and (during Create) a *ProjectDraft.
// It never runs migration/bootstrap. Open Existing forwards the embedded
// ProjectsModel's messages, including back and validated selections; the
// child owns inventory loading and selection validation. The launcher reaches
// its single real filesystem commit only when the user reaches stepReview and
// presses "Start project", at which point RunProjectCreate performs the
// staging→validate→rename commit.
//
// main.go constructs this as the initial root of the handoff program and
// inspects Result() after the program exits. Open Existing later replaces
// this model with the prepared App; it does not construct a fake/empty-path
// App to host the launcher.
type LauncherRootModel struct {
	globalDirPath string // pure path, may not exist on disk yet
	projectRoot   string // cwd — where Create would build, if chosen
	width, height int

	view launcherView

	// Welcome prelude state. langIdx indexes launcherLangs; themeName is
	// the currently previewed theme. Both are initialized from a PURE
	// config.LoadTUIConfig read at construction and only ever applied
	// in-memory (i18n.SetLang / SetThemeByName) — persisting them is the
	// create-flow finalizer's job (ProjectDraft.applyToConfig), and only
	// after the user confirms "Start project".
	langIdx   int
	themeName string

	// cursor on the choose page: 0 = start here, 1 = open existing.
	chooseCursor int

	// Open Existing embeds the established /projects model directly. Its
	// activation is owned by the launcher so delayed child messages from a
	// previous entry cannot select a project in the current view.
	projects             ProjectsModel
	projectsActivationID uint64

	// Create: hosts a draft-purpose FirstRunModel plus its ProjectDraft.
	draft      *ProjectDraft
	firstRun   FirstRunModel
	firstRunOn bool

	// preDraftTheme/preDraftLanguage snapshot the launcher's own prelude
	// selection at the moment enterCreate constructs a new draft. The
	// draft wizard starts PAST its welcome step now (the launcher prelude
	// owns language/theme), so the wizard has no remaining path that
	// mutates the process-wide theme/language state — but the cancel
	// handler still restores to this baseline as defense-in-depth, so a
	// future wizard step that previews either one can never leave a
	// cancelled attempt's preview stuck on the choose page.
	preDraftTheme    string
	preDraftLanguage string

	// Unfinished staging detection (Invariant 5, read-only + explicit
	// choice; Resume is a documented stub, Discard is fully functional).
	unfinishedStaging       []string
	unfinishedCursor        int
	unfinishedDiscardArmed  bool
	unfinishedDiscardStatus string

	// createResult/createErr hold the outcome once the user confirms
	// "Start project" and RunProjectCreate has been invoked synchronously
	// (staging/build/validate/rename are fast local filesystem operations;
	// no network I/O is on the pre-commit path, so a blocking call here is
	// the simplest correct implementation — see report for rationale).
	createResult *CreateResult
	createErr    string

	lingtaiCmd string // passed through to RunProjectCreate's post-commit launch phase

	done   bool
	result LauncherResult
}

// NewLauncherRootModel constructs the launcher. globalDirPath must be the
// PURE path (from config.GlobalDirPath, not config.GlobalDir) — the
// launcher must not create ~/.lingtai-tui merely by being constructed.
// lingtaiCmd is the best-effort command discovered before launcher entry.
// It may be empty: after a successful atomic publication the finalizer always
// ensures the runtime and resolves the command again. Tests that need to
// suppress host discovery inject CreateOptions runtime/resolution seams at the
// finalizer boundary rather than relying on an empty string.
//
// The constructor applies the PERSISTED theme and language (in-memory only:
// SetThemeByName / i18n.SetLang, from a pure config.LoadTUIConfig read that
// never writes) before the first frame renders. This mirrors the normal
// startup path's early i18n.SetLang(tuiCfg.Language) so a returning user
// who happens to cd into an empty directory sees the launcher in THEIR
// palette and locale, not the compiled-in defaults — the exact "theme is
// wrong" defect of the previous launcher, which rendered ink-dark/English
// regardless of configuration.
//
// Unfinished-staging detection (design doc Invariant 5) is populated HERE,
// not in Init(). tea.Model's Init() tea.Cmd signature has no way to return
// an updated model — the framework only applies the returned tea.Cmd, so a
// value-receiver Init() that assigns to a field is mutating a throwaway
// copy: the field never reaches the model the tea.Program actually holds. A
// prior version of this constructor left DetectUnfinishedStaging inside
// Init() for exactly that (mistaken) reason and the crash-recovery
// Resume/Discard UI was silently unreachable — m.unfinishedStaging was
// always nil by the time the choose page read it.
// DetectUnfinishedStaging is a pure directory listing (os.ReadDir plus a
// marker-file os.Stat, no writes), so running it during construction keeps
// the same "constructor performs only reads" contract Init() itself would
// have needed to honor.
func NewLauncherRootModel(projectRoot, globalDirPath, lingtaiCmd string) LauncherRootModel {
	baseline := config.LoadTUIConfig(globalDirPath) // pure read; defaults when absent
	themeName := baseline.Theme
	if themeName == "" {
		themeName = DefaultThemeName
	}
	SetThemeByName(themeName)
	langIdx := 0
	if err := i18n.SetLang(baseline.Language); err != nil {
		_ = i18n.SetLang("en")
	}
	for i, l := range launcherLangs {
		if l == i18n.Lang() {
			langIdx = i
			break
		}
	}
	return LauncherRootModel{
		globalDirPath:     globalDirPath,
		projectRoot:       projectRoot,
		lingtaiCmd:        lingtaiCmd,
		view:              launcherViewWelcome,
		langIdx:           langIdx,
		themeName:         themeName,
		unfinishedStaging: DetectUnfinishedStaging(projectRoot),
	}
}

// Result returns the terminal decision. Only meaningful after Done()
// reports true (i.e. after the model has emitted tea.Quit).
func (m LauncherRootModel) Result() LauncherResult { return m.result }
func (m LauncherRootModel) Done() bool             { return m.done }

// Init performs no filesystem work — unfinished-staging detection happens in
// NewLauncherRootModel (see its doc comment for why Init() cannot do this
// via a value-receiver field assignment).
func (m LauncherRootModel) Init() tea.Cmd {
	return nil
}

func (m LauncherRootModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.view == launcherViewCreate && m.firstRunOn {
			updated, cmd := m.firstRun.Update(msg)
			m.firstRun = updated
			return m, cmd
		}
		if m.view == launcherViewPicker {
			updated, cmd := m.projects.Update(msg)
			m.projects = updated
			return m, cmd
		}
		return m, nil

	case ProjectsAgentSelectedMsg:
		// ProjectsModel has already re-scanned and validated this record.
		// The launcher needs the project altitude, not the agent visit
		// transition used by App. Both the child activation and its latest
		// validation request must still be current in this launcher view.
		if m.view == launcherViewPicker && msg.ActivationID == m.projectsActivationID && msg.RequestSeq == m.projects.requestSeq && msg.Record.Enterable && msg.Record.Project != "" {
			m.result = LauncherResult{Kind: DecisionOpenExisting, ProjectRoot: msg.Record.Project}
			m.done = true
			return m, tea.Sequence(func() tea.Msg { return LauncherDoneMsg{Result: m.result} }, tea.Quit)
		}
		return m, nil

	case ViewChangeMsg:
		if m.view == launcherViewPicker && msg.View == "mail" {
			m.view = launcherViewChoose
			return m, nil
		}
		return m, nil

	case ProjectDraftCancelledMsg:
		if m.view != launcherViewCreate {
			return m, nil
		}
		// Back out of the create wizard entirely — no writes occurred (see
		// ProjectDraftCancelledMsg's doc comment), so the only thing to do
		// is discard the old draft/FirstRunModel and return to the choose
		// page. Discarding (not merely hiding) the old draft/firstRun is
		// the point: a subsequent "Start a new project" must construct a
		// genuinely FRESH ProjectDraft via enterCreate, never resume a
		// half-filled one — a parent review's exact "subsequent Create
		// starts a fresh draft" requirement.
		//
		// Restore theme/language to the launcher's own prelude baseline
		// BEFORE discarding the draft. The draft wizard now starts past
		// its welcome step (the launcher prelude owns language/theme), so
		// no wizard path currently previews either — this restore is
		// defense-in-depth so a future wizard preview could never leave
		// the choose page stuck showing a cancelled attempt's state. It
		// performs no writes (SetThemeByName/i18n.SetLang are in-memory
		// only).
		restoreTheme := m.preDraftTheme
		if restoreTheme == "" {
			restoreTheme = DefaultThemeName
		}
		SetThemeByName(restoreTheme)
		restoreLang := m.preDraftLanguage
		if restoreLang == "" {
			restoreLang = "en"
		}
		_ = i18n.SetLang(restoreLang)
		m.draft = nil
		m.firstRun = FirstRunModel{}
		m.firstRunOn = false
		m.createErr = ""
		m.createResult = nil
		m.view = launcherViewChoose
		return m, nil

	case ProjectDraftConfirmedMsg:
		if m.view != launcherViewCreate {
			return m, nil
		}
		// This is the one point where the launcher performs a real
		// filesystem mutation: RunProjectCreate's staging→validate→rename
		// sequence. Everything before this message was draft-only.
		res := RunProjectCreate(msg.Draft, CreateOptions{
			GlobalDir:           m.globalDirPath,
			LingtaiCmd:          m.lingtaiCmd,
			ExpectedProjectRoot: m.projectRoot,
		})
		m.createResult = &res
		if res.Err != nil && !res.Committed {
			m.createErr = res.Err.Error()
			// Pre-rename failure: no project was created. Stay on the
			// review step (already the current firstRun step) so the
			// user sees the error and can retry/adjust without losing
			// their draft.
			return m, nil
		}
		m.result = LauncherResult{
			Kind:         DecisionCreate,
			ProjectRoot:  msg.Draft.ProjectRoot,
			Draft:        msg.Draft,
			CreateResult: &res,
		}
		m.done = true
		return m, tea.Sequence(func() tea.Msg { return LauncherDoneMsg{Result: m.result} }, tea.Quit)

	case tea.KeyPressMsg:
		switch m.view {
		case launcherViewWelcome:
			return m.updateWelcome(msg)
		case launcherViewChoose:
			return m.updateChoose(msg)
		case launcherViewPicker:
			return m.updatePicker(msg)
		case launcherViewStaging:
			return m.updateUnfinishedStaging(msg)
		case launcherViewCreate:
			if msg.String() == "ctrl+c" {
				return m.cancelAndQuit()
			}
			updated, cmd := m.firstRun.Update(msg)
			m.firstRun = updated
			return m, cmd
		}
		return m, nil
	}

	// Forward everything else (mouse wheel, paste, sub-model async
	// messages) to the active child model.
	if m.view == launcherViewCreate && m.firstRunOn {
		updated, cmd := m.firstRun.Update(msg)
		m.firstRun = updated
		return m, cmd
	}
	if m.view == launcherViewPicker {
		updated, cmd := m.projects.Update(msg)
		m.projects = updated
		return m, cmd
	}
	return m, nil
}

// cancelAndQuit records the zero-write cancel decision and quits the
// launcher's tea.Program. Reachable from Welcome (Esc/q/Ctrl+C), Choose,
// Picker, and Staging (q/Ctrl+C), and Create (Ctrl+C); Esc on those pages
// goes BACK one page instead, so leaving is always deliberate and never a
// mis-keyed Esc.
func (m LauncherRootModel) cancelAndQuit() (tea.Model, tea.Cmd) {
	m.result = LauncherResult{Kind: DecisionCancel}
	m.done = true
	return m, tea.Sequence(func() tea.Msg { return LauncherDoneMsg{Result: m.result} }, tea.Quit)
}

func (m LauncherRootModel) updateWelcome(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up":
		if m.langIdx > 0 {
			m.langIdx--
			_ = i18n.SetLang(launcherLangs[m.langIdx])
		}
	case "down":
		if m.langIdx < len(launcherLangs)-1 {
			m.langIdx++
			_ = i18n.SetLang(launcherLangs[m.langIdx])
		}
	case "ctrl+t":
		// Cycle through registered themes — in-memory preview only; the
		// choice is persisted (via ProjectDraft.applyToConfig) only if the
		// user later confirms creating a project.
		names := ThemeNames()
		next := names[0]
		for i, n := range names {
			if n == m.themeName {
				next = names[(i+1)%len(names)]
				break
			}
		}
		m.themeName = next
		SetThemeByName(next)
	case "enter":
		m.view = launcherViewChoose
	case "esc", "q", "ctrl+c":
		return m.cancelAndQuit()
	}
	return m, nil
}

func (m LauncherRootModel) updateChoose(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.chooseCursor > 0 {
			m.chooseCursor--
		}
	case "down", "j":
		if m.chooseCursor < 1 {
			m.chooseCursor++
		}
	case "esc":
		m.view = launcherViewWelcome
	case "q", "ctrl+c":
		return m.cancelAndQuit()
	case "enter":
		if m.chooseCursor == 1 {
			return m.enterPicker()
		}
		// Start a new project here.
		if len(m.unfinishedStaging) > 0 {
			m.view = launcherViewStaging
			m.unfinishedCursor = 0
			m.unfinishedDiscardArmed = false
			m.unfinishedDiscardStatus = ""
			return m, nil
		}
		return m.enterCreate()
	}
	return m, nil
}

// enterPicker embeds the established /projects model. The child owns the
// inventory scan, cursor, validation, and rendering; the launcher only maps
// its validated selection/back messages to launcher decisions/navigation.
func (m LauncherRootModel) enterPicker() (tea.Model, tea.Cmd) {
	m.view = launcherViewPicker
	m.projectsActivationID++
	if m.projectsActivationID == 0 {
		m.projectsActivationID = 1
	}
	m.projects = NewProjectsModelWithActivation(m.globalDirPath, filepath.Join(m.projectRoot, ".lingtai"), ProjectsContext{}, m.projectsActivationID)
	if m.width > 0 || m.height > 0 {
		m.projects.SetSize(m.width, m.height)
	}
	return m, m.projects.Init()
}

func (m LauncherRootModel) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+c" {
		return m.cancelAndQuit()
	}
	updated, cmd := m.projects.Update(msg)
	m.projects = updated
	return m, cmd
}

// enterCreate constructs the draft-purpose FirstRunModel. hasPresets is a
// pure read (preset.HasAny stats ~/.lingtai-tui/presets/, creating
// nothing) so it stays inside the zero-write contract.
//
// The launcher's welcome prelude already collected language and theme, so
// the draft is seeded with both BEFORE the wizard is constructed —
// NewDraftFirstRunModel starts the wizard at its preset-pick step (its own
// welcome step would duplicate the prelude) and the finalizer persists the
// seeded values via ProjectDraft.applyToConfig only after the user confirms
// "Start project". preDraftTheme/preDraftLanguage capture the same prelude
// baseline for the cancel handler's defense-in-depth restore.
func (m LauncherRootModel) enterCreate() (tea.Model, tea.Cmd) {
	m.draft = NewProjectDraft(m.projectRoot)
	m.draft.Language = launcherLangs[m.langIdx]
	m.draft.Theme = m.themeName
	m.preDraftTheme = m.themeName
	m.preDraftLanguage = m.draft.Language
	m.view = launcherViewCreate
	baseDir := filepath.Join(m.projectRoot, ".lingtai") // never created — passed only for read-oriented helpers that expect a path shape
	m.firstRun = NewDraftFirstRunModel(baseDir, m.globalDirPath, preset.HasAny(), m.draft)
	m.firstRunOn = true
	cmd := m.firstRun.Init()
	if m.width > 0 {
		updated, sizeCmd := m.firstRun.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
		m.firstRun = updated
		cmd = tea.Batch(cmd, sizeCmd)
	}
	return m, cmd
}

func (m LauncherRootModel) updateUnfinishedStaging(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.unfinishedCursor > 0 {
			m.unfinishedCursor--
		}
		m.unfinishedDiscardArmed = false
	case "down", "j":
		if m.unfinishedCursor < len(m.unfinishedStaging)-1 {
			m.unfinishedCursor++
		}
		m.unfinishedDiscardArmed = false
	case "esc":
		m.view = launcherViewChoose
		return m, nil
	case "q", "ctrl+c":
		return m.cancelAndQuit()
	case "r":
		// Resume is intentionally NOT implemented in this vertical slice
		// (see design doc Invariant 5 scoping note in the implementation
		// report) — surfaced honestly rather than silently no-op'd.
		m.unfinishedDiscardStatus = i18n.T("launcher.staging.resume_unsupported")
		return m, nil
	case "d":
		if len(m.unfinishedStaging) == 0 {
			return m, nil
		}
		if !m.unfinishedDiscardArmed {
			m.unfinishedDiscardArmed = true
			m.unfinishedDiscardStatus = i18n.T("launcher.staging.discard_confirm")
			return m, nil
		}
		target := m.unfinishedStaging[m.unfinishedCursor]
		if err := DiscardUnfinishedStaging(target); err != nil {
			m.unfinishedDiscardStatus = err.Error()
		} else {
			m.unfinishedStaging = append(append([]string{}, m.unfinishedStaging[:m.unfinishedCursor]...), m.unfinishedStaging[m.unfinishedCursor+1:]...)
			if m.unfinishedCursor >= len(m.unfinishedStaging) {
				m.unfinishedCursor = max(0, len(m.unfinishedStaging)-1)
			}
			m.unfinishedDiscardStatus = i18n.T("launcher.staging.discarded")
		}
		m.unfinishedDiscardArmed = false
		if len(m.unfinishedStaging) == 0 {
			return m.enterCreate()
		}
		return m, nil
	case "c":
		// Continue to Create anyway, leaving the leftover staging in
		// place untouched.
		return m.enterCreate()
	}
	return m, nil
}

// View implements the launcher phase of the root handoff program. Bubble Tea
// v2's root Model.View returns tea.View; composition mirrors App.View's
// structure without constructing a fake App before a project is selected.
func (m LauncherRootModel) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	ApplyThemeToView(&v)
	v.ReportFocus = true
	return v
}

func (m LauncherRootModel) viewContent() string {
	switch m.view {
	case launcherViewWelcome:
		return m.viewWelcome()
	case launcherViewChoose:
		return m.viewChoose()
	case launcherViewPicker:
		return m.projects.View()
	case launcherViewStaging:
		return m.viewUnfinishedStaging()
	case launcherViewCreate:
		out := m.firstRun.View()
		if m.createErr != "" {
			failed := i18n.TF("launcher.create.failed", m.createErr)
			for _, line := range wrapToWidth(failed, launcherTextWidth(m.width, 2)) {
				out += "\n  " + lipgloss.NewStyle().Bold(true).Foreground(ColorSuspended).Render(line)
			}
			out += "\n"
		}
		return out
	}
	return ""
}

// viewWelcome reuses the canonical FirstRunModel Welcome presentation. This
// view-only value deliberately enables welcome-only mode (and no rehydration)
// so the launcher shares the existing layout and footer without constructing a
// second renderer; the root still owns launcher navigation and quit handling.
func (m LauncherRootModel) viewWelcome() string {
	return (FirstRunModel{
		width:         m.width,
		height:        m.height,
		langCursor:    m.langIdx,
		welcomeOnly:   true,
		rehydrateMode: false,
	}).viewWelcome()
}

// viewChoose renders the existing project-status sentence above the
// functional create/open choices and navigation.
func (m LauncherRootModel) viewChoose() string {
	var lines []string
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
	lines = append(lines, titleStyle.Render(i18n.T("launcher.choose.title")))
	lines = append(lines, "")
	displayRoot := abbreviateHomePath(m.projectRoot)
	for _, line := range wrapToWidth(i18n.TF("launcher.welcome.explain2", displayRoot), launcherTextWidth(m.width, 2)) {
		lines = append(lines, StyleSubtle.Render(line))
	}
	lines = append(lines, "")
	options := []string{i18n.T("launcher.choose.here"), i18n.T("launcher.choose.open")}
	for i, label := range options {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.chooseCursor {
			cursor = "> "
			style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
		}
		lines = append(lines, cursor+style.Render(truncatePathToWidth(label, launcherTextWidth(m.width, lipgloss.Width(cursor)+2))))
		if i == 0 {
			lines = append(lines, "")
		}
	}
	lines = append(lines, "")
	hint := "↑↓ " + i18n.T("welcome.select_lang") +
		"  [Enter] " + i18n.T("welcome.confirm") +
		"  [Esc] " + i18n.T("launcher.hint_back") +
		"  [q/Ctrl+C] " + i18n.T("launcher.hint_quit")
	for _, line := range wrapToWidth(hint, launcherTextWidth(m.width, 2)) {
		lines = append(lines, StyleFaint.Render(line))
	}
	return verticallyCentered(centerBlock(lines, m.width), m.height)
}

// launcherTextWidth returns a bounded text column for a terminal width. A
// positive margin reserves the fixed indentation that the caller adds after
// wrapping; an unknown width gets a conservative test/display width.
func launcherTextWidth(width, margin int) int {
	if width <= 0 {
		return 80
	}
	if width-margin < 1 {
		return 1
	}
	return width - margin
}

func (m LauncherRootModel) stagingFooterLines() []string {
	lines := []string{strings.Repeat("─", max(0, m.width))}
	hint := "[d] " + i18n.T("launcher.staging.discard") +
		"  [r] " + i18n.T("launcher.staging.resume") +
		"  [c] " + i18n.T("launcher.staging.continue") +
		"  [Esc] " + i18n.T("launcher.hint_back") +
		"  [q/Ctrl+C] " + i18n.T("launcher.hint_quit")
	for _, line := range wrapToWidth(hint, launcherTextWidth(m.width, 2)) {
		lines = append(lines, "  "+StyleFaint.Render(line))
	}
	return lines
}

func (m LauncherRootModel) stagingBodyLines() ([]string, int, int) {
	var lines []string
	for _, hl := range wrapToWidth(i18n.T("launcher.staging.hint"), launcherTextWidth(m.width, 2)) {
		lines = append(lines, "  "+hl)
	}
	lines = append(lines, "")
	selectedLine := -1
	for i, dir := range m.unfinishedStaging {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.unfinishedCursor {
			cursor = "> "
			style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
		}
		if i == m.unfinishedCursor {
			selectedLine = len(lines)
		}
		lines = append(lines, cursor+style.Render(truncatePathToWidth(dir, launcherTextWidth(m.width, lipgloss.Width(cursor)))))
	}
	statusLine := -1
	if m.unfinishedDiscardStatus != "" {
		lines = append(lines, "")
		statusLine = len(lines)
		// The first physical line carries the status meaning (including the
		// discard error prefix). Bound it horizontally and keep it as one
		// body line so a long error cannot consume the actionable footer.
		status := truncateTextToWidth(m.unfinishedDiscardStatus, launcherTextWidth(m.width, 2))
		lines = append(lines, "  "+status)
	}
	return lines, selectedLine, statusLine
}

func (m LauncherRootModel) viewUnfinishedStaging() string {
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSuspended)
	titleLines := []string{"  " + titleStyle.Render(truncatePathToWidth(i18n.T("launcher.staging.title"), launcherTextWidth(m.width, 2)))}
	footerLines := m.stagingFooterLines()
	bodyLines, selectedLine, statusLine := m.stagingBodyLines()

	// Reserve the title and actionable footer first. The explanation, staging
	// paths, and status are a variable region; when it is too tall, window it
	// around the selected path and status rather than pushing the footer below
	// the terminal viewport.
	avail := m.height - len(titleLines) - len(footerLines)
	if avail < 1 {
		avail = 1
	}
	if len(bodyLines) > avail {
		target := selectedLine
		if target < 0 {
			target = 0
		}
		if statusLine >= 0 {
			if target < 0 || statusLine < target {
				target = statusLine
			} else {
				target = (target + statusLine) / 2
			}
		}
		start := target - avail/2
		if start > len(bodyLines)-avail {
			start = len(bodyLines) - avail
		}
		if start < 0 {
			start = 0
		}
		bodyLines = bodyLines[start : start+avail]
	}
	for len(bodyLines) < avail {
		bodyLines = append(bodyLines, "")
	}

	lines := append(append([]string{}, titleLines...), bodyLines...)
	lines = append(lines, footerLines...)
	return strings.Join(lines, "\n")
}

// verticallyCentered pads content with leading newlines so its block sits
// vertically centered — the same rule firstrun.go's welcome page uses.
func verticallyCentered(content string, height int) string {
	contentLines := strings.Count(content, "\n")
	topPad := (height - contentLines) / 2
	if topPad < 1 {
		topPad = 1
	}
	return strings.Repeat("\n", topPad) + content
}

// centerBlock left-aligns the given lines against each other, then centers
// the whole block horizontally — a readable middle ground between fully
// centered text (welcome page) and a hard left margin (wizard pages).
func centerBlock(lines []string, width int) string {
	maxW := 0
	for _, l := range lines {
		if w := lipgloss.Width(l); w > maxW {
			maxW = w
		}
	}
	pad := 0
	if width > maxW {
		pad = (width - maxW) / 2
	}
	prefix := strings.Repeat(" ", pad)
	var b strings.Builder
	for _, l := range lines {
		b.WriteString(prefix + l + "\n")
	}
	return b.String()
}

// wrapToWidth is a small display-width-aware greedy word wrapper for
// option/hint copy. CJK text (no spaces) falls back to rune-width chunking
// so zh/wen descriptions still wrap instead of overflowing.
func wrapToWidth(s string, width int) []string {
	if width < 4 {
		width = 4
	}
	var out []string
	for _, para := range strings.Split(s, "\n") {
		words := strings.Fields(para)
		if len(words) == 0 {
			out = append(out, "")
			continue
		}
		line := ""
		flush := func() {
			if line != "" {
				out = append(out, line)
				line = ""
			}
		}
		for _, w := range words {
			for lipgloss.Width(w) > width {
				// Single overlong token (typical for CJK, which has no
				// spaces): split at display width.
				head, tail := splitAtDisplayWidth(w, width-lipgloss.Width(line)-boolToInt(line != ""))
				if head == "" {
					flush()
					head, tail = splitAtDisplayWidth(w, width)
				}
				if line != "" {
					line += " "
				}
				line += head
				flush()
				w = tail
			}
			if w == "" {
				continue
			}
			if line == "" {
				line = w
			} else if lipgloss.Width(line)+1+lipgloss.Width(w) <= width {
				line += " " + w
			} else {
				flush()
				line = w
			}
		}
		flush()
	}
	return out
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// splitAtDisplayWidth splits s so the head's display width is at most w.
func splitAtDisplayWidth(s string, w int) (string, string) {
	if w <= 0 {
		return "", s
	}
	used := 0
	for i, r := range s {
		rw := lipgloss.Width(string(r))
		if used+rw > w {
			return s[:i], s[i:]
		}
		used += rw
	}
	return s, ""
}

// truncatePathToWidth shortens a display path to fit width, keeping the
// tail (the discriminating part of a filesystem path) and prefixing an
// ellipsis.
func truncatePathToWidth(p string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(p) <= width {
		return p
	}
	if width == 1 {
		return "…"
	}
	target := width - lipgloss.Width("…")
	if target <= 0 {
		return "…"
	}
	// Keep the tail, which is the useful/discriminating part of a path or
	// project name, while measuring display columns rather than bytes.
	runes := []rune(p)
	for len(runes) > 0 && lipgloss.Width(string(runes)) > target {
		runes = runes[1:]
	}
	return "…" + string(runes)
}

// truncateTextToWidth keeps the beginning of explanatory/status text so its
// marker and meaning remain visible when a body budget is tight.
func truncateTextToWidth(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= width {
		return s
	}
	if width == 1 {
		return "…"
	}
	head, _ := splitAtDisplayWidth(s, width-lipgloss.Width("…"))
	return head + "…"
}

// abbreviateHomePath renders the user's home directory as "~" for display.
// Display-only — decisions and results always carry the full path.
func abbreviateHomePath(p string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return p
	}
	if p == home {
		return "~"
	}
	if strings.HasPrefix(p, home+string(filepath.Separator)) {
		return "~" + p[len(home):]
	}
	return p
}

// ProbeNoProjectPure is the pure, non-mutating check main.go uses to decide
// whether to enter the launcher at all: does <projectDir>/.lingtai exist?
// Uses Lstat (never Stat) so a symlink at that path counts as "exists"
// rather than being followed or (implicitly) created through — see design
// doc Invariant 1. This function performs NO filesystem writes and no
// directory creation; it is safe to call before config.GlobalDirPath,
// before any migration, before any bootstrap.
//
// It reports a typed (bool, error) rather than folding every error into
// "project exists": os.Lstat can fail for reasons other than "the path is
// absent" (permission denied on a parent directory, an I/O error, an
// unreadable NFS mount, ...). Silently treating any such error as "has
// project" is a fail-OPEN bug — it routes straight into the normal startup
// pipeline (config.GlobalDir/migrations/bootstrap) without the launcher ever
// making a real decision, exactly the eager-write gate this feature exists
// to prevent. Callers MUST fail closed on a non-nil error: surface it and
// exit before touching config.GlobalDir()/any write, rather than guessing
// either polarity.
//
// Return contract (the bool keeps its original meaning — "should the
// launcher run?" — only a genuine error return is new):
//   - absent (os.IsNotExist) -> (true, nil): no project, safe to enter the
//     launcher.
//   - a stat succeeded (dir, file, or symlink) -> (false, nil): project
//     exists, proceed with normal startup.
//   - any other Lstat error -> (false, err): the caller cannot make an
//     honest decision and must not guess either way; the false alongside a
//     non-nil error is not itself meaningful — callers must check err first.
func ProbeNoProjectPure(projectDir string) (bool, error) {
	lingtaiDir := filepath.Join(projectDir, ".lingtai")
	_, err := os.Lstat(lingtaiDir)
	switch {
	case err == nil:
		return false, nil
	case os.IsNotExist(err):
		return true, nil
	default:
		return false, fmt.Errorf("checking %s: %w", lingtaiDir, err)
	}
}
