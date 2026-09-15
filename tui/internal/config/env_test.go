package config

import (
	"os"
	"strings"
	"testing"
)

// retiredEnvLine is a line a user's pre-existing .env may still carry from a
// retired feature (the former Soul opt-in). The TUI must never rewrite or
// remove it: WriteEnvFile owns only Config.Keys lines.
const retiredEnvLine = "LINGTAI_SOUL_FLOW_ENABLED=1"

func readEnv(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(EnvFilePath(dir))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	return string(data)
}

func TestWriteEnvFile_PreservesUnmanagedKeysAndComments(t *testing.T) {
	// Saving API keys must not clobber any unmanaged var or comment —
	// including a line left behind by a retired feature.
	dir := t.TempDir()
	seed := "# user proxy\nHTTPS_PROXY=http://example:8080\n" + retiredEnvLine + "\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := Config{Keys: map[string]string{"MINIMAX_API_KEY": "k1"}}
	if err := WriteEnvFile(dir, cfg); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	got := readEnv(t, dir)
	// Preserved lines stay in order; the new managed key is appended.
	want := seed + "MINIMAX_API_KEY=k1\n"
	if got != want {
		t.Errorf("WriteEnvFile .env = %q, want %q", got, want)
	}
}

func TestWriteEnvFile_UpdatesManagedKeyInPlace(t *testing.T) {
	dir := t.TempDir()
	seed := "MINIMAX_API_KEY=old\n" + retiredEnvLine + "\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := Config{Keys: map[string]string{"MINIMAX_API_KEY": "new"}}
	if err := WriteEnvFile(dir, cfg); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	got := readEnv(t, dir)
	want := "MINIMAX_API_KEY=new\n" + retiredEnvLine + "\n"
	if got != want {
		t.Errorf("WriteEnvFile update .env = %q, want %q", got, want)
	}
}

func TestWriteEnvFile_PreservesFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := EnvFilePath(dir)
	if err := os.WriteFile(path, []byte(retiredEnvLine+"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := WriteEnvFile(dir, Config{Keys: map[string]string{"ZHIPU_API_KEY": "z"}}); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf(".env perm = %o, want 600", perm)
	}
}

func TestSaveConfig_PreservesUnmanagedEnvVar(t *testing.T) {
	// End-to-end: a retired-feature line is present, then an API key is
	// saved via the normal SaveConfig path (SaveConfig → WriteEnvFile); the
	// unmanaged line survives untouched.
	dir := t.TempDir()
	if err := os.WriteFile(EnvFilePath(dir), []byte(retiredEnvLine+"\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := Config{Keys: map[string]string{"ZHIPU_API_KEY": "z"}}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got := readEnv(t, dir)
	if !envHasLine(got, retiredEnvLine) {
		t.Errorf(".env lost retired-feature line after SaveConfig; got %q", got)
	}
	if !envHasLine(got, "ZHIPU_API_KEY=z") {
		t.Errorf(".env missing saved API key; got %q", got)
	}
}

func envHasLine(content, line string) bool {
	for _, l := range strings.Split(content, "\n") {
		if l == line {
			return true
		}
	}
	return false
}
