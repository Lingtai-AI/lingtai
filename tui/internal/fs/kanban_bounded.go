package fs

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// KanbanReadLimit is the single record/line/directory-entry bound used by the
// /kanban snapshot. Unrelated readers keep their existing whole-source
// semantics.
const KanbanReadLimit = 1000

const (
	// KanbanJSONLByteLimit prevents one giant line from defeating the line
	// bound. A byte-limited prefix fragment is discarded rather than decoded.
	KanbanJSONLByteLimit int64 = 8 * 1024 * 1024
	// KanbanFixedFileByteLimit bounds fixed-state manifests, status, contacts,
	// and heartbeats read by the Kanban-specific network/snapshot path.
	KanbanFixedFileByteLimit int64 = 2 * 1024 * 1024
	// daemon.json has a much smaller published shape than an agent manifest.
	kanbanDaemonCardByteLimit int64 = 256 * 1024
	kanbanTailBlockSize             = 64 * 1024
)

var errKanbanFileTooLarge = errors.New("kanban fixed-state file exceeds byte limit")

// BoundedReadStats records what a Kanban read actually consumed. It is kept on
// the asynchronous snapshot for guarded timing/IO probes; rendering never
// consults it.
type BoundedReadStats struct {
	Path                   string
	FileBytes              int64
	BytesRead              int64
	CompleteLines          int
	ValidRecords           int
	MalformedRecords       int
	DirectoryEntriesRead   int
	LineLimit              int
	ByteLimit              int64
	Complete               bool
	Truncated              bool
	ByteLimited            bool
	TrailingPartialIgnored bool
	Status                 string
}

const (
	KanbanSourceAvailable   = "available"
	KanbanSourceEmpty       = "empty"
	KanbanSourceRecent      = "recent"
	KanbanSourcePartial     = "partial"
	KanbanSourceMalformed   = "malformed"
	KanbanSourceUnavailable = "unavailable"
)

// KanbanReadState derives presentation truth directly from the existing read
// stats. An empty Status is reserved for zero-value/test snapshots that did
// not perform a source read.
func KanbanReadState(stat BoundedReadStats) string {
	if stat.Status == "" {
		return ""
	}
	switch stat.Status {
	case "unavailable", "disabled", "too-large", "unsafe":
		return KanbanSourceUnavailable
	case "malformed":
		return KanbanSourceMalformed
	case "partial":
		return KanbanSourcePartial
	case "windowed":
		return KanbanSourceRecent
	}
	if stat.ValidRecords == 0 && stat.MalformedRecords > 0 {
		return KanbanSourceMalformed
	}
	if stat.ByteLimited || stat.TrailingPartialIgnored || stat.MalformedRecords > 0 {
		return KanbanSourcePartial
	}
	if stat.Truncated {
		return KanbanSourceRecent
	}
	if stat.ValidRecords == 0 {
		return KanbanSourceEmpty
	}
	return KanbanSourceAvailable
}

func finishKanbanJSONLRead(stat *BoundedReadStats) {
	switch KanbanReadState(*stat) {
	case KanbanSourceEmpty:
		stat.Status = "empty"
	case KanbanSourceRecent:
		stat.Status = "windowed"
	case KanbanSourcePartial:
		stat.Status = "partial"
	case KanbanSourceMalformed:
		stat.Status = "malformed"
	}
}

func appendKanbanReadStats(dst *[]BoundedReadStats, stat BoundedReadStats) {
	if dst != nil {
		*dst = append(*dst, stat)
	}
}

func readBoundedFile(path string, byteLimit int64) ([]byte, BoundedReadStats, error) {
	stat := BoundedReadStats{Path: path, ByteLimit: byteLimit}
	f, err := os.Open(path)
	if err != nil {
		stat.Status = "unavailable"
		return nil, stat, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		stat.Status = "unavailable"
		return nil, stat, err
	}
	stat.FileBytes = info.Size()
	if byteLimit <= 0 || info.Size() > byteLimit {
		stat.ByteLimited = true
		stat.Status = "too-large"
		return nil, stat, errKanbanFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(f, byteLimit))
	stat.BytesRead = int64(len(data))
	if err != nil {
		stat.Status = "unavailable"
		return nil, stat, err
	}
	// Detect a concurrent append beyond the ceiling without reading another
	// byte. Never parse a potentially truncated fixed-state document.
	if latest, latestErr := f.Stat(); latestErr == nil && latest.Size() > byteLimit {
		stat.FileBytes = latest.Size()
		stat.ByteLimited = true
		stat.Status = "too-large"
		return nil, stat, errKanbanFileTooLarge
	}
	stat.Complete = true
	stat.Status = "ok"
	return data, stat, nil
}

