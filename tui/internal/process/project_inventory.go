package process

import (
	"bytes"
	"encoding/json"
	"errors"
	"os/exec"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/inventory"
)

// QueryProjectInventory uses only the exact supplied runtime interpreter's
// sibling CLI, just like the other Project operations. No PATH/global Python,
// timeout, retry, worker, scanner limit, or Go-policy fallback is introduced.
func QueryProjectInventory(python string, observation inventory.Observation) (inventory.Decision, error) {
	if !filepath.IsAbs(python) {
		return inventory.Decision{}, errors.New("Project inventory requires an exact runtime interpreter")
	}
	request, err := json.Marshal(observation)
	if err != nil {
		return inventory.Decision{}, err
	}
	cmd := exec.Command(kernelCLI(python), "project", "inventory-query")
	cmd.Stdin = bytes.NewReader(request)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		// Preserve the original Go exec error, never reconstruct an errno from
		// Python or mistake a failed query for an empty inventory.
		return inventory.Decision{}, err
	}
	if stderr.Len() != 0 {
		return inventory.Decision{}, errors.New("Project inventory query outcome unknown")
	}
	return decodeProjectInventory(stdout.Bytes())
}

func decodeProjectInventory(data []byte) (inventory.Decision, error) {
	var result inventory.Decision
	raw, err := recipeObject(data, &result, "status members groups phantom_projects", "")
	if err != nil {
		return result, err
	}
	if result.Status != "inventory_queried" {
		return result, errors.New("Project inventory query outcome unknown")
	}
	var members []json.RawMessage
	if err := json.Unmarshal(raw["members"], &members); err != nil {
		return result, err
	}
	for _, member := range members {
		if _, err := recipeObject(member, nil, "row directory pid role human orchestrator phantom reason", ""); err != nil {
			return result, err
		}
	}
	// Inner arrays are required, not null. The inventory adapter validates the
	// exact row/identity/group references before any record is exposed.
	var groups []json.RawMessage
	if err := json.Unmarshal(raw["groups"], &groups); err != nil {
		return result, err
	}
	for _, group := range groups {
		if bytes.Equal(bytes.TrimSpace(group), []byte("null")) {
			return result, errors.New("invalid Project inventory group")
		}
	}
	return result, nil
}
