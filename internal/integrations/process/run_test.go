package process

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestProcessHelper(t *testing.T) {
	index := -1
	for i, s := range os.Args {
		if s == "--process-helper" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	switch os.Args[index+1] {
	case "echo":
		data, _ := io.ReadAll(os.Stdin)
		_ = json.NewEncoder(os.Stdout).Encode(map[string]any{"args": os.Args[index+2:], "input": string(data), "secret": os.Getenv("GEKTOR_TEST_SECRET")})
		fmt.Fprint(os.Stderr, "diagnostic")
	case "fail":
		fmt.Fprint(os.Stdout, "before failure")
		fmt.Fprint(os.Stderr, "failure details")
		os.Exit(7)
	case "wait":
		fmt.Fprint(os.Stdout, "started")
		if len(os.Args) > index+2 {
			if err := os.WriteFile(os.Args[index+2], nil, 0600); err != nil {
				os.Exit(1)
			}
		}
		time.Sleep(30 * time.Second)
	case "flood":
		for {
			fmt.Fprint(os.Stdout, strings.Repeat("x", 8192))
		}
	}
	os.Exit(0)
}

func helperRequest(t *testing.T, mode string) Request {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Request{Program: exe, Args: []string{"-test.run=^TestProcessHelper$", "--", "--process-helper", mode}, Directory: t.TempDir(), Timeout: 5 * time.Second}
}

func TestRunLiteralArgumentsInputAndScrubbedEnvironment(t *testing.T) {
	t.Setenv("GEKTOR_TEST_SECRET", "must not inherit")
	in := helperRequest(t, "echo")
	literal := []string{"a b", "$(echo bad)", "a&b", `quote"value`, "*.txt"}
	in.Args = append(in.Args, literal...)
	in.Stdin = strings.NewReader("artifact bytes")
	r, err := Run(t.Context(), in)
	if err != nil || r.ExitCode != 0 || string(r.Stderr) != "diagnostic" {
		t.Fatal(r, err)
	}
	var out struct {
		Args          []string
		Input, Secret string
	}
	if err := json.Unmarshal(r.Stdout, &out); err != nil {
		t.Fatal(string(r.Stdout), err)
	}
	a, _ := json.Marshal(out.Args)
	b, _ := json.Marshal(literal)
	if !bytes.Equal(a, b) || out.Input != "artifact bytes" || out.Secret != "" {
		t.Fatal(out)
	}
}

func TestRunFailureTimeoutAndOutputBounds(t *testing.T) {
	r, err := Run(t.Context(), helperRequest(t, "fail"))
	if err == nil || r.ExitCode != 7 || string(r.Stdout) != "before failure" || string(r.Stderr) != "failure details" {
		t.Fatal(r, err)
	}
	in := helperRequest(t, "wait")
	in.Timeout = 300 * time.Millisecond
	start := time.Now()
	r, err = Run(t.Context(), in)
	// The deadline includes process startup; a loaded machine may stop the
	// helper before it writes anything. Retention after readiness is tested below.
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(start) > 8*time.Second {
		t.Fatal(r, err, time.Since(start))
	}
	r, err = Run(t.Context(), helperRequest(t, "flood"))
	if err == nil || !r.Truncated || len(r.Stdout) != OutputLimit {
		t.Fatal(len(r.Stdout), r.Truncated, err)
	}
}

func TestCancellationRetainsOutputAfterProcessReadiness(t *testing.T) {
	in := helperRequest(t, "wait")
	ready := filepath.Join(in.Directory, "ready")
	in.Args = append(in.Args, ready)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	type outcome struct {
		result Result
		err    error
	}
	done := make(chan outcome, 1)
	go func() { result, err := Run(ctx, in); done <- outcome{result, err} }()
	poll := time.NewTicker(5 * time.Millisecond)
	defer poll.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		select {
		case out := <-done:
			t.Fatalf("process ended before readiness: %+v", out)
		case <-poll.C:
		}
	}
	cancel()
	out := <-done
	if !errors.Is(out.err, context.Canceled) || string(out.result.Stdout) != "started" {
		t.Fatalf("result=%+v err=%v", out.result, out.err)
	}
}
