package api

import (
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestServerStartDefaultsToLoopbackAndKeepsPortFilePortOnly(t *testing.T) {
	dir := t.TempDir()
	portFile := filepath.Join(dir, "port")
	srv := NewServer(dir, nil)
	if err := srv.Start(portFile, "", 0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	if got := srv.Host(); got != defaultHost {
		t.Fatalf("Host() = %q, want %q", got, defaultHost)
	}
	if got, want := srv.URL(), fmt.Sprintf("http://localhost:%d", srv.Port()); got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}

	data, err := os.ReadFile(portFile)
	if err != nil {
		t.Fatalf("read port file: %v", err)
	}
	portText := strings.TrimSpace(string(data))
	if _, err := strconv.Atoi(portText); err != nil {
		t.Fatalf("port file = %q, want decimal port only: %v", portText, err)
	}
	if portText != strconv.Itoa(srv.Port()) {
		t.Fatalf("port file = %q, want %d", portText, srv.Port())
	}
	if strings.Contains(portText, ":") {
		t.Fatalf("port file = %q, want port only without host", portText)
	}
}

func TestServerStartFailsWhenPortFileCannotBeWritten(t *testing.T) {
	dir := t.TempDir()
	// Occupy the port path with a directory so the port-file write fails
	// deterministically on every platform.
	portFile := filepath.Join(dir, "port")
	if err := os.Mkdir(portFile, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", portFile, err)
	}

	srv := NewServer(dir, nil)
	err := srv.Start(portFile, "", 0)
	if err == nil {
		t.Fatal("Start succeeded, want error because the port file could not be written")
	}
	if !strings.Contains(err.Error(), "publish discovery port") {
		t.Fatalf("Start error %q should identify the discovery-port publication failure", err)
	}

	// A failed start must leave no active listener: the previously bound
	// port is released and can be rebound immediately.
	ln, err := net.Listen("tcp", net.JoinHostPort(defaultHost, strconv.Itoa(srv.Port())))
	if err != nil {
		t.Fatalf("port %d still bound after failed Start: %v", srv.Port(), err)
	}
	ln.Close()

	// A failed start must leave no background recorder: Stop must not block.
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("Stop after failed Start: %v", err)
	}
}

func TestServerStartWildcardHostKeepsLocalhostURL(t *testing.T) {
	srv := NewServer(t.TempDir(), nil)
	if err := srv.Start("", "0.0.0.0", 0); err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Stop(context.Background())

	if got := srv.Host(); got != "0.0.0.0" {
		t.Fatalf("Host() = %q, want 0.0.0.0", got)
	}
	if got, want := srv.URL(), fmt.Sprintf("http://localhost:%d", srv.Port()); got != want {
		t.Fatalf("URL() = %q, want %q", got, want)
	}
}

func TestServerURLUsesExplicitNamedHosts(t *testing.T) {
	for _, tc := range []struct {
		name string
		host string
		want string
	}{
		{name: "dns", host: "portal.example.test", want: "http://portal.example.test:4321"},
		{name: "ipv4", host: "192.0.2.10", want: "http://192.0.2.10:4321"},
		{name: "ipv6", host: "2001:db8::1", want: "http://[2001:db8::1]:4321"},
		{name: "loopback_ipv6", host: "::1", want: "http://localhost:4321"},
		{name: "wildcard_ipv6", host: "::", want: "http://localhost:4321"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := &Server{host: tc.host, port: 4321}
			if got := srv.URL(); got != tc.want {
				t.Fatalf("URL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestHostWarningsOnlyForExternalOrWildcardBinds(t *testing.T) {
	for _, tc := range []struct {
		host string
		warn bool
	}{
		{host: "", warn: false},
		{host: "127.0.0.1", warn: false},
		{host: "::1", warn: false},
		{host: "localhost", warn: false},
		{host: "0.0.0.0", warn: true},
		{host: "::", warn: true},
		{host: "192.0.2.10", warn: true},
		{host: "portal.example.test", warn: true},
	} {
		t.Run(tc.host, func(t *testing.T) {
			got := HostRequiresWarning(tc.host)
			if got != tc.warn {
				t.Fatalf("HostRequiresWarning(%q) = %v, want %v", tc.host, got, tc.warn)
			}
		})
	}

	srv := &Server{host: "0.0.0.0"}
	warning := srv.ExternalAccessWarning()
	if !strings.Contains(warning, "unauthenticated") || !strings.Contains(warning, "trusted LAN") {
		t.Fatalf("warning %q should mention unauthenticated trusted-LAN exposure", warning)
	}
}

func writeTapeFile(t *testing.T, path string, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestNeedsReconstruction_TailInspection proves the startup format check
// inspects the final tape line and keeps its historical semantics: missing or
// empty tapes need reconstruction, new-format lines (mail edge with direct)
// do not, and old-format lines (mail edge without direct) do.
func TestNeedsReconstruction_TailInspection(t *testing.T) {
	const newFormat = "{\"t\":1000,\"net\":{\"nodes\":[],\"avatar_edges\":[],\"contact_edges\":[],\"mail_edges\":[{\"sender\":\"a\",\"recipient\":\"b\",\"count\":1,\"direct\":1,\"cc\":0,\"bcc\":0}],\"stats\":{}}}\n"
	const oldFormat = "{\"t\":1000,\"net\":{\"nodes\":[],\"avatar_edges\":[],\"contact_edges\":[],\"mail_edges\":[{\"sender\":\"a\",\"recipient\":\"b\",\"count\":1}],\"stats\":{}}}\n"
	const noMailFormat = "{\"t\":1000,\"net\":{\"nodes\":[],\"avatar_edges\":[],\"contact_edges\":[],\"mail_edges\":[],\"stats\":{}}}\n"

	t.Run("missing tape", func(t *testing.T) {
		if !needsReconstruction(filepath.Join(t.TempDir(), "topology.jsonl")) {
			t.Fatal("missing tape should need reconstruction")
		}
	})

	t.Run("empty tape", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".portal", "topology.jsonl")
		writeTapeFile(t, path, "")
		if !needsReconstruction(path) {
			t.Fatal("empty tape should need reconstruction")
		}
	})

	t.Run("new format", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".portal", "topology.jsonl")
		writeTapeFile(t, path, newFormat)
		if needsReconstruction(path) {
			t.Fatal("new-format tape should not need reconstruction")
		}
	})

	t.Run("old format", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".portal", "topology.jsonl")
		writeTapeFile(t, path, oldFormat)
		if !needsReconstruction(path) {
			t.Fatal("old-format tape should need reconstruction")
		}
	})

	t.Run("new format after many old lines", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".portal", "topology.jsonl")
		var buf strings.Builder
		for i := 0; i < 5000; i++ {
			buf.WriteString(oldFormat)
		}
		buf.WriteString(newFormat)
		writeTapeFile(t, path, buf.String())
		if needsReconstruction(path) {
			t.Fatal("the final new-format line should win over older old-format lines")
		}
	})

	t.Run("no mail edges", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), ".portal", "topology.jsonl")
		writeTapeFile(t, path, noMailFormat)
		if needsReconstruction(path) {
			t.Fatal("tape without mail edges should not need reconstruction")
		}
	})
}
