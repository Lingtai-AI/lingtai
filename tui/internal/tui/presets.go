package tui

// UsePresetMsg is emitted when a preset is selected for use.
type UsePresetMsg struct {
	Name string
}

// AllCapabilities is the list of all available capability names.
var AllCapabilities = []string{
	"file", "email", "bash", "web_search", "psyche", "codex",
	"vision", "talk", "draw", "compose", "video", "listen", "web_read",
	"avatar", "daemon", "library",
}

// AllAddons is the list of available addon names.
var AllAddons = []string{"imap", "telegram", "feishu", "wechat"}
