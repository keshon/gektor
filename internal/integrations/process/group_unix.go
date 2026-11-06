//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

// Group puts a background command in its own process group so the
// whole subtree (sh -> npm -> node) can be signalled at once. Killing only
// the direct child leaves the real server running as an orphan, and that
// orphan holding the output pipe is what makes cmd.Wait block.
type Group struct{}

func NewGroup() *Group { return &Group{} }

func (g *Group) BeforeStart(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func (g *Group) AfterStart(cmd *exec.Cmd) error { return nil }

func (g *Group) Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func (g *Group) Release() {}
