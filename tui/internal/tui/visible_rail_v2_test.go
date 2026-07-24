package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

const (
	visibleRailV2Width = 24
	visibleRailV2Human = "visible-v2/human"
)

type visibleRailV2Fixture struct {
	app       App
	root      string
	lingtai   string
	humanDir  string
	targets   []fs.DirectTarget
	statePath string
}

type visibleRailV2RailObservation struct {
	present      bool
	focused      bool
	scrollOffset int
	unread       map[string]int
}

type visibleRailV2StateObservation struct {
	rows              []agentSelectorRow
	cursor            int
	selectedThreadKey string
	inputFocused      bool
	inputValue        string
	directTarget      fs.DirectTarget
	directCurrent     bool
	rail              visibleRailV2RailObservation
}

func visibleRailV2Apply(app App, msg tea.Msg) (App, tea.Cmd) {
	model, cmd := app.Update(msg)
	return model.(App), cmd
}

func visibleRailV2CollectCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var messages []tea.Msg
		for _, child := range batch {
			messages = append(messages, visibleRailV2CollectCmd(child)...)
		}
		return messages
	}
	return []tea.Msg{msg}
}

func visibleRailV2AcceptPrepared(t *testing.T, app App) App {
	t.Helper()

	var cmd tea.Cmd
	app, cmd = visibleRailV2Apply(app, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
	if cmd == nil {
		t.Fatal("real ctrl+r issued no prepared refresh command")
	}
	raw := cmd()
	refresh, ok := raw.(mailRefreshMsg)
	if !ok {
		t.Fatalf("real ctrl+r command produced %T, want mailRefreshMsg", raw)
	}
	if refresh.refreshRequestSerial == 0 || !refresh.prepared || refresh.directPublication == nil {
		t.Fatalf("real ctrl+r completion is not the accepted V1 prepared payload: serial=%d prepared=%v publication=%p",
			refresh.refreshRequestSerial, refresh.prepared, refresh.directPublication)
	}

	var follow tea.Cmd
	app, follow = visibleRailV2Apply(app, refresh)
	unreadResults := 0
	for _, msg := range visibleRailV2CollectCmd(follow) {
		result, ok := msg.(directUnreadResultMsg)
		if !ok {
			continue
		}
		unreadResults++
		app, _ = visibleRailV2Apply(app, result)
	}
	if unreadResults == 0 {
		t.Fatal("accepted real prepared refresh did not produce the V1 direct-unread result")
	}
	if app.mail.directPublication == nil || app.mail.directUnread == nil {
		t.Fatalf("accepted prepared state incomplete: publication=%p unread=%p",
			app.mail.directPublication, app.mail.directUnread)
	}
	return app
}

func newVisibleRailV2Fixture(t *testing.T, labels []string, unreadCounts []int, width, height int, banner string) visibleRailV2Fixture {
	t.Helper()
	if len(labels) != len(unreadCounts) {
		t.Fatalf("fixture labels=%d unread counts=%d", len(labels), len(unreadCounts))
	}
	i18n.SetLang("en")

	root := t.TempDir()
	lingtai := filepath.Join(root, ".lingtai")
	humanDir := filepath.Join(lingtai, "human")
	directPerformanceWriteManifest(t, humanDir, "human", "Human", visibleRailV2Human, true)

	targets := make([]fs.DirectTarget, 0, len(labels))
	for index, label := range labels {
		agentID := fmt.Sprintf("visible-agent-%02d", index)
		target := fs.DirectTarget{
			ProjectDirectory: root,
			Directory:        filepath.Join(lingtai, agentID),
			AgentID:          agentID,
			Address:          "visible-v2/" + agentID,
		}
		directPerformanceWriteManifest(t, target.Directory, target.AgentID, label, target.Address, false)
		targets = append(targets, target)
	}

	// Baseline every safe target before publishing the accepted messages. The
	// real prepared V1 sync below then observes the messages as unread.
	_, err := fs.OpenDirectUnreadStore(root, visibleRailV2Human, targets, nil)
	if err != nil {
		t.Fatalf("seed DirectUnreadStore: %v", err)
	}
	statePath := filepath.Join(lingtai, ".tui-asset", "direct-unread.json")

	messages := make([]fs.MailMessage, 0)
	at := time.Date(2026, 7, 23, 17, 0, 0, 0, time.UTC)
	sequence := 0
	for targetIndex, count := range unreadCounts {
		for unreadIndex := 0; unreadIndex < count; unreadIndex++ {
			sequence++
			target := targets[targetIndex]
			messages = append(messages, fs.MailMessage{
				MailboxID:  fmt.Sprintf("visible-v2-%02d-%03d", targetIndex, unreadIndex),
				From:       target.Address,
				To:         visibleRailV2Human,
				Message:    fmt.Sprintf("visible V2 unread %d", sequence),
				ReceivedAt: at.Add(time.Duration(sequence) * time.Second).Format(time.RFC3339Nano),
				Identity:   map[string]interface{}{"agent_id": target.AgentID},
				Delivered:  true,
			})
		}
	}

	mail := NewMailModel(humanDir, visibleRailV2Human, lingtai, "", "Main", 200, "", "en", false, 0)
	mail.generation = 101
	mail.initialLoading = false
	mail.cache = fs.NewMailCache(humanDir)
	mail.cache.Messages = append([]fs.MailMessage(nil), messages...)
	app := App{
		currentView:   appViewMail,
		projectDir:    lingtai,
		mail:          mail,
		startupBanner: banner,
	}
	app, _ = visibleRailV2Apply(app, tea.WindowSizeMsg{Width: width, Height: height})
	app = visibleRailV2AcceptPrepared(t, app)

	return visibleRailV2Fixture{
		app:       app,
		root:      root,
		lingtai:   lingtai,
		humanDir:  humanDir,
		targets:   targets,
		statePath: statePath,
	}
}

func visibleRailV2RowIndex(t *testing.T, mail MailModel, agentID string) int {
	t.Helper()
	for index, row := range mail.agentSelector.rows {
		if agentID == "" && row.Main || !row.Main && row.Target.AgentID == agentID {
			return index
		}
	}
	t.Fatalf("canonical selector has no row for %q", agentID)
	return -1
}

// visibleRailV2ObserveRail reads only the future presentation state. The RED
// remains compile-valid before that state exists, while every test also pins
// concrete App/View/input/selector behavior rather than treating reflection
// absence as the feature proof.
func visibleRailV2ObserveRail(mail MailModel) visibleRailV2RailObservation {
	root := reflect.ValueOf(mail)
	for index := 0; index < root.NumField(); index++ {
		candidate := root.Field(index)
		if candidate.Kind() == reflect.Pointer {
			if candidate.IsNil() {
				continue
			}
			candidate = candidate.Elem()
		}
		if candidate.Kind() != reflect.Struct {
			continue
		}
		focused := candidate.FieldByName("focused")
		scroll := candidate.FieldByName("scrollOffset")
		unread := candidate.FieldByName("unreadByThread")
		if !focused.IsValid() || focused.Kind() != reflect.Bool ||
			!scroll.IsValid() || scroll.Kind() != reflect.Int ||
			!unread.IsValid() || unread.Kind() != reflect.Map {
			continue
		}
		observation := visibleRailV2RailObservation{
			present:      true,
			focused:      focused.Bool(),
			scrollOffset: int(scroll.Int()),
			unread:       make(map[string]int),
		}
		iter := unread.MapRange()
		for iter.Next() {
			if iter.Key().Kind() == reflect.String && iter.Value().Kind() == reflect.Int {
				observation.unread[iter.Key().String()] = int(iter.Value().Int())
			}
		}
		return observation
	}
	return visibleRailV2RailObservation{unread: map[string]int{}}
}

func visibleRailV2ObserveState(app App) visibleRailV2StateObservation {
	target, current := app.mail.currentDirectTarget()
	return visibleRailV2StateObservation{
		rows:              append([]agentSelectorRow(nil), app.mail.agentSelector.rows...),
		cursor:            app.mail.agentSelector.cursor,
		selectedThreadKey: app.mail.agentSelector.selectedThreadKey,
		inputFocused:      app.mail.input.Focused(),
		inputValue:        app.mail.input.Value(),
		directTarget:      target,
		directCurrent:     current,
		rail:              visibleRailV2ObserveRail(app.mail),
	}
}

func visibleRailV2FileSnapshot(t *testing.T, root string) map[string]string {
	t.Helper()
	snapshot := make(map[string]string)
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		snapshot[relative] = string(data)
		return nil
	})
	if err != nil {
		t.Fatalf("snapshot fixture files: %v", err)
	}
	return snapshot
}

