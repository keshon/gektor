//go:build !windows

package process

import "os/exec"

func Hide(cmd *exec.Cmd) {}
