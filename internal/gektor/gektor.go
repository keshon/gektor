package gektor

import (
	"os"
	"path/filepath"
)

// DirName is the directory itself. Dotted, so it is obvious in a home
// directory listing, and unlikely to collide with anything a user makes.
const DirName = ".gektor"

// Sub returns a path inside the user's GEKTOR directory without creating
// anything: gektor.Sub("tasks", id) is the task directory for id. Callers
// that write there create it themselves.
func Sub(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		// No home means no user data. Callers fall back to whatever
		// they were doing before; nothing here can silently scatter
		// files into the working directory.
		return filepath.Join(append([]string{DirName}, parts...)...)
	}
	root := filepath.Join(home, DirName)
	return filepath.Join(append([]string{root}, parts...)...)
}
