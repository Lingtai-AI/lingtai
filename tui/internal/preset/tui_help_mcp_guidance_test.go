package preset

import (
	"strings"
	"testing"
)

func TestTUIHelpMCPGuidanceAcrossLocales(t *testing.T) {
	required := map[string][]string{
		"en":  {"read-only", "does not configure or edit MCP", "`/addon` command is retired", "current MCP/curated-addon documentation", "explicit authorization"},
		"zh":  {"只读", "不负责设置或编辑 MCP", "`/addon` 命令已退休", "当前 MCP/精选插件文档", "明确授权"},
		"wen": {"只读", "配置非由此成，亦非由此编辑", "`/addon` 旧令已退", "当前 MCP/精选插件文档", "明示授权"},
	}

	for locale, markers := range required {
		t.Run(locale, func(t *testing.T) {
			path := "skills/lingtai-tui-help/assets/slash-commands." + locale + ".md"
			body, err := skillsFS.ReadFile(path)
			if err != nil {
				t.Fatalf("read %s: %v", path, err)
			}
			text := string(body)
			_, section, ok := strings.Cut(text, "### `/mcp`")
			if !ok {
				t.Fatalf("%s has no /mcp reference section", path)
			}
			if section, _, ok = strings.Cut(section, "\n### "); !ok {
				t.Fatalf("%s /mcp reference section has no closing command heading", path)
			}
			section = strings.Join(strings.Fields(section), " ")
			for _, marker := range markers {
				if !strings.Contains(section, marker) {
					t.Errorf("%s /mcp section missing %q", path, marker)
				}
			}
		})
	}
}
