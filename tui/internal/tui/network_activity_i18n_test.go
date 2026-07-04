package tui

import (
	"reflect"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

func TestNetworkActivityDaemonActiveDisplaysAsActive(t *testing.T) {
	oldLang := i18n.Lang()
	t.Cleanup(func() { i18n.SetLang(oldLang) })

	for _, lang := range []string{"en", "zh", "wen"} {
		t.Run(lang, func(t *testing.T) {
			i18n.SetLang(lang)
			got := networkActivityStatusLabel(fs.NetworkStatusDaemonActive)
			want := networkActivityStatusLabel(fs.NetworkStatusActive)
			if got != want {
				t.Fatalf("daemon-active label = %q, want active label %q", got, want)
			}
		})
	}

	gotColor := NetworkActivityColor(fs.NetworkStatusDaemonActive)
	wantColor := NetworkActivityColor(fs.NetworkStatusActive)
	if !reflect.DeepEqual(gotColor, wantColor) {
		t.Fatalf("daemon-active color = %#v, want active color %#v", gotColor, wantColor)
	}
}