func visibleRailV2RailSegments(app App) []string {
	lines := strings.Split(ansi.Strip(app.View().Content), "\n")
	budget := app.layoutBudget()
	segments := make([]string, 0, budget.ChildHeight)
	for childY := 0; childY < budget.ChildHeight; childY++ {
		lineIndex := budget.TopChromeRows + childY
		if lineIndex >= len(lines) {
			segments = append(segments, "")
			continue
		}
		segments = append(segments, ansi.Cut(lines[lineIndex], 0, visibleRailV2Width))
	}
	return segments
}

func visibleRailV2RailRow(app App, canonicalIndex int) string {
	segments := visibleRailV2RailSegments(app)
	line := 2 + canonicalIndex
	if line < 0 || line >= len(segments) {
		return ""
	}
	return segments[line]
}

func visibleRailV2RailLineContaining(app App, text string) string {
	for _, line := range visibleRailV2RailSegments(app) {
		if strings.Contains(line, text) {
			return line
		}
	}
	return ""
}

func visibleRailV2Focus(t *testing.T, app App) App {
	t.Helper()
	var cmd tea.Cmd
	app, cmd = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyTab})
	if cmd != nil {
		t.Errorf("Tab into the visible rail returned an unexpected command")
	}
	rail := visibleRailV2ObserveRail(app.mail)
	if !rail.present || !rail.focused || app.mail.input.Focused() {
		t.Errorf("Tab did not focus the visible rail and blur the shared composer: present=%v focused=%v input=%v",
			rail.present, rail.focused, app.mail.input.Focused())
	}
	return app
}

func visibleRailV2AssertCurrent(t *testing.T, app App, target fs.DirectTarget) {
	t.Helper()
	current, ok := app.mail.currentDirectTarget()
	if !ok || current.AgentID != target.AgentID ||
		app.mail.agentSelector.selectedThreadKey != fs.DirectThreadKey(target) {
		t.Errorf("current direct target = %#v (current=%v, key=%q), want canonical %q",
			current, ok, app.mail.agentSelector.selectedThreadKey, target.AgentID)
	}
}

func TestVisibleRailV2GeometryAndComposition(t *testing.T) {
	for _, test := range []struct {
		width        int
		wantVisible  bool
		wantRail     int
		wantContent  int
		wantTerminal int
	}{
		{width: 84, wantVisible: false, wantRail: 0, wantContent: 84, wantTerminal: 84},
		{width: 85, wantVisible: true, wantRail: 24, wantContent: 61, wantTerminal: 85},
		{width: 120, wantVisible: true, wantRail: 24, wantContent: 96, wantTerminal: 120},
	} {
		t.Run(fmt.Sprintf("mail-width-%d", test.width), func(t *testing.T) {
			fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, test.width, 24, "V2 ROOT CHROME")
			app := fixture.app
			budget := app.layoutBudget()

			if budget.RailVisible != test.wantVisible ||
				budget.RailWidth != test.wantRail ||
				budget.ContentWidth != test.wantContent ||
				budget.TerminalWidth != test.wantTerminal {
				t.Errorf("width %d budget = terminal/content/rail/visible %d/%d/%d/%v, want %d/%d/%d/%v",
					test.width, budget.TerminalWidth, budget.ContentWidth, budget.RailWidth, budget.RailVisible,
					test.wantTerminal, test.wantContent, test.wantRail, test.wantVisible)
			}
			if budget.TopChromeRows != 1 || budget.ChildHeight != 23 {
				t.Errorf("width %d vertical budget = top %d child %d, want top 1 child 23",
					test.width, budget.TopChromeRows, budget.ChildHeight)
			}
			if app.mail.width != test.wantContent ||
				app.mail.input.width != test.wantContent ||
				app.mail.viewport.Width() != test.wantContent {
				t.Errorf("width %d child geometry = mail/input/viewport %d/%d/%d, want one budgeted width %d",
					test.width, app.mail.width, app.mail.input.width, app.mail.viewport.Width(), test.wantContent)
			}

			if test.wantVisible {
				segments := visibleRailV2RailSegments(app)
				if len(segments) == 0 || !strings.Contains(segments[0], i18n.T("agent_rail.title")) {
					t.Errorf("width %d visible composition has no localized rail title in the left 24 cells", test.width)
				}
			}
		})
	}

	t.Run("non-mail-keeps-full-width", func(t *testing.T) {
		app := App{currentView: appViewHelp, help: NewHelpModel(), width: 120, height: 24}
		app, _ = visibleRailV2Apply(app, tea.WindowSizeMsg{Width: 120, Height: 24})
		budget := app.layoutBudget()
		if budget.RailVisible || budget.RailWidth != 0 || budget.ContentWidth != 120 || app.help.inner.width != 120 {
			t.Errorf("non-Mail 120-column geometry = visible=%v rail=%d content=%d child=%d, want false/0/120/120",
				budget.RailVisible, budget.RailWidth, budget.ContentWidth, app.help.inner.width)
		}
	})
}