// readBoundedJSONLTail is the one Kanban JSONL primitive. It seeks backward
// from EOF in fixed-size blocks, reads no more than maxBytes, returns at most
// maxLines complete trailing physical lines, drops a byte-boundary prefix and
// an unterminated EOF suffix, and never streams from byte zero unless the whole
// file itself fits inside the bound.
func readBoundedJSONLTail(path string, maxLines int, maxBytes int64) ([][]byte, BoundedReadStats) {
	stat := BoundedReadStats{Path: path, LineLimit: maxLines, ByteLimit: maxBytes}
	if maxLines <= 0 || maxBytes <= 0 {
		stat.Status = "disabled"
		return nil, stat
	}
	f, err := os.Open(path)
	if err != nil {
		stat.Status = "unavailable"
		return nil, stat
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		stat.Status = "unavailable"
		return nil, stat
	}
	stat.FileBytes = info.Size()
	if info.Size() == 0 {
		stat.Complete = true
		stat.Status = "empty"
		return nil, stat
	}

	pos := info.Size()
	remaining := maxBytes
	newlineCount := 0
	chunks := make([][]byte, 0, 4)
	for pos > 0 && remaining > 0 && newlineCount < maxLines+1 {
		readSize := int64(kanbanTailBlockSize)
		if readSize > pos {
			readSize = pos
		}
		if readSize > remaining {
			readSize = remaining
		}
		pos -= readSize
		remaining -= readSize
		buf := make([]byte, int(readSize))
		n, readErr := f.ReadAt(buf, pos)
		if readErr != nil && readErr != io.EOF {
			stat.Status = "unavailable"
			return nil, stat
		}
		buf = buf[:n]
		stat.BytesRead += int64(n)
		newlineCount += bytes.Count(buf, []byte{'\n'})
		chunks = append(chunks, buf)
	}
	stat.ByteLimited = pos > 0 && remaining == 0
	stat.Truncated = pos > 0

	suffix := make([]byte, 0, stat.BytesRead)
	for i := len(chunks) - 1; i >= 0; i-- {
		suffix = append(suffix, chunks[i]...)
	}
	if len(suffix) == 0 {
		stat.Complete = pos == 0
		stat.Status = "empty"
		return nil, stat
	}

	// A non-newline-terminated EOF row may still be in flight. Ignore it.
	if suffix[len(suffix)-1] != '\n' {
		stat.TrailingPartialIgnored = true
		lastNL := bytes.LastIndexByte(suffix, '\n')
		if lastNL < 0 {
			stat.Status = "partial"
			return nil, stat
		}
		suffix = suffix[:lastNL+1]
	}
	// If the read stopped before byte zero, its first bytes belong to an
	// unknown partial line. Drop through the next newline.
	if pos > 0 {
		firstNL := bytes.IndexByte(suffix, '\n')
		if firstNL < 0 || firstNL+1 >= len(suffix) {
			stat.Status = "partial"
			return nil, stat
		}
		suffix = suffix[firstNL+1:]
	}
	if len(suffix) == 0 {
		stat.Status = "empty"
		return nil, stat
	}

	// Remove exactly the terminating delimiter; additional blank lines remain
	// physical lines and therefore count toward the 1000-line budget.
	suffix = suffix[:len(suffix)-1]
	lines := bytes.Split(suffix, []byte{'\n'})
	if len(lines) > maxLines {
		stat.Truncated = true
		lines = lines[len(lines)-maxLines:]
	}
	stat.CompleteLines = len(lines)
	stat.Complete = !stat.Truncated && !stat.TrailingPartialIgnored
	stat.Status = "ok"
	return lines, stat
}

// KanbanTokenWindow is derived solely from the final KanbanReadLimit physical
// lines of one token ledger.
type KanbanTokenWindow struct {
	Totals            TokenTotals
	ByProvider        map[string]TokenTotals
	Entries           []LedgerEntry // file order, oldest to newest within the tail
	Recent            []LedgerEntry // newest first, display-limited
	OldestTimestamp   time.Time
	TimestampCoverage bool
	Read              BoundedReadStats
}

func ReadKanbanTokenWindow(path string, recentN int) KanbanTokenWindow {
	return readKanbanTokenWindow(path, recentN, true)
}

