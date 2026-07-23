package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/anthropics/lingtai-tui/internal/fs"
)

type reviewBlockerRefreshFixture struct {
	mail     MailModel
	target   fs.DirectTarget
	accepted []fs.MailMessage
}

func newReviewBlockerRefreshFixture(t *testing.T) reviewBlockerRefreshFixture {
	t.Helper()
	root := t.TempDir()
	lingtaiDir := filepath.Join(root, ".lingtai")
	humanDir := filepath.Join(lingtaiDir, "human")
	target := fs.DirectTarget{
		ProjectDirectory: root,
		Directory:        filepath.Join(lingtaiDir, "agent-a"),
		AgentID:          "agent-a",
		Address:          directPerformanceHuman + "/agent-a",
	}
	directPerformanceWriteManifest(t, humanDir, "human", "Human", directPerformanceHuman, true)
	directPerformanceWriteManifest(t, target.Directory, target.AgentID, "Alpha", target.Address, false)

	accepted := []fs.MailMessage{directPerformanceIncoming(target, 1, "accepted refresh FIFO probe")}
	mail := NewMailModel(
		humanDir,
		directPerformanceHuman,
		lingtaiDir,
		"",
		"Main",
		10,
		"",
		"en",
		false,
		0,
	)
	mail.generation = 41
	mail.cache = fs.NewMailCache(humanDir)
	mail.cache.Messages = append([]fs.MailMessage(nil), accepted...)
	mail, _ = mail.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	return reviewBlockerRefreshFixture{mail: mail, target: target, accepted: accepted}
}

func reviewBlockerRealRefresh(t *testing.T, mail MailModel) tea.Msg {
	t.Helper()
	msg := mail.refreshMail()
	refresh, ok := msg.(mailRefreshMsg)
	if !ok {
		t.Fatalf("real refreshMail produced %T, want mailRefreshMsg", msg)
	}
	if refresh.generation != mail.generation {
		t.Fatalf("real refreshMail generation = %d, want current %d", refresh.generation, mail.generation)
	}
	return msg
}

func reviewBlockerReplaceWithFIFO(t *testing.T, path string) []byte {
	t.Helper()
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read normal source %q before FIFO replacement: %v", path, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove normal source %q before FIFO replacement: %v", path, err)
	}
	cmd := exec.Command("mkfifo", path)
	if err := cmd.Run(); err != nil {
		if _, missing := err.(*exec.Error); missing {
			t.Skipf("mkfifo is unavailable: %v", err)
		}
		t.Fatalf("mkfifo %q: %v", path, err)
	}
	return original
}

func reviewBlockerRestoreRegularSource(t *testing.T, path string, original []byte) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatalf("remove released FIFO %q: %v", path, err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatalf("restore regular source %q after FIFO release: %v", path, err)
	}
}

