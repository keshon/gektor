package release

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"
)

type Options struct {
	Root    string
	Version string // Empty stages a development distribution without archives.
	Target  string // Only used for development distributions.
	Log     io.Writer
}

type source struct {
	root, revision string
	timestamp      time.Time
	tracked        []string
	dirty          bool
}

var versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)

// Build replaces its target directories under dist only after staging succeeds.
// Versioned builds require a clean checkout and use committed payload bytes.
func Build(ctx context.Context, options Options) ([]string, error) {
	if options.Version != "" && !versionPattern.MatchString(options.Version) {
		return nil, fmt.Errorf("release version must be vMAJOR.MINOR.PATCH")
	}
	if options.Version != "" && options.Target != "" {
		return nil, fmt.Errorf("versioned releases always build Windows and Linux; -target is for dist")
	}
	targets := []string{options.Target}
	if options.Version != "" {
		targets = []string{"windows-amd64", "linux-amd64"}
	} else if targets[0] == "" {
		targets[0] = runtime.GOOS + "-amd64"
	}
	for _, target := range targets {
		if target != "windows-amd64" && target != "linux-amd64" {
			return nil, fmt.Errorf("unsupported target %q", target)
		}
	}
	if options.Log == nil {
		options.Log = io.Discard
	}
	src, err := readSource(ctx, options.Root, options.Version != "")
	if err != nil {
		return nil, err
	}
	if src.dirty {
		fmt.Fprintln(options.Log, "Development distribution includes tracked working-tree changes; versioned releases require a clean checkout.")
	}
	out := filepath.Join(src.root, "dist")
	if err := os.MkdirAll(out, 0755); err != nil {
		return nil, err
	}
	if info, err := os.Lstat(out); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("dist must be a real directory")
	}
	var outputs, archives []string
	for _, target := range targets {
		files := runtimeFiles(src.tracked, target)
		directory, err := stage(ctx, src, target, files, options.Log)
		if err != nil {
			return nil, fmt.Errorf("stage %s: %w", target, err)
		}
		outputs = append(outputs, directory)
		if options.Version == "" {
			continue
		}
		files = append(files, binaryName(target))
		suffix := ".tar.gz"
		if strings.HasPrefix(target, "windows-") {
			suffix = ".zip"
		}
		archive := filepath.Join(out, "gektor-"+options.Version+"-"+target+suffix)
		if err := writeArchive(archive, directory, files, src.timestamp); err != nil {
			return nil, err
		}
		archives = append(archives, archive)
		outputs = append(outputs, archive)
	}
	if len(archives) > 0 {
		var sums strings.Builder
		for _, archive := range archives {
			digest, err := fileDigest(archive)
			if err != nil {
				return nil, err
			}
			fmt.Fprintf(&sums, "%x  %s\n", digest, filepath.Base(archive))
		}
		name := filepath.Join(out, "SHA256SUMS.txt")
		if err := os.WriteFile(name, []byte(sums.String()), 0644); err != nil {
			return nil, err
		}
		outputs = append(outputs, name)
	}
	return outputs, nil
}

func git(ctx context.Context, root string, args ...string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", root}, args...)...)
	data, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
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
	module, err := os.ReadFile(filepath.Join(src.root, "go.mod"))
	if err != nil || !strings.HasPrefix(string(module), "module github.com/keshon/gektor\n") && !strings.HasPrefix(string(module), "module github.com/keshon/gektor\r\n") {
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
	seconds, err := strconv.ParseInt(strings.TrimSpace(string(stamp)), 10, 64)
	if err != nil {
		return source{}, err
	}
	src.timestamp = time.Unix(seconds, 0).UTC()
	names, err := git(ctx, src.root, "ls-files", "--cached", "-z")
	if err != nil {
		return source{}, err
	}
	src.tracked = strings.Split(strings.TrimSuffix(string(names), "\x00"), "\x00")
	return src, nil
}

func binaryName(target string) string {
	if strings.HasPrefix(target, "windows-") {
		return "gektor.exe"
	}
	return "gektor"
}

func stage(ctx context.Context, src source, target string, files []string, log io.Writer) (string, error) {
	out := filepath.Join(src.root, "dist")
	temporary, err := os.MkdirTemp(out, ".stage-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(temporary)
	included := map[string]bool{binaryName(target): true}
	for _, name := range files {
		included[name] = true
	}
	for _, name := range files {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		input := filepath.Join(src.root, filepath.FromSlash(name))
		info, err := os.Lstat(input)
		if err != nil || !info.Mode().IsRegular() {
			return "", fmt.Errorf("missing or non-regular release file %s", name)
		}
		var data []byte
		if src.dirty {
			data, err = os.ReadFile(input)
		} else {
			// Git blobs avoid checkout-specific CRLF conversion in release payloads.
			data, err = git(ctx, src.root, "show", src.revision+":"+name)
		}
		if err != nil {
			return "", err
		}
		output := filepath.Join(temporary, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(output), 0755); err != nil {
			return "", err
		}
		if err := os.WriteFile(output, releaseBytes(name, data, included, src.revision), 0644); err != nil {
			return "", err
		}
	}
	osName, _, _ := strings.Cut(target, "-")
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-o", filepath.Join(temporary, binaryName(target)), "./cmd/gektor")
	cmd.Dir, cmd.Env = src.root, append(os.Environ(), "GOOS="+osName, "GOARCH=amd64", "CGO_ENABLED=0")
	cmd.Stdout, cmd.Stderr = log, log
	fmt.Fprintln(log, "Building", target)
	if err := cmd.Run(); err != nil {
		return "", err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !src.dirty {
		after, err := readSource(ctx, src.root, true)
		if err != nil {
			return "", err
		}
		if after.revision != src.revision {
			return "", fmt.Errorf("checkout revision changed while building")
		}
	}
	destination := filepath.Join(out, target)
	if err := os.RemoveAll(destination); err != nil {
		return "", err
	}
	if err := os.Rename(temporary, destination); err != nil {
		return "", err
	}
	return destination, nil
}

func fileDigest(name string) ([sha256.Size]byte, error) {
	file, err := os.Open(name)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return [sha256.Size]byte{}, err
	}
	return [sha256.Size]byte(hash.Sum(nil)), nil
}
