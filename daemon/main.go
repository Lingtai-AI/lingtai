package main

import (
	"fmt"
	"os"
	"path/filepath"

	"lingtai-daemon/internal/agent"
	"lingtai-daemon/internal/config"
	"lingtai-daemon/internal/i18n"
	"lingtai-daemon/internal/setup"
	"lingtai-daemon/internal/tui"
)

func main() {
	args := os.Args[1:]

	// Parse flags
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--lang":
			if i+1 < len(args) {
				i18n.Lang = args[i+1]
				i++
			}
		default:
			positional = append(positional, args[i])
		}
	}

	cwd, _ := os.Getwd()
	lingtaiDir := filepath.Join(cwd, ".lingtai")

	// lingtai setup — (re)configure current directory
	if len(positional) > 0 {
		switch positional[0] {
		case "setup":
			os.MkdirAll(lingtaiDir, 0755)
			if err := setup.Run(lingtaiDir); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			return
		case "help", "--help", "-h":
			printHelp()
			return
		}
	}

	// Default: check cwd for .lingtai/
	configPath := filepath.Join(lingtaiDir, "configs", "config.json")

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		// No .lingtai/ — run setup wizard
		fmt.Printf("\n  \033[1m\033[36m灵台\033[0m  No .lingtai/ found — starting setup.\n\n")
		os.MkdirAll(lingtaiDir, 0755)
		if err := setup.Run(lingtaiDir); err != nil {
			fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
			os.Exit(1)
		}
		// After setup, fall through to start agent
	}

	// .lingtai/ exists — load config, start agent, open chat TUI
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError loading config: %v\033[0m\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n  \033[1m\033[36m灵台\033[0m  Starting agent...\n")
	proc, err := agent.Start(agent.StartOptions{
		ConfigPath: configPath,
		AgentPort:  cfg.AgentPort,
		WorkingDir: cfg.WorkingDir(),
		Headless:   true,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError starting agent: %v\033[0m\n", err)
		os.Exit(1)
	}

	tui.Run(cfg, proc)

	// TUI exited — stop agent
	proc.Stop()
}

func printHelp() {
	fmt.Printf(`
  灵台 LingTai — agent framework

  Usage:
    lingtai              Start agent in current directory (setup if needed)
    lingtai setup        (Re)configure current directory

  Flags:
    --lang <code>        UI language (en, zh, lzh)

  Run lingtai in any directory. It uses .lingtai/ in the current
  directory — like git uses .git/.

  Provider configs are saved as "combos" at ~/.lingtai/combos/
  for reuse across projects.

`)
}
