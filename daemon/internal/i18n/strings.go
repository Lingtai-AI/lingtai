package i18n

import "os"

// Lang is the current language code.
var Lang = detectLang()

// S returns the localized string for the given key.
func S(key string) string {
	if m, ok := translations[Lang]; ok {
		if s, ok := m[key]; ok {
			return s
		}
	}
	// Fall back to English
	if s, ok := translations["en"][key]; ok {
		return s
	}
	return key
}

// Renamed from "strings" to "translations" to avoid shadowing the built-in strings package.
var translations = map[string]map[string]string{
	"en": {
		"title":           "Daemon",
		"setup_title":     "Setup Wizard",
		"manage_title":    "Running Spirits",
		"starting":        "Starting agent...",
		"shutting_down":   "Shutting down...",
		"connected":       "Connected",
		"disconnected":    "Disconnected",
		"press_ctrl_c":    "Press Ctrl+C to shut down",
		"type_message":    "Type a message...",
		"no_spirits":      "No running spirits found.",
		"name":            "Name",
		"pid":             "PID",
		"port":            "Port",
		"uptime":          "Uptime",
		"status":          "Status",
		"running":         "running",
		"dead":            "dead (stale PID)",
		"setup_model":     "LLM Provider",
		"setup_imap":      "IMAP Email",
		"setup_telegram":  "Telegram Bot",
		"setup_general":   "General Settings",
		"setup_review":    "Review",
		"setup_done":      "Setup Complete",
	},
	"zh": {
		"title":           "器灵",
		"setup_title":     "设置向导",
		"manage_title":    "运行中的器灵",
		"starting":        "正在启动代理...",
		"shutting_down":   "正在关闭...",
		"connected":       "已连接",
		"disconnected":    "未连接",
		"press_ctrl_c":    "按 Ctrl+C 关闭",
		"type_message":    "输入消息...",
		"no_spirits":      "没有运行中的器灵。",
		"name":            "名称",
		"pid":             "进程号",
		"port":            "端口",
		"uptime":          "运行时间",
		"status":          "状态",
		"running":         "运行中",
		"dead":            "已停止（残留PID）",
		"setup_model":     "语言模型配置",
		"setup_imap":      "IMAP 邮箱",
		"setup_telegram":  "Telegram 机器人",
		"setup_general":   "基本设置",
		"setup_review":    "确认",
		"setup_done":      "设置完成",
	},
}

func detectLang() string {
	lang := os.Getenv("LANG")
	if len(lang) >= 2 && (lang[:2] == "zh") {
		return "zh"
	}
	return "en"
}
