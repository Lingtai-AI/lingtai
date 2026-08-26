// Package streamprogress is the read-only loopback client for the kernel's
// documented RAM-resident stream-progress API (`lingtai.stream-progress/v1`,
// lingtai-kernel src/lingtai/kernel/stream_progress/CONTRACT.md).
//
// The kernel agent process serves one JSON snapshot — how many Unicode
// characters of the current LLM response have streamed, never the text — on
// `GET /v1/stream-progress` bound only to 127.0.0.1. The port is not written
// anywhere: producer and every consumer derive the same ordered candidate list
// from the agent's stable `agent_id` (CandidatePorts), so a restarted TUI can
// reattach to a living agent without shared filesystem state. This package
// performs the probe, validates the body, caches the accepted port in memory,
// and rescans after any failure. It never persists a snapshot and is never
// called from a Bubble Tea View().
package streamprogress

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

const (
	// Schema is the exact `schema` string a valid v1 snapshot carries.
	Schema = "lingtai.stream-progress/v1"
	// Path is the one read-only resource the kernel serves.
	Path = "/v1/stream-progress"
	// DefaultTimeout bounds one loopback probe; a refused candidate returns in
	// microseconds, so a full eight-candidate rescan stays well under a second.
	DefaultTimeout = 200 * time.Millisecond

	// Discovery arithmetic — byte-for-byte the kernel's `candidate_ports`.
	portBase       = 41000
	portSpan       = 20000
	portStride     = 7919
	candidateCount = 8

	// maxBodyBytes caps one response body. A body that exceeds it is rejected
	// outright — never truncated and parsed — so a foreign service cannot hide
	// forbidden trailing bytes past the point the client stops reading.
	maxBodyBytes = 64 << 10
)

// ErrInvalidSnapshot is wrapped by every validation failure in ParseSnapshot.
var ErrInvalidSnapshot = errors.New("streamprogress: invalid snapshot")

// ErrRedirect is returned when a candidate answers with an HTTP redirect. The
// client never follows one: a foreign service squatting on a candidate port
// must not be able to send the consumer anywhere — off loopback least of all.
var ErrRedirect = errors.New("streamprogress: redirect refused")

// Seed is `uint16_be(SHA256(Schema + "\0" + UTF8(agentID))[0:2])`.
func Seed(agentID string) uint16 {
	sum := sha256.Sum256([]byte(Schema + "\x00" + agentID))
	return binary.BigEndian.Uint16(sum[:2])
}

// CandidatePorts returns the eight ordered loopback ports a publisher for
// agentID may bind: candidate i is `41000 + ((seed + i*7919) mod 20000)`.
func CandidatePorts(agentID string) []int {
	seed := int(Seed(agentID))
	ports := make([]int, candidateCount)
	for i := range ports {
		ports[i] = portBase + ((seed + i*portStride) % portSpan)
	}
	return ports
}

// Snapshot is one validated v1 reading. It carries exactly the documented
// fields; there is no text field.
type Snapshot struct {
	Schema        string
	AgentID       string
	Generation    uint64
	Active        bool
	StreamedChars int64
	UpdatedUnixMS int64
	PID           int
}

// ParseSnapshot validates one response body for agentID against the frozen v1
// contract: exactly one JSON object carrying exactly the seven documented
// fields — nothing missing, nothing extra (a `text` field or any other
// addition is a different schema, not an additive extension), no duplicate
// keys, and nothing after the object — with `schema` exactly Schema,
// `agent_id` exactly agentID, `generation` an unsigned integer,
// `streamed_chars` and `updated_unix_ms` non-negative, and `pid` positive.
func ParseSnapshot(body []byte, agentID string) (Snapshot, error) {
	var raw struct {
		Schema        *string `json:"schema"`
		AgentID       *string `json:"agent_id"`
		Generation    *uint64 `json:"generation"`
		Active        *bool   `json:"active"`
		StreamedChars *int64  `json:"streamed_chars"`
		UpdatedUnixMS *int64  `json:"updated_unix_ms"`
		PID           *int    `json:"pid"`
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&raw); err != nil {
		return Snapshot{}, fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
	}
	// Exactly one value: a second JSON value (or any non-whitespace byte)
	// after the object is not a v1 snapshot.
	if _, err := dec.Token(); err != io.EOF {
		return Snapshot{}, fmt.Errorf("%w: trailing data after snapshot", ErrInvalidSnapshot)
	}
	if err := rejectDuplicateKeys(body); err != nil {
		return Snapshot{}, err
	}
	if raw.Schema == nil || *raw.Schema != Schema {
		return Snapshot{}, fmt.Errorf("%w: schema mismatch", ErrInvalidSnapshot)
	}
	if raw.AgentID == nil || *raw.AgentID != agentID {
		return Snapshot{}, fmt.Errorf("%w: agent_id mismatch", ErrInvalidSnapshot)
	}
	if raw.Generation == nil || raw.Active == nil || raw.StreamedChars == nil ||
		raw.UpdatedUnixMS == nil || raw.PID == nil {
		return Snapshot{}, fmt.Errorf("%w: missing field", ErrInvalidSnapshot)
	}
	if *raw.StreamedChars < 0 {
		return Snapshot{}, fmt.Errorf("%w: negative streamed_chars", ErrInvalidSnapshot)
	}
	if *raw.UpdatedUnixMS < 0 {
		return Snapshot{}, fmt.Errorf("%w: negative updated_unix_ms", ErrInvalidSnapshot)
	}
	if *raw.PID <= 0 {
		return Snapshot{}, fmt.Errorf("%w: non-positive pid", ErrInvalidSnapshot)
	}
	return Snapshot{
		Schema:        *raw.Schema,
		AgentID:       *raw.AgentID,
		Generation:    *raw.Generation,
		Active:        *raw.Active,
		StreamedChars: *raw.StreamedChars,
		UpdatedUnixMS: *raw.UpdatedUnixMS,
		PID:           *raw.PID,
	}, nil
}

