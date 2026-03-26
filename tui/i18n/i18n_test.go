package i18n

import "testing"

func TestT_ReturnsEnglishString(t *testing.T) {
	SetLang("en")
	got := T("app.title")
	if got != "灵台" {
		t.Errorf("T(\"app.title\") = %q, want %q", got, "灵台")
	}
}

func TestT_UnknownKeyReturnsKey(t *testing.T) {
	got := T("nonexistent.key")
	if got != "nonexistent.key" {
		t.Errorf("T(\"nonexistent.key\") = %q, want %q", got, "nonexistent.key")
	}
}

func TestSetLang_SwitchesLanguage(t *testing.T) {
	SetLang("zh")
	got := T("app.title")
	if got != "灵台" {
		t.Errorf("after SetLang(\"zh\"), T(\"app.title\") = %q, want %q", got, "灵台")
	}
	// Restore
	SetLang("en")
}

func TestTF_FormatsArgs(t *testing.T) {
	SetLang("en")
	got := TF("error.agent_timeout", "/tmp/logs")
	want := "Agent failed to start. Check logs at /tmp/logs"
	if got != want {
		t.Errorf("TF = %q, want %q", got, want)
	}
}

func TestLang_ReturnsCurrentLanguage(t *testing.T) {
	SetLang("en")
	if Lang() != "en" {
		t.Errorf("Lang() = %q, want %q", Lang(), "en")
	}
	SetLang("zh")
	if Lang() != "zh" {
		t.Errorf("Lang() = %q, want %q", Lang(), "zh")
	}
	SetLang("en")
}
