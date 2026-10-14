// Package vad adapts the portable Sherpa Silero classifier through an owned process.
package vad

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"github.com/keshon/tars/internal/process"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Client struct {
	cancel context.CancelFunc
	input  io.WriteCloser
	output io.ReadCloser
	done   chan error
	once   sync.Once
}

func Start(parent context.Context, root string) (*Client, error) {
	ctx, cancel := context.WithCancel(parent)
	cmd := exec.CommandContext(ctx, filepath.Join(root, "assets", "runtimes", "silero", "vad-worker.exe"), filepath.Join(root, "assets", "runtimes", "sherpa", "lib"), filepath.Join(root, "assets", "models", "listening", "silero", "silero_vad.onnx"))
	var diagnostic bytes.Buffer
	cmd.Stderr = &diagnostic
	input, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	output, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		input.Close()
		return nil, err
	}
	c := &Client{cancel: cancel, input: input, output: output, done: make(chan error, 1)}
	go func() { c.done <- process.Run(cmd) }()
	ready := make(chan error, 1)
	go func() {
		var magic uint32
		err := binary.Read(output, binary.LittleEndian, &magic)
		if err == nil && magic != 0x56414431 {
			err = fmt.Errorf("invalid VAD handshake")
		}
		ready <- err
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	select {
	case err = <-ready:
	case <-timer.C:
		err = fmt.Errorf("VAD initialization timed out")
	case <-parent.Done():
		err = parent.Err()
	}
	if err != nil {
		c.Close()
		if message := strings.TrimSpace(diagnostic.String()); message != "" {
			return nil, fmt.Errorf("Silero VAD: %w: %s", err, message)
		}
		return nil, fmt.Errorf("Silero VAD: %w", err)
	}
	return c, nil
}

// Speech is serialized by the capture writer, with one 20 ms PCM16 frame per call.
func (c *Client) Speech(pcm []byte) (bool, error) {
	if len(pcm) != 640 {
		return false, fmt.Errorf("VAD requires a 20 ms frame")
	}
	if _, err := c.input.Write(pcm); err != nil {
		return false, err
	}
	var speech uint32
	if err := binary.Read(c.output, binary.LittleEndian, &speech); err != nil {
		return false, err
	}
	if speech > 1 {
		return false, fmt.Errorf("invalid VAD classification")
	}
	return speech == 1, nil
}
func (c *Client) Close() {
	c.once.Do(func() { c.cancel(); c.input.Close(); c.output.Close(); <-c.done })
}
