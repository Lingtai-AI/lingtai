package api

import (
	"context"
	"fmt"
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
