package release

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReleaseFromCleanRepository(t *testing.T) {
	root := t.TempDir()
	contents := map[string]string{
		"go.mod":             "module github.com/keshon/gektor\n\ngo 1.26.10\n",
		"cmd/gektor/main.go": "package main\nfunc main() {}\n",
		".gitignore":         "/dist/\n",
		".env":               "SECRET=not-for-distribution\n",
		"README.md":          "[Source](cmd/gektor/main.go)\n",
		"skills/SKILL.md":    "Frozen\n",
	}
	for _, name := range windowsFiles {
		contents[name] = "@echo off\r\n"
	}
	for name, data := range contents {
		filename := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(filename), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filename, []byte(data), 0644); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "--quiet"}, {"add", "--all"},
		{"-c", "user.name=Packaging Test", "-c", "user.email=packaging@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture"},
	} {
		cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
		if output, err := cmd.CombinedOutput(); err != nil {
			t.Fatal(string(output), err)
		}
	}
	outputs, err := Build(t.Context(), Options{Root: root, Version: "v1.2.3", Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 5 {
		t.Fatal(outputs)
	}
	sums, err := os.ReadFile(filepath.Join(root, "dist", "SHA256SUMS.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"windows-amd64.zip", "linux-amd64.tar.gz"} {
		name := "gektor-v1.2.3-" + suffix
		digest, err := fileDigest(filepath.Join(root, "dist", name))
		if err != nil || !strings.Contains(string(sums), fmt.Sprintf("%x  %s\n", digest, name)) {
			t.Fatal("checksum differs", name, err)
		}
	}
	for target, magic := range map[string]string{"windows-amd64": "MZ", "linux-amd64": "\x7fELF"} {
		directory := filepath.Join(root, "dist", target)
		binary, err := os.ReadFile(filepath.Join(directory, binaryName(target)))
		if err != nil || !strings.HasPrefix(string(binary), magic) {
			t.Fatal("incorrect executable target", target, err)
		}
		if _, err := os.Stat(filepath.Join(directory, ".env")); !os.IsNotExist(err) {
			t.Fatal("secret included in payload", err)
		}
		frozen, err := os.ReadFile(filepath.Join(directory, "skills", "SKILL.md"))
		if err != nil || string(frozen) != "Frozen\n" {
			t.Fatal("frozen skill bytes differ", err)
		}
	}
}
