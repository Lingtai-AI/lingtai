package tui

import (
	"fmt"
	"io"
	"os"
	"unicode/utf8"
)

const settingsPsycheMaxBytes = 65536

// readSettingsPsycheOwner is only the strict Psyche writer's owner read gate.
// Inspect before opening, bind the descriptor to that identity, and never read
// more than the closed owner's limit plus one byte, even if it grows mid-read.
func readSettingsPsycheOwner(path string) ([]byte, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	invalid := func() ([]byte, error) {
		return nil, fmt.Errorf("Psyche settings must be stable regular non-symlink UTF-8 within 64 KiB")
	}
	if !before.Mode().IsRegular() || before.Size() > settingsPsycheMaxBytes {
		return invalid()
	}
	file, err := openSettingsPsycheOwner(path)
	if err != nil {
		return nil, fmt.Errorf("open Psyche settings: %w", err)
	}
	defer file.Close()
	opened, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect Psyche settings: %w", err)
	}
	if !sameSettingsPsycheOwner(before, opened) {
		return invalid()
	}
	data, err := io.ReadAll(io.LimitReader(file, settingsPsycheMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read Psyche settings: %w", err)
	}
	after, err := file.Stat()
	if err != nil {
		return nil, fmt.Errorf("inspect Psyche settings: %w", err)
	}
	pathAfter, err := os.Lstat(path)
	// A disappearing/replaced owner is an error, not a missing owner to seed.
	if err != nil {
		return invalid()
	}
	if len(data) > settingsPsycheMaxBytes || int64(len(data)) != opened.Size() ||
		!sameSettingsPsycheOwner(opened, after) || !sameSettingsPsycheOwner(before, pathAfter) || !utf8.Valid(data) {
		return invalid()
	}
	return data, nil
}

func sameSettingsPsycheOwner(a, b os.FileInfo) bool {
	return a.Mode().IsRegular() && b.Mode().IsRegular() && os.SameFile(a, b) &&
		a.Mode() == b.Mode() && a.Size() == b.Size() && a.ModTime().Equal(b.ModTime())
}
