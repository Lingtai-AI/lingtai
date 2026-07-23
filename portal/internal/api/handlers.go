package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/anthropics/lingtai-portal/i18n"
	"github.com/anthropics/lingtai-portal/internal/fs"
)

var TopologyMu sync.Mutex

type networkSnapshotBuilder func(string) (fs.Network, error)

type networkSnapshotCache struct {
	mu         sync.Mutex
	build      networkSnapshotBuilder
	inFlight   chan struct{}
	network    fs.Network
	err        error
	hasNetwork bool
}

func newNetworkSnapshotCache(build networkSnapshotBuilder) *networkSnapshotCache {
	if build == nil {
		build = func(baseDir string) (fs.Network, error) {
			return fs.BuildNetworkWithOptions(baseDir, fs.NetworkOptions{SkipMailEdges: true})
		}
	}
	return &networkSnapshotCache{build: build}
}

func (c *networkSnapshotCache) get(baseDir string) (fs.Network, bool, error) {
	for {
		c.mu.Lock()
		if c.inFlight != nil {
			if c.hasNetwork {
				network := c.network
				c.mu.Unlock()
				return network, true, nil
			}
			ch := c.inFlight
			c.mu.Unlock()
			<-ch
			c.mu.Lock()
			network, err, ok := c.network, c.err, c.hasNetwork
			c.mu.Unlock()
			if ok || err != nil {
				return network, true, err
			}
			continue
		}

		ch := make(chan struct{})
		c.inFlight = ch
		build := c.build
		c.mu.Unlock()

		network, err := build(baseDir)

		c.mu.Lock()
		if err == nil {
			c.network = network
			c.hasNetwork = true
		}
		c.err = err
		c.inFlight = nil
		close(ch)
		c.mu.Unlock()

		return network, false, err
	}
}

func networkRequestIncludesMailEdges(r *http.Request) bool {
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("mail"))) {
	case "1", "true", "yes", "include", "included":
		return true
	}
	return false
}

func normalizeNetworkResponse(network *fs.Network) {
	if network.Nodes == nil {
		network.Nodes = []fs.AgentNode{}
	}
	if network.AvatarEdges == nil {
		network.AvatarEdges = []fs.AvatarEdge{}
	}
	if network.ContactEdges == nil {
		network.ContactEdges = []fs.ContactEdge{}
	}
	if network.MailEdges == nil {
		network.MailEdges = []fs.MailEdge{}
	}
	network.Lang = i18n.Lang()
}

func NewNetworkHandler(baseDir string) http.HandlerFunc {
	cache := newNetworkSnapshotCache(nil)
	return func(w http.ResponseWriter, r *http.Request) {
		includeMailEdges := networkRequestIncludesMailEdges(r)
		var (
			network   fs.Network
			fromCache bool
			err       error
		)
		if includeMailEdges {
			network, err = fs.BuildNetwork(baseDir)
		} else {
			network, fromCache, err = cache.get(baseDir)
		}
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		normalizeNetworkResponse(&network)
		if fromCache {
			w.Header().Set("X-LingTai-Network-Cache", "hit")
		}
		if includeMailEdges {
			w.Header().Set("X-LingTai-Network-Mail-Edges", "included")
		} else {
			w.Header().Set("X-LingTai-Network-Mail-Edges", "skipped")
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(network)
	}
}

// NewTopologyHandler serves the full topology tape as a JSON array.
func NewTopologyHandler(baseDir string) http.HandlerFunc {
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")

	return func(w http.ResponseWriter, r *http.Request) {
		data, err := os.ReadFile(topologyPath)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		// Parse JSONL → JSON array
		var entries []json.RawMessage
		for _, line := range splitLines(data) {
			if len(line) > 0 {
				entries = append(entries, json.RawMessage(line))
			}
		}
		if entries == nil {
			entries = []json.RawMessage{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}
}

func splitLines(data []byte) [][]byte {
	var lines [][]byte
	start := 0
	for i, b := range data {
		if b == '\n' {
			if i > start {
				lines = append(lines, data[start:i])
			}
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, data[start:])
	}
	return lines
}

// AppendTopology writes one JSONL line: {"t": <unix_ms>, "net": <network>}
// using the current wall-clock time.
func AppendTopology(path string, network fs.Network) {
	AppendTopologyAt(path, network, time.Now().UnixMilli())
}

// AppendTopologyAt writes one JSONL line with an explicit timestamp.
func AppendTopologyAt(path string, network fs.Network, unixMs int64) {
	TopologyMu.Lock()
	defer TopologyMu.Unlock()

	// Normalize nil slices so JSON encodes [] instead of null
	if network.Nodes == nil {
		network.Nodes = []fs.AgentNode{}
	}
	if network.AvatarEdges == nil {
		network.AvatarEdges = []fs.AvatarEdge{}
	}
	if network.ContactEdges == nil {
		network.ContactEdges = []fs.ContactEdge{}
	}
	if network.MailEdges == nil {
		network.MailEdges = []fs.MailEdge{}
	}

	entry := fs.TapeFrame{
		T:   unixMs,
		Net: network,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		os.MkdirAll(filepath.Dir(path), 0o755)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
	}
	defer f.Close()
	f.Write(line)
}

// NewProgressHandler serves GET /api/topology/progress.
// Returns reconstruction progress as {"current":N,"total":M} or {} if not rebuilding.
func NewProgressHandler(baseDir string) http.HandlerFunc {
	progressPath := filepath.Join(baseDir, ".portal", "reconstruct.progress")

	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		data, err := os.ReadFile(progressPath)
		if err != nil {
			w.Write([]byte("{}"))
			return
		}
		parts := splitProgress(string(data))
		if parts == nil {
			w.Write([]byte("{}"))
			return
		}
		json.NewEncoder(w).Encode(map[string]int{"current": parts[0], "total": parts[1]})
	}
}

func splitProgress(s string) []int {
	for i, c := range s {
		if c == '/' {
			var cur, tot int
			if _, err := fmt.Sscanf(s[:i], "%d", &cur); err != nil {
				return nil
			}
			if _, err := fmt.Sscanf(s[i+1:], "%d", &tot); err != nil {
				return nil
			}
			return []int{cur, tot}
		}
	}
	return nil
}
