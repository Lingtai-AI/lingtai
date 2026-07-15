package migrate

import "testing"

func TestM028AddonSpecsUseBundledModules(t *testing.T) {
	want := map[string]addonSpec{
		"imap":     {module: "lingtai.mcp_servers.imap", envVarName: "LINGTAI_IMAP_CONFIG", defaultRel: ".secrets/imap.json"},
		"telegram": {module: "lingtai.mcp_servers.telegram", envVarName: "LINGTAI_TELEGRAM_CONFIG", defaultRel: ".secrets/telegram.json"},
		"feishu":   {module: "lingtai.mcp_servers.feishu", envVarName: "LINGTAI_FEISHU_CONFIG", defaultRel: ".secrets/feishu.json"},
		"wechat":   {module: "lingtai.mcp_servers.wechat", envVarName: "LINGTAI_WECHAT_CONFIG", defaultRel: ".secrets/wechat/config.json"},
		"whatsapp": {module: "lingtai.mcp_servers.whatsapp", envVarName: "LINGTAI_WHATSAPP_CONFIG", defaultRel: ".secrets/whatsapp.json"},
	}

	if len(addonSpecs) != len(want) {
		t.Fatalf("addonSpecs has %d entries, want %d: %#v", len(addonSpecs), len(want), addonSpecs)
	}
	for name, wantSpec := range want {
		got, ok := addonSpecs[name]
		if !ok {
			t.Errorf("addonSpecs missing %q", name)
			continue
		}
		if got != wantSpec {
			t.Errorf("addonSpecs[%q] = %#v, want %#v", name, got, wantSpec)
		}
	}
}
