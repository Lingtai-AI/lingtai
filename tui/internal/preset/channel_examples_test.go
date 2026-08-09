package preset

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// stripJSONCComments removes // line comments and /* */ block comments while
// preserving string literals, so the result can be parsed as plain JSON.
func stripJSONCComments(src []byte) []byte {
	var b strings.Builder
	inString := false
	for i := 0; i < len(src); i++ {
		c := src[i]
		if inString {
			b.WriteByte(c)
			if c == '\\' && i+1 < len(src) {
				i++
				b.WriteByte(src[i])
			} else if c == '"' {
				inString = false
			}
			continue
		}
		switch {
		case c == '"':
			inString = true
			b.WriteByte(c)
		case c == '/' && i+1 < len(src) && src[i+1] == '/':
			for i < len(src) && src[i] != '\n' {
				i++
			}
			b.WriteByte('\n')
		case c == '/' && i+1 < len(src) && src[i+1] == '*':
			i += 2
			for i+1 < len(src) && !(src[i] == '*' && src[i+1] == '/') {
				i++
			}
			i++ // skip the closing '/'
		default:
			b.WriteByte(c)
		}
	}
	return []byte(b.String())
}

func readRepoChannelExample(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../examples/" + name)
	if err != nil {
		t.Fatalf("read examples/%s: %v", name, err)
	}
	return data
}

// TestChannelExamplesParse guards the trailing-comma hazard (issue #568): the
// shipped channel example configs must remain parseable as JSON once JSONC
// comments are stripped, matching what the addon config loaders accept.
func TestChannelExamplesParse(t *testing.T) {
	for _, name := range []string{"telegram.jsonc", "imap.jsonc"} {
		t.Run(name, func(t *testing.T) {
			clean := stripJSONCComments(readRepoChannelExample(t, name))
			var v any
			if err := json.Unmarshal(clean, &v); err != nil {
				t.Fatalf("examples/%s does not parse as JSON after comment stripping: %v\nstripped:\n%s", name, err, clean)
			}
		})
	}
}

// TestChannelExamplesAllowlistFirst encodes the safety posture as CI policy:
// the shipped channel examples must enable populated sender allowlists, so a
// future edit cannot silently regress to open access (issue #568).
func TestChannelExamplesAllowlistFirst(t *testing.T) {
	tg := readRepoChannelExample(t, "telegram.jsonc")
	var tgCfg struct {
		AllowedUsers []int `json:"allowed_users"`
	}
	if err := json.Unmarshal(stripJSONCComments(tg), &tgCfg); err != nil {
		t.Fatalf("parse examples/telegram.jsonc: %v", err)
	}
	if len(tgCfg.AllowedUsers) == 0 {
		t.Error("examples/telegram.jsonc must ship a non-empty allowed_users allowlist (open access regressed)")
	}

	im := readRepoChannelExample(t, "imap.jsonc")
	var imCfg struct {
		AllowedSenders []string `json:"allowed_senders"`
	}
	if err := json.Unmarshal(stripJSONCComments(im), &imCfg); err != nil {
		t.Fatalf("parse examples/imap.jsonc: %v", err)
	}
	if len(imCfg.AllowedSenders) == 0 {
		t.Error("examples/imap.jsonc must ship a non-empty allowed_senders allowlist (open access regressed)")
	}
}

// TestChannelTemplatesMatchExamples keeps the TUI preset templates byte-identical
// to the standalone examples, mirroring TestExamplesInitMatchesTemplate.
func TestChannelTemplatesMatchExamples(t *testing.T) {
	for _, name := range []string{"telegram.jsonc", "imap.jsonc"} {
		tmpl, err := templatesFS.ReadFile("templates/" + name)
		if err != nil {
			t.Fatalf("read embedded template %s: %v", name, err)
		}
		ex := readRepoChannelExample(t, name)
		if !bytes.Equal(tmpl, ex) {
			t.Errorf("tui/internal/preset/templates/%s has drifted from examples/%s; run: cp examples/%s tui/internal/preset/templates/%s", name, name, name, name)
		}
	}
}

// TestReadmeNoStaleOpenAccessClaim prevents the retired absolute safety claims
// from resurfacing in README.md via merge-conflict resolution (issue #568).
func TestReadmeNoStaleOpenAccessClaim(t *testing.T) {
	data, err := os.ReadFile("../../../README.md")
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	for _, retired := range []string{
		"unknown senders do not auto-receive replies",
		"safety defaults for unknown senders",
	} {
		if strings.Contains(string(data), retired) {
			t.Errorf("README.md must not contain the retired claim %q; it overstates channel-addon behavior (issue #568)", retired)
		}
	}
}