// reviewBlockerRequireRefreshUpdateReturns races Update against a writer blocked
// opening the substituted FIFO. If Update opens that source, the writer handshake
// completes before it supplies the original valid bytes, proving synchronous I/O
// without relying on a fast-machine latency threshold. When Update returns first,
// a test-owned reader releases and joins the otherwise blocked cleanup writer.
func reviewBlockerRequireRefreshUpdateReturns(t *testing.T, mail MailModel, msg tea.Msg, fifoPath string, original []byte, source string) {
	t.Helper()
	writerReady := make(chan error, 1)
	writerDone := make(chan error, 1)
	go func() {
		fifo, err := os.OpenFile(fifoPath, os.O_WRONLY, 0o600)
		if err != nil {
			writerReady <- fmt.Errorf("open FIFO writer: %w", err)
			return
		}
		writerReady <- nil
		if _, err := fifo.Write(original); err != nil {
			_ = fifo.Close()
			writerDone <- fmt.Errorf("write FIFO source: %w", err)
			return
		}
		if err := fifo.Close(); err != nil {
			writerDone <- fmt.Errorf("close FIFO writer: %w", err)
			return
		}
		writerDone <- nil
	}()

	updateDone := make(chan struct{})
	go func() {
		_, _ = mail.Update(msg)
		close(updateDone)
	}()

	readerObserved := false
	writerReadyConsumed := false
	select {
	case err := <-writerReady:
		writerReadyConsumed = true
		if err != nil {
			t.Fatalf("prepare %s FIFO writer: %v", source, err)
		}
		readerObserved = true
	case <-updateDone:
		// Update may have returned immediately after consuming the FIFO. The writer
		// announces its successful open before it writes, so this nonblocking check
		// still detects that forbidden read deterministically.
		select {
		case err := <-writerReady:
			writerReadyConsumed = true
			if err != nil {
				t.Fatalf("prepare %s FIFO writer: %v", source, err)
			}
			readerObserved = true
		default:
		}
	case <-time.After(5 * time.Second):
		// Neither the Update nor a FIFO reader made progress. Release the cleanup
		// writer, restore the path, and join Update before reporting the hang.
		reader, err := os.OpenFile(fifoPath, os.O_RDONLY, 0o600)
		if err != nil {
			t.Fatalf("open %s FIFO cleanup reader after timeout: %v", source, err)
		}
		if !writerReadyConsumed {
			if err := <-writerReady; err != nil {
				_ = reader.Close()
				t.Fatalf("prepare %s FIFO writer after timeout: %v", source, err)
			}
		}
		if err := <-writerDone; err != nil {
			_ = reader.Close()
			t.Fatalf("release %s FIFO writer after timeout: %v", source, err)
		}
		if err := reader.Close(); err != nil {
			t.Fatalf("close %s FIFO cleanup reader after timeout: %v", source, err)
		}
		reviewBlockerRestoreRegularSource(t, fifoPath, original)
		select {
		case <-updateDone:
		case <-time.After(5 * time.Second):
			t.Fatalf("accepted real refresh Update did not join after restoring timed-out %s source", source)
		}
		t.Fatalf("accepted real refresh Update neither returned nor opened the %s FIFO within 5s", source)
	}

	if readerObserved {
		if err := <-writerDone; err != nil {
			t.Fatalf("release observed %s FIFO reader: %v", source, err)
		}
		select {
		case <-updateDone:
		case <-time.After(5 * time.Second):
			t.Fatalf("accepted real refresh Update did not join within 5s after releasing observed %s FIFO", source)
		}
		reviewBlockerRestoreRegularSource(t, fifoPath, original)
		t.Errorf("accepted real refresh Update synchronously opened the %s FIFO", source)
		return
	}

	// Green path: Update returned without opening the source. Pair a test-owned
	// reader with the blocked writer solely to clean up and join that goroutine.
	reader, err := os.OpenFile(fifoPath, os.O_RDONLY, 0o600)
	if err != nil {
		t.Fatalf("open %s FIFO cleanup reader: %v", source, err)
	}
	if !writerReadyConsumed {
		if err := <-writerReady; err != nil {
			_ = reader.Close()
			t.Fatalf("prepare %s FIFO cleanup writer: %v", source, err)
		}
	}
	if err := <-writerDone; err != nil {
		_ = reader.Close()
		t.Fatalf("join %s FIFO cleanup writer: %v", source, err)
	}
	if err := reader.Close(); err != nil {
		t.Fatalf("close %s FIFO cleanup reader: %v", source, err)
	}
	reviewBlockerRestoreRegularSource(t, fifoPath, original)
}

func TestAcceptedRealRefreshUpdateDoesNotPerformManifestOrDirectUnreadIO(t *testing.T) {
	t.Run("target manifest", func(t *testing.T) {
		fixture := newReviewBlockerRefreshFixture(t)
		msg := reviewBlockerRealRefresh(t, fixture.mail)
		manifestPath := filepath.Join(fixture.target.Directory, ".agent.json")
		original := reviewBlockerReplaceWithFIFO(t, manifestPath)

		reviewBlockerRequireRefreshUpdateReturns(t, fixture.mail, msg, manifestPath, original, "target-manifest")
	})

	t.Run("direct unread state", func(t *testing.T) {
		fixture := newReviewBlockerRefreshFixture(t)
		if _, err := fs.OpenDirectUnreadStore(
			fixture.target.ProjectDirectory,
			directPerformanceHuman,
			[]fs.DirectTarget{fixture.target},
			fixture.accepted,
		); err != nil {
			t.Fatalf("seed normal direct unread source: %v", err)
		}
		msg := reviewBlockerRealRefresh(t, fixture.mail)
		statePath := filepath.Join(fixture.target.ProjectDirectory, ".lingtai", ".tui-asset", "direct-unread.json")
		original := reviewBlockerReplaceWithFIFO(t, statePath)

		reviewBlockerRequireRefreshUpdateReturns(t, fixture.mail, msg, statePath, original, fmt.Sprintf("direct-unread state (%s)", filepath.Base(statePath)))
	})
}
