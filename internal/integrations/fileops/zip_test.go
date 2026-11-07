package fileops

import (
	"archive/zip"
	"bytes"
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeZIP(t *testing.T, s Service, names ...string) {
	t.Helper()
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	for _, name := range names {
		header := &zip.FileHeader{Name: name, Method: zip.Store}
		if name == "link" {
			header.SetMode(fs.ModeSymlink | 0700)
		}
		f, err := w.CreateHeader(header)
		if err != nil {
			t.Fatal(err)
		}
		if name[len(name)-1] != '/' {
			if _, err := f.Write([]byte("data")); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.WS.WriteFile("input.zip", data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestZIPListingBoundsArchiveAndMetadataWork(t *testing.T) {
	s := testService(t)
	names := make([]string, MaxArchiveEntries+1)
	for i := range names {
		names[i] = "entry"
	}
	writeZIP(t, s, names...)
	if _, err := s.ListArchive(t.Context(), ArchiveListRequest{Path: "input.zip"}); err == nil || !strings.Contains(err.Error(), "entries") {
		t.Fatal("entry limit not enforced", err)
	}
	names = make([]string, 1100)
	for i := range names {
		names[i] = strings.Repeat("a", 2100)
	}
	writeZIP(t, s, names...)
	if _, err := s.ListArchive(t.Context(), ArchiveListRequest{Path: "input.zip"}); err == nil || !strings.Contains(err.Error(), "budget") {
		t.Fatal("metadata budget not enforced", err)
	}
	f, err := s.WS.CreateExclusive("large.zip", 0600)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(MaxFileBytes + 1); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ListArchive(t.Context(), ArchiveListRequest{Path: "large.zip"}); err == nil || !strings.Contains(err.Error(), "regular file") {
		t.Fatal("file size limit not enforced", err)
	}
}

func TestZIPListingPagesAndFlagsUntrustedNamesWithoutExtraction(t *testing.T) {
	s := testService(t)
	writeZIP(t, s, "good.txt", "../escape", "/absolute", "C:/drive", "back\\slash", "link", "folder/")
	for _, offset := range []int{0, 3, 6, 100} {
		result, err := s.ListArchive(t.Context(), ArchiveListRequest{Path: "input.zip", Offset: offset, Limit: 3})
		if err != nil {
			t.Fatal(err)
		}
		if result.Total != 7 || len(result.Entries) != min(3, max(7-offset, 0)) || result.More != (offset < 6) {
			t.Fatal("incorrect page", result)
		}
		for _, entry := range result.Entries {
			if entry.SafePath != (entry.Name == "good.txt" || entry.Name == "link" || entry.Name == "folder/") || entry.Symlink != (entry.Name == "link") {
				t.Fatal("incorrect path/link flag", entry)
			}
		}
	}
	files, err := os.ReadDir(s.WS.Root())
	if err != nil || len(files) != 1 || files[0].Name() != "input.zip" {
		t.Fatal("listing extracted or wrote files", files, err)
	}
}

func TestZIPListingRejectsInvalidInputAndCancellation(t *testing.T) {
	s := testService(t)
	if err := s.WS.WriteFile("broken.zip", []byte("not a ZIP"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, req := range []ArchiveListRequest{{Path: "broken.zip"}, {Path: "../outside"}, {Path: "broken.zip", Offset: -1}, {Path: "broken.zip", Limit: 101}} {
		if _, err := s.ListArchive(t.Context(), req); err == nil {
			t.Fatal("accepted", req)
		}
	}
	writeZIP(t, s, "one")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.ListArchive(ctx, ArchiveListRequest{Path: "input.zip"}); err == nil {
		t.Fatal("ignored cancellation")
	}
	outside := filepath.Join(t.TempDir(), "outside.zip")
	data, _ := s.WS.ReadFile("input.zip")
	if err := os.WriteFile(outside, data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(s.WS.Root(), "link.zip")); err != nil {
		t.Skip(err)
	}
	if _, err := s.ListArchive(t.Context(), ArchiveListRequest{Path: "link.zip"}); err == nil {
		t.Fatal("read outside workspace")
	}
}
