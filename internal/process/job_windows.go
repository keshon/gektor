package process

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var createJob = kernel32.NewProc("CreateJobObjectW")
var setJob = kernel32.NewProc("SetInformationJobObject")
var assignJob = kernel32.NewProc("AssignProcessToJobObject")

// Layout from winnt.h JOBOBJECT_EXTENDED_LIMIT_INFORMATION.
// https://learn.microsoft.com/en-us/windows/win32/api/winnt/ns-winnt-jobobject_extended_limit_information
type basicLimits struct {
	ProcessTime, JobTime                 int64
	Flags                                uint32
	MinimumWorkingSet, MaximumWorkingSet uintptr
	ActiveProcesses                      uint32
	Affinity                             uintptr
	Priority, Scheduling                 uint32
}
type extendedLimits struct {
	Basic                                                      basicLimits
	IO                                                         [6]uint64
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type owner struct{ handle syscall.Handle }

func newOwner() (*owner, error) {
	h, _, err := createJob.Call(0, 0)
	if h == 0 {
		return nil, fmt.Errorf("create process job: %w", err)
	}
	o := &owner{syscall.Handle(h)}
	limits := extendedLimits{}
	limits.Basic.Flags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	ok, _, err := setJob.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		o.close()
		return nil, fmt.Errorf("configure process job: %w", err)
	}
	return o, nil
}
func (o *owner) attach(p *os.Process) error {
	h, err := syscall.OpenProcess(0x0100|0x0001, false, uint32(p.Pid)) // SET_QUOTA | TERMINATE
	if err != nil {
		return err
	}
	defer syscall.CloseHandle(h)
	ok, _, err := assignJob.Call(uintptr(o.handle), uintptr(h))
	if ok == 0 {
		return fmt.Errorf("assign process job: %w", err)
	}
	return nil
}
func (o *owner) close() { _ = syscall.CloseHandle(o.handle) }
