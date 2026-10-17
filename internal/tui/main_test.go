package tui

import (
	"os"
	"testing"
)

// Sessions and task directories live in the user's home directory. A test
// that starts a run, creates a session or lists saved ones would otherwise
// write real session dirs there -- which is not a failure the suite would
// notice, just a developer's own library filling up with "hello?".
//
// Redirecting tasksRoot once here means no individual test can forget. The
// helpers that need a specific tree (sessionsRootFor, chdirSessions) still
// override it per test and restore on cleanup.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "tars-tui-sessions")
	if err != nil {
		panic(err)
	}
	tasksRoot = func() string { return dir }
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}