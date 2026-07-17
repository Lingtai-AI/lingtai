package i18n

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// contains is a local substring check.
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

func loadLocaleForTest(t *testing.T, locale string) map[string]string {
	t.Helper()
	data, err := localeFS.ReadFile(locale + ".json")
	if err != nil {
		t.Fatalf("read %s locale: %v", locale, err)
	}
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("parse %s locale: %v", locale, err)
	}
	return m
}

func copyStringMap(m map[string]string) map[string]string {
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}

func withActiveStringsForTest(t *testing.T, m map[string]string) {
	t.Helper()
	mu.Lock()
	old := activeStrings
	activeStrings = m
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		activeStrings = old
		mu.Unlock()
	})
}

func withCachedLocaleForTest(t *testing.T, locale string, m map[string]string) {
	t.Helper()
	cacheMu.Lock()
	old, hadOld := cache[locale]
	cache[locale] = m
	cacheMu.Unlock()
	t.Cleanup(func() {
		cacheMu.Lock()
		if hadOld {
			cache[locale] = old
		} else {
			delete(cache, locale)
		}
		cacheMu.Unlock()
	})
}

func resetCacheForTest(t *testing.T) {
	t.Helper()
	cacheMu.Lock()
	old := cache
	cache = map[string]map[string]string{"en": englishStrings}
	cacheMu.Unlock()
	t.Cleanup(func() {
		cacheMu.Lock()
		cache = old
		cacheMu.Unlock()
	})
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
	if got, want := T("firstrun.preset_pick.codex_needs_oauth_hint"), "Codex login required — see Codex Credentials section below."; got != want {
		t.Errorf("en firstrun.preset_pick.codex_needs_oauth_hint = %q, want %q", got, want)
	}
	if got, want := T("preset.codex_credential_section"), "Codex Credentials"; got != want {
		t.Errorf("en preset.codex_credential_section = %q, want %q", got, want)
	}
	expired := TF("codex.oauth_expired_banner", T("preset.codex_credential_section"))
	if hasCJK(expired) {
		t.Errorf("en codex.oauth_expired_banner = %q, must not contain Chinese", expired)
	}
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
		expired := TF("codex.oauth_expired_banner", T("preset.codex_credential_section"))
		if !hasCJK(expired) {
			t.Errorf("%s codex.oauth_expired_banner = %q, expected localized Chinese prose", lang, expired)
		}
		if !contains(expired, T("preset.codex_credential_section")) {
			t.Errorf("%s codex.oauth_expired_banner = %q, expected localized credential section", lang, expired)
		}
		got := TF("codex.oauth_unverified_agent", "alice")
		if !hasCJK(got) {
			t.Errorf("%s codex.oauth_unverified_agent = %q, expected Chinese", lang, got)
		}
		if !contains(got, "alice") {
			t.Errorf("%s codex.oauth_unverified_agent = %q, expected agent name interpolated", lang, got)
		}
	}
}

