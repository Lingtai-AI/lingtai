package streamprogress

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Pinned byte-for-byte with lingtai-kernel tests/test_stream_progress.py.
var knownVectors = []struct {
	agentID string
	seed    uint16
	ports   []int
}{
	{"20260826-120000-abcd", 58026, []int{59026, 46945, 54864, 42783, 50702, 58621, 46540, 54459}},
	{"orch", 4407, []int{45407, 53326, 41245, 49164, 57083, 45002, 52921, 60840}},
	{"", 29159, []int{50159, 58078, 45997, 53916, 41835, 49754, 57673, 45592}},
	{"器灵-01", 38923, []int{59923, 47842, 55761, 43680, 51599, 59518, 47437, 55356}},
}

func TestCandidatePortsKnownVectors(t *testing.T) {
	for _, v := range knownVectors {
		if got := Seed(v.agentID); got != v.seed {
			t.Errorf("Seed(%q) = %d, want %d", v.agentID, got, v.seed)
		}
		got := CandidatePorts(v.agentID)
		if fmt.Sprint(got) != fmt.Sprint(v.ports) {
			t.Errorf("CandidatePorts(%q) = %v, want %v", v.agentID, got, v.ports)
		}
		for _, p := range got {
			if p < 41000 || p >= 61000 {
				t.Errorf("candidate %d out of documented range", p)
			}
		}
	}
	if Schema != "lingtai.stream-progress/v1" || Path != "/v1/stream-progress" {
		t.Fatalf("schema/path constants drifted: %q %q", Schema, Path)
	}
}

func snapshotJSON(agentID string, active bool, chars int64, generation uint64) string {
	return fmt.Sprintf(`{"schema":%q,"agent_id":%q,"generation":%d,"active":%t,"streamed_chars":%d,"updated_unix_ms":1756200000000,"pid":4242}`,
		Schema, agentID, generation, active, chars)
}

func TestParseSnapshotAcceptsValidAndRejectsSchemaIdentityAndShape(t *testing.T) {
	snap, err := ParseSnapshot([]byte(snapshotJSON("orch", true, 4099, 3)), "orch")
	if err != nil {
		t.Fatalf("valid body rejected: %v", err)
	}
	if snap.AgentID != "orch" || !snap.Active || snap.StreamedChars != 4099 || snap.Generation != 3 || snap.PID != 4242 || snap.UpdatedUnixMS != 1756200000000 {
		t.Fatalf("unexpected snapshot %+v", snap)
	}

	// Surrounding whitespace is the only tolerated addition.
	padded := "  \n" + snapshotJSON("orch", true, 8, 1) + "\n\t "
	if _, err := ParseSnapshot([]byte(padded), "orch"); err != nil {
		t.Fatalf("whitespace-padded body must parse: %v", err)
	}

	valid := snapshotJSON("orch", true, 1, 1)
	withField := func(extra string) string { return strings.TrimSuffix(valid, "}") + "," + extra + "}" }
	cases := map[string]string{
		"schema mismatch":  strings.Replace(valid, Schema, "lingtai.stream-progress/v2", 1),
		"identity":         snapshotJSON("someone-else", true, 1, 1),
		"missing field":    `{"schema":"` + Schema + `","agent_id":"orch","generation":1,"active":true}`,
		"negative chars":   snapshotJSON("orch", true, -1, 1),
		"not json":         `<html>nope</html>`,
		"wrong type":       `{"schema":"` + Schema + `","agent_id":"orch","generation":"1","active":true,"streamed_chars":1,"updated_unix_ms":1,"pid":1}`,
		"missing schema":   `{"agent_id":"orch","generation":1,"active":true,"streamed_chars":1,"updated_unix_ms":1,"pid":1}`,
		"missing agent_id": `{"schema":"` + Schema + `","generation":1,"active":true,"streamed_chars":1,"updated_unix_ms":1,"pid":1}`,
		// Frozen v1 is exactly seven fields: a text field is forbidden and any
		// other extra field is a different schema, not an additive extension.
		"forbidden text field":  withField(`"text":"partial output"`),
		"arbitrary extra field": withField(`"extra":"x"`),
		"duplicate agent_id":    withField(`"agent_id":"orch"`),
		"duplicate schema":      withField(`"schema":"` + Schema + `"`),
		// Exactly one JSON object: nothing may follow it.
		"trailing object":  valid + valid,
		"trailing scalar":  valid + " 1",
		"trailing garbage": valid + "x",
		"trailing comma":   strings.TrimSuffix(valid, "}") + ",}",
		"null body":        `null`,
		"array body":       `[` + valid + `]`,
		"empty body":       ``,
		// Integer domains: generation is unsigned (uint64 decode rejects -1),
		// updated_unix_ms is non-negative, pid is positive.
		"negative generation":      strings.Replace(valid, `"generation":1`, `"generation":-1`, 1),
		"float generation":         strings.Replace(valid, `"generation":1`, `"generation":1.5`, 1),
		"negative updated_unix_ms": strings.Replace(valid, `"updated_unix_ms":1756200000000`, `"updated_unix_ms":-1`, 1),
		"zero pid":                 strings.Replace(valid, `"pid":4242`, `"pid":0`, 1),
		"negative pid":             strings.Replace(valid, `"pid":4242`, `"pid":-4242`, 1),
		"bool as number":           strings.Replace(valid, `"active":true`, `"active":1`, 1),
	}
	for name, body := range cases {
		if _, err := ParseSnapshot([]byte(body), "orch"); err == nil {
			t.Errorf("%s: expected rejection of %q", name, body)
		}
	}
}

