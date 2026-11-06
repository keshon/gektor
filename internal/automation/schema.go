package automation

import (
	"bytes"
	"encoding/json"
	"fmt"
	"github.com/santhosh-tekuri/jsonschema/v6"
	"sync"
)

var schemaMu sync.Mutex
var schemas = map[string]*jsonschema.Schema{}

type noRemote struct{}

func (noRemote) Load(url string) (any, error) {
	return nil, fmt.Errorf("external schema reference unavailable: %s", url)
}
func CompileSchema(raw json.RawMessage) (*jsonschema.Schema, error) {
	if len(raw) > 64<<10 {
		return nil, fmt.Errorf("schema exceeds 64 KiB")
	}
	key := string(raw)
	schemaMu.Lock()
	found := schemas[key]
	schemaMu.Unlock()
	if found != nil {
		return found, nil
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	c.UseLoader(noRemote{})
	if err = c.AddResource("urn:gektor:node", doc); err != nil {
		return nil, err
	}
	schema, err := c.Compile("urn:gektor:node")
	if err != nil {
		return nil, err
	}
	schemaMu.Lock()
	if len(schemas) >= 128 {
		clear(schemas)
	}
	schemas[key] = schema
	schemaMu.Unlock()
	return schema, nil
}
func ValidateJSON(value, raw json.RawMessage) error {
	schema, err := CompileSchema(raw)
	if err != nil {
		return fmt.Errorf("schema: %w", err)
	}
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(value))
	if err != nil {
		return fmt.Errorf("invalid_json: %w", err)
	}
	if err = schema.Validate(doc); err != nil {
		return fmt.Errorf("invalid_arguments: %w", err)
	}
	return nil
}
