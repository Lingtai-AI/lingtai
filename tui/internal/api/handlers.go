package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

func NewNetworkHandler(baseDir string) http.HandlerFunc {
	topologyPath := filepath.Join(baseDir, ".tui-asset", "topology.jsonl")

	return func(w http.ResponseWriter, r *http.Request) {
		network, err := fs.BuildNetwork(baseDir)
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
		w.Header().Set("Access-Control-Allow-Origin", "*")
		json.NewEncoder(w).Encode(network)

		// Append topology snapshot (best-effort, non-blocking)
		go appendTopology(topologyPath, network)
	}
}

// appendTopology writes one JSONL line: {"t": <unix_ms>, "net": <network>}
func appendTopology(path string, network fs.Network) {
	entry := struct {
		T   int64      `json:"t"`
		Net fs.Network `json:"net"`
	}{
		T:   time.Now().UnixMilli(),
		Net: network,
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return
	}
	line = append(line, '\n')

	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		// Ensure parent dir exists (first run before InitProject on existing project)
		os.MkdirAll(filepath.Dir(path), 0o755)
		f, err = os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return
		}
	}
	defer f.Close()
	fmt.Fprint(f, string(line))
}
