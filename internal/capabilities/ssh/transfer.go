package ssh

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/pkg/sftp"
)

type TransferRequest struct {
	Connection     string `json:"connection"`
	LocalPath      string `json:"local_path"`
	RemotePath     string `json:"remote_path"`
	MaxBytes       int64  `json:"max_bytes,omitempty"`
	TimeoutSeconds int    `json:"timeout_seconds,omitempty"`
}

type TransferResult struct {
	Connection string `json:"connection"`
	LocalPath  string `json:"local_path"`
	RemotePath string `json:"remote_path"`
	Size       int64  `json:"size"`
	SHA256     string `json:"sha256"`
}

// FileAuthorizer checks the local read_file or write_file action before connecting.
type FileAuthorizer func(context.Context, string, string) error

type Direction uint8

const (
	Upload Direction = iota
	Download
)

func (s *Service) ValidateTransfer(in TransferRequest) (TransferRequest, error) {
	_, err := s.profile(in.Connection)
	if err != nil {
		return in, err
	}
	if s.WS == nil || in.LocalPath == "" {
		return in, fmt.Errorf("workspace and local_path are required")
	}
	in.LocalPath, err = s.WS.Relative(in.LocalPath)
	if err != nil {
		return in, err
	}
	in.LocalPath = filepath.ToSlash(in.LocalPath)
	if !path.IsAbs(in.RemotePath) || strings.ContainsAny(in.RemotePath, "\\\x00\r\n") || len(in.RemotePath) > 4096 {
		return in, fmt.Errorf("remote_path must be an absolute POSIX path without backslashes or control lines")
	}
	in.RemotePath = path.Clean(in.RemotePath)
	if in.RemotePath == "/" || in.LocalPath == "." {
		return in, fmt.Errorf("transfer requires file paths")
	}
	if in.MaxBytes == 0 {
		in.MaxBytes = MaxFileBytes
	}
	if in.MaxBytes < 1 || in.MaxBytes > MaxFileBytes {
		return in, fmt.Errorf("max_bytes must be 1–%d", MaxFileBytes)
	}
	in.TimeoutSeconds, err = timeout(in.TimeoutSeconds)
	return in, err
}

func (s *Service) Transfer(ctx context.Context, in TransferRequest, direction Direction, authorize FileAuthorizer) (TransferResult, error) {
	in, err := s.ValidateTransfer(in)
	result := TransferResult{Connection: in.Connection, LocalPath: in.LocalPath, RemotePath: in.RemotePath}
	if err != nil {
		return result, err
	}
	if direction != Upload && direction != Download {
		return result, fmt.Errorf("invalid SSH transfer direction")
	}
	if authorize == nil {
		return result, fmt.Errorf("SSH local file authorization unavailable")
	}
	action := "write_file"
	if direction == Upload {
		action = "read_file"
	}
	err = authorize(ctx, action, in.LocalPath)
	if err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(in.TimeoutSeconds)*time.Second)
	defer cancel()
	client, closeConnection, secrets, err := connect(ctx, s.profiles[in.Connection], s.credentials)
	if err != nil {
		return result, err
	}
	defer closeConnection()
	remote, err := sftp.NewClient(client)
	if err != nil {
		return result, fmt.Errorf("open SFTP: %s", redact(err.Error(), secrets))
	}
	defer remote.Close() // Connection shutdown joins all SFTP requests.

	var size int64
	var digest string
	if direction == Upload {
		size, digest, err = s.upload(ctx, remote, in)
	} else {
		size, digest, err = s.download(ctx, remote, in)
	}
	result.Size, result.SHA256 = size, digest
	if ctx.Err() != nil {
		return result, fmt.Errorf("SSH transfer interrupted; verify remote state before retrying: %w", ctx.Err())
	}
	if err != nil {
		return result, fmt.Errorf("SSH transfer: %s", redact(err.Error(), secrets))
	}
	return result, nil
}

func (s *Service) upload(ctx context.Context, remote *sftp.Client, in TransferRequest) (int64, string, error) {
	source, err := s.WS.Open(in.LocalPath)
	if err != nil {
		return 0, "", err
	}
	defer source.Close() // Read-only source; transfer errors take precedence.
	stat, err := source.Stat()
	if err != nil {
		return 0, "", err
	}
	if !stat.Mode().IsRegular() || stat.Size() > in.MaxBytes {
		return 0, "", fmt.Errorf("upload source must be a regular file within max_bytes")
	}
	_, err = remote.Lstat(in.RemotePath)
	if err == nil {
		return 0, "", fmt.Errorf("remote destination already exists")
	}
	if !errors.Is(err, os.ErrNotExist) {
		return 0, "", err
	}

	temporary := path.Join(path.Dir(in.RemotePath), ".gektor-"+rand.Text())
	destination, err := remote.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return 0, "", err
	}
	complete := false
	defer func() {
		_ = destination.Close() // Explicit close below reports write errors.
		if !complete {
			_ = remote.Remove(temporary) // Best effort; a broken connection may leave a temporary remote file.
		}
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(source, in.MaxBytes+1))
	if err != nil {
		return 0, "", err
	}
	if n > in.MaxBytes {
		return 0, "", fmt.Errorf("upload exceeds max_bytes")
	}
	err = destination.Close()
	if err != nil {
		return 0, "", err
	}
	if ctx.Err() != nil {
		return 0, "", ctx.Err()
	}
	// The hardlink extension atomically refuses existing names, even on servers
	// whose ordinary Rename overwrites. Unsupported servers fail without fallback.
	err = remote.Link(temporary, in.RemotePath)
	if err != nil {
		return 0, "", fmt.Errorf("publish upload (requires hardlink@openssh.com): %w", err)
	}
	err = remote.Remove(temporary)
	if err != nil {
		return 0, "", fmt.Errorf("upload published but temporary remote file cleanup failed: %w", err)
	}
	complete = true
	return n, fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func (s *Service) download(ctx context.Context, remote *sftp.Client, in TransferRequest) (int64, string, error) {
	source, err := remote.Open(in.RemotePath)
	if err != nil {
		return 0, "", err
	}
	defer source.Close() // Read-only source; transfer errors take precedence.
	stat, err := source.Stat()
	if err != nil {
		return 0, "", err
	}
	if !stat.Mode().IsRegular() || stat.Size() > in.MaxBytes {
		return 0, "", fmt.Errorf("download source must be a regular file within max_bytes")
	}
	root, err := s.WS.OpenRoot(filepath.Dir(in.LocalPath))
	if err != nil {
		return 0, "", err
	}
	defer root.Close() // Root lifetime ends after publication and cleanup.
	temporary := ".gektor-" + rand.Text()
	destination, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return 0, "", err
	}
	defer func() {
		_ = destination.Close()    // Explicit close below reports write errors.
		_ = root.Remove(temporary) // Only our random temporary name is removed.
	}()
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(destination, hash), io.LimitReader(source, in.MaxBytes+1))
	if err != nil {
		return 0, "", err
	}
	if n > in.MaxBytes {
		return 0, "", fmt.Errorf("download exceeds max_bytes")
	}
	err = destination.Sync()
	if err != nil {
		return 0, "", err
	}
	err = destination.Close()
	if err != nil {
		return 0, "", err
	}
	if ctx.Err() != nil {
		return 0, "", ctx.Err()
	}
	// Link publishes complete content atomically and refuses an existing name.
	err = root.Link(temporary, filepath.Base(in.LocalPath))
	if err != nil {
		return 0, "", err
	}
	return n, fmt.Sprintf("%x", hash.Sum(nil)), nil
}
