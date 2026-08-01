// internal/fs/location_test.go
package fs

import (
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type locationRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn locationRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func installLocationTransport(t *testing.T, fn locationRoundTripFunc) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = fn
	t.Cleanup(func() {
		http.DefaultTransport = previous
	})
}

func locationResponse() *http.Response {
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

func writeLocationManifest(t *testing.T, humanDir string, manifest map[string]interface{}) {
	t.Helper()
	if err := os.MkdirAll(humanDir, 0o755); err != nil {
		t.Fatalf("create human dir: %v", err)
	}
	data, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	if err := os.WriteFile(filepath.Join(humanDir, ".agent.json"), data, 0o644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func readLocationManifest(t *testing.T, humanDir string) map[string]interface{} {
	t.Helper()
	raw, err := ReadAgentRaw(humanDir)
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	return raw
}

func waitLocationSignal(t *testing.T, signal <-chan struct{}, label string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(2 * time.Second):
		t.Fatalf("timed out waiting for %s", label)
	}
}

func waitLocationUpdates(t *testing.T, updates ...<-chan struct{}) {
	t.Helper()
	for i, update := range updates {
		select {
		case <-update:
		case <-time.After(2 * time.Second):
			t.Fatalf("timed out waiting for location update %d", i+1)
		}
	}
}

func TestLocationStale_Empty(t *testing.T) {
	loc := Location{}
	if !LocationStale(loc, time.Hour) {
		t.Error("empty Location should be stale")
	}
}

func TestLocationStale_Recent(t *testing.T) {
	loc := Location{
		ResolvedAt: time.Now().Format(time.RFC3339),
	}
	if LocationStale(loc, time.Hour) {
		t.Error("Location resolved just now should NOT be stale")
	}
}

func TestLocationStale_Old(t *testing.T) {
	loc := Location{
		ResolvedAt: time.Now().Add(-2 * time.Hour).Format(time.RFC3339),
	}
	if !LocationStale(loc, time.Hour) {
		t.Error("Location resolved 2h ago should be stale with 1h maxAge")
	}
}

func TestUpdateHumanLocationCoalescesSamePath(t *testing.T) {
	humanDir := t.TempDir()
	writeLocationManifest(t, humanDir, map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
	})

	var requests atomic.Int32
	firstRequest := make(chan struct{})
	secondRequest := make(chan struct{})
	release := make(chan struct{})
	installLocationTransport(t, func(_ *http.Request) (*http.Response, error) {
		switch request := requests.Add(1); request {
		case 1:
			close(firstRequest)
		case 2:
			close(secondRequest)
		}
		<-release
		return locationResponse(), nil
	})

	firstDone := make(chan struct{})
	go func() {
		UpdateHumanLocation(humanDir)
		close(firstDone)
	}()
	waitLocationSignal(t, firstRequest, "first resolver request")

	secondDone := make(chan struct{})
	go func() {
		alias := humanDir + string(os.PathSeparator) + "."
		UpdateHumanLocation(alias)
		close(secondDone)
	}()

	// Current main admits the second caller into ResolveLocation while the first
	// caller is still blocked. The fixed implementation keeps it behind the
	// same-path mutex until the first commit makes the location fresh.
	select {
	case <-secondRequest:
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	waitLocationUpdates(t, firstDone, secondDone)

	if got := requests.Load(); got != 1 {
		t.Fatalf("same-path updates made %d resolver requests; want exactly 1", got)
	}
}

func TestUpdateHumanLocationMergesLatestManifest(t *testing.T) {
	humanDir := t.TempDir()
	writeLocationManifest(t, humanDir, map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
		"nickname":   "before-lookup",
	})

	started := make(chan struct{})
	release := make(chan struct{})
	installLocationTransport(t, func(_ *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return locationResponse(), nil
	})

	done := make(chan struct{})
	go func() {
		UpdateHumanLocation(humanDir)
		close(done)
	}()
	waitLocationSignal(t, started, "blocked resolver request")

	latest := readLocationManifest(t, humanDir)
	latest["nickname"] = "changed-during-lookup"
	writeLocationManifest(t, humanDir, latest)
	close(release)
	waitLocationUpdates(t, done)

	got := readLocationManifest(t, humanDir)
	if got["nickname"] != "changed-during-lookup" {
		t.Fatalf("unrelated latest field was clobbered: nickname=%v", got["nickname"])
	}
	if _, ok := got["location"]; !ok {
		t.Fatal("resolved location was not committed")
	}
}

func TestUpdateHumanLocationUsesUniqueSiblingTemp(t *testing.T) {
	humanDir := t.TempDir()
	writeLocationManifest(t, humanDir, map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
	})

	fixedTemp := filepath.Join(humanDir, ".agent.json.tmp")
	if err := os.Mkdir(fixedTemp, 0o755); err != nil {
		t.Fatalf("obstruct fixed temp path: %v", err)
	}
	started := make(chan struct{})
	release := make(chan struct{})
	installLocationTransport(t, func(_ *http.Request) (*http.Response, error) {
		close(started)
		<-release
		return locationResponse(), nil
	})

	done := make(chan struct{})
	go func() {
		UpdateHumanLocation(humanDir)
		close(done)
	}()
	waitLocationSignal(t, started, "fixed-temp resolver request")
	close(release)
	waitLocationUpdates(t, done)

	got := readLocationManifest(t, humanDir)
	if _, ok := got["location"]; !ok {
		t.Fatal("fixed .agent.json.tmp obstruction prevented the location update")
	}
	info, err := os.Stat(fixedTemp)
	if err != nil {
		t.Fatalf("pre-existing temp obstruction disappeared: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("pre-existing temp obstruction changed type: mode=%v", info.Mode())
	}
	fixedMatches, err := filepath.Glob(filepath.Join(humanDir, ".agent.json.tmp*"))
	if err != nil {
		t.Fatalf("list fixed temp paths: %v", err)
	}
	if len(fixedMatches) != 1 || fixedMatches[0] != fixedTemp {
		t.Fatalf("unexpected fixed temp paths: %v", fixedMatches)
	}
	generatedTemps, err := filepath.Glob(filepath.Join(humanDir, "..agent.json.tmp-*"))
	if err != nil {
		t.Fatalf("list generated temp residue: %v", err)
	}
	if len(generatedTemps) != 0 {
		t.Fatalf("generated temp residue remains: %v", generatedTemps)
	}
}

func TestUpdateHumanLocationConcurrentCallersLeaveValidManifest(t *testing.T) {
	humanDir := t.TempDir()
	writeLocationManifest(t, humanDir, map[string]interface{}{
		"agent_name": "human",
		"address":    "human",
		"admin":      nil,
	})

	var requests atomic.Int32
	installLocationTransport(t, func(_ *http.Request) (*http.Response, error) {
		requests.Add(1)
		return locationResponse(), nil
	})

	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	firstDone := make(chan struct{})
	secondDone := make(chan struct{})
	go func() {
		ready <- struct{}{}
		<-start
		UpdateHumanLocation(humanDir)
		close(firstDone)
	}()
	go func() {
		ready <- struct{}{}
		<-start
		alias := humanDir + string(os.PathSeparator) + "."
		UpdateHumanLocation(alias)
		close(secondDone)
	}()
	<-ready
	<-ready
	close(start)
	waitLocationUpdates(t, firstDone, secondDone)

	if got := requests.Load(); got != 1 {
		t.Fatalf("concurrent same-manifest updates made %d resolver requests; want 1", got)
	}
	node, err := ReadAgent(humanDir)
	if err != nil {
		t.Fatalf("read final manifest: %v", err)
	}
	if node.Location == nil || node.Location.City != "Austin" || node.Location.ResolvedAt == "" {
		t.Fatalf("final location = %#v; want one valid resolved value", node.Location)
	}
	generatedTemps, err := filepath.Glob(filepath.Join(humanDir, "..agent.json.tmp-*"))
	if err != nil {
		t.Fatalf("list generated temp residue: %v", err)
	}
	if len(generatedTemps) != 0 {
		t.Fatalf("generated temp residue remains: %v", generatedTemps)
	}
}

func captureLocationPanic(fn func()) (panicValue interface{}) {
	defer func() {
		panicValue = recover()
	}()
	fn()
	return nil
}

func TestHumanLocationUpdatesIgnoreMissingMalformedManifests(t *testing.T) {
	cases := []struct {
		name     string
		contents *string
	}{
		{name: "missing"},
		{name: "invalid JSON", contents: stringPointer("{")},
		{name: "null JSON", contents: stringPointer("null")},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			humanDir := t.TempDir()
			manifestPath := filepath.Join(humanDir, ".agent.json")
			if tc.contents != nil {
				if err := os.WriteFile(manifestPath, []byte(*tc.contents), 0o644); err != nil {
					t.Fatalf("write malformed manifest: %v", err)
				}
			}

			var requests atomic.Int32
			installLocationTransport(t, func(_ *http.Request) (*http.Response, error) {
				requests.Add(1)
				return locationResponse(), nil
			})

			updatePanic := captureLocationPanic(func() {
				UpdateHumanLocation(humanDir)
			})
			if updatePanic != nil {
				t.Errorf("UpdateHumanLocation panicked: %v", updatePanic)
			}
			if got := requests.Load(); got != 0 {
				t.Errorf("UpdateHumanLocation made %d resolver requests; want 0", got)
			}

			storePanic := captureLocationPanic(func() {
				StoreResolvedHumanLocation(humanDir, Location{
					City:       "Austin",
					ResolvedAt: time.Now().Format(time.RFC3339),
				})
			})
			if storePanic != nil {
				t.Errorf("StoreResolvedHumanLocation panicked: %v", storePanic)
			}
			if got := requests.Load(); got != 0 {
				t.Errorf("location entry points made %d resolver requests; want 0", got)
			}

			if tc.contents == nil {
				if _, err := os.Stat(manifestPath); !os.IsNotExist(err) {
					t.Errorf("missing manifest was created: err=%v", err)
				}
				return
			}
			got, err := os.ReadFile(manifestPath)
			if err != nil {
				t.Fatalf("read malformed manifest after no-op: %v", err)
			}
			if string(got) != *tc.contents {
				t.Errorf("malformed manifest changed: got %q want %q", got, *tc.contents)
			}
		})
	}
}

func stringPointer(value string) *string {
	return &value
}
