package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/anthropics/lingtai-portal/internal/fs"
)

func TestDeltaEncode_KeyframesAndDeltas(t *testing.T) {
	base := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", AgentName: "agent-a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{{Sender: "a", Recipient: "b", Count: 1, Direct: 1}},
		Stats:        fs.NetworkStats{Active: 1, TotalMails: 1},
	}

	frames := make([]fs.TapeFrame, 5)
	for i := range frames {
		net := base
		net.MailEdges = []fs.MailEdge{{Sender: "a", Recipient: "b", Count: 1 + i, Direct: 1 + i}}
		net.Stats = fs.NetworkStats{Active: 1, TotalMails: 1 + i}
		frames[i] = fs.TapeFrame{T: int64(1000 + i*3000), Net: net}
	}

	chunk := deltaEncode(frames, 3)

	if chunk.Start != 1000 {
		t.Errorf("Start = %d, want 1000", chunk.Start)
	}
	if chunk.End != 13000 {
		t.Errorf("End = %d, want 13000", chunk.End)
	}
	if chunk.KeyframeInterval != 3 {
		t.Errorf("KeyframeInterval = %d, want 3", chunk.KeyframeInterval)
	}
	if len(chunk.Frames) != 5 {
		t.Fatalf("len(Frames) = %d, want 5", len(chunk.Frames))
	}

	for _, idx := range []int{0, 3} {
		if chunk.Frames[idx].Net == nil {
			t.Errorf("frame[%d] should be keyframe (Net != nil)", idx)
		}
		if chunk.Frames[idx].Delta != nil {
			t.Errorf("frame[%d] keyframe should not have Delta", idx)
		}
	}

	for _, idx := range []int{1, 2, 4} {
		if chunk.Frames[idx].Net != nil {
			t.Errorf("frame[%d] should be delta (Net == nil)", idx)
		}
	}

	if chunk.Frames[1].Delta == nil {
		t.Fatal("frame[1] delta is nil")
	}
	if len(chunk.Frames[1].Delta.Mail) != 1 {
		t.Errorf("frame[1] delta.Mail len = %d, want 1", len(chunk.Frames[1].Delta.Mail))
	}
}

func TestDeltaEncode_EmptyDelta(t *testing.T) {
	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{{Sender: "a", Recipient: "b", Count: 5, Direct: 5}},
		Stats:        fs.NetworkStats{Active: 1, TotalMails: 5},
	}

	frames := []fs.TapeFrame{
		{T: 1000, Net: net},
		{T: 4000, Net: net},
	}

	chunk := deltaEncode(frames, 100)

	if chunk.Frames[1].Delta != nil {
		t.Errorf("expected nil delta for identical frame, got %+v", chunk.Frames[1].Delta)
	}
}

func TestDeltaEncode_NodeChanges(t *testing.T) {
	net0 := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 1},
	}
	net1 := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "SUSPENDED"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Suspended: 1},
	}

	frames := []fs.TapeFrame{
		{T: 1000, Net: net0},
		{T: 4000, Net: net1},
	}

	chunk := deltaEncode(frames, 100)

	if chunk.Frames[1].Delta == nil {
		t.Fatal("expected delta for node state change")
	}
	if len(chunk.Frames[1].Delta.Nodes) != 1 {
		t.Errorf("delta.Nodes len = %d, want 1", len(chunk.Frames[1].Delta.Nodes))
	}
	if chunk.Frames[1].Delta.Nodes[0].State != "SUSPENDED" {
		t.Errorf("delta node state = %q, want SUSPENDED", chunk.Frames[1].Delta.Nodes[0].State)
	}
}

func TestBuildManifest_CachesCompletedHour(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")

	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 1},
	}
	for _, ts := range []int64{3600000, 3601000, 3602000, 3603000, 7200000, 7201000, 7202000, 7203000} {
		AppendTopologyAt(topologyPath, net, ts)
	}

	manifest, err := buildManifest(topologyPath, replayDir)
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}

	if manifest.TapeStart != 3600000 {
		t.Errorf("TapeStart = %d, want 3600000", manifest.TapeStart)
	}
	if manifest.TapeEnd != 7203000 {
		t.Errorf("TapeEnd = %d, want 7203000", manifest.TapeEnd)
	}
	if len(manifest.Chunks) != 2 {
		t.Fatalf("len(Chunks) = %d, want 2", len(manifest.Chunks))
	}

	hour1File := filepath.Join(replayDir, "3600000.json.gz")
	if _, err := os.Stat(hour1File); err != nil {
		t.Errorf("hour-1 cache file missing: %v", err)
	}

	hour2File := filepath.Join(replayDir, "7200000.json.gz")
	if _, err := os.Stat(hour2File); !os.IsNotExist(err) {
		t.Errorf("hour-2 (current) chunk should not be cached, but file exists")
	}
}

