package tui

import (
	"testing"
	"time"
)

// The home telemetry row is resolved asynchronously: gathering it does sqlite +
// token-ledger + .status.json I/O that must never run on the Bubble Tea render
// (View) or input (syncViewportHeight) path. These tests pin the scheduling and
// state machine that keeps that I/O off the UI thread — debounce (in-flight),
// TTL, first-load, and the visibility-change gate that avoids layout thrash.

// A freshly constructed model must render NO telemetry row: no fetch has landed,
// so hasHomeTelemetry() is false regardless of the zero-value snapshot. This is
// the guard against the contextUsage==0 sentinel trap: hasData() treats
// contextUsage>=0 as "present", and the zero value is 0, so without the
// homeTelemetryLoaded gate a brand-new model would spuriously show "ctx 0%".
func TestHomeTelemetryHiddenBeforeFirstFetch(t *testing.T) {
	m := MailModel{}
	if m.homeTelemetry.hasData() != true {
		// Document the trap explicitly: the zero snapshot DOES report hasData()
		// (contextUsage==0 passes the >=0 test), which is exactly why the loaded
		// gate is required.
		t.Fatal("precondition changed: zero homeTelemetry no longer reports hasData(); the loaded gate may be unnecessary — re-check hasHomeTelemetry")
	}
	if m.hasHomeTelemetry() {
		t.Fatal("hasHomeTelemetry() must be false before the first async fetch lands (homeTelemetryLoaded gate), even though the zero snapshot reports hasData()")
	}
}

// maybeScheduleHomeTelemetry is the single debounce/TTL/in-flight gate. The first
// call (nothing loaded, nothing in flight) must schedule a fetch and mark it
// in-flight.
func TestMaybeScheduleFirstFetch(t *testing.T) {
	m := MailModel{}
	cmd := m.maybeScheduleHomeTelemetry(time.Unix(1000, 0))
	if cmd == nil {
		t.Fatal("first schedule must return a fetch command")
	}
	if !m.homeTelemetryInFlight {
		t.Fatal("scheduling must mark the fetch in-flight so concurrent renders don't double-schedule")
	}
}

// While a fetch is in flight, no new fetch may be scheduled — a burst of
// keypresses/renders must not spawn a pile of sqlite subprocesses.
func TestMaybeScheduleDebouncesInFlight(t *testing.T) {
	m := MailModel{homeTelemetryInFlight: true}
	if cmd := m.maybeScheduleHomeTelemetry(time.Unix(1000, 0)); cmd != nil {
		t.Fatal("must not schedule a second fetch while one is in flight")
	}
}

// After a completed fetch, a second fetch within the TTL is skipped (reuse the
// cached snapshot); once the TTL elapses a new fetch is allowed again.
func TestMaybeScheduleHonorsTTL(t *testing.T) {
	base := time.Unix(1000, 0)
	m := MailModel{
		homeTelemetryLoaded:    true,
		homeTelemetryLastFetch: base,
	}

	// Within the TTL: no fetch.
	within := base.Add(homeTelemetryTTL - time.Millisecond)
	if cmd := m.maybeScheduleHomeTelemetry(within); cmd != nil {
		t.Fatal("must not re-fetch within the TTL — the cached snapshot should be reused")
	}
	if m.homeTelemetryInFlight {
		t.Fatal("a TTL-skipped schedule must not flip the in-flight flag")
	}

	// At/after the TTL: fetch allowed.
	after := base.Add(homeTelemetryTTL)
	if cmd := m.maybeScheduleHomeTelemetry(after); cmd == nil {
		t.Fatal("must re-fetch once the TTL has elapsed")
	}
}

// The very first fetch is exempt from the TTL: with nothing loaded yet, the row
// should appear as promptly as possible rather than waiting a full TTL.
func TestMaybeScheduleFirstFetchIgnoresTTL(t *testing.T) {
	// Not loaded, and a lastFetch stamp that would otherwise be "too recent".
	now := time.Unix(1000, 0)
	m := MailModel{homeTelemetryLastFetch: now}
	if cmd := m.maybeScheduleHomeTelemetry(now); cmd == nil {
		t.Fatal("the first fetch (nothing loaded) must not be blocked by the TTL floor")
	}
}

// applyHomeTelemetry lands a fetch result: it stores the snapshot, marks it
// loaded, clears in-flight, stamps the completion time, and reports whether the
// row's VISIBILITY flipped (the only telemetry change that affects layout).
func TestApplyHomeTelemetryStateTransitions(t *testing.T) {
	m := MailModel{homeTelemetryInFlight: true}
	now := time.Unix(2000, 0)

	// First landing with real data: visibility flips false→true.
	data := homeTelemetry{contextUsage: 0.5, sessionTokens: 100}
	changed := m.applyHomeTelemetry(data, now)
	if !changed {
		t.Fatal("first data landing must report a visibility change (no row → row)")
	}
	if !m.homeTelemetryLoaded {
		t.Fatal("applyHomeTelemetry must mark the snapshot loaded")
	}
	if m.homeTelemetryInFlight {
		t.Fatal("applyHomeTelemetry must clear the in-flight flag so the next tick can re-fetch")
	}
	if !m.homeTelemetryLastFetch.Equal(now) {
		t.Fatalf("applyHomeTelemetry must stamp the completion time for the TTL, got %v want %v", m.homeTelemetryLastFetch, now)
	}
	if !m.hasHomeTelemetry() {
		t.Fatal("after landing real data hasHomeTelemetry() must be true")
	}

	// Second landing with the same visibility (still has data): no layout change,
	// so the caller must NOT re-sync the viewport (avoids thrash on numeric ticks).
	data2 := homeTelemetry{contextUsage: 0.6, sessionTokens: 200}
	if changed := m.applyHomeTelemetry(data2, now.Add(time.Second)); changed {
		t.Fatal("a data→data update with unchanged visibility must NOT report a layout change")
	}

	// Landing empty data (contextUsage sentinel -1, no tokens): visibility flips
	// true→false, so the row must be removed and the caller re-syncs.
	empty := homeTelemetry{contextUsage: -1}
	if changed := m.applyHomeTelemetry(empty, now.Add(2*time.Second)); !changed {
		t.Fatal("data→no-data must report a visibility change so the row is removed")
	}
	if m.hasHomeTelemetry() {
		t.Fatal("after landing empty telemetry hasHomeTelemetry() must be false")
	}
}
