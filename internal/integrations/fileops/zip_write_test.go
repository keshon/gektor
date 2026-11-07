package fileops

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func allowWrite(context.Context, string) error { return nil }

func TestZIPExtractionSeparatesPayloadAndMetadataBudgets(t *testing.T) {
	s := testService(t)
	payload := bytes.Repeat([]byte("x"), 3<<20)
	var data bytes.Buffer
	w := zip.NewWriter(&data)
	f, err := w.CreateHeader(&zip.FileHeader{Name: "large.bin", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.WS.WriteFile("input.zip", data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	result, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out", MaxBytes: 4 << 20}, allowWrite)
	if err != nil || result.Bytes != int64(len(payload)) {
		t.Fatal("payload was limited to metadata budget", result, err)
	}
	actual, err := s.WS.ReadFile("out/large.bin")
	if err != nil || !bytes.Equal(actual, payload) {
		t.Fatal("payload changed", err)
	}
}

type cancelOnCheck struct {
	context.Context
	check func()
}

func (c cancelOnCheck) Err() error { c.check(); return c.Context.Err() }

func TestZIPWritesRemoveOutputWhenCancelledAfterCreation(t *testing.T) {
	for _, extract := range []bool{false, true} {
		t.Run(map[bool]string{false: "create", true: "extract"}[extract], func(t *testing.T) {
			s := testService(t)
			writeZIP(t, s, "one")
			if err := s.WS.WriteFile("one", []byte("source"), 0600); err != nil {
				t.Fatal(err)
			}
			base, cancel := context.WithCancel(t.Context())
			defer cancel()
			watch, output := "bundle.zip", "bundle.zip"
			if extract {
				watch, output = "out/one", "out"
			}
			ctx := cancelOnCheck{Context: base, check: func() {
				if _, err := s.WS.Stat(watch); err == nil {
					cancel()
				}
			}}
			var err error
			if extract {
				_, err = s.ExtractZIP(ctx, ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, allowWrite)
			} else {
				_, err = s.CreateZIP(ctx, ZIPCreateRequest{Paths: []string{"one"}, Path: "bundle.zip"})
			}
			if !errors.Is(err, context.Canceled) {
				t.Fatal("ignored cancellation during writing", err)
			}
			if _, err := s.WS.Stat(output); !os.IsNotExist(err) {
				t.Fatal("cancelled operation left output", err)
			}
		})
	}
}

func TestZIPCreateListExtractRoundTrip(t *testing.T) {
	s := testService(t)
	if err := s.WS.Mkdir("src", 0700); err != nil {
		t.Fatal(err)
	}
	payload := []byte{0, 255, 10, 128, 1}
	if err := s.WS.WriteFile("src/data.bin", payload, 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.WS.WriteFile("notes.txt", []byte("notes"), 0600); err != nil {
		t.Fatal(err)
	}
	request := ZIPCreateRequest{Paths: []string{"src/data.bin", "notes.txt"}, Path: "bundle.zip"}
	created, err := s.CreateZIP(t.Context(), request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := s.WS.ReadFile("bundle.zip")
	if err != nil || created.Files != 2 || created.Size != int64(len(data)) || created.SHA256 != fmt.Sprintf("%x", sha256.Sum256(data)) {
		t.Fatal(created, err)
	}
	if !reflect.DeepEqual(request.Paths, []string{"src/data.bin", "notes.txt"}) {
		t.Fatal("mutated caller arguments")
	}
	listed, err := s.ListZIP(t.Context(), ZIPRequest{Path: created.Path})
	if err != nil || listed.Total != 2 || listed.Entries[0].Name != "src/data.bin" {
		t.Fatal(listed, err)
	}
	var authorized []string
	extracted, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "bundle.zip", OutDir: "out"}, func(_ context.Context, path string) error {
		if _, err := s.WS.Stat("out"); !os.IsNotExist(err) {
			t.Fatal("authorization happened after writing")
		}
		authorized = append(authorized, path)
		return nil
	})
	if err != nil || extracted.Files != 2 || extracted.Bytes != 10 || !reflect.DeepEqual(authorized, []string{"out/src", "out/src/data.bin", "out/notes.txt"}) {
		t.Fatal(extracted, authorized, err)
	}
	copy, err := s.WS.ReadFile("out/src/data.bin")
	if err != nil || !bytes.Equal(copy, payload) {
		t.Fatal(copy, err)
	}
	if info, err := s.WS.Stat("out/src/data.bin"); err != nil || info.Mode().Perm()&0111 != 0 {
		t.Fatal("executable mode restored", info, err)
	}
	if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "bundle.zip", OutDir: "out"}, allowWrite); err == nil {
		t.Fatal("overwrote existing output directory")
	}
	if _, err := s.CreateZIP(t.Context(), request); err == nil {
		t.Fatal("overwrote existing ZIP")
	}
	still, _ := s.WS.ReadFile("bundle.zip")
	if !bytes.Equal(still, data) {
		t.Fatal("changed existing archive")
	}
}

func TestZIPCreateRejectsImplicitOrUnsafeSources(t *testing.T) {
	s := testService(t)
	if err := s.WS.WriteFile("one", []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.WS.Mkdir("folder", 0700); err != nil {
		t.Fatal(err)
	}
	for _, paths := range [][]string{nil, {"folder"}, {"../outside"}, {"one", "ONE"}, {"one", "one"}, {"bundle.zip"}, {"A/one", "a/two"}} {
		if _, err := s.CreateZIP(t.Context(), ZIPCreateRequest{Paths: paths, Path: "bundle.zip"}); err == nil {
			t.Fatal("accepted", paths)
		}
		if _, err := s.WS.Stat("bundle.zip"); !os.IsNotExist(err) {
			t.Fatal("failed creation left output", err)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.CreateZIP(ctx, ZIPCreateRequest{Paths: []string{"one"}, Path: "bundle.zip"}); err == nil {
		t.Fatal("ignored cancellation")
	}
	t.Run("source symlink", func(t *testing.T) {
		if err := os.Symlink(filepath.Join(s.WS.Root(), "one"), filepath.Join(s.WS.Root(), "link")); err != nil {
			t.Skip(err)
		}
		if _, err := s.CreateZIP(t.Context(), ZIPCreateRequest{Paths: []string{"link"}, Path: "bundle.zip"}); err == nil {
			t.Fatal("accepted source symlink")
		}
	})
}

func TestZIPExtractionCountsImplicitDirectoriesAndRejectsDestinationLinks(t *testing.T) {
	s := testService(t)
	names := make([]string, 200)
	for i := range names {
		names[i] = fmt.Sprintf("root%03d/", i) + strings.Repeat("d/", 55) + "file"
	}
	writeZIP(t, s, names...)
	if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, allowWrite); err == nil || !strings.Contains(err.Error(), "output paths") {
		t.Fatal("implicit directories bypassed count limit", err)
	}
	if _, err := s.WS.Stat("out"); !os.IsNotExist(err) {
		t.Fatal("over-limit extraction created output", err)
	}
	writeZIP(t, s, "file")
	t.Run("destination link", func(t *testing.T) {
		outside := t.TempDir()
		if err := os.Symlink(outside, filepath.Join(s.WS.Root(), "link")); err != nil {
			t.Skip(err)
		}
		for _, dest := range []string{"link", "link/new"} {
			if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: dest}, allowWrite); err == nil {
				t.Fatal("followed destination link", dest)
			}
		}
		files, err := os.ReadDir(outside)
		if err != nil || len(files) != 0 {
			t.Fatal("wrote outside workspace", files, err)
		}
	})
}

func TestZIPCreateRejectsOversizeSourcesWithoutOutput(t *testing.T) {
	s := testService(t)
	f, err := s.WS.CreateExclusive("large.bin", 0600)
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
	if _, err := s.CreateZIP(t.Context(), ZIPCreateRequest{Paths: []string{"large.bin"}, Path: "bundle.zip"}); err == nil {
		t.Fatal("oversize source accepted")
	}
	if _, err := s.WS.Stat("bundle.zip"); !os.IsNotExist(err) {
		t.Fatal("oversize creation wrote output", err)
	}
}

func TestZIPExtractionRejectsUnsafeEntriesBeforeWriting(t *testing.T) {
	for _, names := range [][]string{
		{"../escape"}, {"/absolute"}, {"C:/drive"}, {"a\\b"}, {"CON.txt"}, {"file:stream"}, {"trailing."}, {"a\nline"},
		{"dup", "dup"}, {"name", "NAME"}, {"node", "node/file"}, {"node/file", "node"}, {"A/one", "a/two"}, {"link"},
	} {
		t.Run(strings.Join(names, ","), func(t *testing.T) {
			s := testService(t)
			writeZIP(t, s, names...)
			called := false
			if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, func(context.Context, string) error { called = true; return nil }); err == nil {
				t.Fatal("accepted dangerous archive", names)
			}
			if called {
				t.Fatal("authorization began before rejecting invalid paths")
			}
			if _, err := s.WS.Stat("out"); !os.IsNotExist(err) {
				t.Fatal("invalid archive created output", err)
			}
		})
	}
}

