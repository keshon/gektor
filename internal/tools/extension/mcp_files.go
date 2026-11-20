package extension

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const mcpFileMax = 8 << 20

// fileFields is derived only from string arguments that explicitly carry base64.
func (t *MCPTool) fileFields() map[string]string {
	fields := map[string]string{}
	if t.workspace == "" {
		return fields
	}
	var schema struct {
		Properties map[string]struct {
			Type     string `json:"type"`
			Encoding string `json:"contentEncoding"`
			Format   string `json:"format"`
		} `json:"properties"`
	}
	err := json.Unmarshal(t.def.InputSchema, &schema)
	if err != nil {
		return fields
	}
	for name, property := range schema.Properties {
		if property.Format == "gektor-file" && property.Type == "string" && strings.HasSuffix(name, "_file") {
			fields[name] = strings.TrimSuffix(name, "_file")
			continue
		}
		_, collision := schema.Properties[name+"_file"]
		if property.Type == "string" && (property.Encoding == "base64" || name == "base64") && !collision {
			fields[name+"_file"] = name
		}
	}
	return fields
}

func (t *MCPTool) fileSchema() json.RawMessage {
	fields := t.fileFields()
	if len(fields) == 0 {
		return t.def.InputSchema
	}
	var schema map[string]any
	err := json.Unmarshal(t.def.InputSchema, &schema)
	if err != nil {
		return t.def.InputSchema
	}
	properties := schema["properties"].(map[string]any)
	required, _ := schema["required"].([]any)
	allOf, _ := schema["allOf"].([]any)
	for _, local := range slices.Sorted(maps.Keys(fields)) {
		remote := fields[local]
		properties[local] = map[string]any{"type": "string", "format": "gektor-file", "description": "Local workspace file to send as " + remote + "; GEKTOR reads and base64-encodes it without adding bytes to model context. Maximum 8 MiB. Supply this OR " + remote}
		wasRequired := slices.Contains(required, any(remote))
		required = slices.DeleteFunc(required, func(value any) bool { return value == remote })
		choices := []any{map[string]any{"required": []string{remote}, "not": map[string]any{"required": []string{local}}}, map[string]any{"required": []string{local}, "not": map[string]any{"required": []string{remote}}}}
		if !wasRequired {
			choices = append(choices, map[string]any{"not": map[string]any{"anyOf": []any{map[string]any{"required": []string{local}}, map[string]any{"required": []string{remote}}}}})
		}
		allOf = append(allOf, map[string]any{"oneOf": choices})
	}
	if len(required) == 0 {
		delete(schema, "required")
	} else {
		schema["required"] = required
	}
	schema["allOf"] = allOf
	raw, err := json.Marshal(schema)
	if err != nil {
		return t.def.InputSchema
	}
	return raw
}

func (t *MCPTool) filePath(path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("MCP file path must not be empty")
	}
	if filepath.IsAbs(path) {
		var err error
		path, err = filepath.Rel(t.workspace, path)
		if err != nil {
			return "", err
		}
	}
	if !filepath.IsLocal(path) {
		return "", fmt.Errorf("MCP file must be inside the workspace")
	}
	return path, nil
}

func (t *MCPTool) Resources(args json.RawMessage) ([]string, error) {
	var params map[string]any
	err := json.Unmarshal(args, &params)
	if err != nil {
		return nil, err
	}
	var resources []string
	for local := range t.fileFields() {
		value, supplied := params[local]
		if !supplied {
			continue
		}
		path, ok := value.(string)
		if !ok {
			return nil, fmt.Errorf("%s must be a file path", local)
		}
		relative, err := t.filePath(path)
		if err != nil {
			return nil, err
		}
		resources = append(resources, filepath.Join(t.workspace, relative))
	}
	slices.Sort(resources)
	return resources, nil
}

func (t *MCPTool) expandFiles(params map[string]any) error {
	for local, remote := range t.fileFields() {
		value, supplied := params[local]
		if !supplied {
			continue
		}
		if _, collision := params[remote]; collision {
			return fmt.Errorf("supply either %s or %s, not both", local, remote)
		}
		path, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a file path", local)
		}
		encoded, err := t.encodeFile(path)
		if err != nil {
			return err
		}
		delete(params, local)
		params[remote] = encoded
	}
	return nil
}

func (t *MCPTool) encodeFile(path string) (string, error) {
	relative, err := t.filePath(path)
	if err != nil {
		return "", err
	}
	root, err := os.OpenRoot(t.workspace)
	if err != nil {
		return "", err
	}
	defer root.Close()
	file, err := root.Open(relative)
	if err != nil {
		return "", err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > mcpFileMax {
		return "", fmt.Errorf("MCP upload requires a regular file of at most 8 MiB")
	}
	contents, err := io.ReadAll(io.LimitReader(file, mcpFileMax+1))
	if err != nil {
		return "", err
	}
	if len(contents) > mcpFileMax {
		return "", fmt.Errorf("MCP upload exceeds 8 MiB")
	}
	return base64.StdEncoding.EncodeToString(contents), nil
}
