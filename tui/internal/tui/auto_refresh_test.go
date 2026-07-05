package tui

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/config"
)

// Auto-refresh contract:
//   - Enabled by default; only an explicit opt-out persists a key.
//   - The kanban (props) view opts into the 1s tick via AutoReloadCmd, but
//     skips while the picker or detail pane is open (don't interrupt the user).
//   - The app-level tick drives a reload when enabled and re-arms; when
//     disabled it drops without re-arming. Ctrl+R is untouched (covered by
//     ctrl_r_refresh_test.go).

func TestAutoRefreshEnabledByDefault(t *testing.T) {
	dir := t.TempDir()
	// No file on disk → defaults apply.
	if !config.LoadTUIConfig(dir).AutoRefreshEnabled() {
		t.Fatal("auto refresh should be enabled by default when no config exists")
	}
	if !config.DefaultTUIConfig().AutoRefreshEnabled() {
		t.Fatal("DefaultTUIConfig should have auto refresh enabled")
	}
}

func TestAutoRefreshPersistenceRoundTrip(t *testing.T) {
	dir := t.TempDir()

	// Default-on writes no auto_refresh_off key (omitempty inverse flag).
	if err := config.SaveTUIConfig(dir, config.DefaultTUIConfig()); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(dir, "tui_config.json"))
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if _, present := m["auto_refresh_off"]; present {
		t.Fatalf("default-on config should not persist auto_refresh_off; got %s", raw)
	}

	// Explicit opt-out persists and round-trips to disabled.
	cfg := config.DefaultTUIConfig()
	cfg.AutoRefreshOff = true
	if err := config.SaveTUIConfig(dir, cfg); err != nil {
		t.Fatal(err)
	}
	if config.LoadTUIConfig(dir).AutoRefreshEnabled() {
		t.Fatal("after opt-out, auto refresh should load as disabled")
	}
}

func TestPropsAutoReloadCmd(t *testing.T) {
	m := NewPropsModel(t.TempDir(), t.TempDir(), t.TempDir())

	// Normal state: a reload command is returned.
	if m.AutoReloadCmd() == nil {
		t.Fatal("props AutoReloadCmd should return a reload command in the normal state")
	}

	// Picker open: skip the tick so we don't reload mid-selection.
	m.pickerOpen = true
	if m.AutoReloadCmd() != nil {
		t.Fatal("props AutoReloadCmd should be nil while the agent picker is open")
	}
	m.pickerOpen = false

	// Detail pane open: still reload the outer dashboard; the App-layer tick also
	// refreshes detail caches in place before scheduling this command.
	m.detailOpen = true
	if m.AutoReloadCmd() == nil {
		t.Fatal("props AutoReloadCmd should keep the Ctrl+D detail layer live")
	}
}

func TestAppAutoRefreshTickReloadsAndRearms(t *testing.T) {
	a := App{
		currentView: appViewProps,
		props:       NewPropsModel(t.TempDir(), t.TempDir(), t.TempDir()),
		tuiConfig:   config.DefaultTUIConfig(), // auto refresh on
	}
	updated, cmd := a.Update(autoRefreshTickMsg{})
	if cmd == nil {
		t.Fatal("enabled auto-refresh tick on props should return a (reload+rearm) batch")
	}
	if ua, ok := updated.(App); ok && !ua.autoRefreshArmed {
		t.Fatal("auto-refresh tick should keep the ticker armed while enabled")
	}
}

func TestAppAutoRefreshTickKeepsKanbanDetailLive(t *testing.T) {
	a := App{
		currentView: appViewProps,
		props:       NewPropsModel(t.TempDir(), t.TempDir(), t.TempDir()),
		tuiConfig:   config.DefaultTUIConfig(),
	}
	a.props.detailOpen = true
	updated, cmd := a.Update(autoRefreshTickMsg{})
	if cmd == nil {
		t.Fatal("enabled auto-refresh tick should keep the kanban detail layer live")
	}
	ua, ok := updated.(App)
	if !ok {
		t.Fatal("auto-refresh tick should return an App")
	}
	if !ua.props.detailOpen {
		t.Fatal("auto-refresh should not close the kanban Ctrl+D detail layer")
	}
}

