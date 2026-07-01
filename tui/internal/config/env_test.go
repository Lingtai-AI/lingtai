package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func readEnv(t *testing.T, dir string) string {
	t.Helper()
	data, err := os.ReadFile(EnvFilePath(dir))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	return string(data)
}

func TestSetEnvVar_AddsToEmptyDir(t *testing.T) {
	dir := t.TempDir()
	if err := SetEnvVar(dir, SoulFlowEnabledEnvVar, "1"); err != nil {
		t.Fatalf("SetEnvVar: %v", err)
	}
	if got := readEnv(t, dir); got != "LINGTAI_SOUL_FLOW_ENABLED=1\n" {
		t.Errorf(".env = %q, want %q", got, "LINGTAI_SOUL_FLOW_ENABLED=1\n")
	}
}

func TestSetEnvVar_PreservesUnrelatedKeysAndComments(t *testing.T) {
	dir := t.TempDir()
	seed := "# my proxy\nHTTPS_PROXY=http://example:8080\nMINIMAX_API_KEY=secret\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetEnvVar(dir, SoulFlowEnabledEnvVar, "1"); err != nil {
		t.Fatalf("SetEnvVar: %v", err)
	}
	got := readEnv(t, dir)
	want := "# my proxy\nHTTPS_PROXY=http://example:8080\nMINIMAX_API_KEY=secret\nLINGTAI_SOUL_FLOW_ENABLED=1\n"
	if got != want {
		t.Errorf(".env = %q, want %q", got, want)
	}
}

func TestSetEnvVar_UpdatesInPlace(t *testing.T) {
	dir := t.TempDir()
	seed := "A=1\nLINGTAI_SOUL_FLOW_ENABLED=0\nB=2\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetEnvVar(dir, SoulFlowEnabledEnvVar, "1"); err != nil {
		t.Fatalf("SetEnvVar: %v", err)
	}
	got := readEnv(t, dir)
	want := "A=1\nLINGTAI_SOUL_FLOW_ENABLED=1\nB=2\n"
	if got != want {
		t.Errorf("update-in-place .env = %q, want %q", got, want)
	}
}

func TestSetEnvVar_RemovesKeyPreservingRest(t *testing.T) {
	dir := t.TempDir()
	seed := "# header\nA=1\nLINGTAI_SOUL_FLOW_ENABLED=1\nB=2\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetEnvVar(dir, SoulFlowEnabledEnvVar, ""); err != nil {
		t.Fatalf("SetEnvVar remove: %v", err)
	}
	got := readEnv(t, dir)
	want := "# header\nA=1\nB=2\n"
	if got != want {
		t.Errorf("remove .env = %q, want %q", got, want)
	}
}

func TestSetEnvVar_RemoveMissingKeyIsNoOpOnKeys(t *testing.T) {
	dir := t.TempDir()
	seed := "A=1\nB=2\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetEnvVar(dir, SoulFlowEnabledEnvVar, ""); err != nil {
		t.Fatalf("SetEnvVar remove missing: %v", err)
	}
	got := readEnv(t, dir)
	if got != seed {
		t.Errorf("remove-missing .env = %q, want unchanged %q", got, seed)
	}
}

