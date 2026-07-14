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
	"github.com/anthropics/lingtai-tui/internal/inventory"
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
// Ctrl+C at the welcome or choose page) — zero filesystem writes occurred.
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

// launcherView is the launcher's own tiny navigation graph:
//
//	Welcome ⇄ Choose ⇄ (Picker | Staging | Create)
//
// Welcome is always first (Jason's redesign direction: a no-project user
// meets LingTai through the SAME welcome visual language as first-run —
// brand, explanation, language — BEFORE being asked to decide anything).
// Choose is the explicit create-here / open-existing decision. Picker is
// the redesigned project-level "open existing" catalog. Staging is the
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
var launcherLangLabels = []string{"English", "现代汉语", "文言"}

// launcherProjectRow is one project-level row of the redesigned "open an
// existing project" picker. The unit of choice on that page is a PROJECT
// (a root the normal startup pipeline can be handed), never an agent —
// which is why the picker no longer embeds the agent-level ProjectsModel.
// Rows are merged from two read-only sources and deduplicated by
// normalized root path: the running-agent inventory snapshot and
// registry.jsonl (config.ListRegisteredProjects — never LoadAndPrune).
type launcherProjectRow struct {
	Name       string // basename of the project root
	Path       string // project root (parent of .lingtai/)
	Running    bool   // present in the process-table inventory snapshot
	AgentCount int    // running agent count (Running rows only)
	Registered bool   // present in registry.jsonl
	Missing    bool   // root's .lingtai is gone (stale registry row or phantom process)
}

// pickerViewportUnit is the smallest unit the picker may put in or take out
// of its viewport. A project unit owns both its name and path lines; a header
// is a separate unit so scrolling can omit it without ever splitting a
// project pair. The rendered lines already include their display styles.
type pickerViewportUnit struct {
	lines         []string
	isProject     bool
	isGroupHeader bool
}

// launcherScanMsg delivers one async inventory scan result to the picker.
// seq must match the picker's current scan sequence or the result is
// dropped — same staleness rule ProjectsModel uses for its catalog, kept
// here in miniature since the launcher owns its own (much smaller) list.
type launcherScanMsg struct {
	seq      uint64
	snapshot inventory.Snapshot
	err      string
}

// LauncherRootModel is the pre-App root Bubble Tea model for the no-project
// case (design doc Invariant 2/6): it owns ONLY view state, a read-only
// project catalog, and (during Create) a *ProjectDraft. It never runs
// migration/bootstrap and never touches the filesystem except through
// explicit read-only calls (config.LoadTUIConfig, config.ListRegisteredProjects,
// inventory.Scan) until the user reaches stepReview and presses
// "Start project", at which point RunProjectCreate performs the single
// staging→validate→rename commit.
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

	// Picker state (see launcherProjectRow). pickerScanSeq guards stale
	// async scan results; pickerStatus is a transient localized feedback
	// line (missing row activated, project vanished, scan error detail).
	pickerRegistered []config.RegisteredProject
	pickerRows       []launcherProjectRow
	pickerCursor     int
	pickerScanSeq    uint64
	pickerScanning   bool
	pickerScanErr    string
	pickerStatus     string

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
		return m, nil

	case launcherScanMsg:
		if msg.seq != m.pickerScanSeq {
			return m, nil // stale scan result from a superseded request
		}
		m.pickerScanning = false
		m.pickerScanErr = msg.err
		if msg.err == "" {
			m.pickerRows = buildLauncherProjectRows(m.pickerRegistered, msg.snapshot)
		} else {
			// Scan failed: the registered catalog is still honest on its
			// own — show it, plus the error, rather than a blank page.
			m.pickerRows = buildLauncherProjectRows(m.pickerRegistered, inventory.Snapshot{})
		}
		if m.pickerCursor >= len(m.pickerRows) {
			m.pickerCursor = max(0, len(m.pickerRows)-1)
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
			updated, cmd := m.firstRun.Update(msg)
			m.firstRun = updated
			return m, cmd
		}
		return m, nil
	}

	// Forward everything else (mouse wheel, paste, sub-model async
	// messages) to the create wizard when it owns the active view.
	if m.view == launcherViewCreate && m.firstRunOn {
		updated, cmd := m.firstRun.Update(msg)
		m.firstRun = updated
		return m, cmd
	}
	return m, nil
}

