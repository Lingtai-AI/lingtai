package api

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
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

func TestComputeDelta_RemovedAvatarEdge(t *testing.T) {
	prev := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}, {Address: "b", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{{Parent: "a", Child: "b", ChildName: "agent-b"}},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 2},
	}
	curr := prev
	curr.AvatarEdges = []fs.AvatarEdge{}

	delta := computeDelta(&prev, &curr)
	if delta == nil {
		t.Fatal("expected delta for removed avatar edge")
	}
	if len(delta.AvatarEdges) != 0 {
		t.Fatalf("AvatarEdges len = %d, want 0", len(delta.AvatarEdges))
	}
	if len(delta.AvatarEdgesRemoved) != 1 {
		t.Fatalf("AvatarEdgesRemoved len = %d, want 1", len(delta.AvatarEdgesRemoved))
	}
	if delta.AvatarEdgesRemoved[0] != [2]string{"a", "b"} {
		t.Errorf("AvatarEdgesRemoved[0] = %#v, want [a b]", delta.AvatarEdgesRemoved[0])
	}
}

func TestComputeDelta_RemovedContactAndMailEdgesOnly(t *testing.T) {
	prev := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}, {Address: "b", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{{Owner: "a", Target: "b", Name: "agent-b"}},
		MailEdges:    []fs.MailEdge{{Sender: "a", Recipient: "b", Count: 3, Direct: 3}},
		Stats:        fs.NetworkStats{Active: 2},
	}
	curr := prev
	curr.ContactEdges = []fs.ContactEdge{}
	curr.MailEdges = []fs.MailEdge{}

	delta := computeDelta(&prev, &curr)
	if delta == nil {
		t.Fatal("expected non-nil removal-only delta")
	}
	if len(delta.ContactEdges) != 0 {
		t.Fatalf("ContactEdges len = %d, want 0", len(delta.ContactEdges))
	}
	if len(delta.Mail) != 0 {
		t.Fatalf("Mail len = %d, want 0", len(delta.Mail))
	}
	if len(delta.ContactEdgesRemoved) != 1 || delta.ContactEdgesRemoved[0] != [2]string{"a", "b"} {
		t.Fatalf("ContactEdgesRemoved = %#v, want [[a b]]", delta.ContactEdgesRemoved)
	}
	if len(delta.MailRemoved) != 1 || delta.MailRemoved[0] != [2]string{"a", "b"} {
		t.Fatalf("MailRemoved = %#v, want [[a b]]", delta.MailRemoved)
	}
	if len(delta.Nodes) != 0 || len(delta.AvatarEdgesRemoved) != 0 || delta.Stats != nil {
		t.Fatalf("unexpected extra delta fields: %+v", delta)
	}
}

func TestComputeDelta_NodeRemovalWithDependentEdgeRemovals(t *testing.T) {
	prev := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}, {Address: "b", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{{Parent: "a", Child: "b", ChildName: "agent-b"}},
		ContactEdges: []fs.ContactEdge{{Owner: "a", Target: "b", Name: "agent-b"}},
		MailEdges:    []fs.MailEdge{{Sender: "a", Recipient: "b", Count: 1, Direct: 1}},
		Stats:        fs.NetworkStats{Active: 2},
	}
	curr := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 2},
	}

	delta := computeDelta(&prev, &curr)
	if delta == nil {
		t.Fatal("expected delta for node and edge removals")
	}
	foundTombstone := false
	for _, n := range delta.Nodes {
		if n.Address == "b" && n.State == "__REMOVED__" {
			foundTombstone = true
		}
	}
	if !foundTombstone {
		t.Fatalf("delta.Nodes = %#v, want tombstone for b", delta.Nodes)
	}
	if len(delta.AvatarEdgesRemoved) != 1 || delta.AvatarEdgesRemoved[0] != [2]string{"a", "b"} {
		t.Fatalf("AvatarEdgesRemoved = %#v, want [[a b]]", delta.AvatarEdgesRemoved)
	}
	if len(delta.ContactEdgesRemoved) != 1 || delta.ContactEdgesRemoved[0] != [2]string{"a", "b"} {
		t.Fatalf("ContactEdgesRemoved = %#v, want [[a b]]", delta.ContactEdgesRemoved)
	}
	if len(delta.MailRemoved) != 1 || delta.MailRemoved[0] != [2]string{"a", "b"} {
		t.Fatalf("MailRemoved = %#v, want [[a b]]", delta.MailRemoved)
	}
}