func TestVisibleRailV2CanonicalRenderAndPureView(t *testing.T) {
	fixture := newVisibleRailV2Fixture(
		t,
		[]string{"Alpha", "Bravo", "This Label Is Deliberately Longer Than Twenty Four Cells"},
		[]int{12, 3, 1},
		85,
		14,
		"",
	)
	app := fixture.app
	alphaIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
	bravoIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[1].AgentID)

	var activationCmd tea.Cmd
	app.mail, activationCmd = app.mail.activateConversationRow(bravoIndex)
	if activationCmd == nil {
		t.Error("canonical V1 activation of Bravo produced no visibility command")
	}
	app.mail = app.mail.setSelectorCursor(alphaIndex)
	app = visibleRailV2Focus(t, app)

	segments := visibleRailV2RailSegments(app)
	if len(segments) < 2+len(app.mail.agentSelector.rows) {
		t.Fatalf("visible rail produced %d child rows, want at least %d", len(segments), 2+len(app.mail.agentSelector.rows))
	}
	for line, segment := range segments {
		if got := lipgloss.Width(segment); got != visibleRailV2Width {
			t.Errorf("rail line %d width = %d, want exact %d cells: %q", line, got, visibleRailV2Width, segment)
		}
	}
	if !strings.Contains(segments[0], i18n.T("agent_rail.title")) {
		t.Errorf("rail title row = %q, want localized title %q", segments[0], i18n.T("agent_rail.title"))
	}
	if got := strings.TrimSpace(segments[1]); got != strings.Repeat("─", visibleRailV2Width) {
		t.Errorf("rail separator row = %q, want 24-cell separator", got)
	}

	mainLine := visibleRailV2RailRow(app, visibleRailV2RowIndex(t, app.mail, ""))
	alphaLine := visibleRailV2RailRow(app, alphaIndex)
	bravoLine := visibleRailV2RailRow(app, bravoIndex)
	longIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[2].AgentID)
	longLine := visibleRailV2RailRow(app, longIndex)
	if !strings.HasPrefix(mainLine, "  "+i18n.T("agent_selector.main")) {
		t.Errorf("Main rail row = %q, want canonical first row with neither marker", mainLine)
	}
	if strings.HasSuffix(mainLine, " 12") || strings.HasSuffix(mainLine, " 3") || strings.HasSuffix(mainLine, " 1") {
		t.Errorf("Main rail row acquired a direct unread badge: %q", mainLine)
	}
	if !strings.HasPrefix(alphaLine, "> Alpha") || !strings.HasSuffix(alphaLine, " 12") {
		t.Errorf("cursor-only Alpha row = %q, want > marker and full decimal badge 12", alphaLine)
	}
	if !strings.HasPrefix(bravoLine, "• Bravo") || !strings.HasSuffix(bravoLine, " 3") {
		t.Errorf("current-only Bravo row = %q, want • marker and badge 3", bravoLine)
	}
	if strings.Contains(longLine, "This Label Is Deliberately Longer Than Twenty Four Cells") ||
		!strings.HasPrefix(longLine, "  This Label") || !strings.HasSuffix(longLine, " 1") {
		t.Errorf("long direct row = %q, want width-safe truncation leaving its full badge", longLine)
	}

	rail := visibleRailV2ObserveRail(app.mail)
	if rail.unread[fs.DirectThreadKey(fixture.targets[0])] != 12 ||
		rail.unread[fs.DirectThreadKey(fixture.targets[1])] != 3 ||
		rail.unread[fs.DirectThreadKey(fixture.targets[2])] != 1 {
		t.Errorf("derived accepted unread map = %#v, want Alpha=12 Bravo=3 Long=1", rail.unread)
	}

	// /agents and the rail are two presentations of the same V1 rows. V2 must
	// not restyle the canonical overlay while adding the separate rail.
	selector := ansi.Strip(app.mail.renderAgentSelector())
	canonicalSelectorWidth := app.mail.width - 4
	if canonicalSelectorWidth < 16 {
		canonicalSelectorWidth = app.mail.width
	}
	canonicalSelectorLines := []string{i18n.T("agent_selector.title")}
	for index, row := range app.mail.agentSelector.rows {
		current := row.Main && app.mail.agentSelector.selectedThreadKey == "" ||
			!row.Main && fs.DirectThreadKey(row.Target) == app.mail.agentSelector.selectedThreadKey
		prefix := selectorRowPrefix(current, index == app.mail.agentSelector.cursor)
		canonicalSelectorLines = append(canonicalSelectorLines,
			prefix+ansi.Truncate(row.Label, canonicalSelectorWidth-2, ""))
	}
	canonicalSelectorLines = append(canonicalSelectorLines, i18n.T("agent_selector.hint"))
	canonicalSelectorBox := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		Padding(0, 1).
		Width(canonicalSelectorWidth).
		Render(strings.Join(canonicalSelectorLines, "\n"))
	wantSelector := ansi.Strip(lipgloss.Place(app.mail.width, app.mail.height,
		lipgloss.Center, lipgloss.Center, canonicalSelectorBox))
	if selector != wantSelector {
		t.Error("V2 restyled the canonical /agents overlay while adding the separate rail")
	}

	app.mail = app.mail.setSelectorCursor(bravoIndex)
	bothLine := visibleRailV2RailRow(app, bravoIndex)
	if !strings.HasPrefix(bothLine, ">•Bravo") {
		t.Errorf("current+cursor Bravo row = %q, want >• marker", bothLine)
	}

	// View is a pure projection: two calls are byte-stable and cannot mutate
	// canonical navigation, presentation state, the durable store, or files.
	beforeState := visibleRailV2ObserveState(app)
	beforeFiles := visibleRailV2FileSnapshot(t, fixture.root)
	first := app.View().Content
	second := app.View().Content
	afterState := visibleRailV2ObserveState(app)
	afterFiles := visibleRailV2FileSnapshot(t, fixture.root)
	if first != second {
		t.Error("repeated App.View calls are not byte-stable")
	}
	if !reflect.DeepEqual(beforeState, afterState) {
		t.Errorf("App.View mutated navigation/presentation state:\nbefore=%#v\nafter=%#v", beforeState, afterState)
	}
	if !reflect.DeepEqual(beforeFiles, afterFiles) {
		t.Error("App.View changed fixture filesystem bytes")
	}
}

