package service

import "encoding/json"

// DefinitionSchema describes recipes using the registered node contracts.
func (t Service) DefinitionSchema() json.RawMessage {
	raw := json.RawMessage(`{"type":"object","properties":{"version":{"type":"integer","const":3},"name":{"type":"string","minLength":1,"maxLength":120},"inputs":{"type":"object","maxProperties":32,"additionalProperties":{"type":"object","properties":{"type":{"type":"string","enum":["string","integer","number","boolean","array","object"]},"required":{"type":"boolean"},"default":{}},"required":["type"],"additionalProperties":false}},"steps":{"type":"array","minItems":1,"maxItems":16,"items":{"type":"object","properties":{"id":{"type":"string","minLength":1,"maxLength":64},"tool":{"type":"string","enum":[]},"review":{"type":"boolean"},"for_each":{},"node_version":{"type":"integer","minimum":1},"retry":{"type":"object","properties":{"max_attempts":{"type":"integer","minimum":1,"maximum":3},"delay_ms":{"type":"integer","minimum":0,"maximum":10000}},"required":["max_attempts"],"additionalProperties":false},"depends_on":{"type":"array","items":{"type":"string"},"uniqueItems":true},"when":{},"merge":{"type":"string","enum":["all","any"]},"args":{"type":"object"}},"required":["id","tool","args"],"additionalProperties":false}}},"required":["version","name","steps"],"additionalProperties":false}`)
	var schema map[string]any
	_ = json.Unmarshal(raw, &schema)
	schema["properties"].(map[string]any)["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["tool"].(map[string]any)["enum"] = t.Registry.Names()
	schema["properties"].(map[string]any)["outputs"] = map[string]any{"type": "object", "maxProperties": 32, "description": "Named values returned by this recipe. Required for recipes used by workflow_call. Values may use $input and $step references."}
	schema["properties"].(map[string]any)["steps"].(map[string]any)["items"].(map[string]any)["properties"].(map[string]any)["id"].(map[string]any)["pattern"] = `^[^@/][^/]*$`
	data, _ := json.Marshal(schema)
	return data
}
