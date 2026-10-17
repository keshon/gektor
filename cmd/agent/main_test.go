package main

import (
	"os"
	"testing"
)

// Task state and spilled output live in the user's TARS directory. A test
// that runs a real agent flow writes real task directories there, and the
// next run lists them as the user's own sessions. Redirect the home.
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