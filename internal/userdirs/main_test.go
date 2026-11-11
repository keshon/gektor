package userdirs

import (
	"fmt"
	"os"
	"testing"
)

// Package tests own an isolated home. No test may read or leave data in the
// developer's real GEKTOR directory. TestMain removes it before os.Exit.
func TestMain(m *testing.M) {
	directory, err := os.MkdirTemp("", "gektor-home")
	if err != nil {
		panic(err)
	}

	// os.UserHomeDir reads USERPROFILE on Windows and HOME elsewhere.
	for _, variable := range []string{"USERPROFILE", "HOME"} {
		err = os.Setenv(variable, directory)
		if err != nil {
			// Preserve the setup failure; cleanup is best effort on this exit.
			_ = os.RemoveAll(directory)
			panic(err)
		}
	}

	code := m.Run()
	err = os.RemoveAll(directory)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		code = 1
	}

	os.Exit(code)
}
