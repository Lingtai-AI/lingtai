package preset

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestExamplesInitMatchesTemplate(t *testing.T) {
	template := readInitTemplate(t)
	example := readRepoInitExample(t)

	if !bytes.Equal(template, example) {
		t.Fatal("examples/init.jsonc has drifted from tui/internal/preset/templates/init.jsonc; run: cp tui/internal/preset/templates/init.jsonc examples/init.jsonc")
	}
}

func TestInitTemplateHasNoRetiredOrSeedEntries(t *testing.T) {
	files := map[string][]byte{
		"tui/internal/preset/templates/init.jsonc": readInitTemplate(t),
		"examples/init.jsonc":                      readRepoInitExample(t),
	}
	forbiddenKeys := []string{
		"brief",
		"brief_file",
		"procedures",
		"procedures_file",
		"principle",
		"principle_file",
		"substrate",
		"substrate_file",
		"prompt",
		"prompt_file",
		"lingtai",
		"lingtai_file",
		"stamina",
		"molt_pressure",
		"molt_prompt",
		"psyche",
		"codex",
		"web_read",
		"talk",
		"compose",
		"draw",
		"listen",
	}

	for name, data := range files {
		for _, key := range forbiddenKeys {
			assertNoJSONCKeyEntry(t, name, string(data), key)
		}
	}
}

func assertNoJSONCKeyEntry(t *testing.T, name, text, key string) {
	t.Helper()
	keyEntry := regexp.MustCompile(`"` + regexp.QuoteMeta(key) + `"\s*:`)
	for lineNum, line := range strings.Split(text, "\n") {
		if keyEntry.MatchString(line) {
			t.Errorf("%s:%d still contains forbidden init key entry %q", name, lineNum+1, key)
		}
	}
}

func readInitTemplate(t *testing.T) []byte {
	t.Helper()
	data, err := templatesFS.ReadFile("templates/init.jsonc")
	if err != nil {
		t.Fatalf("read embedded init template: %v", err)
	}
	return data
}

func readRepoInitExample(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile("../../../examples/init.jsonc")
	if err != nil {
		t.Fatalf("read examples/init.jsonc: %v", err)
	}
	return data
}

// Email is now a Core owner inside tools, not a retired top-level seed. Retain
// the root-owner exclusion while allowing the canonical commented tool example.
func TestInitTemplateCanonicalOwners(t *testing.T) {
	for _, data := range [][]byte{readInitTemplate(t), readRepoInitExample(t)} {
		var doc map[string]interface{}
		if DecodeKernelJSONCUseNumber(data, &doc) != nil {
			t.Fatal("canonical template is not parseable")
		}
		for _, key := range []string{"agent", "llm", "tools", "mcp", "plugins"} {
			if _, exists := doc[key]; !exists {
				t.Fatalf("missing canonical owner %s", key)
			}
		}
		for _, key := range []string{"manifest", "addons", "email", "covenant_file", "pad"} {
			if _, exists := doc[key]; exists {
				t.Fatalf("retired root owner %s", key)
			}
		}
	}
}

func TestSelectedSourceJSONCCodecPreservesLiteralsAndNumbers(t *testing.T) {
	raw := []byte("{\n // example says \"quoted\" and {brackets}\n \"llm\":{\"provider\":\"openai\",\"model\":\"literal, } // text\",\"route\":9007199254740993,},\n}")
	var doc map[string]interface{}
	if err := DecodeKernelJSONCUseNumber(raw, &doc); err != nil {
		t.Fatal(err)
	}
	llm := doc["llm"].(map[string]interface{})
	if llm["model"] != "literal, } // text" || llm["route"] != json.Number("9007199254740993") {
		t.Fatal("JSONC codec altered source literals")
	}
}
