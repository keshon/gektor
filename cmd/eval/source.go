package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// Include dirty and untracked Go sources, without hashing ignored artifacts or secrets.
func sourceDigest() (string, error) {
	root, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	files, err := exec.Command("git", "-C", strings.TrimSpace(string(root)), "ls-files", "-z", "--cached", "--others", "--exclude-standard").Output()
	if err != nil {
		return "", err
	}
	names := strings.Split(string(files), "\x00")
	sort.Strings(names)
	h := sha256.New()
	last := ""
	for _, name := range names {
		if name == last || !(strings.HasSuffix(name, ".go") || name == "go.mod" || name == "go.sum") {
			continue
		}
		last = name
		data, err := os.ReadFile(filepath.Join(strings.TrimSpace(string(root)), name))
		if os.IsNotExist(err) {
			data = []byte("<deleted>")
		} else if err != nil {
			return "", err
		}
		fmt.Fprintf(h, "%s\x00%x\x00", name, sha256.Sum256(data))
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
