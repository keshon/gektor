package release

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestArchiveMatchesStageAndIsReproducible(t *testing.T) {
	for _, suffix := range []string{".zip", ".tar.gz"} {
		t.Run(suffix, func(t *testing.T) {
			directory := t.TempDir()
			payload := map[string][]byte{
				"gektor":    {0, 1, 255},
				"README.md": []byte("offline docs\r\n"),
				".env":      []byte("must not be archived"),
			}
			for name, data := range payload {
				path := filepath.Join(directory, name)
				err := os.WriteFile(path, data, 0644)
				if err != nil {
					t.Fatal(err)
				}
			}

			files := []string{"gektor", "README.md"}
			destination := t.TempDir()
			archive := filepath.Join(destination, "kit"+suffix)
			committedAt := time.Unix(1760000000, 0)
			stamp := committedAt.UTC()
			err := writeArchive(archive, directory, files, stamp)
			if err != nil {
				t.Fatal(err)
			}
			first, err := fileDigest(archive)
			if err != nil {
				t.Fatal(err)
			}

			readme := filepath.Join(directory, "README.md")
			now := time.Now()
			err = os.Chtimes(readme, now, now)
			if err != nil {
				t.Fatal(err)
			}
			err = writeArchive(archive, directory, files, stamp)
			if err != nil {
				t.Fatal(err)
			}
			second, err := fileDigest(archive)
			if err != nil || first != second {
				t.Fatal("archive depends on source file timestamps", err)
			}

			err = os.WriteFile(readme, []byte("changed"), 0644)
			if err != nil {
				t.Fatal(err)
			}
			zipFormat := suffix == ".zip"
			err = verifyArchive(archive, zipFormat, directory, files)
			if err == nil || !strings.Contains(err.Error(), "differs") {
				t.Fatal("changed staged content went undetected", err)
			}

			err = verifyArchive(archive, zipFormat, directory, []string{"gektor"})
			if err == nil {
				t.Fatal("unexpected member went undetected")
			}
		})
	}
}

func TestFailedArchiveKeepsPublishedOutput(t *testing.T) {
	for _, suffix := range []string{".zip", ".tar.gz"} {
		t.Run(suffix, func(t *testing.T) {
			directory := t.TempDir()
			destination := t.TempDir()
			archive := filepath.Join(destination, "kit"+suffix)
			err := os.WriteFile(archive, []byte("published"), 0644)
			if err != nil {
				t.Fatal(err)
			}

			stamp := time.Unix(1760000000, 0)
			err = writeArchive(archive, directory, []string{"missing.txt"}, stamp)
			if err == nil {
				t.Fatal("missing payload accepted")
			}

			data, err := os.ReadFile(archive)
			if err != nil || string(data) != "published" {
				t.Fatal("failed staging replaced the published archive", string(data), err)
			}
			entries, err := os.ReadDir(destination)
			if err != nil || len(entries) != 1 {
				t.Fatal("temporary archive was retained", entries, err)
			}
		})
	}
}
