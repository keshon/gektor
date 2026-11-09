package files

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
)

func TestArchiveFormatsRoundTripAndNoOverwrite(t *testing.T) {
	for _, format := range []string{"tar", "tar.gz", "gzip"} {
		t.Run(format, func(t *testing.T) {
			s := testService(t)
			payload := []byte{0, 255, 128, 10}
			if err := s.WS.Mkdir("folder", 0700); err != nil {
				t.Fatal(err)
			}
			if err := s.WS.WriteFile("folder/source.bin", payload, 0700); err != nil {
				t.Fatal(err)
			}
			req := ArchiveCreateRequest{Paths: []string{"folder/source.bin"}, Path: "bundle", Format: format}
			packed, err := s.CreateArchive(t.Context(), req)
			if err != nil || packed.Files != 1 || len(packed.SHA256) != 64 {
				t.Fatal(packed, err)
			}
			before := mustRead(t, s, "bundle")
			if _, err := s.CreateArchive(t.Context(), req); err == nil {
				t.Fatal("overwrote archive")
			}
			listed, err := s.ListArchive(t.Context(), ArchiveListRequest{Path: "bundle", Format: format, Limit: 1})
			if err != nil || len(listed.Entries) != 1 || listed.More {
				t.Fatal(listed, err)
			}
			name := "folder/source.bin"
			if format == "gzip" {
				name = "source.bin"
			}
			if listed.Entries[0].Name != name || listed.Entries[0].Size != uint64(len(payload)) {
				t.Fatal(listed)
			}
			var approved []string
			extracted, err := s.ExtractArchive(t.Context(), ArchiveExtractRequest{Path: "bundle", OutDir: "out", Format: format}, func(_ context.Context, p string) error {
				if _, err := s.WS.Stat("out"); !os.IsNotExist(err) {
					t.Fatal("write preceded authorization")
				}
				approved = append(approved, p)
				return nil
			})
			if err != nil || extracted.Bytes != int64(len(payload)) || len(approved) == 0 {
				t.Fatal(extracted, err)
			}
			actual, err := s.WS.ReadFile("out/" + name)
			if err != nil || !bytes.Equal(actual, payload) {
				t.Fatal(actual, err)
			}
			if _, err := s.ExtractArchive(t.Context(), ArchiveExtractRequest{Path: "bundle", OutDir: "out", Format: format}, allowWrite); err == nil {
				t.Fatal("reused output folder")
			}
			if !bytes.Equal(before, mustRead(t, s, "bundle")) {
				t.Fatal("source changed")
			}
		})
	}
}

func mustRead(t *testing.T, s Service, name string) []byte {
	t.Helper()
	b, err := s.WS.ReadFile(name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func tarBytes(t *testing.T, headers []*tar.Header) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, h := range headers {
		if err := w.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		if h.Size > 0 {
			if _, err := w.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestTARRejectsUnsafeEntriesBeforeWriting(t *testing.T) {
	tests := map[string][]*tar.Header{
		"traversal": {{Name: "../escape", Size: 1, Typeflag: tar.TypeReg}},
		"symlink":   {{Name: "link", Linkname: "../escape", Typeflag: tar.TypeSymlink}},
		"hardlink":  {{Name: "link", Linkname: "source", Typeflag: tar.TypeLink}},
		"device":    {{Name: "device", Typeflag: tar.TypeChar}},
		"case":      {{Name: "a", Size: 1, Typeflag: tar.TypeReg}, {Name: "A", Size: 1, Typeflag: tar.TypeReg}},
		"parent":    {{Name: "a", Size: 1, Typeflag: tar.TypeReg}, {Name: "a/b", Size: 1, Typeflag: tar.TypeReg}},
		"duplicate": {{Name: "a", Size: 1, Typeflag: tar.TypeReg}, {Name: "a", Size: 1, Typeflag: tar.TypeReg}},
	}
	for name, headers := range tests {
		t.Run(name, func(t *testing.T) {
			s := testService(t)
			if err := s.WS.WriteFile("input.tar", tarBytes(t, headers), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := s.ExtractArchive(t.Context(), ArchiveExtractRequest{Path: "input.tar", OutDir: "out", Format: "tar"}, allowWrite); err == nil {
				t.Fatal("accepted unsafe archive")
			}
			if _, err := s.WS.Stat("out"); !os.IsNotExist(err) {
				t.Fatal("wrote unsafe archive", err)
			}
		})
	}
}

func TestStreamArchiveLimitsCRCAndGeneratedPermissions(t *testing.T) {
	s := testService(t)
	data := tarBytes(t, []*tar.Header{{Name: "file", Size: 64, Typeflag: tar.TypeReg}})
	if err := s.WS.WriteFile("input.tar", data, 0600); err != nil {
		t.Fatal(err)
	}
	for _, req := range []ArchiveExtractRequest{{Path: "input.tar", OutDir: "small", Format: "tar", MaxBytes: 32}, {Path: "input.tar", OutDir: "denied", Format: "tar"}} {
		_, err := s.ExtractArchive(t.Context(), req, func(context.Context, string) error { return fmt.Errorf("denied") })
		if err == nil {
			t.Fatal("accepted", req)
		}
		if _, err := s.WS.Stat(req.OutDir); !os.IsNotExist(err) {
			t.Fatal("wrote before validation/permission", err)
		}
	}
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	w.Name = "file"
	_, _ = w.Write([]byte("payload"))
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	valid := append([]byte{}, compressed.Bytes()...)
	for name, data := range map[string][]byte{"crc": append([]byte{}, valid...), "members": append(append([]byte{}, valid...), valid...), "truncated": valid[:len(valid)-4]} {
		if name == "crc" {
			data[len(data)-8] ^= 0xff
		}
		if err := s.WS.WriteFile(name+".gz", data, 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ExtractArchive(t.Context(), ArchiveExtractRequest{Path: name + ".gz", OutDir: name, Format: "gzip"}, allowWrite); err == nil {
			t.Fatal("accepted", name)
		}
		if _, err := s.WS.Stat(name); !os.IsNotExist(err) {
			t.Fatal("left failed output", err)
		}
	}
	// A tiny compressed stream cannot expand outside the shared budget.
	compressed.Reset()
	w = gzip.NewWriter(&compressed)
	_, _ = w.Write([]byte(strings.Repeat("x", int(MaxFileBytes)+1)))
	_ = w.Close()
	if err := s.WS.WriteFile("bomb.gz", compressed.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListArchive(t.Context(), ArchiveListRequest{Path: "bomb.gz", Format: "gzip"}); err == nil {
		t.Fatal("unbounded gzip expansion")
	}
}
