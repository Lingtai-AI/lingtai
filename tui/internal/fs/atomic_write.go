package fs

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const atomicReplacementTempAttempts = 100

// writeAtomicReplacement publishes a complete file through a unique sibling
// temporary. The destination remains untouched until replaceFile succeeds.
// Existing targets retain their permission bits; new targets use mode subject
// to the process umask.
func writeAtomicReplacement(path string, mode os.FileMode, write func(io.Writer) error) (err error) {
	directory := filepath.Dir(path)
	temporaryMode := mode.Perm()
	preserveExistingMode := false
	if info, statErr := os.Stat(path); statErr == nil {
		temporaryMode = info.Mode().Perm()
		preserveExistingMode = true
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("inspect replacement target: %w", statErr)
	}

	temporary, err := createAtomicReplacementTemp(directory, filepath.Base(path), temporaryMode)
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
	if preserveExistingMode {
		// OpenFile applies the current umask. Restore the canonical target's exact
		// permission bits so a later restrictive umask cannot narrow them and a
		// permissive fallback cannot widen an operator-restricted target.
		if err := temporary.Chmod(temporaryMode); err != nil {
			return fmt.Errorf("preserve replacement target mode: %w", err)
		}
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

func createAtomicReplacementTemp(directory, base string, mode os.FileMode) (*os.File, error) {
	var random [12]byte
	for attempt := 0; attempt < atomicReplacementTempAttempts; attempt++ {
		if _, err := rand.Read(random[:]); err != nil {
			return nil, fmt.Errorf("generate replacement temp suffix: %w", err)
		}
		path := filepath.Join(directory, "."+base+".tmp-"+hex.EncodeToString(random[:]))
		temporary, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_RDWR, mode.Perm())
		if err == nil {
			return temporary, nil
		}
		if os.IsExist(err) {
			continue
		}
		return nil, err
	}
	return nil, fmt.Errorf("exhausted %d unique replacement temp attempts", atomicReplacementTempAttempts)
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
