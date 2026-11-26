package run

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	commandrun "github.com/keshon/gektor/internal/tools/command"
)

func TestForegroundSchemasMatchTimeoutPolicy(t *testing.T) {
	for _, tool := range []agent.Tool{Shell{}, Program{}} {
		t.Run(tool.Name(), func(t *testing.T) {
			var schema struct {
				Properties map[string]struct {
					Minimum int
					Maximum int
					Default int
				}
			}
			err := json.Unmarshal(tool.Schema(), &schema)
			if err != nil {
				t.Fatal(err)
			}
			timeout := schema.Properties["timeout_seconds"]
			actualDefault, err := commandrun.Timeout(nil)
			if err != nil {
				t.Fatal(err)
			}
			if timeout.Minimum != 1 || timeout.Maximum != 600 || time.Duration(timeout.Default)*time.Second != actualDefault {
				t.Fatalf("advertised timeout differs from execution: %+v, actual default=%v", timeout, actualDefault)
			}
		})
	}
}

func TestRunProgramHonorsDeadline(t *testing.T) {
	for _, scenario := range []struct {
		name           string
		seconds        int
		parentDeadline time.Duration
	}{
		{name: "explicit tool timeout", seconds: 1, parentDeadline: 10 * time.Second},
		{name: "earlier parent deadline", seconds: 600, parentDeadline: 100 * time.Millisecond},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ws, root := testWS(t)
			t.Setenv("GEKTOR_STREAM_TEST_GATE", filepath.Join(root, "never-released"))
			input := programInput{
				Program: os.Args[0], Args: []string{"-test.run=^TestShellStreamingHelper$"}, TimeoutSeconds: &scenario.seconds,
			}
			args, err := json.Marshal(input)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), scenario.parentDeadline)
			defer cancel()
			tool := Program{WS: ws}
			output, err := tool.Run(ctx, args)
			if !errors.Is(err, context.DeadlineExceeded) || strings.Contains(output, "exit_code:") {
				t.Fatalf("deadline lost or completion invented: output=%q err=%v", output, err)
			}
		})
	}
}