func readKanbanTokenWindow(path string, recentN int, skipDaemonRows bool) KanbanTokenWindow {
	lines, read := readBoundedJSONLTail(path, KanbanReadLimit, KanbanJSONLByteLimit)
	window := KanbanTokenWindow{ByProvider: map[string]TokenTotals{}, Read: read}
	for _, raw := range lines {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var entry LedgerEntry
		if line[0] != '{' || json.Unmarshal(line, &entry) != nil {
			window.Read.MalformedRecords++
			continue
		}
		window.Read.ValidRecords++
		if skipDaemonRows && isDaemonLedgerEntry(entry) {
			continue
		}
		window.Entries = append(window.Entries, entry)
		window.Totals.Input += entry.Input
		window.Totals.Output += entry.Output
		window.Totals.Thinking += entry.Thinking
		window.Totals.Cached += entry.Cached
		window.Totals.APICalls++
		provider := DeriveLedgerProvider(entry.Endpoint, entry.Model)
		totals := window.ByProvider[provider]
		totals.Input += entry.Input
		totals.Output += entry.Output
		totals.Thinking += entry.Thinking
		totals.Cached += entry.Cached
		totals.APICalls++
		window.ByProvider[provider] = totals
	}
	window.TimestampCoverage = len(window.Entries) > 0
	for _, entry := range window.Entries {
		ts, err := time.Parse(time.RFC3339, entry.TS)
		if err != nil {
			window.TimestampCoverage = false
			continue
		}
		if window.OldestTimestamp.IsZero() || ts.Before(window.OldestTimestamp) {
			window.OldestTimestamp = ts
		}
	}
	finishKanbanJSONLRead(&window.Read)
	start := 0
	if recentN > 0 && len(window.Entries) > recentN {
		start = len(window.Entries) - recentN
	}
	window.Recent = append(window.Recent, window.Entries[start:]...)
	for i, j := 0, len(window.Recent)-1; i < j; i, j = i+1, j-1 {
		window.Recent[i], window.Recent[j] = window.Recent[j], window.Recent[i]
	}
	return window
}

// KanbanEventWindow is derived from the same bounded tail shape as token and
// context data. Session counts are therefore explicitly "within recent
// window" statistics, never whole-history claims.
type KanbanEventWindow struct {
	CurrentSince      time.Time
	LastSince         time.Time
	LastBefore        time.Time
	CoverageSince     time.Time
	TimestampCoverage bool
	ToolCalls         MoltSessionToolCalls
	Rebuilds          []time.Time
	Refreshes         []time.Time
	Read              BoundedReadStats
}

func ReadKanbanEventWindow(agentDir string, recentN int) KanbanEventWindow {
	path := filepath.Join(agentDir, "logs", "events.jsonl")
	lines, read := readBoundedJSONLTail(path, KanbanReadLimit, KanbanJSONLByteLimit)
	window := KanbanEventWindow{Read: read}
	type eventRow struct {
		Type string  `json:"type"`
		TS   float64 `json:"ts"`
	}
	events := make([]eventRow, 0, len(lines))
	timestampsMonotonic := true
	var previousTimestamp time.Time
	for _, raw := range lines {
		var event eventRow
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		if json.Unmarshal(line, &event) != nil {
			window.Read.MalformedRecords++
			continue
		}
		window.Read.ValidRecords++
		events = append(events, event)
		if event.TS > 0 {
			t := unixFloatTime(event.TS)
			if window.CoverageSince.IsZero() {
				window.CoverageSince = t
			}
			if !previousTimestamp.IsZero() && t.Before(previousTimestamp) {
				timestampsMonotonic = false
			}
			previousTimestamp = t
		}
		if event.Type == "psyche_molt" && event.TS > 0 {
			window.LastSince = window.CurrentSince
			window.LastBefore = unixFloatTime(event.TS)
			window.CurrentSince = window.LastBefore
		}
	}
	for _, event := range events {
		if event.TS <= 0 {
			continue
		}
		t := unixFloatTime(event.TS)
		switch event.Type {
		case "tool_call":
			if !window.CurrentSince.IsZero() && timeWithinBounds(t, window.CurrentSince, time.Time{}) {
				window.ToolCalls.Current++
			}
			if !window.LastSince.IsZero() && !window.LastBefore.IsZero() && timeWithinBounds(t, window.LastSince, window.LastBefore) {
				window.ToolCalls.Last++
			}
		case "psyche_molt":
			window.Rebuilds = append(window.Rebuilds, t)
		case "refresh_complete":
			window.Refreshes = append(window.Refreshes, t)
		}
	}
	reverseAndLimitTimes := func(values []time.Time) []time.Time {
		for i, j := 0, len(values)-1; i < j; i, j = i+1, j-1 {
			values[i], values[j] = values[j], values[i]
		}
		if recentN > 0 && len(values) > recentN {
			values = values[:recentN]
		}
		return values
	}
	window.Rebuilds = reverseAndLimitTimes(window.Rebuilds)
	window.Refreshes = reverseAndLimitTimes(window.Refreshes)
	window.TimestampCoverage = len(events) > 0 && !window.CoverageSince.IsZero() && timestampsMonotonic
	for _, event := range events {
		if event.TS <= 0 {
			window.TimestampCoverage = false
			break
		}
	}
	finishKanbanJSONLRead(&window.Read)
	return window
}

