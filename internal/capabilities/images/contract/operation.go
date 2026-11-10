// Package contract shares image operation contracts between Agent and Workflows.
package contract

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/capabilities/images"
)

type OutputAuthorizer func(context.Context, string, json.RawMessage) error
type Operation struct {
	Service         images.Service
	Kind            string
	AuthorizeOutput OutputAuthorizer
}

func (o Operation) ReadOnly() bool { return o.Kind != "variants" }

func (o Operation) Name() string { return "image_" + o.Kind }

// RetrySafe declares whether a failed attempt may be repeated automatically.
func (o Operation) RetrySafe() bool {
	return o.Kind == "inspect" || o.Kind == "preview" || o.Kind == "compare"
}

func (o Operation) Description() string {
	switch o.Kind {
	case "inspect":
		return "Inspect image files and report dimensions, format and metadata."
	case "preview":
		return "Preview image optimization or width variants without writing. Planned filenames are in items[].output."
	case "variants":
		return "Write image width variants to a new output directory; sources and existing files are preserved. Names: <stem>-<width>w.<ext>; items[].output contains saved paths. Preview shows names without writing."
	default:
		return "Pixel fidelity comparison (PSNR/SSIM), for aligned versions of an image. Cannot identify objects or establish semantic similarity."
	}
}
func (o Operation) Schema() json.RawMessage {
	if o.Kind == "compare" {
		return json.RawMessage(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string","minLength":1},"minItems":2,"maxItems":2}},"required":["paths"],"additionalProperties":false}`)
	}
	properties := `"paths":{"type":"array","items":{"type":"string","minLength":1},"minItems":1,"maxItems":16}`
	required := `"paths"`
	if o.Kind != "inspect" {
		properties += `,"widths":{"type":"array","items":{"type":"integer","minimum":1,"maximum":8192},"minItems":1,"maxItems":8,"uniqueItems":true},"format":{"type":"string","enum":["keep","webp"]}`
	}
	if o.Kind == "variants" {
		properties += `,"out_dir":{"type":"string","minLength":1}`
		required += `,"widths","out_dir"`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}

func (o Operation) parse(raw json.RawMessage) (images.Request, error) {
	var in images.Request
	if err := automation.ValidateJSON(raw, o.Schema()); err != nil {
		return in, err
	}
	if err := json.Unmarshal(raw, &in); err != nil {
		return in, err
	}
	return o.Service.Validate(o.Kind, in)
}
func (o Operation) Validate(raw json.RawMessage) error { _, err := o.parse(raw); return err }
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
func (o Operation) OutputSchema() json.RawMessage {
	return json.RawMessage(`{"type":"object","properties":{"schema":{"type":"string"},"tool":{"type":"string"},"summary":{"type":"object","additionalProperties":{"type":"integer"}},"status":{"type":"string"},"sources":{"type":["array","null"],"items":{"type":"object","properties":{"path":{"type":"string"},"sha256_before":{"type":"string"},"sha256_after":{"type":"string"},"unchanged":{"type":"boolean"},"error":{"type":"string"}},"required":["path","sha256_before"]}},"items":{"type":["array","null"],"items":{"type":"object","properties":{"path":{"type":"string"},"output":{"type":"string","description":"Actual saved path for variants; planned basename for preview. Select the matching metrics.width."},"status":{"type":"string"},"written":{"type":"boolean"},"code":{"type":"string"},"reason":{"type":"string"},"bytesBefore":{"type":"integer"},"bytesAfter":{"type":"integer"},"metrics":{"type":"object","properties":{"width":{"type":"integer"},"height":{"type":"integer"}}}},"required":["path","status"]}},"recovery":{"type":"string"},"error":{"type":"string"},"failure_stage":{"type":"string"}},"required":["schema","tool","summary","status","sources"]}`)
}

func Names() []string { return []string{"inspect", "preview", "variants", "compare"} }

func (o Operation) ValidateTemplate(raw json.RawMessage) error {
	return automation.ValidateLiteralPaths(raw, []string{"paths", "out_dir"}, func(path string) error {
		if o.Service.WS == nil {
			return fmt.Errorf("workspace unavailable")
		}
		_, err := o.Service.WS.Relative(path)
		return err
	})
}
