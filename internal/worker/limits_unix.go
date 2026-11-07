//go:build !windows

package worker

import (
	"golang.org/x/sys/unix"
	"os/exec"
)

func Configure(cmd *exec.Cmd) {}
func Limit() error {
	// Go reserves substantial virtual address space; cap it independently of
	// the lower soft heap target inherited through GOMEMLIMIT.
	return unix.Setrlimit(unix.RLIMIT_AS, &unix.Rlimit{Cur: 2 << 30, Max: 2 << 30})
}
