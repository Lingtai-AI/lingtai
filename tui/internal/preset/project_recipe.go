package preset

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/anthropics/lingtai-tui/internal/config"
	"github.com/anthropics/lingtai-tui/internal/process"
)

// Selection is the existing installed-CLI resolver, not a new product setting.
// Recipe enumerates all members in kernel; no selected-member/Go traversal gate.
func recipePython() (string, error) {
	globalDir, err := config.GlobalDirPath()
	if err != nil {
		return "", err
	}
	return config.LingtaiCmd(globalDir), nil
}

func applyKernelRecipe(bundleDir, lingtaiDir, lang string, render func(string) string) (int, error) {
	python, err := recipePython()
	if err != nil {
		return 0, err
	}
	result, err := process.ApplyProjectRecipeNative(python, bundleDir, lingtaiDir, lang, render, newNativeRecipeOwner(bundleDir, lingtaiDir))
	if err == nil {
		return result.LegacyApplied, nil
	}
	var failure *process.ProjectRecipeError
	if !errors.As(err, &failure) || !failure.Known {
		return result.LegacyApplied, err
	}
	// Preserve first old phase and causal chain from factual evidence only.
	// The frozen ABI does NOT prove exact JSON decoder text/type or syscall
	// formatting; keep typed receipt in the chain rather than guessing causes.
	if f := result.LegacyFailure; f != nil {
		switch f.Phase {
		case "metadata":
			return result.LegacyApplied, fmt.Errorf("ApplyRecipe: invalid recipe in %s: %w", bundleDir, err)
		case "enumeration":
			return result.LegacyApplied, fmt.Errorf("ApplyRecipe: read .lingtai: %w", err)
		case "prompt":
			return result.LegacyApplied, fmt.Errorf("ApplyRecipe: write .prompt for %s: %w", filepath.Base(filepath.Dir(f.Path())), err)
		case "skills":
			return result.LegacyApplied, fmt.Errorf("ApplyRecipe: update skills.paths for %s: %w", filepath.Base(filepath.Dir(f.Path())), err)
		case "snapshot":
			return result.LegacyApplied, fmt.Errorf("ApplyRecipe: snapshot applied recipe: %w", err)
		}
	}
	return result.LegacyApplied, err
}
