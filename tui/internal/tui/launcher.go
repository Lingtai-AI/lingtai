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
// handling). DecisionCancel means the user backed out entirely (Esc/q/
// Ctrl+C at the landing page) — zero filesystem writes occurred.
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
// a terminal decision. main.go's tea.Program wrapper for the launcher
// watches for this (the launcher is a SEPARATE tea.Program run from the
// real App's — see main.go's gate wiring) — practically, main.go runs the
// launcher via p.Run() and inspects the final model's Result() after Quit,
// rather than needing a message at all; LauncherDoneMsg exists for
// programmatic/test callers that want to observe the decision without
// tearing down the whole bubbletea run loop.
type LauncherDoneMsg struct {
	Result LauncherResult
}

type launcherView int

const (
	launcherViewLanding launcherView = iota
	launcherViewOpenExisting
	launcherViewCreate
)

// LauncherRootModel is the pre-App root Bubble Tea model for the no-project
// case (design doc Invariant 2/6): it owns ONLY view state, a read-only
// project catalog, and (during Create) a *ProjectDraft. It never runs
// migration/bootstrap and never touches the filesystem except through
// explicit read-only calls (config.ListRegisteredProjects, the embedded
// ProjectsModel's inventory scan) until the user reaches stepReview and
// presses "Start project", at which point RunProjectCreate performs the
// single staging→validate→rename commit.
//
// main.go constructs this, runs it via its OWN tea.Program (separate from
// the real App's), and inspects Result() after the program exits — it does
// NOT construct a fake/empty-path App to host this (design doc: "why not
// just fake an empty-project App").
type LauncherRootModel struct {
	globalDirPath string // pure path, may not exist on disk yet
	projectRoot   string // cwd — where Create would build, if chosen
	width, height int

	view launcherView
	// cursor on the landing page: 0 = Create new, 1 = Open existing
	landingCursor int

	// Open Existing: reuses ProjectsModel's catalog/rendering/freshness
	// guard for RUNNING projects. Stopped registered projects are listed
	// separately above it (openExistingRegistered) since ProjectsModel's
	// registry source is inventory-only (see projects.go's
	// projectSourceRegistry doc comment) — merging that catalog inside
	// ProjectsModel itself is out of scope for this vertical slice; see
	// the implementation report for the explicit scoping note.
	openExistingRegistered []config.RegisteredProject
	openExistingSection    int // 0 = registered list, 1 = running ProjectsModel
	openExistingCursor     int
	projects               ProjectsModel

	// Create: hosts a draft-purpose FirstRunModel plus its ProjectDraft.
	draft      *ProjectDraft
	firstRun   FirstRunModel
	firstRunOn bool // true once firstRun has been constructed/Init'd

	// preDraftTheme/preDraftLanguage snapshot the persisted (baseline)
	// theme/language at the moment enterCreate constructs a new draft —
	// BEFORE the draft wizard's Welcome step can preview either one via
	// SetThemeByName/i18n.SetLang, both of which mutate genuinely global,
	// process-wide in-memory state regardless of draftMode. A parent
	// review found that ProjectDraftCancelledMsg discarded the draft
	// pointer but never restored this global state, so cancelling out of
	// the wizard after previewing a theme/language left the process (and
	// any freshly started NEXT draft) stuck showing the cancelled
	// preview — the opposite of a true reset. Captured once per
	// enterCreate call (never touched again until the next
	// enterCreate/cancel cycle) via the pure config.LoadTUIConfig read.
	preDraftTheme    string
	preDraftLanguage string

	// Unfinished staging detection (Invariant 5, read-only + explicit
	// choice; Resume is a documented stub, Discard is fully functional).
	unfinishedStaging       []string
	unfinishedCursor        int
	showUnfinishedStaging   bool
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
// Unfinished-staging detection (design doc Invariant 5) is populated HERE,
// not in Init(). tea.Model's Init() tea.Cmd signature has no way to return
// an updated model — the framework only applies the returned tea.Cmd, so a
// value-receiver Init() that assigns to a field is mutating a throwaway
// copy: the field never reaches the model the tea.Program actually holds. A
// prior version of this constructor left DetectUnfinishedStaging inside
// Init() for exactly that (mistaken) reason and the crash-recovery
// Resume/Discard UI was silently unreachable — m.unfinishedStaging was
// always nil by the time updateLanding/updateUnfinishedStaging read it.
// DetectUnfinishedStaging is a pure directory listing (os.ReadDir plus a
// marker-file os.Stat, no writes), so running it during construction keeps
// the same "constructor performs only reads" contract Init() itself would
// have needed to honor.
func NewLauncherRootModel(projectRoot, globalDirPath, lingtaiCmd string) LauncherRootModel {
	return LauncherRootModel{
		globalDirPath:     globalDirPath,
		projectRoot:       projectRoot,
		lingtaiCmd:        lingtaiCmd,
		view:              launcherViewLanding,
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
		if m.view == launcherViewOpenExisting {
			updated, cmd := m.projects.Update(msg)
			m.projects = updated
			return m, cmd
		}
		if m.view == launcherViewCreate && m.firstRunOn {
			updated, cmd := m.firstRun.Update(msg)
			m.firstRun = updated
			return m, cmd
		}
		return m, nil

	case LauncherProjectSelectedMsg:
		if m.view != launcherViewOpenExisting ||
			msg.ActivationID != m.projects.activationID ||
			msg.RequestSeq != m.projects.requestSeq {
			return m, nil
		}
		root, ok := existingProjectRoot(msg.ProjectRoot)
		if !ok {
			m.projects.status = i18n.T("projects.target_changed")
			return m, nil
		}
		m.result = LauncherResult{Kind: DecisionOpenExisting, ProjectRoot: root}
		m.done = true
		return m, tea.Sequence(func() tea.Msg { return LauncherDoneMsg{Result: m.result} }, tea.Quit)

	case ProjectDraftCancelledMsg:
		if m.view != launcherViewCreate {
			return m, nil
		}
		// Back out of the create wizard entirely — no writes occurred (see
		// ProjectDraftCancelledMsg's doc comment), so the only thing to do
		// is discard the old draft/FirstRunModel and return to landing.
		// Discarding (not merely hiding) the old draft/firstRun is the
		// point: a subsequent "Create new project" must construct a
		// genuinely FRESH ProjectDraft via enterCreate, never resume a
		// half-filled one — a parent review's exact "subsequent Create
		// starts a fresh draft" requirement.
		//
		// Restore theme/language to the pre-draft baseline BEFORE
		// discarding the draft. stepWelcome's theme cycle (ctrl+t) and
		// language selection (up/down) both mutate genuinely global,
		// process-wide in-memory state (styles.SetThemeByName,
		// i18n.SetLang) purely for live preview, entirely independent of
		// draftMode — a parent review found Cancel discarded the draft
		// pointer but left that preview mutation in place, so the landing
		// page (and a subsequent fresh draft) stayed stuck showing
		// whatever the cancelled attempt had last previewed instead of
		// reverting to what was actually persisted. This performs no
		// writes (SetThemeByName/i18n.SetLang are in-memory only) and
		// only reads back what enterCreate already captured.
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
		m.view = launcherViewLanding
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
		case launcherViewLanding:
			return m.updateLanding(msg)
		case launcherViewOpenExisting:
			if m.showUnfinishedStaging {
				return m.updateUnfinishedStaging(msg)
			}
			return m.updateOpenExisting(msg)
		case launcherViewCreate:
			updated, cmd := m.firstRun.Update(msg)
			m.firstRun = updated
			return m, cmd
		}
		return m, nil
	}

	// Forward everything else (mouse wheel, paste, sub-model async
	// messages) to whichever sub-model owns the active view.
	switch m.view {
	case launcherViewOpenExisting:
		updated, cmd := m.projects.Update(msg)
		m.projects = updated
		return m, cmd
	case launcherViewCreate:
		if m.firstRunOn {
			updated, cmd := m.firstRun.Update(msg)
			m.firstRun = updated
			return m, cmd
		}
	}
	return m, nil
}

