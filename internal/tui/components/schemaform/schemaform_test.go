package schemaform

import (
	"encoding/json"
	"testing"
)

func TestSchemaValuesPreserveTypesReferencesAndUnknowns(t *testing.T) {
	s := Parse(json.RawMessage(`{"properties":{"name":{"type":"string"},"count":{"type":"integer"},"data":{}},"required":["name"]}`))
	raw := json.RawMessage(`{"name":"hello","extra":9007199254740993}`)
	out, err := s.Values(raw, map[string]string{"name": "plain text", "count": "12", "data": `{"$step":"source"}`})
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]json.RawMessage
	_ = json.Unmarshal(out, &v)
	if string(v["extra"]) != "9007199254740993" || string(v["name"]) != `"plain text"` || string(v["count"]) != "12" {
		t.Fatal(string(out))
	}
	if _, err := s.Values(raw, map[string]string{"count": "oops"}); err == nil {
		t.Fatal("invalid JSON accepted")
	}
}
