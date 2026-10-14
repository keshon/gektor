package process

import (
	"os/exec"
	"syscall"
)

func Hide(cmd *exec.Cmd) { cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true} }
