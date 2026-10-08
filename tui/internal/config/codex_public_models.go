package config

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

// CodexPublicModelsURL is the fixed public Codex model-picker directory this
// TUI reads for Codex model suggestions. It is producer-published public
// metadata (no OAuth/API-key request, no Codex CLI dependency) — it is NOT
// proof that any given account can actually use a listed model; account
// entitlement is discovered only by trying the model.
const CodexPublicModelsURL = "https://raw.githubusercontent.com/openai/codex/main/codex-rs/models-manager/models.json"

// codexPublicModelsMaxBytes bounds the response body so a misbehaving or
// compromised host cannot make the TUI buffer unbounded memory.
const codexPublicModelsMaxBytes = 2 * 1024 * 1024 // ~2 MiB

// codexPublicModelsTimeout bounds the whole request so a hung connection
// never blocks preset-editor entry beyond a few seconds.
const codexPublicModelsTimeout = 5 * time.Second

// codexPublicModelsCacheFile is the last-good cache written under the
// caller's globalDir. It lives under globalDir/cache, never under the saved
// preset tree, and never blocks or gates a preset Save.
const codexPublicModelsCacheFile = "codex_public_models.json"

// CodexModelOption is one Codex model suggestion: the persisted slug (what
// the TUI writes into manifest.llm.model) and its public display label (what
// the TUI renders). Label equals Slug for the offline static fallback and
// for any upstream entry the source never renamed.
type CodexModelOption struct {
	Slug  string `json:"slug"`
	Label string `json:"label"`
}

// codexModelsDirectory mirrors only the subset of openai/codex's
// codex-rs/models-manager/models.json this TUI reads (models[].slug and
// models[].display_name). Every other field — including numeric-generation,
// visibility, and account-entitlement metadata — is deliberately never
// decoded: this TUI does not filter on it, and public metadata is a
// suggestion, not proof an account can use a given model.
type codexModelsDirectory struct {
	Models []struct {
		Slug        string `json:"slug"`
		DisplayName string `json:"display_name"`
	} `json:"models"`
}

// DefaultCodexModelOptions is the compiled-in offline/static fallback
// lineup: seeded into a fresh preset editor before any refresh completes,
// and used whenever both a live fetch and the on-disk cache are unavailable.
// It is deliberately small and stable; slug and label are identical here.
func DefaultCodexModelOptions() []CodexModelOption {
	return []CodexModelOption{
		{Slug: "gpt-5.6-sol", Label: "gpt-5.6-sol"},
		{Slug: "gpt-6-astra", Label: "gpt-6-astra"},
		{Slug: "gpt-5.6-terra", Label: "gpt-5.6-terra"},
		{Slug: "gpt-5.6-luna", Label: "gpt-5.6-luna"},
	}
}

// ParseCodexPublicModels filters the raw models.json bytes down to entries
// whose display_name STARTS WITH the exact case-sensitive prefix "GPT" —
// the sole binding filter. It does not filter by slug shape, numeric
// generation, visibility, or any entitlement field, and it deduplicates by
// slug (first occurrence wins, order otherwise preserved). An entry with an
// empty slug is skipped (there is nothing to persist). It returns an error
// for malformed JSON or a directory with zero qualifying entries, so callers
// can fail open to the cache or the static fallback instead of showing an
// empty picker.
func ParseCodexPublicModels(body []byte) ([]CodexModelOption, error) {
	var dir codexModelsDirectory
	if err := json.Unmarshal(body, &dir); err != nil {
		return nil, fmt.Errorf("parse codex public models directory: %w", err)
	}
	seen := make(map[string]struct{}, len(dir.Models))
	out := make([]CodexModelOption, 0, len(dir.Models))
	for _, m := range dir.Models {
		if m.Slug == "" || !strings.HasPrefix(m.DisplayName, "GPT") {
			continue
		}
		if _, ok := seen[m.Slug]; ok {
			continue
		}
		seen[m.Slug] = struct{}{}
		out = append(out, CodexModelOption{Slug: m.Slug, Label: m.DisplayName})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("codex public models directory had no GPT-prefixed entries")
	}
	return out, nil
}

// codexPublicModelsFetchOptions injects side effects for tests: an
// alternate HTTP client (e.g. one backed by a fake RoundTripper) and/or a
// non-default URL. Production leaves both empty.
type codexPublicModelsFetchOptions struct {
	HTTPClient *http.Client
	URL        string
}

// fetchCodexPublicModels performs the bounded network fetch (timeout +
// response-size cap) and parses the result. It does not touch the on-disk
// cache — RefreshCodexPublicModels is the caller-facing entry point that
// adds cache read/write around this.
func fetchCodexPublicModels(opts codexPublicModelsFetchOptions) ([]CodexModelOption, error) {
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: codexPublicModelsTimeout}
	}
	url := opts.URL
	if url == "" {
		url = CodexPublicModelsURL
	}
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("codex public models directory returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, codexPublicModelsMaxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > codexPublicModelsMaxBytes {
		return nil, fmt.Errorf("codex public models directory response exceeded %d bytes", codexPublicModelsMaxBytes)
	}
	return ParseCodexPublicModels(body)
}

