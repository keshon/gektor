package fileops

import (
	"archive/tar"
	"archive/zip"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
)

func validateArchiveFormat(format string) error {
	switch format {
	case "", "zip", "tar", "tar.gz", "gzip":
		return nil
	}
	return fmt.Errorf("format must be zip, tar, tar.gz or gzip")
}

// Stream formats must be scanned to list entries. Bound the entire decoded
// stream, including TAR headers and padding, as well as the retained payload.
func (s Service) readStreamArchive(ctx context.Context, name, format string, maxBytes int64) ([]archiveEntry, int64, error) {
	f, err := s.WS.Open(name)
	if err != nil {
		return nil, 0, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > MaxFileBytes {
		return nil, 0, fmt.Errorf("archive requires a regular file of at most %d bytes", MaxFileBytes)
	}
	input := bufio.NewReader(io.LimitReader(contextReader{ctx, f}, MaxFileBytes+1))
	var stream io.Reader = input
	var gz *gzip.Reader
	if format == "tar.gz" || format == "gzip" {
		gz, err = gzip.NewReader(input)
		if err != nil {
			return nil, 0, err
		}
		defer gz.Close()
		gz.Multistream(false)
		stream = gz
	}
	bounded := &io.LimitedReader{R: contextReader{ctx, stream}, N: maxBytes + MaxArchiveMetadataBytes + 1}
	var entries []archiveEntry
	var retained int64
	if format == "gzip" {
		data, e := io.ReadAll(io.LimitReader(bounded, maxBytes+1))
		if e != nil {
			return nil, 0, e
		}
		if int64(len(data)) > maxBytes {
			return nil, 0, fmt.Errorf("expanded archive exceeds %d bytes", maxBytes)
		}
		entryName := gz.Name
		if entryName == "" {
			entryName = strings.TrimSuffix(path.Base(name), ".gz")
		}
		entries = append(entries, memoryEntry(entryName, data, false, false))
	} else {
		tr := tar.NewReader(bounded)
		for {
			h, e := tr.Next()
			if e == io.EOF {
				break
			}
			if e != nil {
				return nil, 0, e
			}
			if len(entries) >= MaxArchiveEntries || len(h.Name) > 4096 {
				return nil, 0, fmt.Errorf("archive entries or names exceed limit")
			}
			directory := h.Typeflag == tar.TypeDir
			special := h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA && !directory
			// Sparse TAR can expand without consuming the input budget. Do not accept it.
			for key := range h.PAXRecords {
				if strings.Contains(strings.ToLower(key), "sparse") {
					return nil, 0, fmt.Errorf("sparse TAR is unsupported")
				}
			}
			if h.Size < 0 || h.Size > maxBytes-retained {
				return nil, 0, fmt.Errorf("expanded archive exceeds %d bytes", maxBytes)
			}
			data, e := io.ReadAll(io.LimitReader(tr, h.Size+1))
			if e != nil {
				return nil, 0, e
			}
			if int64(len(data)) != h.Size {
				return nil, 0, fmt.Errorf("truncated TAR entry")
			}
			retained += h.Size
			name := h.Name
			if directory && !strings.HasSuffix(name, "/") {
				name += "/"
			}
			entry := memoryEntry(name, data, directory, special)
			entry.Symlink = h.Typeflag == tar.TypeSymlink
			entries = append(entries, entry)
		}
	}
	// Read through the compression trailer to validate CRC and reject concatenated
	// gzip members. No ignored tail can conceal another archive from inspection.
	if _, err := io.Copy(io.Discard, bounded); err != nil {
		return nil, 0, err
	}
	if bounded.N == 0 {
		return nil, 0, fmt.Errorf("decoded archive exceeds stream budget")
	}
	if gz != nil {
		if _, err := input.Peek(1); err != io.EOF {
			return nil, 0, fmt.Errorf("trailing data or multiple gzip members are unsupported")
		}
	}
	return entries, info.Size(), nil
}

func memoryEntry(name string, data []byte, directory, special bool) archiveEntry {
	return archiveEntry{Name: name, Size: uint64(len(data)), Directory: directory, Special: special, Open: func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(data)), nil }}
}

func writeArchive(ctx context.Context, out io.Writer, in ArchiveCreateRequest, sources []*os.File) error {
	var copied int64
	copyFile := func(w io.Writer, f *os.File) error {
		n, err := io.Copy(w, io.LimitReader(contextReader{ctx, f}, MaxFileBytes-copied+1))
		copied += n
		if err != nil {
			return err
		}
		if copied > MaxFileBytes {
			return fmt.Errorf("archive sources exceed %d bytes", MaxFileBytes)
		}
		return nil
	}
	if in.Format == "" || in.Format == "zip" {
		w := zip.NewWriter(out)
		for i, f := range sources {
			entry, err := w.CreateHeader(&zip.FileHeader{Name: in.Paths[i], Method: zip.Deflate})
			if err != nil {
				return err
			}
			if err := copyFile(entry, f); err != nil {
				return err
			}
		}
		return w.Close()
	}
	var gz *gzip.Writer
	if in.Format == "gzip" || in.Format == "tar.gz" {
		gz = gzip.NewWriter(out)
		out = gz
	}
	if in.Format == "gzip" {
		gz.Name = path.Base(in.Paths[0])
		if err := copyFile(gz, sources[0]); err != nil {
			return err
		}
	} else {
		w := tar.NewWriter(out)
		for i, f := range sources {
			info, err := f.Stat()
			if err != nil {
				return err
			}
			if err := w.WriteHeader(&tar.Header{Name: in.Paths[i], Mode: 0600, Size: info.Size(), Typeflag: tar.TypeReg}); err != nil {
				return err
			}
			if err := copyFile(w, f); err != nil {
				return err
			}
		}
		if err := w.Close(); err != nil {
			return err
		}
	}
	if gz != nil {
		return gz.Close()
	}
	return nil
}
