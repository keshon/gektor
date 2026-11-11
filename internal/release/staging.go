package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func stage(ctx context.Context, src source, target string, files []string, log io.Writer) (string, error) {
	out := filepath.Join(src.root, "dist")
	temporary, err := os.MkdirTemp(out, ".stage-")
	if err != nil {
		return "", err
	}
	// Unpublished staging files are disposable. After rename this old path is absent.
	defer os.RemoveAll(temporary)

	err = copyPayload(ctx, src, temporary, target, files)
	if err != nil {
		return "", err
	}

	osName, _, _ := strings.Cut(target, "-")
	executable := binaryName(target)
	output := filepath.Join(temporary, executable)
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", output, "./cmd/gektor")
	cmd.Dir = src.root
	environment := os.Environ()
	cmd.Env = append(environment, "GOOS="+osName, "GOARCH=amd64", "CGO_ENABLED=0")
	cmd.Stdout = log
	cmd.Stderr = log
	fmt.Fprintln(log, "Building", target)
	err = cmd.Run()
	if err != nil {
		return "", err
	}
	err = ctx.Err()
	if err != nil {
		return "", err
	}

	if !src.dirty {
		after, sourceErr := readSource(ctx, src.root, true)
		if sourceErr != nil {
			return "", sourceErr
		}
		if after.revision != src.revision {
			return "", fmt.Errorf("checkout revision changed while building")
		}
	}

	destination := filepath.Join(out, target)
	err = os.RemoveAll(destination)
	if err != nil {
		return "", err
	}
	err = os.Rename(temporary, destination)
	if err != nil {
		return "", err
	}

	return destination, nil
}

func copyPayload(ctx context.Context, src source, directory, target string, files []string) error {
	executable := binaryName(target)
	included := map[string]bool{executable: true}
	for _, name := range files {
		included[name] = true
	}

	for _, name := range files {
		err := ctx.Err()
		if err != nil {
			return err
		}

		relative := filepath.FromSlash(name)
		input := filepath.Join(src.root, relative)
		info, err := os.Lstat(input)
		if err != nil {
			return fmt.Errorf("missing or non-regular release file %s", name)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("missing or non-regular release file %s", name)
		}

		var data []byte
		if src.dirty {
			data, err = os.ReadFile(input)
		} else {
			// Git blobs avoid checkout-specific CRLF conversion in release payloads.
			data, err = git(ctx, src.root, "show", src.revision+":"+name)
		}
		if err != nil {
			return err
		}

		output := filepath.Join(directory, relative)
		parent := filepath.Dir(output)
		err = os.MkdirAll(parent, 0755)
		if err != nil {
			return err
		}
		payload := releaseBytes(name, data, included, src.revision)
		err = os.WriteFile(output, payload, 0644)
		if err != nil {
			return err
		}
	}

	return nil
}
