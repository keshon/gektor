package automation

import (
	"context"
	"encoding/json"
	"testing"
)

func TestRegistryRejectsInvalidDefinitions(t *testing.T) {
	n := Node{Name: "echo", Version: 3, InputSchema: json.RawMessage(`{"type":"object"}`), Resources: func(json.RawMessage) ([]string, error) { return nil, nil }, Execute: func(context.Context, json.RawMessage) (string, error) { return `{}`, nil }}
	if _, err := NewRegistry(n, n); err == nil {
		t.Fatal("duplicate accepted")
	}
	n.InputSchema = json.RawMessage(`{"$ref":"https://example.invalid/schema"}`)
	if _, err := NewRegistry(n); err == nil {
		t.Fatal("external schema accepted")
	}
}
func TestSchemaPreservesIntegerPrecision(t *testing.T) {
	schema := json.RawMessage(`{"type":"object","properties":{"value":{"const":9007199254740993}},"required":["value"]}`)
	if err := ValidateJSON(json.RawMessage(`{"value":9007199254740993}`), schema); err != nil {
		t.Fatal(err)
	}
	if err := ValidateJSON(json.RawMessage(`{"value":9007199254740992}`), schema); err == nil {
		t.Fatal("rounded unequal integer")
	}
}
