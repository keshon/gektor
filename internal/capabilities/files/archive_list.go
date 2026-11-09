package files

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const MaxArchiveEntries = 10000
const MaxArchiveMetadataBytes = 2 << 20

type ArchiveListRequest struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Format string `json:"format,omitempty"`
}

type ArchiveEntry struct {
	Name           string `json:"name"`
	Size           uint64 `json:"size"`
	CompressedSize uint64 `json:"compressed_size"`
	Directory      bool   `json:"directory"`
	Symlink        bool   `json:"symlink"`
	SafePath       bool   `json:"safe_path"`
}

type ArchiveListResult struct {
	Path    string         `json:"path"`
	Total   int            `json:"total"`
	Offset  int            `json:"offset"`
	Entries []ArchiveEntry `json:"entries"`
	More    bool           `json:"more"`
}

func (s Service) ValidateArchiveList(in ArchiveListRequest) (ArchiveListRequest, error) {
	var err error
	if err = validateArchiveFormat(in.Format); err != nil {
		return in, err
	}
	in.Path, err = s.path(in.Path)
	if err != nil {
		return in, err
	}
	if in.Limit == 0 {
		in.Limit = 50
	}
	if in.Offset < 0 || in.Limit < 1 || in.Limit > 100 {
		return in, fmt.Errorf("offset must be nonnegative and limit must be 1–100")
	}
	return in, nil
}

// ListArchive reads ZIP metadata or scans a bounded TAR/GZIP stream. It writes
// no files. A safe_path flag is not a promise that an entry is safe to extract.
func (s Service) ListArchive(ctx context.Context, in ArchiveListRequest) (ArchiveListResult, error) {
	result := ArchiveListResult{Entries: []ArchiveEntry{}}
	in, err := s.ValidateArchiveList(in)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
	}
	if in.Format != "" && in.Format != "zip" {
		entries, size, err := s.readStreamArchive(ctx, in.Path, in.Format, MaxFileBytes)
		if err != nil {
			return result, err
		}
		result.Path, result.Total, result.Offset = in.Path, len(entries), min(in.Offset, len(entries))
		end := result.Offset + min(in.Limit, len(entries)-result.Offset)
		for _, e := range entries[result.Offset:end] {
			compressed := uint64(0)
			if in.Format == "gzip" {
				compressed = uint64(size)
			}
			result.Entries = append(result.Entries, ArchiveEntry{Name: e.Name, Size: e.Size, CompressedSize: compressed, Directory: e.Directory, Symlink: e.Symlink, SafePath: safeArchiveName(strings.TrimSuffix(e.Name, "/"))})
		}
		result.More = end < len(entries)
		return result, nil
	}
	f, z, err := s.openZIP(ctx, in.Path)
	if err != nil {
		return result, err
	}
	defer f.Close()
	result.Path, result.Total, result.Offset = in.Path, len(z.File), min(in.Offset, len(z.File))
	end := result.Offset + min(in.Limit, len(z.File)-result.Offset)
	for _, entry := range z.File[result.Offset:end] {
		if err := ctx.Err(); err != nil {
			return result, err
		}
		name := strings.TrimSuffix(entry.Name, "/")
		if len(entry.Name) > 4096 {
			return result, fmt.Errorf("ZIP entry name exceeds 4096 bytes")
		}
		result.Entries = append(result.Entries, ArchiveEntry{Name: entry.Name, Size: entry.UncompressedSize64, CompressedSize: entry.CompressedSize64,
			Directory: entry.FileInfo().IsDir(), Symlink: entry.Mode()&fs.ModeSymlink != 0,
			SafePath: safeArchiveName(name)})
	}
	result.More = end < len(z.File)
	return result, nil
}

func (s Service) openZIP(ctx context.Context, path string) (*os.File, *zip.Reader, error) {
	f, err := s.WS.Open(path)
	if err != nil {
		return nil, nil, err
	}
	fail := func(err error) (*os.File, *zip.Reader, error) { _ = f.Close(); return nil, nil, err }
	info, err := f.Stat()
	if err != nil {
		return fail(err)
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return fail(fmt.Errorf("ZIP requires a regular file of at most %d bytes", MaxFileBytes))
	}
	reader := &contextReaderAt{ctx: ctx, r: f, remaining: MaxArchiveMetadataBytes}
	z, err := zip.NewReader(reader, info.Size())
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return fail(fmt.Errorf("read ZIP: %w", err))
	}
	if len(z.File) > MaxArchiveEntries {
		return fail(fmt.Errorf("ZIP exceeds %d entries", MaxArchiveEntries))
	}
	// File.Open retains this ReaderAt. Payload and local-header reads need a
	// separate budget from central-directory discovery, shared across entries.
	reader.remaining = MaxFileBytes + MaxArchiveMetadataBytes
	return f, z, nil
}

// Use portable names so a ZIP created on one OS cannot gain drive, stream or
// device semantics when extracted on another. Extraction never creates links.
func safeArchiveName(name string) bool {
	if len(name) > 4096 || !fs.ValidPath(name) || name == "." || strings.ContainsAny(name, "\\:*?\"<>|") {
		return false
	}
	for _, r := range name {
		if r < 32 || r == 127 {
			return false
		}
	}
	parts := strings.Split(name, "/")
	if len(parts) > 64 {
		return false
	}
	for _, part := range parts {
		if strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		for _, prefix := range []string{"COM", "LPT"} {
			if tail, ok := strings.CutPrefix(base, prefix); ok && len([]rune(tail)) == 1 && strings.ContainsAny(tail, "123456789¹²³") {
				return false
			}
		}
	}
	return true
}
