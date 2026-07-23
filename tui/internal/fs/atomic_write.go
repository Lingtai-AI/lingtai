package fs

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// writeAtomicReplacement publishes a complete file through a unique sibling
// temporary. The destination remains untouched until replaceFile succeeds.
func writeAtomicReplacement(path string, mode os.FileMode, write func(io.Writer) error) (err error) {
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("create replacement temp: %w", err)
	}
	temporaryPath := temporary.Name()
	published := false
	defer func() {
		_ = temporary.Close()
		if !published {
			_ = os.Remove(temporaryPath)
		}
	}()

	if err := write(temporary); err != nil {
		return fmt.Errorf("write replacement temp: %w", err)
	}
	if err := temporary.Chmod(mode); err != nil {
		return fmt.Errorf("set replacement temp mode: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		return fmt.Errorf("sync replacement temp: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close replacement temp: %w", err)
	}
	if err := replaceFile(temporaryPath, path); err != nil {
		return fmt.Errorf("publish replacement: %w", err)
	}
	published = true
	bestEffortSyncDirectory(directory)
	return nil
}

func writeAtomicBytes(path string, data []byte, mode os.FileMode) error {
	return writeAtomicReplacement(path, mode, func(destination io.Writer) error {
		written, err := destination.Write(data)
		if err != nil {
			return err
		}
		if written != len(data) {
			return io.ErrShortWrite
		}
		return nil
	})
}

func bestEffortSyncDirectory(path string) {
	directory, err := os.Open(path)
	if err != nil {
		return
	}
	defer directory.Close()
	_ = directory.Sync()
}
