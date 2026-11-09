package files

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

type ArchiveExtractRequest struct {
	Path     string `json:"path"`
	OutDir   string `json:"out_dir"`
	MaxBytes int64  `json:"max_bytes,omitempty"`
	Format   string `json:"format,omitempty"`
}

type ArchiveExtractResult struct {
	Path      string `json:"path"`
	Directory string `json:"directory"`
	Files     int    `json:"files"`
	Bytes     int64  `json:"bytes"`
}

type WriteAuthorizer func(context.Context, string) error

func (s Service) ValidateArchiveExtract(in ArchiveExtractRequest) (ArchiveExtractRequest, error) {
	var err error
	if err = validateArchiveFormat(in.Format); err != nil {
		return in, err
	}
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
	files []archiveEntry
}

type archiveEntry struct {
	Name                                   string
	Size                                   uint64
	Directory, Special, Encrypted, Symlink bool
	Open                                   func() (io.ReadCloser, error)
}

func zipEntries(z *zip.Reader) []archiveEntry {
	entries := make([]archiveEntry, 0, len(z.File))
	for _, f := range z.File {
		entries = append(entries, archiveEntry{Name: f.Name, Size: f.UncompressedSize64, Directory: f.FileInfo().IsDir(),
			Special:   (!f.Mode().IsRegular() && !f.FileInfo().IsDir()) || f.Mode()&os.ModeSymlink != 0 || (f.Method != zip.Store && f.Method != zip.Deflate),
			Encrypted: f.Flags&1 != 0, Symlink: f.Mode()&os.ModeSymlink != 0, Open: f.Open})
	}
	return entries
}

func planArchive(ctx context.Context, entries []archiveEntry, maxBytes int64) (extractPlan, error) {
	var plan extractPlan
	explicit, names := map[string]bool{}, map[string]bool{} // names maps case-folded names to directory status.
	spelling := map[string]string{}
	var declared uint64
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return plan, err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		key := strings.ToLower(name)
		dir := entry.Directory
		if !safeArchiveName(name) || explicit[key] {
			return plan, fmt.Errorf("unsafe or duplicate archive entry %q", entry.Name)
		}
		if original, exists := spelling[key]; exists && original != name {
			return plan, fmt.Errorf("archive case collision at %q", name)
		}
		if entry.Encrypted || entry.Special {
			return plan, fmt.Errorf("encrypted or special archive entry %q is unsupported", entry.Name)
		}
		if dir && (entry.Size != 0 || !strings.HasSuffix(entry.Name, "/")) {
			return plan, fmt.Errorf("invalid archive directory %q", entry.Name)
		}
		if priorDir, exists := names[key]; exists && (!priorDir || !dir) {
			return plan, fmt.Errorf("archive file/directory collision at %q", name)
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
					return plan, fmt.Errorf("archive file/directory or case collision at %q", parent)
				}
			} else {
				names[parentKey], spelling[parentKey] = true, parent
				plan.dirs = append(plan.dirs, parent)
			}
		}
		if len(names) > MaxArchiveEntries {
			return plan, fmt.Errorf("archive exceeds %d output paths", MaxArchiveEntries)
		}
		if !dir {
			if entry.Size > uint64(maxBytes)-declared {
				return plan, fmt.Errorf("expanded archive exceeds %d bytes", maxBytes)
			}
			declared += entry.Size
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

// ExtractArchive validates and authorizes every generated path before creating the
// destination. The output is confined to a new directory handle, not path joins
// opened through the process working directory. No archive permissions survive.
func (s Service) ExtractArchive(ctx context.Context, in ArchiveExtractRequest, authorize WriteAuthorizer) (result ArchiveExtractResult, err error) {
	in, err = s.ValidateArchiveExtract(in)
	if err != nil {
		return result, err
	}
	if authorize == nil {
		return result, fmt.Errorf("archive output authorization unavailable")
	}
	if _, e := s.WS.Lstat(in.OutDir); e == nil {
		return result, fmt.Errorf("extraction destination already exists")
	} else if !errors.Is(e, os.ErrNotExist) {
		return result, e
	}
	var entries []archiveEntry
	if in.Format == "" || in.Format == "zip" {
		f, z, e := s.openZIP(ctx, in.Path)
		if e != nil {
			return result, e
		}
		defer f.Close()
		entries = zipEntries(z)
	} else {
		entries, _, err = s.readStreamArchive(ctx, in.Path, in.Format, in.MaxBytes)
		if err != nil {
			return result, err
		}
	}
	plan, err := planArchive(ctx, entries, in.MaxBytes)
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
			return result, fmt.Errorf("expanded archive exceeds %d bytes", in.MaxBytes)
		}
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	complete = true
	return ArchiveExtractResult{Path: in.Path, Directory: in.OutDir, Files: len(plan.files), Bytes: expanded}, nil
}
