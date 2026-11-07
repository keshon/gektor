package fileops

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"
)

const MaxZIPEntries = 10000
const MaxZIPMetadataBytes = 2 << 20

type ZIPRequest struct {
	Path   string `json:"path"`
	Offset int    `json:"offset,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type ZIPEntry struct {
	Name           string `json:"name"`
	Size           uint64 `json:"size"`
	CompressedSize uint64 `json:"compressed_size"`
	Directory      bool   `json:"directory"`
	Symlink        bool   `json:"symlink"`
	SafePath       bool   `json:"safe_path"`
}

type ZIPResult struct {
	Path    string     `json:"path"`
	Total   int        `json:"total"`
	Offset  int        `json:"offset"`
	Entries []ZIPEntry `json:"entries"`
	More    bool       `json:"more"`
}

func (s Service) ValidateZIP(in ZIPRequest) (ZIPRequest, error) {
	var err error
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

// ListZIP reads the central directory only. It never opens or decompresses an
// entry, and a safe_path flag is not a promise that an entry is safe to extract.
func (s Service) ListZIP(ctx context.Context, in ZIPRequest) (ZIPResult, error) {
	result := ZIPResult{Entries: []ZIPEntry{}}
	in, err := s.ValidateZIP(in)
	if err != nil {
		return result, err
	}
	if err := ctx.Err(); err != nil {
		return result, err
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
		result.Entries = append(result.Entries, ZIPEntry{Name: entry.Name, Size: entry.UncompressedSize64, CompressedSize: entry.CompressedSize64,
			Directory: entry.FileInfo().IsDir(), Symlink: entry.Mode()&fs.ModeSymlink != 0,
			SafePath: safeZIPName(name)})
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
	reader := &contextReaderAt{ctx: ctx, r: f, remaining: MaxZIPMetadataBytes}
	z, err := zip.NewReader(reader, info.Size())
	if err != nil && !errors.Is(err, zip.ErrInsecurePath) {
		return fail(fmt.Errorf("read ZIP: %w", err))
	}
	if len(z.File) > MaxZIPEntries {
		return fail(fmt.Errorf("ZIP exceeds %d entries", MaxZIPEntries))
	}
	// File.Open retains this ReaderAt. Payload and local-header reads need a
	// separate budget from central-directory discovery, shared across entries.
	reader.remaining = MaxFileBytes + MaxZIPMetadataBytes
	return f, z, nil
}

// Use portable names so a ZIP created on one OS cannot gain drive, stream or
// device semantics when extracted on another. Extraction never creates links.
func safeZIPName(name string) bool {
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
