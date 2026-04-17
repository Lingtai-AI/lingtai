package postman

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

const (
	DefaultPort  = 7777
	pollInterval = 3 * time.Second
	pidFileName  = ".postman.pid"
)

// IsRunning checks if a postman daemon is already running.
func IsRunning(globalDir string) bool {
	data, err := os.ReadFile(filepath.Join(globalDir, pidFileName))
	if err != nil {
		return false
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

func writePID(globalDir string) {
	os.WriteFile(filepath.Join(globalDir, pidFileName), []byte(strconv.Itoa(os.Getpid())), 0o644)
}

func removePID(globalDir string) {
	os.Remove(filepath.Join(globalDir, pidFileName))
}

// Run starts the postman daemon. Blocks until interrupted.
func Run(globalDir string, port int, watchDirs []string) {
	if IsRunning(globalDir) {
		fmt.Println("postman: already running")
		return
	}

	fmt.Printf("postman: starting on UDP port %d\n", port)
	fmt.Printf("postman: watching %d directories\n", len(watchDirs))

	writePID(globalDir)
	defer removePID(globalDir)

	stop := make(chan struct{})
	go func() {
		if err := ListenUDP(port, stop); err != nil {
			fmt.Printf("postman: listener error: %v\n", err)
		}
	}()
	defer close(stop)

	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	fmt.Println("postman: running (ctrl-c to stop)")

	for range ticker.C {
		for _, dir := range watchDirs {
			scanAndSend(dir, port)
		}
	}
}

func scanAndSend(lingtaiDir string, port int) {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		agentDir := filepath.Join(lingtaiDir, e.Name())
		items := ScanOutbox(agentDir)
		for _, item := range items {
			if err := SendUDP(item.PeerAddr, port, item.Data); err != nil {
				fmt.Printf("postman: send to %s failed: %v\n", item.PeerAddr, err)
				continue
			}
			CleanOutboxItem(item.AgentDir, item.ID)
		}
	}
}
