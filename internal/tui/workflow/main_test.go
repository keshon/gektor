package workflow

import (
	"github.com/keshon/gektor/internal/workflow/script"
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 2 && os.Args[1] == "workflow" && os.Args[2] == "internal-script-worker" {
		if script.Serve(os.Stdin, os.Stdout) != nil {
			os.Exit(1)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
