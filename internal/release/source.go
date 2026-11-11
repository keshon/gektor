package release

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type source struct {
	root      string
	revision  string
	timestamp time.Time
	tracked   []string
	dirty     bool
}

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	commandArgs := append([]string{"-C", root}, args...)
	cmd := exec.CommandContext(ctx, "git", commandArgs...)
	data, err := cmd.Output()
	if err != nil {
		operation := strings.Join(args, " ")
		return nil, fmt.Errorf("git %s: %w", operation, err)
	}

	return data, nil
}

func readSource(ctx context.Context, root string, clean bool) (source, error) {
	if root == "" {
		root = "."
	}

	directory, err := git(ctx, root, "rev-parse", "--show-toplevel")
	if err != nil {
		return source{}, err
	}
	src := source{root: strings.TrimSpace(string(directory))}
	modulePath := filepath.Join(src.root, "go.mod")
	module, err := os.ReadFile(modulePath)
	if err != nil {
		return source{}, fmt.Errorf("packaging requires the GEKTOR repository")
	}
	moduleText := string(module)
	isGektor := strings.HasPrefix(moduleText, "module github.com/keshon/gektor\n") ||
		strings.HasPrefix(moduleText, "module github.com/keshon/gektor\r\n")
	if !isGektor {
		return source{}, fmt.Errorf("packaging requires the GEKTOR repository")
	}

	status, err := git(ctx, src.root, "status", "--porcelain", "--untracked-files=normal")
	if err != nil {
		return source{}, err
	}
	src.dirty = len(status) != 0
	if clean && src.dirty {
		return source{}, fmt.Errorf("versioned release requires a clean checkout; commit tracked changes and remove untracked files, or use dist")
	}

	revision, err := git(ctx, src.root, "rev-parse", "HEAD")
	if err != nil {
		return source{}, err
	}
	src.revision = strings.TrimSpace(string(revision))
	stamp, err := git(ctx, src.root, "show", "-s", "--format=%ct", "HEAD")
	if err != nil {
		return source{}, err
	}
	stampText := strings.TrimSpace(string(stamp))
	seconds, err := strconv.ParseInt(stampText, 10, 64)
	if err != nil {
		return source{}, err
	}
	committedAt := time.Unix(seconds, 0)
	src.timestamp = committedAt.UTC()

	names, err := git(ctx, src.root, "ls-files", "--cached", "-z")
	if err != nil {
		return source{}, err
	}
	tracked := strings.TrimSuffix(string(names), "\x00")
	src.tracked = strings.Split(tracked, "\x00")

	return src, nil
}
