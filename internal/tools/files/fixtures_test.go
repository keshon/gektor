package files

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "gektor-files-home")
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
