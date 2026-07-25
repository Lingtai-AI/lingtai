package preset

import (
	"bytes"
	"os"
	"testing"
)

func TestExamplesInitMatchesTemplate(t *testing.T) {
	template := readInitTemplate(t)
	example := readRepoInitExample(t)

	if !bytes.Equal(template, example) {
		t.Fatal("examples/init.jsonc has drifted from tui/internal/preset/templates/init.jsonc; run: cp tui/internal/preset/templates/init.jsonc examples/init.jsonc")
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
