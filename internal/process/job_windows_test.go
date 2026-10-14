package process

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// This helper owns its own job handle. Its parent kills only the helper,
// leaving Windows to terminate the job's child when the owner's handles close.
func TestJobOwnerHelper(t *testing.T) {
	if os.Args[len(os.Args)-1] != "job-owner-helper" {
		return
	}
	binary, err := os.Executable()
	if err != nil {
		os.Exit(2)
	}
	child, err := Start(context.Background(), binary, os.TempDir(), []string{"-test.run=TestProcessHelper", "--", "owned-helper"}, io.Discard)
	if err != nil {
		os.Exit(2)
	}
	fmt.Fprintln(os.Stdout, child.command.Process.Pid)
	time.Sleep(30 * time.Second)
	child.Stop()
	os.Exit(0)
}

func TestOwnerTerminationKillsOwnedChild(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=TestJobOwnerHelper", "--", "job-owner-helper")
	Hide(cmd)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()
	pidResult := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(stdout).ReadString('\n'); pidResult <- line }()
	var pid int
	select {
	case line := <-pidResult:
		pid, err = strconv.Atoi(strings.TrimSpace(line))
		if err != nil {
			t.Fatal("owner did not report a child", line)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("owner helper did not start")
	}
	handle, err := syscall.OpenProcess(0x00100000|0x0001, false, uint32(pid)) // SYNCHRONIZE | TERMINATE
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	defer syscall.TerminateProcess(handle, 1) // Only this test's retained child handle.
	if err = cmd.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	result, _, callErr := kernel32.NewProc("WaitForSingleObject").Call(uintptr(handle), 5000)
	if result != 0 {
		t.Fatalf("owned child survived launcher termination: wait=%d error=%v", result, callErr)
	}
}

func TestClosingJobTerminatesOwnedProcess(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	job, err := newOwner()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=TestProcessHelper", "--", "owned-helper")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		job.close()
		t.Fatal(err)
	}
	Hide(cmd)
	if err := cmd.Start(); err != nil {
		job.close()
		t.Fatal(err)
	}
	if err := job.attach(cmd.Process); err != nil {
		job.close()
		cmd.Process.Kill()
		cmd.Wait()
		t.Fatal(err)
	}
	ready := make(chan error, 1)
	go func() { marker := make([]byte, 5); _, err := io.ReadFull(stdout, marker); ready <- err }()
	select {
	case err := <-ready:
		if err != nil {
			job.close()
			cmd.Wait()
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		job.close()
		cmd.Wait()
		t.Fatal("helper did not start")
	}
	job.close()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		cmd.Process.Kill()
		<-done
		t.Fatal("job did not terminate child")
	}
}
