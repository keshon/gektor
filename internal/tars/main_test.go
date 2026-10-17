package tars

import (
	"os"
	"testing"
)

// Every path this package produces lives in the user's home directory. A
// test that runs against the real one leaves a .tars behind on the
// developer's machine, and the next run reads it back as though it were
// theirs. Redirect the home for the package instead.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "tars-home")
	if err != nil {
		panic(err)
	}
	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere.
	os.Setenv("USERPROFILE", home)
	os.Setenv("HOME", home)
	code := m.Run()
	os.RemoveAll(home)
	os.Exit(code)
}