func (m LauncherRootModel) updateLanding(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.landingCursor > 0 {
			m.landingCursor--
		}
	case "down", "j":
		if m.landingCursor < 1 {
			m.landingCursor++
		}
	case "esc", "q", "ctrl+c":
		m.result = LauncherResult{Kind: DecisionCancel}
		m.done = true
		return m, tea.Sequence(func() tea.Msg { return LauncherDoneMsg{Result: m.result} }, tea.Quit)
	case "enter":
		if m.landingCursor == 1 {
			// Open Existing: read-only catalog load.
			m.view = launcherViewOpenExisting
			m.openExistingRegistered = config.ListRegisteredProjects(m.globalDirPath)
			m.projects = NewLauncherProjectsModel(m.globalDirPath, ProjectsContext{})
			cmd := m.projects.Init()
			var sizeCmd tea.Cmd
			if m.width > 0 {
				updated, c := m.projects.Update(tea.WindowSizeMsg{Width: m.width, Height: m.height})
				m.projects = updated
				sizeCmd = c
			}
			return m, tea.Batch(cmd, sizeCmd)
		}
		// Create new project.
		if len(m.unfinishedStaging) > 0 && !m.showUnfinishedStaging {
			m.view = launcherViewOpenExisting // reuse the same key handling surface
			m.showUnfinishedStaging = true
			m.unfinishedCursor = 0
			return m, nil
		}
		return m.enterCreate()
	}
	return m, nil
}