func TestVisibleRailV2KeyboardFocusAndActivation(t *testing.T) {
	newFixture := func(t *testing.T) visibleRailV2Fixture {
		return newVisibleRailV2Fixture(t, []string{"Alpha", "Bravo", "Charlie"}, []int{0, 0, 0}, 85, 16, "")
	}

	t.Run("Tab enters and Tab or Esc leaves", func(t *testing.T) {
		for _, leave := range []tea.KeyPressMsg{
			{Code: tea.KeyTab},
			{Code: tea.KeyEsc},
		} {
			fixture := newFixture(t)
			app := visibleRailV2Focus(t, fixture.app)
			app, _ = visibleRailV2Apply(app, leave)
			rail := visibleRailV2ObserveRail(app.mail)
			if rail.focused || !app.mail.input.Focused() {
				t.Errorf("%s did not return focus to the shared composer: rail=%v input=%v",
					leave.String(), rail.focused, app.mail.input.Focused())
			}
		}
	})

	t.Run("navigation moves only the canonical cursor", func(t *testing.T) {
		fixture := newFixture(t)
		app := visibleRailV2Focus(t, fixture.app)
		selected := app.mail.agentSelector.selectedThreadKey
		keys := []struct {
			key  tea.KeyPressMsg
			want int
		}{
			{key: tea.KeyPressMsg{Code: tea.KeyEnd}, want: len(app.mail.agentSelector.rows) - 1},
			{key: tea.KeyPressMsg{Code: tea.KeyHome}, want: 0},
			{key: tea.KeyPressMsg{Code: tea.KeyDown}, want: 1},
			{key: tea.KeyPressMsg{Code: 'j', Text: "j"}, want: 2},
			{key: tea.KeyPressMsg{Code: 'k', Text: "k"}, want: 1},
			{key: tea.KeyPressMsg{Code: tea.KeyUp}, want: 0},
		}
		for _, step := range keys {
			app, _ = visibleRailV2Apply(app, step.key)
			if app.mail.agentSelector.cursor != step.want {
				t.Errorf("%s canonical cursor = %d, want %d", step.key.String(), app.mail.agentSelector.cursor, step.want)
			}
			if app.mail.agentSelector.selectedThreadKey != selected {
				t.Errorf("%s activated while moving cursor: current %q -> %q",
					step.key.String(), selected, app.mail.agentSelector.selectedThreadKey)
			}
		}
	})

	for _, activation := range []tea.KeyPressMsg{
		{Code: tea.KeyEnter},
		{Code: ' ', Text: " "},
	} {
		t.Run("canonical activation by "+activation.String(), func(t *testing.T) {
			fixture := newFixture(t)
			app := visibleRailV2Focus(t, fixture.app)
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyDown})
			app, _ = visibleRailV2Apply(app, activation)
			visibleRailV2AssertCurrent(t, app, fixture.targets[0])
		})
	}

	t.Run("ordinary key and paste cannot reach composer", func(t *testing.T) {
		fixture := newFixture(t)
		app := fixture.app
		app.mail.input.SetValue("seed")
		app = visibleRailV2Focus(t, app)
		if got := app.mail.input.Value(); got != "seed" {
			t.Errorf("Tab into rail changed composer to %q", got)
		}
		app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: 'x', Text: "x"})
		app, _ = visibleRailV2Apply(app, tea.PasteMsg{Content: "PASTE-MUST-NOT-LAND"})
		if got := app.mail.input.Value(); got != "seed" {
			t.Errorf("focused rail key/paste changed composer to %q", got)
		}
	})

	t.Run("ctrl+r still uses the real prepared producer", func(t *testing.T) {
		fixture := newFixture(t)
		app := visibleRailV2Focus(t, fixture.app)
		var cmd tea.Cmd
		app, cmd = visibleRailV2Apply(app, tea.KeyPressMsg{Code: 'r', Mod: tea.ModCtrl})
		if cmd == nil {
			t.Fatal("rail-focused ctrl+r produced no refresh command")
		}
		refresh, ok := cmd().(mailRefreshMsg)
		if !ok || !refresh.prepared || refresh.refreshRequestSerial == 0 || refresh.directPublication == nil {
			t.Errorf("rail-focused ctrl+r completion = %#v, want real nonzero-serial prepared V1 publication", refresh)
		}
	})

	t.Run("editor palette selector and copy precedence", func(t *testing.T) {
		t.Run("editor warning prevents rail focus", func(t *testing.T) {
			fixture := newFixture(t)
			app := fixture.app
			app.mail.showEditorWarn = true
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyTab})
			if visibleRailV2ObserveRail(app.mail).focused {
				t.Error("Tab stole focus while the editor warning was open")
			}
		})
		t.Run("Ctrl+E opens the warning synchronously and delayed agents stays inert", func(t *testing.T) {
			fixture := newFixture(t)
			app := fixture.app
			app.mail.input.SetValue("route-affine draft")
			app, openEditorCmd := visibleRailV2Apply(app, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			if openEditorCmd != nil {
				t.Error("real Ctrl+E left an asynchronous result that can outlive its Mail route")
				// Keep exercising the old implementation after the authentic RED so the
				// competing delayed-surface assertion remains independently observable.
				app, _ = visibleRailV2Apply(app, openEditorCmd())
			}
			if !app.mail.showEditorWarn || app.mail.editorWarnText != "route-affine draft" ||
				visibleRailV2ObserveRail(app.mail).focused || !app.mail.input.Focused() {
				t.Errorf("Ctrl+E did not synchronously install the sole warning surface: warning=%v text=%q rail=%v input=%v",
					app.mail.showEditorWarn, app.mail.editorWarnText,
					visibleRailV2ObserveRail(app.mail).focused, app.mail.input.Focused())
			}

			beforeKey := app.mail.agentSelector.selectedThreadKey
			beforeCursor := app.mail.agentSelector.cursor
			beforeInput := app.mail.input.Value()
			app, _ = visibleRailV2Apply(app, PaletteSelectMsg{Command: "agents"})
			if !app.mail.showEditorWarn || app.mail.agentSelector.selectorOpen {
				t.Errorf("delayed /agents result competed with the already-visible editor warning: warning=%v selector=%v",
					app.mail.showEditorWarn, app.mail.agentSelector.selectorOpen)
			}
			if app.mail.agentSelector.selectedThreadKey != beforeKey ||
				app.mail.agentSelector.cursor != beforeCursor || app.mail.input.Value() != beforeInput {
				t.Error("delayed /agents result changed current/cursor/composer behind the editor warning")
			}

			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
			if app.mail.showEditorWarn || app.mail.agentSelector.selectorOpen ||
				visibleRailV2ObserveRail(app.mail).focused || !app.mail.input.Focused() {
				t.Errorf("one Esc did not close only the warning and restore composer routing: warning=%v selector=%v rail=%v input=%v",
					app.mail.showEditorWarn, app.mail.agentSelector.selectorOpen,
					visibleRailV2ObserveRail(app.mail).focused, app.mail.input.Focused())
			}
		})
		t.Run("palette Ctrl+E is synchronous and cannot create a second surface", func(t *testing.T) {
			fixture := newFixture(t)
			app := fixture.app
			app.mail.input.SetValue("/agents")
			app.mail.palette.SetFilter("agents")
			if !app.mail.input.IsPaletteActive() {
				t.Fatal("/agents did not activate the real Mail palette")
			}

			app, openEditorCmd := visibleRailV2Apply(app, tea.KeyPressMsg{Code: 'e', Mod: tea.ModCtrl})
			if openEditorCmd != nil {
				t.Error("palette-active Ctrl+E left an asynchronous result that can outlive its Mail route")
			}
			if !app.mail.showEditorWarn || app.mail.editorWarnText != "/agents" {
				t.Errorf("palette-active Ctrl+E did not synchronously preserve exact text: warning=%v text=%q",
					app.mail.showEditorWarn, app.mail.editorWarnText)
			}

			// On the old implementation Ctrl+E and palette Enter produced independent
			// commands. Preserve that real reverse delivery order for the authentic RED:
			// /agents opens first, then the delayed editor result arrives second.
			if openEditorCmd != nil {
				var selectCmd tea.Cmd
				app, selectCmd = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
				if selectCmd == nil {
					t.Fatal("old palette-active path produced no real /agents selection command")
				}
				for _, msg := range visibleRailV2CollectCmd(selectCmd) {
					app, _ = visibleRailV2Apply(app, msg)
				}
				app, _ = visibleRailV2Apply(app, openEditorCmd())
			} else {
				// A previously queued /agents result must stay inert once the synchronous
				// warning owns Mail.
				app, _ = visibleRailV2Apply(app, PaletteSelectMsg{Command: "agents"})
			}
			if !app.mail.showEditorWarn || app.mail.agentSelector.selectorOpen ||
				app.mail.editorWarnText != "/agents" {
				t.Errorf("palette Ctrl+E delivery order left competing surfaces: warning=%v selector=%v text=%q",
					app.mail.showEditorWarn, app.mail.agentSelector.selectorOpen, app.mail.editorWarnText)
			}

			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
			if app.mail.showEditorWarn || app.mail.agentSelector.selectorOpen {
				t.Errorf("first Esc did not close the sole warning surface: warning=%v selector=%v",
					app.mail.showEditorWarn, app.mail.agentSelector.selectorOpen)
			}
			// The preserved slash text may reopen its base palette after warning cancel;
			// close that palette before proving there is no delayed cross-route editor.
			if app.mail.input.IsPaletteActive() {
				app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
			}
			if app.mail.showEditorWarn || app.mail.agentSelector.selectorOpen ||
				app.mail.input.IsPaletteActive() || !app.mail.input.Focused() {
				t.Errorf("palette Ctrl+E did not restore a single composer owner: warning=%v selector=%v palette=%v input=%v",
					app.mail.showEditorWarn, app.mail.agentSelector.selectorOpen,
					app.mail.input.IsPaletteActive(), app.mail.input.Focused())
			}
			app = visibleRailV2Focus(t, app)
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyDown})
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyEnter})
			visibleRailV2AssertCurrent(t, app, fixture.targets[0])
			if app.mail.showEditorWarn || app.mail.agentSelector.selectorOpen {
				t.Error("route activation resurrected a delayed palette editor surface")
			}
		})
		t.Run("palette prevents rail focus", func(t *testing.T) {
			fixture := newFixture(t)
			app := fixture.app
			app.mail.input.SetValue("/")
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyTab})
			if visibleRailV2ObserveRail(app.mail).focused {
				t.Error("Tab stole focus while the palette was active")
			}
		})
		t.Run("/agents overlay prevents rail focus", func(t *testing.T) {
			fixture := newFixture(t)
			app := fixture.app
			app.mail = app.mail.openAgentSelector()
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyTab})
			if !app.mail.agentSelector.selectorOpen || visibleRailV2ObserveRail(app.mail).focused {
				t.Error("Tab escaped /agents into a second selector focus")
			}
		})
		t.Run("delayed /agents result takes focus from the rail", func(t *testing.T) {
			fixture := newFixture(t)
			app := visibleRailV2Focus(t, fixture.app)
			app, _ = visibleRailV2Apply(app, PaletteSelectMsg{Command: "agents"})
			if !app.mail.agentSelector.selectorOpen || visibleRailV2ObserveRail(app.mail).focused || app.mail.input.Focused() {
				t.Errorf("delayed /agents result left competing focus: selector=%v rail=%v input=%v",
					app.mail.agentSelector.selectorOpen, visibleRailV2ObserveRail(app.mail).focused, app.mail.input.Focused())
			}
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
			if app.mail.agentSelector.selectorOpen || !app.mail.input.Focused() {
				t.Errorf("one Esc after delayed /agents result did not close only the selector and restore composer focus: selector=%v input=%v",
					app.mail.agentSelector.selectorOpen, app.mail.input.Focused())
			}
		})
		t.Run("copy first Esc and global ctrl+c retain precedence", func(t *testing.T) {
			fixture := newFixture(t)
			app := visibleRailV2Focus(t, fixture.app)
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: 'y', Mod: tea.ModCtrl})
			if !app.mail.copyMode {
				t.Error("ctrl+y did not toggle Mail copy mode while the rail was focused")
			}
			app, _ = visibleRailV2Apply(app, tea.KeyPressMsg{Code: tea.KeyEsc})
			if app.mail.copyMode || !visibleRailV2ObserveRail(app.mail).focused {
				t.Error("first copy-mode Esc did not exit only copy mode and retain rail focus")
			}
			_, quitCmd := visibleRailV2Apply(app, tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
			quitMsg := runCmd(quitCmd)
			if _, ok := quitMsg.(tea.QuitMsg); !ok {
				t.Errorf("rail-focused ctrl+c produced %T, want global tea.QuitMsg", quitMsg)
			}
		})
	})
}

