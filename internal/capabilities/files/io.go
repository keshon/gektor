package files

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
)

func (s Service) path(name string) (string, error) {
	if s.WS == nil {
		return "", fmt.Errorf("workspace unavailable")
	}

	if name == "" {
		return "", fmt.Errorf("path is required")
	}

	rel, err := s.WS.Relative(name)
	if err != nil {
		return "", err
	}

	if rel == "." {
		return "", fmt.Errorf("path must name a file")
	}

	return filepath.ToSlash(rel), nil
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

type contextReaderAt struct {
	ctx       context.Context
	r         io.ReaderAt
	remaining int64
}

func (r *contextReaderAt) ReadAt(p []byte, offset int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	if int64(len(p)) > r.remaining {
		return 0, fmt.Errorf("ZIP read budget exceeded")
	}

	r.remaining -= int64(len(p))

	return r.r.ReadAt(p, offset)
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.r.Read(p)
}
