package main

import (
	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/process"
	"os"
	"path/filepath"
)

// Retain the original list fixture oracles; parent acceptance supplies the
// exact newly installed candidate CLI, never a mock copy of Go domain policy.
func init() {
	python := os.Getenv("LINGTAI_PROJECT_INVENTORY_TEST_PYTHON")
	if python == "" {
		return
	}
	if !filepath.IsAbs(python) {
		panic("inventory test interpreter must be absolute")
	}
	inventory.Query = func(o inventory.Observation) (inventory.Decision, error) {
		return process.QueryProjectInventory(python, o)
	}
}
