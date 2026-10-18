package preset

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
)

// DecodeJSONUseNumber decodes exactly one JSON document while retaining number
// tokens as json.Number. The second decode preserves json.Unmarshal's rejection
// of trailing non-whitespace data instead of silently accepting another value.
func DecodeJSONUseNumber(data []byte, dst interface{}) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("invalid trailing data after JSON document")
		}
		return err
	}
	return nil
}

// InitPresetPolicy returns only the authored policy at its existing owner.
// It does not choose or authorize a preset and never creates/migrates sections.
func InitPresetPolicy(root map[string]interface{}) map[string]interface{} {
	if manifest, ok := root["manifest"].(map[string]interface{}); ok {
		policy, _ := manifest["preset"].(map[string]interface{})
		return policy
	}
	llm, _ := root["llm"].(map[string]interface{})
	policy, _ := llm["preset"].(map[string]interface{})
	return policy
}

// recipeSkills selects an existing legacy override or canonical default-on
// skills owner. Creating an in-memory canonical override is not a disk write;
// Core still decides whether WriteSkills is requested.
func recipeSkills(root map[string]interface{}) (map[string]interface{}, bool, error) {
	if manifest, legacy := root["manifest"].(map[string]interface{}); legacy {
		caps, _ := manifest["capabilities"].(map[string]interface{})
		if caps == nil {
			return nil, false, nil
		}
		changed, err := CanonicalizeCapabilities(caps)
		skills, _ := caps["skills"].(map[string]interface{})
		return skills, changed, err
	}
	if _, canonical := root["llm"].(map[string]interface{}); !canonical {
		return nil, false, nil
	}
	tools, ok := root["tools"].(map[string]interface{})
	if _, present := root["tools"]; present && !ok {
		return nil, false, errors.New("tools must be an object")
	}
	if tools == nil {
		tools = map[string]interface{}{}
		root["tools"] = tools
	}
	if off, ok := tools["skills"].(bool); ok && !off {
		return nil, false, nil
	}
	skills, ok := tools["skills"].(map[string]interface{})
	if _, present := tools["skills"]; present && !ok {
		return nil, false, errors.New("tools.skills must be an object or false")
	}
	if skills == nil {
		skills = map[string]interface{}{}
		tools["skills"] = skills
	}
	return skills, false, nil
}

// DecodeKernelJSONCUseNumber is a syntax-only codec for authorized local reads
// of the exact source selected by Core. Like kernel config_resolve.load_jsonc,
// it accepts // comments and trailing commas without altering string literals.
// It never selects a preset, expands defaults or writes the source.
func DecodeKernelJSONCUseNumber(data []byte, dst interface{}) error {
	clean := make([]byte, 0, len(data))
	quoted, escaped, comment := false, false, false
	for i := 0; i < len(data); i++ {
		c := data[i]
		if comment {
			if c == '\n' {
				comment = false
				clean = append(clean, c)
			}
			continue
		}
		if quoted {
			clean = append(clean, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			clean = append(clean, c)
			continue
		}
		if c == '/' && i+1 < len(data) && data[i+1] == '/' {
			comment = true
			i++
			continue
		}
		clean = append(clean, c)
	}
	out := make([]byte, 0, len(clean))
	quoted, escaped = false, false
	for i := 0; i < len(clean); i++ {
		c := clean[i]
		if quoted {
			out = append(out, c)
			if escaped {
				escaped = false
			} else if c == '\\' {
				escaped = true
			} else if c == '"' {
				quoted = false
			}
			continue
		}
		if c == '"' {
			quoted = true
			out = append(out, c)
			continue
		}
		if c == ',' {
			j := i + 1
			for j < len(clean) && (clean[j] == ' ' || clean[j] == '\t' || clean[j] == '\n' || clean[j] == '\r') {
				j++
			}
			if j < len(clean) && (clean[j] == '}' || clean[j] == ']') {
				continue
			}
		}
		out = append(out, c)
	}
	return DecodeJSONUseNumber(out, dst)
}
