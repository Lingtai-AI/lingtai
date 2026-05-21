package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/fs"
)

type runningTUIProcess struct {
	PID     int
	CWD     string
	Command string
}

func handleTUIUpgrade(version, latestVersion string) bool {
	fmt.Printf("lingtai-tui %s (latest: %s)\n", version, latestVersion)

	others := findOtherTUIProcesses()
	if len(others) > 0 {
		fmt.Println("  Other lingtai-tui processes are running:")
		for _, p := range others {
			if p.CWD != "" {
				fmt.Printf("    PID %d  cwd=%s\n", p.PID, p.CWD)
			} else {
				fmt.Printf("    PID %d  %s\n", p.PID, p.Command)
			}
		}
		fmt.Println("  Upgrading while they keep running can leave old/new Cellar binaries mixed.")
		fmt.Print("  Put agents in their projects to sleep, stop those TUI processes, and upgrade now? [y/N] ")
		if !answerYes(readLineLower()) {
			fmt.Println("  Upgrade skipped. Quit the other TUI windows first, then run:")
			fmt.Println("    brew update && brew upgrade lingtai-ai/lingtai/lingtai-tui")
			return false
		}
		if err := prepareOtherTUIProcessesForUpgrade(others); err != nil {
			fmt.Fprintf(os.Stderr, "  Could not prepare other TUI processes for upgrade: %v\n", err)
			fmt.Println("  Upgrade skipped. Please close them manually and try again.")
			return false
		}
	} else {
		fmt.Print("  Upgrade now? [Y/n] ")
		answer := readLineLower()
		if answer == "n" || answer == "no" {
			return false
		}
	}

	fmt.Println("  Upgrading...")
	cmd := exec.Command("brew", "upgrade", "lingtai-ai/lingtai/lingtai-tui")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "  Upgrade failed: %v\n", err)
		return false
	}

	// Verify the upgrade actually changed the binary by re-checking the
	// version. Brew returns exit 0 even for "already installed".
	postUpgrade := config.CheckTUIUpgrade(version)
	if postUpgrade != "" {
		// Still outdated — brew formula not updated yet, don't loop.
		fmt.Println("  Brew formula not yet updated. Run manually later:")
		fmt.Println("    brew update && brew upgrade lingtai-ai/lingtai/lingtai-tui")
		return false
	}

	fmt.Println("  Upgraded! Please restart lingtai-tui to use the new version:")
	fmt.Println("    lingtai-tui")
	return true
}

func readLineLower() string {
	reader := bufio.NewReader(os.Stdin)
	line, _ := reader.ReadString('\n')
	return strings.TrimSpace(strings.ToLower(line))
}

func answerYes(answer string) bool {
	return answer == "y" || answer == "yes"
}

func prepareOtherTUIProcessesForUpgrade(procs []runningTUIProcess) error {
	projects := map[string]bool{}
	for _, p := range procs {
		if projectDir := findProjectDirFromCWD(p.CWD); projectDir != "" {
			projects[projectDir] = true
		}
	}

	for projectDir := range projects {
		fmt.Printf("  Putting agents in %s to sleep...\n", projectDir)
		if err := sleepAgentsInProject(projectDir); err != nil {
			return err
		}
	}

	for _, p := range procs {
		fmt.Printf("  Stopping lingtai-tui PID %d...\n", p.PID)
		if err := stopTUIProcess(p.PID); err != nil {
			return err
		}
	}
	return nil
}

func findProjectDirFromCWD(cwd string) string {
	if cwd == "" {
		return ""
	}
	dir, err := filepath.Abs(cwd)
	if err != nil {
		dir = cwd
	}
	for {
		if info, err := os.Stat(filepath.Join(dir, ".lingtai")); err == nil && info.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}

func sleepAgentsInProject(projectDir string) error {
	lingtaiDir := filepath.Join(projectDir, ".lingtai")
	agents, err := fs.DiscoverAgents(lingtaiDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}

	var alive []string
	for _, agent := range agents {
		if agent.IsHuman {
			continue
		}
		sleepFile := filepath.Join(agent.WorkingDir, ".sleep")
		if err := os.WriteFile(sleepFile, []byte(""), 0o644); err != nil {
			return err
		}
		if !agentIsAsleep(agent.WorkingDir) {
			alive = append(alive, agent.WorkingDir)
		}
	}

	deadline := time.Now().Add(10 * time.Second)
	for len(alive) > 0 && time.Now().Before(deadline) {
		remaining := alive[:0]
		for _, dir := range alive {
			if !agentIsAsleep(dir) {
				remaining = append(remaining, dir)
			}
		}
		alive = remaining
		if len(alive) > 0 {
			time.Sleep(250 * time.Millisecond)
		}
	}
	if len(alive) > 0 {
		fmt.Printf("  Warning: %d agent(s) did not report asleep after .sleep signal.\n", len(alive))
	}
	return nil
}

func agentIsAsleep(agentDir string) bool {
	agent, err := fs.ReadAgent(agentDir)
	if err != nil {
		return true
	}
	return strings.EqualFold(agent.State, "asleep")
}