func TestZIPExtractionRejectsSpecialAndEncryptedEntries(t *testing.T) {
	for _, mode := range []fs.FileMode{fs.ModeNamedPipe, fs.ModeDevice, fs.ModeSocket} {
		s := testService(t)
		var data bytes.Buffer
		w := zip.NewWriter(&data)
		header := &zip.FileHeader{Name: "special"}
		header.SetMode(mode | 0600)
		if _, err := w.CreateHeader(header); err != nil {
			t.Fatal(err)
		}
		if err := w.Close(); err != nil {
			t.Fatal(err)
		}
		if err := s.WS.WriteFile("input.zip", data.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, allowWrite); err == nil {
			t.Fatal("accepted special entry", mode)
		}
	}
	s := testService(t)
	writeZIP(t, s, "regular")
	data, _ := s.WS.ReadFile("input.zip")
	central := bytes.Index(data, []byte{'P', 'K', 1, 2})
	data[central+8] |= 1
	if err := s.WS.WriteFile("input.zip", data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, allowWrite); err == nil {
		t.Fatal("accepted encrypted entry")
	}
}

func TestZIPExtractionBoundsAndFailureCleanup(t *testing.T) {
	s := testService(t)
	writeZIP(t, s, "folder/one", "folder/two")
	if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out", MaxBytes: 7}, allowWrite); err == nil {
		t.Fatal("expanded byte budget ignored")
	}
	if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, nil); err == nil {
		t.Fatal("missing authorization accepted")
	}
	_, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, func(_ context.Context, path string) error {
		if path == "out/folder/two" {
			return fmt.Errorf("denied")
		}
		return nil
	})
	if err == nil {
		t.Fatal("ignored denied output")
	}
	if _, err := s.WS.Stat("out"); !os.IsNotExist(err) {
		t.Fatal("preflight wrote output", err)
	}
	data, _ := s.WS.ReadFile("input.zip")
	// Corrupt the second stored payload. The first file will have been written
	// before the CRC failure, so this checks rollback of files and directories.
	first := bytes.Index(data, []byte("data"))
	second := bytes.Index(data[first+4:], []byte("data")) + first + 4
	data[second] ^= 255
	if err := s.WS.WriteFile("input.zip", data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ExtractZIP(t.Context(), ZIPExtractRequest{Path: "input.zip", OutDir: "out"}, allowWrite); err == nil {
		t.Fatal("corrupted ZIP succeeded")
	}
	if _, err := s.WS.Stat("out"); !os.IsNotExist(err) {
		t.Fatal("partial extraction survived", err)
	}
}