// FetchCodexPublicModels performs one bounded network fetch against the
// fixed public directory URL using client (a production 5s-timeout client
// when nil), with no cache or fallback involved. Exported so callers that
// only want the raw network attempt (e.g. an explicit manual refresh) don't
// have to go through the cache-aware RefreshCodexPublicModels.
func FetchCodexPublicModels(client *http.Client) ([]CodexModelOption, error) {
	return fetchCodexPublicModels(codexPublicModelsFetchOptions{HTTPClient: client})
}

func codexPublicModelsCachePath(globalDir string) string {
	return filepath.Join(globalDir, "cache", codexPublicModelsCacheFile)
}

// ParseCodexPublicModelsCache decodes the TUI's own last-good cache shape: a
// plain JSON array of already-filtered, already-deduplicated
// {slug,label} pairs — distinct from the upstream directory's raw shape.
func ParseCodexPublicModelsCache(data []byte) ([]CodexModelOption, error) {
	var raw []CodexModelOption
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	// A structurally valid element can still be unusable: a JSON `null`
	// entry decodes to a zero-value {Slug:"",Label:""}, which is not a
	// pickable model. Drop empty-slug entries here so the caller's
	// len(options)==0 check (LoadCachedCodexPublicModels) correctly treats
	// an all-blank cache as a miss and falls through to the static
	// fallback, instead of reporting a "good" cache that renders a blank
	// picker entry.
	options := make([]CodexModelOption, 0, len(raw))
	for _, opt := range raw {
		if opt.Slug == "" {
			continue
		}
		options = append(options, opt)
	}
	return options, nil
}

// LoadCachedCodexPublicModels reads the last-good cache written by a prior
// successful fetch. ok is false for a missing, unreadable, malformed, or
// empty cache — never an error the caller must branch on, since a cache miss
// is exactly the "fall through to the static fallback" case.
func LoadCachedCodexPublicModels(globalDir string) (options []CodexModelOption, ok bool) {
	if globalDir == "" {
		return nil, false
	}
	data, err := os.ReadFile(codexPublicModelsCachePath(globalDir))
	if err != nil {
		return nil, false
	}
	options, err = ParseCodexPublicModelsCache(data)
	if err != nil || len(options) == 0 {
		return nil, false
	}
	return options, true
}

// SaveCodexPublicModelsCache atomically writes the last-good fetch result
// under globalDir/cache — never under the saved preset tree. Callers treat a
// write failure as non-fatal: the in-memory options already reflect this
// fetch for the current session, and only the next launch's cache read is
// affected.
func SaveCodexPublicModelsCache(globalDir string, options []CodexModelOption) error {
	if globalDir == "" {
		return fmt.Errorf("codex public models cache: empty globalDir")
	}
	data, err := json.Marshal(options)
	if err != nil {
		return err
	}
	path := codexPublicModelsCachePath(globalDir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return fs.WriteFileAtomic(path, data, 0o644)
}

// CodexPublicModelsResult is the outcome of one bounded refresh attempt.
type CodexPublicModelsResult struct {
	Options []CodexModelOption
	// Live is true only when THIS call's own network fetch succeeded — false
	// when the result came from the on-disk cache or the static fallback.
	Live bool
}

// RefreshCodexPublicModels performs one bounded, best-effort attempt to
// refresh the Codex public model directory:
//
//  1. Try the network (client, or a production 5s-timeout client when nil).
//     Success overwrites the last-good cache and returns those options.
//  2. On ANY failure — timeout, transport error, non-200, malformed/empty
//     body, or an oversized body — fall back to the last-good on-disk
//     cache. A failed fetch NEVER writes to or clears the existing cache
//     file, so one bad response cannot erase previously good data.
//  3. If there is no usable cache either (first run, or cache also
//     unreadable), fall back to the compiled-in DefaultCodexModelOptions.
//
// The returned Options slice is therefore never empty. globalDir may be
// empty (e.g. a host that has none), which simply skips steps 1's cache
// write and step 2's cache read.
func RefreshCodexPublicModels(globalDir string, client *http.Client) CodexPublicModelsResult {
	if options, err := fetchCodexPublicModels(codexPublicModelsFetchOptions{HTTPClient: client}); err == nil {
		if globalDir != "" {
			_ = SaveCodexPublicModelsCache(globalDir, options)
		}
		return CodexPublicModelsResult{Options: options, Live: true}
	}
	if cached, ok := LoadCachedCodexPublicModels(globalDir); ok {
		return CodexPublicModelsResult{Options: cached}
	}
	return CodexPublicModelsResult{Options: DefaultCodexModelOptions()}
}
