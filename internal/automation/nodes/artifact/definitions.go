package artifact

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/workspace"
)

func Definitions(ws *workspace.Workspace) []automation.Node {
	return []automation.Node{
		{Name: "artifact_import", Version: 1, Description: "Copy a regular workspace file into immutable run-owned storage (maximum 16 MiB). Returns artifact metadata; downstream nodes reference its id.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"path":{"type":"string","minLength":1}},"required":["path"],"additionalProperties":false}`),
			Resources: func(raw json.RawMessage) ([]string, error) {
				var in struct{ Path string }
				if err := json.Unmarshal(raw, &in); err != nil {
					return nil, err
				}
				if ws == nil {
					return nil, fmt.Errorf("workspace unavailable")
				}
				rel, err := ws.Relative(in.Path)
				return []string{filepath.ToSlash(rel)}, err
			},
			Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct{ Path string }
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", err
				}
				a, err := automation.Artifacts(ctx)
				if err != nil {
					return "", err
				}
				if ws == nil {
					return "", fmt.Errorf("workspace unavailable")
				}
				f, err := ws.Open(in.Path)
				if err != nil {
					return "", err
				}
				defer f.Close()
				info, err := f.Stat()
				if err != nil {
					return "", err
				}
				if !info.Mode().IsRegular() {
					return "", fmt.Errorf("artifact import requires a regular file")
				}
				meta, err := a.Put(ctx, filepath.Base(in.Path), f)
				return encode(meta, err)
			}},
		{Name: "artifact_write", Version: 1, Description: "Save explicit UTF-8 text as an immutable run artifact. Returns metadata, not a workspace path.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"name":{"type":"string","minLength":1,"maxLength":255},"text":{"type":"string","maxLength":65536}},"required":["name","text"],"additionalProperties":false}`),
			Resources: func(raw json.RawMessage) ([]string, error) {
				var in struct{ Name string }
				err := json.Unmarshal(raw, &in)
				return []string{in.Name}, err
			},
			Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct{ Name, Text string }
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", err
				}
				a, err := automation.Artifacts(ctx)
				if err != nil {
					return "", err
				}
				meta, err := a.Put(ctx, in.Name, strings.NewReader(in.Text))
				return encode(meta, err)
			}},
		{Name: "artifact_read_text", Version: 1, ReadOnly: true, RetrySafe: true, Description: "Read up to 32 KiB of UTF-8 text from an artifact in this run. Rejects binary or larger files; returns text and metadata.",
			InputSchema: json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","minLength":1,"maxLength":64}},"required":["id"],"additionalProperties":false}`),
			Resources: func(raw json.RawMessage) ([]string, error) {
				var in struct{ ID string }
				err := json.Unmarshal(raw, &in)
				return []string{in.ID}, err
			},
			Execute: func(ctx context.Context, raw json.RawMessage) (string, error) {
				var in struct{ ID string }
				if err := json.Unmarshal(raw, &in); err != nil {
					return "", err
				}
				a, err := automation.Artifacts(ctx)
				if err != nil {
					return "", err
				}
				meta, data, err := a.Read(ctx, in.ID)
				if err != nil {
					return "", err
				}
				if len(data) > 32<<10 || !utf8.Valid(data) || strings.ContainsRune(string(data), 0) {
					return "", fmt.Errorf("artifact is not UTF-8 text of at most 32 KiB")
				}
				return encode(map[string]any{"artifact": meta, "text": string(data)}, nil)
			}},
	}
}

func encode(value any, err error) (string, error) {
	if err != nil {
		return "", err
	}
	b, err := json.Marshal(value)
	return string(b), err
}
