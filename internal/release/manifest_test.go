package release

import (
	"bytes"
	"context"
	"os"
	"path"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestManifestExcludesSecretsAndWorkRecords(t *testing.T) {
	files := runtimeFiles([]string{
		"docs/start.md", "docs/internal/plan.md", "docs/design/tui/mockup.fig",
		"docs/design/tui/README.md", ".env", "skills/browser/SKILL.md",
		"docs/reference/cli.md", "docs/images/gektor-demo.webp", "gektor.exe",
		"internal/release/build.go", "bin/credentials.json",
	}, "linux-amd64")
	want := []string{"docs/images/gektor-demo.webp", "docs/reference/cli.md", "docs/start.md", "skills/browser/SKILL.md"}
	if !reflect.DeepEqual(files, want) {
		t.Fatal(files)
	}
}

func TestOfflineLinksAndFrozenSkills(t *testing.T) {
	data := []byte("[Local](reference/cli.md#flags) [Self](#here) [Source](../cmd/gektor/options.go) [Web](https://example.com)\r\n")
	included := map[string]bool{"docs/start.md": true, "docs/reference/cli.md": true}
	result := string(releaseBytes("docs/start.md", data, included, "abc123"))
	for _, want := range []string{
		"](reference/cli.md#flags)", "](#here)", "](https://example.com)",
		"](https://github.com/keshon/gektor/blob/abc123/cmd/gektor/options.go)",
	} {
		if !strings.Contains(result, want) {
			t.Fatal(result)
		}
	}
	for _, name := range []string{"skills/README.md", "skills/browser/SKILL.md", "internal/workspace/builtin/workflows/SKILL.md"} {
		if !bytes.Equal(releaseBytes(name, data, nil, "abc123"), data) {
			t.Fatal("frozen skill changed", name)
		}
	}
}

func TestCurrentArticlesKeepRemainingLinksInsideDistribution(t *testing.T) {
	root, err := filepath.Abs("../..")
	if err != nil {
		t.Fatal(err)
	}
	names, err := git(t.Context(), root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range []string{"windows-amd64", "linux-amd64"} {
		files := runtimeFiles(strings.Split(strings.TrimSuffix(string(names), "\x00"), "\x00"), target)
		included := map[string]bool{binaryName(target): true}
		for _, name := range files {
			included[name] = true
		}
		for _, name := range files {
			if !strings.HasSuffix(name, ".md") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
			if err != nil {
				t.Fatal(err)
			}
			for _, match := range markdownLink.FindAllSubmatch(releaseBytes(name, data, included, "test-revision"), -1) {
				href := string(match[1])
				if absoluteLink.MatchString(href) || strings.HasPrefix(name, "skills/") || strings.HasSuffix(name, "/SKILL.md") {
					continue
				}
				location, _, _ := strings.Cut(href, "#")
				destination := name
				if location != "" {
					destination = path.Clean(path.Join(path.Dir(name), location))
				}
				if !included[destination] {
					t.Fatalf("%s: %s points outside %s payload", name, href, target)
				}
			}
		}
	}
}

func TestBuildRejectsInvalidVersionAndTargetBeforeWriting(t *testing.T) {
	for _, options := range []Options{
		{Version: "v1.2"}, {Version: "../v1.2.3"}, {Target: "../windows-amd64"},
		{Version: "v1.2.3", Target: "windows-amd64"},
	} {
		options.Root = t.TempDir()
		if _, err := Build(context.Background(), options); err == nil {
			t.Fatal("accepted invalid options", options)
		}
	}
}
