package agenttools

import "encoding/json"

func workflowArgumentExamples() []json.RawMessage {
	return []json.RawMessage{
		json.RawMessage(`{"version":4,"name":"Double count","inputs":{"count":{"type":"integer","default":3}},"steps":[{"id":"double","tool":"script_transform","args":{"input":{"$input":"count"},"code":"return input * 2;"}}],"outputs":{"doubled":{"$step":"double"}}}`),
		json.RawMessage(`{"version":4,"name":"Prefix labels","inputs":{"labels":{"type":"array","default":["alpha","beta"]}},"steps":[{"id":"prefix","tool":"script_transform","args":{"input":{"$input":"labels"},"code":"return ['start'].concat(input);"}}],"outputs":{"labels":{"$step":"prefix"}}}`),
		json.RawMessage(`{"version":4,"name":"Read saved text","inputs":{"text":{"type":"string","default":"hello"}},"steps":[{"id":"save","tool":"artifact_write","args":{"name":"note.txt","text":{"$input":"text"}}},{"id":"read","tool":"artifact_read_text","args":{"id":{"$step":"save","pointer":"/id"}}}],"outputs":{"text":{"$step":"read","pointer":"/text"}}}`),
		json.RawMessage(`{"version":4,"name":"Required label","inputs":{"label":{"type":"string","required":true}},"steps":[{"id":"echo","tool":"script_transform","args":{"input":{"$input":"label"},"code":"return input;"}}],"outputs":{"label":{"$step":"echo"}}}`),
	}
}
