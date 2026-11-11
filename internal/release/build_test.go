package release

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildReleaseFromCleanRepository(t *testing.T) {
	root := t.TempDir()
	contents := map[string]string{
		"go.mod":             "module github.com/keshon/gektor\n\ngo 1.26.11\n",
		"cmd/gektor/main.go": "package main\nfunc main() {}\n",
		".gitignore":         "/dist/\n",
		".env":               "SECRET=not-for-distribution\n",
		"README.md":          "[Source](cmd/gektor/main.go)\n",
		"skills/SKILL.md":    "Frozen\n",
	}
	for _, name := range windowsFiles() {
		contents[name] = "@echo off\r\n"
	}
	for name, data := range contents {
		relative := filepath.FromSlash(name)
		filename := filepath.Join(root, relative)
		parent := filepath.Dir(filename)
		err := os.MkdirAll(parent, 0755)
		if err != nil {
			t.Fatal(err)
		}
		content := []byte(data)
		err = os.WriteFile(filename, content, 0644)
		if err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{
		{"init", "--quiet"}, {"add", "--all"},
		{"-c", "user.name=Packaging Test", "-c", "user.email=packaging@example.invalid", "-c", "commit.gpgsign=false", "commit", "--quiet", "-m", "fixture"},
	} {
		runTestGit(t, root, args...)
	}
	outputs, err := Build(t.Context(), Options{Root: root, Version: "v1.2.3", Log: io.Discard})
	if err != nil {
		t.Fatal(err)
	}
	if len(outputs) != 5 {
		t.Fatal(outputs)
	}
	checksumPath := filepath.Join(root, "dist", "SHA256SUMS.txt")
	sums, err := os.ReadFile(checksumPath)
	if err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"windows-amd64.zip", "linux-amd64.tar.gz"} {
		name := "gektor-v1.2.3-" + suffix
		archivePath := filepath.Join(root, "dist", name)
		digest, digestErr := fileDigest(archivePath)
		if digestErr != nil {
			t.Fatal(digestErr)
		}
		expected := fmt.Sprintf("%x  %s\n", digest, name)
		if !strings.Contains(string(sums), expected) {
			t.Fatal("checksum differs", name, digestErr)
		}
	}
	for target, magic := range map[string]string{"windows-amd64": "MZ", "linux-amd64": "\x7fELF"} {
		directory := filepath.Join(root, "dist", target)
		executable := binaryName(target)
		binaryPath := filepath.Join(directory, executable)
		binary, binaryErr := os.ReadFile(binaryPath)
		if binaryErr != nil || !strings.HasPrefix(string(binary), magic) {
			t.Fatal("incorrect executable target", target, binaryErr)
		}
		secret := filepath.Join(directory, ".env")
		_, statErr := os.Stat(secret)
		if !os.IsNotExist(statErr) {
			t.Fatal("secret included in payload", statErr)
		}
		skillPath := filepath.Join(directory, "skills", "SKILL.md")
		frozen, skillErr := os.ReadFile(skillPath)
		if skillErr != nil || string(frozen) != "Frozen\n" {
			t.Fatal("frozen skill bytes differ", skillErr)
		}
	}
}
