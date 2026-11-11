package agent

import (
	"os"
	"testing"

	"github.com/keshon/gektor/internal/workflow/script"
)

// Isolate the default user directory for tests that do not supply a session root.
func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-script-worker" {
		if err := script.Serve(os.Stdin, os.Stdout); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	dir, err := os.MkdirTemp("", "gektor-tui-sessions")
	if err != nil {
		panic(err)
	}
	err = os.Setenv("USERPROFILE", dir)
	if err != nil {
		panic(err)
	}
	err = os.Setenv("HOME", dir)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
