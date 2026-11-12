package media

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (s *Service) work() (string, error) { return os.MkdirTemp("", "gektor-media-") }

func (s *Service) stageInput(ctx context.Context, path, dir string, index int) (string, error) {
	f, err := s.WS.Open(path)
	if err != nil {
		return "", err
	}

	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return "", err
	}

	if !info.Mode().IsRegular() || info.Size() > MaxBytes {
		return "", fmt.Errorf("media input must be a regular file up to 256 MiB")
	}

	target := filepath.Join(dir, fmt.Sprintf("input-%d%s", index, filepath.Ext(path)))
	out, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}

	defer out.Close()

	_, err = copyBounded(ctx, out, f, MaxBytes)

	return target, err
}

func copyBounded(ctx context.Context, out io.Writer, in io.Reader, limit int64) (int64, error) {
	reader := &contextReader{ctx: ctx, r: in}
	n, err := io.Copy(out, io.LimitReader(reader, limit+1))
	if n > limit {
		return n, fmt.Errorf("media exceeds %d bytes", limit)
	}

	return n, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	return r.r.Read(p)
}

type outputTarget struct {
	Path     string
	MaxBytes int64
}

func (s *Service) publishEncoded(ctx context.Context, in outputTarget, file, provider string) (any, error) {
	info, err := os.Stat(file)
	if err != nil {
		return nil, err
	}

	if info.Size() >= in.MaxBytes {
		return nil, fmt.Errorf("output reached max_bytes; refusing a possibly truncated result")
	}

	return s.publish(ctx, in, file, provider)
}

func (s *Service) publish(ctx context.Context, in outputTarget, file, provider string) (result any, err error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	if containerExtension(in.Path) != containerExtension(file) {
		return nil, fmt.Errorf("output container is %s but path ends in %s; choose a matching path or explicitly convert", filepath.Ext(file), filepath.Ext(in.Path))
	}

	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}

	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > in.MaxBytes {
		return nil, fmt.Errorf("empty, nonregular or oversized media result")
	}

	out, err := s.WS.CreateExclusive(in.Path, 0600)
	if err != nil {
		return nil, err
	}

	ok := false
	defer func() {
		out.Close()
		if !ok {
			if e := s.WS.Remove(in.Path); e != nil && !os.IsNotExist(e) {
				err = errors.Join(err, fmt.Errorf("remove incomplete media output: %w", e))
			}
		}
	}()

	hash := sha256.New()
	size, err := copyBounded(ctx, io.MultiWriter(out, hash), f, in.MaxBytes)
	if err != nil {
		return nil, err
	}

	if err := out.Sync(); err != nil {
		return nil, err
	}

	if err := out.Close(); err != nil {
		return nil, err
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ok = true

	return map[string]any{
		"path":      in.Path,
		"size":      size,
		"sha256":    hex.EncodeToString(hash.Sum(nil)),
		"backend":   provider,
		"container": strings.TrimPrefix(filepath.Ext(file), "."),
	}, nil
}

func (s *Service) preflightOutput(path string) error {
	if path == "" {
		return fmt.Errorf("output path is required")
	}

	parent, err := s.WS.Stat(filepath.Dir(path))
	if err != nil {
		return err
	}

	if !parent.IsDir() {
		return fmt.Errorf("output parent must be a directory")
	}

	_, err = s.WS.Stat(path)
	if err == nil {
		return fmt.Errorf("destination exists")
	}

	if !os.IsNotExist(err) {
		return err
	}

	return nil
}

func readBounded(reader io.Reader, limit int64) ([]byte, error) {
	raw, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}

	if int64(len(raw)) > limit {
		return nil, fmt.Errorf("media data exceeds %d bytes", limit)
	}

	return raw, nil
}

// MP4 and M4A share a container; M4A names audio-only MP4 files.
func containerExtension(path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")

	switch ext {
	case "m4a":
		return "mp4"
	case "jpeg":
		return "jpg"
	}

	return ext
}
