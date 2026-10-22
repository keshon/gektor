//go:build windows

package tools

import (
	"context"
	"os/exec"
	"syscall"
)

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "cmd.exe")
	// cmd.exe does not use CommandLineToArgvW quoting. In particular, Go's
	// automatic backslash-escaping corrupts node -e and quoted executable paths.
	// /S strips exactly the outer pair; everything inside is CMD source text.
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CmdLine:    `cmd.exe /D /S /C "` + command + `"`,
		HideWindow: true,
	}
	return cmd
}
