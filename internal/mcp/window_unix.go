//go:build !windows

package mcp

import "os/exec"

func hideWindow(cmd *exec.Cmd) {}
