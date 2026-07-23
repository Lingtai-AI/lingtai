package api

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/anthropics/lingtai-portal/internal/fs"
)

const defaultKeyframeInterval = 100
const chunkFormatVersion = 2

var errStaleChunkFormat = errors.New("stale replay chunk format")

// ReplayChunk is the wire format for a range of delta-encoded topology frames.
type ReplayChunk struct {
	V                int           `json:"v,omitempty"`
	Start            int64         `json:"start"`
	End              int64         `json:"end"`
	KeyframeInterval int           `json:"keyframe_interval"`
	Frames           []ReplayFrame `json:"frames"`
}

// ReplayFrame is either a keyframe (Net set) or a delta (Delta set).
type ReplayFrame struct {
	T     int64       `json:"t"`
	Net   *fs.Network `json:"net,omitempty"`
	Delta *FrameDelta `json:"d,omitempty"`
}

// FrameDelta holds only the fields that changed relative to the previous frame.
type FrameDelta struct {
	Nodes               []fs.AgentNode   `json:"nodes,omitempty"`
	AvatarEdges         []fs.AvatarEdge  `json:"avatar_edges,omitempty"`
	AvatarEdgesRemoved  [][2]string      `json:"avatar_edges_removed,omitempty"`
	ContactEdges        []fs.ContactEdge `json:"contact_edges,omitempty"`
	ContactEdgesRemoved [][2]string      `json:"contact_edges_removed,omitempty"`
	Mail                []fs.MailEdge    `json:"mail,omitempty"`
	MailRemoved         [][2]string      `json:"mail_removed,omitempty"`
	Stats               *fs.NetworkStats `json:"stats,omitempty"`
}

// ChunkInfo is a manifest entry describing one chunk.
type ChunkInfo struct {
	Start  int64 `json:"start"`
	End    int64 `json:"end"`
	Frames int   `json:"frames"`
}

// ReplayManifest lists all available chunks for a tape.
type ReplayManifest struct {
	TapeStart int64       `json:"tape_start"`
	TapeEnd   int64       `json:"tape_end"`
	Chunks    []ChunkInfo `json:"chunks"`
}

// deltaEncode converts a slice of TapeFrame into a ReplayChunk with keyframes
// every keyframeInterval frames and compact deltas in between.
func deltaEncode(frames []fs.TapeFrame, keyframeInterval int) ReplayChunk {
	chunk := ReplayChunk{
		V:                chunkFormatVersion,
		KeyframeInterval: keyframeInterval,
	}
	if len(frames) == 0 {
		return chunk
	}

	chunk.Start = frames[0].T
	chunk.End = frames[len(frames)-1].T

	var prev *fs.Network
	for i, f := range frames {
		if i%keyframeInterval == 0 || prev == nil {
			net := f.Net
			chunk.Frames = append(chunk.Frames, ReplayFrame{T: f.T, Net: &net})
			prev = &net
			continue
		}

		delta := computeDelta(prev, &f.Net)
		chunk.Frames = append(chunk.Frames, ReplayFrame{T: f.T, Delta: delta})
		curr := f.Net
		prev = &curr
	}

	return chunk
}

