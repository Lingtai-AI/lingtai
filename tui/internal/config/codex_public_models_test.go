package config

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// codexModelsRoundTripper serves one canned response (or error) for every
// request, mirroring the injected-transport seam used by
// kernel_release_install_test.go's kernelManifestRoundTripper. No test in
// this file dials a live endpoint.
type codexModelsRoundTripper struct {
	status   int
	body     string
	err      error
	requests []string
}

func (rt *codexModelsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	rt.requests = append(rt.requests, req.URL.String())
	if rt.err != nil {
		return nil, rt.err
	}
	status := rt.status
	if status == 0 {
		status = http.StatusOK
	}
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(rt.body)),
	}, nil
}

func codexModelsClient(body string) (*http.Client, *codexModelsRoundTripper) {
	rt := &codexModelsRoundTripper{body: body}
	return &http.Client{Transport: rt}, rt
}

func codexModelsDirectoryFixture(entries ...string) string {
	return fmt.Sprintf(`{"models":[%s]}`, strings.Join(entries, ","))
}

func codexModelEntry(slug, displayName string) string {
	return fmt.Sprintf(`{"slug":%q,"display_name":%q,"generation":99,"visibility":"internal","account_gated":true}`, slug, displayName)
}

// --- ParseCodexPublicModels: the binding display_name filter -------------

