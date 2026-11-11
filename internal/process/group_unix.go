//go:build !windows

package process

import (
	"os/exec"
	"syscall"
)

// Group puts a command in its own process group so the
// whole subtree (sh -> npm -> node) can be signalled at once. Killing only
// the direct child leaves the real server running as an orphan, and that
// orphan holding the output pipe is what makes cmd.Wait block.
type Group struct{}

// NewGroup creates a process-tree owner. Call BeforeStart before starting cmd.
func NewGroup() *Group { return &Group{} }

// BeforeStart configures cmd to start in a new process group.
func (g *Group) BeforeStart(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// AfterStart completes setup; Unix groups require no work after cmd.Start.
func (g *Group) AfterStart(cmd *exec.Cmd) error { return nil }

// Kill sends SIGKILL to the child's process group. It is a no-op before Start.
func (g *Group) Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

// Release is a no-op; Unix process groups hold no parent-owned handle.
func (g *Group) Release() {}