func TestVisibleRailV2MouseWheelAndCoordinateRouting(t *testing.T) {
	t.Run("modal surfaces keep rail click and wheel inert", func(t *testing.T) {
		labels := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"}
		for _, modal := range []struct {
			name   string
			open   func(MailModel) MailModel
			isOpen func(MailModel) bool
		}{
			{
				name: "editor warning",
				open: func(m MailModel) MailModel {
					m.showEditorWarn = true
					m.editorWarnText = "modal draft"
					return m
				},
				isOpen: func(m MailModel) bool { return m.showEditorWarn },
			},
			{
				name:   "agents selector",
				open:   func(m MailModel) MailModel { return m.openAgentSelector() },
				isOpen: func(m MailModel) bool { return m.agentSelector.selectorOpen },
			},
			{
				name: "palette",
				open: func(m MailModel) MailModel {
					m.input.SetValue("/")
					return m
				},
				isOpen: func(m MailModel) bool { return m.input.IsPaletteActive() },
			},
		} {
			t.Run(modal.name, func(t *testing.T) {
				fixture := newVisibleRailV2Fixture(t, labels, make([]int, len(labels)), 85, 7, "ROOT")
				app := fixture.app
				app.mail = modal.open(app.mail)
				if !app.mail.directVisibilityObscured() {
					t.Fatal("test modal did not obscure the current Mail surface")
				}
				budget := app.layoutBudget()
				row := visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
				beforeKey := app.mail.agentSelector.selectedThreadKey
				beforeCursor := app.mail.agentSelector.cursor
				beforeInput := app.mail.input.Value()
				beforeScroll := visibleRailV2ObserveRail(app.mail).scrollOffset

				var cmd tea.Cmd
				app, cmd = visibleRailV2Apply(app, tea.MouseWheelMsg{
					X: 1, Y: budget.TopChromeRows + 3, Button: tea.MouseWheelDown,
				})
				if cmd != nil {
					t.Error("modal rail wheel scheduled work")
				}
				app, cmd = visibleRailV2Apply(app, tea.MouseClickMsg(tea.Mouse{
					X: 1, Y: budget.TopChromeRows + 2 + row, Button: tea.MouseLeft,
				}))
				if cmd != nil {
					t.Error("modal rail click scheduled work")
				}
				if !modal.isOpen(app.mail) {
					t.Errorf("rail mouse closed or displaced the owning %s surface", modal.name)
				}
				if app.mail.agentSelector.selectedThreadKey != beforeKey ||
					app.mail.agentSelector.cursor != beforeCursor || app.mail.input.Value() != beforeInput ||
					visibleRailV2ObserveRail(app.mail).scrollOffset != beforeScroll {
					t.Errorf("%s allowed rail mouse to change current/cursor/composer/scroll behind the modal", modal.name)
				}
			})
		}
	})

	t.Run("row click activates while title blank and right click are inert", func(t *testing.T) {
		fixture := newVisibleRailV2Fixture(t, []string{"Alpha", "Bravo"}, []int{0, 0}, 85, 12, "ROOT")
		app := fixture.app
		budget := app.layoutBudget()
		alphaIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)

		app, _ = visibleRailV2Apply(app, tea.MouseClickMsg(tea.Mouse{
			X: 1, Y: budget.TopChromeRows + 2 + alphaIndex, Button: tea.MouseLeft,
		}))
		visibleRailV2AssertCurrent(t, app, fixture.targets[0])

		currentKey := app.mail.agentSelector.selectedThreadKey
		currentCursor := app.mail.agentSelector.cursor
		for _, click := range []tea.MouseClickMsg{
			tea.MouseClickMsg(tea.Mouse{X: 1, Y: budget.TopChromeRows, Button: tea.MouseLeft}),
			tea.MouseClickMsg(tea.Mouse{X: 1, Y: budget.TopChromeRows + budget.ChildHeight - 1, Button: tea.MouseLeft}),
			tea.MouseClickMsg(tea.Mouse{X: 1, Y: budget.TopChromeRows + 2, Button: tea.MouseRight}),
		} {
			app, _ = visibleRailV2Apply(app, click)
		}
		if app.mail.agentSelector.selectedThreadKey != currentKey || app.mail.agentSelector.cursor != currentCursor {
			t.Error("title/blank/right rail click changed current selection or cursor")
		}
	})

	t.Run("wheel scrolls only a bounded rail window", func(t *testing.T) {
		labels := []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot", "Golf", "Hotel"}
		fixture := newVisibleRailV2Fixture(t, labels, make([]int, len(labels)), 85, 7, "ROOT")
		app := fixture.app
		budget := app.layoutBudget()
		beforeView := visibleRailV2RailSegments(app)
		beforeCursor := app.mail.agentSelector.cursor
		beforeCurrent := app.mail.agentSelector.selectedThreadKey
		for range 20 {
			app, _ = visibleRailV2Apply(app, tea.MouseWheelMsg{
				X: 1, Y: budget.TopChromeRows + 3, Button: tea.MouseWheelDown,
			})
		}
		rail := visibleRailV2ObserveRail(app.mail)
		visibleRows := budget.ChildHeight - 2
		if visibleRows < 1 {
			visibleRows = 1
		}
		wantMax := len(app.mail.agentSelector.rows) - visibleRows
		if wantMax < 0 {
			wantMax = 0
		}
		if !rail.present || rail.scrollOffset != wantMax {
			t.Errorf("wheel-down rail scroll = present=%v offset=%d, want bounded max %d",
				rail.present, rail.scrollOffset, wantMax)
		}
		if app.mail.agentSelector.cursor != beforeCursor ||
			app.mail.agentSelector.selectedThreadKey != beforeCurrent {
			t.Error("rail wheel activated or moved the canonical cursor")
		}
		if reflect.DeepEqual(beforeView, visibleRailV2RailSegments(app)) {
			t.Error("rail wheel did not change the visible canonical row window")
		}
		for range 20 {
			app, _ = visibleRailV2Apply(app, tea.MouseWheelMsg{
				X: 1, Y: budget.TopChromeRows + 3, Button: tea.MouseWheelUp,
			})
		}
		if got := visibleRailV2ObserveRail(app.mail).scrollOffset; got != 0 {
			t.Errorf("wheel-up rail scroll = %d, want lower clamp 0", got)
		}
	})

	t.Run("content click restores composer focus", func(t *testing.T) {
		fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 85, 14, "ROOT")
		app := visibleRailV2Focus(t, fixture.app)
		budget := app.layoutBudget()
		app, _ = visibleRailV2Apply(app, tea.MouseClickMsg(tea.Mouse{
			X:      visibleRailV2Width + 1,
			Y:      budget.TopChromeRows + 2,
			Button: tea.MouseLeft,
		}))
		if visibleRailV2ObserveRail(app.mail).focused || !app.mail.input.Focused() {
			t.Error("left click in Mail content did not clear rail focus and restore composer focus")
		}
	})

	t.Run("content wheel subtracts top chrome and rail exactly once", func(t *testing.T) {
		fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 85, 20, "ROOT")
		app := fixture.app
		app.mail.ready = true
		app.mail.viewport.SetContent(strings.Repeat("coordinate probe\n", 100))
		app.mail.viewport.GotoTop()
		start, _ := app.mail.inputRegionBounds()
		budget := app.layoutBudget()
		rootY := budget.TopChromeRows + start - 1
		app, _ = visibleRailV2Apply(app, tea.MouseWheelMsg{
			X:      visibleRailV2Width + 1,
			Y:      rootY,
			Button: tea.MouseWheelDown,
		})
		if got := app.mail.viewport.YOffset(); got == 0 {
			t.Errorf("content wheel at root (%d,%d) did not reach child viewport after one X/Y translation",
				visibleRailV2Width+1, rootY)
		}
	})

	t.Run("copy mode keeps rail mouse inert", func(t *testing.T) {
		fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{0}, 85, 12, "")
		app := visibleRailV2Focus(t, fixture.app)
		app.mail.copyMode = true
		before := visibleRailV2ObserveState(app)
		app, cmd := visibleRailV2Apply(app, tea.MouseClickMsg(tea.Mouse{X: 1, Y: 3, Button: tea.MouseLeft}))
		if cmd != nil {
			t.Error("copy-mode rail click scheduled work")
		}
		after := visibleRailV2ObserveState(app)
		if before.cursor != after.cursor || before.selectedThreadKey != after.selectedThreadKey ||
			before.rail.focused != after.rail.focused || before.inputValue != after.inputValue {
			t.Error("copy-mode rail click changed cursor/current/focus/composer")
		}
		if app.View().MouseMode != tea.MouseModeNone {
			t.Error("Mail copy mode did not retain terminal mouse precedence")
		}
	})
}

