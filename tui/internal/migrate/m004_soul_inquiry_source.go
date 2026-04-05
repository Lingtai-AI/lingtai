package migrate

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

// migrateSoulInquirySource adds "source":"agent" to existing soul_inquiry.jsonl
// entries that lack a source field. All pre-existing entries were produced by the
// agent process; the new "human" source comes from TUI /btw via the .inquiry file.
func migrateSoulInquirySource(lingtaiDir string) error {
	entries, err := os.ReadDir(lingtaiDir)
	if err != nil {
		return err
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		logPath := filepath.Join(lingtaiDir, entry.Name(), "logs", "soul_inquiry.jsonl")
		if _, err := os.Stat(logPath); os.IsNotExist(err) {
			continue
		}
		if err := backfillSource(logPath); err != nil {
			return err
		}
	}
	return nil
}

func backfillSource(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	var lines []json.RawMessage
	changed := false
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		raw := scanner.Bytes()
		if len(raw) == 0 {
			continue
		}

		var m map[string]interface{}
		if err := json.Unmarshal(raw, &m); err != nil {
			// Keep unparseable lines as-is
			lines = append(lines, append(json.RawMessage{}, raw...))
			continue
		}

		if _, ok := m["source"]; !ok {
			m["source"] = "agent"
			rewritten, err := json.Marshal(m)
			if err != nil {
				lines = append(lines, append(json.RawMessage{}, raw...))
				continue
			}
			lines = append(lines, rewritten)
			changed = true
		} else {
			lines = append(lines, append(json.RawMessage{}, raw...))
		}
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if !changed {
		return nil
	}

	// Write atomically via temp file
	tmpPath := path + ".tmp"
	out, err := os.Create(tmpPath)
	if err != nil {
		return err
	}
	for _, line := range lines {
		out.Write(line)
		out.Write([]byte("\n"))
	}
	if err := out.Close(); err != nil {
		os.Remove(tmpPath)
		return err
	}
	return os.Rename(tmpPath, path)
}
