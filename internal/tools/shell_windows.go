//go:build windows

package tools

import (
	"context"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

var windowsShell = sync.OnceValue(func() string {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		if path, err := exec.LookPath(name); err == nil {
			return path
		}
	}
	return "powershell.exe" // Start reports the missing executable; never switch syntax to CMD.
})

// ShellName describes the same process-wide selection used by shellCommand.
func ShellName() string { return filepath.Base(windowsShell()) }

func shellCommand(ctx context.Context, command string) *exec.Cmd {
	return powerShellCommand(ctx, windowsShell(), command)
}

func powerShellCommand(ctx context.Context, executable, command string) *exec.Cmd {
	// Keep argv ASCII and send source through UTF-8 stdin: user quotes and
	// variables never pass through an extra command-line parser. Append the
	// status check inside the script so its own last command determines success.
	// Keep ordinary PowerShell error semantics rather than injecting fail-fast.
	// For native argv/exit-code fidelity, callers use run_program.
	const bootstrap = `[Console]::InputEncoding=[Text.UTF8Encoding]::new();` +
		`[Console]::OutputEncoding=[Text.UTF8Encoding]::new();` +
		`$OutputEncoding=[Console]::OutputEncoding;` +
		`& ([ScriptBlock]::Create([Console]::In.ReadToEnd()+[Environment]::NewLine+` +
		`'if (-not $?) { if ($LASTEXITCODE) { exit $LASTEXITCODE }; exit 1 }'))`
	cmd := exec.CommandContext(ctx, executable, "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", bootstrap)
	cmd.Stdin = strings.NewReader(command)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	return cmd
}