func TestDeltaEncode_EdgeRemovalRoundTripContract(t *testing.T) {
	net0 := replayEdgeNetwork(false)
	net1 := replayEdgeNetwork(true)
	net2 := replayEdgeNetwork(false)
	frames := []fs.TapeFrame{
		{T: 3600000, Net: net0},
		{T: 3603000, Net: net1},
		{T: 3606000, Net: net2},
	}

	chunk := deltaEncode(frames, 100)
	if chunk.V != chunkFormatVersion {
		t.Fatalf("chunk.V = %d, want %d", chunk.V, chunkFormatVersion)
	}
	if len(chunk.Frames[1].Delta.AvatarEdges) != 1 {
		t.Fatalf("frame[1] AvatarEdges len = %d, want 1", len(chunk.Frames[1].Delta.AvatarEdges))
	}
	if len(chunk.Frames[2].Delta.AvatarEdgesRemoved) != 1 {
		t.Fatalf("frame[2] AvatarEdgesRemoved len = %d, want 1", len(chunk.Frames[2].Delta.AvatarEdgesRemoved))
	}

	avatarEdges := map[string]fs.AvatarEdge{}
	for _, frame := range chunk.Frames {
		if frame.Net != nil {
			avatarEdges = map[string]fs.AvatarEdge{}
			for _, e := range frame.Net.AvatarEdges {
				avatarEdges[e.Parent+"\x00"+e.Child] = e
			}
			continue
		}
		if frame.Delta == nil {
			continue
		}
		for _, removed := range frame.Delta.AvatarEdgesRemoved {
			delete(avatarEdges, removed[0]+"\x00"+removed[1])
		}
		for _, e := range frame.Delta.AvatarEdges {
			avatarEdges[e.Parent+"\x00"+e.Child] = e
		}
	}
	if len(avatarEdges) != 0 {
		t.Fatalf("final avatar edge count = %d, want 0", len(avatarEdges))
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

func TestLoadChunk_StaleV0WithCompleteJSONLReturnsV2WithoutRewritingCache(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")
	cachePath := filepath.Join(replayDir, "3600000.json.gz")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatal(err)
	}

	stale := staleReplayChunk([]int64{3600000, 3603000, 3606000})
	if err := writeChunkCache(cachePath, stale); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	before := readCacheBytes(t, cachePath)

	for _, frame := range []fs.TapeFrame{
		{T: 3600000, Net: replayEdgeNetwork(false)},
		{T: 3603000, Net: replayEdgeNetwork(true)},
		{T: 3606000, Net: replayEdgeNetwork(false)},
	} {
		AppendTopologyAt(topologyPath, frame.Net, frame.T)
	}

	chunk, err := loadChunk(replayDir, topologyPath, 3600000)
	if err != nil {
		t.Fatalf("loadChunk: %v", err)
	}
	if chunk.V != chunkFormatVersion {
		t.Fatalf("chunk.V = %d, want %d", chunk.V, chunkFormatVersion)
	}
	if len(chunk.Frames) != 3 {
		t.Fatalf("len(Frames) = %d, want 3", len(chunk.Frames))
	}
	if chunk.Frames[2].Delta == nil || len(chunk.Frames[2].Delta.AvatarEdgesRemoved) != 1 {
		t.Fatalf("frame[2] delta = %+v, want avatar edge removal", chunk.Frames[2].Delta)
	}

	// The reconstructed V2 chunk is an in-memory result only: a read path must
	// never persistently mutate or replace the on-disk replay cache.
	if after := readCacheBytes(t, cachePath); !bytes.Equal(before, after) {
		t.Fatalf("cache bytes changed on read: %d bytes before, %d bytes after", len(before), len(after))
	}
	if _, err := readChunkCache(cachePath); !errors.Is(err, errStaleChunkFormat) {
		t.Fatalf("readChunkCache error = %v, want stale format preserved on disk", err)
	}
}

// TestLoadChunk_StaleV0WithEqualCountJSONLGapServesStale is the counterexample
// that frame count plus endpoints cannot detect: JSONL [0, 6, 9] has the same
// frame count as stale [0, 3, 6] and brackets its endpoints, yet is missing
// T=3603000 entirely. Serving the JSONL reconstruction would silently drop that
// timestamp, and persisting it would delete the only copy of that history.
func TestLoadChunk_StaleV0WithEqualCountJSONLGapServesStale(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")
	cachePath := filepath.Join(replayDir, "3600000.json.gz")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatal(err)
	}

	stale := staleReplayChunk([]int64{3600000, 3603000, 3606000})
	if err := writeChunkCache(cachePath, stale); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	before := readCacheBytes(t, cachePath)

	for _, ts := range []int64{3600000, 3606000, 3609000} {
		AppendTopologyAt(topologyPath, replayEdgeNetwork(false), ts)
	}

	chunk, err := loadChunk(replayDir, topologyPath, 3600000)
	if err != nil {
		t.Fatalf("loadChunk: %v", err)
	}
	if chunk.V != 0 {
		t.Fatalf("chunk.V = %d, want stale V0", chunk.V)
	}
	if len(chunk.Frames) != len(stale.Frames) {
		t.Fatalf("len(Frames) = %d, want %d", len(chunk.Frames), len(stale.Frames))
	}
	if !chunkHasFrameAt(chunk, 3603000) {
		t.Fatalf("frames = %v, want stale history retaining T=3603000", frameTimes(chunk))
	}

	if after := readCacheBytes(t, cachePath); !bytes.Equal(before, after) {
		t.Fatalf("cache bytes changed on read: %d bytes before, %d bytes after", len(before), len(after))
	}
	if _, err := readChunkCache(cachePath); !errors.Is(err, errStaleChunkFormat) {
		t.Fatalf("readChunkCache error = %v, want stale format preserved on disk", err)
	}
}

