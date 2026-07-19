//go:build !windows

package spawnprocess

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func mustLookPath(t *testing.T, name string) string {
	t.Helper()
	path, err := exec.LookPath(name)
	if err != nil {
		t.Fatalf("required test binary %q not on PATH: %v", name, err)
	}
	return path
}

func kindOf(t *testing.T, err error) FailureKind {
	t.Helper()
	var spawnErr *Error
	if !errors.As(err, &spawnErr) {
		t.Fatalf("want *spawnprocess.Error, got %T: %v", err, err)
	}
	return spawnErr.Kind
}

func TestSpawnEmptyExecutableIsInvalidSpec(t *testing.T) {
	child, err := Spawn(Spec{})
	if child != nil {
		t.Fatalf("want no child, got %+v", child)
	}
	if kind := kindOf(t, err); kind != FailureInvalidSpec {
		t.Fatalf("want %q, got %q", FailureInvalidSpec, kind)
	}
}

func TestSpawnMissingExecutableIsNotFound(t *testing.T) {
	child, err := Spawn(Spec{Executable: filepath.Join(t.TempDir(), "missing")})
	if child != nil {
		t.Fatalf("want no child, got %+v", child)
	}
	if kind := kindOf(t, err); kind != FailureNotFound {
		t.Fatalf("want %q, got %q", FailureNotFound, kind)
	}
}

func TestSpawnReturnsChildIdentity(t *testing.T) {
	child, err := Spawn(Spec{Executable: mustLookPath(t, "true")})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if child == nil || child.PID <= 0 || child.Proc == nil || child.Proc.Pid != child.PID {
		t.Fatalf("want consistent child identity, got %+v", child)
	}
	state, err := child.Proc.Wait() // test-side reaping; the port never waits
	if err != nil || !state.Success() {
		t.Fatalf("child did not exit cleanly: state=%v err=%v", state, err)
	}
}

func TestSpawnHonorsDirEnvAndStdout(t *testing.T) {
	dir := t.TempDir()
	out, err := os.Create(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	child, err := Spawn(Spec{
		Executable: mustLookPath(t, "sh"),
		Args:       []string{"-c", "pwd && printf '%s\\n' \"$SPAWN_PROBE\""},
		Env:        []string{"SPAWN_PROBE=probe-value"},
		Dir:        dir,
		Stdout:     out,
	})
	if err != nil {
		t.Fatalf("spawn: %v", err)
	}
	if _, err := child.Proc.Wait(); err != nil {
		t.Fatalf("wait: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "out"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	wantDir, _ := filepath.EvalSymlinks(dir)
	if !strings.Contains(got, wantDir) && !strings.Contains(got, dir) {
		t.Errorf("stdout %q does not show working directory %q", got, dir)
	}
	if !strings.Contains(got, "probe-value") {
		t.Errorf("stdout %q does not show env value", got)
	}
}
