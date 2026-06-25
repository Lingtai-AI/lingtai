package tui

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/anthropics/lingtai-tui/internal/fs"
	"github.com/anthropics/lingtai-tui/internal/sqlitelog"
)

const clearSourceTUI = "tui"

type clearDoneMsg struct {
	completed bool
	err       error
}

type clearWaitConfig struct {
	aliveTimeout      time.Duration
	completionTimeout time.Duration
	suspendTimeout    time.Duration
	pollInterval      time.Duration
}

type clearContextDeps struct {
	isAlive func(string, float64) bool
	revive  func(string, string) error
	sleep   func(time.Duration)
}

var defaultClearWaitConfig = clearWaitConfig{
	aliveTimeout:      10 * time.Second,
	completionTimeout: 30 * time.Second,
	suspendTimeout:    10 * time.Second,
	pollInterval:      200 * time.Millisecond,
}

var defaultClearContextDeps = clearContextDeps{
	isAlive: fs.IsAlive,
	revive:  reviveDir,
	sleep:   time.Sleep,
}

func requestClearContext(lingtaiCmd, dir string) (completed bool, err error) {
	return requestClearContextWithDeps(lingtaiCmd, dir, defaultClearWaitConfig, defaultClearContextDeps)
}

func requestClearContextWithDeps(lingtaiCmd, dir string, cfg clearWaitConfig, deps clearContextDeps) (completed bool, err error) {
	deps = normalizeClearContextDeps(deps)
	cfg = normalizeClearWaitConfig(cfg)

	if deps.isAlive(dir, 3.0) {
		return false, writeClearSignal(dir)
	}

	if err := deps.revive(lingtaiCmd, dir); err != nil {
		return false, err
	}
	if err := waitForAgentAlive(dir, cfg.aliveTimeout, cfg.pollInterval, deps); err != nil {
		_ = suspendTemporaryClearAgent(dir, cfg, deps)
		return false, err
	}

	beforeMoltCount, hasBeforeMoltCount := readMoltCount(dir)
	eventsOffset := eventLogOffset(dir)

	if err := writeClearSignal(dir); err != nil {
		_ = suspendTemporaryClearAgent(dir, cfg, deps)
		return false, err
	}

	waitErr := waitForClearCompletion(dir, beforeMoltCount, hasBeforeMoltCount, eventsOffset, cfg, deps)
	suspendErr := suspendTemporaryClearAgent(dir, cfg, deps)
	if waitErr != nil {
		return false, waitErr
	}
	if suspendErr != nil {
		return true, suspendErr
	}
	return true, nil
}

func normalizeClearContextDeps(deps clearContextDeps) clearContextDeps {
	if deps.isAlive == nil {
		deps.isAlive = fs.IsAlive
	}
	if deps.revive == nil {
		deps.revive = reviveDir
	}
	if deps.sleep == nil {
		deps.sleep = time.Sleep
	}
	return deps
}

func normalizeClearWaitConfig(cfg clearWaitConfig) clearWaitConfig {
	if cfg.aliveTimeout <= 0 {
		cfg.aliveTimeout = defaultClearWaitConfig.aliveTimeout
	}
	if cfg.completionTimeout <= 0 {
		cfg.completionTimeout = defaultClearWaitConfig.completionTimeout
	}
	if cfg.suspendTimeout <= 0 {
		cfg.suspendTimeout = defaultClearWaitConfig.suspendTimeout
	}
	if cfg.pollInterval <= 0 {
		cfg.pollInterval = defaultClearWaitConfig.pollInterval
	}
	return cfg
}

func writeClearSignal(dir string) error {
	return os.WriteFile(filepath.Join(dir, ".clear"), []byte(clearSourceTUI+"\n"), 0o644)
}

func waitForAgentAlive(dir string, timeout, pollInterval time.Duration, deps clearContextDeps) error {
	deadline := time.Now().Add(timeout)
	for {
		if deps.isAlive(dir, 3.0) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent did not become alive after revive")
		}
		deps.sleep(pollInterval)
	}
}

func suspendTemporaryClearAgent(dir string, cfg clearWaitConfig, deps clearContextDeps) error {
	if err := os.WriteFile(filepath.Join(dir, ".suspend"), []byte(""), 0o644); err != nil {
		return err
	}
	deadline := time.Now().Add(cfg.suspendTimeout)
	for {
		if !deps.isAlive(dir, 3.0) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("agent did not suspend after clear")
		}
		deps.sleep(cfg.pollInterval)
	}
}

func waitForClearCompletion(dir string, beforeMoltCount int, hasBeforeMoltCount bool, eventsOffset int64, cfg clearWaitConfig, deps clearContextDeps) error {
	deadline := time.Now().Add(cfg.completionTimeout)
	for {
		if hasBeforeMoltCount {
			if moltCount, ok := readMoltCount(dir); ok && moltCount > beforeMoltCount {
				return nil
			}
		}
		if hasTUIClearCompletionEvent(dir, eventsOffset) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("timed out waiting for agent clear completion")
		}
		deps.sleep(cfg.pollInterval)
	}
}

func readMoltCount(dir string) (int, bool) {
	data, err := os.ReadFile(filepath.Join(dir, ".agent.json"))
	if err != nil {
		return 0, false
	}
	var manifest map[string]interface{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		return 0, false
	}
	switch v := manifest["molt_count"].(type) {
	case float64:
		return int(v), true
	case int:
		return v, true
	case int64:
		return int(v), true
	case json.Number:
		i, err := v.Int64()
		return int(i), err == nil
	default:
		return 0, false
	}
}

func eventLogOffset(dir string) int64 {
	info, err := os.Stat(filepath.Join(dir, "logs", "events.jsonl"))
	if err != nil {
		return 0
	}
	return info.Size()
}

func hasTUIClearCompletionEvent(dir string, offset int64) bool {
	if ok, err := sqlitelog.HasTUIClearCompletionEvent(dir, offset); err == nil && ok {
		return true
	}

	path := filepath.Join(dir, "logs", "events.jsonl")
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return false
	}
	if offset < 0 || offset > info.Size() {
		offset = 0
	}
	if _, err := f.Seek(offset, io.SeekStart); err != nil {
		return false
	}
	reader := bufio.NewReader(f)
	for {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSpace(line)
			if len(line) > 0 {
				var event map[string]interface{}
				if err := json.Unmarshal(line, &event); err == nil {
					eventType, _ := event["type"].(string)
					source, _ := event["source"].(string)
					if source == clearSourceTUI && (eventType == "psyche_molt" || eventType == "clear_received") {
						return true
					}
				}
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			break
		}
	}
	return false
}
