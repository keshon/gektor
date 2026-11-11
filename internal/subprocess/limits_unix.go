//go:build !windows

package subprocess

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func configure(cmd *exec.Cmd) {}

// Limit applies the platform memory limit to the calling worker process.
// Call it once at worker startup, before reading or processing requests.
func Limit() error {
	limit := uint64(2 << 30)
	if runtime.GOOS == "linux" {
		// The race runtime reserves a large shadow mapping before Serve runs.
		// Bound additional mappings instead of setting a cap below that existing
		// reservation. GOMEMLIMIT remains the independent soft heap target.
		f, err := os.Open("/proc/self/statm")
		if err != nil {
			return fmt.Errorf("read worker address space: %w", err)
		}
		data, err := io.ReadAll(io.LimitReader(f, 256))
		_ = f.Close()
		if err != nil {
			return fmt.Errorf("read worker address space: %w", err)
		}
		fields := strings.Fields(string(data))
		if len(fields) == 0 {
			return fmt.Errorf("worker address space is unavailable")
		}
		pages, err := strconv.ParseUint(fields[0], 10, 64)
		pageSize := uint64(os.Getpagesize())
		const allowance = 512 << 20
		if err != nil || pages > (^uint64(0)-allowance)/pageSize {
			return fmt.Errorf("invalid worker address space size")
		}
		limit = pages*pageSize + allowance
	}
	return unix.Setrlimit(unix.RLIMIT_AS, &unix.Rlimit{Cur: limit, Max: limit})
}
