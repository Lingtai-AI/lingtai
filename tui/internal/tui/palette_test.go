package tui

import (
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
)

func TestDefaultCommandsHaveLocalizedPaletteText(t *testing.T) {
	langs := []string{"en", "zh", "wen"}

	for _, cmd := range DefaultCommands() {
		for _, lang := range langs {
			if got := i18n.TIn(lang, cmd.Description); got == cmd.Description {
				t.Errorf("%s locale is missing description key %q for /%s", lang, cmd.Description, cmd.Name)
			}
			if got := i18n.TIn(lang, cmd.Detail); got == cmd.Detail {
				t.Errorf("%s locale is missing detail key %q for /%s", lang, cmd.Detail, cmd.Name)
			}
		}
	}
}