// KanbanSessionTokenStats publishes session partitions only when the event
// tail demonstrably covers the oldest timestamp in the independently bounded
// token tail and contains a real current-session molt boundary.
func KanbanSessionTokenStats(tokens KanbanTokenWindow, events KanbanEventWindow) (MoltSessionTokenStats, bool) {
	var stats MoltSessionTokenStats
	tokenState := KanbanReadState(tokens.Read)
	eventState := KanbanReadState(events.Read)
	if !tokens.TimestampCoverage || !events.TimestampCoverage || events.CurrentSince.IsZero() ||
		events.CoverageSince.After(tokens.OldestTimestamp) ||
		(tokenState != KanbanSourceAvailable && tokenState != KanbanSourceRecent) ||
		(eventState != KanbanSourceAvailable && eventState != KanbanSourceRecent) {
		return stats, false
	}
	for _, entry := range tokens.Entries {
		if ledgerEntryWithinBounds(entry, events.CurrentSince, time.Time{}) {
			addLedgerEntryToSessionStats(&stats.Current, entry)
		}
		if !events.LastSince.IsZero() && !events.LastBefore.IsZero() && ledgerEntryWithinBounds(entry, events.LastSince, events.LastBefore) {
			addLedgerEntryToSessionStats(&stats.Last, entry)
		}
	}
	return stats, true
}

