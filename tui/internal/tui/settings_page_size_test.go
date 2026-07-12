package tui

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/internal/config"
)

func pageSizeField(t *testing.T, m SettingsModel) SettingField {
	t.Helper()
	for _, f := range m.fields {
		if f.Key == "mail_page_size" {
			return f
		}
	}
	t.Fatal("settings model missing mail_page_size field")
	return SettingField{}
}

func TestSettingsMailPageSizeOptionsAndDefault(t *testing.T) {
	m := NewSettingsModel(t.TempDir(), t.TempDir(), t.TempDir(), config.DefaultTUIConfig())
	f := pageSizeField(t, m)
	wantOptions := []string{"100", "200", "500", "1000", "2000"}
	if !reflect.DeepEqual(f.Options, wantOptions) {
		t.Fatalf("mail_page_size options = %#v, want %#v", f.Options, wantOptions)
	}
	if got := f.Options[f.Current]; got != "200" {
		t.Fatalf("mail_page_size default current = %d (%q), want the 200 option", f.Current, got)
	}
}

func TestSettingsMailPageSizeHundredOption(t *testing.T) {
	cfg := config.DefaultTUIConfig()
	cfg.MailPageSize = 100
	m := NewSettingsModel(t.TempDir(), t.TempDir(), t.TempDir(), cfg)
	f := pageSizeField(t, m)
	if got := f.Options[f.Current]; got != "100" {
		t.Fatalf("MailPageSize=100 selected %q, want 100", got)
	}

	m.applyField(&f)
	loaded := config.LoadTUIConfig(m.globalDir)
	if loaded.MailPageSize != 100 {
		t.Fatalf("100 page size persisted as %d, want 100", loaded.MailPageSize)
	}
}

func TestReturningToMailAfterPageSizeChangeRebuildsExactWindow(t *testing.T) {
	globalDir := t.TempDir()
	projectDir := t.TempDir()
	orchDir := buildWindowedAgentDir(t, 250)

	cfg := config.DefaultTUIConfig()
	cfg.MailPageSize = 100
	if err := config.SaveTUIConfig(globalDir, cfg); err != nil {
		t.Fatal(err)
	}

	a := App{
		currentView: appViewSettings,
		globalDir:   globalDir,
		projectDir:  projectDir,
		orchDir:     orchDir,
		orchName:    "agent",
	}
	a.installMailModel(NewMailModel(t.TempDir(), "human", projectDir, orchDir, "agent", 200, globalDir, "en", false, 0))
	initial := a.mail.initialRebuild().(mailRefreshMsg)
	a.mail, _ = a.mail.Update(initial)
	if a.mail.sessionCache.Len() != 200 {
		t.Fatalf("precondition cache = %d, want 200", a.mail.sessionCache.Len())
	}
	oldGeneration := a.mail.generation

	model, cmd := a.switchToView("mail")
	got := model.(App)
	if got.mail.pageSize != 100 || got.mail.generation == oldGeneration || !got.mail.initialLoading {
		t.Fatalf("page-size change did not start fresh Mail generation: page=%d generation=%d old=%d loading=%v", got.mail.pageSize, got.mail.generation, oldGeneration, got.mail.initialLoading)
	}
	batch, ok := cmd().(tea.BatchMsg)
	if !ok || len(batch) == 0 {
		t.Fatalf("return-to-Mail command = %T, want non-empty tea.BatchMsg", cmd())
	}
	refresh, ok := batch[0]().(mailRefreshMsg)
	if !ok {
		t.Fatalf("first return-to-Mail command = %T, want fresh mailRefreshMsg", batch[0]())
	}
	if got := refresh.sessionCache.Len(); got != 100 {
		t.Fatalf("new page-size content cache = %d, want exactly 100", got)
	}
}

func TestSettingsMailPageSizeHasNoUnlimitedChoice(t *testing.T) {
	cfg := config.DefaultTUIConfig()
	cfg.MailPageSize = 0 // removed legacy sentinel
	m := NewSettingsModel(t.TempDir(), t.TempDir(), t.TempDir(), cfg)
	f := pageSizeField(t, m)
	for _, option := range f.Options {
		if option == "infinite" || option == "unlimited" || option == "0" {
			t.Fatalf("mail_page_size kept removed unlimited option %q", option)
		}
	}
	if got := f.Options[f.Current]; got != "200" {
		t.Fatalf("legacy zero selected %q, want finite default 200", got)
	}
}
