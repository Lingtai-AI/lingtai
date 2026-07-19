package tuiinstance

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAcquireThenContendedThenReleaseCycle(t *testing.T) {
	dir := t.TempDir()

	first := Acquire(dir)
	if first.State != Acquired || first.Token == nil {
		t.Fatalf("first Acquire = %+v, want Acquired with token", first)
	}
	if _, err := os.Stat(filepath.Join(dir, lockFileName)); err != nil {
		t.Fatalf("lock file missing after Acquire: %v", err)
	}

	second := Acquire(dir)
	if second.State != Contended {
		t.Fatalf("Acquire while held: state = %v, want Contended", second.State)
	}
	if second.Token != nil {
		t.Fatal("Contended acquisition must not carry a token")
	}
	if second.Detail == "" {
		t.Fatal("Contended acquisition must carry a Detail")
	}

	first.Token.Release()
	first.Token.Release() // second call must be a safe no-op

	if _, err := os.Stat(filepath.Join(dir, lockFileName)); err != nil {
		t.Fatalf("Release must not delete the lock file: %v", err)
	}

	third := Acquire(dir)
	if third.State != Acquired || third.Token == nil {
		t.Fatalf("Acquire after Release = %+v, want Acquired", third)
	}
	third.Token.Release()
}

func TestAcquireUnknownOnMissingScopeDir(t *testing.T) {
	got := Acquire(filepath.Join(t.TempDir(), "does-not-exist"))
	if got.State != Unknown {
		t.Fatalf("state = %v, want Unknown", got.State)
	}
	if got.Token != nil {
		t.Fatal("Unknown acquisition must not carry a token")
	}
	if got.Detail == "" {
		t.Fatal("Unknown acquisition must carry a Detail")
	}
}

func TestStaleLockFileIsNotEvidence(t *testing.T) {
	// A pre-existing, unheld lock file (any age/content) must not block.
	dir := t.TempDir()
	path := filepath.Join(dir, lockFileName)
	if err := os.WriteFile(path, []byte("pid 12345 stale"), 0o600); err != nil {
		t.Fatal(err)
	}
	got := Acquire(dir)
	if got.State != Acquired || got.Token == nil {
		t.Fatalf("Acquire over unheld stale file = %+v, want Acquired", got)
	}
	got.Token.Release()
}
