package timemachine

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// CommitInfo holds a commit hash and its author time.
type CommitInfo struct {
	Hash string
	Time time.Time
}

const maxSnapshots = 100

// SelectKeepers decides which commits to retain based on retention buckets.
// Input: oldest-first. Output: kept hashes oldest-first.
func SelectKeepers(commits []CommitInfo, now time.Time) []string {
	if len(commits) == 0 {
		return nil
	}

	kept := make(map[string]bool)
	windows := make(map[string]bool)

	kept[commits[0].Hash] = true
	kept[commits[len(commits)-1].Hash] = true

	for i := len(commits) - 1; i >= 0; i-- {
		c := commits[i]
		age := now.Sub(c.Time)
		var interval time.Duration

		switch {
		case age <= 2*time.Hour:
			interval = 0
		case age <= 24*time.Hour:
			interval = 30 * time.Minute
		case age <= 7*24*time.Hour:
			interval = 6 * time.Hour
		default:
			interval = 24 * time.Hour
		}

		if interval == 0 {
			kept[c.Hash] = true
			continue
		}

		window := c.Time.Truncate(interval)
		windowKey := fmt.Sprintf("%d-%s", interval, window.Format(time.RFC3339))
		if !windows[windowKey] {
			windows[windowKey] = true
			kept[c.Hash] = true
		}
	}

	var result []string
	for _, c := range commits {
		if kept[c.Hash] {
			result = append(result, c.Hash)
		}
	}

	if len(result) > maxSnapshots {
		result = result[len(result)-maxSnapshots:]
	}
	return result
}

// ListCommits returns all commits oldest-first.
func ListCommits(dir string) ([]CommitInfo, error) {
	cmd := exec.Command("git", "log", "--format=%H %aI", "--reverse")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}

	var commits []CommitInfo
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, " ", 2)
		if len(parts) != 2 {
			continue
		}
		t, err := time.Parse(time.RFC3339, parts[1])
		if err != nil {
			continue
		}
		commits = append(commits, CommitInfo{Hash: parts[0], Time: t})
	}
	return commits, nil
}

// ThinHistory rebuilds the commit chain keeping only the listed hashes.
func ThinHistory(dir string, keepers []string) error {
	if len(keepers) == 0 {
		return nil
	}

	keepSet := make(map[string]bool, len(keepers))
	for _, h := range keepers {
		keepSet[h] = true
	}

	all, err := ListCommits(dir)
	if err != nil {
		return err
	}

	removeCount := 0
	for _, c := range all {
		if !keepSet[c.Hash] {
			removeCount++
		}
	}
	if removeCount == 0 {
		return nil
	}

	branchCmd := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD")
	branchCmd.Dir = dir
	branchOut, err := branchCmd.Output()
	if err != nil {
		return fmt.Errorf("detect branch: %w", err)
	}
	origBranch := strings.TrimSpace(string(branchOut))
	if origBranch == "" || origBranch == "HEAD" {
		origBranch = "main"
	}

	if err := Git(dir, "checkout", "--orphan", "tm-rebuild"); err != nil {
		return fmt.Errorf("checkout orphan: %w", err)
	}
	Git(dir, "rm", "-rf", "--cached", ".")

	replayErr := func() error {
		for _, hash := range keepers {
			if err := Git(dir, "checkout", hash, "--", "."); err != nil {
				return fmt.Errorf("checkout tree %s: %w", hash, err)
			}
			if err := Git(dir, "add", "-A"); err != nil {
				return fmt.Errorf("add: %w", err)
			}

			cmd := exec.Command("git", "log", "-1", "--format=%s%n%aI", hash)
			cmd.Dir = dir
			msgOut, err := cmd.Output()
			if err != nil {
				return fmt.Errorf("get message %s: %w", hash, err)
			}
			lines := strings.SplitN(strings.TrimSpace(string(msgOut)), "\n", 2)
			msg := lines[0]
			if msg == "" {
				msg = "snapshot"
			}

			commitCmd := exec.Command("git", "commit", "--allow-empty", "-m", msg)
			commitCmd.Dir = dir
			if len(lines) > 1 {
				commitCmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+lines[1])
			}
			if err := commitCmd.Run(); err != nil {
				return fmt.Errorf("commit replay %s: %w", hash, err)
			}
		}
		return nil
	}()

	if replayErr != nil {
		Git(dir, "checkout", origBranch)
		Git(dir, "branch", "-D", "tm-rebuild")
		return replayErr
	}

	Git(dir, "branch", "-D", origBranch)
	Git(dir, "branch", "-M", "tm-rebuild", origBranch)
	Git(dir, "gc", "--prune=now")
	return nil
}

// RepoSizeBytes returns the total size of .git/ in bytes.
func RepoSizeBytes(dir string) (int64, error) {
	cmd := exec.Command("git", "count-objects", "-v")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("git count-objects: %w", err)
	}

	var total int64
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "size:") || strings.HasPrefix(line, "size-pack:") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				val, err := strconv.ParseInt(strings.TrimSpace(parts[1]), 10, 64)
				if err == nil {
					total += val * 1024
				}
			}
		}
	}
	return total, nil
}

// EnforceSizeCap halves snapshots until repo is under maxSize (max 5 rounds).
func EnforceSizeCap(dir string, maxSize int64) {
	for round := 0; round < 5; round++ {
		Git(dir, "gc", "--aggressive", "--prune=now")

		size, err := RepoSizeBytes(dir)
		if err != nil || size <= maxSize {
			return
		}

		commits, err := ListCommits(dir)
		if err != nil || len(commits) <= 10 {
			return
		}

		var keepers []string
		for i, c := range commits {
			if i == 0 || i == len(commits)-1 || i%2 == 0 {
				keepers = append(keepers, c.Hash)
			}
		}

		if err := ThinHistory(dir, keepers); err != nil {
			return
		}
	}
}
