package tools

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestBrowserPackagePreparation(t *testing.T) {
	for _, mode := range []string{"cached", "missing", "failed", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			var steps []bool
			var notes []string
			err := prepareBrowserPackage(ctx, func(s string) { notes = append(notes, s) }, func(ctx context.Context, install bool) (string, error) {
				steps = append(steps, install)
				if _, ok := ctx.Deadline(); !ok {
					t.Fatal("unbounded setup")
				}
				if mode == "cached" || install && mode == "missing" {
					return "help", nil
				}
				if mode == "cancelled" {
					cancel()
				}
				return "npm network unavailable", fmt.Errorf("setup error")
			})
			wantSteps := 2
			if mode == "cached" || mode == "cancelled" {
				wantSteps = 1
			}
			if len(steps) != wantSteps || steps[0] || wantSteps == 2 && !steps[1] {
				t.Fatal(steps)
			}
			if (err == nil) != (mode == "cached" || mode == "missing") {
				t.Fatal(err)
			}
			if mode == "failed" && (!strings.Contains(err.Error(), "npm network unavailable") || !strings.Contains(err.Error(), "-browser=")) {
				t.Fatal(err)
			}
			if len(notes) == 0 {
				t.Fatal("silent setup")
			}
		})
	}
}

func TestMissingNodeHasActionableBrowserError(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	err := PrepareBrowser(t.Context(), "chrome", t.TempDir(), nil)
	if err == nil || !strings.Contains(err.Error(), "Node.js") || !strings.Contains(err.Error(), "reopen") || !strings.Contains(err.Error(), "-browser=") {
		t.Fatal(err)
	}
}
