package tui

import (
	"strings"
	"testing"
)

func TestFormatHomeAsyncStats(t *testing.T) {
	t.Run("no-data renders empty", func(t *testing.T) {
		if got := formatHomeAsyncStats(homeAsyncStats{}, 80); got != "" {
			t.Fatalf("empty stats should render nothing, got %q", got)
		}
	})

	t.Run("counts render in order with label", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 3, queued: 2, done: 12, failed: 1}, 120)
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

	t.Run("zero buckets are omitted", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 1}, 80)
		if strings.Contains(got, "queued") || strings.Contains(got, "done") || strings.Contains(got, "failed") {
			t.Errorf("zero buckets should be omitted: %q", got)
		}
	})

	t.Run("narrow terminal keeps hint", func(t *testing.T) {
		got := formatHomeAsyncStats(homeAsyncStats{running: 1}, 30)
		if got == "" {
			t.Fatal("expected a row on narrow terminal")
		}
	})
}
