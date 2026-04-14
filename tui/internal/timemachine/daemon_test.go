package timemachine

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestInitGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	if err := InitGit(dir); err != nil {
		t.Fatalf("InitGit failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(".git not created")
	}
	out, _ := exec.Command("git", "-C", dir, "log", "--oneline", "-1").Output()
	if !strings.Contains(string(out), "init") {
		t.Errorf("expected initial commit, got: %s", out)
	}
}

func TestInitGitIdempotent(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()

	InitGit(dir)
	out1, _ := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()
	InitGit(dir)
	out2, _ := exec.Command("git", "-C", dir, "rev-list", "--count", "HEAD").Output()

	if string(out1) != string(out2) {
		t.Error("InitGit should not create extra commits on re-run")
	}
}

func TestCommit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	InitGit(dir)

	os.WriteFile(filepath.Join(dir, "test.txt"), []byte("hello"), 0o644)

	committed, err := Commit(dir, "test commit")
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}
	if !committed {
		t.Error("expected commit")
	}

	out, _ := exec.Command("git", "-C", dir, "log", "--oneline", "-1").Output()
	if !strings.Contains(string(out), "test commit") {
		t.Errorf("expected commit message, got: %s", out)
	}
}

func TestCommitNoChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	InitGit(dir)

	committed, _ := Commit(dir, "nothing")
	if committed {
		t.Error("expected no commit when nothing changed")
	}
}

func TestScanLargeFiles(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte("# test\n"), 0o644)
	InitGit(dir)

	bigFile := filepath.Join(dir, "big.bin")
	f, _ := os.Create(bigFile)
	f.Write(make([]byte, 10*1024*1024+1))
	f.Close()

	ScanLargeFiles(dir, 10*1024*1024)

	data, _ := os.ReadFile(filepath.Join(dir, ".gitignore"))
	if !strings.Contains(string(data), "big.bin") {
		t.Error("large file not added to .gitignore")
	}
}

func TestFindOrchestrator(t *testing.T) {
	dir := t.TempDir()
	lingtaiDir := filepath.Join(dir, ".lingtai")

	orchDir := filepath.Join(lingtaiDir, "wukong")
	os.MkdirAll(orchDir, 0o755)
	os.WriteFile(filepath.Join(orchDir, ".agent.json"),
		[]byte(`{"agent_name":"wukong","admin":{"karma":true}}`), 0o644)

	otherDir := filepath.Join(lingtaiDir, "bajie")
	os.MkdirAll(otherDir, 0o755)
	os.WriteFile(filepath.Join(otherDir, ".agent.json"),
		[]byte(`{"agent_name":"bajie","admin":{"karma":false}}`), 0o644)

	humanDir := filepath.Join(lingtaiDir, "human")
	os.MkdirAll(humanDir, 0o755)
	os.WriteFile(filepath.Join(humanDir, ".agent.json"),
		[]byte(`{"agent_name":"human","admin":null}`), 0o644)

	orchPath := FindOrchestrator(lingtaiDir)
	if orchPath != orchDir {
		t.Errorf("expected %s, got %s", orchDir, orchPath)
	}
}

func TestScanInbox(t *testing.T) {
	dir := t.TempDir()
	inboxDir := filepath.Join(dir, "human", "mailbox", "inbox")

	known := make(map[string]bool)
	newMsgs := ScanInbox(dir, known)
	if len(newMsgs) != 0 {
		t.Errorf("expected 0 new messages, got %d", len(newMsgs))
	}

	msgDir := filepath.Join(inboxDir, "msg-001")
	os.MkdirAll(msgDir, 0o755)
	os.WriteFile(filepath.Join(msgDir, "message.json"),
		[]byte(`{"from":"orchestrator","subject":"hello"}`), 0o644)

	newMsgs = ScanInbox(dir, known)
	if len(newMsgs) != 1 {
		t.Fatalf("expected 1 new message, got %d", len(newMsgs))
	}
	if newMsgs[0].From != "orchestrator" || newMsgs[0].Subject != "hello" {
		t.Errorf("unexpected message: %+v", newMsgs[0])
	}

	newMsgs = ScanInbox(dir, known)
	if len(newMsgs) != 0 {
		t.Errorf("expected 0 on re-scan, got %d", len(newMsgs))
	}
}

func TestSelectKeepers(t *testing.T) {
	now := time.Now()
	var commits []CommitInfo
	for i := 199; i >= 0; i-- {
		commits = append(commits, CommitInfo{
			Hash: fmt.Sprintf("hash%03d", i),
			Time: now.Add(-time.Duration(i) * 5 * time.Minute),
		})
	}

	keepers := SelectKeepers(commits, now)

	if len(keepers) > 100 {
		t.Errorf("expected at most 100 keepers, got %d", len(keepers))
	}

	twoHoursAgo := now.Add(-2 * time.Hour)
	for _, c := range commits {
		if c.Time.After(twoHoursAgo) {
			found := false
			for _, k := range keepers {
				if k == c.Hash {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("recent commit %s should be kept", c.Hash)
			}
		}
	}
}

func TestSelectKeepersUnderCap(t *testing.T) {
	now := time.Now()
	var commits []CommitInfo
	for i := 9; i >= 0; i-- {
		commits = append(commits, CommitInfo{
			Hash: fmt.Sprintf("hash%d", i),
			Time: now.Add(-time.Duration(i) * 5 * time.Minute),
		})
	}
	keepers := SelectKeepers(commits, now)
	if len(keepers) != 10 {
		t.Errorf("expected 10 keepers, got %d", len(keepers))
	}
}

func TestRepoSizeBytes(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	dir := t.TempDir()
	InitGit(dir)

	size, err := RepoSizeBytes(dir)
	if err != nil {
		t.Fatalf("RepoSizeBytes failed: %v", err)
	}
	if size == 0 {
		t.Error("expected non-zero repo size")
	}
}