// computeDelta returns a FrameDelta describing what changed between prev and
// curr, or nil if nothing changed.
func computeDelta(prev, curr *fs.Network) *FrameDelta {
	var delta FrameDelta
	hasChange := false

	// --- Node changes ---
	prevNodes := make(map[string]fs.AgentNode, len(prev.Nodes))
	for _, n := range prev.Nodes {
		prevNodes[n.Address] = n
	}

	for _, n := range curr.Nodes {
		if pn, ok := prevNodes[n.Address]; !ok || !nodesEqual(pn, n) {
			delta.Nodes = append(delta.Nodes, n)
			hasChange = true
		}
	}

	// Detect removed nodes (present in prev but absent in curr).
	currNodes := make(map[string]bool, len(curr.Nodes))
	for _, n := range curr.Nodes {
		currNodes[n.Address] = true
	}
	for _, n := range prev.Nodes {
		if !currNodes[n.Address] {
			// Emit a tombstone: address + sentinel state so it can't be
			// confused with a real node that has empty fields.
			delta.Nodes = append(delta.Nodes, fs.AgentNode{
				Address: n.Address,
				State:   "__REMOVED__",
			})
			hasChange = true
		}
	}

	// --- Avatar edge changes ---
	type avatarKey struct{ parent, child string }
	prevAvatars := make(map[avatarKey]fs.AvatarEdge, len(prev.AvatarEdges))
	for _, e := range prev.AvatarEdges {
		prevAvatars[avatarKey{e.Parent, e.Child}] = e
	}
	currAvatars := make(map[avatarKey]bool, len(curr.AvatarEdges))
	for _, e := range curr.AvatarEdges {
		k := avatarKey{e.Parent, e.Child}
		currAvatars[k] = true
		if pe, ok := prevAvatars[k]; !ok || pe != e {
			delta.AvatarEdges = append(delta.AvatarEdges, e)
			hasChange = true
		}
	}
	for _, e := range prev.AvatarEdges {
		k := avatarKey{e.Parent, e.Child}
		if !currAvatars[k] {
			delta.AvatarEdgesRemoved = append(delta.AvatarEdgesRemoved, [2]string{e.Parent, e.Child})
			hasChange = true
		}
	}

	// --- Contact edge changes ---
	type contactKey struct{ owner, target string }
	prevContacts := make(map[contactKey]fs.ContactEdge, len(prev.ContactEdges))
	for _, e := range prev.ContactEdges {
		prevContacts[contactKey{e.Owner, e.Target}] = e
	}
	currContacts := make(map[contactKey]bool, len(curr.ContactEdges))
	for _, e := range curr.ContactEdges {
		k := contactKey{e.Owner, e.Target}
		currContacts[k] = true
		if pe, ok := prevContacts[k]; !ok || pe != e {
			delta.ContactEdges = append(delta.ContactEdges, e)
			hasChange = true
		}
	}
	for _, e := range prev.ContactEdges {
		k := contactKey{e.Owner, e.Target}
		if !currContacts[k] {
			delta.ContactEdgesRemoved = append(delta.ContactEdgesRemoved, [2]string{e.Owner, e.Target})
			hasChange = true
		}
	}

	// --- Mail edge changes ---
	type mailKey struct{ sender, recipient string }
	prevMail := make(map[mailKey]fs.MailEdge, len(prev.MailEdges))
	for _, e := range prev.MailEdges {
		prevMail[mailKey{e.Sender, e.Recipient}] = e
	}
	currMail := make(map[mailKey]bool, len(curr.MailEdges))
	for _, e := range curr.MailEdges {
		k := mailKey{e.Sender, e.Recipient}
		currMail[k] = true
		if pe, ok := prevMail[k]; !ok || pe != e {
			delta.Mail = append(delta.Mail, e)
			hasChange = true
		}
	}
	for _, e := range prev.MailEdges {
		k := mailKey{e.Sender, e.Recipient}
		if !currMail[k] {
			delta.MailRemoved = append(delta.MailRemoved, [2]string{e.Sender, e.Recipient})
			hasChange = true
		}
	}

	// --- Stats ---
	if curr.Stats != prev.Stats {
		stats := curr.Stats
		delta.Stats = &stats
		hasChange = true
	}

	if !hasChange {
		return nil
	}
	return &delta
}