func TestWriteReconstructedReplay_UsesFirstFrameForTapeStartAndTruncatesTopology(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")
	progressPath := filepath.Join(dir, ".portal", "reconstruct.progress")

	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 1},
	}
	frames := []fs.TapeFrame{
		{T: 3_900_000, Net: net},
		{T: 3_903_000, Net: net},
		{T: 7_201_000, Net: net},
	}

	manifest, err := writeReconstructedReplay(topologyPath, replayDir, progressPath, frames)
	if err != nil {
		t.Fatalf("writeReconstructedReplay: %v", err)
	}

	if manifest.TapeStart != 3_900_000 {
		t.Errorf("TapeStart = %d, want 3900000", manifest.TapeStart)
	}
	if manifest.Chunks[0].Start != 3_600_000 {
		t.Errorf("Chunks[0].Start = %d, want 3600000", manifest.Chunks[0].Start)
	}
	for _, start := range []int64{3_600_000, 7_200_000} {
		if _, err := os.Stat(filepath.Join(replayDir, strconv.FormatInt(start, 10)+".json.gz")); err != nil {
			t.Errorf("cache %d missing: %v", start, err)
		}
	}

	data, err := os.ReadFile(filepath.Join(replayDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var cached ReplayManifest
	if err := json.Unmarshal(data, &cached); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if cached.TapeStart != manifest.TapeStart {
		t.Errorf("cached TapeStart = %d, want %d", cached.TapeStart, manifest.TapeStart)
	}

	topologyData, err := os.ReadFile(topologyPath)
	if err != nil {
		t.Fatalf("read topology: %v", err)
	}
	lines := bytes.Split(bytes.TrimSpace(topologyData), []byte("\n"))
	if len(lines) != 1 {
		t.Fatalf("topology lines = %d, want 1", len(lines))
	}
	var last fs.TapeFrame
	if err := json.Unmarshal(lines[0], &last); err != nil {
		t.Fatalf("decode topology frame: %v", err)
	}
	if last.T != 7_201_000 {
		t.Errorf("topology frame T = %d, want 7201000", last.T)
	}
	progress, err := os.ReadFile(progressPath)
	if err != nil {
		t.Fatalf("read progress: %v", err)
	}
	if string(progress) != "3/3" {
		t.Errorf("progress = %q, want 3/3", progress)
	}
}

func TestBuildManifest_UsesExistingCache(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")

	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 1},
	}
	for _, ts := range []int64{3600000, 3601000, 7200000, 7201000} {
		AppendTopologyAt(topologyPath, net, ts)
	}

	// First: buildManifest should seed completed-hour caches.
	m1, err := buildManifest(topologyPath, replayDir)
	if err != nil {
		t.Fatal(err)
	}

	hour1File := filepath.Join(replayDir, "3600000.json.gz")
	info1, _ := os.Stat(hour1File)
	modTime1 := info1.ModTime()

	// Second: buildManifest should reuse cached chunks
	m2, err := buildManifest(topologyPath, replayDir)
	if err != nil {
		t.Fatal(err)
	}

	info2, _ := os.Stat(hour1File)
	if !info2.ModTime().Equal(modTime1) {
		t.Error("hour-1 cache was rewritten, should have been reused")
	}

	if m1.TapeStart != m2.TapeStart || m1.TapeEnd != m2.TapeEnd {
		t.Error("manifests differ between compilations")
	}
}

func TestLoadChunk_FromCache(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")

	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{{Sender: "a", Recipient: "b", Count: 3, Direct: 3}},
		Stats:        fs.NetworkStats{Active: 1, TotalMails: 3},
	}
	for _, ts := range []int64{3600000, 3601000, 3602000, 7200000} {
		n := net
		n.MailEdges = []fs.MailEdge{{Sender: "a", Recipient: "b", Count: int(ts / 1000), Direct: int(ts / 1000)}}
		AppendTopologyAt(topologyPath, n, ts)
	}

	if _, err := buildManifest(topologyPath, replayDir); err != nil {
		t.Fatalf("buildManifest: %v", err)
	}

	chunk, err := loadChunk(replayDir, topologyPath, 3600000)
	if err != nil {
		t.Fatalf("loadChunk: %v", err)
	}

	if chunk.Start != 3600000 {
		t.Errorf("chunk.Start = %d, want 3600000", chunk.Start)
	}
	if len(chunk.Frames) != 3 {
		t.Errorf("len(Frames) = %d, want 3", len(chunk.Frames))
	}
}

