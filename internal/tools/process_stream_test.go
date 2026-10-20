package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/workspace"
)

func TestCommandOutputStreamsSplitUTF8AndBoundsPreview(t *testing.T) {
	var chunks []string
	out := &commandOutput{onOutput: func(text string) {
		if !utf8.ValidString(text) {
			t.Errorf("invalid UTF-8 chunk: %q", text)
		}
		chunks = append(chunks, text)
	}}
	for _, b := range []byte("привет 😀\n") {
		out.Write([]byte{b})
	}
	if got := strings.Join(chunks, ""); got != "привет 😀\n" {
		t.Fatalf("preview=%q", got)
	}
	out.Write([]byte(strings.Repeat("x", shellMaxBytes*3) + "last log line"))
	out.flush()
	preview := strings.Join(chunks, "")
	if len(preview) > shellMaxBytes+200 || strings.Count(preview, "live preview limit reached") != 1 {
		t.Fatalf("unbounded preview or repeated notice: %d bytes", len(preview))
	}
	if !strings.HasSuffix(out.String(), "last log line") {
		t.Fatal("final result lost recent output")
	}
}

// The child cannot finish until the parent observes its output. This proves
// delivery during execution rather than replay after process exit.
func TestShellStreamingHelper(t *testing.T) {
	gate := os.Getenv("GEKTOR_STREAM_TEST_GATE")
	if gate == "" {
		return
	}
	fmt.Fprint(os.Stdout, "first output\n")
	deadline := time.Now().Add(15 * time.Second)
	for {
		if _, err := os.Stat(gate); err == nil {
			break
		}
		if time.Now().After(deadline) {
			os.Exit(8)
		}
		time.Sleep(10 * time.Millisecond)
	}
	fmt.Fprint(os.Stderr, "последняя строка\n")
	if os.Getenv("GEKTOR_STREAM_TEST_FAIL") == "1" {
		os.Exit(7)
	}
}

func TestShellOutputArrivesBeforeExitAndSurvivesFailureOrCancellation(t *testing.T) {
	for _, mode := range []string{"success", "failure", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			gate := filepath.Join(root, "release")
			t.Setenv("GEKTOR_STREAM_TEST_GATE", gate)
			fail := "0"
			if mode == "failure" {
				fail = "1"
			}
			t.Setenv("GEKTOR_STREAM_TEST_FAIL", fail)
			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			ws, err := workspace.New(root)
			if err != nil {
				t.Fatal(err)
			}
			command := fmt.Sprintf("'%s' -test.run=^TestShellStreamingHelper$", os.Args[0])
			if runtime.GOOS == "windows" {
				t.Setenv("GEKTOR_STREAM_TEST_EXE", "\""+os.Args[0]+"\"")
				command = "call %GEKTOR_STREAM_TEST_EXE% -test.run=TestShellStreamingHelper"
			}
			args, _ := json.Marshal(map[string]string{"command": command})
			var live strings.Builder
			var releaseErr error
			result, err := (RunShell{WS: ws}).RunStreaming(ctx, args, func(text string) {
				live.WriteString(text)
				if strings.Contains(live.String(), "first output") {
					if mode == "cancel" {
						cancel()
					} else {
						releaseErr = os.WriteFile(gate, nil, 0600)
					}
				}
			})
			if releaseErr != nil {
				t.Fatal(releaseErr)
			}
			if !strings.Contains(live.String(), "first output") || !strings.Contains(result, "first output") {
				t.Fatalf("live=%q result=%q err=%v", live.String(), result, err)
			}
			if mode == "success" && err != nil {
				t.Fatal(err)
			}
			if mode != "success" && err == nil {
				t.Fatal("expected failure or cancellation")
			}
			if mode != "cancel" && !strings.Contains(result, "последняя строка") {
				t.Fatalf("stderr missing: %q", result)
			}
			if mode == "cancel" && ctx.Err() != context.Canceled {
				t.Fatalf("cancel=%v", ctx.Err())
			}
		})
	}
}
