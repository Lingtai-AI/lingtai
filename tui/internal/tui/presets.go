package tui

// UsePresetMsg is emitted when a preset is selected for use.
type UsePresetMsg struct {
	Name string
}

// AllAddons is the list of available addon names.
var AllAddons = []string{"imap", "telegram", "feishu", "wechat"}
