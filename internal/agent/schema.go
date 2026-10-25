package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// Schemas are fixed for a registry's lifetime. Compile once, and never fetch
// remote references while validating a model-generated call.
var argumentSchemas sync.Map

type schemaResult struct {
	schema *jsonschema.Schema
	err    error
}
type noSchemaNetwork struct{}

// ValidateToolSchema compiles extension schemas without fetching references.
func ValidateToolSchema(schema json.RawMessage) error {
	var doc any
	if err := json.Unmarshal(schema, &doc); err != nil {
		return err
	}
	if _, ok := doc.(map[string]any); !ok {
		return fmt.Errorf("tool schema must be an object")
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(noSchemaNetwork{})
	if err := c.AddResource("urn:gektor:extension", doc); err != nil {
		return err
	}
	_, err := c.Compile("urn:gektor:extension")
	return err
}

func (noSchemaNetwork) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference is unavailable: %s", url)
}

func validateArguments(args, schema json.RawMessage) error {
	var value any
	if err := json.Unmarshal(args, &value); err != nil {
		return fmt.Errorf("invalid_json: %w", err)
	}
	if _, ok := value.(map[string]any); !ok {
		return fmt.Errorf("invalid_arguments: expected an object, received %s", string(args))
	}
	if len(schema) == 0 {
		return nil
	}
	key := string(schema)
	cached, ok := argumentSchemas.Load(key)
	if !ok {
		result := schemaResult{}
		var doc any
		result.err = json.Unmarshal(schema, &doc)
		if result.err == nil {
			c := jsonschema.NewCompiler()
			c.UseLoader(noSchemaNetwork{})
			result.err = c.AddResource("urn:gektor:arguments", doc)
			if result.err == nil {
				result.schema, result.err = c.Compile("urn:gektor:arguments")
			}
		}
		cached, _ = argumentSchemas.LoadOrStore(key, result)
	}
	result := cached.(schemaResult)
	if result.err != nil {
		return fmt.Errorf("tool_schema_error: %w", result.err)
	}
	if err := result.schema.Validate(value); err != nil {
		return fmt.Errorf("invalid_arguments: %s; correct the supplied arguments and reissue the call", strings.TrimSpace(err.Error()))
	}
	return nil
}
