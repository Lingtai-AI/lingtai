package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"lingtai-daemon/internal/config"
	"lingtai-daemon/internal/i18n"
	"lingtai-daemon/internal/manage"
	"lingtai-daemon/internal/setup"
)

func main() {
	home, _ := os.UserHomeDir()
	configDir := filepath.Join(home, ".lingtai")
	configPath := filepath.Join(configDir, "config.json")

	args := os.Args[1:]

	// Parse flags
	var positional []string
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--config":
			if i+1 < len(args) {
				configPath = args[i+1]
				configDir = filepath.Dir(configPath)
				i++
			}
		case "--lang":
			if i+1 < len(args) {
				i18n.Lang = args[i+1]
				i++
			}
		default:
			positional = append(positional, args[i])
		}
	}

	// Subcommands
	if len(positional) > 0 {
		switch positional[0] {
		case "setup":
			os.MkdirAll(configDir, 0755)
			if err := setup.Run(configDir); err != nil {
				fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
				os.Exit(1)
			}
			return
		case "manage":
			baseDir := configDir
			for i, arg := range args {
				if arg == "--base-dir" && i+1 < len(args) {
					baseDir = args[i+1]
				}
			}
			if strings.HasPrefix(baseDir, "~") {
				baseDir = filepath.Join(home, baseDir[1:])
			}
			spirits := manage.ScanSpirits(baseDir)
			fmt.Print(manage.FormatTable(spirits))
			return
		case "help", "--help", "-h":
			printHelp()
			return
		}
	}

	// No subcommand — show config or run setup if missing
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		fmt.Printf("\n  \033[1m\033[36m灵台\033[0m  No config found — starting setup wizard.\n\n")
		os.MkdirAll(configDir, 0755)
		if err := setup.Run(configDir); err != nil {
			fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
			os.Exit(1)
		}
		fmt.Println()
		return
	}

	// Config exists — load and display
	cfg, err := config.Load(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError: %v\033[0m\n", err)
		os.Exit(1)
	}

	printConfig(cfg, configPath)
}

func printConfig(cfg *config.Config, configPath string) {
	fmt.Printf("\n  \033[1m\033[36m灵台 LingTai\033[0m\n\n")
	fmt.Printf("  \033[1mConfig:\033[0m     %s\n", configPath)
	fmt.Printf("  \033[1mProvider:\033[0m   %s\n", cfg.Model.Provider)
	fmt.Printf("  \033[1mModel:\033[0m      %s\n", cfg.Model.Model)
	fmt.Printf("  \033[1mBase dir:\033[0m   %s\n", cfg.BaseDir)
	fmt.Printf("  \033[1mAgent port:\033[0m %d\n", cfg.AgentPort)

	if cfg.IMAP != nil {
		addr, _ := cfg.IMAP["email_address"].(string)
		fmt.Printf("  \033[1mIMAP:\033[0m       \033[32m● %s\033[0m\n", addr)
	}
	if cfg.Telegram != nil {
		fmt.Printf("  \033[1mTelegram:\033[0m   \033[32m● enabled\033[0m\n")
	}

	fmt.Printf("\n  \033[2mRun 'lingtai setup' to reconfigure.\033[0m\n\n")
}

func printHelp() {
	fmt.Printf(`
  灵台 LingTai — agent framework configuration

  Usage:
    lingtai              Show config (or run setup if none exists)
    lingtai setup        Run the setup wizard
    lingtai manage       List running agents

  Flags:
    --config <path>      Use a specific config file (default: ~/.lingtai/config.json)
    --lang <code>        Language (en, zh)

  Config is stored at ~/.lingtai/config.json by default.
  Other tools (lingtai-fangcun, custom apps) read this config automatically.

`)
}
