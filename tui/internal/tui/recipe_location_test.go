package tui

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

type recipeLocationRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn recipeLocationRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func recipeLocationResponse() *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body: io.NopCloser(strings.NewReader(`{
			"city": "Austin",
			"region": "Texas",
			"country": "US",
			"timezone": "America/Chicago",
			"loc": "30.2672,-97.7431"
		}`)),
	}
}

func TestSubstituteGreetPlaceholdersLocationReusesResolvedLocation(t *testing.T) {
	humanDir := t.TempDir()
	manifest, err := json.MarshalIndent(map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
	}, "", "  ")
	if err != nil {
		t.Fatalf("marshal human manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(humanDir, ".agent.json"), manifest, 0o644); err != nil {
		t.Fatalf("write human manifest: %v", err)
	}

	var requests atomic.Int32
	previousTransport := http.DefaultTransport
	http.DefaultTransport = recipeLocationRoundTripFunc(func(_ *http.Request) (*http.Response, error) {
		requests.Add(1)
		return recipeLocationResponse(), nil
	})
	t.Cleanup(func() {
		http.DefaultTransport = previousTransport
	})

	got := SubstituteGreetPlaceholders("location={{location}}", "human", humanDir, "en")
	if got != "location=Austin, Texas, US" {
		t.Fatalf("substituted greet = %q; want resolved location", got)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("recipe location fallback made %d resolver requests; want exactly 1", got)
	}

	// Persistence is part of the synchronous recipe fallback contract. A return
	// from substitution is therefore sufficient evidence; no polling or sleep.
	node, err := fs.ReadAgent(humanDir)
	if err != nil {
		t.Fatalf("read persisted human manifest: %v", err)
	}
	if node.Location == nil || node.Location.City != "Austin" || node.Location.ResolvedAt == "" {
		t.Fatalf("persisted location = %#v; want the resolved Austin value", node.Location)
	}
}

// TestSubstituteGreetPlaceholdersLeavesRetiredSoulDelayLiteral pins the
// explicit unsupported behavior for the retired `{{soul_delay}}` token: an
// old custom recipe that still carries it gets the literal token back in its
// generated .prompt. No Soul value is ever substituted, and the recipe
// application layer has no unknown-placeholder rule to fail on it.
func TestSubstituteGreetPlaceholdersLeavesRetiredSoulDelayLiteral(t *testing.T) {
	got := SubstituteGreetPlaceholders("lang={{lang}} delay={{soul_delay}}", "human", "", "en")
	if got != "lang=en delay={{soul_delay}}" {
		t.Fatalf("substituted greet = %q; want retired token left verbatim", got)
	}
}
