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
			for name, data := range map[string][]byte{"gektor": {0, 1, 255}, "README.md": []byte("offline docs\r\n"), ".env": []byte("must not be archived")} {
				if err := os.WriteFile(filepath.Join(directory, name), data, 0644); err != nil {
					t.Fatal(err)
				}
			}
			files := []string{"gektor", "README.md"}
			archive := filepath.Join(t.TempDir(), "kit"+suffix)
			stamp := time.Unix(1760000000, 0).UTC()
			if err := writeArchive(archive, directory, files, stamp); err != nil {
				t.Fatal(err)
			}
			first, err := fileDigest(archive)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.Chtimes(filepath.Join(directory, "README.md"), time.Now(), time.Now()); err != nil {
				t.Fatal(err)
			}
			if err := writeArchive(archive, directory, files, stamp); err != nil {
				t.Fatal(err)
			}
			second, err := fileDigest(archive)
			if err != nil || first != second {
				t.Fatal("archive depends on source file timestamps", err)
			}
			if err := os.WriteFile(filepath.Join(directory, "README.md"), []byte("changed"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := verifyArchive(archive, suffix == ".zip", directory, files); err == nil || !strings.Contains(err.Error(), "differs") {
				t.Fatal("changed staged content went undetected", err)
			}
			if err := verifyArchive(archive, suffix == ".zip", directory, []string{"gektor"}); err == nil {
				t.Fatal("unexpected member went undetected")
			}
		})
	}
}