func TestParseSnapshotAcceptsZeroUpdatedAndBoundaryValues(t *testing.T) {
	body := `{"schema":"` + Schema + `","agent_id":"orch","generation":0,"active":false,"streamed_chars":0,"updated_unix_ms":0,"pid":1}`
	snap, err := ParseSnapshot([]byte(body), "orch")
	if err != nil {
		t.Fatalf("boundary body rejected: %v", err)
	}
	if snap.Generation != 0 || snap.Active || snap.StreamedChars != 0 || snap.UpdatedUnixMS != 0 || snap.PID != 1 {
		t.Fatalf("unexpected snapshot %+v", snap)
	}
	// Field order is not part of the contract.
	reordered := `{"pid":1,"updated_unix_ms":0,"streamed_chars":0,"active":false,"generation":0,"agent_id":"orch","schema":"` + Schema + `"}`
	if _, err := ParseSnapshot([]byte(reordered), "orch"); err != nil {
		t.Fatalf("reordered body rejected: %v", err)
	}
}

func TestEstimatedTokensIsIntegerQuarter(t *testing.T) {
	for chars, want := range map[int64]int64{0: 0, 3: 0, 4: 1, 7: 1, 4099: 1024, 4100: 1025, -8: 0} {
		if got := (Snapshot{StreamedChars: chars}).EstimatedTokens(); got != want {
			t.Errorf("EstimatedTokens(%d) = %d, want %d", chars, got, want)
		}
	}
}

// fakeServer serves a fixed agent identity and counts hits.
type fakeServer struct {
	srv   *httptest.Server
	hits  atomic.Int64
	chars atomic.Int64
}

func newFakeServer(t *testing.T, agentID string, active bool) *fakeServer {
	t.Helper()
	f := &fakeServer{}
	f.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.hits.Add(1)
		if r.URL.Path != Path {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(snapshotJSON(agentID, active, f.chars.Load(), 1)))
	}))
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeServer) host() string { return strings.TrimPrefix(f.srv.URL, "http://") }

// routeCandidates maps candidate ports to fake hosts; every other candidate
// resolves to a port nothing listens on (fast connection refused).
func routeCandidates(t *testing.T, c *Client, agentID string, hosts map[int]string) {
	t.Helper()
	refused := freeLoopbackPort(t)
	candidates := CandidatePorts(agentID)
	c.hostFor = func(port int) string {
		for i, cand := range candidates {
			if cand == port {
				if h, ok := hosts[i]; ok {
					return h
				}
			}
		}
		return net.JoinHostPort("127.0.0.1", strconv.Itoa(refused))
	}
}

func freeLoopbackPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	l.Close()
	return port
}

