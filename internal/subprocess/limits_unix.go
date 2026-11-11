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
		var err error
		limit, err = linuxAddressSpaceLimit()
		if err != nil {
			return err
		}
	}

	bounds := unix.Rlimit{Cur: limit, Max: limit}
	return unix.Setrlimit(unix.RLIMIT_AS, &bounds)
}

// linuxAddressSpaceLimit allows existing runtime mappings plus 512 MiB.
// The race runtime reserves a large shadow mapping before worker startup;
// a fixed cap below it would reject every additional allocation.
// GOMEMLIMIT remains the independent soft heap target.
func linuxAddressSpaceLimit() (uint64, error) {
	file, err := os.Open("/proc/self/statm")
	if err != nil {
		return 0, fmt.Errorf("read worker address space: %w", err)
	}
	// Closing the read-only proc descriptor cannot change the measured limit.
	defer file.Close()

	reader := io.LimitReader(file, 256)
	data, err := io.ReadAll(reader)
	if err != nil {
		return 0, fmt.Errorf("read worker address space: %w", err)
	}

	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("worker address space is unavailable")
	}

	pages, err := strconv.ParseUint(fields[0], 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid worker address space size")
	}

	pageSize := uint64(os.Getpagesize())
	const allowance = 512 << 20
	if pages > (^uint64(0)-allowance)/pageSize {
		return 0, fmt.Errorf("invalid worker address space size")
	}

	return pages*pageSize + allowance, nil
}