// enterCreate constructs the draft-purpose FirstRunModel. hasPresets is a
// pure read (preset.HasAny stats ~/.lingtai-tui/presets/, creating
// nothing) so it stays inside the zero-write contract.
//
// Captures preDraftTheme/preDraftLanguage from the persisted TUI config
// BEFORE constructing firstRun — config.LoadTUIConfig is a pure read (falls
// back to defaults if tui_config.json doesn't exist yet, never writes) —
// so a subsequent ProjectDraftCancelledMsg can restore exactly this
// baseline regardless of what the wizard's Welcome step previewed
// in-memory afterward.
func (m LauncherRootModel) enterCreate() (tea.Model, tea.Cmd) {
	m.draft = NewProjectDraft(m.projectRoot)
	m.view = launcherViewCreate
	baseline := config.LoadTUIConfig(m.globalDirPath)
	m.preDraftTheme = baseline.Theme
	m.preDraftLanguage = baseline.Language
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
		m.showUnfinishedStaging = false
		m.view = launcherViewLanding
		return m, nil
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
			m.showUnfinishedStaging = false
			return m.enterCreate()
		}
		return m, nil
	case "c":
		// Continue to Create anyway, leaving the leftover staging in
		// place untouched.
		m.showUnfinishedStaging = false
		return m.enterCreate()
	}
	return m, nil
}

func (m LauncherRootModel) updateOpenExisting(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc", "q":
		if m.openExistingSection == 1 {
			// Let the embedded ProjectsModel's own esc/q return path run
			// first if it's mid-interaction; otherwise fall through to
			// landing. ProjectsModel's Update returns a ViewChangeMsg
			// command on esc/q, which this model doesn't route anywhere
			// (it isn't part of App's view graph) — so intercept directly
			// here instead of forwarding.
		}
		m.view = launcherViewLanding
		return m, nil
	case "tab":
		if m.openExistingSection == 0 && len(m.openExistingRegistered) > 0 {
			m.openExistingSection = 1
		} else {
			m.openExistingSection = 0
		}
		return m, nil
	}
	if m.openExistingSection == 0 && len(m.openExistingRegistered) > 0 {
		switch msg.String() {
		case "up", "k":
			if m.openExistingCursor > 0 {
				m.openExistingCursor--
			}
			return m, nil
		case "down", "j":
			if m.openExistingCursor < len(m.openExistingRegistered)-1 {
				m.openExistingCursor++
			}
			return m, nil
		case "enter":
			row := m.openExistingRegistered[m.openExistingCursor]
			if !row.Alive {
				return m, nil // stale/missing — disabled, not selectable
			}
			// Revalidate at the decision boundary instead of trusting the
			// liveness snapshot captured when the picker opened. A project can
			// disappear while the launcher is visible; falling through with a
			// stale root would hand a now-missing .lingtai path to the normal,
			// write-capable startup pipeline.
			root, ok := existingProjectRoot(row.Path)
			if !ok {
				m.openExistingRegistered[m.openExistingCursor].Alive = false
				m.openExistingRegistered[m.openExistingCursor].StaleReason = "missing_dir"
				return m, nil
			}
			m.result = LauncherResult{Kind: DecisionOpenExisting, ProjectRoot: root}
			m.done = true
			return m, tea.Sequence(func() tea.Msg { return LauncherDoneMsg{Result: m.result} }, tea.Quit)
		}
	}
	updated, cmd := m.projects.Update(msg)
	m.projects = updated
	return m, cmd
}

// View implements tea.Model for the root launcher program (main.go runs
// this in its own tea.Program, separate from the real App's). Bubble Tea
// v2's root Model.View returns tea.View; content composition mirrors
// App.View's structure (plain string content wrapped into a tea.View with
// alt-screen + mouse mode) without reusing App itself, since the launcher
// intentionally has no project/orchestrator context to construct an App
// with yet.
func (m LauncherRootModel) View() tea.View {
	v := tea.NewView(m.viewContent())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	t := ActiveTheme()
	if t.PaintBG {
		v.BackgroundColor = t.BG
		v.ForegroundColor = t.Text
	}
	v.ReportFocus = true
	return v
}