func TestFetchProbesInOrderRejectsForeignIdentityCachesAndRescans(t *testing.T) {
	const agentID = "20260826-120000-abcd"
	foreign := newFakeServer(t, "someone-else", true) // squats on candidate 0
	mine := newFakeServer(t, agentID, true)           // candidate 1
	mine.chars.Store(4099)

	c := NewClient(DefaultTimeout)
	routeCandidates(t, c, agentID, map[int]string{0: foreign.host(), 1: mine.host()})

	snap, ok := c.Fetch(agentID)
	if !ok || snap.AgentID != agentID || snap.StreamedChars != 4099 {
		t.Fatalf("Fetch = %+v, %v", snap, ok)
	}
	if foreign.hits.Load() != 1 {
		t.Fatalf("candidate 0 (foreign identity) should have been probed once and rejected; hits=%d", foreign.hits.Load())
	}
	if port, cached := c.CachedPort(agentID); !cached || port != CandidatePorts(agentID)[1] {
		t.Fatalf("accepted port should be cached: %d %v", port, cached)
	}

	// Cached port is used directly: the foreign server is not probed again.
	mine.chars.Store(8000)
	if snap, ok := c.Fetch(agentID); !ok || snap.StreamedChars != 8000 {
		t.Fatalf("second Fetch = %+v, %v", snap, ok)
	}
	if foreign.hits.Load() != 1 || mine.hits.Load() != 2 {
		t.Fatalf("cache not honored: foreign=%d mine=%d", foreign.hits.Load(), mine.hits.Load())
	}

	// The agent restarts on a different candidate: the cached port fails, the
	// client forgets it and rescans, landing on the new publisher.
	mine.srv.Close()
	reborn := newFakeServer(t, agentID, true)
	reborn.chars.Store(12)
	routeCandidates(t, c, agentID, map[int]string{0: foreign.host(), 1: mine.host(), 2: reborn.host()})
	snap, ok = c.Fetch(agentID)
	if !ok || snap.StreamedChars != 12 {
		t.Fatalf("rescan Fetch = %+v, %v", snap, ok)
	}
	if port, _ := c.CachedPort(agentID); port != CandidatePorts(agentID)[2] {
		t.Fatalf("cache should move to the reborn publisher, got %d", port)
	}
	if foreign.hits.Load() != 2 {
		t.Fatalf("rescan should have re-probed candidate 0 exactly once more; hits=%d", foreign.hits.Load())
	}
	if mine.hits.Load() != 2 {
		t.Fatalf("closed cached candidate 1 was refused once (cached attempt) and must not be dialed again in the rescan; hits=%d", mine.hits.Load())
	}

	// Nothing valid anywhere: not ok, and the stale cache entry is dropped.
	reborn.srv.Close()
	if _, ok := c.Fetch(agentID); ok {
		t.Fatal("Fetch must fail when no candidate answers validly")
	}
	if _, cached := c.CachedPort(agentID); cached {
		t.Fatal("failed rescan must leave no cached port")
	}
}

// identitySwitchServer answers with one agent identity until flipped, so a
// cached port can start failing validation while still accepting connections.
type identitySwitchServer struct {
	srv     *httptest.Server
	hits    atomic.Int64
	agentID atomic.Value
}

func newIdentitySwitchServer(t *testing.T, agentID string) *identitySwitchServer {
	t.Helper()
	s := &identitySwitchServer{}
	s.agentID.Store(agentID)
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.hits.Add(1)
		_, _ = w.Write([]byte(snapshotJSON(s.agentID.Load().(string), true, 4, 1)))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *identitySwitchServer) host() string { return strings.TrimPrefix(s.srv.URL, "http://") }