func TestLoadChunk_StaleV0WithNoJSONLFramesServesStale(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")
	cachePath := filepath.Join(replayDir, "3600000.json.gz")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(topologyPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(topologyPath, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	stale := staleReplayChunk([]int64{3600000, 3603000, 3606000})
	if err := writeChunkCache(cachePath, stale); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}

	chunk, err := loadChunk(replayDir, topologyPath, 3600000)
	if err != nil {
		t.Fatalf("loadChunk: %v", err)
	}
	if chunk.V != 0 {
		t.Fatalf("chunk.V = %d, want stale V0", chunk.V)
	}
	if len(chunk.Frames) != len(stale.Frames) {
		t.Fatalf("len(Frames) = %d, want %d", len(chunk.Frames), len(stale.Frames))
	}
	if got := firstFrameForChunk(ChunkInfo{Start: 3600000}, replayDir, topologyPath); got != 3600000 {
		t.Fatalf("firstFrameForChunk = %d, want 3600000 from stale cache", got)
	}
	if _, err := readChunkCache(cachePath); !errors.Is(err, errStaleChunkFormat) {
		t.Fatalf("readChunkCache error = %v, want stale format", err)
	}
}

func TestLoadChunk_StaleV0WithPartialJSONLServesStaleWithoutOverwrite(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")
	cachePath := filepath.Join(replayDir, "3600000.json.gz")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatal(err)
	}

	stale := staleReplayChunk([]int64{3600000, 3603000, 3606000})
	if err := writeChunkCache(cachePath, stale); err != nil {
		t.Fatalf("write stale cache: %v", err)
	}
	AppendTopologyAt(topologyPath, replayEdgeNetwork(false), 3606000)

	chunk, err := loadChunk(replayDir, topologyPath, 3600000)
	if err != nil {
		t.Fatalf("loadChunk: %v", err)
	}
	if chunk.V != 0 {
		t.Fatalf("chunk.V = %d, want stale V0", chunk.V)
	}
	if len(chunk.Frames) != len(stale.Frames) {
		t.Fatalf("len(Frames) = %d, want %d", len(chunk.Frames), len(stale.Frames))
	}
	if _, err := readChunkCache(cachePath); !errors.Is(err, errStaleChunkFormat) {
		t.Fatalf("readChunkCache error = %v, want stale format after partial JSONL", err)
	}
}

