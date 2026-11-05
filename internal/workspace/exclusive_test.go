package workspace

import (
	"os"
	"testing"
)

func TestExclusiveCreatePreservesExistingFile(t *testing.T) {
	ws, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	f, err := ws.CreateExclusive("output", 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("original"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if f, err := ws.CreateExclusive("output", 0600); err == nil {
		f.Close()
		t.Fatal("exclusive create replaced existing file")
	}
	data, err := ws.ReadFile("output")
	if err != nil || string(data) != "original" {
		t.Fatal("existing contents changed", err)
	}
	if f, err := ws.CreateExclusive("../escape", os.FileMode(0600)); err == nil {
		f.Close()
		t.Fatal("escaped root")
	}
}
