// Package ssh owns one authenticated connection per command or file transfer.
// Profiles are snapshotted at construction; no sessions survive a call.
package ssh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/keshon/gektor/internal/workspace"
	gossh "golang.org/x/crypto/ssh"
)

const MaxFileBytes int64 = 16 << 20
const MaxOutputBytes = 64 << 10

type Config struct {
	Workspace    *workspace.Workspace
	ProfilesPath string
}

// Service borrows the workspace and retains an immutable profile snapshot.
type Service struct {
	WS       *workspace.Workspace
	profiles map[string]Profile
	loadErr  error
}

func New(cfg Config) *Service {
	profiles, err := loadProfiles(cfg.ProfilesPath)
	return &Service{WS: cfg.Workspace, profiles: profiles, loadErr: err}
}

type ExecRequest struct {
	Connection     string `json:"connection"`
	Command        string `json:"command"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type ExecResult struct {
	Connection string `json:"connection"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
}

func (s *Service) profile(name string) (Profile, error) {
	if s == nil {
		return Profile{}, fmt.Errorf("SSH unavailable")
	}
	if s.loadErr != nil {
		return Profile{}, s.loadErr
	}
	profile, ok := s.profiles[name]
	if !ok {
		return Profile{}, fmt.Errorf("SSH connection %q is not registered", name)
	}
	return profile, nil
}

func timeout(seconds int) (int, error) {
	if seconds == 0 {
		seconds = 30
	}
	if seconds < 1 || seconds > 600 {
		return 0, fmt.Errorf("timeout_seconds must be 1–600")
	}
	return seconds, nil
}

func (s *Service) ValidateExec(in ExecRequest) (ExecRequest, error) {
	_, err := s.profile(in.Connection)
	if err != nil {
		return in, err
	}
	if strings.TrimSpace(in.Command) == "" || len(in.Command) > 32<<10 || strings.ContainsRune(in.Command, 0) {
		return in, fmt.Errorf("command must contain 1–32768 bytes without NUL")
	}
	in.TimeoutSeconds, err = timeout(in.TimeoutSeconds)
	return in, err
}

func (s *Service) Resource(connection, remotePath string) (string, error) {
	profile, err := s.profile(connection)
	if err != nil {
		return "", err
	}
	resource := url.URL{Scheme: "ssh", Host: profile.address(), User: url.User(profile.User), Path: remotePath}
	return resource.String(), nil
}

// connect owns the socket and cancellation watcher. The caller must call close.
func connect(ctx context.Context, profile Profile) (*gossh.Client, func(), []string, error) {
	config, secrets, err := profile.clientConfig()
	if err != nil {
		return nil, nil, nil, err
	}
	dialer := net.Dialer{}
	socket, err := dialer.DialContext(ctx, "tcp", profile.address())
	if err != nil {
		return nil, nil, nil, err
	}
	done, joined := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(joined)
		select {
		case <-ctx.Done():
			_ = socket.Close() // Cancellation interrupts handshake and active channels.
		case <-done:
		}
	}()
	closeSocket := func() {
		_ = socket.Close() // Socket shutdown is idempotent; operation errors take precedence.
		close(done)
		<-joined
	}

	connection, channels, requests, err := gossh.NewClientConn(socket, profile.address(), config)
	if err != nil {
		closeSocket()
		return nil, nil, nil, fmt.Errorf("SSH handshake: %s", redact(err.Error(), secrets))
	}
	client := gossh.NewClient(connection, channels, requests)
	cleanup := func() {
		_ = client.Close() // The completed operation determines success.
		closeSocket()
		_ = client.Wait() // Join the SSH transport before releasing the call.
	}
	return client, cleanup, secrets, nil
}

func (s *Service) Exec(ctx context.Context, in ExecRequest) (ExecResult, error) {
	result := ExecResult{Connection: in.Connection, ExitCode: -1}
	in, err := s.ValidateExec(in)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
	defer cancel()
	profile := s.profiles[in.Connection]
	client, closeConnection, secrets, err := connect(ctx, profile)
	if err != nil {
		return result, err
	}
	defer closeConnection()
	session, err := client.NewSession()
	if err != nil {
		return result, fmt.Errorf("open SSH session: %s", redact(err.Error(), secrets))
	}
	defer session.Close() // Closing a channel cannot undo a remote command.

	stdout := boundedOutput{client: client}
	stderr := boundedOutput{client: client}
	session.Stdout, session.Stderr = &stdout, &stderr
	err = session.Run(in.Command)
	result.Stdout = redact(stdout.buffer.String(), secrets)
	result.Stderr = redact(stderr.buffer.String(), secrets)
	var exit *gossh.ExitError
	switch {
	case stdout.exceeded || stderr.exceeded:
		// Truncation can split a secret before redaction can recognize it.
		result.Stdout, result.Stderr = "", ""
		return result, fmt.Errorf("SSH output exceeds 64 KiB per stream; output discarded; remote outcome may be incomplete")
	case ctx.Err() != nil:
		return result, fmt.Errorf("SSH interrupted; remote command may still be running: %w", ctx.Err())
	case errors.As(err, &exit):
		result.ExitCode = exit.ExitStatus()
		return result, fmt.Errorf("SSH command exited with status %d", result.ExitCode)
	case err != nil:
		return result, fmt.Errorf("SSH command interrupted; remote outcome unknown")
	}
	result.ExitCode = 0
	return result, nil
}

type boundedOutput struct {
	buffer   bytes.Buffer
	client   *gossh.Client
	exceeded bool
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	remaining := MaxOutputBytes - b.buffer.Len()
	if len(p) > remaining {
		_, _ = b.buffer.Write(p[:remaining]) // bytes.Buffer cannot fail.
		b.exceeded = true
		_ = b.client.Close() // Stop remote output instead of leaving a blocked copier.
		return remaining, fmt.Errorf("SSH output limit")
	}
	return b.buffer.Write(p)
}

func redact(text string, secrets []string) string {
	for _, secret := range secrets {
		encoded, _ := json.Marshal(secret) // Encoding a string cannot fail.
		text = strings.ReplaceAll(text, string(encoded[1:len(encoded)-1]), "[REDACTED]")
		text = strings.ReplaceAll(text, secret, "[REDACTED]")
	}
	return text
}

// Check authenticates and verifies the configured host key, then joins the connection.
// It opens no session or SFTP channel and does not execute a remote command.
func (s *Service) Check(ctx context.Context, name string) error {
	profile, err := s.profile(name)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, closeConnection, _, err := connect(ctx, profile)
	if ctx.Err() != nil {
		if closeConnection != nil {
			closeConnection()
		}
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	closeConnection()
	return nil
}