// TestJSONLCoversStaleChunk_SequenceAware pins the coverage predicate to
// subsequence semantics: the stale timestamp sequence must appear in the JSONL
// in order and with duplicate occurrences preserved, while extra JSONL frames
// interleaved around it are legitimate. Distinct-timestamp set membership is
// not sufficient — it accepts both duplicate loss and reordered history when
// unrelated extra frames make the counts line up.
func TestJSONLCoversStaleChunk_SequenceAware(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stale []int64
		jsonl []int64
		want  bool
	}{
		{
			// Duplicate hidden by an extra frame: stale has T=3 twice, JSONL
			// has it once, and the extra T=9 makes the counts match.
			name:  "duplicate occurrence lost behind extra frame",
			stale: []int64{0, 3, 3, 6},
			jsonl: []int64{0, 3, 6, 9},
			want:  false,
		},
		{
			// Reordered history: same distinct timestamps, different order.
			name:  "stale order not preserved by jsonl",
			stale: []int64{0, 6, 3},
			jsonl: []int64{0, 3, 6, 9},
			want:  false,
		},
		{
			// Legitimate growth: every stale timestamp appears in order; the
			// interleaved and trailing extras are new frames, not losses.
			name:  "ordered stale covered despite extra jsonl frames",
			stale: []int64{0, 3, 6},
			jsonl: []int64{0, 1, 3, 5, 6, 9},
			want:  true,
		},
		{
			// The originally-reported gap case must stay rejected.
			name:  "equal count endpoint gap still rejected",
			stale: []int64{0, 3, 6},
			jsonl: []int64{0, 6, 9},
			want:  false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := jsonlCoversStaleChunk(jsonlFramesAt(tc.jsonl), staleReplayChunk(tc.stale))
			if got != tc.want {
				t.Fatalf("jsonlCoversStaleChunk(jsonl=%v, stale=%v) = %v, want %v", tc.jsonl, tc.stale, got, tc.want)
			}
		})
	}
}

func TestLoadChunk_CorruptCacheIsNotCompatibilityFallback(t *testing.T) {
	dir := t.TempDir()
	topologyPath := filepath.Join(dir, ".portal", "missing-topology.jsonl")
	replayDir := filepath.Join(dir, ".portal", "replay", "chunks")
	cachePath := filepath.Join(replayDir, "3600000.json.gz")
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cachePath, []byte("not a gzip replay chunk"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := loadChunk(replayDir, topologyPath, 3600000); err == nil {
		t.Fatal("loadChunk succeeded with corrupt cache and missing JSONL, want error")
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

func replayEdgeNetwork(withAvatarEdge bool) fs.Network {
	net := fs.Network{
		Nodes:        []fs.AgentNode{{Address: "a", State: "ACTIVE"}, {Address: "b", State: "ACTIVE"}},
		AvatarEdges:  []fs.AvatarEdge{},
		ContactEdges: []fs.ContactEdge{},
		MailEdges:    []fs.MailEdge{},
		Stats:        fs.NetworkStats{Active: 2},
	}
	if withAvatarEdge {
		net.AvatarEdges = []fs.AvatarEdge{{Parent: "a", Child: "b", ChildName: "agent-b"}}
	}
	return net
}

func readCacheBytes(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read cache bytes: %v", err)
	}
	return data
}

func chunkHasFrameAt(chunk ReplayChunk, ts int64) bool {
	for _, f := range chunk.Frames {
		if f.T == ts {
			return true
		}
	}
	return false
}

func frameTimes(chunk ReplayChunk) []int64 {
	times := make([]int64, 0, len(chunk.Frames))
	for _, f := range chunk.Frames {
		times = append(times, f.T)
	}
	return times
}

func jsonlFramesAt(times []int64) []fs.TapeFrame {
	frames := make([]fs.TapeFrame, 0, len(times))
	for _, ts := range times {
		frames = append(frames, fs.TapeFrame{T: ts, Net: replayEdgeNetwork(false)})
	}
	return frames
}

func staleReplayChunk(times []int64) ReplayChunk {
	net := replayEdgeNetwork(false)
	chunk := ReplayChunk{
		Start:            times[0],
		End:              times[len(times)-1],
		KeyframeInterval: defaultKeyframeInterval,
		Frames:           []ReplayFrame{{T: times[0], Net: &net}},
	}
	for _, t := range times[1:] {
		chunk.Frames = append(chunk.Frames, ReplayFrame{T: t})
	}
	return chunk
}
