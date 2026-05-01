package tui

// UsePresetMsg is emitted when a preset is selected for use.
type UsePresetMsg struct {
	Name string
}

// AllCapabilities is the list of all available capability names.
// email and psyche are kernel intrinsics (always loaded), not capabilities.
var AllCapabilities = []string{
	"file", "bash", "web_search", "codex",
	"vision",
	"avatar", "daemon", "library",
}

// AllAddons is the list of available addon names.
var AllAddons = []string{"imap", "telegram", "feishu", "wechat"}