func TestVisibleRailV2ReorderResizeAndScroll(t *testing.T) {
	fixture := newVisibleRailV2Fixture(
		t,
		[]string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"},
		[]int{1, 1, 0, 0, 0, 0},
		85,
		8,
		"ROOT",
	)
	app := fixture.app
	alphaKey := fs.DirectThreadKey(fixture.targets[0])
	bravoKey := fs.DirectThreadKey(fixture.targets[1])
	alphaIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
	bravoIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[1].AgentID)
	app.mail, _ = app.mail.activateConversationRow(bravoIndex)
	app.mail = app.mail.setSelectorCursor(alphaIndex)
	app = visibleRailV2Focus(t, app)

	budget := app.layoutBudget()
	for range 20 {
		app, _ = visibleRailV2Apply(app, tea.MouseWheelMsg{
			X: 1, Y: budget.TopChromeRows + 3, Button: tea.MouseWheelDown,
		})
	}
	beforeExpansion := visibleRailV2ObserveRail(app.mail).scrollOffset
	if beforeExpansion <= 0 {
		t.Fatalf("short window produced no positive rail scroll offset: %d", beforeExpansion)
	}
	app, _ = visibleRailV2Apply(app, tea.WindowSizeMsg{Width: 85, Height: 30})
	if got, want := visibleRailV2ObserveRail(app.mail).scrollOffset,
		agentRailMaxScroll(len(app.mail.agentSelector.rows), app.mail.height); got != want {
		t.Fatalf("vertical expansion left stored rail scroll offset = %d, want clamped %d", got, want)
	}
	app, _ = visibleRailV2Apply(app, tea.WindowSizeMsg{Width: 85, Height: 8})
	app.mail.agentRail.scrollOffset = beforeExpansion // isolate the remaining reorder proof

	// Re-label A so its numeric index changes, then accept through the real V1
	// prepared producer. Stable cursor identity and current identity are distinct.
	directPerformanceWriteManifest(
		t,
		fixture.targets[0].Directory,
		fixture.targets[0].AgentID,
		"Zulu",
		fixture.targets[0].Address,
		false,
	)
	app = visibleRailV2AcceptPrepared(t, app)
	if app.mail.agentSelector.cursorThreadKey() != alphaKey {
		t.Errorf("accepted reorder cursor key = %q, want stable A %q",
			app.mail.agentSelector.cursorThreadKey(), alphaKey)
	}
	if app.mail.agentSelector.selectedThreadKey != bravoKey {
		t.Errorf("accepted reorder current key = %q, want stable B %q",
			app.mail.agentSelector.selectedThreadKey, bravoKey)
	}
	alphaIndex = visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
	bravoIndex = visibleRailV2RowIndex(t, app.mail, fixture.targets[1].AgentID)
	if line := visibleRailV2RailLineContaining(app, "Zulu"); !strings.HasPrefix(line, "> Zulu") {
		t.Errorf("reordered cursor row = %q, want canonical A identity rendered at its new index", line)
	}

	// Width collapse hides the rail, clears only presentation focus, and keeps B.
	app, _ = visibleRailV2Apply(app, tea.WindowSizeMsg{Width: 84, Height: 8})
	if app.layoutBudget().RailVisible {
		t.Error("width 84 retained the Mail rail")
	}
	if visibleRailV2ObserveRail(app.mail).focused || !app.mail.input.Focused() {
		t.Error("width collapse did not clear rail focus and restore composer focus")
	}
	visibleRailV2AssertCurrent(t, app, fixture.targets[1])

	// Re-expansion does not invent focus. A later view exit also clears focus
	// without changing the preserved direct center.
	app, _ = visibleRailV2Apply(app, tea.WindowSizeMsg{Width: 85, Height: 8})
	if visibleRailV2ObserveRail(app.mail).focused {
		t.Error("width re-expansion restored stale rail focus")
	}
	app = visibleRailV2Focus(t, app)
	model, _ := app.Update(ViewChangeMsg{View: "help"})
	app = model.(App)
	if visibleRailV2ObserveRail(app.mail).focused || !app.mail.input.Focused() {
		t.Error("leaving Mail retained hidden rail focus")
	}
	visibleRailV2AssertCurrent(t, app, fixture.targets[1])

	// A removed row cannot be resurrected by presentation badge state.
	model, _ = app.Update(MarkdownViewerCloseMsg{})
	app = model.(App)
	if err := os.RemoveAll(fixture.targets[3].Directory); err != nil {
		t.Fatalf("remove Delta route: %v", err)
	}
	app = visibleRailV2AcceptPrepared(t, app)
	for _, row := range app.mail.agentSelector.rows {
		if !row.Main && row.Target.AgentID == fixture.targets[3].AgentID {
			t.Error("accepted removal left Delta in canonical selector rows")
		}
	}
	for _, line := range visibleRailV2RailSegments(app) {
		if strings.Contains(line, "Delta") {
			t.Error("visible rail resurrected removed Delta from presentation state")
		}
	}
}

