package subprocess

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// Limit applies the platform memory limit to the calling worker process.
// Call it once at worker startup, before reading or processing requests.
// On success the worker retains an open kernel job handle until process exit;
// the OS releases it then. Failed setup closes the handle immediately.
func Limit() error {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return err
	}

	limits := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	limits.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_PROCESS_MEMORY
	limits.ProcessMemoryLimit = 256 << 20
	size := uint32(unsafe.Sizeof(limits))
	// Keep the unsafe conversion in the syscall argument, preserving pointer lifetime.
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), size)
	if err != nil {
		// Preserve the setup failure if best-effort handle cleanup also fails.
		_ = windows.CloseHandle(job)
		return err
	}

	process := windows.CurrentProcess()
	err = windows.AssignProcessToJobObject(job, process)
	if err != nil {
		// Preserve the assignment failure if best-effort handle cleanup also fails.
		_ = windows.CloseHandle(job)
		return fmt.Errorf("worker memory limit: %w", err)
	}

	// Windows handles have no Go finalizer. Keeping this handle open needs no
	// package-level reference; the job stays alive until process exit.
	return nil
}
