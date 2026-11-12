package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

const maxArgumentSchemas = 128

// Dynamic tool catalogs must not retain every historical schema indefinitely.
var argumentSchemas = struct {
	sync.Mutex
	entries map[string]schemaResult
}{entries: map[string]schemaResult{}}

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
	argumentSchemas.Lock()
	result, ok := argumentSchemas.entries[key]
	argumentSchemas.Unlock()
	if !ok {
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
		argumentSchemas.Lock()
		if len(argumentSchemas.entries) >= maxArgumentSchemas {
			clear(argumentSchemas.entries)
		}
		argumentSchemas.entries[key] = result
		argumentSchemas.Unlock()
	}
	if result.err != nil {
		return fmt.Errorf("tool_schema_error: %w", result.err)
	}
	if err := result.schema.Validate(value); err != nil {
		return fmt.Errorf("invalid_arguments: %s; correct the supplied arguments and reissue the call", strings.TrimSpace(err.Error()))
	}
	return nil
}

// RejectUnknownFields checks the top-level fields of a tool argument object.
func RejectUnknownFields(args json.RawMessage, allowed ...string) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(args, &raw); err != nil {
		return fmt.Errorf("expected object, got invalid JSON: %w", err)
	}
	allow := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		allow[k] = true
	}
	for k := range raw {
		if !allow[k] {
			return fmt.Errorf("unknown field %q (expected: %s)", k, joinOr(allowed))
		}
	}
	return nil
}

func joinOr(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	case 2:
		return parts[0] + " or " + parts[1]
	default:
		out := parts[0]
		for i := 1; i < len(parts)-1; i++ {
			out += ", " + parts[i]
		}
		return out + ", or " + parts[len(parts)-1]
	}
}
