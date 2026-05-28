package tui

// UsePresetMsg is emitted when a preset is selected for use.
type UsePresetMsg struct {
	Name string
}

// AllCapabilities is the list of capabilities the TUI still allows users to
// configure from setup flows. Kernel core capabilities are always included by
// the runtime floor and are not ordinary preset opt-in toggles.
var AllCapabilities = []string{
	"web_search", "vision",
}

// AllAddons is the list of available addon names.
var AllAddons = []string{"imap", "telegram", "feishu", "wechat"}
