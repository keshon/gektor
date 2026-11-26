package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func Native(ctx context.Context, program string, args []string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	if ext := strings.ToLower(filepath.Ext(cmd.Path)); runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat" || ext == ".ps1") {
		return nil, fmt.Errorf("%s is a shell script, not a native executable; use run_shell or start_background with PowerShell syntax instead", program)
	}
	return cmd, nil
}

func diagnoseStartError(cmd *exec.Cmd, startErr error) error {
	var pathErr *os.PathError
	if !errors.As(startErr, &pathErr) || pathErr.Op != "fork/exec" {
		return startErr
	}

	// Start's path includes executable extension resolution. A failed PATH
	// lookup has no fork/exec PathError; never guess from the requested name.
	path := pathErr.Path
	if !filepath.IsAbs(path) {
		// Windows drive-relative and volume-rooted paths need per-drive state.
		// Skip these ambiguous forms rather than inspect a different file.
		if filepath.VolumeName(path) != "" || (runtime.GOOS == "windows" && len(path) > 0 && os.IsPathSeparator(path[0])) {
			return startErr
		}
		path = filepath.Join(cmd.Dir, path)
	}
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return startErr
	}
	file, err := os.Open(path)
	if err != nil {
		return startErr
	}
	var header [8]byte
	_, readErr := io.ReadFull(file, header[:])
	closeErr := file.Close()
	if readErr != nil || closeErr != nil || string(header[:]) != "!<arch>\n" {
		return startErr
	}
	return fmt.Errorf("file is an ar archive, not an executable; check the build target: %w", startErr)
}
