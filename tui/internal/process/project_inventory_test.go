package process

import "testing"

func TestInventoryClosedReceipt(t *testing.T) {
	valid := `{"status":"inventory_queried","members":[{"row":0,"directory":"/w==","pid":123,"role":"MAIN","human":false,"orchestrator":true,"phantom":false,"reason":""}],"groups":[[0]],"phantom_projects":[]}`
	if _, err := decodeProjectInventory([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		`{"status":"inventory_queried","status":"inventory_queried","members":[],"groups":[],"phantom_projects":[]}`,
		`{"status":"inventory_queried","members":[],"groups":null,"phantom_projects":[]}`,
		`{"status":"inventory_queried","members":[{"row":0,"directory":"/w==","pid":123,"role":"MAIN","human":null,"orchestrator":true,"phantom":false,"reason":""}],"groups":[[0]],"phantom_projects":[]}`,
		valid + `{}`,
	} {
		if _, err := decodeProjectInventory([]byte(bad)); err == nil {
			t.Fatalf("accepted invalid receipt: %s", bad)
		}
	}
}