// cancelAndQuit records the zero-write cancel decision and quits the
// launcher's tea.Program. Reachable from Welcome (Esc/q/Ctrl+C) and from
// Choose, Picker, and Staging (q/Ctrl+C); Esc on those pages goes BACK one
// page instead, so leaving is always deliberate and never a mis-keyed Esc.
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

// enterPicker loads the read-only project catalog: registry.jsonl rows
// immediately (config.ListRegisteredProjects — never LoadAndPrune, which
// rewrites the file), plus an async process-table inventory scan for
// running projects. Both are pure reads; the picker stays inside the
// zero-write contract.
func (m LauncherRootModel) enterPicker() (tea.Model, tea.Cmd) {
	m.view = launcherViewPicker
	m.pickerStatus = ""
	m.pickerScanErr = ""
	m.pickerCursor = 0
	m.pickerRegistered = config.ListRegisteredProjects(m.globalDirPath)
	m.pickerRows = buildLauncherProjectRows(m.pickerRegistered, inventory.Snapshot{})
	m.pickerScanning = true
	m.pickerScanSeq++
	return m, m.scanRunningProjects(m.pickerScanSeq)
}

// scanRunningProjects runs the shared inventory scan (same
// projectsScanInventory seam ProjectsModel uses, so tests inject fakes the
// same way) off the Update loop and reports back with the request's seq.
func (m LauncherRootModel) scanRunningProjects(seq uint64) tea.Cmd {
	return func() tea.Msg {
		snap, err := projectsScanInventory(inventory.Options{SelfPID: os.Getpid()})
		if err != nil {
			return launcherScanMsg{seq: seq, err: err.Error()}
		}
		return launcherScanMsg{seq: seq, snapshot: snap}
	}
}

// buildLauncherProjectRows merges the running-project inventory snapshot
// with the registered-project catalog into ONE deduplicated project-level
// list: running projects first (in snapshot order, carrying their agent
// count and, when also registered, the Registered flag), then registered
// projects that are not currently running (registry order), with rows whose
// .lingtai is gone marked Missing rather than hidden — the registry is
// never pruned here, so hiding them would misreport what's on disk.
// Deduplication is by inventory.NormalizePath so a relative or uncleaned
// registry path still matches its running snapshot twin.
func buildLauncherProjectRows(registered []config.RegisteredProject, snap inventory.Snapshot) []launcherProjectRow {
	seen := map[string]int{}
	var rows []launcherProjectRow
	for _, g := range snap.Groups {
		if g.Project == "" {
			continue
		}
		key := inventory.NormalizePath(g.Project)
		if idx, ok := seen[key]; ok {
			rows[idx].AgentCount += len(g.Records)
			continue
		}
		seen[key] = len(rows)
		rows = append(rows, launcherProjectRow{
			Name:       filepath.Base(g.Project),
			Path:       g.Project,
			Running:    true,
			AgentCount: len(g.Records),
			// A phantom group means processes claim a project whose
			// .lingtai no longer resolves — honest state: running AND
			// missing, disabled with the same reason as a stale
			// registry row.
			Missing: g.Phantom,
		})
	}
	for _, rp := range registered {
		key := inventory.NormalizePath(rp.Path)
		if idx, ok := seen[key]; ok {
			rows[idx].Registered = true
			continue
		}
		seen[key] = len(rows)
		rows = append(rows, launcherProjectRow{
			Name:       filepath.Base(rp.Path),
			Path:       rp.Path,
			Registered: true,
			Missing:    !rp.Alive,
		})
	}
	return rows
}

