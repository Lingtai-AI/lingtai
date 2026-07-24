package fs

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSessionAtomicReplacementNeverExposesTornCanonical(t *testing.T) {
	root := t.TempDir()
	humanDir := filepath.Join(root, "human")
	logsDir := filepath.Join(humanDir, "logs")
	statePath := filepath.Join(logsDir, "session.jsonl")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll session logs: %v", err)
	}

	preEntries := []SessionEntry{{Ts: "2026-07-23T00:00:00Z", Type: "text_output", Body: "complete pre-state"}}
	preBytes := encodeSessionSnapshot(t, preEntries)
	if err := os.WriteFile(statePath, preBytes, 0o644); err != nil {
		t.Fatalf("seed session pre-state: %v", err)
	}

	const (
		entryCount = 96
		bodyBytes  = 64 * 1024
		writes     = 12
	)
	entriesA := largeSessionSnapshot("writer-A", 'A', entryCount, bodyBytes)
	entriesB := largeSessionSnapshot("writer-B", 'B', entryCount, bodyBytes)
	wantA := encodeSessionSnapshot(t, entriesA)
	wantB := encodeSessionSnapshot(t, entriesB)

	writerA := NewSessionCache(humanDir, root, MainAggregateWriter)
	writerB := NewSessionCache(humanDir, root, MainAggregateWriter)
	writerA.entries = entriesA
	writerB.entries = entriesB

	start := make(chan struct{})
	stopObserver := make(chan struct{})
	observerReady := make(chan struct{})
	type observationResult struct {
		reads int
		err   error
	}
	observerDone := make(chan observationResult, 1)
	go func() {
		reads := 0
		ready := false
		for {
			data, err := os.ReadFile(statePath)
			if err != nil {
				observerDone <- observationResult{reads: reads, err: fmt.Errorf("read canonical session path: %w", err)}
				return
			}
			reads++
			if !bytes.Equal(data, preBytes) && !bytes.Equal(data, wantA) && !bytes.Equal(data, wantB) {
				prefix := data
				if len(prefix) > 160 {
					prefix = prefix[:160]
				}
				observerDone <- observationResult{
					reads: reads,
					err:   fmt.Errorf("observed torn canonical session snapshot: size=%d prefix=%q", len(data), prefix),
				}
				return
			}
			if !ready {
				close(observerReady)
				ready = true
			}
			select {
			case <-stopObserver:
				observerDone <- observationResult{reads: reads}
				return
			default:
				runtime.Gosched()
			}
		}
	}()

	select {
	case <-observerReady:
	case result := <-observerDone:
		t.Fatalf("session observer stopped before writers started: %v", result.err)
	case <-time.After(5 * time.Second):
		t.Fatal("session observer did not read the seeded canonical snapshot within 5s")
	}

	var writers sync.WaitGroup
	persistRepeatedly := func(cache *SessionCache) {
		defer writers.Done()
		<-start
		for i := 0; i < writes; i++ {
			cache.Persist()
			runtime.Gosched()
		}
	}
	writers.Add(2)
	go persistRepeatedly(writerA)
	go persistRepeatedly(writerB)
	close(start)
	writers.Wait()
	close(stopObserver)

	var result observationResult
	select {
	case result = <-observerDone:
	case <-time.After(5 * time.Second):
		t.Fatal("session observer did not terminate within 5s")
	}
	if result.err != nil {
		t.Fatal(result.err)
	}
	if result.reads < 2 {
		t.Fatalf("session observer completed only %d read(s), want repeated observation", result.reads)
	}

	finalBytes, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read final canonical session snapshot: %v", err)
	}
	if !bytes.Equal(finalBytes, wantA) && !bytes.Equal(finalBytes, wantB) {
		t.Fatalf("final session snapshot is not either complete writer: size=%d", len(finalBytes))
	}
	entries, err := os.ReadDir(logsDir)
	if err != nil {
		t.Fatalf("ReadDir session logs: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(statePath) {
			t.Errorf("session replacement leaked sibling artifact %q", entry.Name())
		}
	}
}

