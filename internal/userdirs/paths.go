// Package userdirs locates user data. Callers create directories and own the
// resources stored there; resolving a path has no filesystem effects.
package userdirs

import (
	"os"
	"path/filepath"
)

// DirName is GEKTOR's dot-directory under the user's home.
const DirName = ".gektor"

// Sub joins parts beneath the user's GEKTOR directory without creating it.
// If home lookup fails, the result is relative to the current directory.
// Callers that require home storage must reject a non-absolute result.
func Sub(parts ...string) string {
	home, err := os.UserHomeDir()
	root := DirName
	if err == nil {
		root = filepath.Join(home, DirName)
	}

	elements := append([]string{root}, parts...)
	return filepath.Join(elements...)
}