func (m LauncherRootModel) updatePicker(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.view = launcherViewChoose
		return m, nil
	case "q", "ctrl+c":
		return m.cancelAndQuit()
	case "up", "k":
		if m.pickerCursor > 0 {
			m.pickerCursor--
			m.pickerStatus = ""
		}
		return m, nil
	case "down", "j":
		if m.pickerCursor < len(m.pickerRows)-1 {
			m.pickerCursor++
			m.pickerStatus = ""
		}
		return m, nil
	case "r", "ctrl+r":
		m.pickerStatus = ""
		m.pickerScanErr = ""
		m.pickerRegistered = config.ListRegisteredProjects(m.globalDirPath)
		m.pickerRows = buildLauncherProjectRows(m.pickerRegistered, inventory.Snapshot{})
		if m.pickerCursor >= len(m.pickerRows) {
			m.pickerCursor = max(0, len(m.pickerRows)-1)
		}
		m.pickerScanning = true
		m.pickerScanSeq++
		return m, m.scanRunningProjects(m.pickerScanSeq)
	case "enter":
		if m.pickerCursor >= len(m.pickerRows) {
			return m, nil
		}
		row := m.pickerRows[m.pickerCursor]
		if row.Missing {
			m.pickerStatus = i18n.T("launcher.picker.missing_blocked")
			return m, nil
		}
		// Revalidate at the decision boundary instead of trusting the
		// snapshot captured when the picker opened (or last rescanned). A
		// project can disappear while the launcher is visible; falling
		// through with a stale root would hand a now-missing .lingtai
		// path to the normal, write-capable startup pipeline.
		root, ok := existingProjectRoot(row.Path)
		if !ok {
			m.pickerRows[m.pickerCursor].Missing = true
			m.pickerStatus = i18n.T("launcher.picker.gone")
			return m, nil
		}
		m.result = LauncherResult{Kind: DecisionOpenExisting, ProjectRoot: root}
		m.done = true
		return m, tea.Sequence(func() tea.Msg { return LauncherDoneMsg{Result: m.result} }, tea.Quit)
	}
	return m, nil
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
	case launcherViewWelcome:
		return m.viewWelcome()
	case launcherViewChoose:
		return m.viewChoose()
	case launcherViewPicker:
		return m.viewPicker()
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

// viewWelcome renders the launcher's prelude in the SAME visual language as
// the first-run welcome page (renderWelcomeBrand: braille logo, product
// name, poem — single-sourced with firstrun.go's viewWelcome so the two
// cannot drift), followed by a short explanation of what LingTai is and
// what this launcher will (not) do, the standard centered language
// selector, and keyboard hints. Everything here is an in-memory preview;
// the page states so explicitly.
func (m LauncherRootModel) viewWelcome() string {
	width := launcherTextWidth(m.width, 4)
	compact := m.height > 0 && m.height <= 24
	var lines []string
	lines = append(lines, strings.Split(strings.TrimSuffix(launcherWelcomeBrand(m.width, m.height), "\n"), "\n")...)
	if !compact {
		lines = append(lines, "")
	}

	appendCenteredWrapped := func(text string, style lipgloss.Style) {
		for _, line := range wrapToWidth(text, width) {
			lines = append(lines, centerText(style.Render(line), m.width))
		}
	}
	appendCenteredWrapped(i18n.T("launcher.welcome.explain1"), StyleSubtle)
	if !compact {
		lines = append(lines, "")
	}
	displayRoot := truncatePathToWidth(abbreviateHomePath(m.projectRoot), launcherTextWidth(m.width, 20))
	appendCenteredWrapped(i18n.TF("launcher.welcome.explain2", displayRoot), StyleSubtle)

	if !compact {
		lines = append(lines, "")
	}
	for i, label := range launcherLangLabels {
		style := lipgloss.NewStyle().Foreground(ColorText)
		line := " " + style.Render(label) + " "
		if i == m.langIdx {
			style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
			line = style.Render("[" + label + "]")
		}
		lines = append(lines, centerText(line, m.width))
	}
	if !compact {
		lines = append(lines, "")
	}
	appendCenteredWrapped(i18n.T("launcher.zero_write_status"), lipgloss.NewStyle().Foreground(ColorActive))
	if !compact {
		lines = append(lines, "")
	}
	hints := "↑↓ " + i18n.T("welcome.select_lang") +
		"  [Enter] " + i18n.T("launcher.welcome.continue") +
		"  [Ctrl+T] " + i18n.T("settings.theme") +
		"  [Esc/q/Ctrl+C] " + i18n.T("launcher.hint_quit")
	for _, line := range wrapToWidth(hints, width) {
		lines = append(lines, centerText(StyleFaint.Render(line), m.width))
	}

	return verticallyCentered(strings.Join(lines, "\n")+"\n", m.height)
}

