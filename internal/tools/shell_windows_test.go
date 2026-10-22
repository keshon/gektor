//go:build windows

package tools

import (
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
	for _, command := range []string{
		`node -e "console.log('node ok')"`,
		`"` + program + `" -e "console.log('node ok')"`,
		`node -e "console.log('node ok')" > "output file.txt" && type "output file.txt"`,
		`node -e "console.log('node ok')" | findstr /c:"node ok"`,
	} {
		out, err := runCommand(t.Context(), command, root, 15*time.Second, nil)
		if err != nil || strings.TrimSpace(out) != "node ok" {
			t.Fatalf("%s: %q %v", command, out, err)
		}
	}
}

func TestProgramModeRejectsBatchScript(t *testing.T) {
	_, err := (programInput{Program: "script.cmd"}).cmd(t.Context())
	if err == nil || !strings.Contains(err.Error(), "use run_shell or start_background with CMD syntax") {
		t.Fatal(err)
	}
}