// ReadKanbanContextStats summarizes only the final KanbanReadLimit complete
// chat-history lines.
func ReadKanbanContextStats(agentDir string) (ContextStats, BoundedReadStats) {
	path := filepath.Join(agentDir, "history", "chat_history.jsonl")
	lines, read := readBoundedJSONLTail(path, KanbanReadLimit, KanbanJSONLByteLimit)
	var stats ContextStats
	callCounts := map[string]int{}
	resultCounts := map[string]int{}
	for _, rawLine := range lines {
		line := bytes.TrimSpace(rawLine)
		var entry struct {
			Role    string          `json:"role"`
			System  string          `json:"system"`
			Content json.RawMessage `json:"content"`
		}
		if len(line) == 0 {
			continue
		}
		if json.Unmarshal(line, &entry) != nil {
			read.MalformedRecords++
			continue
		}
		read.ValidRecords++
		stats.Entries++
		switch entry.Role {
		case "system":
			stats.SystemMessages++
		case "assistant":
			stats.AssistantMessages++
		case "user":
			stats.UserMessages++
		}
		var blocks []json.RawMessage
		if len(entry.Content) > 0 && json.Unmarshal(entry.Content, &blocks) != nil {
			var plain string
			if json.Unmarshal(entry.Content, &plain) == nil && plain != "" {
				if entry.Role == "assistant" {
					stats.TextOutputs++
				} else if entry.Role != "system" {
					stats.TextInputs++
				}
			}
		}
		for _, rawBlock := range blocks {
			var block struct {
				Type string `json:"type"`
				Name string `json:"name"`
			}
			if json.Unmarshal(rawBlock, &block) != nil {
				continue
			}
			name := block.Name
			if name == "" {
				name = "unknown"
			}
			switch block.Type {
			case "tool_call":
				stats.ToolCalls++
				callCounts[name]++
			case "tool_result":
				stats.ToolResults++
				resultCounts[name]++
			case "text":
				if entry.Role == "assistant" {
					stats.TextOutputs++
				} else if entry.Role != "system" {
					stats.TextInputs++
				}
			}
		}
	}
	names := make([]string, 0, len(callCounts)+len(resultCounts))
	seen := map[string]bool{}
	for name := range callCounts {
		names = append(names, name)
		seen[name] = true
	}
	for name := range resultCounts {
		if !seen[name] {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool {
		if callCounts[names[i]] != callCounts[names[j]] {
			return callCounts[names[i]] > callCounts[names[j]]
		}
		return names[i] < names[j]
	})
	for _, name := range names {
		stats.ToolCounts = append(stats.ToolCounts, ContextToolCount{Name: name, Calls: callCounts[name], Results: resultCounts[name]})
	}
	finishKanbanJSONLRead(&read)
	return stats, read
}

func parseBoundedJSONMap(path string, byteLimit int64) (map[string]any, BoundedReadStats, error) {
	data, stat, err := readBoundedFile(path, byteLimit)
	if err != nil {
		return nil, stat, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		stat.Status = "malformed"
		return nil, stat, err
	}
	stat.ValidRecords = 1
	return raw, stat, nil
}

func ReadKanbanAgentRaw(dir string) (map[string]any, BoundedReadStats, error) {
	return parseBoundedJSONMap(filepath.Join(dir, ".agent.json"), KanbanFixedFileByteLimit)
}

func ReadKanbanInitManifest(dir string) (map[string]any, []BoundedReadStats, error) {
	resolvedPath := filepath.Join(dir, "system", "manifest.resolved.json")
	resolved, resolvedStat, resolvedErr := parseBoundedJSONMap(resolvedPath, KanbanFixedFileByteLimit)
	reads := []BoundedReadStats{resolvedStat}
	if resolvedErr == nil && resolved["schema"] == "lingtai.manifest.resolved/v1" && resolved["source"] == "kernel" {
		if version, ok := resolved["schema_version"].(float64); ok && version == 1 {
			if manifest, ok := resolved["manifest"].(map[string]any); ok {
				flattenInitManifest(manifest)
				return manifest, reads, nil
			}
		}
	}
	rawPath := filepath.Join(dir, "init.json")
	raw, rawStat, err := parseBoundedJSONMap(rawPath, KanbanFixedFileByteLimit)
	reads = append(reads, rawStat)
	if err != nil {
		return nil, reads, err
	}
	manifest, ok := raw["manifest"].(map[string]any)
	if !ok {
		return nil, reads, errors.New("no manifest in init.json")
	}
	flattenInitManifest(manifest)
	return manifest, reads, nil
}

func ReadKanbanStatus(dir string) (AgentStatus, BoundedReadStats) {
	data, stat, err := readBoundedFile(filepath.Join(dir, ".status.json"), KanbanFixedFileByteLimit)
	if err != nil {
		return AgentStatus{}, stat
	}
	var status AgentStatus
	if json.Unmarshal(data, &status) != nil {
		stat.Status = "malformed"
		return AgentStatus{}, stat
	}
	if !statusEconomyIsComplete(data) {
		clearStatusEconomy(&status)
	}
	stat.ValidRecords = 1
	return status, stat
}

func readKanbanHeartbeat(dir string, threshold float64, byteLimit int64, stats *[]BoundedReadStats) bool {
	data, stat, err := readBoundedFile(filepath.Join(dir, ".agent.heartbeat"), byteLimit)
	appendKanbanReadStats(stats, stat)
	if err != nil {
		return false
	}
	ts, err := strconv.ParseFloat(strings.TrimSpace(string(data)), 64)
	if err != nil {
		return false
	}
	sec := int64(ts)
	nsec := int64((ts - float64(sec)) * 1e9)
	return time.Since(time.Unix(sec, nsec)).Seconds() < threshold
}

func readDirEntriesBounded(path string, limit int) ([]os.DirEntry, BoundedReadStats, error) {
	stat := BoundedReadStats{Path: path, LineLimit: limit}
	if limit <= 0 {
		stat.Status = "disabled"
		return nil, stat, nil
	}
	f, err := os.Open(path)
	if err != nil {
		stat.Status = "unavailable"
		return nil, stat, err
	}
	defer f.Close()
	// One extra directory entry is sufficient to prove that the displayed
	// window is incomplete while keeping the directory traversal bounded.
	entries, err := f.ReadDir(limit + 1)
	stat.DirectoryEntriesRead = len(entries)
	if err != nil && err != io.EOF {
		stat.Status = "unavailable"
		return entries, stat, err
	}
	if len(entries) > limit {
		entries = entries[:limit]
		stat.Truncated = true
	} else {
		stat.Complete = true
	}
	stat.Status = "ok"
	return entries, stat, nil
}

func discoverKanbanAgents(baseDir string, limit int, byteLimit int64, stats *[]BoundedReadStats) ([]AgentNode, bool, error) {
	entries, dirStat, err := readDirEntriesBounded(baseDir, limit)
	appendKanbanReadStats(stats, dirStat)
	if err != nil {
		return nil, false, err
	}
	nodes := make([]AgentNode, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		agentDir := filepath.Join(baseDir, entry.Name())
		data, read, readErr := readBoundedFile(filepath.Join(agentDir, ".agent.json"), byteLimit)
		appendKanbanReadStats(stats, read)
		if readErr != nil {
			if os.IsNotExist(readErr) {
				continue
			}
			nodes = append(nodes, malformedAgentNode(agentDir, readErr))
			continue
		}
		var manifest agentManifest
		if json.Unmarshal(data, &manifest) != nil {
			nodes = append(nodes, malformedAgentNode(agentDir, errors.New("parse manifest")))
			continue
		}
		isHuman := manifest.Admin == nil || string(*manifest.Admin) == "null"
		nodes = append(nodes, AgentNode{
			AgentID: manifest.AgentID, Address: manifest.Address, AgentName: manifest.AgentName,
			Nickname: manifest.Nickname, State: manifest.State, IsHuman: isHuman,
			Capabilities: ParseCapabilities(manifest.Capabilities), Location: manifest.Location, WorkingDir: agentDir,
		})
	}
	return nodes, dirStat.Complete, nil
}

func resolveContainedKanbanChild(raw, baseDir string) (string, bool) {
	if raw == "" || filepath.IsAbs(raw) {
		return "", false
	}
	clean := filepath.Clean(raw)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", false
	}
	resolvedBase, err := filepath.EvalSymlinks(baseDir)
	if err != nil {
		return "", false
	}
	candidate := filepath.Join(baseDir, clean)
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(resolvedBase, resolved)
	if err != nil || rel == "." || filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Stat(resolved)
	if err != nil || !info.IsDir() {
		return "", false
	}
	// Keep the lexical in-network path so it matches directories discovered
	// from baseDir; EvalSymlinks above is only the physical containment proof.
	return filepath.Clean(candidate), true
}