func TestFetchFailedCachedPortIsNotReprobedInTheImmediateRescan(t *testing.T) {
	const agentID = "orch"
	first := newIdentitySwitchServer(t, agentID) // candidate 0, cached after the first Fetch
	second := newFakeServer(t, agentID, true)    // candidate 1
	c := NewClient(DefaultTimeout)
	routeCandidates(t, c, agentID, map[int]string{0: first.host(), 1: second.host()})

	if _, ok := c.Fetch(agentID); !ok {
		t.Fatal("first Fetch should succeed on candidate 0")
	}
	if port, _ := c.CachedPort(agentID); port != CandidatePorts(agentID)[0] {
		t.Fatalf("candidate 0 should be cached, got %d", port)
	}
	if first.hits.Load() != 1 || second.hits.Load() != 0 {
		t.Fatalf("unexpected hits first=%d second=%d", first.hits.Load(), second.hits.Load())
	}

	// The cached port now answers with a foreign identity (another agent took
	// it over). The cached probe fails once; the immediate rescan must not
	// probe that same port a second time and lands on candidate 1.
	first.agentID.Store("someone-else")
	snap, ok := c.Fetch(agentID)
	if !ok || snap.AgentID != agentID {
		t.Fatalf("rescan Fetch = %+v, %v", snap, ok)
	}
	if first.hits.Load() != 2 {
		t.Fatalf("failed cached port must be probed exactly once (cached attempt), not again in the rescan; hits=%d", first.hits.Load())
	}
	if second.hits.Load() != 1 {
		t.Fatalf("rescan should have landed on candidate 1; hits=%d", second.hits.Load())
	}
	if port, _ := c.CachedPort(agentID); port != CandidatePorts(agentID)[1] {
		t.Fatalf("cache should move to candidate 1, got %d", port)
	}

	// A later Fetch (new call, cache now candidate 1) is unaffected: the skip
	// applies only within the rescan that immediately follows a cache miss.
	if _, ok := c.Fetch(agentID); !ok {
		t.Fatal("cached candidate 1 should keep answering")
	}
	if first.hits.Load() != 2 || second.hits.Load() != 2 {
		t.Fatalf("unexpected hits after cached fetch first=%d second=%d", first.hits.Load(), second.hits.Load())
	}
}

func TestFetchNeverFollowsRedirects(t *testing.T) {
	const agentID = "orch"
	// A valid publisher that a redirect would point at — reachable on
	// loopback, so following the redirect would "work" and mask the bug.
	target := newFakeServer(t, agentID, true)
	// A foreign service squatting on candidate 0 that redirects everything;
	// off-loopback and on-loopback redirects are both refused.
	var redirects atomic.Int64
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects.Add(1)
		http.Redirect(w, r, target.srv.URL+Path, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)
	external := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		redirects.Add(1)
		http.Redirect(w, r, "http://203.0.113.7:41000"+Path, http.StatusMovedPermanently)
	}))
	t.Cleanup(external.Close)

	c := NewClient(DefaultTimeout)
	routeCandidates(t, c, agentID, map[int]string{
		0: strings.TrimPrefix(redirector.URL, "http://"),
		1: strings.TrimPrefix(external.URL, "http://"),
	})
	if _, ok := c.Fetch(agentID); ok {
		t.Fatal("redirecting candidates must not yield a snapshot")
	}
	if redirects.Load() != 2 {
		t.Fatalf("both redirecting candidates should have been probed exactly once; got %d", redirects.Load())
	}
	if target.hits.Load() != 0 {
		t.Fatalf("redirect target must never be contacted; hits=%d", target.hits.Load())
	}
	if _, cached := c.CachedPort(agentID); cached {
		t.Fatal("a redirect must not be cached as a valid port")
	}

	// The transport-level check itself: a direct redirect answer is an error,
	// not a followed request.
	if _, err := c.http.Get(redirector.URL + Path); err == nil {
		t.Fatal("client must refuse redirects at the transport level")
	}
	if target.hits.Load() != 0 {
		t.Fatalf("redirect target contacted after direct probe; hits=%d", target.hits.Load())
	}
}

func TestFetchEmptyAgentIDNeverProbes(t *testing.T) {
	c := NewClient(DefaultTimeout)
	c.hostFor = func(int) string { t.Fatal("empty agent id must not probe"); return "" }
	if _, ok := c.Fetch(""); ok {
		t.Fatal("empty agent id must not be ok")
	}
}

func TestFetchRejectsNon200AndNonJSON(t *testing.T) {
	const agentID = "orch"
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(bad.Close)
	html := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html>a foreign local service</html>"))
	}))
	t.Cleanup(html.Close)
	c := NewClient(DefaultTimeout)
	routeCandidates(t, c, agentID, map[int]string{0: strings.TrimPrefix(bad.URL, "http://"), 1: strings.TrimPrefix(html.URL, "http://")})
	if _, ok := c.Fetch(agentID); ok {
		t.Fatal("non-200 / non-JSON candidates must be rejected")
	}
}

