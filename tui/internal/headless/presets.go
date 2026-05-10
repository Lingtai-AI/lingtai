package headless

import (
	"io"

	"github.com/anthropics/lingtai-tui/internal/preset"
)

// PresetEntry is one preset in the JSON output.
type PresetEntry struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Tier        string `json:"tier,omitempty"`
	Source      string `json:"source"`
	Path        string `json:"path"`
}

// PresetsOutput is the top-level JSON response for the presets command.
type PresetsOutput struct {
	Presets []PresetEntry `json:"presets"`
}

// RunPresets lists available presets as JSON to stdout.
// Errors are written as JSON to stderr.
func RunPresets(stdout, stderr io.Writer, savedOnly, templatesOnly bool) {
	all, err := preset.List()
	if err != nil {
		WriteError(stderr, "failed to list presets: "+err.Error(), "list_failed")
		return
	}

	var entries []PresetEntry
	for _, p := range all {
		source := "saved"
		if p.Source == preset.SourceTemplate {
			source = "template"
		}
		if savedOnly && source != "saved" {
			continue
		}
		if templatesOnly && source != "template" {
			continue
		}
		entries = append(entries, PresetEntry{
			Name:        p.Name,
			Description: p.Description.Summary,
			Tier:        p.Description.Tier,
			Source:      source,
			Path:        preset.RefFor(p),
		})
	}

	if entries == nil {
		entries = []PresetEntry{}
	}
	WriteJSON(stdout, PresetsOutput{Presets: entries})
}
