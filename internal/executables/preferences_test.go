package executables

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/process"
)

func TestSaveValidatesBeforeReplacingAndClearsProbeCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "applications.json")
	r := New(path)
	calls := 0
	r.run = func(context.Context, process.Request) (process.Result, error) {
		calls++
		return process.Result{Stdout: []byte("fixture 1")}, nil
	}
	want := Config{Applications: map[string]Setting{"ffmpeg": {Path: os.Args[0]}, "node": {Disabled: true}}}
	err := r.Save(want)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Probe(t.Context(), "ffmpeg", false)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Probe(t.Context(), "ffmpeg", false)
	if err != nil || calls != 1 {
		t.Fatal(calls, err)
	}
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []Config{
		{Applications: map[string]Setting{"ffmpeg": {Path: "relative.exe"}}},
		{Applications: map[string]Setting{"unknown": {}}},
		{Applications: map[string]Setting{"ffmpeg": {Path: filepath.Join(t.TempDir(), strings.Repeat("a", 33000))}}},
	} {
		err = r.Save(bad)
		if err == nil {
			t.Fatal("invalid preferences saved")
		}
		saved, readErr := os.ReadFile(path)
		if readErr != nil || string(saved) != string(original) {
			t.Fatal("failed save damaged existing file", readErr)
		}
	}
	err = r.Save(want)
	if err != nil {
		t.Fatal(err)
	}
	_, err = r.Probe(t.Context(), "ffmpeg", false)
	if err != nil || calls != 2 {
		t.Fatal("save retained stale probe cache", calls, err)
	}
	loaded, err := r.Load()
	if err != nil || !loaded.Applications["node"].Disabled || loaded.Applications["ffmpeg"].Path != os.Args[0] {
		t.Fatal(loaded, err)
	}
}

func TestSaveFailureRemovesTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "applications.json")
	err := os.Mkdir(target, 0700)
	if err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(target, "keep")
	err = os.WriteFile(sentinel, []byte("keep"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	err = New(target).Save(Config{Applications: map[string]Setting{"node": {Disabled: true}}})
	if err == nil {
		t.Fatal("replaced a directory")
	}
	files, err := filepath.Glob(filepath.Join(dir, ".applications-*.json"))
	if err != nil || len(files) != 0 {
		t.Fatal("temporary file leaked", files, err)
	}
	saved, err := os.ReadFile(sentinel)
	if err != nil || string(saved) != "keep" {
		t.Fatal("save damaged target", err)
	}
}
