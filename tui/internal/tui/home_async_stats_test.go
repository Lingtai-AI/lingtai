package tui

import (
	"strings"
	"testing"
	"time"
)

func TestFormatHomeAsyncStats(t *testing.T) {
	t.Run("all-zero renders zero count and token segments", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{}, 120)
		if got == "" {
			t.Fatal("all-zero stats must still render the row")
		}
		for _, want := range []string{
			"running 0", "queued 0", "done 0", "failed 0",
			"in 0", "out 0", "cache 0%", "calls 0",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("row missing %q: %q", want, got)
			}
		}
	})

	t.Run("counts render in order with label", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 3, queued: 2, done: 12, failed: 1}, 160)
		if got == "" {
			t.Fatal("expected a row")
		}
		for _, want := range []string{"running 3", "queued 2", "done 12", "failed 1"} {
			if !strings.Contains(got, want) {
				t.Errorf("row missing %q: %q", want, got)
			}
		}
		if !strings.HasPrefix(got, "  ") {
			t.Errorf("row should be left-padded, got %q", got)
		}
	})

	t.Run("token and api-call stats render compactly", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{
			running: 3, queued: 2, done: 12, failed: 1,
			input: 1234, output: 567, cached: 300, calls: 9,
		}, 200)
		for _, want := range []string{
			"running 3", "queued 2", "done 12", "failed 1",
			"in 1.2k", "out 567", "cache 24%", "calls 9",
		} {
			if !strings.Contains(got, want) {
				t.Errorf("row missing %q: %q", want, got)
			}
		}
	})

	t.Run("lingtai models render directly or as stable model counts", func(t *testing.T) {
		single := formatHomeAsyncStats(homeAsyncStats{running: 1, models: []string{"gpt-5.6-terra"}}, 220)
		if want := "(gpt-5.6-terra)"; !strings.Contains(single, want) {
			t.Fatalf("single-model row missing %q: %q", want, single)
		}

		multiple := formatHomeAsyncStats(homeAsyncStats{running: 3, models: []string{"beta", "alpha", "beta"}}, 220)
		if want := "(alpha × 1 · beta × 2)"; !strings.Contains(multiple, want) {
			t.Fatalf("multi-model row missing stable counts %q: %q", want, multiple)
		}
	})

	t.Run("zero count buckets are omitted", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 1}, 160)
		if strings.Contains(got, "queued") || strings.Contains(got, "done") || strings.Contains(got, "failed") {
			t.Errorf("zero count buckets should be omitted: %q", got)
		}
	})

	t.Run("narrow terminal keeps hint", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 1}, 30)
		if got == "" {
			t.Fatal("expected a row on narrow terminal")
		}
	})
}

// TestHomeAsyncStatsRowVisibilityAfterLoad pins the always-visible contract:
// the mailview daemon row is hidden only before the first snapshot lands, and
// stays visible with all-zero counts once loaded — idle daemons inside the
// 10-minute window must not drop the row (Jason's regression report).
func TestHomeAsyncStatsRowVisibilityAfterLoad(t *testing.T) {
	var m MailModel
	if m.hasHomeAsyncStats() {
		t.Fatal("row must be hidden before the first snapshot lands")
	}
	if !m.applyHomeAsyncStats(homeAsyncStats{}, time.Now()) {
		t.Fatal("first snapshot landing must flip the row to visible")
	}
	if !m.hasHomeAsyncStats() {
		t.Fatal("row must be visible after load even when all counts are zero")
	}
	if m.applyHomeAsyncStats(homeAsyncStats{running: 1}, time.Now()) {
		t.Fatal("later snapshots must not flip visibility")
	}
	if !m.hasHomeAsyncStats() {
		t.Fatal("row must stay visible with non-zero counts")
	}
	m.applyHomeAsyncStats(homeAsyncStats{}, time.Now())
	if !m.hasHomeAsyncStats() {
		t.Fatal("row must stay visible when counts drop back to zero")
	}
}
