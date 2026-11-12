package run

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GEKTOR_EXEC_HELPER") == "1" {
		writeLiteralExecutionArgs()
		return
	}
	// Helpers inherit the parent's isolated home and may exit directly.
	if os.Getenv("GEKTOR_STREAM_TEST_GATE") != "" {
		code := m.Run()
		os.Exit(code)
	}
	home, err := os.MkdirTemp("", "gektor-run-home")
	if err != nil {
		panic(err)
	}
	err = os.Setenv("USERPROFILE", home)
	if err != nil {
		panic(err)
	}
	err = os.Setenv("HOME", home)
	if err != nil {
		panic(err)
	}
	code := m.Run()
	err = os.RemoveAll(home)
	if err != nil {
		panic(err)
	}
	os.Exit(code)
}

func writeLiteralExecutionArgs() {
	for i, arg := range os.Args {
		switch arg {
		case "--literal-args":
			dir, err := os.Getwd()
			if err != nil {
				os.Exit(2)
			}
			err = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[i+1:], "dir": dir})
			if err != nil {
				os.Exit(2)
			}
			return
		case "--quiet-success":
			return
		case "--exit-result":
			fmt.Fprintln(os.Stdout, "stdout marker")
			fmt.Fprintln(os.Stderr, "stderr marker")
			os.Exit(3)
		}
	}
	os.Exit(2)
}
