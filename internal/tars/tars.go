// Package tars resolves the one directory TARS keeps its data in.
//
// Everything the harness persists for the user lives under a single
// dot-directory in their home, like .claude or .codex: preferences, the
// session library, task transcripts, mission ledgers and spilled tool
// output. It sits beside the executable only by accident of an early
// layout, and a var/ directory came in with a merged project -- two homes
// for the same data, neither of them documented.
//
// Nothing here belongs in a workspace. A run writes its task directory to
// the user's home, so pointing TARS at a different project never splits
// the session library, and a workspace is never dirtied by merely running
// against it.
package tars

import (
	"os"
	"path/filepath"
)

// DirName is the directory itself. Dotted, so it is obvious in a home
// directory listing, and unlikely to collide with anything a user makes.
const DirName = ".tars"

// Sub returns a path inside the user's TARS directory without creating
// anything: tars.Sub("tasks", id) is the task directory for id. Callers
// that write there create it themselves.
func Sub(parts ...string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		// No home means no user data. Callers fall back to whatever
		// they were doing before; nothing here can silently scatter
		// files into the working directory.
		return filepath.Join(append([]string{DirName}, parts...)...)
	}
	return filepath.Join(append([]string{home, DirName}, parts...)...)
}
