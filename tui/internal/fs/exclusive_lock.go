package fs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var exclusiveFileLockPathLocks sync.Map // canonical lock path -> *sync.Mutex

// ExclusiveFileLock owns an advisory, exclusive file lock and its local path
// mutex. Release unlocks and closes the file, then releases the local mutex; it
// never removes the stable lock file.
type ExclusiveFileLock struct {
	mu        sync.Mutex
	file      *os.File
	pathMutex *sync.Mutex
	released  bool
}

// AcquireExclusiveFileLock blocks until this process owns an advisory,
// exclusive lock for lockPath. The stable lock file is retained after Release.
func AcquireExclusiveFileLock(lockPath string) (*ExclusiveFileLock, error) {
	if strings.TrimSpace(lockPath) == "" {
		return nil, fmt.Errorf("exclusive lock path is empty")
	}
	canonicalPath, err := filepath.Abs(filepath.Clean(lockPath))
	if err != nil {
		return nil, fmt.Errorf("resolve exclusive lock path: %w", err)
	}

	value, _ := exclusiveFileLockPathLocks.LoadOrStore(canonicalPath, &sync.Mutex{})
	pathMutex := value.(*sync.Mutex)
	pathMutex.Lock()

	if err := os.MkdirAll(filepath.Dir(canonicalPath), 0o755); err != nil {
		pathMutex.Unlock()
		return nil, fmt.Errorf("create lock parent: %w", err)
	}
	file, err := os.OpenFile(canonicalPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		pathMutex.Unlock()
		return nil, fmt.Errorf("open exclusive lock: %w", err)
	}
	if err := lockFileExclusive(file); err != nil {
		_ = file.Close()
		pathMutex.Unlock()
		return nil, fmt.Errorf("lock exclusive file: %w", err)
	}

	return &ExclusiveFileLock{file: file, pathMutex: pathMutex}, nil
}

// Release unlocks, closes, and frees the local path mutex. It is safe to call
// more than once. It never removes the stable lock file.
func (lock *ExclusiveFileLock) Release() error {
	if lock == nil {
		return nil
	}

	lock.mu.Lock()
	if lock.released {
		lock.mu.Unlock()
		return nil
	}
	lock.released = true
	file := lock.file
	pathMutex := lock.pathMutex
	lock.file = nil
	lock.pathMutex = nil
	lock.mu.Unlock()

	var result error
	if file != nil {
		if err := unlockFile(file); err != nil {
			result = fmt.Errorf("unlock exclusive file: %w", err)
		}
		if err := file.Close(); err != nil && result == nil {
			result = fmt.Errorf("close exclusive lock: %w", err)
		}
	}
	if pathMutex != nil {
		pathMutex.Unlock()
	}
	return result
}