func readKanbanLedger(dir, baseDir string, limit int, byteLimit int64, stats *[]BoundedReadStats) ([]AvatarEdge, []string) {
	path := filepath.Join(dir, "delegates", "ledger.jsonl")
	lines, read := readBoundedJSONLTail(path, limit, byteLimit)
	var edges []AvatarEdge
	var childDirs []string
	for _, line := range lines {
		var record ledgerRecord
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		if json.Unmarshal(trimmed, &record) != nil {
			read.MalformedRecords++
			continue
		}
		read.ValidRecords++
		if record.Event != "avatar" || record.WorkingDir == "" {
			continue
		}
		resolved, ok := resolveContainedKanbanChild(record.WorkingDir, baseDir)
		if !ok {
			continue
		}
		edges = append(edges, AvatarEdge{Parent: dir, Child: resolved, ChildName: record.Name})
		childDirs = append(childDirs, resolved)
	}
	finishKanbanJSONLRead(&read)
	appendKanbanReadStats(stats, read)
	return edges, childDirs
}

func readKanbanContacts(dir string, limit int, byteLimit int64, stats *[]BoundedReadStats) []ContactEdge {
	path := filepath.Join(dir, "mailbox", "contacts.json")
	data, read, err := readBoundedFile(path, byteLimit)
	if err != nil {
		appendKanbanReadStats(stats, read)
		return nil
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	token, err := dec.Token()
	if err != nil || token != json.Delim('[') {
		read.Status = "malformed"
		appendKanbanReadStats(stats, read)
		return nil
	}
	baseDir := filepath.Dir(dir)
	var edges []ContactEdge
	for dec.More() && len(edges) < limit {
		var record contactRecord
		if dec.Decode(&record) != nil {
			read.Status = "malformed"
			break
		}
		read.ValidRecords++
		edges = append(edges, ContactEdge{Owner: dir, Target: ResolveAddress(record.Address, baseDir), Name: record.Name})
	}
	appendKanbanReadStats(stats, read)
	return edges
}

func readKanbanNetworkStatus(agentDir string, byteLimit int64, stats *[]BoundedReadStats) (networkStatusSnapshot, time.Time, bool) {
	path := filepath.Join(agentDir, ".status.json")
	info, infoErr := os.Stat(path)
	data, read, err := readBoundedFile(path, byteLimit)
	appendKanbanReadStats(stats, read)
	if err != nil || infoErr != nil {
		return networkStatusSnapshot{}, time.Time{}, false
	}
	var status networkStatusSnapshot
	if json.Unmarshal(data, &status) != nil {
		return networkStatusSnapshot{}, time.Time{}, false
	}
	return status, info.ModTime(), true
}

type daemonDispatchRecord struct {
	Schema    string `json:"schema"`
	Sequence  int64  `json:"sequence"`
	RunID     string `json:"run_id"`
	CreatedAt string `json:"created_at"`
}

func validKanbanRunID(runID string) bool {
	return runID != "" && runID != "." && runID != ".." && filepath.Base(runID) == runID &&
		!strings.ContainsAny(runID, `/\\`)
}

func readKanbanDispatches(agentDir string, lineLimit int, byteLimit int64, stats *[]BoundedReadStats) ([]daemonDispatchRecord, BoundedReadStats, string) {
	daemonDir := filepath.Join(agentDir, "daemons")
	path := filepath.Join(daemonDir, ".dispatch-ledger.jsonl")
	if !lstatKanbanDirectory(daemonDir, stats) {
		read := BoundedReadStats{Path: path, Status: "unavailable"}
		appendKanbanReadStats(stats, read)
		return nil, read, KanbanSourceUnavailable
	}
	guard, ok := lstatKanbanRegularFile(path)
	if !ok {
		appendKanbanReadStats(stats, guard)
		return nil, guard, KanbanReadState(guard)
	}
	lines, read := readBoundedJSONLTail(path, lineLimit, byteLimit)
	parsed := make([]daemonDispatchRecord, 0, len(lines))
	for _, raw := range lines {
		line := bytes.TrimSpace(raw)
		if len(line) == 0 {
			continue
		}
		var record daemonDispatchRecord
		if json.Unmarshal(line, &record) != nil || record.Schema != "lingtai.daemon_dispatch/v1" || !validKanbanRunID(record.RunID) {
			read.MalformedRecords++
			continue
		}
		read.ValidRecords++
		parsed = append(parsed, record)
	}
	// Walk newest-first so duplicate run IDs retain exactly their latest
	// append-ledger occurrence and ordering.
	records := make([]daemonDispatchRecord, 0, len(parsed))
	seen := make(map[string]struct{}, len(parsed))
	for i := len(parsed) - 1; i >= 0; i-- {
		if _, ok := seen[parsed[i].RunID]; ok {
			continue
		}
		seen[parsed[i].RunID] = struct{}{}
		records = append(records, parsed[i])
	}
	finishKanbanJSONLRead(&read)
	state := KanbanReadState(read)
	appendKanbanReadStats(stats, read)
	return records, read, state
}

func lstatKanbanDirectory(path string, stats *[]BoundedReadStats) bool {
	stat := BoundedReadStats{Path: path}
	info, err := os.Lstat(path)
	if err != nil {
		stat.Status = "unavailable"
		appendKanbanReadStats(stats, stat)
		return false
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		stat.Status = "unsafe"
		appendKanbanReadStats(stats, stat)
		return false
	}
	stat.Complete = true
	stat.Status = "ok"
	appendKanbanReadStats(stats, stat)
	return true
}

func lstatKanbanRegularFile(path string) (BoundedReadStats, bool) {
	stat := BoundedReadStats{Path: path}
	info, err := os.Lstat(path)
	if err != nil {
		stat.Status = "unavailable"
		return stat, false
	}
	stat.FileBytes = info.Size()
	if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		stat.Status = "unsafe"
		return stat, false
	}
	return stat, true
}

