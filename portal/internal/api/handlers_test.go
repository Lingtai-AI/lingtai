package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func setupNetworkHandlerMailFixture(t *testing.T) string {
	t.Helper()
	base := t.TempDir()
	for _, agent := range []struct {
		name  string
		admin interface{}
	}{
		{name: "alice", admin: map[string]bool{"karma": true}},
		{name: "bob", admin: map[string]bool{"karma": false}},
		{name: "human", admin: nil},
	} {
		dir := filepath.Join(base, agent.name)
		if err := os.MkdirAll(filepath.Join(dir, "mailbox", "inbox"), 0o755); err != nil {
			t.Fatal(err)
		}
		manifest, err := json.Marshal(map[string]interface{}{
			"agent_name": agent.name,
			"address":    agent.name,
			"state":      "ACTIVE",
			"admin":      agent.admin,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".agent.json"), manifest, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	message, err := json.Marshal(fs.MailMessage{
		ID:         "msg-1",
		From:       "alice",
		To:         "bob",
		ReceivedAt: "2026-07-15T00:00:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	messageDir := filepath.Join(base, "alice", "mailbox", "inbox", "msg-1")
	if err := os.MkdirAll(messageDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(messageDir, "message.json"), message, 0o644); err != nil {
		t.Fatal(err)
	}
	return base
}

func TestNetworkHandlerMailMode(t *testing.T) {
	base := setupNetworkHandlerMailFixture(t)
	for _, tc := range []struct {
		name      string
		path      string
		wantMails int
	}{
		{name: "default remains full", path: "/api/network", wantMails: 1},
		{name: "explicit fast opts out of mail", path: "/api/network?mail=0", wantMails: 0},
		{name: "explicit full includes mail", path: "/api/network?mail=1", wantMails: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rr := httptest.NewRecorder()
			NewNetworkHandler(base).ServeHTTP(rr, httptest.NewRequest(http.MethodGet, tc.path, nil))
			if rr.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200", rr.Code)
			}
			var network fs.Network
			if err := json.NewDecoder(rr.Body).Decode(&network); err != nil {
				t.Fatalf("decode network: %v", err)
			}
			if got := network.Stats.TotalMails; got != tc.wantMails {
				t.Fatalf("total_mails = %d, want %d", got, tc.wantMails)
			}
			if got := len(network.MailEdges); got != tc.wantMails {
				t.Fatalf("mail_edges = %d, want %d", got, tc.wantMails)
			}
		})
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

// TestTopologyHandler_BoundedTail proves the bounded retrieval contract: a
// large tape is served as the most recent maxTopologyResponseFrames frames,
// not the full retained history.
func TestTopologyHandler_BoundedTail(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ".portal", "topology.jsonl")
	const total = maxTopologyResponseFrames + 50
	for i := 0; i < total; i++ {
		AppendTopologyAt(path, fs.Network{Nodes: []fs.AgentNode{}}, int64(1000+i))
	}

	handler := NewTopologyHandler(dir)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var entries []fs.TapeFrame
	if err := json.NewDecoder(rr.Body).Decode(&entries); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(entries) != maxTopologyResponseFrames {
		t.Fatalf("got %d entries, want %d (bounded retrieval contract)", len(entries), maxTopologyResponseFrames)
	}
	wantFirst := int64(1000 + total - maxTopologyResponseFrames)
	if entries[0].T != wantFirst {
		t.Fatalf("first entry t = %d, want %d (newest %d-frame window)", entries[0].T, wantFirst, maxTopologyResponseFrames)
	}
	if last := entries[len(entries)-1].T; last != int64(1000+total-1) {
		t.Fatalf("last entry t = %d, want %d", last, 1000+total-1)
	}
}

// TestTopologyHandler_BlocksUntilTopologyMuAvailable proves the read path takes
// TopologyMu: concurrent read/write behavior is serialized, so a reader cannot
// observe an append/rebuild in progress.
func TestTopologyHandler_BlocksUntilTopologyMuAvailable(t *testing.T) {
	dir := t.TempDir()
	AppendTopologyAt(filepath.Join(dir, ".portal", "topology.jsonl"), fs.Network{}, 1000)
	handler := NewTopologyHandler(dir)

	TopologyMu.Lock()
	done := make(chan struct{})
	go func() {
		defer close(done)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology", nil))
	}()

	select {
	case <-done:
		TopologyMu.Unlock()
		t.Fatal("topology handler completed while TopologyMu was held: read path did not take the lock")
	case <-time.After(150 * time.Millisecond):
		// Expected: the reader is blocked behind the writer.
	}

	TopologyMu.Unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("topology handler did not complete after the lock was released")
	}
}

// TestLastLine_TailInspection proves lastLine returns the final non-empty line
// from the tail of a tape without reading the whole file, and reports ok=false
// for missing/empty files.
func TestLastLine_TailInspection(t *testing.T) {
	t.Run("large file returns final line", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "tape.jsonl")
		var buf bytes.Buffer
		for i := 0; i < 100000; i++ {
			fmt.Fprintf(&buf, "{\"t\":%d}\n", i)
		}
		buf.WriteString("{\"t\":999999,\"last\":true}\n")
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		line, ok := lastLine(path, maxTapeTailBytes)
		if !ok || !strings.Contains(string(line), "\"last\":true") {
			t.Fatalf("lastLine = %q, ok = %v; want the final line", line, ok)
		}
	})

	t.Run("single line without newline", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "single")
		if err := os.WriteFile(path, []byte("{\"t\":1}"), 0o644); err != nil {
			t.Fatal(err)
		}
		line, ok := lastLine(path, maxTapeTailBytes)
		if !ok || string(line) != "{\"t\":1}" {
			t.Fatalf("lastLine = %q, ok = %v", line, ok)
		}
	})

	t.Run("final line longer than tail window falls back to full scan", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "bigline")
		var buf bytes.Buffer
		for i := 0; i < 5; i++ {
			fmt.Fprintf(&buf, "{\"t\":%d}\n", i)
		}
		long := strings.Repeat("x", 1000)
		buf.WriteString(long)
		buf.WriteByte('\n')
		if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		// A tiny tail window forces the backward read to miss the newline, so
		// lastLine must fall back to the forward scan and return the full
		// final line rather than a partial one.
		line, ok := lastLine(path, 100)
		if !ok || string(line) != long {
			t.Fatalf("lastLine = %q (len %d), ok = %v; want the full %d-byte final line", line, len(line), ok, len(long))
		}
	})

	t.Run("trailing blank lines", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "blanks")
		if err := os.WriteFile(path, []byte("{\"t\":1}\n\n\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		line, ok := lastLine(path, maxTapeTailBytes)
		if !ok || string(line) != "{\"t\":1}" {
			t.Fatalf("lastLine = %q, ok = %v", line, ok)
		}
	})

	t.Run("missing file", func(t *testing.T) {
		if _, ok := lastLine(filepath.Join(t.TempDir(), "nope"), maxTapeTailBytes); ok {
			t.Fatal("expected ok=false for missing file")
		}
	})

	t.Run("empty file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty")
		if err := os.WriteFile(path, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if _, ok := lastLine(path, maxTapeTailBytes); ok {
			t.Fatal("expected ok=false for empty file")
		}
	})
}