// nodesEqual compares two AgentNode values for semantic equality.
func nodesEqual(a, b fs.AgentNode) bool {
	if a.Address != b.Address || a.AgentName != b.AgentName || a.Nickname != b.Nickname {
		return false
	}
	if a.State != b.State || a.Alive != b.Alive || a.IsHuman != b.IsHuman {
		return false
	}
	if len(a.Capabilities) != len(b.Capabilities) {
		return false
	}
	for i := range a.Capabilities {
		if a.Capabilities[i] != b.Capabilities[i] {
			return false
		}
	}
	if a.Location == nil && b.Location == nil {
		return true
	}
	if a.Location == nil || b.Location == nil {
		return false
	}
	aLoc, _ := json.Marshal(a.Location)
	bLoc, _ := json.Marshal(b.Location)
	return string(aLoc) == string(bLoc)
}

// ---------------------------------------------------------------------------
// Chunk compilation and caching
// ---------------------------------------------------------------------------

const hourMs int64 = 3600 * 1000

func hourBucket(t int64) int64 {
	return (t / hourMs) * hourMs
}

// buildManifest constructs the manifest from a cached manifest.json on disk
// plus a quick tail-scan of the JSONL for any new frames since the last build.
// This is O(new_frames) — typically just the current hour.
func buildManifest(topologyPath, replayDir string) (ReplayManifest, error) {
	os.MkdirAll(replayDir, 0o755)

	// 1. Try reading the cached manifest
	manifestPath := filepath.Join(replayDir, "manifest.json")
	var cached ReplayManifest
	var hasCached bool
	if data, err := os.ReadFile(manifestPath); err == nil {
		if json.Unmarshal(data, &cached) == nil && len(cached.Chunks) > 0 {
			hasCached = true
		}
	}

	// 2. Determine where to start scanning the JSONL
	// We need to re-scan from the start of the last cached chunk
	// (it may have been the "current" hour that has grown since last build).
	// However, if the last cached chunk already has a .json.gz on disk
	// (e.g. written by a rebuild), trust it and only scan after it.
	var scanFrom int64
	var fixedChunks []ChunkInfo // chunks we trust (completed hours)
	if hasCached {
		lastCached := cached.Chunks[len(cached.Chunks)-1]
		lastCachePath := filepath.Join(replayDir, strconv.FormatInt(lastCached.Start, 10)+".json.gz")
		if _, err := os.Stat(lastCachePath); err == nil {
			// Last chunk has a cache file — trust all chunks
			fixedChunks = cached.Chunks
		} else {
			// Last chunk has no cache — drop it for re-scan from JSONL
			fixedChunks = cached.Chunks[:len(cached.Chunks)-1]
		}
		if len(fixedChunks) > 0 {
			scanFrom = fixedChunks[len(fixedChunks)-1].Start + hourMs
		}
	}

	// 3. Scan JSONL tail for new/updated frames
	tailFrames, err := scanJSONLFrom(topologyPath, scanFrom)
	if err != nil && !os.IsNotExist(err) {
		return ReplayManifest{}, err
	}

	// 4. Group tail frames into hours
	chunks := append([]ChunkInfo{}, fixedChunks...)
	if len(tailFrames) > 0 {
		type hourGroup struct {
			start  int64
			frames []fs.TapeFrame
		}
		var tailHours []*hourGroup
		hourIndex := make(map[int64]*hourGroup)

		for _, f := range tailFrames {
			bucket := hourBucket(f.T)
			g, ok := hourIndex[bucket]
			if !ok {
				g = &hourGroup{start: bucket}
				hourIndex[bucket] = g
				tailHours = append(tailHours, g)
			}
			g.frames = append(g.frames, f)
		}

		// Cache all completed tail hours (all except the last)
		for i, g := range tailHours {
			info := ChunkInfo{
				Start:  g.start,
				End:    g.frames[len(g.frames)-1].T,
				Frames: len(g.frames),
			}
			chunks = append(chunks, info)

			if i < len(tailHours)-1 {
				cachePath := filepath.Join(replayDir, strconv.FormatInt(g.start, 10)+".json.gz")
				if _, err := os.Stat(cachePath); err != nil {
					// Only write if not already cached
					chunk := deltaEncode(g.frames, defaultKeyframeInterval)
					writeChunkCache(cachePath, chunk)
				}
			}
		}
	}

	if len(chunks) == 0 {
		return ReplayManifest{}, nil
	}

	sort.Slice(chunks, func(i, j int) bool { return chunks[i].Start < chunks[j].Start })

	// TapeStart is the first real frame timestamp, NOT chunks[0].Start (which
	// is the hour-bucket floor). Prefer the cached value (written correctly by
	// NewRebuildHandler / earlier buildManifest runs); otherwise peek the
	// earliest chunk's first frame from disk or JSONL.
	tapeStart := int64(0)
	if hasCached && cached.TapeStart > 0 {
		tapeStart = cached.TapeStart
	}
	if tapeStart == 0 {
		tapeStart = firstFrameForChunk(chunks[0], replayDir, topologyPath)
	}
	if tapeStart == 0 {
		tapeStart = chunks[0].Start
	}

	manifest := ReplayManifest{
		TapeStart: tapeStart,
		TapeEnd:   chunks[len(chunks)-1].End,
		Chunks:    chunks,
	}

	// 5. Write manifest cache to disk
	if data, err := json.Marshal(manifest); err == nil {
		os.WriteFile(manifestPath, data, 0o644)
	}

	return manifest, nil
}

