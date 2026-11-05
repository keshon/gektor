// Package pixelita adapts the image engine to rooted workspace access.
package pixelita

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/workspace"
	imagekit "github.com/keshon/pixelita"
)

type Tool struct {
	WS              *workspace.Workspace
	Operation       string
	AuthorizeOutput OutputAuthorizer
}

type OutputAuthorizer func(context.Context, string, json.RawMessage) error

func (t Tool) ReadOnly() bool { return t.Operation != "variants" }

func (t Tool) Name() string { return "image_" + t.Operation }
func (t Tool) Mode() agent.ToolMode {
	if t.Operation == "variants" {
		return agent.Exclusive
	}
	return agent.Concurrent
}
func (t Tool) Description() string {
	switch t.Operation {
	case "inspect":
		return "Inspect image files and report dimensions, format and metadata."
	case "preview":
		return "Preview image optimization or width variants without writing."
	case "variants":
		return "Write image width variants to a new output directory; sources and existing files are preserved."
	default:
		return "Compare two images with PSNR and SSIM; measurements do not replace visual review."
	}
}
func (t Tool) Schema() json.RawMessage {
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

func (t Tool) Validate(args json.RawMessage) (Arguments, error) {
	var in Arguments
	if err := agent.NewRegistry(t).ValidateCall(t.Name(), args); err != nil {
		return in, err
	}
	if err := json.Unmarshal(args, &in); err != nil {
		return in, err
	}
	if t.WS == nil {
		return in, fmt.Errorf("workspace unavailable")
	}
	for _, path := range append(append([]string{}, in.Paths...), in.OutDir) {
		if path == "" {
			continue
		}
		if _, err := t.WS.Relative(path); err != nil {
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

func (t Tool) Run(ctx context.Context, args json.RawMessage) (string, error) {
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
	paths := make([]string, 0, len(in.Paths))
	names := map[string]string{}
	sources := make([]SourceEvidence, 0, len(in.Paths))
	for i, path := range in.Paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		f, err := t.WS.Open(path)
		if err != nil {
			return "", err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
			f.Close()
			return "", fmt.Errorf("input must be a regular image file of at most 64 MiB: %s", path)
		}
		data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		f.Close()
		if err != nil {
			return "", err
		}
		if len(data) > 64<<20 {
			return "", fmt.Errorf("input exceeds 64 MiB: %s", path)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return "", fmt.Errorf("inspect image %s: %w", path, err)
		}
		if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
			return "", fmt.Errorf("image must contain at most 40 million pixels: %s", path)
		}
		// Separate directories preserve names without conflating equal basenames.
		sub := filepath.Join(dir, fmt.Sprint(i))
		if err := os.Mkdir(sub, 0700); err != nil {
			return "", err
		}
		staged := filepath.Join(sub, filepath.Base(path))
		if err := os.WriteFile(staged, data, 0600); err != nil {
			return "", err
		}
		paths = append(paths, staged)
		names[staged] = path
		sources = append(sources, SourceEvidence{Path: filepath.ToSlash(path), Before: fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	var rep *imagekit.Report
	switch t.Operation {
	case "inspect":
		rep, err = imagekit.Inspect(imagekit.InspectRequest{Context: ctx, Paths: paths, Jobs: 1})
	case "compare":
		rep, err = imagekit.Compare(imagekit.CompareRequest{A: paths[0], B: paths[1]})
	case "preview", "variants":
		rep, err = imagekit.Optimize(imagekit.OptimizeRequest{Context: ctx, Paths: paths, Widths: in.Widths, Format: in.Format, OutDir: filepath.Join(dir, "output"), Apply: t.Operation == "variants", Jobs: 1})
	default:
		return "", fmt.Errorf("unknown image operation")
	}
	if err != nil {
		return "", err
	}
	var sourceErr error
	for i := range sources {
		s := &sources[i]
		f, e := t.WS.Open(s.Path)
		if e == nil {
			h := sha256.New()
			var n int64
			n, e = io.Copy(h, io.LimitReader(f, (64<<20)+1))
			f.Close()
			if e == nil && n > 64<<20 {
				e = fmt.Errorf("source exceeds size limit")
			}
			if e == nil {
				s.After = fmt.Sprintf("%x", h.Sum(nil))
				same := s.Before == s.After
				s.Unchanged = &same
				if !same {
					e = fmt.Errorf("source changed during operation")
				}
			}
		}
		if e != nil {
			s.Error = e.Error()
			sourceErr = fmt.Errorf("source verification %s: %w", s.Path, e)
		}
	}
	failed := rep.Summary.Failed > 0
	blocked := failed
	for _, item := range rep.Items {
		if item.Code == "unsupported_metadata" {
			blocked = true
		}
	}
	if len(in.Widths) > 0 && rep.Summary.Skipped > 0 {
		blocked = true
	}
	if t.Operation == "variants" && rep.Summary.Changed != len(in.Paths)*len(in.Widths) {
		blocked = true
	}
	var operationErr error
	if sourceErr != nil {
		failed = true
		blocked = true
		operationErr = sourceErr
	}
	if blocked {
		reason := "requested outputs were not produced"
		for _, it := range rep.Items {
			if it.Status == "failed" || (!failed && it.Status == "skipped") {
				reason = it.Code + ": " + it.Reason
				break
			}
		}
		if operationErr == nil {
			operationErr = fmt.Errorf("image %s blocked: %s; no workspace outputs written", t.Operation, reason)
		}
	}
	if t.Operation == "preview" {
		rep.Notes = []string{"Preview only; no workspace outputs written."}
	}
	// Existing outputs are never reused. Encoding finishes before publication.
	publicationFailed := false
	if t.Operation == "variants" && !blocked {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if t.AuthorizeOutput == nil {
			return "", fmt.Errorf("image output authorization unavailable")
		}
		// Authorize actual planned filenames, not just their parent directory.
		for _, item := range rep.Items {
			destination := filepath.Join(in.OutDir, filepath.Base(item.Output))
			if err := t.AuthorizeOutput(ctx, destination, args); err != nil {
				return "", err
			}
		}
		if err := publish(ctx, t.WS, filepath.Join(dir, "output"), in.OutDir); err != nil {
			publicationFailed = true
			failed = true
			operationErr = err
			blocked = true
		}
	}
	for i := range rep.Items {
		it := &rep.Items[i]
		if blocked && t.Operation == "variants" {
			it.Written = false
			it.Output = ""
			if it.Status == "done" {
				it.Status = "skipped"
				it.Code = "batch_not_published"
				it.Reason = "batch was not published; see operation error"
				if publicationFailed {
					it.Status = "not_published"
					it.Code = "publication_failed"
					it.Reason = operationErr.Error()
				}
			}
		}
		if path, ok := names[it.Path]; ok {
			it.Path = filepath.ToSlash(path)
		}
		if it.Output != "" {
			if t.Operation == "variants" {
				it.Output = filepath.ToSlash(filepath.Join(in.OutDir, filepath.Base(it.Output)))
			} else {
				it.Output = filepath.Base(it.Output)
			}
		}
	}
	rep.Finish()
	status := "inspected"
	if t.Operation == "compare" {
		status = "compared"
	}
	if t.Operation == "variants" {
		status = "written"
	}
	if blocked {
		status = "blocked"
	} else if t.Operation == "preview" {
		status = "previewed"
	}
	if failed {
		status = "failed"
	}
	recovery := ""
	if blocked {
		recovery = "Report the item restriction; do not retry unchanged arguments or invent unsupported recovery commands."
		for _, item := range rep.Items {
			if item.Code == "unsupported_metadata" {
				recovery = "Capability limitation: no supported workaround is exposed for this metadata. keep/apply/retry do not help. Report the blocker; do not invent conversion commands or strip profiles automatically."
				break
			}
		}
	}
	if failed {
		recovery = "Report the operation error. For an existing destination, choose a fresh output directory only if authorized; do not overwrite it or retry unchanged arguments."
	}
	failureStage, errorText := "", ""
	if operationErr != nil {
		errorText = operationErr.Error()
		failureStage = "processing"
	}
	if publicationFailed {
		failureStage = "publication"
		recovery = "Encoding succeeded; publication failed. Read the operation error and preserve existing files."
		if errors.Is(operationErr, os.ErrExist) {
			recovery += " Choose a fresh output directory only if authorized."
		}
	}
	// Native clients see operation outcomes, rather than CLI accounting labels
	// such as skipped/headers_only for a successful inspection.
	summary := map[string]int{"files": len(rep.Items)}
	for i := range rep.Items {
		it := &rep.Items[i]
		switch {
		case t.Operation == "inspect" && it.Code == "headers_only":
			it.Status = "inspected"
		case it.Status == "would":
			it.Status = "previewed"
		case it.Status == "done":
			it.Status = "written"
			if t.Operation == "compare" {
				it.Status = "compared"
			}
		case it.Code == "unsupported_metadata":
			it.Status = "blocked"
		}
		summary[string(it.Status)]++
	}
	data, err := json.Marshal(struct {
		*imagekit.Report
		Schema       string           `json:"schema"`
		Tool         string           `json:"tool"`
		Summary      map[string]int   `json:"summary"`
		Status       string           `json:"status"`
		Recovery     string           `json:"recovery,omitempty"`
		Sources      []SourceEvidence `json:"sources"`
		FailureStage string           `json:"failure_stage,omitempty"`
		Error        string           `json:"error,omitempty"`
	}{rep, "gektor.images.v1", t.Name(), summary, status, recovery, sources, failureStage, errorText})
	if err != nil {
		return "", err
	}
	return string(data), operationErr
}

// Unchanged is absent when an after-hash could not be obtained; absence is not
// evidence of preservation. Hashes compare source bytes, not just metadata.
type SourceEvidence struct {
	Path      string `json:"path"`
	Before    string `json:"sha256_before"`
	After     string `json:"sha256_after,omitempty"`
	Unchanged *bool  `json:"unchanged,omitempty"`
	Error     string `json:"error,omitempty"`
}

func publish(ctx context.Context, ws *workspace.Workspace, source, destination string) (err error) {
	if err := ctx.Err(); err != nil {
		return err
	}
	entries, err := os.ReadDir(source)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return fmt.Errorf("no variants were produced")
	}
	parent := filepath.Dir(destination)
	if err := ws.MkdirAll(parent, 0755); err != nil {
		return err
	}
	if err := ws.Mkdir(destination, 0755); err != nil {
		return fmt.Errorf("reserve new output directory: %w", err)
	}
	var created []string
	complete := false
	defer func() {
		if !complete {
			for _, path := range created {
				if e := ws.Remove(path); e != nil {
					err = errors.Join(err, fmt.Errorf("cleanup %s: %w", path, e))
				}
			}
			if e := ws.Remove(destination); e != nil {
				err = errors.Join(err, fmt.Errorf("cleanup directory %s: %w", destination, e))
			}
		}
	}()
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		if e.IsDir() || strings.ContainsAny(e.Name(), "/\\") {
			return fmt.Errorf("unexpected output entry")
		}
		data, err := os.ReadFile(filepath.Join(source, e.Name()))
		if err != nil {
			return err
		}
		path := filepath.Join(destination, e.Name())
		f, err := ws.CreateExclusive(path, 0600)
		if err != nil {
			return err
		}
		created = append(created, path)
		_, writeErr := f.Write(data)
		closeErr := f.Close()
		if err := errors.Join(writeErr, closeErr); err != nil {
			return err
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	complete = true
	return nil
}

// Decode accepts one JSON value, rejecting accidental concatenated arguments.
func Decode(data []byte, out any) error {
	d := json.NewDecoder(bytes.NewReader(data))
	d.DisallowUnknownFields()
	if err := d.Decode(out); err != nil {
		return err
	}
	if err := d.Decode(new(any)); err != io.EOF {
		return fmt.Errorf("expected one JSON object")
	}
	return nil
}
