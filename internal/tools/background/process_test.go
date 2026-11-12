package background

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"testing"
)

func TestMain(m *testing.M) {
	if os.Getenv("GEKTOR_EXEC_HELPER") == "1" {
		writeLiteralExecutionArgs()
		return
	}
	// Helpers inherit the parent's isolated home and may exit directly.
	if os.Getenv("GEKTOR_BACKGROUND_HELPER") != "" {
		code := m.Run()
		os.Exit(code)
	}
	home, err := os.MkdirTemp("", "gektor-background-home")
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
		case "--spawn-http-child":
			child := exec.Command(os.Args[0], "--http-fixture")
			child.Stdout, child.Stderr = os.Stdout, os.Stderr
			err := child.Run()
			if err != nil {
				os.Exit(4)
			}
			return
		case "--http-fixture":
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				os.Exit(3)
			}
			fmt.Println("url: http://" + listener.Addr().String())
			// The fixture intentionally serves until the parent stops its process tree.
			_ = http.Serve(listener, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "managed server works") }))
			return
		}
	}
	os.Exit(2)
}