// firstFrameForChunk returns the timestamp of the earliest frame in a chunk,
// preferring the cached .json.gz and falling back to a JSONL scan. Returns 0
// if no frame can be located.
func firstFrameForChunk(info ChunkInfo, replayDir, topologyPath string) int64 {
	cachePath := filepath.Join(replayDir, strconv.FormatInt(info.Start, 10)+".json.gz")
	if chunk, err := readChunkCache(cachePath); (err == nil || errors.Is(err, errStaleChunkFormat)) && len(chunk.Frames) > 0 {
		return chunk.Frames[0].T
	}
	// Fallback: scan JSONL for the first frame in this chunk's hour window.
	f, err := os.Open(topologyPath)
	if err != nil {
		return 0
	}
	defer f.Close()
	hourEnd := info.Start + hourMs
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var fr fs.TapeFrame
		if err := json.Unmarshal([]byte(line), &fr); err != nil {
			continue
		}
		if fr.T < info.Start {
			continue
		}
		if fr.T >= hourEnd {
			break
		}
		return fr.T
	}
	return 0
}

// scanJSONLFrom reads topology.jsonl and returns frames with T >= fromMs.
// Scans from the beginning but skips frames before fromMs. For large files
// where most data is cached, only the tail is collected.
func scanJSONLFrom(topologyPath string, fromMs int64) ([]fs.TapeFrame, error) {
	f, err := os.Open(topologyPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var frames []fs.TapeFrame
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		// Quick timestamp extraction without full unmarshal for skipping
		var frame fs.TapeFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if frame.T < fromMs {
			continue
		}
		frames = append(frames, frame)
	}
	return frames, nil
}

