package fileops

import (
	"archive/zip"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

type ZIPCreateRequest struct {
	Paths []string `json:"paths"`
	Path  string   `json:"path"`
}

type ZIPCreateResult struct {
	Path   string `json:"path"`
	Files  int    `json:"files"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

func (s Service) ValidateZIPCreate(in ZIPCreateRequest) (ZIPCreateRequest, error) {
	var err error
	in.Path, err = s.path(in.Path)
	if err != nil {
		return in, err
	}
	if !safeZIPName(in.Path) {
		return in, fmt.Errorf("ZIP destination must use a portable workspace path")
	}
	if len(in.Paths) < 1 || len(in.Paths) > 128 {
		return in, fmt.Errorf("paths must contain 1–128 explicit files")
	}
	in.Paths = append([]string(nil), in.Paths...)
	seen := map[string]bool{strings.ToLower(in.Path): true}
	spelling := map[string]string{}
	for i, name := range in.Paths {
		name, err = s.path(name)
		if err != nil {
			return in, err
		}
		key := strings.ToLower(name)
		if !safeZIPName(name) || seen[key] {
			return in, fmt.Errorf("unsafe, duplicate or destination source path %q", name)
		}
		seen[key], in.Paths[i] = true, name
		for part := name; part != "."; part = path.Dir(part) {
			key := strings.ToLower(part)
			if original, ok := spelling[key]; ok && original != part {
				return in, fmt.Errorf("ZIP source case collision at %q", part)
			}
			spelling[key] = part
		}
	}
	return in, nil
}

// CreateZIP archives only explicitly named regular files. Entry names retain
// their workspace-relative paths; folders are never discovered recursively.
func (s Service) CreateZIP(ctx context.Context, in ZIPCreateRequest) (result ZIPCreateResult, err error) {
	in, err = s.ValidateZIPCreate(in)
	if err != nil {
		return result, err
	}
	var sources []*os.File
	defer func() {
		for _, f := range sources {
			_ = f.Close()
		}
	}()
	var declared int64
	for _, name := range in.Paths {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		info, err := s.WS.Lstat(name)
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() {
			return result, fmt.Errorf("ZIP source %q must be a regular file, not a link or directory", name)
		}
		f, err := s.WS.Open(name)
		if err != nil {
			return result, err
		}
		sources = append(sources, f)
		info, err = f.Stat()
		if err != nil {
			return result, err
		}
		if !info.Mode().IsRegular() || info.Size() > MaxFileBytes-declared {
			return result, fmt.Errorf("ZIP sources exceed %d bytes or are not regular files", MaxFileBytes)
		}
		declared += info.Size()
	}
	f, err := s.WS.CreateExclusive(in.Path, 0600)
	if err != nil {
		return result, err
	}
	complete := false
	defer func() {
		_ = f.Close()
		if !complete {
			if e := s.WS.Remove(in.Path); e != nil && !errors.Is(e, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove incomplete ZIP %q: %w", in.Path, e))
			}
		}
	}()
	hash := sha256.New()
	output := &limitedWriter{writer: io.MultiWriter(f, hash), remaining: MaxFileBytes}
	w := zip.NewWriter(output)
	var copied int64
	for i, source := range sources {
		entry, err := w.CreateHeader(&zip.FileHeader{Name: in.Paths[i], Method: zip.Deflate})
		if err != nil {
			return result, err
		}
		n, err := io.Copy(entry, io.LimitReader(contextReader{ctx, source}, MaxFileBytes-copied+1))
		if err != nil {
			return result, err
		}
		copied += n
		if copied > MaxFileBytes {
			return result, fmt.Errorf("ZIP sources exceed %d bytes", MaxFileBytes)
		}
	}
	if err := w.Close(); err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := f.Sync(); err != nil {
		return result, err
	}
	if err := f.Close(); err != nil {
		return result, err
	}
	complete = true
	return ZIPCreateResult{Path: in.Path, Files: len(sources), Size: MaxFileBytes - output.remaining, SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}

type limitedWriter struct {
	writer    io.Writer
	remaining int64
}

func (w *limitedWriter) Write(p []byte) (int, error) {
	if int64(len(p)) > w.remaining {
		return 0, fmt.Errorf("ZIP output exceeds %d bytes", MaxFileBytes)
	}
	n, err := w.writer.Write(p)
	w.remaining -= int64(n)
	return n, err
}
