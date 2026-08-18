package fs

import (
	"testing"
)

// TestExtractSessionEventTextToolCallLTP renders LingTai-protocol tool calls as
// tool.action_name when the args carry an explicit action field, and leaves
// every other tool_call shape untouched.
func TestExtractSessionEventTextToolCallLTP(t *testing.T) {
	tests := []struct {
		name string
		raw  map[string]interface{}
		want string
	}{
		{
			name: "ltp-v2-args-with-action",
			raw: map[string]interface{}{
				"tool_name": "shell",
				"tool_args": map[string]interface{}{
					"action": "run",
					"input":  map[string]interface{}{"command": "ls"},
				},
			},
			want: `shell.run({"command":"ls"})`,
		},
		{
			name: "ltp-v2-nested-args-with-action",
			raw: map[string]interface{}{
				"tool_name": "file",
				"tool_args": map[string]interface{}{
					"action": "read",
					"input":  map[string]interface{}{"file_path": "/tmp/x"},
				},
			},
			want: `file.read({"file_path":"/tmp/x"})`,
		},
		{
			name: "non-ltp-map-args-without-action",
			raw: map[string]interface{}{
				"tool_name": "web",
				"tool_args": map[string]interface{}{
					"query": "hello",
				},
			},
			want: `web({"query":"hello"})`,
		},
		{
			name: "non-ltp-map-args-reasoning-dropped",
			raw: map[string]interface{}{
				"tool_name": "web",
				"tool_args": map[string]interface{}{
					"query":      "hello",
					"_reasoning": "private diary text",
				},
			},
			want: `web({"query":"hello"})`,
		},
		{
			name: "string-args-pass-through",
			raw: map[string]interface{}{
				"tool_name": "bash",
				"tool_args": "echo hi",
			},
			want: "bash(echo hi)",
		},
		{
			name: "empty-action-keeps-json",
			raw: map[string]interface{}{
				"tool_name": "shell",
				"tool_args": map[string]interface{}{
					"action": "",
					"input":  map[string]interface{}{"command": "ls"},
				},
			},
			want: `shell({"action":"","input":{"command":"ls"}})`,
		},
		{
			name: "ltp-v2-reasoning-dropped",
			raw: map[string]interface{}{
				"tool_name": "file",
				"tool_args": map[string]interface{}{
					"action":     "read",
					"file_path":  "/tmp/x",
					"_reasoning": "private diary text",
				},
			},
			want: `file.read({"file_path":"/tmp/x"})`,
		},
		{
			name: "ltp-v2-action-only-no-params",
			raw: map[string]interface{}{
				"tool_name": "system",
				"tool_args": map[string]interface{}{
					"action": "presets",
				},
			},
			want: "system.presets",
		},
		{
			name: "ltp-v2-multi-input-keys",
			raw: map[string]interface{}{
				"tool_name": "file",
				"tool_args": map[string]interface{}{
					"action": "edit",
					"input": map[string]interface{}{
						"file_path":  "/tmp/a.txt",
						"old_string": "x",
						"new_string": "y",
					},
				},
			},
			want: `file.edit({"file_path":"/tmp/a.txt","new_string":"y","old_string":"x"})`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractSessionEventText(tt.raw, "tool_call")
			if got != tt.want {
				t.Errorf("extractSessionEventText(tool_call) = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestParseEventMapToolCallCarriesReasoningSeparately(t *testing.T) {
	e := parseEventMap(map[string]interface{}{
		"type":      "tool_call",
		"ts":        float64(1787010000),
		"tool_name": "file",
		"tool_args": map[string]interface{}{
			"action":     "read",
			"input":      map[string]interface{}{"file_path": "/tmp/x"},
			"_reasoning": "inspect the source",
		},
	})
	if e == nil {
		t.Fatal("parseEventMap returned nil")
	}
	if e.Reasoning != "inspect the source" {
		t.Fatalf("Reasoning = %q, want tool call's own _reasoning", e.Reasoning)
	}
	if e.Body != `file.read({"file_path":"/tmp/x"})` {
		t.Fatalf("Body = %q, want rendered params without private _reasoning", e.Body)
	}
}