func visibleRailV2MarkSeenCommand(t *testing.T, app App, target fs.DirectTarget) (App, tea.Cmd) {
	t.Helper()
	index := visibleRailV2RowIndex(t, app.mail, target.AgentID)
	var visibilityCmd tea.Cmd
	app.mail, visibilityCmd = app.mail.activateConversationRow(index)
	if visibilityCmd == nil {
		t.Fatal("canonical V1 activation produced no deferred visibility command")
	}
	raw := visibilityCmd()
	visibility, ok := raw.(directVisibilityMsg)
	if !ok {
		t.Fatalf("activation command produced %T, want directVisibilityMsg", raw)
	}
	app, markSeenCmd := visibleRailV2Apply(app, visibility)
	if markSeenCmd == nil {
		t.Fatal("accepted visible direct projection produced no prepared MarkSeen command")
	}
	return app, markSeenCmd
}

func visibleRailV2ApplyUnreadResult(t *testing.T, app App, cmd tea.Cmd) (App, directUnreadResultMsg) {
	t.Helper()
	var found directUnreadResultMsg
	count := 0
	for _, msg := range visibleRailV2CollectCmd(cmd) {
		result, ok := msg.(directUnreadResultMsg)
		if !ok {
			continue
		}
		found = result
		count++
		app, _ = visibleRailV2Apply(app, result)
	}
	if count != 1 {
		t.Fatalf("durable unread command produced %d directUnreadResultMsg values, want exactly 1", count)
	}
	return app, found
}

