//go:build !windows

package command

import (
	"context"
	"os/exec"
)

func ShellName() string { return "sh" }

func Shell(ctx context.Context, command string) *exec.Cmd {
	return exec.CommandContext(ctx, "sh", "-c", command)
}
