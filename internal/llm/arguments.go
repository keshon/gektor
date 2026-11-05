package llm

import "encoding/json"

func NormalizeArguments(raw json.RawMessage) json.RawMessage {
	var asString string
	if json.Unmarshal(raw, &asString) == nil {
		return json.RawMessage(asString)
	}
	return raw
}
