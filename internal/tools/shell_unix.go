//go:build !windows

package tools

import (
	"context"
	"os/exec"
)

func ShellName() string { return "sh" }

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", command)
}
