package subprocess

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestMain(m *testing.M) {
	if len(os.Args) == 3 && os.Args[1] == "workflow" {
		// The parent may close fixture pipes while enforcing limits. These dummy
		// workers intentionally ignore output errors and let Run report the verdict.
		switch os.Args[2] {
		case "test-limited":
			err := Limit()
			if err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			data := make([]byte, 32<<20)
			for i := 0; i < len(data); i += 4096 {
				data[i] = 1
			}
			fmt.Fprintf(os.Stdout, `{"bytes":%d,"last_page":%d}`, len(data), data[len(data)-4096])
			return
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
	_, err = Run(t.Context(), "test-large", nil, 5*time.Second, 1024)
	if err == nil {
		t.Fatal("accepted oversized worker output")
	}
	_, err = Run(t.Context(), "test-invalid", nil, 5*time.Second, 1024)
	if err == nil {
		t.Fatal("accepted invalid worker output")
	}
}

func TestLinuxAddressLimitAllowsRuntimeReservations(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Linux RLIMIT_AS accounts for reserved mappings; Windows uses a separate job memory limit")
	}
	out, err := Run(t.Context(), "test-limited", nil, 10*time.Second, 1024)
	if err != nil || out != `{"bytes":33554432,"last_page":1}` {
		t.Fatal(out, err)
	}
}