// launcherWelcomeBrand keeps the shared welcome identity while reducing its
// fixed-height logo/blank block when the launcher has a small terminal. The
// language, safety statement, and key hints are never dropped.
func launcherWelcomeBrand(width, height int) string {
	brand := strings.TrimRight(renderWelcomeBrand(width), "\n")
	if height <= 0 || height > 24 {
		return brand + "\n"
	}
	nonBlank := make([]string, 0)
	for _, line := range strings.Split(brand, "\n") {
		if strings.TrimSpace(line) != "" {
			nonBlank = append(nonBlank, line)
		}
	}
	logoRows := 4
	if height < 20 {
		logoRows = 0
	} else if height < 24 {
		logoRows = 2
	}
	if len(nonBlank) < 3 {
		return brand + "\n"
	}
	if logoRows > len(nonBlank)-3 {
		logoRows = len(nonBlank) - 3
	}
	selected := append([]string{}, nonBlank[:logoRows]...)
	selected = append(selected, nonBlank[len(nonBlank)-3:]...)
	return strings.Join(selected, "\n") + "\n"
}

// viewChoose renders the explicit start-here / open-existing decision as a
// centered block in the welcome page's visual family. Option copy states
// consequences (what will and will not be written, and when), not UI
// mechanics; the zero-write status line stays visible until a real
// decision is made.
func (m LauncherRootModel) viewChoose() string {
	var lines []string
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAgent)
	lines = append(lines, titleStyle.Render(i18n.T("launcher.choose.title")))
	cwd := truncatePathToWidth(abbreviateHomePath(m.projectRoot), launcherTextWidth(m.width, 12))
	lines = append(lines, StyleSubtle.Render(i18n.TF("launcher.choose.cwd", cwd)))
	lines = append(lines, "")

	options := []struct{ label, desc string }{
		{i18n.T("launcher.choose.here"), i18n.T("launcher.choose.here_desc")},
		{i18n.T("launcher.choose.open"), i18n.T("launcher.choose.open_desc")},
	}
	for i, opt := range options {
		cursor := "  "
		style := lipgloss.NewStyle().Foreground(ColorText)
		if i == m.chooseCursor {
			cursor = "> "
			style = lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
		}
		label := truncatePathToWidth(opt.label, launcherTextWidth(m.width, lipgloss.Width(cursor)+2))
		lines = append(lines, cursor+style.Render(label))
		for _, dl := range wrapToWidth(opt.desc, chooseDescWidth(m.width)) {
			lines = append(lines, "    "+StyleFaint.Render(dl))
		}
		if i == 0 {
			lines = append(lines, "")
		}
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(ColorActive).Render(i18n.T("launcher.zero_write_status")))
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

// chooseDescWidth bounds the option-description wrap width so the choose
// block stays a readable column instead of one screen-wide line.
func chooseDescWidth(width int) int {
	w := launcherTextWidth(width, 4)
	if w > 64 {
		w = 64
	}
	return w
}

// viewPicker renders the redesigned "open an existing project" catalog:
// one deduplicated project-level list under two grouped section headers
// (running first, then registered-but-stopped), each row a name line with
// status and a dim path line beneath it. Missing rows stay visible but
// disabled with an explicit reason. The page is top-aligned like every
// other list screen in the TUI, with a title bar, a scrollable body
// windowed around the cursor, and a persistent keyboard-help footer.
func (m LauncherRootModel) viewPicker() string {
	titleLines := []string{
		StyleTitle.Render("  " + truncatePathToWidth(i18n.T("launcher.picker.title"), launcherTextWidth(m.width, 2))),
		strings.Repeat("─", max(0, m.width)),
	}
	units, selectedUnit := m.pickerViewportUnits()
	footerLines := m.pickerFooterLines()

	// Reserve the title and every physically wrapped footer/status line before
	// selecting the body window. The body is selected as whole units, never as
	// a slice of physical lines.
	avail := m.height - len(titleLines) - len(footerLines)
	if avail < 1 {
		avail = 1
	}
	window := selectPickerViewportUnits(units, selectedUnit, avail)
	bodyLines := flattenPickerUnits(window)
	if len(bodyLines) > avail {
		// This only occurs below the supported smallest terminal sizes, where a
		// two-line project unit cannot physically fit. Normal terminal sizes
		// always retain the selected pair atomically.
		bodyLines = bodyLines[:avail]
	}
	for len(bodyLines) < avail {
		bodyLines = append(bodyLines, "")
	}

	lines := append(append([]string{}, titleLines...), bodyLines...)
	lines = append(lines, footerLines...)
	return strings.Join(lines, "\n")
}

