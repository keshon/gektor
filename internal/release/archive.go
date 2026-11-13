package release

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func payloadMode(name string) fs.FileMode {
	if name == "gektor" || name == "gektor.exe" || strings.HasSuffix(name, ".sh") {
		return 0755
	}

	return 0644
}

// writeArchive verifies every archived byte before publishing the final name.
// It owns the temporary file and leaves the caller's file list unchanged.
func writeArchive(name, directory string, files []string, timestamp time.Time) error {
	members := slices.Clone(files)
	slices.Sort(members)
	parent := filepath.Dir(name)
	temporary, err := os.CreateTemp(parent, ".archive-")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	// Failed archives are disposable; rename removes this temporary path on success.
	defer os.Remove(temporaryName)

	zipFormat := strings.HasSuffix(name, ".zip")
	if zipFormat {
		err = writeZip(temporary, directory, members, timestamp)
	} else {
		err = writeTarGzip(temporary, directory, members, timestamp)
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}

	err = verifyArchive(temporaryName, zipFormat, directory, members)
	if err != nil {
		return err
	}

	return os.Rename(temporaryName, name)
}

func writeZip(output io.Writer, directory string, files []string, timestamp time.Time) error {
	bundle := zip.NewWriter(output)
	err := writeZipMembers(bundle, directory, files, timestamp)
	closeErr := bundle.Close()
	if err != nil {
		return err
	}

	return closeErr
}

func writeZipMembers(bundle *zip.Writer, directory string, files []string, timestamp time.Time) error {
	for _, name := range files {
		header := &zip.FileHeader{Name: name, Method: zip.Deflate}
		mode := payloadMode(name)
		header.SetMode(mode)
		header.SetModTime(timestamp)
		writer, err := bundle.CreateHeader(header)
		if err != nil {
			return err
		}

		err = copyMember(writer, directory, name)
		if err != nil {
			return err
		}
	}

	return nil
}

func writeTarGzip(output io.Writer, directory string, files []string, timestamp time.Time) error {
	compressed := gzip.NewWriter(output)
	bundle := tar.NewWriter(compressed)
	err := writeTarMembers(bundle, directory, files, timestamp)
	tarErr := bundle.Close()
	gzipErr := compressed.Close()
	if err != nil {
		return err
	}
	if tarErr != nil {
		return tarErr
	}

	return gzipErr
}

func writeTarMembers(bundle *tar.Writer, directory string, files []string, timestamp time.Time) error {
	for _, name := range files {
		relative := filepath.FromSlash(name)
		path := filepath.Join(directory, relative)
		info, err := os.Stat(path)
		if err != nil {
			return err
		}

		mode := payloadMode(name)
		header := &tar.Header{
			Name:    name,
			Mode:    int64(mode),
			Size:    info.Size(),
			ModTime: timestamp,
			Format:  tar.FormatUSTAR,
		}
		err = bundle.WriteHeader(header)
		if err != nil {
			return err
		}

		err = copyMember(bundle, directory, name)
		if err != nil {
			return err
		}
	}

	return nil
}

func copyMember(writer io.Writer, directory, name string) error {
	relative := filepath.FromSlash(name)
	path := filepath.Join(directory, relative)
	input, err := os.Open(path)
	if err != nil {
		return err
	}

	_, err = io.Copy(writer, input)
	closeErr := input.Close()
	if err != nil {
		return err
	}

	return closeErr
}
