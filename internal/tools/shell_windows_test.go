//go:build windows

package tools

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWindowsShellPreservesQuotes(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required for the archived quoting regression")
	}
	root := t.TempDir()
	// Exercise spaces in the executable path even when Node's installation
	// happens to be in a path without spaces (as on GitHub runners).
	dir := filepath.Join(root, "with spaces")
	if err := os.Mkdir(dir, 0700); err != nil {
		t.Fatal(err)
	}
	program := filepath.Join(dir, "node.exe")
	data, err := os.ReadFile(node)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(program, data, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "batch script.cmd"), []byte("@echo off\r\necho node ok\r\nexit /b 0\r\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"pwsh.exe", "powershell.exe"} {
		executable, err := exec.LookPath(shell)
		if err != nil {
			t.Logf("%s unavailable", shell)
			continue
		}
		t.Run(shell, func(t *testing.T) {
			for _, command := range []string{
				`node -e "console.log('node ok')"`,
				`& '.\batch script.cmd'`,
				`& '` + program + `' -e "console.log('node ok')"`,
				`node -e "console.log('node ok')" > 'output file.txt'; Get-Content 'output file.txt'`,
				`node -e "console.log('node ok')" | Select-String -SimpleMatch 'node ok' | ForEach-Object { $_.Line }`,
			} {
				out, err := runProcess(t.Context(), powerShellCommand(t.Context(), executable, command), root, nil)
				if err != nil || strings.TrimSpace(out) != "node ok" {
					t.Fatalf("%s: %q %v", command, out, err)
				}
			}
		})
	}
}

func TestPowerShellSourceAndExitStatus(t *testing.T) {
	for _, name := range []string{"pwsh.exe", "powershell.exe"} {
		shell, err := exec.LookPath(name)
		if err != nil {
			continue
		}
		t.Run(name, func(t *testing.T) {
			// 5.1 turns redirected native stderr into PowerShell error records;
			// pwsh does not. Preserve shell semantics, not a text-based repair.
			redirectedCode := 0
			if name == "powershell.exe" {
				redirectedCode = 1
			}
			for _, tc := range []struct {
				name, source, want string
				code               int
			}{
				{"unicode and quotes", "$x = 'Привет «мир» a\"b C:\\путь с пробелами\\'\nWrite-Output $x", `Привет «мир» a"b C:\путь с пробелами\`, 0},
				{"large source", "#" + strings.Repeat("padding", 6000) + "\nWrite-Output 'large-source-ok'", "large-source-ok", 0},
				{"param first", "param([string]$value='default'); Write-Output $value", "default", 0},
				{"native failure", `node -e "process.stdout.write('before');process.exit(7)"`, "before", 7},
				{"expected rejection", `node -e "process.exit(7)"; if ($LASTEXITCODE -ne 7) { throw 'wrong code' }; Write-Output 'expected rejection'`, "expected rejection", 0},
				{"stderr is output", `node -e "process.stderr.write('diagnostic')"`, "diagnostic", 0},
				{"redirected stderr", `node -e "process.stderr.write('diagnostic')" 2>&1 | ForEach-Object { $_.ToString() }`, "diagnostic", redirectedCode},
				{"cmdlet failure", `Write-Error 'broken'`, "broken", 1},
				{"explicit exit", `exit 9`, "", 9},
				{"parser error", `if (`, "", 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if strings.Contains(tc.source, "node -e") {
						if _, err := exec.LookPath("node"); err != nil {
							t.Skip("node is required for native process cases")
						}
					}
					ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
					defer cancel()
					out, err := runProcess(ctx, powerShellCommand(ctx, shell, tc.source), t.TempDir(), nil)
					code := 0
					if err != nil {
						var exit *exec.ExitError
						if !errors.As(err, &exit) {
							t.Fatalf("%s: %v", out, err)
						}
						code = exit.ExitCode()
					}
					if code != tc.code || !strings.Contains(out, tc.want) {
						t.Fatalf("code=%d want=%d out=%q err=%v", code, tc.code, out, err)
					}
				})
			}
		})
	}
}

func TestProgramModeRejectsBatchScript(t *testing.T) {
	for _, name := range []string{"script.cmd", "script.bat", "script.ps1"} {
		_, err := (programInput{Program: name}).cmd(t.Context())
		if err == nil || !strings.Contains(err.Error(), "use run_shell or start_background with PowerShell syntax") {
			t.Fatal(err)
		}
	}
}