func (m LauncherRootModel) pickerFooterLines() []string {
	lines := []string{strings.Repeat("─", max(0, m.width))}
	hint := "  ↑↓ " + i18n.T("welcome.select_lang") +
		"  [Enter] " + i18n.T("launcher.hint_open") +
		"  [r] " + i18n.T("launcher.hint_rescan") +
		"  [Esc] " + i18n.T("launcher.hint_back") +
		"  [q/Ctrl+C] " + i18n.T("launcher.hint_quit")
	for _, line := range wrapToWidth(hint, launcherTextWidth(m.width, 2)) {
		lines = append(lines, StyleFaint.Render(line))
	}
	status := ""
	switch {
	case m.pickerStatus != "":
		status = RuneBullet + " " + m.pickerStatus
	case m.pickerScanning:
		status = RuneBullet + " " + i18n.T("launcher.picker.scanning")
	case m.pickerScanErr != "":
		status = RuneBullet + " " + i18n.T("launcher.picker.scan_error")
	}
	if status != "" {
		style := StyleFaint
		if m.pickerStatus != "" || m.pickerScanErr != "" {
			style = lipgloss.NewStyle().Foreground(ColorStuck)
		}
		for _, line := range wrapToWidth(status, launcherTextWidth(m.width, 2)) {
			lines = append(lines, "  "+style.Render(line))
		}
	}
	return lines
}

// pickerViewportUnits projects the catalog into atomic viewport units. Header
// units may be omitted when the viewport is tight, but a project unit always
// contains the name and path together.
func (m LauncherRootModel) pickerViewportUnits() ([]pickerViewportUnit, int) {
	sectionStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	nameStyle := lipgloss.NewStyle().Foreground(ColorText)
	selectedStyle := lipgloss.NewStyle().Bold(true).Foreground(ColorAccent)
	missingStyle := lipgloss.NewStyle().Foreground(ColorTextFaint)
	pathStyle := StyleFaint
	runningStyle := lipgloss.NewStyle().Foreground(ColorActive)

	units := make([]pickerViewportUnit, 0, len(m.pickerRows)*2+2)
	selectedUnit := -1
	appendHeader := func(text string) {
		units = append(units, pickerViewportUnit{
			lines:         []string{"  " + sectionStyle.Render(text)},
			isGroupHeader: true,
		})
	}
	if len(m.pickerRows) == 0 {
		lines := []string{""}
		if m.pickerScanning {
			lines = append(lines, "  "+StyleFaint.Render(i18n.T("launcher.picker.scanning")))
		} else {
			lines = append(lines, "  "+StyleFaint.Render(i18n.T("launcher.picker.empty")))
			lines = append(lines, "  "+StyleFaint.Render(i18n.T("launcher.picker.empty_hint")))
		}
		return []pickerViewportUnit{{lines: lines}}, -1
	}

	renderedRunningHeader := false
	renderedRegisteredHeader := false
	for i, row := range m.pickerRows {
		if row.Running && !renderedRunningHeader {
			appendHeader(i18n.T("launcher.picker.running"))
			renderedRunningHeader = true
		}
		if !row.Running && !renderedRegisteredHeader {
			appendHeader(i18n.T("launcher.picker.registered"))
			renderedRegisteredHeader = true
		}

		cursor := "  "
		style := nameStyle
		if i == m.pickerCursor {
			cursor = "> "
			style = selectedStyle
		}
		if row.Missing {
			style = missingStyle
			if i == m.pickerCursor {
				style = missingStyle.Bold(true)
			}
		}

		var badgeText []string
		var badgeStyles []lipgloss.Style
		if row.Running && row.AgentCount > 0 {
			label := i18n.TF("launcher.picker.agents", row.AgentCount)
			if row.AgentCount == 1 {
				label = i18n.T("launcher.picker.agents_one")
			}
			badgeText = append(badgeText, "● "+label)
			badgeStyles = append(badgeStyles, runningStyle)
		}
		if row.Missing {
			badgeText = append(badgeText, i18n.T("launcher.picker.missing"))
			badgeStyles = append(badgeStyles, missingStyle)
		}
		namePrefix := "  " + cursor
		badgeSuffix := ""
		if len(badgeText) > 0 {
			badgeSuffix = "  " + strings.Join(badgeText, "  ")
		}
		nameWidth := launcherTextWidth(m.width, lipgloss.Width(namePrefix)+lipgloss.Width(badgeSuffix))
		nameLine := namePrefix + style.Render(truncatePathToWidth(row.Name, nameWidth))
		for j, badge := range badgeText {
			nameLine += "  " + badgeStyles[j].Render(badge)
		}
		pathPrefix := "      "
		projectUnit := pickerViewportUnit{
			lines: []string{
				nameLine,
				pathPrefix + pathStyle.Render(truncatePathToWidth(abbreviateHomePath(row.Path), launcherTextWidth(m.width, lipgloss.Width(pathPrefix)))),
			},
			isProject: true,
		}
		if i == m.pickerCursor {
			selectedUnit = len(units)
		}
		units = append(units, projectUnit)
	}

	// Honest empty-group notes: when one source has entries and the other
	// has none, say so rather than leaving an unexplained gap.
	if !renderedRunningHeader && !m.pickerScanning && m.pickerScanErr == "" {
		appendHeader(i18n.T("launcher.picker.running"))
		units = append(units, pickerViewportUnit{lines: []string{"  " + StyleFaint.Render(i18n.T("launcher.picker.none_running"))}})
	}
	return units, selectedUnit
}

