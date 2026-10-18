package tui

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/anthropics/lingtai-tui/internal/config"
)

func repairSettingsModel(t *testing.T) (SettingsModel, string, []byte) {
	t.Helper()
	dir := t.TempDir()
	init := []byte(`{"agent":{"language":"en"},"llm":{"provider":"openai","model":"fixture"},"tools":{}}`)
	if err := os.WriteFile(filepath.Join(dir, "init.json"), init, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "settings"), 0700); err != nil {
		t.Fatal(err)
	}
	m := NewSettingsModel(t.TempDir(), t.TempDir(), dir, config.DefaultTUIConfig())
	for i := range m.fields {
		if m.fields[i].Key == "agent_lang" {
			m.cursor = i
		}
	}
	m.width = 100
	return m, filepath.Join(dir, "settings", "psyche.json"), init
}

func assertRepairSettingsFailure(t *testing.T, m SettingsModel, original []byte, partial bool) SettingsModel {
	t.Helper()
	m, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	var failure *AgentLanguageSaveError
	if !errors.As(m.saveErr, &failure) || failure.PsycheWritten != partial {
		t.Fatalf("wrong save outcome: %#v", m.saveErr)
	}
	if got := m.fields[m.cursor].Options[m.fields[m.cursor].Current]; got != "en" {
		t.Fatalf("false displayed language %q", got)
	}
	if !strings.Contains(m.View(), failure.Error()) {
		t.Fatal("failure absent from settings view")
	}
	data, err := os.ReadFile(filepath.Join(m.orchDir, "init.json"))
	if err != nil || !bytes.Equal(data, original) {
		t.Fatalf("init changed on failure: %s, %v", data, err)
	}
	return m
}

func TestSettingsUpdateRefusesInvalidPsycheOwners(t *testing.T) {
	for name, owner := range map[string][]byte{
		"duplicate":     []byte(`{"schema_version":1,"covenant":"a","covenant":"b"}`),
		"unknown":       []byte(`{"schema_version":1,"alien":true}`),
		"oversized":     bytes.Repeat([]byte(" "), settingsPsycheMaxBytes+1),
		"invalid UTF-8": append([]byte(`{"schema_version":1,"covenant":"`), 0xff, '"', '}'),
	} {
		t.Run(name, func(t *testing.T) {
			m, path, original := repairSettingsModel(t)
			if err := os.WriteFile(path, owner, 0600); err != nil {
				t.Fatal(err)
			}
			assertRepairSettingsFailure(t, m, original, false)
			got, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(got, owner) {
				t.Fatal("invalid owner overwritten")
			}
		})
	}
}

func TestSettingsUpdateRefusesSymlinkAndDirectoryOwners(t *testing.T) {
	for _, kind := range []string{"symlink", "directory"} {
		t.Run(kind, func(t *testing.T) {
			m, path, original := repairSettingsModel(t)
			if kind == "directory" {
				if err := os.Mkdir(path, 0700); err != nil {
					t.Fatal(err)
				}
			} else {
				target := filepath.Join(t.TempDir(), "owner.json")
				owner := []byte(`{"schema_version":1}`)
				if err := os.WriteFile(target, owner, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Skipf("symlink unavailable: %v", err)
				}
				defer func() {
					got, err := os.ReadFile(target)
					if err != nil || !bytes.Equal(got, owner) {
						t.Error("symlink target changed")
					}
				}()
			}
			assertRepairSettingsFailure(t, m, original, false)
			info, err := os.Lstat(path)
			if err != nil || (kind == "symlink" && info.Mode()&os.ModeSymlink == 0) || (kind == "directory" && !info.IsDir()) {
				t.Fatal("invalid owner replaced")
			}
		})
	}
}
