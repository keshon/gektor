//go:build windows

package mcp

import (
	"os/exec"
	"syscall"
)

func hideWindow(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