func flattenPickerUnits(units []pickerViewportUnit) []string {
	var lines []string
	for _, unit := range units {
		lines = append(lines, unit.lines...)
	}
	return lines
}

// selectPickerViewportUnits chooses a contiguous group-aligned unit window
// around the selected project. It maximizes visible project rows first, then
// physical use of the reserved body, while preferring a balanced window and
// retaining headers where there is room.
func selectPickerViewportUnits(units []pickerViewportUnit, selected, avail int) []pickerViewportUnit {
	if len(units) == 0 {
		return nil
	}
	if selected < 0 || selected >= len(units) {
		selected = 0
		for i, unit := range units {
			if unit.isProject {
				selected = i
				break
			}
		}
	}
	bestStart, bestEnd := -1, -1
	bestProjects, bestLines, bestImbalance, bestHeaders := -1, -1, 0, -1
	for start := 0; start <= selected; start++ {
		used := 0
		for end := start; end < len(units); end++ {
			used += len(units[end].lines)
			if used > avail {
				break
			}
			if selected < start || selected >= end+1 {
				continue
			}
			projects, headers := 0, 0
			for _, unit := range units[start : end+1] {
				if unit.isProject {
					projects++
				}
				if unit.isGroupHeader {
					headers++
				}
			}
			left, right := selected-start, end-selected
			imbalance := left - right
			if imbalance < 0 {
				imbalance = -imbalance
			}
			if projects > bestProjects ||
				(projects == bestProjects && used > bestLines) ||
				(projects == bestProjects && used == bestLines && imbalance < bestImbalance) ||
				(projects == bestProjects && used == bestLines && imbalance == bestImbalance && headers > bestHeaders) {
				bestStart, bestEnd = start, end+1
				bestProjects, bestLines, bestImbalance, bestHeaders = projects, used, imbalance, headers
			}
		}
	}
	if bestStart >= 0 {
		return units[bestStart:bestEnd]
	}
	// Only a pathological body budget can be smaller than one unit. Keep the
	// selected unit as the honest fallback; viewPicker applies the final hard
	// clip only for such unsupported terminal sizes.
	return units[selected : selected+1]
}

// renderPickerBody renders the complete grouped body and reports which
// physical line the cursor's name line occupies. Viewport selection itself is
// unit-based in viewPicker; this helper remains useful to tests and diagnostics.
func (m LauncherRootModel) renderPickerBody() (string, int) {
	units, selected := m.pickerViewportUnits()
	lines := flattenPickerUnits(units)
	cursorLine := -1
	if selected >= 0 {
		cursorLine = 0
		for i := 0; i < selected; i++ {
			cursorLine += len(units[i].lines)
		}
	}
	return strings.Join(lines, "\n"), cursorLine
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