// TestBuildManifest_SingleHourAfterRebuild reproduces the bug where networks
// with < 1 hour of history lose all frames after a rebuild. The rebuild handler
// caches every hour (including the last) as .json.gz but truncates topology.jsonl
// to just the last frame. buildManifest must trust the cached .json.gz for the
// last hour rather than re-scanning the now-truncated JSONL.
func TestBuildManifest_SingleHourAfterRebuild(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")

	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 1},
	}

	// Write 4 frames within a single hour bucket
	for _, ts := range []int64{3600000, 3603000, 3606000, 3609000} {
		AppendTopologyAt(topologyPath, net, ts)
	}

	frames := make([]fs.TapeFrame, 4)
	for i, ts := range []int64{3600000, 3603000, 3606000, 3609000} {
		frames[i] = fs.TapeFrame{T: ts, Net: net}
	}
	if _, err := writeReconstructedReplay(topologyPath, replayDir, "", frames); err != nil {
		t.Fatalf("writeReconstructedReplay: %v", err)
	}

	// Now buildManifest should still report 4 frames, not 1
	m, err := buildManifest(topologyPath, replayDir)
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}

	if len(m.Chunks) != 1 {
		t.Fatalf("len(Chunks) = %d, want 1", len(m.Chunks))
	}
	if m.Chunks[0].Frames != 4 {
		t.Errorf("Chunks[0].Frames = %d, want 4 (got truncated JSONL data instead of cached chunk)", m.Chunks[0].Frames)
	}
	if m.TapeStart != 3600000 {
		t.Errorf("TapeStart = %d, want 3600000", m.TapeStart)
	}
	if m.TapeEnd != 3609000 {
		t.Errorf("TapeEnd = %d, want 3609000", m.TapeEnd)
	}
}

func TestManifestHandler(t *testing.T) {
	dir := t.TempDir()
	baseDir := filepath.Join(dir, "base")
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")

	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 1},
	}
	AppendTopologyAt(topologyPath, net, 3600000)
	AppendTopologyAt(topologyPath, net, 3601000)

	handler := NewManifestHandler(baseDir)
	req := httptest.NewRequest("GET", "/api/topology/manifest", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	var manifest ReplayManifest
	if err := json.NewDecoder(rr.Body).Decode(&manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.TapeStart != 3600000 {
		t.Errorf("TapeStart = %d, want 3600000", manifest.TapeStart)
	}
	if len(manifest.Chunks) == 0 {
		t.Error("expected at least 1 chunk")
	}
	assertNoCORS(t, rr)
}

func TestChunkHandler(t *testing.T) {
	dir := t.TempDir()
	baseDir := filepath.Join(dir, "base")
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")

	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 1},
	}
	AppendTopologyAt(topologyPath, net, 3600000)
	AppendTopologyAt(topologyPath, net, 3601000)
	AppendTopologyAt(topologyPath, net, 7200000)

	// Compile first so cache exists
	replayDir := filepath.Join(baseDir, ".portal", "replay", "chunks")
	if _, err := buildManifest(topologyPath, replayDir); err != nil {
		t.Fatalf("buildManifest: %v", err)
	}

	handler := NewChunkHandler(baseDir)
	req := httptest.NewRequest("GET", "/api/topology/chunk?start=3600000", nil)
	req.Header.Set("Accept-Encoding", "gzip")
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}

	// Response should be gzipped
	var body []byte
	if rr.Header().Get("Content-Encoding") == "gzip" {
		gr, err := gzip.NewReader(rr.Body)
		if err != nil {
			t.Fatalf("gzip reader: %v", err)
		}
		body, _ = io.ReadAll(gr)
		gr.Close()
	} else {
		body, _ = io.ReadAll(rr.Body)
	}

	var chunk ReplayChunk
	if err := json.Unmarshal(body, &chunk); err != nil {
		t.Fatalf("decode chunk: %v", err)
	}
	if len(chunk.Frames) != 2 {
		t.Errorf("len(Frames) = %d, want 2", len(chunk.Frames))
	}
	assertNoCORS(t, rr)
}

func TestReplayHandlersDoNotSetCORSHeadersOnFallbackAndErrorPaths(t *testing.T) {
	t.Run("manifest empty fallback", func(t *testing.T) {
		handler := NewManifestHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology/manifest", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("rebuild method error", func(t *testing.T) {
		handler := NewRebuildHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology/rebuild", nil))
		if rr.Code != http.StatusMethodNotAllowed {
			t.Fatalf("status = %d, want 405", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("rebuild filesystem error", func(t *testing.T) {
		handler := NewRebuildHandler(filepath.Join(t.TempDir(), "missing"))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/topology/rebuild", nil))
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("rebuild empty tape", func(t *testing.T) {
		handler := NewRebuildHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/topology/rebuild", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("chunk missing start error", func(t *testing.T) {
		handler := NewChunkHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology/chunk", nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("chunk invalid start error", func(t *testing.T) {
		handler := NewChunkHandler(t.TempDir())
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology/chunk?start=nope", nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("status = %d, want 400", rr.Code)
		}
		assertNoCORS(t, rr)
	})

	t.Run("chunk filesystem error", func(t *testing.T) {
		handler := NewChunkHandler(filepath.Join(t.TempDir(), "missing"))
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/topology/chunk?start=0", nil))
		if rr.Code != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", rr.Code)
		}
		assertNoCORS(t, rr)
	})
}
