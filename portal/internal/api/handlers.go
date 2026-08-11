package api

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/anthropics/lingtai-portal/i18n"
	"github.com/anthropics/lingtai-portal/internal/fs"
)

var TopologyMu sync.Mutex

func NewNetworkHandler(baseDir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Preserve the historical full network by default. Only the live fast lane opts into the explicitly incomplete ?mail=0 shape.
		options := fs.NetworkOptions{SkipMailEdges: r.URL.Query().Get("mail") == "0"}
		network, err := fs.BuildNetworkWithOptions(baseDir, options)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
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
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(network)
	}
}

// maxTopologyResponseFrames bounds how many tape frames /api/topology serves.
// The endpoint returns the most recent frames of the tape, not the full
// history — long-lived tapes must not scale the response with all retained
// history. The replay timeline uses the bounded manifest/chunk API instead.
const maxTopologyResponseFrames = 1000

// NewTopologyHandler serves the most recent frames of the topology tape as a
// JSON array. Bounded retrieval contract: at most maxTopologyResponseFrames
// frames, streamed line by line (the whole file is never materialized), read
// under TopologyMu so readers see a consistent snapshot relative to append
// and rebuild writers.
func NewTopologyHandler(baseDir string) http.HandlerFunc {
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")

	return func(w http.ResponseWriter, r *http.Request) {
		TopologyMu.Lock()
		entries, err := readTopologyTail(topologyPath, maxTopologyResponseFrames)
		TopologyMu.Unlock()
		if err != nil {
			// Missing tape is not an error: serve an empty array, matching the
			// historical fallback behavior.
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte("[]"))
			return
		}

		if entries == nil {
			entries = []json.RawMessage{}
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(entries)
	}
}

// readTopologyTail streams topology.jsonl and returns the last n non-empty
// lines as raw JSON messages. Memory is bounded by n and the per-line token
// limit; the file is never fully read into memory.
func readTopologyTail(path string, n int) ([]json.RawMessage, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), maxTapeLineBytes)
	var entries []json.RawMessage
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if len(entries) == n {
			// Ring behavior: drop the oldest frame to keep only the last n.
			copy(entries, entries[1:])
			entries[len(entries)-1] = json.RawMessage(append([]byte(nil), line...))
			continue
		}
		entries = append(entries, json.RawMessage(append([]byte(nil), line...)))
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return entries, nil
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
