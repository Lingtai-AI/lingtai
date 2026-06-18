package headless

import (
	"encoding/json"
	"io"
	"os"
)

// WriteJSON marshals data as indented JSON to w, followed by a newline.
func WriteJSON(w io.Writer, data interface{}) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(data)
}

// WriteError writes a JSON error object to w.
func WriteError(w io.Writer, message, code string) error {
	return WriteJSON(w, map[string]string{
		"error": message,
		"code":  code,
	})
}

// WriteErrorDetail writes a JSON error object with machine-readable details.
func WriteErrorDetail(w io.Writer, message, code string, details map[string]interface{}) error {
	out := map[string]interface{}{
		"error": message,
		"code":  code,
	}
	for k, v := range details {
		out[k] = v
	}
	return WriteJSON(w, out)
}

// ExitError writes a JSON error to stderr and exits with code 1.
func ExitError(message, code string) {
	WriteError(os.Stderr, message, code)
	os.Exit(1)
}