func (m LauncherRootModel) viewContent() string {
	switch m.view {
	case launcherViewLanding:
		return m.viewLanding()
	case launcherViewOpenExisting:
		if m.showUnfinishedStaging {
			return m.viewUnfinishedStaging()
		}
		return m.viewOpenExisting()
	case launcherViewCreate:
		out := m.firstRun.View()
		if m.createErr != "" {
			out += "\n  " + lipgloss.NewStyle().Bold(true).Foreground(ColorSuspended).Render(i18n.TF("launcher.create.failed", m.createErr)) + "\n"
		}
		return out
	}
	return ""
}

func (m LauncherRootModel) viewLanding() string {
	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
	b.WriteString("\n  " + titleStyle.Render(i18n.T("launcher.landing.title")) + "\n")
	b.WriteString("  " + StyleFaint.Render(i18n.T("launcher.landing.subtitle")) + "\n\n")
	b.WriteString("  " + i18n.T("launcher.landing.question") + "\n\n")

	options := []struct{ label, desc string }{
		{i18n.T("launcher.landing.create"), i18n.T("launcher.landing.create_desc")},
		{i18n.T("launcher.landing.open"), i18n.T("launcher.landing.open_desc")},
	}
	for i, opt := range options {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.landingCursor {
			cursor = "> "
			style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
		}
		b.WriteString(cursor + style.Render(opt.label) + "\n")
		b.WriteString("    " + StyleFaint.Render(opt.desc) + "\n")
	}

	b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorActive).Render(i18n.T("launcher.landing.status")) + "\n")
	b.WriteString(StyleFaint.Render("  ↑↓ "+i18n.T("welcome.select_lang")+"  [Enter] "+i18n.T("welcome.confirm")+"  [Esc] "+i18n.T("firstrun.back")) + "\n")
	return b.String()
}

func (m LauncherRootModel) viewOpenExisting() string {
	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
	b.WriteString("\n  " + titleStyle.Render(i18n.T("launcher.open_existing.title")) + "\n\n")

	if len(m.openExistingRegistered) > 0 {
		b.WriteString("  " + StyleFaint.Render(i18n.T("launcher.open_existing.registered_header")) + "\n")
		for i, row := range m.openExistingRegistered {
			cursor := "  "
			style := lipgloss.NewStyle().Foreground(ColorText)
			if m.openExistingSection == 0 && i == m.openExistingCursor {
				cursor = "> "
				style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
			}
			line := row.Path
			if !row.Alive {
				style = lipgloss.NewStyle().Foreground(ColorTextFaint)
				line += "  (" + i18n.T("launcher.open_existing.disabled_"+row.StaleReason) + ")"
			}
			b.WriteString(cursor + style.Render(line) + "\n")
		}
		b.WriteString("\n")
	}

	b.WriteString("  " + StyleFaint.Render(i18n.T("launcher.open_existing.running_header")) + "\n")
	b.WriteString(m.projects.View())

	b.WriteString("\n  " + lipgloss.NewStyle().Foreground(ColorActive).Render(i18n.T("launcher.landing.status")) + "\n")
	return b.String()
}

func (m LauncherRootModel) viewUnfinishedStaging() string {
	var b strings.Builder
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorSuspended)
	b.WriteString("\n  " + titleStyle.Render(i18n.T("launcher.staging.title")) + "\n\n")
	b.WriteString("  " + i18n.T("launcher.staging.hint") + "\n\n")
	for i, dir := range m.unfinishedStaging {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.unfinishedCursor {
			cursor = "> "
			style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
		}
		b.WriteString(cursor + style.Render(dir) + "\n")
	}
	if m.unfinishedDiscardStatus != "" {
		b.WriteString("\n  " + m.unfinishedDiscardStatus + "\n")
	}
	b.WriteString("\n" + StyleFaint.Render("  [d] "+i18n.T("launcher.staging.discard")+
		"  [r] "+i18n.T("launcher.staging.resume")+
		"  [c] "+i18n.T("launcher.staging.continue")+
		"  [Esc] "+i18n.T("firstrun.back")) + "\n")
	return b.String()
}

// existingProjectRoot performs the final pure validation for an Open
// Existing decision. It returns a clean absolute project root only while
// <root>/.lingtai still resolves to a directory; no mutation, pruning, or
// migration is allowed at this boundary.
func existingProjectRoot(root string) (string, bool) {
	if strings.TrimSpace(root) == "" {
		return "", false
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return "", false
	}
	abs = filepath.Clean(abs)
	info, err := os.Stat(filepath.Join(abs, ".lingtai"))
	if err != nil || !info.IsDir() {
		return "", false
	}
	return abs, true
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
