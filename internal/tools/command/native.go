package command

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

func Native(ctx context.Context, program string, args []string) (*exec.Cmd, error) {
	cmd := exec.CommandContext(ctx, program, args...)
	if ext := strings.ToLower(filepath.Ext(cmd.Path)); runtime.GOOS == "windows" && (ext == ".cmd" || ext == ".bat" || ext == ".ps1") {
		return nil, fmt.Errorf("%s is a shell script, not a native executable; use run_shell or start_background with PowerShell syntax instead", program)
	}
	return cmd, nil
}
