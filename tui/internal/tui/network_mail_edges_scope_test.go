package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/anthropics/lingtai-tui/i18n"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

// TestTUINetworkLoadsSkipMailEdges is a source-level contract because the cost
// it guards is invisible in the resulting message: fs.BuildNetwork reads every
// message body in every agent's inbox to aggregate per-edge counts, and it grows
// with the mailbox forever. No TUI view displays those counts, so every network
// load here must ask for the mail-edge-free snapshot. The structural check is
// what makes a new view inherit the contract instead of quietly reintroducing
// the scan.
func TestTUINetworkLoadsSkipMailEdges(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		checked++
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(line, "fs.BuildNetwork(") {
				t.Errorf("%s:%d calls fs.BuildNetwork, which scans every inbox message of every agent; use fs.BuildNetworkWithOptions with SkipMailEdges: true", name, i+1)
			}
			if strings.Contains(line, "fs.BuildNetworkWithOptions(") && !strings.Contains(line, "SkipMailEdges: true") {
				t.Errorf("%s:%d builds a network without SkipMailEdges: true", name, i+1)
			}
		}
	}
	if checked == 0 {
		t.Fatal("scanned no production sources; the guard would pass vacuously")
	}
}

// TestProjectDetailShowsNoMessageCount pins the display half: even handed a
// snapshot that carries a mail total, the project summary must not print it. A
// total message number is precisely what the TUI does not compute or show.
func TestProjectDetailShowsNoMessageCount(t *testing.T) {
	m := NewAgoraProjectsModel(t.TempDir(), filepath.Join(t.TempDir(), ".lingtai"))
	m.projects = []projectEntry{{
		Path: "/tmp/demo",
		Name: "demo",
		Network: fs.Network{
			Nodes: []fs.AgentNode{{AgentName: "libai", State: "IDLE"}},
			Stats: fs.NetworkStats{Idle: 1, TotalMails: 4242},
		},
	}}
	m.cursor = 0

	out := m.renderRight(80)

	if out == "" {
		t.Fatal("project detail rendered empty; the assertions below would pass vacuously")
	}
	if !strings.Contains(out, "libai") {
		t.Fatalf("project detail lost its agent list:\n%s", out)
	}
	if strings.Contains(out, "4242") {
		t.Errorf("project detail displays a total message count:\n%s", out)
	}
	if label := i18n.T("props.total_mails"); strings.Contains(out, label) {
		t.Errorf("project detail still renders the %q section:\n%s", label, out)
	}
}