// TestEnglishCodexCredentialSurfacesHaveNoCJK scopes the English-locale check to
// Codex credential keys only. The English catalog intentionally retains native
// brand and aesthetic text elsewhere, so a global CJK ban would be incorrect.
func TestEnglishCodexCredentialSurfacesHaveNoCJK(t *testing.T) {
	en := loadLocaleForTest(t, "en")
	for key, value := range en {
		credentialSurface := strings.HasPrefix(key, "codex.") ||
			strings.HasPrefix(key, "firstrun.preset_pick.codex_") ||
			strings.HasPrefix(key, "preset.codex_") ||
			key == "preset_editor.api_key_codex_readonly" ||
			strings.HasPrefix(key, "login.codex_")
		if credentialSurface && hasCJK(value) {
			t.Errorf("en %s = %q, Codex credential surface must not contain Chinese", key, value)
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

func TestT_FallsBackToEnglishWhenActiveLocaleMissesKey(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	if err := SetLang("zh"); err != nil {
		t.Fatalf("SetLang(\"zh\") returned error: %v", err)
	}
	key := "settings.language"
	zh := loadLocaleForTest(t, "zh")
	gap := copyStringMap(zh)
	delete(gap, key)
	withActiveStringsForTest(t, gap)

	if got, want := T(key), "Language"; got != want {
		t.Fatalf("T(%q) = %q, want English fallback %q", key, got, want)
	}
}

func TestT_MissingEverywhereReturnsKey(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	if err := SetLang("zh"); err != nil {
		t.Fatalf("SetLang(\"zh\") returned error: %v", err)
	}
	key := "__i18n_missing_everywhere__"
	if got := T(key); got != key {
		t.Fatalf("T(%q) = %q, want raw key", key, got)
	}
}

func TestSetLang_SwitchesLanguage(t *testing.T) {
	SetLang("zh")
	got := T("settings.language")
	if got != "语言" {
		t.Errorf("after SetLang(\"zh\"), T(\"settings.language\") = %q, want %q", got, "语言")
	}
	// Restore
	SetLang("en")
}

func TestSetLang_InvalidLanguageLeavesStateUnchanged(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	if err := SetLang("zh"); err != nil {
		t.Fatalf("SetLang(\"zh\") returned error: %v", err)
	}
	beforeLang := Lang()
	beforeText := T("mail.initial_loading")

	if err := SetLang("fr"); err == nil {
		t.Fatal("SetLang(\"fr\") returned nil error")
	}
	if got := Lang(); got != beforeLang {
		t.Fatalf("Lang() after invalid SetLang = %q, want %q", got, beforeLang)
	}
	if got := T("mail.initial_loading"); got != beforeText {
		t.Fatalf("T(mail.initial_loading) after invalid SetLang = %q, want %q", got, beforeText)
	}
}

func TestSetLang_ValidSwitchWorksAfterInvalidLanguage(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	if err := SetLang("zh"); err != nil {
		t.Fatalf("SetLang(\"zh\") returned error: %v", err)
	}
	if err := SetLang("fr"); err == nil {
		t.Fatal("SetLang(\"fr\") returned nil error")
	}
	if err := SetLang("wen"); err != nil {
		t.Fatalf("SetLang(\"wen\") after invalid language returned error: %v", err)
	}
	if got := T("settings.language"); got != "言" {
		t.Fatalf("T(settings.language) after SetLang(\"wen\") = %q, want %q", got, "言")
	}
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

func TestLocaleJSONKeySetsAreComplete(t *testing.T) {
	en := loadLocaleForTest(t, "en")
	for _, locale := range []string{"en", "zh", "wen"} {
		m := loadLocaleForTest(t, locale)
		for key, value := range m {
			if value == "" {
				t.Fatalf("%s locale has empty value for key %q", locale, key)
			}
		}
		if len(m) != len(en) {
			t.Fatalf("%s locale has %d keys, want %d", locale, len(m), len(en))
		}
		for key := range en {
			if _, ok := m[key]; !ok {
				t.Fatalf("%s locale missing key %q", locale, key)
			}
		}
		for key := range m {
			if _, ok := en[key]; !ok {
				t.Fatalf("%s locale has extra key %q", locale, key)
			}
		}
	}
}

func TestTInUsesRequestedLocale(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	if err := SetLang("en"); err != nil {
		t.Fatalf("SetLang(\"en\") returned error: %v", err)
	}
	if got := TIn("zh", "settings.language"); got != "语言" {
		t.Fatalf("TIn(\"zh\", \"settings.language\") = %q, want %q", got, "语言")
	}
}

func TestTInUnknownLocaleFallsBackThroughCurrentLocale(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	if err := SetLang("zh"); err != nil {
		t.Fatalf("SetLang(\"zh\") returned error: %v", err)
	}
	if got := TIn("fr", "settings.language"); got != "语言" {
		t.Fatalf("TIn unknown locale = %q, want current locale translation", got)
	}
}

func TestTInRequestedLocaleMissingKeyFallsBackToEnglish(t *testing.T) {
	t.Cleanup(func() { SetLang("en") })

	key := "settings.language"
	zh := loadLocaleForTest(t, "zh")
	gap := copyStringMap(zh)
	delete(gap, key)
	withCachedLocaleForTest(t, "zh", gap)

	if got, want := TIn("zh", key), "Language"; got != want {
		t.Fatalf("TIn(\"zh\", %q) = %q, want English fallback %q", key, got, want)
	}
}

func TestTInCachesLocaleMaps(t *testing.T) {
	resetCacheForTest(t)

	if got := TIn("zh", "settings.language"); got != "语言" {
		t.Fatalf("first TIn(\"zh\") = %q, want %q", got, "语言")
	}
	cacheMu.RLock()
	first := cache["zh"]
	firstPtr := reflect.ValueOf(first).Pointer()
	cacheMu.RUnlock()
	if first == nil {
		t.Fatal("TIn did not populate zh cache")
	}

	if got := TIn("zh", "common.quit"); got != "退出" {
		t.Fatalf("second TIn(\"zh\") = %q, want %q", got, "退出")
	}
	cacheMu.RLock()
	second := cache["zh"]
	secondPtr := reflect.ValueOf(second).Pointer()
	cacheMu.RUnlock()
	if firstPtr == 0 || firstPtr != secondPtr {
		t.Fatalf("zh cache identity changed: first=%d second=%d", firstPtr, secondPtr)
	}
}
