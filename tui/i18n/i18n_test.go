package i18n

import "testing"

// contains is a local substring check; the package already declares a
// package-level `strings` var, so we cannot import the strings package here.
func contains(haystack, needle string) bool {
	if len(needle) > len(haystack) {
		return false
	}
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}

func hasCJK(s string) bool {
	for _, r := range s {
		if r >= 0x4e00 && r <= 0x9fff {
			return true
		}
	}
	return false
}

// hasLatinWord reports whether s contains a run of >=2 ASCII letters — a real
// English word, not an incidental brand token like a "/" path. Used to detect
// English UI prose leaking into a Chinese-locale string.
func hasLatinWord(s string) bool {
	run := 0
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			run++
			if run >= 2 {
				return true
			}
		} else {
			run = 0
		}
	}
	return false
}

// TestMailInitialLoading_LocaleSpecific is the regression for the bilingual
// "loading... / 加载中..." string that triggered this fix. English mode must
// show only English; Chinese modes must show only Chinese.
func TestMailInitialLoading_LocaleSpecific(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	SetLang("en")
	if got := T("mail.initial_loading"); hasCJK(got) {
		t.Errorf("en mail.initial_loading = %q, must not contain Chinese", got)
	}

	for _, lang := range []string{"zh", "wen"} {
		SetLang(lang)
		got := T("mail.initial_loading")
		if hasLatinWord(got) {
			t.Errorf("%s mail.initial_loading = %q, must not contain English words", lang, got)
		}
		if !hasCJK(got) {
			t.Errorf("%s mail.initial_loading = %q, expected Chinese", lang, got)
		}
	}
}

// TestCodexBanners_LocaleSpecific covers the two Codex OAuth startup warnings
// that previously hardcoded mixed-language literals in app.go.
func TestCodexBanners_LocaleSpecific(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	SetLang("en")
	expired := TF("codex.oauth_expired_banner", T("preset.codex_credential_section"))
	if !contains(expired, "session expired") {
		t.Errorf("en codex.oauth_expired_banner = %q, expected English prose", expired)
	}
	unverified := TF("codex.oauth_unverified_agent", "alice")
	if hasCJK(unverified) {
		t.Errorf("en codex.oauth_unverified_agent = %q, must not contain Chinese", unverified)
	}
	if !contains(unverified, "alice") {
		t.Errorf("en codex.oauth_unverified_agent = %q, expected agent name interpolated", unverified)
	}

	for _, lang := range []string{"zh", "wen"} {
		SetLang(lang)
		got := TF("codex.oauth_unverified_agent", "alice")
		if !hasCJK(got) {
			t.Errorf("%s codex.oauth_unverified_agent = %q, expected Chinese", lang, got)
		}
		if !contains(got, "alice") {
			t.Errorf("%s codex.oauth_unverified_agent = %q, expected agent name interpolated", lang, got)
		}
	}
}

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
