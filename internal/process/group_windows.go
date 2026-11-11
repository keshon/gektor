//go:build windows

package process

import (
	"context"
	"fmt"
	"os/exec"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Group ties a command's descendant tree to a
// Windows job object, the analogue of setpgid on Unix.
//
// taskkill /T alone is not enough. It walks the tree from a PID, so once
// the direct child exits — a launcher shell that spawns the real server
// and returns — there is no PID left to walk from and every descendant
// survives, holding its port and its files. A job object owns processes
// regardless of who their parent is or whether that parent is still
// alive, and terminating the job takes all of them.
type Group struct {
	job windows.Handle
}

// NewGroup allocates a process-tree owner. Call Release after the command ends.
// If job creation fails, Kill still attempts a taskkill tree sweep.
func NewGroup() *Group {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		// Retain taskkill fallback when job ownership is unavailable.
		return &Group{}
	}
	// KILL_ON_JOB_CLOSE means the tree also dies if this process exits
	// without a clean shutdown, so a crashed agent does not leave a
	// server running.
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
		BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
			LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
		},
	}
	if _, err := windows.SetInformationJobObject(job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return &Group{}
	}
	return &Group{job: job}
}

// BeforeStart runs while cmd is still being configured.
func (g *Group) BeforeStart(cmd *exec.Cmd) {}

// AfterStart adopts the freshly started process into the job. There is a
// short window between Start and this call in which the child could
// spawn a grandchild that escapes the job; kill keeps the taskkill sweep
// as a second pass for exactly that case.
func (g *Group) AfterStart(cmd *exec.Cmd) error {
	if g.job == 0 || cmd.Process == nil {
		return nil
	}
	h, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		return fmt.Errorf("open process for job assignment: %w", err)
	}
	defer windows.CloseHandle(h)
	return windows.AssignProcessToJobObject(g.job, h)
}

// Kill terminates the owned job and attempts a PID-based tree sweep.
// It returns the job termination error; taskkill failures are best-effort.
func (g *Group) Kill(cmd *exec.Cmd) error {
	var jobErr error
	if g.job != 0 {
		jobErr = windows.TerminateJobObject(g.job, 1)
	}
	// Second pass for anything that started before the job assignment.
	// Its exit status is deliberately ignored: taskkill reports failure
	// when the process is already gone, which is the expected case here.
	if cmd.Process != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		kill := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", fmt.Sprint(cmd.Process.Pid))
		hideWindow(kill)
		_ = kill.Run()
	}
	return jobErr
}

// Release closes the job handle, terminating remaining owned descendants.
// Repeated calls have no effect.
func (g *Group) Release() {
	if g.job != 0 {
		windows.CloseHandle(g.job)
		g.job = 0
	}
}