func TestAutoRefreshActiveViewOnlyReloadsKanban(t *testing.T) {
	dir := t.TempDir()
	kanban := App{currentView: appViewProps, props: NewPropsModel(dir, dir, dir), tuiConfig: config.DefaultTUIConfig()}
	if _, cmd := kanban.autoRefreshActiveView(); cmd == nil {
		t.Fatal("kanban auto-refresh should return the same reload command as Ctrl+R")
	}

	// Interactive / markdown / picker-heavy views must not auto-refresh every
	// second: doing so can reset scroll or selection while the human is navigating
	// with the keyboard. They retain explicit Ctrl+R refresh only.
	blocked := []App{
		{currentView: appViewProjects, projects: NewProjectsModel(dir, dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewDaemons, daemons: NewDaemonsModel(dir, dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewDoctor, doctor: DoctorModel{orchDir: dir, globalDir: dir}, tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewMailbox, mailbox: NewMailboxModel(dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewSystem, system: NewSystemModel(dir, dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewLibrary, library: NewLibraryModel(dir, dir, "en"), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewKnowledge, knowledge: NewKnowledgeModel(dir, dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewPresets, presetLibrary: NewPresetLibraryModel("en", dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewAddon, addon: NewAddonModel(dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewNotification, notification: NewNotificationModel(dir), tuiConfig: config.DefaultTUIConfig()},
		{currentView: appViewHelp, help: NewHelpModel(), tuiConfig: config.DefaultTUIConfig()},
	}
	for _, tc := range blocked {
		if _, cmd := tc.autoRefreshActiveView(); cmd != nil {
			t.Fatalf("view %v should not participate in 1s auto-refresh; use Ctrl+R", tc.currentView)
		}
	}
}

func TestAppAutoRefreshTickDisabledDropsAndUnarms(t *testing.T) {
	cfg := config.DefaultTUIConfig()
	cfg.AutoRefreshOff = true
	a := App{
		currentView:      appViewProps,
		props:            NewPropsModel(t.TempDir(), t.TempDir(), t.TempDir()),
		tuiConfig:        cfg, // auto refresh off
		autoRefreshArmed: true,
	}
	updated, cmd := a.Update(autoRefreshTickMsg{})
	if cmd != nil {
		t.Fatal("disabled auto-refresh tick should not re-arm (nil cmd)")
	}
	if ua, ok := updated.(App); ok && ua.autoRefreshArmed {
		t.Fatal("disabled auto-refresh tick should mark the ticker unarmed")
	}
}

func TestNewAppMarksStartupAutoRefreshArmed(t *testing.T) {
	a := NewApp(t.TempDir(), t.TempDir(), false, false, nil, config.DefaultTUIConfig(), "", "")
	if !a.autoRefreshArmed {
		t.Fatal("NewApp should mark auto refresh armed when Init will start the startup ticker")
	}
	cfg := config.DefaultTUIConfig()
	cfg.AutoRefreshOff = true
	disabled := NewApp(t.TempDir(), t.TempDir(), false, false, nil, cfg, "", "")
	if disabled.autoRefreshArmed {
		t.Fatal("NewApp should not mark auto refresh armed when disabled")
	}
}

func TestStartAutoRefreshIsIdempotent(t *testing.T) {
	a := App{tuiConfig: config.DefaultTUIConfig()}
	a, cmd := a.startAutoRefresh()
	if cmd == nil || !a.autoRefreshArmed {
		t.Fatal("first startAutoRefresh should arm and return a tick command")
	}
	// Already armed → no second concurrent ticker.
	_, cmd2 := a.startAutoRefresh()
	if cmd2 != nil {
		t.Fatal("startAutoRefresh should be a no-op when already armed")
	}
}

func TestSettingsAutoRefreshToggleSetsConfig(t *testing.T) {
	cfg := config.DefaultTUIConfig() // on
	m := NewSettingsModel(t.TempDir(), t.TempDir(), "", cfg)

	var f *SettingField
	for i := range m.fields {
		if m.fields[i].Key == "auto_refresh" {
			f = &m.fields[i]
			break
		}
	}
	if f == nil {
		t.Fatal("settings should expose an auto_refresh field")
	}
	if f.Options[f.Current] != "on" {
		t.Fatalf("auto_refresh should default to on; got %q", f.Options[f.Current])
	}

	// Cycle to "off" and apply.
	f.Current = 0 // "off"
	m.applyField(f)
	if m.tuiConfig.AutoRefreshEnabled() {
		t.Fatal("applying auto_refresh=off should disable auto refresh in the config")
	}
}