func TestParseCodexPublicModels_ArbitrarySlugWithGPTLabelAccepted(t *testing.T) {
	body := codexModelsDirectoryFixture(codexModelEntry("aardvark-1", "GPT Aardvark"))
	got, err := ParseCodexPublicModels([]byte(body))
	if err != nil {
		t.Fatalf("ParseCodexPublicModels: %v", err)
	}
	want := []CodexModelOption{{Slug: "aardvark-1", Label: "GPT Aardvark"}}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func TestParseCodexPublicModels_GPTNumericSlugWithNonGPTLabelRejected(t *testing.T) {
	// The slug looks exactly like a real Codex id, but the binding filter is
	// display_name, not slug shape — a non-"GPT"-prefixed label must be
	// excluded even for an otherwise plausible gpt-* slug.
	body := codexModelsDirectoryFixture(codexModelEntry("gpt-5.9-nova", "Nova 5.9 (internal)"))
	if _, err := ParseCodexPublicModels([]byte(body)); err == nil {
		t.Fatalf("expected an error for a directory with no GPT-prefixed display names")
	}
}

func TestParseCodexPublicModels_CaseSensitivePrefix(t *testing.T) {
	// Lowercase "gpt" must NOT match — the filter is exact-case "GPT".
	body := codexModelsDirectoryFixture(codexModelEntry("gpt-5.6-sol", "gpt-5.6 Sol"))
	if _, err := ParseCodexPublicModels([]byte(body)); err == nil {
		t.Fatalf("expected an error: lowercase display_name must not satisfy the case-sensitive GPT prefix")
	}
}

func TestParseCodexPublicModels_LabelDistinctFromSlug(t *testing.T) {
	body := codexModelsDirectoryFixture(codexModelEntry("gpt-5.6-sol", "GPT-5.6 Sol"))
	got, err := ParseCodexPublicModels([]byte(body))
	if err != nil {
		t.Fatalf("ParseCodexPublicModels: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("got %d entries, want 1", len(got))
	}
	if got[0].Slug != "gpt-5.6-sol" {
		t.Fatalf("Slug = %q, want the persisted id unchanged", got[0].Slug)
	}
	if got[0].Label != "GPT-5.6 Sol" {
		t.Fatalf("Label = %q, want the display_name verbatim", got[0].Label)
	}
	if got[0].Slug == got[0].Label {
		t.Fatalf("Slug and Label must differ for this fixture: both are %q", got[0].Slug)
	}
}

func TestParseCodexPublicModels_DeduplicatesBySlug(t *testing.T) {
	body := codexModelsDirectoryFixture(
		codexModelEntry("gpt-5.6-sol", "GPT-5.6 Sol"),
		codexModelEntry("gpt-5.6-sol", "GPT-5.6 Sol (renamed)"),
		codexModelEntry("gpt-6-astra", "GPT-6 Astra"),
	)
	got, err := ParseCodexPublicModels([]byte(body))
	if err != nil {
		t.Fatalf("ParseCodexPublicModels: %v", err)
	}
	want := []CodexModelOption{
		{Slug: "gpt-5.6-sol", Label: "GPT-5.6 Sol"},
		{Slug: "gpt-6-astra", Label: "GPT-6 Astra"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %#v, want %#v", i, got[i], want[i])
		}
	}
}

func TestParseCodexPublicModels_MalformedJSONFailsOpen(t *testing.T) {
	if _, err := ParseCodexPublicModels([]byte("not json")); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestParseCodexPublicModels_EmptyDirectoryFailsOpen(t *testing.T) {
	if _, err := ParseCodexPublicModels([]byte(`{"models":[]}`)); err == nil {
		t.Fatal("expected an error for an empty models array")
	}
}

func TestParseCodexPublicModels_NoQualifyingEntriesFailsOpen(t *testing.T) {
	body := codexModelsDirectoryFixture(codexModelEntry("gpt-5.9", "o5.9 Preview"))
	if _, err := ParseCodexPublicModels([]byte(body)); err == nil {
		t.Fatal("expected an error when no entry's display_name starts with GPT")
	}
}

// --- fetchCodexPublicModels: bounded network fetch ------------------------

func TestFetchCodexPublicModels_Success(t *testing.T) {
	client, rt := codexModelsClient(codexModelsDirectoryFixture(codexModelEntry("gpt-6-astra", "GPT-6 Astra")))
	got, err := FetchCodexPublicModels(client)
	if err != nil {
		t.Fatalf("FetchCodexPublicModels: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "gpt-6-astra" {
		t.Fatalf("got %#v", got)
	}
	if len(rt.requests) != 1 || rt.requests[0] != CodexPublicModelsURL {
		t.Fatalf("requests = %#v, want exactly one request to %q", rt.requests, CodexPublicModelsURL)
	}
}

func TestFetchCodexPublicModels_HTTPErrorStatus(t *testing.T) {
	client := &http.Client{Transport: &codexModelsRoundTripper{status: 500, body: "boom"}}
	if _, err := FetchCodexPublicModels(client); err == nil {
		t.Fatal("expected an error for HTTP 500")
	}
}

func TestFetchCodexPublicModels_TransportError(t *testing.T) {
	// Stands in for a timeout/connection failure: any transport-level error
	// must fail open exactly like a non-200 or malformed body.
	client := &http.Client{Transport: &codexModelsRoundTripper{err: errors.New("simulated timeout")}}
	if _, err := FetchCodexPublicModels(client); err == nil {
		t.Fatal("expected an error for a transport failure")
	}
}

func TestFetchCodexPublicModels_OversizedBodyRejected(t *testing.T) {
	huge := strings.Repeat("x", codexPublicModelsMaxBytes+1)
	body := `{"models":[{"slug":"gpt-5.6-sol","display_name":"GPT-5.6 Sol","padding":"` + huge + `"}]}`
	client := &http.Client{Transport: &codexModelsRoundTripper{body: body}}
	if _, err := FetchCodexPublicModels(client); err == nil {
		t.Fatal("expected an error for a response over the size bound")
	}
}

func TestFetchCodexPublicModels_MalformedBodyFailsOpen(t *testing.T) {
	client := &http.Client{Transport: &codexModelsRoundTripper{body: "{not valid json"}}
	if _, err := FetchCodexPublicModels(client); err == nil {
		t.Fatal("expected an error for a malformed response body")
	}
}

// --- cache: last-good persistence -----------------------------------------

func TestCodexPublicModelsCache_RoundTrip(t *testing.T) {
	globalDir := t.TempDir()
	want := []CodexModelOption{{Slug: "gpt-5.6-sol", Label: "GPT-5.6 Sol"}}
	if err := SaveCodexPublicModelsCache(globalDir, want); err != nil {
		t.Fatalf("SaveCodexPublicModelsCache: %v", err)
	}
	got, ok := LoadCachedCodexPublicModels(globalDir)
	if !ok {
		t.Fatal("LoadCachedCodexPublicModels ok = false, want true")
	}
	if len(got) != 1 || got[0] != want[0] {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	// The cache lives under globalDir/cache, never under a saved preset tree.
	if _, err := os.Stat(filepath.Join(globalDir, "cache", codexPublicModelsCacheFile)); err != nil {
		t.Fatalf("expected cache file under globalDir/cache: %v", err)
	}
}

func TestCodexPublicModelsCache_MissingFileIsNotOK(t *testing.T) {
	globalDir := t.TempDir()
	if _, ok := LoadCachedCodexPublicModels(globalDir); ok {
		t.Fatal("expected ok=false for a globalDir with no cache yet")
	}
}

func TestCodexPublicModelsCache_MalformedFileIsNotOK(t *testing.T) {
	globalDir := t.TempDir()
	path := codexPublicModelsCachePath(globalDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCachedCodexPublicModels(globalDir); ok {
		t.Fatal("expected ok=false for a malformed cache file")
	}
}

func TestCodexPublicModelsCache_EmptyArrayIsNotOK(t *testing.T) {
	globalDir := t.TempDir()
	path := codexPublicModelsCachePath(globalDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := LoadCachedCodexPublicModels(globalDir); ok {
		t.Fatal("expected ok=false for an empty cached array")
	}
}

// --- RefreshCodexPublicModels: the fail-open orchestration ----------------

func TestRefreshCodexPublicModels_LiveSuccessWritesCache(t *testing.T) {
	globalDir := t.TempDir()
	client, _ := codexModelsClient(codexModelsDirectoryFixture(codexModelEntry("gpt-6-astra", "GPT-6 Astra")))
	result := RefreshCodexPublicModels(globalDir, client)
	if !result.Live {
		t.Fatal("Live = false, want true for a successful fetch")
	}
	if len(result.Options) != 1 || result.Options[0].Slug != "gpt-6-astra" {
		t.Fatalf("Options = %#v", result.Options)
	}
	cached, ok := LoadCachedCodexPublicModels(globalDir)
	if !ok || len(cached) != 1 || cached[0].Slug != "gpt-6-astra" {
		t.Fatalf("cache after live success = %#v (ok=%v), want the fetched options", cached, ok)
	}
}

func TestRefreshCodexPublicModels_FailedFetchFallsBackToCacheWithoutClobberingIt(t *testing.T) {
	globalDir := t.TempDir()
	good := []CodexModelOption{{Slug: "gpt-5.6-sol", Label: "GPT-5.6 Sol"}}
	if err := SaveCodexPublicModelsCache(globalDir, good); err != nil {
		t.Fatalf("seed cache: %v", err)
	}
	failingClient := &http.Client{Transport: &codexModelsRoundTripper{status: 503, body: "unavailable"}}

	result := RefreshCodexPublicModels(globalDir, failingClient)
	if result.Live {
		t.Fatal("Live = true, want false for a failed fetch")
	}
	if len(result.Options) != 1 || result.Options[0] != good[0] {
		t.Fatalf("Options = %#v, want the last-good cache %#v", result.Options, good)
	}

	// The bad response must not have touched the cache file on disk.
	cached, ok := LoadCachedCodexPublicModels(globalDir)
	if !ok || len(cached) != 1 || cached[0] != good[0] {
		t.Fatalf("cache after failed fetch = %#v (ok=%v), want untouched %#v", cached, ok, good)
	}
}

func TestRefreshCodexPublicModels_FailedFetchNoCacheUsesStaticFallback(t *testing.T) {
	globalDir := t.TempDir()
	failingClient := &http.Client{Transport: &codexModelsRoundTripper{err: errors.New("network unreachable")}}

	result := RefreshCodexPublicModels(globalDir, failingClient)
	if result.Live {
		t.Fatal("Live = true, want false for a failed fetch")
	}
	want := DefaultCodexModelOptions()
	if len(result.Options) != len(want) {
		t.Fatalf("Options = %#v, want the static fallback %#v", result.Options, want)
	}
	for i := range want {
		if result.Options[i] != want[i] {
			t.Fatalf("Options[%d] = %#v, want %#v", i, result.Options[i], want[i])
		}
	}
}

func TestRefreshCodexPublicModels_EmptyGlobalDirSkipsCache(t *testing.T) {
	failingClient := &http.Client{Transport: &codexModelsRoundTripper{err: errors.New("offline")}}
	result := RefreshCodexPublicModels("", failingClient)
	if result.Live {
		t.Fatal("Live = true, want false")
	}
	if len(result.Options) == 0 {
		t.Fatal("Options must never be empty, even with no globalDir and a failed fetch")
	}
}

// --- ParseCodexPublicModels: whitespace must not satisfy the prefix -------

func TestParseCodexPublicModels_LeadingSpaceDisplayNameRejected(t *testing.T) {
	// A leading space reads as "GPT ..." to a human but does not START
	// WITH "GPT" — HasPrefix is exact, not trimmed. Pins that the filter
	// is never accidentally loosened with a TrimSpace.
	body := codexModelsDirectoryFixture(codexModelEntry("gpt-6-astra", " GPT-6 Astra"))
	if _, err := ParseCodexPublicModels([]byte(body)); err == nil {
		t.Fatal("expected an error: a leading-space display_name must not satisfy the GPT prefix")
	}
}

// --- fetchCodexPublicModels: the timeout is actually enforced --------------

func TestFetchCodexPublicModels_TimeoutIsEnforced(t *testing.T) {
	// A local httptest server (never the live public endpoint) that sleeps
	// past a short injected client timeout proves the bound itself is
	// enforced, not just that some error string comes back for any
	// transport failure (TestFetchCodexPublicModels_TransportError already
	// covers the latter).
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
	}))
	defer srv.Close()

	client := &http.Client{Timeout: 20 * time.Millisecond}
	start := time.Now()
	_, err := fetchCodexPublicModels(codexPublicModelsFetchOptions{HTTPClient: client, URL: srv.URL})
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected a timeout error")
	}
	if elapsed >= 200*time.Millisecond {
		t.Fatalf("fetch took %v, want it aborted well before the handler's 200ms sleep", elapsed)
	}
}

// --- cache: structurally-valid-but-unusable entries are not a good cache --

func TestCodexPublicModelsCache_NullEntryIsNotOK(t *testing.T) {
	// [null] unmarshals into one zero-value CodexModelOption
	// ({Slug:"",Label:""}) — structurally valid JSON, len 1, but not a
	// usable picker entry. Must be treated like an empty cache (ok=false,
	// fall through to the static fallback), never surfaced as a "good"
	// one-blank-entry cache.
	globalDir := t.TempDir()
	path := codexPublicModelsCachePath(globalDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("[null]"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got, ok := LoadCachedCodexPublicModels(globalDir); ok {
		t.Fatalf("expected ok=false for a null-entry cache, got ok=true options=%#v", got)
	}
}

func TestParseCodexPublicModelsCache_FiltersEmptySlugEntries(t *testing.T) {
	got, err := ParseCodexPublicModelsCache([]byte(`[null,{"slug":"gpt-5.6-sol","label":"GPT-5.6 Sol"},{"slug":"","label":"blank"}]`))
	if err != nil {
		t.Fatalf("ParseCodexPublicModelsCache: %v", err)
	}
	if len(got) != 1 || got[0].Slug != "gpt-5.6-sol" {
		t.Fatalf("got %#v, want only the one entry with a non-empty slug", got)
	}
}
