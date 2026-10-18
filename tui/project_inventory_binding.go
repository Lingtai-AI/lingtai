package main

import (
	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/inventory"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// Compose the same native-observation query for list and ProjectsModel before
// any subcommand or no-project launcher runs. No bootstrap or PATH fallback.
func init() {
	inventory.Query = func(o inventory.Observation) (inventory.Decision, error) {
		global, err := config.GlobalDirPath()
		if err != nil {
			return inventory.Decision{}, err
		}
		return process.QueryProjectInventory(config.VenvPython(config.RuntimeVenvDir(global)), o)
	}
}
