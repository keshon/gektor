// Package images provides rooted image operations using Pixelita.
package images

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/workspace"

	imagekit "github.com/keshon/pixelita"
)

// Inputs resolves explicit source grants supplied by a frontend.
type Inputs interface {
	IsReference(string) bool
	Open(context.Context, string) (*os.File, error)
}
type Service struct {
	WS     *workspace.Workspace
	Inputs Inputs
}
type WriteAuthorizer func(context.Context, string) error
type operation struct {
	Service
	Kind            string
	AuthorizeOutput WriteAuthorizer
}

func (s Service) isReference(path string) bool { return s.Inputs != nil && s.Inputs.IsReference(path) }
func (s Service) openInput(ctx context.Context, path string) (*os.File, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if s.Inputs != nil {
		return s.Inputs.Open(ctx, path)
	}
	return s.WS.Open(path)
}
func (t operation) Name() string { return "image_" + t.Kind }

type Request struct {
	Paths  []string `json:"paths"`
	Widths []int    `json:"widths,omitempty"`
	Format string   `json:"format,omitempty"`
	OutDir string   `json:"out_dir,omitempty"`
}

func (s Service) Validate(kind string, in Request) (Request, error) {
	switch kind {
	case "inspect", "preview", "variants", "compare":
	default:
		return in, fmt.Errorf("unknown image operation %q", kind)
	}
	if s.WS == nil {
		return in, fmt.Errorf("workspace unavailable")
	}
	if len(in.Paths) < 1 || len(in.Paths) > 16 || (kind == "compare" && len(in.Paths) != 2) {
		return in, fmt.Errorf("invalid image input count")
	}
	if (kind == "inspect" || kind == "compare") && (len(in.Widths) > 0 || in.Format != "" || in.OutDir != "") {
		return in, fmt.Errorf("unexpected image options")
	}
	if in.Format != "" && in.Format != "keep" && in.Format != "webp" {
		return in, fmt.Errorf("unsupported image format")
	}
	if len(in.Widths) > 8 {
		return in, fmt.Errorf("at most eight widths are supported")
	}
	seen := map[int]bool{}
	for _, width := range in.Widths {
		if width < 1 || width > 8192 || seen[width] {
			return in, fmt.Errorf("widths must be unique and within 1–8192")
		}
		seen[width] = true
	}
	if kind == "variants" && (len(in.Widths) == 0 || in.OutDir == "") {
		return in, fmt.Errorf("variants requires widths and out_dir")
	}
	in.Paths = append([]string(nil), in.Paths...)
	for i, path := range in.Paths {
		if s.isReference(path) {
			continue
		}
		if path == "" {
			return in, fmt.Errorf("input path is required")
		}
		rel, err := s.WS.Relative(path)
		if err != nil {
			return in, err
		}
		if rel == "." {
			return in, fmt.Errorf("input must name a file")
		}
		in.Paths[i] = filepath.ToSlash(rel)
	}
	if in.OutDir != "" {
		rel, err := s.WS.Relative(in.OutDir)
		if err != nil {
			return in, err
		}
		if rel == "." {
			return in, fmt.Errorf("output must name a new directory")
		}
		in.OutDir = filepath.ToSlash(rel)
	}
	return in, nil
}
func (s Service) Run(ctx context.Context, kind string, in Request, authorize WriteAuthorizer) (string, error) {
	t := operation{Service: s, Kind: kind, AuthorizeOutput: authorize}
	in, err := s.Validate(kind, in)
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
	if err := ctx.Err(); err != nil {
		return "", err
	}
	outcome := t.evaluateReport(in, rep, t.verifySources(ctx, staged.sources))
	if t.Kind == "preview" {
		rep.Notes = []string{"Preview only; no workspace outputs written."}
	}
	// Encoding and authorization finish before any workspace output is created.
	if t.Kind == "variants" && !outcome.blocked {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if t.AuthorizeOutput == nil {
			return "", fmt.Errorf("image output authorization unavailable")
		}
		if err := t.authorizeOutputs(ctx, in, rep); err != nil {
			return "", err
		}
		if err := publish(ctx, t.WS, filepath.Join(dir, "output"), in.OutDir); err != nil {
			outcome.publicationFailed = true
			outcome.failed = true
			outcome.err = err
			outcome.blocked = true
		}
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return t.formatReport(in, rep, staged, outcome)
}

func (t operation) process(ctx context.Context, in Request, dir string, paths []string) (*imagekit.Report, error) {
	var rep *imagekit.Report
	var err error
	switch t.Kind {
	case "inspect":
		rep, err = imagekit.Inspect(imagekit.InspectRequest{Context: ctx, Paths: paths, Jobs: 1})
	case "compare":
		rep, err = imagekit.Compare(imagekit.CompareRequest{A: paths[0], B: paths[1]})
		if rep != nil {
			rep.Notes = append(rep.Notes, "Pixel comparison only; object identity and semantic similarity are not measured. Different dimensions are resampled; perspective, lighting and aspect-ratio changes make these metrics unsuitable for identification.")
		}
	case "preview", "variants":
		rep, err = imagekit.Optimize(imagekit.OptimizeRequest{Context: ctx, Paths: paths, Widths: in.Widths, Format: in.Format, OutDir: filepath.Join(dir, "output"), Apply: t.Kind == "variants", Jobs: 1})
	default:
		return nil, fmt.Errorf("unknown image operation")
	}
	return rep, err
}