// writeReconstructedReplay replaces the replay cache from already-reconstructed
// frames and truncates topology.jsonl to the last frame for live appends.
func writeReconstructedReplay(topologyPath, replayDir, progressPath string, frames []fs.TapeFrame) (ReplayManifest, error) {
	if len(frames) == 0 {
		return ReplayManifest{}, nil
	}
	if err := os.RemoveAll(replayDir); err != nil {
		return ReplayManifest{}, err
	}
	if err := os.MkdirAll(replayDir, 0o755); err != nil {
		return ReplayManifest{}, err
	}
	if progressPath != "" {
		if err := os.MkdirAll(filepath.Dir(progressPath), 0o755); err != nil {
			return ReplayManifest{}, err
		}
		if err := os.WriteFile(progressPath, []byte(fmt.Sprintf("0/%d", len(frames))), 0o644); err != nil {
			return ReplayManifest{}, err
		}
	}

	type hourGroup struct {
		start  int64
		frames []fs.TapeFrame
	}
	var hours []*hourGroup
	hourIndex := make(map[int64]*hourGroup)

	for i, frame := range frames {
		bucket := hourBucket(frame.T)
		g, ok := hourIndex[bucket]
		if !ok {
			g = &hourGroup{start: bucket}
			hourIndex[bucket] = g
			hours = append(hours, g)
		}
		g.frames = append(g.frames, frame)
		if progressPath != "" && (i%1000 == 0 || i == len(frames)-1) {
			if err := os.WriteFile(progressPath, []byte(fmt.Sprintf("%d/%d", i+1, len(frames))), 0o644); err != nil {
				return ReplayManifest{}, err
			}
		}
	}

	manifest := ReplayManifest{
		TapeStart: frames[0].T,
		TapeEnd:   hours[len(hours)-1].frames[len(hours[len(hours)-1].frames)-1].T,
	}

	for _, g := range hours {
		info := ChunkInfo{
			Start:  g.start,
			End:    g.frames[len(g.frames)-1].T,
			Frames: len(g.frames),
		}
		manifest.Chunks = append(manifest.Chunks, info)

		cachePath := filepath.Join(replayDir, strconv.FormatInt(g.start, 10)+".json.gz")
		chunk := deltaEncode(g.frames, defaultKeyframeInterval)
		if err := writeChunkCache(cachePath, chunk); err != nil {
			return ReplayManifest{}, fmt.Errorf("cache chunk %d: %w", g.start, err)
		}
	}

	if err := os.MkdirAll(filepath.Dir(topologyPath), 0o755); err != nil {
		return ReplayManifest{}, err
	}
	lastFrame := frames[len(frames)-1]
	line, err := json.Marshal(lastFrame)
	if err != nil {
		return ReplayManifest{}, err
	}
	if err := os.WriteFile(topologyPath, append(line, '\n'), 0o644); err != nil {
		return ReplayManifest{}, err
	}
	data, err := json.Marshal(manifest)
	if err != nil {
		return ReplayManifest{}, err
	}
	if err := os.WriteFile(filepath.Join(replayDir, "manifest.json"), data, 0o644); err != nil {
		return ReplayManifest{}, err
	}

	return manifest, nil
}

func writeChunkCache(path string, chunk ReplayChunk) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	gw := gzip.NewWriter(f)
	if err := json.NewEncoder(gw).Encode(chunk); err != nil {
		gw.Close()
		return err
	}
	return gw.Close()
}

func readChunkCache(path string) (ReplayChunk, error) {
	f, err := os.Open(path)
	if err != nil {
		return ReplayChunk{}, err
	}
	defer f.Close()

	gr, err := gzip.NewReader(f)
	if err != nil {
		return ReplayChunk{}, err
	}
	defer gr.Close()

	var chunk ReplayChunk
	if err := json.NewDecoder(gr).Decode(&chunk); err != nil {
		return ReplayChunk{}, err
	}
	if chunk.V == 0 {
		return chunk, errStaleChunkFormat
	}
	if chunk.V != chunkFormatVersion {
		return ReplayChunk{}, fmt.Errorf("unsupported replay chunk format version %d", chunk.V)
	}
	return chunk, nil
}

func loadChunk(replayDir, topologyPath string, hourStart int64) (ReplayChunk, error) {
	cachePath := filepath.Join(replayDir, strconv.FormatInt(hourStart, 10)+".json.gz")
	cachedChunk, cacheErr := readChunkCache(cachePath)
	if cacheErr == nil {
		return cachedChunk, nil
	}
	hasStaleCache := errors.Is(cacheErr, errStaleChunkFormat)

	frames, err := scanJSONLChunk(topologyPath, hourStart)
	if err != nil {
		if hasStaleCache {
			return cachedChunk, nil
		}
		return ReplayChunk{}, err
	}

	if len(frames) == 0 {
		if hasStaleCache {
			return cachedChunk, nil
		}
		return ReplayChunk{V: chunkFormatVersion, Start: hourStart, End: hourStart}, nil
	}

	if hasStaleCache && !jsonlCoversStaleChunk(frames, cachedChunk) {
		return cachedChunk, nil
	}

	chunk := deltaEncode(frames, defaultKeyframeInterval)
	if hasStaleCache {
		if err := writeChunkCache(cachePath, chunk); err != nil {
			return ReplayChunk{}, err
		}
	}
	return chunk, nil
}

