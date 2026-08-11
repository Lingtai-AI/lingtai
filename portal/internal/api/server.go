package api

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
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
	mux.Handle("/api/topology/rebuild", NewRebuildHandler(baseDir, rebuildTokenFor(baseDir)))
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

		// Check if tape needs reconstruction
		if needsReconstruction(topologyPath) {
			replayDir := filepath.Join(baseDir, ".portal", "replay", "chunks")
			progressPath := filepath.Join(baseDir, ".portal", "reconstruct.progress")
			os.WriteFile(progressPath, []byte("0/0"), 0o644)

			// Serialize reconstruction with all other topology I/O: the lock
			// is acquired before source scanning or frame allocation begins, so
			// repeated rebuilds cannot stack expensive pre-lock work.
			TopologyMu.Lock()
			frames, err := agentfs.ReconstructTape(baseDir)
			if err == nil && len(frames) > 0 {
				_, _ = writeReconstructedReplay(topologyPath, replayDir, progressPath, frames)
			}
			TopologyMu.Unlock()
			os.Remove(progressPath)
		}

		// Record current state immediately
		if network, err := agentfs.BuildNetwork(baseDir); err == nil {
			AppendTopology(topologyPath, network)
		}

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				network, err := agentfs.BuildNetwork(baseDir)
				if err != nil {
					continue
				}
				AppendTopology(topologyPath, network)
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

// needsReconstruction checks if topology.jsonl is missing, empty,
// or uses the old format (missing direct/cc/bcc on mail edges).
// It inspects only the final line of the tape — reading the whole file at
// startup would scale with all retained history — and runs under TopologyMu
// like every other tape read.
func needsReconstruction(path string) bool {
	TopologyMu.Lock()
	defer TopologyMu.Unlock()

	line, ok := lastLine(path, maxTapeTailBytes)
	if !ok {
		return true
	}
	var frame struct {
		Net struct {
			MailEdges []struct {
				Direct *int `json:"direct"`
			} `json:"mail_edges"`
		} `json:"net"`
	}
	if json.Unmarshal(line, &frame) != nil {
		return true
	}
	if len(frame.Net.MailEdges) == 0 {
		return false
	}
	return frame.Net.MailEdges[0].Direct == nil
}

// maxTapeLineBytes bounds a single topology.jsonl line (matching the replay
// scanners) and maxTapeTailBytes the backward tail window used to locate the
// final line without reading the whole tape.
const (
	maxTapeLineBytes = 10 << 20 // 10 MiB
	maxTapeTailBytes = maxTapeLineBytes + 2<<20
)

// lastLine returns the final non-empty line of path. In the common case it
// reads only the last maxTapeTailBytes of the file; for pathologically long
// final lines it falls back to a forward scan that retains only the last line
// (memory stays bounded either way). ok is false when the file is missing,
// empty, or unreadable.
func lastLine(path string, maxTail int64) ([]byte, bool) {
	f, err := os.Open(path)
	if err != nil {
		return nil, false
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || st.Size() == 0 {
		return nil, false
	}
	size := st.Size()
	readLen := maxTail
	if size < maxTail {
		readLen = size
	}
	buf := make([]byte, readLen)
	if _, err := f.ReadAt(buf, size-readLen); err != nil {
		return nil, false
	}

	// Walk the window backward from the end. Skip a trailing newline, then
	// return the text after the previous newline; skip blank trailing lines.
	end := len(buf)
	if buf[end-1] == '\n' {
		end--
	}
	for i := end - 1; i >= 0; i-- {
		if buf[i] == '\n' {
			line := bytes.TrimSpace(buf[i+1 : end])
			if len(line) > 0 {
				return line, true
			}
			end = i
		}
	}

	// No newline in the window. If the window covered the whole file this is a
	// single-line file; otherwise the final line is longer than the tail
	// window, so fall back to a bounded forward scan (never return a partial
	// line from the middle of an oversized final line).
	if size <= maxTail {
		if line := bytes.TrimSpace(buf[:end]); len(line) > 0 {
			return line, true
		}
		return nil, false
	}
	return scanLastLine(f)
}

// scanLastLine streams path from the start and returns the last non-empty
// line, retaining only one line in memory.
func scanLastLine(f *os.File) ([]byte, bool) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, false
	}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), maxTapeLineBytes)
	var last []byte
	for scanner.Scan() {
		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		last = append(last[:0], line...)
	}
	if scanner.Err() != nil {
		return nil, false
	}
	if len(last) == 0 {
		return nil, false
	}
	return last, true
}

// rebuildTokenFile is the per-project rebuild authorization token file,
// written next to .portal/port so operators and automation on non-loopback
// binds can discover the token and authorize rebuilds.
const rebuildTokenFile = "rebuild-token"

// rebuildTokenFor returns the rebuild authorization token for a project: an
// explicit LINGTAI_PORTAL_REBUILD_TOKEN environment override, otherwise a
// fresh per-process random token persisted to .portal/rebuild-token (0600).
// An empty token disables the token-authenticated rebuild path.
func rebuildTokenFor(baseDir string) string {
	if tok := os.Getenv("LINGTAI_PORTAL_REBUILD_TOKEN"); tok != "" {
		return tok
	}
	token := generateRebuildToken()
	if token == "" {
		return ""
	}
	dir := filepath.Join(baseDir, ".portal")
	if err := os.MkdirAll(dir, 0o755); err == nil {
		os.WriteFile(filepath.Join(dir, rebuildTokenFile), []byte(token+"\n"), 0o600)
	}
	return token
}

// generateRebuildToken returns a 128-bit random hex token, or "" if the
// platform entropy source fails (which disables token-authenticated rebuilds
// rather than shipping a guessable fallback).
func generateRebuildToken() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	return hex.EncodeToString(b[:])
}
