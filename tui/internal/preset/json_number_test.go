package preset

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDecodeJSONUseNumberPreservesExactTokenAndSingleDocumentSemantics(t *testing.T) {
	var doc map[string]interface{}
	if err := DecodeJSONUseNumber([]byte("{\"tenant_id\":9007199254740993}\n\t"), &doc); err != nil {
		t.Fatalf("DecodeJSONUseNumber(valid): %v", err)
	}
	if got, ok := doc["tenant_id"].(json.Number); !ok || got != json.Number("9007199254740993") {
		t.Fatalf("tenant_id = %#v (%T), want exact json.Number", doc["tenant_id"], doc["tenant_id"])
	}

	for _, input := range []string{
		`{"tenant_id":1} {"tenant_id":2}`,
		`{"tenant_id":1} trailing`,
	} {
		var got map[string]interface{}
		if err := DecodeJSONUseNumber([]byte(input), &got); err == nil {
			t.Fatalf("DecodeJSONUseNumber(%q) error = nil, want trailing-data rejection", input)
		}
	}
}

func TestProjectLoadedContextLimitRetainsLargeExactInteger(t *testing.T) {
	for _, input := range []string{`{"manifest":{"llm":{"provider":"custom","model":"test","context_limit":9007199254740993}}}`, `{"manifest":{"llm":{"provider":"custom","model":"test"},"context_limit":9007199254740993}}`} {
		path := filepath.Join(t.TempDir(), "selected.json")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		p, err := loadFromPath(path)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := json.Marshal(p.Manifest)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(raw), "9007199254740993") {
			t.Fatalf("loaded context limit rounded: %s", raw)
		}
	}
}
