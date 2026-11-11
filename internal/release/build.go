package release

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

func distributionTargets(options Options) ([]string, error) {
	versionPattern := regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	if options.Version != "" && !versionPattern.MatchString(options.Version) {
		return nil, fmt.Errorf("release version must be vMAJOR.MINOR.PATCH")
	}
	if options.Version != "" && options.Target != "" {
		return nil, fmt.Errorf("versioned releases always build Windows and Linux; -target is for dist")
	}
	if options.Version != "" {
		return []string{"windows-amd64", "linux-amd64"}, nil
	}

	target := options.Target
	if target == "" {
		target = runtime.GOOS + "-amd64"
	}
	if target != "windows-amd64" && target != "linux-amd64" {
		return nil, fmt.Errorf("unsupported target %q", target)
	}

	return []string{target}, nil
}

func binaryName(target string) string {
	if strings.HasPrefix(target, "windows-") {
		return "gektor.exe"
	}

	return "gektor"
}

func writeChecksums(directory string, archives []string) (string, error) {
	var sums strings.Builder
	for _, archive := range archives {
		digest, err := fileDigest(archive)
		if err != nil {
			return "", err
		}

		name := filepath.Base(archive)
		fmt.Fprintf(&sums, "%x  %s\n", digest, name)
	}

	name := filepath.Join(directory, "SHA256SUMS.txt")
	data := []byte(sums.String())
	err := os.WriteFile(name, data, 0644)
	if err != nil {
		return "", err
	}

	return name, nil
}
