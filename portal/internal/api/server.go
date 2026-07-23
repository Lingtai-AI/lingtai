package api

import (
	"context"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	agentfs "github.com/anthropics/lingtai-portal/internal/fs"
)

const defaultHost = "127.0.0.1"

type Server struct {
	httpServer *http.Server
	port       int
	host       string
	baseDir    string
	cancel     context.CancelFunc
	done       chan struct{}
}

func NewServer(baseDir string, staticFS fs.FS) *Server {
	mux := http.NewServeMux()
	mux.Handle("/api/network", NewNetworkHandler(baseDir))
	mux.Handle("/api/topology", NewTopologyHandler(baseDir))
	mux.Handle("/api/topology/manifest", NewManifestHandler(baseDir))
	mux.Handle("/api/topology/chunk", NewChunkHandler(baseDir))
	mux.Handle("/api/topology/rebuild", NewRebuildHandler(baseDir))
	mux.Handle("/api/topology/progress", NewProgressHandler(baseDir))
	if staticFS != nil {
		mux.Handle("/", http.FileServer(http.FS(staticFS)))
	}
	return &Server{
		httpServer: &http.Server{Handler: mux},
		baseDir:    baseDir,
	}
}

func (s *Server) Start(portFile, host string, fixedPort int) error {
	effectiveHost := EffectiveHost(host)
	port := "0"
	if fixedPort > 0 {
		port = strconv.Itoa(fixedPort)
	}
	addr := net.JoinHostPort(effectiveHost, port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	s.host = effectiveHost
	s.port = ln.Addr().(*net.TCPAddr).Port
	if portFile != "" {
		os.WriteFile(portFile, []byte(fmt.Sprintf("%d", s.port)), 0o644)
	}
	go s.httpServer.Serve(ln)
	return nil
}

// StartRecording begins a background goroutine that snapshots the network
// topology every 3 seconds, writing to .portal/topology.jsonl.
func (s *Server) StartRecording(baseDir string) {
	topologyPath := filepath.Join(baseDir, ".portal", "topology.jsonl")
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})

	go func() {
		defer close(s.done)
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		recordCurrent := func() {
			// Keep recording off the full historical-mail path. Full replay
			// reconstruction remains available through POST /api/topology/rebuild.
			if network, err := agentfs.BuildNetworkWithOptions(baseDir, agentfs.NetworkOptions{SkipMailEdges: true}); err == nil {
				AppendTopology(topologyPath, network)
			}
		}

		// Record current state immediately without reconstructing history first.
		recordCurrent()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recordCurrent()
			}
		}
	}()
}

func (s *Server) Port() int {
	return s.port
}

func (s *Server) Host() string {
	return s.host
}

func (s *Server) URL() string {
	host := s.host
	if host == "" || hostIsLoopback(host) || hostIsWildcard(host) {
		host = "localhost"
	}
	return "http://" + net.JoinHostPort(host, strconv.Itoa(s.port))
}

func (s *Server) ExternalAccessWarning() string {
	if !HostRequiresWarning(s.host) {
		return ""
	}
	return fmt.Sprintf("warning: --host %s exposes the unauthenticated portal API beyond loopback; use only on a trusted LAN.", s.host)
}

func EffectiveHost(host string) string {
	host = strings.TrimSpace(host)
	if strings.HasPrefix(host, "[") && strings.HasSuffix(host, "]") {
		host = strings.TrimPrefix(strings.TrimSuffix(host, "]"), "[")
	}
	if host == "" {
		return defaultHost
	}
	return host
}

func HostRequiresWarning(host string) bool {
	host = EffectiveHost(host)
	return !hostIsLoopback(host) || hostIsWildcard(host)
}

func hostIsLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func hostIsWildcard(host string) bool {
	ip := net.ParseIP(host)
	return ip != nil && ip.IsUnspecified()
}

func (s *Server) Stop(ctx context.Context) error {
	if s.cancel != nil {
		s.cancel()
		<-s.done
	}
	return s.httpServer.Shutdown(ctx)
}