func readKanbanDaemonCard(path string, stats *[]BoundedReadStats) (daemonCard, bool) {
	guard, ok := lstatKanbanRegularFile(path)
	if !ok {
		appendKanbanReadStats(stats, guard)
		return daemonCard{}, false
	}
	data, read, err := readBoundedFile(path, kanbanDaemonCardByteLimit)
	if err != nil {
		appendKanbanReadStats(stats, read)
		return daemonCard{}, false
	}
	var card daemonCard
	if json.Unmarshal(data, &card) != nil {
		read.Status = "malformed"
		read.MalformedRecords = 1
		appendKanbanReadStats(stats, read)
		return daemonCard{}, false
	}
	read.ValidRecords = 1
	appendKanbanReadStats(stats, read)
	return card, true
}

func readKanbanDaemonTokenWindow(runDir string, stats *[]BoundedReadStats) KanbanTokenWindow {
	logsDir := filepath.Join(runDir, "logs")
	if !lstatKanbanDirectory(logsDir, stats) {
		read := BoundedReadStats{Path: filepath.Join(logsDir, "token_ledger.jsonl"), Status: "unavailable"}
		appendKanbanReadStats(stats, read)
		return KanbanTokenWindow{ByProvider: map[string]TokenTotals{}, Read: read}
	}
	path := filepath.Join(logsDir, "token_ledger.jsonl")
	guard, ok := lstatKanbanRegularFile(path)
	if !ok {
		appendKanbanReadStats(stats, guard)
		return KanbanTokenWindow{ByProvider: map[string]TokenTotals{}, Read: guard}
	}
	window := readKanbanTokenWindow(path, KanbanReadLimit, false)
	appendKanbanReadStats(stats, window.Read)
	return window
}

