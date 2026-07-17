package preset

import (
	"encoding/json"
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
