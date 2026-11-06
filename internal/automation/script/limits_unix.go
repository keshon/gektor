//go:build !windows

package script

import (
	"golang.org/x/sys/unix"
	"os/exec"
)

func configureWorker(cmd *exec.Cmd) {}
func limitWorker() error {
	// Go reserves substantial virtual address space; cap it independently of
	// the lower soft heap target inherited through GOMEMLIMIT.
	return unix.Setrlimit(unix.RLIMIT_AS, &unix.Rlimit{Cur: 2 << 30, Max: 2 << 30})
}
