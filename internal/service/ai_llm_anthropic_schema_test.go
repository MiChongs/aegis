package service

import (
	"encoding/json"
	"testing"
)

// Anthropic 拒收顶层 oneOf/anyOf/allOf（整个请求 400），转换时必须去掉；属性内部的组合要保留。
func TestAnthropicInputSchemaDropsTopLevelCombinators(t *testing.T) {
	raw := json.RawMessage(`{
		"type": "object",
		"properties": {
			"url": {"type": "string"},
			"document_id": {"type": "string"},
			"mode": {"anyOf": [{"type": "string"}, {"type": "integer"}]}
		},
		"required": ["url"],
		"oneOf": [{"required": ["url"]}, {"required": ["document_id"]}],
		"anyOf": [{"required": ["url"]}],
		"allOf": [{"required": ["url"]}],
		"additionalProperties": false
	}`)
	schema := anthropicInputSchema(raw)
	encoded, err := json.Marshal(schema)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var top map[string]any
	if err := json.Unmarshal(encoded, &top); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"oneOf", "anyOf", "allOf"} {
		if _, ok := top[key]; ok {
			t.Fatalf("top-level %s should be dropped: %s", key, encoded)
		}
	}
	if top["type"] != "object" || top["additionalProperties"] != false {
		t.Fatalf("type/additionalProperties lost: %s", encoded)
	}
	properties, _ := top["properties"].(map[string]any)
	mode, _ := properties["mode"].(map[string]any)
	if _, ok := mode["anyOf"]; !ok {
		t.Fatalf("nested anyOf should be kept: %s", encoded)
	}
	if required, _ := top["required"].([]any); len(required) != 1 || required[0] != "url" {
		t.Fatalf("required lost: %s", encoded)
	}
}
