package pixelita

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"github.com/keshon/gektor/internal/llm"
)

type stagedImages struct {
	paths   []string
	names   map[string]string
	sources []sourceEvidence
}

// stageInputs preserves equal basenames in separate scratch directories. The
// caller owns the scratch directory and removes it on every exit path.
func (t Operation) stageInputs(ctx context.Context, dir string, inputPaths []string) (stagedImages, error) {
	paths := make([]string, 0, len(inputPaths))
	names := map[string]string{}
	sources := make([]sourceEvidence, 0, len(inputPaths))
	for i, path := range inputPaths {
		if err := ctx.Err(); err != nil {
			return stagedImages{}, err
		}
		f, err := llm.OpenImage(ctx, path, t.WS.Open)
		if err != nil {
			return stagedImages{}, err
		}
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() || info.Size() > 64<<20 {
			f.Close()
			return stagedImages{}, fmt.Errorf("input must be a regular image file of at most 64 MiB: %s", path)
		}
		data, err := io.ReadAll(io.LimitReader(f, (64<<20)+1))
		f.Close()
		if err != nil {
			return stagedImages{}, err
		}
		if len(data) > 64<<20 {
			return stagedImages{}, fmt.Errorf("input exceeds 64 MiB: %s", path)
		}
		config, _, err := image.DecodeConfig(bytes.NewReader(data))
		if err != nil {
			return stagedImages{}, fmt.Errorf("inspect image %s: %w", path, err)
		}
		if config.Width <= 0 || config.Height <= 0 || int64(config.Width)*int64(config.Height) > 40_000_000 {
			return stagedImages{}, fmt.Errorf("image must contain at most 40 million pixels: %s", path)
		}
		// Separate directories preserve names without conflating equal basenames.
		sub := filepath.Join(dir, fmt.Sprint(i))
		if err := os.Mkdir(sub, 0700); err != nil {
			return stagedImages{}, err
		}
		name := filepath.Base(path)
		if llm.IsAttachment(path) {
			name = fmt.Sprintf("attachment-%d", i+1) + llm.ImageExtension(http.DetectContentType(data))
		}
		staged := filepath.Join(sub, name)
		if err := os.WriteFile(staged, data, 0600); err != nil {
			return stagedImages{}, err
		}
		paths = append(paths, staged)
		names[staged] = path
		sources = append(sources, sourceEvidence{Path: filepath.ToSlash(path), Before: fmt.Sprintf("%x", sha256.Sum256(data))})
	}
	return stagedImages{paths: paths, names: names, sources: sources}, nil
}

// verifySources records uncertainty as well as changed bytes. Missing hashes
// never count as evidence that the original was preserved.
func (t Operation) verifySources(ctx context.Context, sources []sourceEvidence) error {
	var sourceErr error
	for i := range sources {
		s := &sources[i]
		f, e := llm.OpenImage(ctx, s.Path, t.WS.Open)
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
	return sourceErr
}

// Unchanged is absent when an after-hash could not be obtained; absence is not
// evidence of preservation. Hashes compare source bytes, not just metadata.
type sourceEvidence struct {
	Path      string `json:"path"`
	Before    string `json:"sha256_before"`
	After     string `json:"sha256_after,omitempty"`
	Unchanged *bool  `json:"unchanged,omitempty"`
	Error     string `json:"error,omitempty"`
}
