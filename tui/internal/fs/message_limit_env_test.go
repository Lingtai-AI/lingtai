package fs

import (
	"fmt"
	"os"
	"testing"
)

// useMessageLimitEnv puts MessageLimitEnv into the state a case describes for
// the duration of the test. "unset" is expressed by removing the variable after
// t.Setenv has registered its restore, so a value inherited from the developer's
// shell cannot make the default cases pass for the wrong reason.
func useMessageLimitEnv(t *testing.T, set bool, value string) {
	t.Helper()
	t.Setenv(MessageLimitEnv, value)
	if !set {
		if err := os.Unsetenv(MessageLimitEnv); err != nil {
			t.Fatalf("unset %s: %v", MessageLimitEnv, err)
		}
	}
}

// TestRecentMessageLimitResolvesTheEnvironmentWindow is the complete parse
// contract. The resolver is the only reader of MessageLimitEnv anywhere, so
// these cases are the whole definition of what the variable can mean: anything
// that is not a non-negative integer is the default, and zero is the explicit
// unlimited opt-in rather than an empty page.
func TestRecentMessageLimitResolvesTheEnvironmentWindow(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   bool
		value string
		want  int
	}{
		{name: "unset environment resolves to the default window", want: DefaultRecentMessageLimit},
		{name: "empty value resolves to the default window", set: true, value: "", want: DefaultRecentMessageLimit},
		{name: "whitespace-only value resolves to the default window", set: true, value: "   ", want: DefaultRecentMessageLimit},
		{name: "non-numeric value resolves to the default window", set: true, value: "lots", want: DefaultRecentMessageLimit},
		{name: "fractional value resolves to the default window", set: true, value: "12.5", want: DefaultRecentMessageLimit},
		{name: "negative value resolves to the default window", set: true, value: "-1", want: DefaultRecentMessageLimit},
		{name: "positive value sets the window", set: true, value: "37", want: 37},
		{name: "whitespace around a positive value is tolerated", set: true, value: "  37\n", want: 37},
		{name: "zero means unlimited", set: true, value: "0", want: 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMessageLimitEnv(t, tc.set, tc.value)
			if got := RecentMessageLimit(); got != tc.want {
				t.Fatalf("RecentMessageLimit() = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestClampToRecentMessageLimitBoundsCallersWithoutClampingUnlimited pins the
// half every consumer depends on: an unlimited window must leave a request
// alone rather than clamp it to zero, which is exactly the mistake each caller
// would be free to make if it compared against the resolver itself.
func TestClampToRecentMessageLimitBoundsCallersWithoutClampingUnlimited(t *testing.T) {
	for _, tc := range []struct {
		name  string
		set   bool
		value string
		n     int
		want  int
	}{
		{name: "default window leaves a smaller request alone", n: 12, want: 12},
		{name: "default window bounds a larger request", n: DefaultRecentMessageLimit + 500, want: DefaultRecentMessageLimit},
		{name: "invalid value bounds with the default window", set: true, value: "-4", n: DefaultRecentMessageLimit + 1, want: DefaultRecentMessageLimit},
		{name: "configured window bounds a larger request", set: true, value: "5", n: 9, want: 5},
		{name: "configured window leaves a smaller request alone", set: true, value: "5", n: 2, want: 2},
		{name: "unlimited window never clamps, least of all to zero", set: true, value: "0", n: 1_000_000, want: 1_000_000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMessageLimitEnv(t, tc.set, tc.value)
			if got := ClampToRecentMessageLimit(tc.n); got != tc.want {
				t.Fatalf("ClampToRecentMessageLimit(%d) = %d, want %d", tc.n, got, tc.want)
			}
		})
	}
}

// TestRefreshRecentLoadsTheResolvedWindow is the fs-side caller semantics: the
// mailbox refresh every page reads through takes its bound from the resolver,
// so the environment decides how many bodies a refresh opens — including the
// explicit zero, which is proven here against a mailbox that actually exceeds
// the default window. Seeding that mailbox is the expensive part, so every case
// shares it.
func TestRefreshRecentLoadsTheResolvedWindow(t *testing.T) {
	humanDir := t.TempDir()
	const extra = 5
	total := DefaultRecentMessageLimit + extra
	for i := range total {
		writeRecentWindowMessage(t, humanDir, "inbox", i)
	}

	for _, tc := range []struct {
		name  string
		set   bool
		value string
		want  int
	}{
		{name: "unset environment loads the default window", want: DefaultRecentMessageLimit},
		{name: "invalid value loads the default window", set: true, value: "nope", want: DefaultRecentMessageLimit},
		{name: "positive value loads exactly that many newest entries", set: true, value: "7", want: 7},
		{name: "zero loads the full history past the default window", set: true, value: "0", want: total},
	} {
		t.Run(tc.name, func(t *testing.T) {
			useMessageLimitEnv(t, tc.set, tc.value)

			cache := NewMailCache(humanDir).RefreshRecent(RecentMessageLimit())

			if len(cache.Messages) != tc.want {
				t.Fatalf("refresh loaded %d messages, want %d", len(cache.Messages), tc.want)
			}
			if len(cache.Messages) == 0 {
				t.Fatal("refresh loaded nothing; the newest-entry assertion below would pass vacuously")
			}
			// Whichever window resolved, what it holds is the newest end of the
			// mailbox in chronological order — the bound never turns into a
			// different slice of history.
			newest := cache.Messages[len(cache.Messages)-1]
			if want := fmt.Sprintf("body-%d", total-1); newest.Message != want {
				t.Fatalf("newest loaded message = %q, want %q", newest.Message, want)
			}
		})
	}
}
