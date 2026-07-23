package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/lingtai-portal/internal/fs"
)

func assertNoCORS(t *testing.T, rr *httptest.ResponseRecorder) {
	t.Helper()
	if got := rr.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want absent", got)
	}
}

func TestHandlersDoNotSetCORSHeaders(t *testing.T) {
	t.Run("network success", func(t *testing.T) {
		handler := NewNetworkHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/network", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("network error", func(t *testing.T) {
		handler := NewNetworkHandler(filepath.Join(t.TempDir(), "missing"))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/network", nil))
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("topology missing tape", func(t *testing.T) {
		handler := NewTopologyHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("topology success", func(t *testing.T) {
		dir := t.TempDir()
		AppendTopologyAt(filepath.Join(dir, ".portal", "topology.jsonl"), fs.Network{}, 1000)
		handler := NewTopologyHandler(dir)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("progress missing file", func(t *testing.T) {
		handler := NewProgressHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology/progress", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("progress success", func(t *testing.T) {
		dir := t.TempDir()
		progressPath := filepath.Join(dir, ".portal", "reconstruct.progress")
		if err := os.MkdirAll(filepath.Dir(progressPath), 0o755); err != nil {
			t.Fatalf("mkdir progress dir: %v", err)
		}
		if err := os.WriteFile(progressPath, []byte("2/5"), 0o644); err != nil {
			t.Fatalf("write progress: %v", err)
		}
		handler := NewProgressHandler(dir)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology/progress", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		assertNoCORS(t, rr)
	})
}

func TestAppendTopologyAt_ExplicitTimestamp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.jsonl")

	net := fs.Network{
		Nodes: []fs.AgentNode{
			{Address: "/test/agent-a", AgentName: "a", State: "ACTIVE"},
		},
	}

	// Write a frame backdated to a specific time
	target := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC).UnixMilli()
	AppendTopologyAt(path, net, target)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read topology: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}

	var entry struct {
		T   int64      `json:"t"`
		Net fs.Network `json:"net"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &entry); err != nil {
		t.Fatalf("parse entry: %v", err)
	}
	if entry.T != target {
		t.Errorf("timestamp = %d, want %d", entry.T, target)
	}
	if len(entry.Net.Nodes) != 1 || entry.Net.Nodes[0].Address != "/test/agent-a" {
		t.Errorf("unexpected network: %+v", entry.Net)
	}
}

func TestAppendTopology_UsesCurrentTime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.jsonl")

	net := fs.Network{
		Nodes: []fs.AgentNode{
			{Address: "/test/agent-b", AgentName: "b"},
		},
	}

	before := time.Now().UnixMilli()
	AppendTopology(path, net)
	after := time.Now().UnixMilli()

	data, _ := os.ReadFile(path)
	var entry struct {
		T int64 `json:"t"`
	}
	json.Unmarshal([]byte(strings.TrimSpace(string(data))), &entry)

	if entry.T < before || entry.T > after {
		t.Errorf("timestamp %d not in range [%d, %d]", entry.T, before, after)
	}
}

func TestAppendTopologyAt_AppendsToExisting(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "topology.jsonl")

	net := fs.Network{Nodes: []fs.AgentNode{}}

	AppendTopologyAt(path, net, 1000)
	AppendTopologyAt(path, net, 2000)
	AppendTopologyAt(path, net, 3000)

	data, _ := os.ReadFile(path)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	var timestamps []int64
	for _, line := range lines {
		var entry struct {
			T int64 `json:"t"`
		}
		json.Unmarshal([]byte(line), &entry)
		timestamps = append(timestamps, entry.T)
	}
	if timestamps[0] != 1000 || timestamps[1] != 2000 || timestamps[2] != 3000 {
		t.Errorf("timestamps = %v, want [1000 2000 3000]", timestamps)
	}
}

func TestAppendTopologyAt_CreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	// Nested path that doesn't exist yet
	path := filepath.Join(dir, "sub", "deep", "topology.jsonl")

	net := fs.Network{Nodes: []fs.AgentNode{}}
	AppendTopologyAt(path, net, 1000)

	if _, err := os.Stat(path); err != nil {
		t.Errorf("file not created: %v", err)
	}
}

func setupNetworkHandlerMailFixture(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	writeNetworkHandlerAgent(t, filepath.Join(base, "alice"), "alice")
	writeNetworkHandlerAgent(t, filepath.Join(base, "bob"), "bob")
	writeNetworkHandlerMail(t, filepath.Join(base, "bob"), "inbox", "msg-1", fs.MailMessage{
		ID:         "msg-1",
		From:       "alice",
		To:         "bob",
		ReceivedAt: time.Now().Format(time.RFC3339),
	})
	return base
}

func writeNetworkHandlerAgent(t *testing.T, agentDir, name string) {
	t.Helper()
	os.MkdirAll(agentDir, 0o755)
	data, _ := json.Marshal(map[string]interface{}{
		"agent_name": name,
		"address":    name,
		"state":      "IDLE",
		"admin":      map[string]interface{}{"karma": false},
	})
	os.WriteFile(filepath.Join(agentDir, ".agent.json"), data, 0o644)
}

func writeNetworkHandlerMail(t *testing.T, agentDir, folder, msgID string, msg fs.MailMessage) {
	t.Helper()
	msgDir := filepath.Join(agentDir, "mailbox", folder, msgID)
	os.MkdirAll(msgDir, 0o755)
	data, _ := json.Marshal(msg)
	os.WriteFile(filepath.Join(msgDir, "message.json"), data, 0o644)
}

func TestNetworkHandlerSkipsMailEdgesByDefault(t *testing.T) {
	base := setupNetworkHandlerMailFixture(t)
	handler := NewNetworkHandler(base)

	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/network", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("default status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-LingTai-Network-Mail-Edges"); got != "skipped" {
		t.Fatalf("default mail-edge header = %q, want skipped", got)
	}
	var fast fs.Network
	if err := json.Unmarshal(rr.Body.Bytes(), &fast); err != nil {
		t.Fatalf("decode fast network: %v", err)
	}
	if len(fast.MailEdges) != 0 || fast.Stats.TotalMails != 0 {
		t.Fatalf("default network should skip mail history, got edges=%d total=%d", len(fast.MailEdges), fast.Stats.TotalMails)
	}

	rr = httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/network?mail=1", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("mail=1 status = %d, want 200: %s", rr.Code, rr.Body.String())
	}
	if got := rr.Header().Get("X-LingTai-Network-Mail-Edges"); got != "included" {
		t.Fatalf("mail=1 header = %q, want included", got)
	}
	var full fs.Network
	if err := json.Unmarshal(rr.Body.Bytes(), &full); err != nil {
		t.Fatalf("decode full network: %v", err)
	}
	if len(full.MailEdges) != 1 || full.Stats.TotalMails != 1 {
		t.Fatalf("mail=1 network should include mail history, got edges=%d total=%d", len(full.MailEdges), full.Stats.TotalMails)
	}
}

func TestNetworkSnapshotCacheCoalescesInFlightBuilds(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	var calls atomic.Int32
	cache := newNetworkSnapshotCache(func(string) (fs.Network, error) {
		if calls.Add(1) == 1 {
			close(started)
		}
		<-release
		return fs.Network{Nodes: []fs.AgentNode{{Address: "agent"}}}, nil
	})

	first := make(chan error, 1)
	go func() {
		_, _, err := cache.get("base")
		first <- err
	}()
	<-started

	second := make(chan struct {
		fromCache bool
		err       error
	}, 1)
	go func() {
		_, fromCache, err := cache.get("base")
		second <- struct {
			fromCache bool
			err       error
		}{fromCache: fromCache, err: err}
	}()

	select {
	case got := <-second:
		t.Fatalf("second request returned before first build completed: %+v", got)
	case <-time.After(20 * time.Millisecond):
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("builder calls while first in flight = %d, want 1", got)
	}
	close(release)
	if err := <-first; err != nil {
		t.Fatalf("first get: %v", err)
	}
	got := <-second
	if got.err != nil {
		t.Fatalf("second get: %v", got.err)
	}
	if !got.fromCache {
		t.Fatalf("second get fromCache = false, want true")
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("builder calls after coalesced requests = %d, want 1", got)
	}
}
