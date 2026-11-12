package tools

import (
	"fmt"
	"os"
	"testing"

	"github.com/keshon/gektor/internal/capabilities/documents"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "workflow" && os.Args[2] == "internal-document-worker" {
		if err := documents.Serve(os.Stdin, os.Stdout); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if os.Getenv("GEKTOR_EXEC_HELPER") == "1" {
		writeLiteralExecutionArgs()
		return
	}
	// Spilled tool output lands in the user's GEKTOR directory. A test that
	// exercises spilling must not write into the developer's real one.
	home, err := os.MkdirTemp("", "gektor-home")
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