func TestSetEnvVar_PreservesFilePermissions(t *testing.T) {
	dir := t.TempDir()
	path := EnvFilePath(dir)
	if err := os.WriteFile(path, []byte("A=1\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := SetEnvVar(dir, SoulFlowEnabledEnvVar, "1"); err != nil {
		t.Fatalf("SetEnvVar: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf(".env perm = %o, want 600", perm)
	}
}

func TestSoulFlowEnabled_TruthyVariants(t *testing.T) {
	cases := map[string]bool{
		"1":     true,
		"true":  true,
		"TRUE":  true,
		"Yes":   true,
		"on":    true,
		"0":     false,
		"false": false,
		"":      false,
		"maybe": false,
	}
	for val, want := range cases {
		dir := t.TempDir()
		if err := os.WriteFile(EnvFilePath(dir),
			[]byte(SoulFlowEnabledEnvVar+"="+val+"\n"), 0o600); err != nil {
			t.Fatalf("seed %q: %v", val, err)
		}
		if got := SoulFlowEnabled(dir); got != want {
			t.Errorf("SoulFlowEnabled(%q) = %v, want %v", val, got, want)
		}
	}
}

func TestSoulFlowEnabled_MissingFileIsDisabled(t *testing.T) {
	dir := t.TempDir()
	if SoulFlowEnabled(dir) {
		t.Error("SoulFlowEnabled on missing .env = true, want false")
	}
}

func TestSoulFlowEnabledInEnvFile_ExplicitPath(t *testing.T) {
	// The /kanban path reads a specific env_file, which may differ from the
	// global dir convention.
	dir := t.TempDir()
	envPath := filepath.Join(dir, "custom.env")
	if err := os.WriteFile(envPath, []byte("LINGTAI_SOUL_FLOW_ENABLED=on\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if !SoulFlowEnabledInEnvFile(envPath) {
		t.Error("SoulFlowEnabledInEnvFile(on) = false, want true")
	}
	if SoulFlowEnabledInEnvFile(filepath.Join(dir, "nope.env")) {
		t.Error("SoulFlowEnabledInEnvFile(missing) = true, want false")
	}
}

func TestSoulFlowEnabled_AbsentKeyIsDisabled(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(EnvFilePath(dir), []byte("A=1\nB=2\n"), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if SoulFlowEnabled(dir) {
		t.Error("SoulFlowEnabled with absent key = true, want false")
	}
}

func TestWriteEnvFile_PreservesUnmanagedKeysAndComments(t *testing.T) {
	// The core regression this PR guards: saving API keys must not
	// clobber a separately-written LINGTAI_SOUL_FLOW_ENABLED line (or
	// any other unmanaged var / comment).
	dir := t.TempDir()
	seed := "# user proxy\nHTTPS_PROXY=http://example:8080\nLINGTAI_SOUL_FLOW_ENABLED=1\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := Config{Keys: map[string]string{"MINIMAX_API_KEY": "k1"}}
	if err := WriteEnvFile(dir, cfg); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	got := readEnv(t, dir)
	// Preserved lines stay in order; the new managed key is appended.
	want := "# user proxy\nHTTPS_PROXY=http://example:8080\nLINGTAI_SOUL_FLOW_ENABLED=1\nMINIMAX_API_KEY=k1\n"
	if got != want {
		t.Errorf("WriteEnvFile .env = %q, want %q", got, want)
	}
	// And the opt-in still reads as enabled after an unrelated key save.
	if !SoulFlowEnabled(dir) {
		t.Error("soul-flow opt-in lost after WriteEnvFile")
	}
}

func TestWriteEnvFile_UpdatesManagedKeyInPlace(t *testing.T) {
	dir := t.TempDir()
	seed := "MINIMAX_API_KEY=old\nLINGTAI_SOUL_FLOW_ENABLED=1\n"
	if err := os.WriteFile(EnvFilePath(dir), []byte(seed), 0o600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	cfg := Config{Keys: map[string]string{"MINIMAX_API_KEY": "new"}}
	if err := WriteEnvFile(dir, cfg); err != nil {
		t.Fatalf("WriteEnvFile: %v", err)
	}
	got := readEnv(t, dir)
	want := "MINIMAX_API_KEY=new\nLINGTAI_SOUL_FLOW_ENABLED=1\n"
	if got != want {
		t.Errorf("WriteEnvFile update .env = %q, want %q", got, want)
	}
}

func TestSaveConfig_PreservesSoulFlowOptIn(t *testing.T) {
	// End-to-end: opt in, then save an API key via the normal SaveConfig
	// path, and confirm the opt-in survives (SaveConfig → WriteEnvFile).
	dir := t.TempDir()
	if err := SetEnvVar(dir, SoulFlowEnabledEnvVar, "1"); err != nil {
		t.Fatalf("SetEnvVar: %v", err)
	}
	cfg := Config{Keys: map[string]string{"ZHIPU_API_KEY": "z"}}
	if err := SaveConfig(dir, cfg); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if !SoulFlowEnabled(dir) {
		t.Error("soul-flow opt-in lost after SaveConfig")
	}
	if got := readEnv(t, dir); !envHasLine(got, "ZHIPU_API_KEY=z") {
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
