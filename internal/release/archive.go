package release

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func payloadMode(name string) fs.FileMode {
	if name == "gektor" || name == "gektor.exe" {
		return 0755
	}
	return 0644
}

// writeArchive reads the staged payload, then verifies every archived byte
// against that payload before publishing the archive's final filename.
func writeArchive(name, directory string, files []string, timestamp time.Time) error {
	files = append([]string(nil), files...)
	sort.Strings(files)
	temporary, err := os.CreateTemp(filepath.Dir(name), ".archive-")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	zipFormat := strings.HasSuffix(name, ".zip")
	err = archivePayload(temporary, zipFormat, directory, files, timestamp)
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	if err := verifyArchive(temporary.Name(), zipFormat, directory, files); err != nil {
		return err
	}
	return os.Rename(temporary.Name(), name)
}

func archivePayload(output io.Writer, zipFormat bool, directory string, files []string, timestamp time.Time) error {
	var add func(string) (io.Writer, error)
	var finish func() error
	if zipFormat {
		bundle := zip.NewWriter(output)
		add = func(name string) (io.Writer, error) {
			header := &zip.FileHeader{Name: name, Method: zip.Deflate}
			header.SetMode(payloadMode(name))
			header.SetModTime(timestamp)
			return bundle.CreateHeader(header)
		}
		finish = bundle.Close
	} else {
		compressed := gzip.NewWriter(output)
		bundle := tar.NewWriter(compressed)
		add = func(name string) (io.Writer, error) {
			info, err := os.Stat(filepath.Join(directory, filepath.FromSlash(name)))
			if err != nil {
				return nil, err
			}
			err = bundle.WriteHeader(&tar.Header{Name: name, Mode: int64(payloadMode(name)), Size: info.Size(), ModTime: timestamp, Format: tar.FormatUSTAR})
			return bundle, err
		}
		finish = func() error {
			err := bundle.Close()
			compressedErr := compressed.Close()
			if err != nil {
				return err
			}
			return compressedErr
		}
	}
	for _, name := range files {
		writer, err := add(name)
		if err != nil {
			_ = finish()
			return err
		}
		input, err := os.Open(filepath.Join(directory, filepath.FromSlash(name)))
		if err != nil {
			_ = finish()
			return err
		}
		_, err = io.Copy(writer, input)
		closeErr := input.Close()
		if err != nil || closeErr != nil {
			_ = finish()
			if err != nil {
				return err
			}
			return closeErr
		}
	}
	return finish()
}

func verifyArchive(name string, zipFormat bool, directory string, files []string) error {
	expected := make(map[string]bool, len(files))
	for _, name := range files {
		expected[name] = true
	}
	check := func(name string, mode fs.FileMode, reader io.Reader) error {
		if !expected[name] {
			return fmt.Errorf("unexpected or duplicate archive member %s", name)
		}
		if !mode.IsRegular() || mode.Perm() != payloadMode(name) {
			return fmt.Errorf("incorrect archive mode for %s: %s", name, mode)
		}
		want, err := fileDigest(filepath.Join(directory, filepath.FromSlash(name)))
		if err != nil {
			return err
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, reader); err != nil {
			return err
		}
		if want != [sha256.Size]byte(hash.Sum(nil)) {
			return fmt.Errorf("archive content differs from staged file %s", name)
		}
		delete(expected, name)
		return nil
	}
	if zipFormat {
		bundle, err := zip.OpenReader(name)
		if err != nil {
			return err
		}
		defer bundle.Close()
		for _, member := range bundle.File {
			reader, err := member.Open()
			if err != nil {
				return err
			}
			err = check(member.Name, member.Mode(), reader)
			closeErr := reader.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
	} else {
		file, err := os.Open(name)
		if err != nil {
			return err
		}
		defer file.Close()
		compressed, err := gzip.NewReader(file)
		if err != nil {
			return err
		}
		defer compressed.Close()
		bundle := tar.NewReader(compressed)
		for {
			member, err := bundle.Next()
			if err == io.EOF {
				// Consume the gzip footer too, so truncated or corrupt streams fail.
				if _, err := io.Copy(io.Discard, compressed); err != nil {
					return err
				}
				break
			}
			if err != nil {
				return err
			}
			if err := check(member.Name, member.FileInfo().Mode(), bundle); err != nil {
				return err
			}
		}
	}
	if len(expected) != 0 {
		return fmt.Errorf("archive is missing %d staged files", len(expected))
	}
	return nil
}
