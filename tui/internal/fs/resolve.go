package fs

import (
	"path/filepath"
	"strings"
)

// ParseAddress parses an address that may contain a host prefix.
// Supported formats:
//   - "localhost:/absolute/path" → ("localhost", "/absolute/path", true)
//   - "[ipv6-addr]:/absolute/path" → ("ipv6-addr", "/absolute/path", true)
//   - "[ipv6-addr]:port:/absolute/path" → ("ipv6-addr", "/absolute/path", true)
//
// Returns ("", "", false) for bare names, local absolute paths, relative paths,
// empty paths after host, or non-absolute paths after host.
func ParseAddress(addr string) (host, path string, ok bool) {
	// Handle bracketed IPv6 addresses: [addr]:... or [addr]:port:...
	if strings.HasPrefix(addr, "[") {
		closeBracket := strings.Index(addr, "]")
		if closeBracket < 0 {
			return "", "", false
		}
		host = addr[1:closeBracket]
		rest := addr[closeBracket+1:] // starts with ":"

		if !strings.HasPrefix(rest, ":") {
			return "", "", false
		}
		rest = rest[1:] // strip the colon after "]"

		// If rest starts with "/" it's the path; otherwise it could be port:/path
		if strings.HasPrefix(rest, "/") {
			path = rest
		} else {
			// Look for :/ which indicates port followed by path
			idx := strings.Index(rest, ":/")
			if idx < 0 {
				return "", "", false
			}
			path = rest[idx+1:]
		}

		if path == "" {
			return "", "", false
		}
		return host, path, true
	}

	// Handle "localhost:/path" or "hostname:..."
	if !strings.HasPrefix(addr, "localhost:") {
		return "", "", false
	}

	path = addr[len("localhost:"):]
	if !strings.HasPrefix(path, "/") {
		return "", "", false
	}
	return "localhost", path, true
}

// IsRemoteAddress returns true if the address contains a non-localhost host prefix.
func IsRemoteAddress(addr string) bool {
	host, _, ok := ParseAddress(addr)
	return ok && host != "localhost"
}

// FormatAbsoluteAddress builds an address string from host and path.
// "localhost" produces "localhost:/path"; anything else produces "[host]:/path".
func FormatAbsoluteAddress(host, path string) string {
	if host == "localhost" {
		return "localhost:" + path
	}
	return "[" + host + "]:" + path
}

// ResolveAddress resolves an agent address to an absolute path.
// If the address is a host:path format (as recognized by ParseAddress),
// it is returned as-is.
// Relative names (e.g. "本我") are joined with baseDir.
// Absolute paths are returned as-is.
func ResolveAddress(addr, baseDir string) string {
	if _, _, ok := ParseAddress(addr); ok {
		return addr
	}
	if filepath.IsAbs(addr) {
		return addr
	}
	return filepath.Join(baseDir, addr)
}

// RelativizeAddress converts an absolute address to a relative name
// by stripping the baseDir prefix. If the address is already relative
// or doesn't start with baseDir, it's returned as-is.
func RelativizeAddress(addr, baseDir string) string {
	if !filepath.IsAbs(addr) {
		return addr
	}
	prefix := baseDir + "/"
	if strings.HasPrefix(addr, prefix) {
		return addr[len(prefix):]
	}
	return addr
}