func TestSessionPersistErrReportsReplacementFailureAndPreservesCanonical(t *testing.T) {
	root := t.TempDir()
	humanDir := filepath.Join(root, "human")
	logsDir := filepath.Join(humanDir, "logs")
	statePath := filepath.Join(logsDir, "session.jsonl")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatalf("MkdirAll session logs: %v", err)
	}
	oldBytes := encodeSessionSnapshot(t, []SessionEntry{{
		Ts: "2026-07-23T00:00:00Z", Type: "text_output", Body: "durable old snapshot",
	}})
	if err := os.WriteFile(statePath, oldBytes, 0o644); err != nil {
		t.Fatalf("seed canonical session snapshot: %v", err)
	}

	cache := NewSessionCache(humanDir, root, MainAggregateWriter)
	cache.entries = []SessionEntry{{
		Ts: "2026-07-23T00:00:01Z", Type: "text_output", Body: "replacement that must not publish",
	}}
	makeDirectoryUnwritableOrSkip(t, logsDir)

	persistErr := reflect.ValueOf(cache).MethodByName("PersistErr")
	if !persistErr.IsValid() {
		t.Fatal("SessionCache has no PersistErr method; replacement failure is not observable")
	}
	errorType := reflect.TypeOf((*error)(nil)).Elem()
	methodType := persistErr.Type()
	if methodType.NumIn() != 0 || methodType.NumOut() != 1 || methodType.Out(0) != errorType {
		t.Fatalf("SessionCache.PersistErr signature = %s, want func() error", methodType)
	}
	results := persistErr.Call(nil)
	if results[0].IsNil() {
		t.Error("SessionCache.PersistErr returned nil for a forced replacement failure")
	}

	after, err := os.ReadFile(statePath)
	if err != nil {
		t.Fatalf("read canonical session after failed replacement: %v", err)
	}
	if !bytes.Equal(after, oldBytes) {
		t.Fatalf("failed session replacement changed canonical bytes:\nbefore=%q\nafter=%q", oldBytes, after)
	}
}

func largeSessionSnapshot(label string, fill byte, count, bodyBytes int) []SessionEntry {
	entries := make([]SessionEntry, count)
	body := strings.Repeat(string(fill), bodyBytes)
	base := time.Date(2026, 7, 23, 0, 0, 0, 0, time.UTC)
	for i := range entries {
		entries[i] = SessionEntry{
			Ts:   base.Add(time.Duration(i) * time.Nanosecond).Format(time.RFC3339Nano),
			Type: "text_output",
			Body: fmt.Sprintf("%s-%03d:%s", label, i, body),
		}
	}
	return entries
}

func encodeSessionSnapshot(t *testing.T, entries []SessionEntry) []byte {
	t.Helper()
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	for _, entry := range entries {
		if err := encoder.Encode(entry); err != nil {
			t.Fatalf("encode session snapshot: %v", err)
		}
	}
	return buffer.Bytes()
}

func makeDirectoryUnwritableOrSkip(t *testing.T, directory string) {
	t.Helper()
	info, err := os.Stat(directory)
	if err != nil {
		t.Fatalf("Stat directory for permission failure: %v", err)
	}
	originalMode := info.Mode().Perm()
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Skipf("cannot make directory unwritable on this platform: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chmod(directory, originalMode); err != nil {
			t.Errorf("restore directory permissions: %v", err)
		}
	})

	probePath := filepath.Join(directory, ".permission-enforcement-probe")
	probe, probeErr := os.OpenFile(probePath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if probeErr == nil {
		if err := probe.Close(); err != nil {
			t.Errorf("close permission probe: %v", err)
		}
		if err := os.Remove(probePath); err != nil {
			t.Errorf("remove permission probe: %v", err)
		}
		if err := os.Chmod(directory, originalMode); err != nil {
			t.Fatalf("restore directory permissions before skip: %v", err)
		}
		t.Skip("directory write permissions are not enforced on this platform; replacement-failure subcase skipped")
	}
	if !os.IsPermission(probeErr) {
		t.Fatalf("permission probe failed for an unrelated reason: %v", probeErr)
	}
}
