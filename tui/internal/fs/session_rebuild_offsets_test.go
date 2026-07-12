package fs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSessionCacheConstructionAndDetachedRebuildAreFilesystemPure(t *testing.T) {
	root := t.TempDir()
	humanDir := filepath.Join(root, "missing", "human")
	orchDir := filepath.Join(root, "orch")
	writeSessionTestFile(t, filepath.Join(orchDir, "logs", "events.jsonl"), `{"ts":1781300001,"type":"text_output","text":"detached"}`+"\n")

	sc := NewSessionCache(humanDir, root)
	if _, err := os.Stat(humanDir); !os.IsNotExist(err) {
		t.Fatalf("constructor touched human directory: stat error = %v", err)
	}
	sc.RebuildFromSourcesInMemory(NewMailCache(humanDir).Refresh(), "human", orchDir, "orch")
	if _, err := os.Stat(humanDir); !os.IsNotExist(err) {
		t.Fatalf("detached rebuild touched human directory: stat error = %v", err)
	}

	sc.Persist()
	if _, err := os.Stat(filepath.Join(humanDir, "logs", "session.jsonl")); err != nil {
		t.Fatalf("accepted persistence did not create derived cache path: %v", err)
	}
}

func TestRebuildPreservesTrailingPartialJSONLRecords(t *testing.T) {
	t.Run("events", func(t *testing.T) {
		root, humanDir, orchDir := newSessionTestDirs(t)
		path := filepath.Join(orchDir, "logs", "events.jsonl")
		writeSessionTestFile(t, path,
			`{"ts":1781300001,"type":"text_output","text":"first"}`+"\n"+
				`{"ts":1781300002,"type":"text_output","text":"sec`)

		sc := NewSessionCache(humanDir, root)
		cache := NewMailCache(humanDir).Refresh()
		sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
		appendSessionTestFile(t, path, `ond"}`+"\n")
		sc.Refresh(cache, "human", orchDir, "orch")
		assertSessionBody(t, sc.Entries(), "second")
	})

	t.Run("inquiry", func(t *testing.T) {
		root, humanDir, orchDir := newSessionTestDirs(t)
		path := filepath.Join(orchDir, "logs", "soul_inquiry.jsonl")
		writeSessionTestFile(t, path,
			`{"ts":"2026-06-12T21:33:21Z","source":"human","voice":"first"}`+"\n"+
				`{"ts":"2026-06-12T21:33:22Z","source":"insight","voice":"sec`)

		sc := NewSessionCache(humanDir, root)
		cache := NewMailCache(humanDir).Refresh()
		sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
		appendSessionTestFile(t, path, `ond"}`+"\n")
		sc.Refresh(cache, "human", orchDir, "orch")
		assertSessionBody(t, sc.Entries(), "second")
	})

	t.Run("soul flow", func(t *testing.T) {
		root, humanDir, orchDir := newSessionTestDirs(t)
		writeSessionTestFile(t, filepath.Join(orchDir, "logs", "events.jsonl"),
			`{"ts":1781300001,"type":"consultation_fire","fire_id":"fire-partial","count":1}`+"\n")
		path := filepath.Join(orchDir, "logs", "soul_flow.jsonl")
		writeSessionTestFile(t, path, `{"kind":"voice","fire_id":"fire-partial","source":"insights","voice":"partial`)

		sc := NewSessionCache(humanDir, root)
		cache := NewMailCache(humanDir).Refresh()
		sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
		appendSessionTestFile(t, path, ` voice"}`+"\n")
		sc.Refresh(cache, "human", orchDir, "orch")
		assertSessionBodyContains(t, sc.Entries(), "partial voice")
	})
}

func TestRebuildPreservesAppendsThatLandAfterAuthoritativeReads(t *testing.T) {
	root, humanDir, orchDir := newSessionTestDirs(t)
	eventsPath := filepath.Join(orchDir, "logs", "events.jsonl")
	inquiryPath := filepath.Join(orchDir, "logs", "soul_inquiry.jsonl")
	soulPath := filepath.Join(orchDir, "logs", "soul_flow.jsonl")
	writeSessionTestFile(t, eventsPath,
		`{"ts":1781300001,"type":"text_output","text":"before"}`+"\n"+
			`{"ts":1781300002,"type":"consultation_fire","fire_id":"fire-during","count":1}`+"\n")
	writeSessionTestFile(t, inquiryPath, `{"ts":"2026-06-12T21:33:21Z","source":"human","voice":"before inquiry"}`+"\n")
	writeSessionTestFile(t, soulPath, "")

	sc := NewSessionCache(humanDir, root)
	sc.afterRebuildIngest = func() {
		appendSessionTestFile(t, eventsPath, `{"ts":1781300003,"type":"text_output","text":"during event"}`+"\n")
		appendSessionTestFile(t, inquiryPath, `{"ts":"2026-06-12T21:33:23Z","source":"insight","voice":"during inquiry"}`+"\n")
		appendSessionTestFile(t, soulPath, `{"kind":"voice","fire_id":"fire-during","source":"insights","voice":"during soul"}`+"\n")
	}
	cache := NewMailCache(humanDir).Refresh()
	sc.RebuildFromSourcesInMemory(cache, "human", orchDir, "orch")
	sc.afterRebuildIngest = nil
	sc.Refresh(cache, "human", orchDir, "orch")

	entries := sc.Entries()
	assertSessionBody(t, entries, "during event")
	assertSessionBody(t, entries, "during inquiry")
	assertSessionBodyContains(t, entries, "during soul")
}

func newSessionTestDirs(t *testing.T) (root, humanDir, orchDir string) {
	t.Helper()
	root = t.TempDir()
	humanDir = filepath.Join(root, "human")
	orchDir = filepath.Join(root, "orch")
	if err := os.MkdirAll(filepath.Join(orchDir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, humanDir, orchDir
}

func writeSessionTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func appendSessionTestFile(t *testing.T, path, content string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertSessionBody(t *testing.T, entries []SessionEntry, body string) {
	t.Helper()
	for _, entry := range entries {
		if entry.Body == body {
			return
		}
	}
	t.Fatalf("session body %q not found in %#v", body, entries)
}

func assertSessionBodyContains(t *testing.T, entries []SessionEntry, text string) {
	t.Helper()
	for _, entry := range entries {
		if strings.Contains(entry.Body, text) {
			return
		}
	}
	t.Fatalf("session body containing %q not found in %#v", text, entries)
}
