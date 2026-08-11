package config

import (
	"fmt"
	"regexp"
)

// addonKeyRe matches an allowed legacy addon/module identifier: a non-empty
// dot-separated sequence of Python identifier segments (letters, digits and
// underscores, each segment starting with a letter or underscore).
//
// Legacy init.json "addons" object keys are interpolated verbatim into
// generated Python import source before agent launch
// ("import lingtai.addons.<key>"), so only characters that cannot alter the
// constructed program are allowed. A semicolon, newline, quote, comment
// marker, whitespace, or any other source-boundary character changes the
// generated program instead of remaining addon-name data and must be
// rejected before any interpreter source or command is constructed.
var addonKeyRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// ValidateAddonKey reports whether name is an allowed addon/module
// identifier that may safely be interpolated into generated Python import
// source (e.g. "import lingtai.addons.<name>").
//
// Every legacy addon object key must pass this check before any interpreter
// source or command is constructed from it. Keys containing semicolons or
// any other source-boundary/non-identifier characters return an error so
// callers can fail fast at the validation boundary instead of building a
// program that a malformed or untrusted key has altered.
func ValidateAddonKey(name string) error {
	if !addonKeyRe.MatchString(name) {
		return fmt.Errorf(
			"addon key %q is not a valid addon/module identifier: only letters, digits, "+
				"underscores and dot-separated segments are allowed, each segment must start "+
				"with a letter or underscore, and semicolons, whitespace, quotes, and other "+
				"source-boundary characters are rejected",
			name,
		)
	}
	return nil
}