// ReadKanbanDaemonDetailSnapshot never lists daemons/. It trusts only the
// bounded dispatch tail, opens those exact run IDs, bounds every daemon.json,
// and tails at most 1000 lines from each per-run token ledger.
func ReadKanbanDaemonDetailSnapshot(agentDir string, recentCallN int, stats *[]BoundedReadStats) DaemonDetailSnapshot {
	records, dispatchRead, state := readKanbanDispatches(agentDir, KanbanReadLimit, KanbanJSONLByteLimit, stats)
	result := DaemonDetailSnapshot{
		ByProvider: map[string]TokenTotals{}, DispatchRead: dispatchRead, WindowState: state,
	}
	if len(records) == 0 {
		return result
	}
	daemonDir := filepath.Join(agentDir, "daemons")
	result.ScannedRuns = len(records)
	result.TotalRuns = len(records)
	result.Counts.Total = len(records)
	all := make([]DaemonLedgerEntry, 0)
	for _, record := range records {
		result.RunIDs = append(result.RunIDs, record.RunID)
		runDir := filepath.Join(daemonDir, record.RunID)
		if !lstatKanbanDirectory(runDir, stats) {
			result.WindowState = KanbanSourcePartial
			continue
		}
		card, cardOK := readKanbanDaemonCard(filepath.Join(runDir, "daemon.json"), stats)
		if !cardOK {
			result.WindowState = KanbanSourcePartial
		}
		if card.RunID == "" {
			card.RunID = record.RunID
		}
		addDaemonDetailState(&result.Counts, card)
		tokens := readKanbanDaemonTokenWindow(runDir, stats)
		result.TokenReads = append(result.TokenReads, tokens.Read)
		switch KanbanReadState(tokens.Read) {
		case KanbanSourceUnavailable, KanbanSourceMalformed, KanbanSourcePartial:
			result.WindowState = KanbanSourcePartial
		}
		if len(tokens.Entries) == 0 {
			addDaemonFallbackTotals(result.ByProvider, card)
			continue
		}
		for _, entry := range tokens.Entries {
			provider := DeriveLedgerProvider(entry.Endpoint, entry.Model)
			totals := result.ByProvider[provider]
			totals.Input += entry.Input
			totals.Output += entry.Output
			totals.Thinking += entry.Thinking
			totals.Cached += entry.Cached
			totals.APICalls++
			result.ByProvider[provider] = totals
			all = append(all, DaemonLedgerEntry{LedgerEntry: entry, RunID: card.RunID, Handle: card.Handle, State: card.State, Backend: card.Backend})
		}
	}
	sort.SliceStable(all, func(i, j int) bool { return all[i].TS > all[j].TS })
	if recentCallN > 0 && len(all) > recentCallN {
		all = all[:recentCallN]
	}
	result.Recent = all
	result.TerminalRuns = result.Counts.Done + result.Counts.Failed
	return result
}

func countKanbanDaemons(agentDir string, dispatchLimit int, jsonlByteLimit int64, stats *[]BoundedReadStats) DaemonCounts {
	records, _, _ := readKanbanDispatches(agentDir, dispatchLimit, jsonlByteLimit, stats)
	if len(records) == 0 {
		return DaemonCounts{}
	}
	daemonDir := filepath.Join(agentDir, "daemons")
	counts := DaemonCounts{Total: len(records)}
	for _, record := range records {
		runDir := filepath.Join(daemonDir, record.RunID)
		if !lstatKanbanDirectory(runDir, stats) {
			continue
		}
		card, _ := readKanbanDaemonCard(filepath.Join(runDir, "daemon.json"), stats)
		addDaemonDetailState(&counts, card)
	}
	return counts
}
