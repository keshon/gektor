// Package release stages development distributions and verified versioned archives.
// Build owns temporary files and subprocesses; callers own its progress writer.
package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// Options selects a repository and distribution targets. Build borrows Log;
// the caller owns its lifetime. A nil Log discards build progress.
type Options struct {
	Root string
	// Version is empty for a development distribution without archives.
	Version string
	// Target selects one development target; versioned releases build both.
	Target string
	Log    io.Writer
}

// Build stages distributions and returns their output paths. It replaces target
// directories under dist only after staging succeeds. Versioned builds require
// a clean checkout and use committed payload bytes. Build waits for its Go/Git
// subprocesses; cancellation leaves unpublished staging files to local cleanup.
func Build(ctx context.Context, options Options) ([]string, error) {
	targets, err := distributionTargets(options)
	if err != nil {
		return nil, err
	}

	log := options.Log
	if log == nil {
		log = io.Discard
	}

	versioned := options.Version != ""
	src, err := readSource(ctx, options.Root, versioned)
	if err != nil {
		return nil, err
	}

	if src.dirty {
		fmt.Fprintln(log, "Development distribution includes tracked working-tree changes; versioned releases require a clean checkout.")
	}

	out := filepath.Join(src.root, "dist")
	err = os.MkdirAll(out, 0755)
	if err != nil {
		return nil, err
	}

	info, err := os.Lstat(out)
	if err != nil {
		return nil, fmt.Errorf("dist must be a real directory")
	}

	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("dist must be a real directory")
	}

	var outputs, archives []string

	for _, target := range targets {
		files := runtimeFiles(src.tracked, target)
		directory, stageErr := stage(ctx, src, target, files, log)
		if stageErr != nil {
			return nil, fmt.Errorf("stage %s: %w", target, stageErr)
		}

		outputs = append(outputs, directory)
		if !versioned {
			continue
		}

		executable := binaryName(target)
		files = append(files, executable)
		suffix := ".tar.gz"
		if strings.HasPrefix(target, "windows-") {
			suffix = ".zip"
		}

		archive := filepath.Join(out, "gektor-"+options.Version+"-"+target+suffix)
		err = writeArchive(archive, directory, files, src.timestamp)
		if err != nil {
			return nil, err
		}

		archives = append(archives, archive)
		outputs = append(outputs, archive)
	}

	if len(archives) == 0 {
		return outputs, nil
	}

	checksums, err := writeChecksums(out, archives)
	if err != nil {
		return nil, err
	}

	outputs = append(outputs, checksums)

	return outputs, nil
}
