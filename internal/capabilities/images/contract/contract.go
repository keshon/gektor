// Package contract shares image operation contracts between Agent and Workflows.
package contract

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/capabilities/images"
	"github.com/keshon/gektor/internal/workflow"
)

type OutputAuthorizer func(context.Context, string, json.RawMessage) error
type Operation struct {
	Service         images.Service
	Kind            string
	AuthorizeOutput OutputAuthorizer
}

type operationSpec struct {
	kind        string
	description string
	readOnly    bool
	retrySafe   bool
	input       string
	output      string
}

func operations() []operationSpec {
	return []operationSpec{
		{
			kind:        "inspect",
			description: "Inspect image files and report dimensions, format and metadata.",
			readOnly:    true,
			retrySafe:   true,
			input: `{
  "type": "object",
  "properties": {
    "paths": {"type":"array","items":{"type":"string","minLength":1},"minItems":1,"maxItems":16}
  },
  "required": ["paths"],
  "additionalProperties": false
}`,
		},
		{
			kind:        "preview",
			description: "Preview image optimization or width variants without writing. Planned filenames are in items[].output.",
			readOnly:    true,
			retrySafe:   true,
			input: `{
  "type": "object",
  "properties": {
    "paths": {"type":"array","items":{"type":"string","minLength":1},"minItems":1,"maxItems":16},
    "widths": {"type":"array","items":{"type":"integer","minimum":1,"maximum":8192},"minItems":1,"maxItems":8,"uniqueItems":true},
    "format": {"type":"string","enum":["keep","webp"]}
  },
  "required": ["paths"],
  "additionalProperties": false
}`,
		},
		{
			kind:        "variants",
			description: "Write image width variants to a new output directory; sources and existing files are preserved. Names: <stem>-<width>w.<ext>; items[].output contains saved paths. Preview shows names without writing.",
			readOnly:    false,
			retrySafe:   false,
			input: `{
  "type": "object",
  "properties": {
    "paths": {"type":"array","items":{"type":"string","minLength":1},"minItems":1,"maxItems":16},
    "widths": {"type":"array","items":{"type":"integer","minimum":1,"maximum":8192},"minItems":1,"maxItems":8,"uniqueItems":true},
    "format": {"type":"string","enum":["keep","webp"]},
    "out_dir": {"type":"string","minLength":1}
  },
  "required": ["paths","widths","out_dir"],
  "additionalProperties": false
}`,
		},
		{
			kind:        "compare",
			description: "Pixel fidelity comparison (PSNR/SSIM), for aligned versions of an image. Cannot identify objects or establish semantic similarity.",
			readOnly:    true,
			retrySafe:   true,
			input: `{
  "type": "object",
  "properties": {
    "paths": {"type":"array","items":{"type":"string","minLength":1},"minItems":2,"maxItems":2}
  },
  "required": ["paths"],
  "additionalProperties": false
}`,
		},
	}
}

func Names() []string {
	specs := operations()
	names := make([]string, 0, len(specs))
	for _, spec := range specs {
		names = append(names, spec.kind)
	}
	return names
}

func (o Operation) spec() operationSpec {
	for _, spec := range operations() {
		if spec.kind == o.Kind {
			return spec
		}
	}
	return operationSpec{}
}

func (o Operation) Name() string            { return "image_" + o.Kind }
func (o Operation) Description() string     { return o.spec().description }
func (o Operation) ReadOnly() bool          { return o.spec().readOnly }
func (o Operation) RetrySafe() bool         { return o.spec().retrySafe }
func (o Operation) Schema() json.RawMessage { return json.RawMessage(o.spec().input) }

const outputSchema = `{
  "type": "object",
  "properties": {
    "schema": {"type":"string"},
    "tool": {"type":"string"},
    "summary": {"type":"object","additionalProperties":{"type":"integer"}},
    "status": {"type":"string"},
    "sources": {"type":["array","null"],"items":{"type":"object","properties":{"path":{"type":"string"},"sha256_before":{"type":"string"},"sha256_after":{"type":"string"},"unchanged":{"type":"boolean"},"error":{"type":"string"}},"required":["path","sha256_before"]}},
    "items": {"type":["array","null"],"items":{"type":"object","properties":{"path":{"type":"string"},"output":{"type":"string","description":"Actual saved path for variants; planned basename for preview. Select the matching metrics.width."},"status":{"type":"string"},"written":{"type":"boolean"},"code":{"type":"string"},"reason":{"type":"string"},"bytesBefore":{"type":"integer"},"bytesAfter":{"type":"integer"},"metrics":{"type":"object","properties":{"width":{"type":"integer"},"height":{"type":"integer"}}}},"required":["path","status"]}},
    "recovery": {"type":"string"},
    "error": {"type":"string"},
    "failure_stage": {"type":"string"}
  },
  "required": ["schema","tool","summary","status","sources"]
}`

func (Operation) OutputSchema() json.RawMessage { return json.RawMessage(outputSchema) }

func (o Operation) parse(raw json.RawMessage) (images.Request, error) {
	var in images.Request
	err := workflow.ValidateJSON(raw, o.Schema())
	if err != nil {
		return in, err
	}

	err = json.Unmarshal(raw, &in)
	if err != nil {
		return in, err
	}

	return o.Service.Validate(o.Kind, in)
}
func (o Operation) Validate(raw json.RawMessage) error {
	_, err := o.parse(raw)
	return err
}
func (o Operation) Resources(raw json.RawMessage) ([]string, error) {
	in, err := o.parse(raw)
	if err != nil {
		return nil, err
	}

	paths := append([]string{}, in.Paths...)
	if in.OutDir != "" {
		paths = append(paths, in.OutDir)
	}

	return paths, nil
}
func (o Operation) Run(ctx context.Context, raw json.RawMessage) (string, error) {
	in, err := o.parse(raw)
	if err != nil {
		return "", err
	}

	var authorize images.WriteAuthorizer
	if o.AuthorizeOutput != nil {
		authorize = func(ctx context.Context, path string) error { return o.AuthorizeOutput(ctx, path, raw) }
	}

	return o.Service.Run(ctx, o.Kind, in, authorize)
}

func (o Operation) ValidateTemplate(raw json.RawMessage) error {
	return workflow.ValidateLiteralPaths(raw, []string{"paths", "out_dir"}, func(path string) error {
		if o.Service.WS == nil {
			return fmt.Errorf("workspace unavailable")
		}

		_, err := o.Service.WS.Relative(path)
		return err
	})
}
