package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestFile writes a file at path with content, creating parent dirs.
func writeTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// startupFixture builds an orchDir with a given init.json and a globalDir with
// optional config.json/.env contents, returning both paths.
func startupFixture(t *testing.T, initJSON, configJSON, env string) (orchDir, globalDir string) {
	t.Helper()
	root := t.TempDir()
	// orchDir is a child of a lingtai dir so DetectOrchestrators sees the parent.
	lingtaiDir := filepath.Join(root, "lingtai")
	orchDir = filepath.Join(lingtaiDir, "orch1")
	writeTestFile(t, filepath.Join(orchDir, "init.json"), initJSON)
	// A minimal .agent.json with admin privileges so DetectOrchestrators counts it.
	writeTestFile(t, filepath.Join(orchDir, ".agent.json"), `{"admin": {"karma": true}}`)
	globalDir = filepath.Join(root, "global")
	if configJSON != "" {
		writeTestFile(t, filepath.Join(globalDir, "config.json"), configJSON)
	}
	if env != "" {
		writeTestFile(t, filepath.Join(globalDir, ".env"), env)
	}
	return orchDir, globalDir
}

func TestCheckStartupDecision_Healthy(t *testing.T) {
	orchDir, globalDir := startupFixture(t,
		`{"addons": ["telegram"]}`,
		`{"keys": {"ANTHROPIC_API_KEY": "sk-mirror"}}`,
		`ANTHROPIC_API_KEY=sk-env`,
	)
	// Declared addon telegram has secrets in file form.
	writeTestFile(t, filepath.Join(orchDir, ".secrets", "telegram.json"), `{"bot_token": "x"}`)

	lines := checkStartupDecision(orchDir, globalDir)
	if len(lines) == 0 {
		t.Fatalf("expected startup decision lines")
	}
	assertLineFlag(t, lines, "orchestrator", true, false)
	assertLineFlag(t, lines, "config.json present", true, false)
	assertLineFlag(t, lines, "API keys present", true, false)
	assertLineFlag(t, lines, "telegram secrets present", true, false)
}

func TestCheckStartupDecision_Degraded(t *testing.T) {
	orchDir, globalDir := startupFixture(t,
		`{"addons": {"imap": {}}}`,
		``, // no config.json → R3.1 degraded
		`ANTHROPIC_API_KEY=sk-env`,
	)
	// imap declared but no .secrets/imap.json

	lines := checkStartupDecision(orchDir, globalDir)
	assertLineFlag(t, lines, "orchestrator", true, false)
	assertLineFlag(t, lines, "config.json missing", false, false)
	assertLineFlag(t, lines, "API keys present", true, false)
	assertLineFlag(t, lines, "imap", false, false)
}

func TestCheckStartupDecision_NoAgents(t *testing.T) {
	root := t.TempDir()
	// orchDir has no .agent.json → DetectOrchestrators finds zero.
	orchDir := filepath.Join(root, "lingtai", "human")
	writeTestFile(t, filepath.Join(orchDir, "init.json"), `{}`)
	// human/.agent.json admin:null → not an orchestrator.
	writeTestFile(t, filepath.Join(orchDir, ".agent.json"), `{"admin": null}`)
	globalDir := filepath.Join(root, "global")

	lines := checkStartupDecision(orchDir, globalDir)
	assertLineFlag(t, lines, "No agents detected", false, false)
}

func TestCheckAddonSecrets_DirectoryForm(t *testing.T) {
	orchDir, _ := startupFixture(t, `{"addons": ["wechat"]}`, "", "")
	// wechat uses .secrets/wechat/config.json directory form.
	writeTestFile(t, filepath.Join(orchDir, ".secrets", "wechat", "config.json"), `{"app_id": "x"}`)

	lines := checkAddonSecrets(orchDir)
	assertLineFlag(t, lines, "wechat secrets present", true, false)
}

func TestCheckAddonSecrets_NoAddons(t *testing.T) {
	orchDir, _ := startupFixture(t, `{}`, "", "")
	lines := checkAddonSecrets(orchDir)
	assertLineFlag(t, lines, "No addons declared", true, false)
}

func TestCheckVersionSkew(t *testing.T) {
	orig := tuiVersion
	defer func() { tuiVersion = orig }()

	tuiVersion = "dev"
	lines := checkVersionSkew()
	if len(lines) != 2 || !lines[0].Warn {
		t.Fatalf("dev build should warn, got %#v", lines)
	}

	tuiVersion = "v0.4.36-20-g873af31"
	lines = checkVersionSkew()
	if len(lines) != 2 || !lines[0].Warn {
		t.Fatalf("git-describe dev stamp should warn, got %#v", lines)
	}

	tuiVersion = "v0.12.0"
	lines = checkVersionSkew()
	if len(lines) != 1 || !lines[0].OK {
		t.Fatalf("release stamp should be OK, got %#v", lines)
	}
}

func TestGitDescribeBuild(t *testing.T) {
	cases := []struct {
		version string
		want    bool
	}{
		{"v0.12.0", false},
		{"v0.4.36-20-g873af31", true},
		{"dev", false},
		{"", false},
		{"v1.2.3-0-gdeadbee", true},
	}
	for _, tc := range cases {
		if got := gitDescribeBuild(tc.version); got != tc.want {
			t.Errorf("gitDescribeBuild(%q) = %v, want %v", tc.version, got, tc.want)
		}
	}
}

// assertLineFlag finds a line containing substr and asserts its OK/Warn flags.
func assertLineFlag(t *testing.T, lines []doctorLine, substr string, wantOK, wantWarn bool) {
	t.Helper()
	for _, line := range lines {
		if strings.Contains(line.Text, substr) {
			if line.OK != wantOK || line.Warn != wantWarn {
				t.Fatalf("line %q flags = (OK=%v, Warn=%v), want (OK=%v, Warn=%v)", line.Text, line.OK, line.Warn, wantOK, wantWarn)
			}
			return
		}
	}
	t.Fatalf("no doctor line contains %q; lines: %#v", substr, lines)
}
