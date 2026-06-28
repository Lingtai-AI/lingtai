package tui

import (
	"reflect"
	"testing"

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
	wantOptions := []string{"100", "200", "500", "1000", "infinite"}
	if !reflect.DeepEqual(f.Options, wantOptions) {
		t.Fatalf("mail_page_size options = %#v, want %#v", f.Options, wantOptions)
	}
	if f.Current != 1 {
		t.Fatalf("mail_page_size default current = %d (%q), want 1 (%q)", f.Current, f.Options[f.Current], wantOptions[1])
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

func TestSettingsMailPageSizeInfiniteMapsToZero(t *testing.T) {
	cfg := config.DefaultTUIConfig()
	cfg.MailPageSize = 0
	m := NewSettingsModel(t.TempDir(), t.TempDir(), t.TempDir(), cfg)
	f := pageSizeField(t, m)
	if got := f.Options[f.Current]; got != "infinite" {
		t.Fatalf("MailPageSize=0 selected %q, want infinite", got)
	}

	f.Current = len(f.Options) - 1
	m.applyField(&f)
	loaded := config.LoadTUIConfig(m.globalDir)
	if loaded.MailPageSize != 0 {
		t.Fatalf("infinite page size persisted as %d, want 0", loaded.MailPageSize)
	}
}
