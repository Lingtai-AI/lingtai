package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"

	"github.com/anthropics/lingtai-portal/i18n"
	"github.com/anthropics/lingtai-portal/internal/api"
)

// version is set at build time via -ldflags "-X main.version=v0.4.2"
var version = "dev"

func main() {
	// Handle version flag before flag.Parse so `version` as a positional
	// subcommand does not trip the flag parser. Matches tui/main.go.
	if len(os.Args) > 1 {
		arg := os.Args[1]
		if arg == "--version" || arg == "-v" || arg == "version" {
			fmt.Println("lingtai-portal " + version)
			os.Exit(0)
		}
	}

	var dir string
	var host string
	var port int
	var open bool
	var lang string

	flag.StringVar(&dir, "dir", "", "Path to project directory (default: current directory)")
	flag.StringVar(&host, "host", "", "Host to bind (default: 127.0.0.1)")
	flag.IntVar(&port, "port", 0, "Fixed port (default: random)")
	flag.BoolVar(&open, "open", false, "Open browser after starting")
	flag.StringVar(&lang, "lang", "en", "Language (en, zh, wen)")
	flag.Parse()

	// Start the portal. Discovery publication (directory + bound port) and
	// the listener/recorder lifecycle are handled atomically by startPortal:
	// either it returns a serving server, or an error with no active listener
	// or background recorder behind it, and the CLI exits nonzero.
	srv, lingtaiDir, err := startPortal(dir, host, port, lang)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error starting portal: %v\n", err)
		os.Exit(1)
	}
	if warning := srv.ExternalAccessWarning(); warning != "" {
		fmt.Fprintln(os.Stderr, warning)
	}

	fmt.Printf("lingtai-portal serving %s\n", lingtaiDir)
	fmt.Printf("  %s\n", srv.URL())

	if open {
		openBrowser(srv.URL())
	}

	// Block until signal
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	<-sigCh

	fmt.Println("\nShutting down...")
	srv.Stop(context.Background())
}

// startPortal validates the project directory, publishes the discovery
// directory and the bound port, and returns a started server with its
// background topology recorder running. It has a single observable startup
// outcome: either the discovery directory and bound port are successfully
// published and the server is serving, or it returns an error and no active
// listener or background recorder remains from the failed start.
func startPortal(dir, host string, port int, lang string) (*api.Server, string, error) {
	if err := i18n.SetLang(lang); err != nil {
		return nil, "", fmt.Errorf("invalid --lang %q: %w", lang, err)
	}

	// Resolve project directory
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dir, _ = filepath.Abs(dir)
	lingtaiDir := filepath.Join(dir, ".lingtai")

	if _, err := os.Stat(lingtaiDir); os.IsNotExist(err) {
		return nil, "", fmt.Errorf("no .lingtai/ found in %s", dir)
	}

	// Runtime project migrations are retired. Existing project files remain
	// untouched; compatibility diagnosis and repair are Agent-owned.

	// Publish the discovery directory before any listener starts; a failure
	// here must surface instead of being discarded.
	portalDir := filepath.Join(lingtaiDir, ".portal")
	if err := os.MkdirAll(portalDir, 0o755); err != nil {
		return nil, "", fmt.Errorf("creating discovery directory %s: %w", portalDir, err)
	}

	// Bind, publish the bound port, then serve. Start releases the listener
	// and returns an error if the port file cannot be written, so a failed
	// start leaves no active listener behind.
	srv := api.NewServer(lingtaiDir, WebFS())
	portFile := filepath.Join(portalDir, "port")
	if err := srv.Start(portFile, host, port); err != nil {
		return nil, "", err
	}

	// Begin the background topology recorder only after discovery
	// publication succeeded.
	srv.StartRecording(lingtaiDir)

	return srv, lingtaiDir, nil
}

func openBrowser(url string) {
	if url == "" {
		return
	}
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		cmd = "open"
		args = []string{url}
	case "linux":
		if isWSL() {
			if path, err := exec.LookPath("wslview"); err == nil {
				cmd = path
				args = []string{url}
			} else {
				cmd = "powershell.exe"
				args = []string{"-NoProfile", "-Command", "Start-Process", "'" + url + "'"}
			}
		} else {
			cmd = "xdg-open"
			args = []string{url}
		}
	case "windows":
		cmd = "rundll32"
		args = []string{"url.dll,FileProtocolHandler", url}
	}
	if cmd != "" {
		exec.Command(cmd, args...).Start()
	}
}

func isWSL() bool {
	b, err := os.ReadFile("/proc/version")
	if err != nil {
		return false
	}
	s := strings.ToLower(string(b))
	return strings.Contains(s, "microsoft") || strings.Contains(s, "wsl")
}
