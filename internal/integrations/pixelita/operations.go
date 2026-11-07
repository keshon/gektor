// Package pixelita adapts the image engine to rooted workspace access.
package pixelita

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/automation"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/workspace"
	imagekit "github.com/keshon/pixelita"
)

type Operation struct {
	WS              *workspace.Workspace
	Operation       string
	AuthorizeOutput OutputAuthorizer
}

type OutputAuthorizer func(context.Context, string, json.RawMessage) error

func (t Operation) ReadOnly() bool { return t.Operation != "variants" }

func (t Operation) Name() string { return "image_" + t.Operation }
func (t Operation) Description() string {
	switch t.Operation {
	case "inspect":
		return "Inspect image files and report dimensions, format and metadata."
	case "preview":
		return "Preview image optimization or width variants without writing."
	case "variants":
		return "Write image width variants to a new output directory; sources and existing files are preserved."
	default:
		return "Pixel fidelity comparison (PSNR/SSIM), for aligned versions of an image. Cannot identify objects or establish semantic similarity."
	}
}
func (t Operation) Schema() json.RawMessage {
	if t.Operation == "compare" {
		return json.RawMessage(`{"type":"object","properties":{"paths":{"type":"array","items":{"type":"string","minLength":1},"minItems":2,"maxItems":2}},"required":["paths"],"additionalProperties":false}`)
	}
	properties := `"paths":{"type":"array","items":{"type":"string","minLength":1},"minItems":1,"maxItems":16}`
	required := `"paths"`
	if t.Operation != "inspect" {
		properties += `,"widths":{"type":"array","items":{"type":"integer","minimum":1,"maximum":8192},"minItems":1,"maxItems":8,"uniqueItems":true},"format":{"type":"string","enum":["keep","webp"]}`
	}
	if t.Operation == "variants" {
		properties += `,"out_dir":{"type":"string","minLength":1}`
		required += `,"widths","out_dir"`
	}
	return json.RawMessage(`{"type":"object","properties":{` + properties + `},"required":[` + required + `],"additionalProperties":false}`)
}

type Arguments struct {
	Paths  []string `json:"paths"`
	Widths []int    `json:"widths,omitempty"`
	Format string   `json:"format,omitempty"`
	OutDir string   `json:"out_dir,omitempty"`
}

func (t Operation) Validate(args json.RawMessage) (Arguments, error) {
	var in Arguments
	if err := automation.ValidateJSON(args, t.Schema()); err != nil {
		return in, err
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return in, err
	}
	if t.WS == nil {
		return in, fmt.Errorf("workspace unavailable")
	}
	for _, path := range in.Paths {
		if llm.IsAttachment(path) {
			continue
		}
		if path == "" {
			continue
		}
		if _, err := t.WS.Relative(path); err != nil {
			return in, err
		}
	}
	if in.OutDir != "" {
		if _, err := t.WS.Relative(in.OutDir); err != nil {
			return in, err
		}
	}
	return in, nil
}

// Resources is also used by workflow authorization; nested steps cannot hide paths.
func Resources(args json.RawMessage) ([]string, error) {
	var in Arguments
	if err := json.Unmarshal(args, &in); err != nil {
		return nil, err
	}
	paths := append([]string{}, in.Paths...)
	if in.OutDir != "" {
		paths = append(paths, in.OutDir)
	}
	return paths, nil
}

func (t Operation) Run(ctx context.Context, args json.RawMessage) (string, error) {
	in, err := t.Validate(args)
	if err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "gektor-image-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	staged, err := t.stageInputs(ctx, dir, in.Paths)
	if err != nil {
		return "", err
	}
	rep, err := t.process(ctx, in, dir, staged.paths)
	if err != nil {
		return "", err
	}
	outcome := t.evaluateReport(in, rep, t.verifySources(ctx, staged.sources))
	if t.Operation == "preview" {
		rep.Notes = []string{"Preview only; no workspace outputs written."}
	}
	// Encoding and authorization finish before any workspace output is created.
	if t.Operation == "variants" && !outcome.blocked {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if t.AuthorizeOutput == nil {
			return "", fmt.Errorf("image output authorization unavailable")
		}
		if err := t.authorizeOutputs(ctx, args, in, rep); err != nil {
			return "", err
		}
		if err := publish(ctx, t.WS, filepath.Join(dir, "output"), in.OutDir); err != nil {
			outcome.publicationFailed = true
			outcome.failed = true
			outcome.err = err
			outcome.blocked = true
		}
	}
	return t.formatReport(in, rep, staged, outcome)
}

func (t Operation) process(ctx context.Context, in Arguments, dir string, paths []string) (*imagekit.Report, error) {
	var rep *imagekit.Report
	var err error
	switch t.Operation {
	case "inspect":
		rep, err = imagekit.Inspect(imagekit.InspectRequest{Context: ctx, Paths: paths, Jobs: 1})
	case "compare":
		rep, err = imagekit.Compare(imagekit.CompareRequest{A: paths[0], B: paths[1]})
		if rep != nil {
			rep.Notes = append(rep.Notes, "Pixel comparison only; object identity and semantic similarity are not measured. Different dimensions are resampled; perspective, lighting and aspect-ratio changes make these metrics unsuitable for identification.")
		}
	case "preview", "variants":
		rep, err = imagekit.Optimize(imagekit.OptimizeRequest{Context: ctx, Paths: paths, Widths: in.Widths, Format: in.Format, OutDir: filepath.Join(dir, "output"), Apply: t.Operation == "variants", Jobs: 1})
	default:
		return nil, fmt.Errorf("unknown image operation")
	}
	return rep, err
}
