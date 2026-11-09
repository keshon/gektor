package automation

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ValidateLiteralPaths checks selected path fields before references are resolved.
// Object references are checked by template schema validation and by the ordinary
// operation contract after binding. The callback owns workspace path policy.
func ValidateLiteralPaths(raw json.RawMessage, fields []string, validate func(string) error) error {
	var args map[string]json.RawMessage
	if err := json.Unmarshal(raw, &args); err != nil {
		return err
	}
	var visit func(json.RawMessage) error
	visit = func(value json.RawMessage) error {
		var path string
		if json.Unmarshal(value, &path) == nil {
			if path == "" {
				return nil
			}
			for _, marker := range []string{"{$input", "{$step", "${input", "${step"} {
				if strings.Contains(path, marker) {
					return fmt.Errorf("reference-like path %q: use a JSON object reference", path)
				}
			}
			return validate(path)
		}
		var items []json.RawMessage
		if json.Unmarshal(value, &items) == nil {
			for _, item := range items {
				if err := visit(item); err != nil {
					return err
				}
			}
		}
		return nil
	}
	for _, field := range fields {
		if err := visit(args[field]); err != nil {
			return err
		}
	}
	return nil
}
