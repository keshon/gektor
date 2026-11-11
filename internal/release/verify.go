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
)

func verifyArchive(name string, zipFormat bool, directory string, files []string) error {
	expected := make(map[string]bool, len(files))
	for _, member := range files {
		expected[member] = true
	}

	var err error
	if zipFormat {
		err = verifyZip(name, directory, expected)
	} else {
		err = verifyTarGzip(name, directory, expected)
	}
	if err != nil {
		return err
	}
	if len(expected) != 0 {
		return fmt.Errorf("archive is missing %d staged files", len(expected))
	}

	return nil
}

func verifyZip(name, directory string, expected map[string]bool) error {
	bundle, err := zip.OpenReader(name)
	if err != nil {
		return err
	}
	// Member read/close errors carry the verification verdict; this reader is read-only.
	defer bundle.Close()

	for _, member := range bundle.File {
		reader, openErr := member.Open()
		if openErr != nil {
			return openErr
		}

		mode := member.Mode()
		err = verifyMember(directory, expected, member.Name, mode, reader)
		closeErr := reader.Close()
		if err != nil {
			return err
		}
		if closeErr != nil {
			return closeErr
		}
	}

	return nil
}

func verifyTarGzip(name, directory string, expected map[string]bool) error {
	file, err := os.Open(name)
	if err != nil {
		return err
	}
	// Stream reads carry errors; closing the read-only descriptors is best effort.
	defer file.Close()

	compressed, err := gzip.NewReader(file)
	if err != nil {
		return err
	}
	defer compressed.Close()
	bundle := tar.NewReader(compressed)

	for {
		member, readErr := bundle.Next()
		if readErr == io.EOF {
			// Consume the gzip footer so truncated or corrupt streams fail too.
			_, err = io.Copy(io.Discard, compressed)
			return err
		}
		if readErr != nil {
			return readErr
		}

		info := member.FileInfo()
		mode := info.Mode()
		err = verifyMember(directory, expected, member.Name, mode, bundle)
		if err != nil {
			return err
		}
	}
}

// verifyMember removes a member from the caller-owned expected set only after
// its name, permissions and bytes match the staged payload.
func verifyMember(directory string, expected map[string]bool, name string, mode fs.FileMode, reader io.Reader) error {
	if !expected[name] {
		return fmt.Errorf("unexpected or duplicate archive member %s", name)
	}
	if !mode.IsRegular() || mode.Perm() != payloadMode(name) {
		return fmt.Errorf("incorrect archive mode for %s: %s", name, mode)
	}

	relative := filepath.FromSlash(name)
	path := filepath.Join(directory, relative)
	want, err := fileDigest(path)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, err = io.Copy(hash, reader)
	if err != nil {
		return err
	}
	got := [sha256.Size]byte(hash.Sum(nil))
	if want != got {
		return fmt.Errorf("archive content differs from staged file %s", name)
	}

	delete(expected, name)
	return nil
}

func fileDigest(name string) ([sha256.Size]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	// Hashing reads report content failures; read-only close is best effort.
	defer file.Close()

	hash := sha256.New()
	_, err = io.Copy(hash, file)
	if err != nil {
		return [sha256.Size]byte{}, err
	}

	return [sha256.Size]byte(hash.Sum(nil)), nil
}
