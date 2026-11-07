package fileops

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"sort"
	"strings"
)

type ZIPExtractRequest struct {
	Path     string `json:"path"`
	OutDir   string `json:"out_dir"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
}

type ZIPExtractResult struct {
	Path      string `json:"path"`
	Directory string `json:"directory"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
}

type WriteAuthorizer func(context.Context, string) error

func (s Service) ValidateZIPExtract(in ZIPExtractRequest) (ZIPExtractRequest, error) {
	var err error
	in.Path, err = s.path(in.Path)
	if err != nil {
		return in, err
	}
	in.OutDir, err = s.path(in.OutDir)
	if err != nil {
		return in, err
	}
	if in.MaxBytes == 0 {
		in.MaxBytes = MaxFileBytes
	}
	if in.MaxBytes < 1 || in.MaxBytes > MaxFileBytes {
		return in, fmt.Errorf("max_bytes must be 1–%d", MaxFileBytes)
	}
	return in, nil
}

type extractPlan struct {
	dirs  []string
	files []*zip.File
}

func planZIP(ctx context.Context, z *zip.Reader, maxBytes int64) (extractPlan, error) {
	var plan extractPlan
	explicit, names := map[string]bool{}, map[string]bool{} // names maps case-folded names to directory status.
	spelling := map[string]string{}
	var declared uint64
	for _, entry := range z.File {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		key := strings.ToLower(name)
		dir := entry.FileInfo().IsDir()
		if !safeZIPName(name) || explicit[key] {
			return plan, fmt.Errorf("unsafe or duplicate ZIP entry %q", entry.Name)
		}
		if original, exists := spelling[key]; exists && original != name {
			return plan, fmt.Errorf("ZIP case collision at %q", name)
		}
		if entry.Flags&1 != 0 || (!entry.Mode().IsRegular() && !dir) || entry.Mode()&os.ModeSymlink != 0 {
			return plan, fmt.Errorf("encrypted or special ZIP entry %q is unsupported", entry.Name)
		}
		if entry.Method != zip.Store && entry.Method != zip.Deflate {
			return plan, fmt.Errorf("unsupported ZIP compression for %q", entry.Name)
		}
		if dir && (entry.UncompressedSize64 != 0 || !strings.HasSuffix(entry.Name, "/")) {
			return plan, fmt.Errorf("invalid ZIP directory %q", entry.Name)
		}
		if priorDir, exists := names[key]; exists && (!priorDir || !dir) {
			return plan, fmt.Errorf("ZIP file/directory collision at %q", name)
		}
		explicit[key] = true
		if _, exists := names[key]; !exists {
			names[key], spelling[key] = dir, name
			if dir {
				plan.dirs = append(plan.dirs, name)
			}
		}
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			parentKey := strings.ToLower(parent)
			if isDir, exists := names[parentKey]; exists {
				if !isDir || spelling[parentKey] != parent {
					return plan, fmt.Errorf("ZIP file/directory or case collision at %q", parent)
				}
			} else {
				names[parentKey], spelling[parentKey] = true, parent
				plan.dirs = append(plan.dirs, parent)
			}
		}
		if len(names) > MaxZIPEntries {
			return plan, fmt.Errorf("ZIP exceeds %d output paths", MaxZIPEntries)
		}
		if !dir {
			if entry.UncompressedSize64 > uint64(maxBytes)-declared {
				return plan, fmt.Errorf("expanded ZIP exceeds %d bytes", maxBytes)
			}
			declared += entry.UncompressedSize64
			plan.files = append(plan.files, entry)
		}
	}
	sort.Slice(plan.dirs, func(i, j int) bool {
		a, b := plan.dirs[i], plan.dirs[j]
		if strings.Count(a, "/") != strings.Count(b, "/") {
			return strings.Count(a, "/") < strings.Count(b, "/")
		}
		return a < b
	})
	return plan, nil
}

// ExtractZIP validates and authorizes every generated path before creating the
// destination. The output is confined to a new directory handle, not path joins
// opened through the process working directory. No archive permissions survive.
func (s Service) ExtractZIP(ctx context.Context, in ZIPExtractRequest, authorize WriteAuthorizer) (result ZIPExtractResult, err error) {
	in, err = s.ValidateZIPExtract(in)
	if err != nil {
		return result, err
	}
	if authorize == nil {
		return result, fmt.Errorf("ZIP output authorization unavailable")
	}
	if _, e := s.WS.Lstat(in.OutDir); e == nil {
		return result, fmt.Errorf("extraction destination already exists")
	} else if !errors.Is(e, os.ErrNotExist) {
		return result, e
	}
	f, z, err := s.openZIP(ctx, in.Path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	plan, err := planZIP(ctx, z, in.MaxBytes)
	if err != nil {
		return result, err
	}
	for _, dir := range plan.dirs {
		if err := authorize(ctx, path.Join(in.OutDir, dir)); err != nil {
			return result, err
		}
	}
	for _, entry := range plan.files {
		if err := authorize(ctx, path.Join(in.OutDir, entry.Name)); err != nil {
			return result, err
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if err := s.WS.Mkdir(in.OutDir, 0700); err != nil {
		return result, err
	}
	root, err := s.WS.OpenRoot(in.OutDir)
	if err != nil {
		if e := s.WS.Remove(in.OutDir); e != nil && !errors.Is(e, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove incomplete extraction directory: %w", e))
		}
		return result, err
	}
	var created []string
	complete := false
	defer func() {
		if !complete {
			for i := len(created) - 1; i >= 0; i-- {
				if e := root.Remove(created[i]); e != nil && !errors.Is(e, os.ErrNotExist) {
					err = errors.Join(err, fmt.Errorf("remove incomplete extraction %q: %w", created[i], e))
				}
			}
		}
		_ = root.Close()
		if !complete {
			if e := s.WS.Remove(in.OutDir); e != nil && !errors.Is(e, os.ErrNotExist) {
				err = errors.Join(err, fmt.Errorf("remove incomplete extraction directory: %w", e))
			}
		}
	}()
	for _, dir := range plan.dirs {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		if err := root.Mkdir(dir, 0700); err != nil {
			return result, err
		}
		created = append(created, dir)
	}
	var expanded int64
	for _, entry := range plan.files {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		dst, err := root.OpenFile(entry.Name, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return result, err
		}
		created = append(created, entry.Name)
		src, err := entry.Open()
		if err != nil {
			_ = dst.Close()
			return result, err
		}
		n, copyErr := io.Copy(dst, io.LimitReader(contextReader{ctx, src}, in.MaxBytes-expanded+1))
		closeErr := src.Close()
		if copyErr == nil && closeErr != nil {
			copyErr = closeErr
		}
		if copyErr == nil {
			copyErr = dst.Sync()
		}
		if e := dst.Close(); copyErr == nil {
			copyErr = e
		}
		if copyErr != nil {
			return result, fmt.Errorf("extract %q: %w", entry.Name, copyErr)
		}
		expanded += n
		if expanded > in.MaxBytes {
			return result, fmt.Errorf("expanded ZIP exceeds %d bytes", in.MaxBytes)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	complete = true
	return ZIPExtractResult{Path: in.Path, Directory: in.OutDir, Files: len(plan.files), Bytes: expanded}, nil
}
