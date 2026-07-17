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

	if err := i18n.SetLang(lang); err != nil {
		fmt.Fprintf(os.Stderr, "invalid --lang %q: %v\n", lang, err)
		os.Exit(1)
	}

	// Resolve project directory
	if dir == "" {
		dir, _ = os.Getwd()
	}
	dir, _ = filepath.Abs(dir)
	lingtaiDir := filepath.Join(dir, ".lingtai")

	if _, err := os.Stat(lingtaiDir); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "No .lingtai/ found in %s\n", dir)
		os.Exit(1)
	}

	// Runtime project migrations are retired. Existing project files remain
	// untouched; compatibility diagnosis and repair are Agent-owned.

	// Ensure .portal/ directory exists
	portalDir := filepath.Join(lingtaiDir, ".portal")
	os.MkdirAll(portalDir, 0o755)

	// Start server and background topology recorder
	srv := api.NewServer(lingtaiDir, WebFS())
	srv.StartRecording(lingtaiDir)
	portFile := filepath.Join(portalDir, "port")
	if err := srv.Start(portFile, host, port); err != nil {
		fmt.Fprintf(os.Stderr, "error starting server: %v\n", err)
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
