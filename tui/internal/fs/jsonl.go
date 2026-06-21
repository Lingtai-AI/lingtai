package fs

import (
	"bufio"
	"bytes"
	"io"
	"os"
)

const jsonlReaderBufferSize = 64 * 1024

// forEachJSONLLine streams a JSONL file one line at a time. Blank lines are
// skipped. It intentionally uses Reader.ReadBytes instead of bufio.Scanner so
// unusually large JSON payloads are not silently capped at Scanner's default
// 64 KiB token limit.
func forEachJSONLLine(path string, fn func([]byte)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	r := bufio.NewReaderSize(f, jsonlReaderBufferSize)
	for {
		line, err := r.ReadBytes('\n')
		if len(line) > 0 {
			line = bytes.TrimSpace(line)
			if len(line) > 0 {
				fn(line)
			}
		}
		if err == nil {
			continue
		}
		if err == io.EOF {
			return nil
		}
		return err
	}
}