func scanJSONLChunk(topologyPath string, hourStart int64) ([]fs.TapeFrame, error) {
	f, err := os.Open(topologyPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	hourEnd := hourStart + hourMs
	var frames []fs.TapeFrame

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 10*1024*1024)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var frame fs.TapeFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			continue
		}
		if frame.T < hourStart {
			continue
		}
		if frame.T >= hourEnd {
			break
		}
		frames = append(frames, frame)
	}
	return frames, scanner.Err()
}

func jsonlCoversStaleChunk(frames []fs.TapeFrame, stale ReplayChunk) bool {
	if len(stale.Frames) == 0 {
		return len(frames) > 0
	}
	if len(frames) < len(stale.Frames) {
		return false
	}
	if frames[0].T > stale.Frames[0].T {
		return false
	}
	return frames[len(frames)-1].T >= stale.Frames[len(stale.Frames)-1].T
}

// NewManifestHandler serves GET /api/topology/manifest.
// Builds the manifest from cached chunks + a tail scan — fast even for large tapes.
func NewManifestHandler(baseDir string) http.HandlerFunc {
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(baseDir, ".portal", "replay", "chunks")

	return func(w http.ResponseWriter, r *http.Request) {
		TopologyMu.Lock()
		manifest, err := buildManifest(topologyPath, replayDir)
		TopologyMu.Unlock()

		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ReplayManifest{Chunks: []ChunkInfo{}})
			return
		}
		if manifest.Chunks == nil {
			manifest.Chunks = []ChunkInfo{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(manifest)
	}
}

// NewRebuildHandler serves POST /api/topology/rebuild.
// Reconstructs the full tape from source data (events.jsonl + mailbox),
// then writes compressed hourly chunks.
func NewRebuildHandler(baseDir string) http.HandlerFunc {
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(baseDir, ".portal", "replay", "chunks")
	progressPath := filepath.Join(baseDir, ".portal", "reconstruct.progress")

	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		// Reconstruct from source data
		frames, err := fs.ReconstructTape(baseDir)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		if len(frames) == 0 {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(ReplayManifest{Chunks: []ChunkInfo{}})
			return
		}

		TopologyMu.Lock()
		manifest, err := writeReconstructedReplay(topologyPath, replayDir, progressPath, frames)
		TopologyMu.Unlock()
		os.Remove(progressPath)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		if manifest.Chunks == nil {
			manifest.Chunks = []ChunkInfo{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(manifest)
	}
}

// NewChunkHandler serves GET /api/topology/chunk?start=<hourMs>.
func NewChunkHandler(baseDir string) http.HandlerFunc {
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")
	replayDir := filepath.Join(baseDir, ".portal", "replay", "chunks")

	return func(w http.ResponseWriter, r *http.Request) {
		startStr := r.URL.Query().Get("start")
		if startStr == "" {
			http.Error(w, "missing start parameter", http.StatusBadRequest)
			return
		}
		hourStart, err := strconv.ParseInt(startStr, 10, 64)
		if err != nil {
			http.Error(w, "invalid start parameter", http.StatusBadRequest)
			return
		}

		// Lock while reading JSONL (loadChunk may fall back to scanning the file)
		TopologyMu.Lock()
		chunk, err := loadChunk(replayDir, topologyPath, hourStart)
		TopologyMu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")

		if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			w.Header().Set("Content-Encoding", "gzip")
			gw := gzip.NewWriter(w)
			json.NewEncoder(gw).Encode(chunk)
			gw.Close()
			return
		}

		json.NewEncoder(w).Encode(chunk)
	}
}