// rejectDuplicateKeys walks the top-level object's keys and fails when one
// repeats: encoding/json silently keeps the last value, which would let a
// body carry two `agent_id`s and still validate.
func rejectDuplicateKeys(body []byte) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return fmt.Errorf("%w: not a JSON object", ErrInvalidSnapshot)
	}
	seen := make(map[string]struct{}, 7)
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
		}
		key, ok := tok.(string)
		if !ok {
			return fmt.Errorf("%w: non-string key", ErrInvalidSnapshot)
		}
		if _, dup := seen[key]; dup {
			return fmt.Errorf("%w: duplicate field %q", ErrInvalidSnapshot, key)
		}
		seen[key] = struct{}{}
		var value json.RawMessage
		if err := dec.Decode(&value); err != nil {
			return fmt.Errorf("%w: %v", ErrInvalidSnapshot, err)
		}
	}
	return nil
}

// Client probes loopback candidates and remembers, in memory only, which port
// last answered validly for each agent id. It is safe for concurrent use.
type Client struct {
	mu      sync.Mutex
	ports   map[string]int // agentID -> last accepted port (RAM only)
	http    *http.Client
	hostFor func(port int) string
}

// NewClient returns a client whose single probe is bounded by timeout
// (DefaultTimeout when non-positive). Connections are not kept alive so a
// polling consumer never pins a server thread between polls, and redirects
// are never followed so the client only ever talks to the loopback address
// it dialed.
func NewClient(timeout time.Duration) *Client {
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	return &Client{
		ports: make(map[string]int),
		http: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				Proxy:             nil, // loopback is never proxied
				DisableKeepAlives: true,
			},
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return ErrRedirect
			},
		},
		hostFor: func(port int) string {
			return net.JoinHostPort("127.0.0.1", strconv.Itoa(port))
		},
	}
}

// CachedPort reports the port last accepted for agentID, if any.
func (c *Client) CachedPort(agentID string) (int, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	port, ok := c.ports[agentID]
	return port, ok
}

// Forget drops the cached port for agentID so the next Fetch rescans.
func (c *Client) Forget(agentID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.ports, agentID)
}

func (c *Client) remember(agentID string, port int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.ports[agentID] = port
}

// Fetch returns the current snapshot for agentID. It tries the cached port
// first; on any failure or identity mismatch it forgets that port and rescans
// the candidates in order — skipping the port that just failed, which was
// probed a moment ago — accepting only a valid v1 body whose agent_id matches
// exactly. ok is false when no candidate answers validly — the caller shows
// nothing and tries again later. An empty agentID is never probed.
func (c *Client) Fetch(agentID string) (snapshot Snapshot, ok bool) {
	if agentID == "" {
		return Snapshot{}, false
	}
	failedPort := -1
	if port, cached := c.CachedPort(agentID); cached {
		if snap, err := c.fetchPort(agentID, port); err == nil {
			return snap, true
		}
		c.Forget(agentID)
		failedPort = port
	}
	for _, port := range CandidatePorts(agentID) {
		if port == failedPort {
			continue
		}
		snap, err := c.fetchPort(agentID, port)
		if err != nil {
			continue
		}
		c.remember(agentID, port)
		return snap, true
	}
	return Snapshot{}, false
}

func (c *Client) fetchPort(agentID string, port int) (Snapshot, error) {
	// A 3xx answer surfaces as an error here: CheckRedirect refuses it, so
	// the redirect target — loopback or not — is never contacted.
	resp, err := c.http.Get("http://" + c.hostFor(port) + Path)
	if err != nil {
		return Snapshot{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Snapshot{}, fmt.Errorf("%w: status %d", ErrInvalidSnapshot, resp.StatusCode)
	}
	// Read one byte past the cap: a body that reaches maxBodyBytes+1 is
	// oversized and rejected whole. Parsing only the first maxBodyBytes would
	// accept a valid object whose forbidden trailing bytes sit past the cut.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return Snapshot{}, err
	}
	if len(body) > maxBodyBytes {
		return Snapshot{}, fmt.Errorf("%w: body exceeds %d bytes", ErrInvalidSnapshot, maxBodyBytes)
	}
	return ParseSnapshot(body, agentID)
}