// TestFetchRejectsOversizedBodyWithValidPrefix: a foreign service answers with a
// valid seven-field object, whitespace up to the read cap, and forbidden bytes
// past it. Parsing only the first maxBodyBytes would accept the valid prefix;
// the client must reject the whole body and never cache that port.
func TestFetchRejectsOversizedBodyWithValidPrefix(t *testing.T) {
	const agentID = "orch"
	valid := snapshotJSON(agentID, true, 4000, 1)
	// Padding alone reaches the cap; the forbidden object sits past it.
	oversized := valid + strings.Repeat(" ", maxBodyBytes-len(valid)) + `{"text":"leaked output"}`
	if len(oversized) <= maxBodyBytes {
		t.Fatalf("setup: body must exceed the cap; len=%d", len(oversized))
	}
	var oversizedHits atomic.Int64
	squatter := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		oversizedHits.Add(1)
		_, _ = w.Write([]byte(oversized))
	}))
	t.Cleanup(squatter.Close)

	c := NewClient(DefaultTimeout)
	routeCandidates(t, c, agentID, map[int]string{0: strings.TrimPrefix(squatter.URL, "http://")})
	if snap, ok := c.Fetch(agentID); ok {
		t.Fatalf("oversized body must be rejected, got %+v", snap)
	}
	if oversizedHits.Load() != 1 {
		t.Fatalf("squatter should have been probed once; hits=%d", oversizedHits.Load())
	}
	if _, cached := c.CachedPort(agentID); cached {
		t.Fatal("an oversized body must never cache its port")
	}

	// The rescan still lands on a valid sibling, and a body that is exactly at
	// the cap — whitespace padding is the one tolerated addition — is accepted.
	atCap := valid + strings.Repeat(" ", maxBodyBytes-len(valid))
	if len(atCap) != maxBodyBytes {
		t.Fatalf("setup: body must sit exactly at the cap; len=%d", len(atCap))
	}
	exact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(atCap))
	}))
	t.Cleanup(exact.Close)
	routeCandidates(t, c, agentID, map[int]string{
		0: strings.TrimPrefix(squatter.URL, "http://"),
		1: strings.TrimPrefix(exact.URL, "http://"),
	})
	snap, ok := c.Fetch(agentID)
	if !ok || snap.StreamedChars != 4000 {
		t.Fatalf("body at the cap must parse normally; got %+v, %v", snap, ok)
	}
	if port, _ := c.CachedPort(agentID); port != CandidatePorts(agentID)[1] {
		t.Fatalf("cache should hold the valid sibling, got %d", port)
	}
	if oversizedHits.Load() != 2 {
		t.Fatalf("squatter should have been re-probed exactly once in the rescan; hits=%d", oversizedHits.Load())
	}
}

// TestFetchRealLoopbackCandidate binds a real discovery candidate on
// 127.0.0.1 — the exact thing the kernel publisher does — and reattaches with
// a default client (no test routing). Skipped when the machine has every
// candidate busy.
func TestFetchRealLoopbackCandidate(t *testing.T) {
	const agentID = "tui-real-loopback"
	var ln net.Listener
	for _, port := range CandidatePorts(agentID) {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
		if err == nil {
			ln = l
			break
		}
	}
	if ln == nil {
		t.Skip("no free discovery candidate on this machine")
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != Path {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"schema": Schema, "agent_id": agentID, "generation": 2, "active": true,
			"streamed_chars": 400, "updated_unix_ms": 1, "pid": 1,
		})
	})}
	go srv.Serve(ln)
	t.Cleanup(func() { _ = srv.Close() })

	c := NewClient(DefaultTimeout)
	deadline := time.Now().Add(2 * time.Second)
	for {
		snap, ok := c.Fetch(agentID)
		if ok {
			if snap.EstimatedTokens() != 100 {
				t.Fatalf("EstimatedTokens = %d, want 100", snap.EstimatedTokens())
			}
			if port, _ := c.CachedPort(agentID); port != ln.Addr().(*net.TCPAddr).Port {
				t.Fatalf("cached port %d != bound %d", port, ln.Addr().(*net.TCPAddr).Port)
			}
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("never reattached to the real loopback candidate")
		}
		time.Sleep(20 * time.Millisecond)
	}
}
