package preset

import (
	"errors"
	"fmt"
	"reflect"
)

// ErrCapabilityConflict identifies a fail-closed legacy/canonical capability
// collision so callers that enumerate presets do not silently hide it.
var ErrCapabilityConflict = errors.New("conflicting capability configuration")

const (
	legacyShellCapability    = "bash"
	canonicalShellCapability = "shell"
)

// CanonicalizeCapabilities is a bounded in-memory helper for explicit TUI
// preset/editor/write flows; the kernel owns init compatibility semantics. It
// accepts legacy bash and moves its configuration object to canonical shell.
// The legacy value is never merged with a different canonical value: a conflict
// is an error and leaves the input untouched. When both values are identical,
// shell wins deterministically and bash is removed.
func CanonicalizeCapabilities(caps map[string]interface{}) (bool, error) {
	if caps == nil {
		return false, nil
	}
	legacy, hasLegacy := caps[legacyShellCapability]
	canonical, hasCanonical := caps[canonicalShellCapability]
	if !hasLegacy {
		return false, nil
	}
	if hasCanonical && !reflect.DeepEqual(legacy, canonical) {
		return false, fmt.Errorf("%w: %q and %q differ", ErrCapabilityConflict, legacyShellCapability, canonicalShellCapability)
	}

	if !hasCanonical {
		caps[canonicalShellCapability] = legacy
	}
	delete(caps, legacyShellCapability)
	return true, nil
}

// NormalizeLegacyCapabilities applies the bounded helper to a preset before
// explicit TUI output. Non-object capability values are left for Validate to
// report, matching the existing preset validation path.
func (p *Preset) NormalizeLegacyCapabilities() error {
	if p == nil || p.Manifest == nil {
		return nil
	}
	caps, ok := p.Manifest["capabilities"].(map[string]interface{})
	if !ok {
		return nil
	}
	_, err := CanonicalizeCapabilities(caps)
	return err
}
