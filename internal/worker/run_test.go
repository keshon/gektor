package worker

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "workflow" {
		switch os.Args[2] {
		case "test-hang":
			time.Sleep(time.Minute)
			return
		case "test-large":
			_, _ = os.Stdout.WriteString(`"` + strings.Repeat("x", 4096) + `"`)
			return
		case "test-invalid":
			_, _ = os.Stdout.WriteString("not json")
			return
		}
	}
	os.Exit(m.Run())
}

func TestWorkerDeadlineAndOutputLimits(t *testing.T) {
	_, err := Run(t.Context(), "test-hang", nil, 500*time.Millisecond, 1024)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("worker deadline was lost", err)
	}
	if _, err := Run(t.Context(), "test-large", nil, 5*time.Second, 1024); err == nil {
		t.Fatal("accepted oversized worker output")
	}
	if _, err := Run(t.Context(), "test-invalid", nil, 5*time.Second, 1024); err == nil {
		t.Fatal("accepted invalid worker output")
	}
}
