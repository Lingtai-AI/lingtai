//go:build windows

package processscan

import "os"

func openRegistryFile(path string) (*os.File, error) {
	return os.Open(path)
}