func TestVisibleRailV2UnreadBadgeLifecycle(t *testing.T) {
	t.Run("accepted badge drops after real MarkSeen result", func(t *testing.T) {
		fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{2}, 85, 12, "")
		app := fixture.app
		alphaIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
		if line := visibleRailV2RailRow(app, alphaIndex); !strings.HasSuffix(line, " 2") {
			t.Errorf("accepted in-memory unread badge row = %q, want full badge 2", line)
		}

		var markSeenCmd tea.Cmd
		app, markSeenCmd = visibleRailV2MarkSeenCommand(t, app, fixture.targets[0])
		app, result := visibleRailV2ApplyUnreadResult(t, app, markSeenCmd)
		if result.err != nil {
			t.Fatalf("real MarkSeen result failed: %v", result.err)
		}
		alphaIndex = visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
		if line := visibleRailV2RailRow(app, alphaIndex); strings.HasSuffix(line, " 2") {
			t.Errorf("successful MarkSeen retained stale badge: %q", line)
		}
		if got := visibleRailV2ObserveRail(app.mail).unread[fs.DirectThreadKey(fixture.targets[0])]; got != 0 {
			t.Errorf("successful MarkSeen derived badge = %d, want 0", got)
		}
	})

	t.Run("stale or mismatched MarkSeen result preserves badge", func(t *testing.T) {
		for _, test := range []struct {
			name   string
			mutate func(*directUnreadResultMsg)
		}{
			{
				name: "stale accepted serial",
				mutate: func(result *directUnreadResultMsg) {
					result.acceptedSnapshotSerial++
				},
			},
			{
				name: "mismatched thread",
				mutate: func(result *directUnreadResultMsg) {
					result.threadKey += "-wrong"
				},
			},
		} {
			t.Run(test.name, func(t *testing.T) {
				fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{1}, 85, 12, "")
				app, markSeenCmd := visibleRailV2MarkSeenCommand(t, fixture.app, fixture.targets[0])
				messages := visibleRailV2CollectCmd(markSeenCmd)
				var result directUnreadResultMsg
				found := false
				for _, msg := range messages {
					candidate, ok := msg.(directUnreadResultMsg)
					if ok {
						result = candidate
						found = true
						break
					}
				}
				if !found || result.err != nil {
					t.Fatalf("real MarkSeen command result = %#v, want successful result to stale", result)
				}
				test.mutate(&result)
				app, _ = visibleRailV2Apply(app, result)

				alphaIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
				if line := visibleRailV2RailRow(app, alphaIndex); !strings.HasSuffix(line, " 1") {
					t.Errorf("%s cleared the last accepted badge: %q", test.name, line)
				}
				if got := visibleRailV2ObserveRail(app.mail).unread[fs.DirectThreadKey(fixture.targets[0])]; got != 1 {
					t.Errorf("%s derived badge = %d, want preserved 1", test.name, got)
				}
				if app.mail.statusFlash != "" {
					t.Errorf("%s flashed status %q, want silent stale/mismatch rejection", test.name, app.mail.statusFlash)
				}
			})
		}
	})

	t.Run("real MarkSeen error preserves badge and owner-neutral status", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("directory write-permission failure injection is not enforced on Windows")
		}
		fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{1}, 85, 12, "")
		app, markSeenCmd := visibleRailV2MarkSeenCommand(t, fixture.app, fixture.targets[0])
		stateDir := filepath.Dir(fixture.statePath)
		if err := os.Chmod(stateDir, 0o555); err != nil {
			t.Fatalf("block direct unread state directory: %v", err)
		}
		t.Cleanup(func() { _ = os.Chmod(stateDir, 0o755) })

		messages := visibleRailV2CollectCmd(markSeenCmd)
		if err := os.Chmod(stateDir, 0o755); err != nil {
			t.Fatalf("restore direct unread state directory: %v", err)
		}
		var result directUnreadResultMsg
		found := false
		for _, msg := range messages {
			if candidate, ok := msg.(directUnreadResultMsg); ok {
				result = candidate
				found = true
				break
			}
		}
		if !found || result.err == nil {
			t.Fatalf("permission-blocked real MarkSeen result = %#v, want a concrete error", result)
		}
		app, _ = visibleRailV2Apply(app, result)

		alphaIndex := visibleRailV2RowIndex(t, app.mail, fixture.targets[0].AgentID)
		if line := visibleRailV2RailRow(app, alphaIndex); !strings.HasSuffix(line, " 1") {
			t.Errorf("failed MarkSeen cleared the last good badge: %q", line)
		}
		if got := visibleRailV2ObserveRail(app.mail).unread[fs.DirectThreadKey(fixture.targets[0])]; got != 1 {
			t.Errorf("failed MarkSeen derived badge = %d, want preserved 1", got)
		}
		if app.mail.statusFlash != i18n.T("agent_selector.unread_failed") {
			t.Errorf("failed MarkSeen status = %q, want %q",
				app.mail.statusFlash, i18n.T("agent_selector.unread_failed"))
		}
		count, err := app.mail.directUnread.UnreadCountPublication(fixture.targets[0], app.mail.directPublication)
		if err != nil || count != 1 {
			t.Errorf("failed MarkSeen V1 store count = %d, err=%v, want retained 1", count, err)
		}
	})
}

func TestVisibleRailV2UnpreparedRefreshCannotInstallDirectState(t *testing.T) {
	fixture := newVisibleRailV2Fixture(t, []string{"Alpha"}, []int{1}, 85, 12, "")
	app := fixture.app
	beforeRows := append([]agentSelectorRow(nil), app.mail.agentSelector.rows...)
	beforeSnapshot := append([]fs.MailMessage(nil), app.mail.acceptedSnapshot.cache.Messages...)
	beforePublication := app.mail.directPublication
	beforeUnread := app.mail.directUnread
	beforeSerial := app.mail.acceptedSnapshotSerial
	beforeBytes, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatalf("read direct unread state before unprepared message: %v", err)
	}

	intruder := fs.DirectTarget{
		ProjectDirectory: fixture.root,
		Directory:        filepath.Join(fixture.lingtai, "serial-zero-intruder"),
		AgentID:          "serial-zero-intruder",
		Address:          "visible-v2/intruder",
	}
	directPerformanceWriteManifest(t, intruder.Directory, intruder.AgentID, "Serial Zero Intruder", intruder.Address, false)
	fakeCache := fs.NewMailCache(fixture.humanDir)
	fakeCache.Messages = []fs.MailMessage{{
		MailboxID:  "serial-zero-unprepared",
		From:       intruder.Address,
		To:         visibleRailV2Human,
		Message:    "must not install",
		ReceivedAt: time.Date(2026, 7, 23, 18, 0, 0, 0, time.UTC).Format(time.RFC3339Nano),
		Identity:   map[string]interface{}{"agent_id": intruder.AgentID},
		Delivered:  true,
	}}

	var cmd tea.Cmd
	app, cmd = visibleRailV2Apply(app, mailRefreshMsg{
		generation: app.mail.generation,
		cache:      fakeCache,
		state:      "active",
	})
	if cmd != nil {
		t.Error("serial-0 unprepared status message scheduled compatibility work")
	}
	if app.mail.orchState != "active" {
		t.Errorf("serial-0 status-only message did not retain status behavior: state=%q", app.mail.orchState)
	}
	if !reflect.DeepEqual(app.mail.agentSelector.rows, beforeRows) {
		t.Errorf("serial-0 unprepared message changed selector row count: before=%d after=%d",
			len(beforeRows), len(app.mail.agentSelector.rows))
	}
	if !reflect.DeepEqual(app.mail.acceptedSnapshot.cache.Messages, beforeSnapshot) {
		t.Error("serial-0 unprepared message replaced the accepted direct snapshot")
	}
	if app.mail.directPublication != beforePublication ||
		app.mail.directUnread != beforeUnread ||
		app.mail.acceptedSnapshotSerial != beforeSerial {
		t.Errorf("serial-0 unprepared message changed direct state: publication %p->%p unread %p->%p serial %d->%d",
			beforePublication, app.mail.directPublication,
			beforeUnread, app.mail.directUnread,
			beforeSerial, app.mail.acceptedSnapshotSerial)
	}
	afterBytes, err := os.ReadFile(fixture.statePath)
	if err != nil {
		t.Fatalf("read direct unread state after unprepared message: %v", err)
	}
	if string(afterBytes) != string(beforeBytes) {
		t.Error("serial-0 unprepared message performed compatibility unread I/O")
	}
	for _, row := range app.mail.agentSelector.rows {
		if !row.Main && row.Target.AgentID == intruder.AgentID {
			t.Error("serial-0 unprepared message installed the intruder direct row")
		}
	}

	// The adjacent real path remains the actual prepared, nonzero-serial V1
	// producer/acceptance rather than a replacement test-only bridge.
	app.mail.cache = fakeCache
	app = visibleRailV2AcceptPrepared(t, app)
	agentIDs := make([]string, 0)
	for _, row := range app.mail.agentSelector.rows {
		if !row.Main {
			agentIDs = append(agentIDs, row.Target.AgentID)
		}
	}
	sort.Strings(agentIDs)
	foundIntruder := false
	for _, agentID := range agentIDs {
		if agentID == intruder.AgentID {
			foundIntruder = true
			break
		}
	}
	if !foundIntruder || app.mail.directPublication == beforePublication {
		t.Errorf("real prepared path did not install the new safe row/publication: rows=%v publication=%p",
			agentIDs, app.mail.directPublication)
	}
}
