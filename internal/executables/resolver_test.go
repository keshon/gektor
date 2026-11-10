package executables

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/process"
)

func TestIndependentProgramProbesDoNotWaitForAnotherVersionCommand(t *testing.T) {
	config := filepath.Join(t.TempDir(), "applications.json")
	data, _ := json.Marshal(Config{Applications: map[string]Setting{"ffmpeg": {Path: os.Args[0]}, "node": {Path: os.Args[0]}}})
	if err := os.WriteFile(config, data, 0600); err != nil {
		t.Fatal(err)
	}
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	r := &Resolver{ConfigPath: config, run: func(ctx context.Context, in process.Request) (process.Result, error) {
		if in.Args[0] == "-version" {
			close(started)
			<-release
		}
		return process.Result{Stdout: []byte("fixture version")}, nil
	}}
	go func() { _, _ = r.Probe(t.Context(), "ffmpeg", true) }()
	<-started
	result := make(chan error, 1)
	go func() { _, err := r.Probe(t.Context(), "node", true); result <- err }()
	select {
	case err := <-result:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("an independent probe waited behind a blocked version command")
	}
}

func TestProbeConfigPrecedenceCacheAndDisabled(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tool.exe")
	if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(dir, "applications.json")
	write := func(setting Setting) {
		raw, _ := json.Marshal(Config{Applications: map[string]Setting{"ffmpeg": setting}})
		if err := os.WriteFile(config, raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(Setting{Path: path})
	calls := 0
	r := &Resolver{ConfigPath: config, run: func(ctx context.Context, in process.Request) (process.Result, error) {
		calls++
		if in.Program != path || in.Timeout.Seconds() != 3 || len(in.Args) != 1 || in.Args[0] != "-version" {
			t.Fatal(in)
		}
		return process.Result{Stdout: []byte("ffmpeg version fixture\nmore")}, nil
	}}
	for range 2 {
		s, err := r.Probe(t.Context(), "ffmpeg", false)
		if err != nil || s.State != "ready" || s.Path != path {
			t.Fatal(s, err)
		}
	}
	if calls != 1 {
		t.Fatal("probe not cached", calls)
	}
	if _, err := r.Require(t.Context(), "ffmpeg"); err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatal("execution reused stale observation")
	}
	write(Setting{Disabled: true})
	s, err := r.Probe(t.Context(), "ffmpeg", false)
	if err != nil || s.State != "disabled" || calls != 2 {
		t.Fatal(s, err, calls)
	}
	write(Setting{Path: "relative.exe"})
	if _, err := r.Probe(t.Context(), "ffmpeg", true); err == nil {
		t.Fatal("relative configured executable accepted")
	}
	if _, err := r.Probe(t.Context(), "unknown", true); err == nil {
		t.Fatal("arbitrary application accepted")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := r.Probe(ctx, "ffmpeg", false); err == nil {
		t.Fatal("cancellation ignored")
	}
}
func TestProbeMissingAndMalformedSettings(t *testing.T) {
	r := &Resolver{ConfigPath: filepath.Join(t.TempDir(), "missing.json")}
	t.Setenv("PATH", t.TempDir())
	s, err := r.Probe(t.Context(), "ffmpeg", false)
	if err != nil || s.State != "missing" {
		t.Fatal(s, err)
	}
	if err := os.WriteFile(r.ConfigPath, []byte(`{"applications":{"ffmpeg":{"extra":true}}}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.Probe(t.Context(), "ffmpeg", false); err == nil {
		t.Fatal("invalid settings silently ignored")
	}
}
