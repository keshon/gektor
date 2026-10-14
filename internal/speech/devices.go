package speech

import (
	"bytes"
	"context"
	"fmt"
	"github.com/keshon/tars/internal/process"
	"os/exec"
	"regexp"
	"runtime"
	"strings"
)

var microphoneLine = regexp.MustCompile(`"([^"\r\n]+)" \(audio\)`)

func parseMicrophones(output string) []string {
	devices := []string{}
	seen := map[string]bool{}
	for _, match := range microphoneLine.FindAllStringSubmatch(output, -1) {
		name := strings.TrimSpace(match[1])
		if !seen[name] {
			devices = append(devices, name)
			seen[name] = true
		}
	}
	return devices
}

// Microphones queries devices without opening an audio capture session.
func Microphones(ctx context.Context, binary string) ([]string, error) {
	if runtime.GOOS != "windows" {
		return nil, fmt.Errorf("microphone discovery currently supports Windows DirectShow")
	}
	command := exec.CommandContext(ctx, binary, "-hide_banner", "-list_devices", "true", "-f", "dshow", "-i", "dummy")
	var output bytes.Buffer
	command.Stderr = &output
	command.Stdout = &output
	err := process.Run(command)
	devices := parseMicrophones(output.String())
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if len(devices) == 0 {
		return nil, fmt.Errorf("no microphones reported by %s: %v", binary, err)
	}
	return devices, nil
}
