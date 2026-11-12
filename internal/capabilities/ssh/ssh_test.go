package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/workspace"
	"github.com/pkg/sftp"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

type testServer struct {
	address string
	signer  gossh.Signer
	private ed25519.PrivateKey
	calls   atomic.Int32
}

func serveSSH(t *testing.T) *testServer {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &testServer{address: listener.Addr().String(), signer: signer, private: key}
	config := &gossh.ServerConfig{
		PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
			if meta.User() != "tester" || string(password) != "test-secret" {
				return nil, fmt.Errorf("bad credentials")
			}
			return nil, nil
		},
		PublicKeyCallback: func(meta gossh.ConnMetadata, public gossh.PublicKey) (*gossh.Permissions, error) {
			if string(public.Marshal()) != string(signer.PublicKey().Marshal()) {
				return nil, fmt.Errorf("bad key")
			}
			return nil, nil
		},
	}
	config.AddHostKey(signer)
	handlers := sftp.InMemHandler()
	var mu sync.Mutex
	var active net.Conn
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			socket, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			server.calls.Add(1)
			mu.Lock()
			active = socket
			mu.Unlock()
			serveConnection(socket, config, handlers)
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		mu.Lock()
		if active != nil {
			active.Close()
		}
		mu.Unlock()
		<-done
	})
	return server
}

func serveConnection(socket net.Conn, config *gossh.ServerConfig, handlers sftp.Handlers) {
	defer socket.Close()
	connection, channels, requests, err := gossh.NewServerConn(socket, config)
	if err != nil {
		return
	}
	defer connection.Close()
	requestsDone := make(chan struct{})
	go func() { gossh.DiscardRequests(requests); close(requestsDone) }()
	defer func() { connection.Close(); <-requestsDone }()
	for incoming := range channels {
		channel, reqs, acceptErr := incoming.Accept()
		if acceptErr != nil {
			continue
		}
		serveChannel(channel, reqs, handlers)
	}
}

func serveChannel(channel gossh.Channel, requests <-chan *gossh.Request, handlers sftp.Handlers) {
	defer channel.Close()
	for request := range requests {
		switch request.Type {
		case "subsystem":
			var subsystem struct{ Name string }
			err := gossh.Unmarshal(request.Payload, &subsystem)
			if err != nil || subsystem.Name != "sftp" {
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			server := sftp.NewRequestServer(channel, handlers)
			server.Serve()
			server.Close()
			return
		case "exec":
			var command struct{ Command string }
			err := gossh.Unmarshal(request.Payload, &command)
			if err != nil {
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			status := uint32(0)
			switch command.Command {
			case "fail":
				io.WriteString(channel.Stderr(), "problem test-secret")
				status = 7
			case "flood":
				io.WriteString(channel, strings.Repeat("x", MaxOutputBytes-4)+"test-secret")
			case "flood_stderr":
				io.WriteString(channel.Stderr(), strings.Repeat("x", MaxOutputBytes-4)+"test-secret")
			case "wait":
				for range requests {
				}
				return
			default:
				io.WriteString(channel, "hello test-secret")
			}
			channel.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{status}))
			return
		default:
			request.Reply(false, nil)
		}
	}
}

func testService(t *testing.T, server *testServer) (*Service, string) {
	t.Helper()
	t.Setenv("GEKTOR_TEST_SSH_PASSWORD", "test-secret")
	dir := t.TempDir()
	known := filepath.Join(dir, "known_hosts")
	err := os.WriteFile(known, []byte(knownhosts.Line([]string{server.address}, server.signer.PublicKey())+"\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(server.address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	profiles := map[string]Profile{"test": {Host: host, Port: port, User: "tester", KnownHosts: known, PasswordEnv: "GEKTOR_TEST_SSH_PASSWORD"}}
	content, err := json.Marshal(profiles)
	if err != nil {
		t.Fatal(err)
	}
	filename := filepath.Join(dir, "ssh.json")
	err = os.WriteFile(filename, content, 0600)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return New(Config{Workspace: ws, ProfilesPath: filename}), filename
}

func TestExecFreshConnectionsExitStatusAndLimits(t *testing.T) {
	server := serveSSH(t)
	service, _ := testService(t, server)
	for _, command := range []string{"ok", "fail", "flood", "flood_stderr", "wait"} {
		t.Run(command, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()
			if command == "wait" {
				ctx, cancel = context.WithTimeout(ctx, 100*time.Millisecond)
				defer cancel()
			}
			out, err := service.Exec(ctx, ExecRequest{Connection: "test", Command: command})
			switch command {
			case "ok":
				if err != nil || out.ExitCode != 0 || out.Stdout != "hello [REDACTED]" {
					t.Fatal(out, err)
				}
			case "fail":
				if err == nil || out.ExitCode != 7 || out.Stderr != "problem [REDACTED]" {
					t.Fatal(out, err)
				}
			case "flood", "flood_stderr":
				if err == nil || out.Stdout != "" || out.Stderr != "" {
					t.Fatal("overflow exposed partial output", out, err)
				}
			case "wait":
				if !errors.Is(err, context.DeadlineExceeded) {
					t.Fatal(out, err)
				}
			}
		})
	}
	if server.calls.Load() != 5 {
		t.Fatal("connections were reused", server.calls.Load())
	}
}

func TestTransferPermissionsIntegrityAndNoClobber(t *testing.T) {
	server := serveSSH(t)
	service, _ := testService(t, server)
	err := service.WS.WriteFile("source.txt", []byte("payload"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	var actions []string
	authorize := func(ctx context.Context, action, filename string) error {
		actions = append(actions, action+":"+filename)
		return nil
	}
	in := TransferRequest{Connection: "test", LocalPath: "source.txt", RemotePath: "/remote.txt"}
	_, err = service.Transfer(t.Context(), in, Upload, func(context.Context, string, string) error { return fmt.Errorf("denied") })
	if err == nil || server.calls.Load() != 0 {
		t.Fatal("denial connected", err)
	}
	uploaded, err := service.Transfer(t.Context(), in, Upload, authorize)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Transfer(t.Context(), in, Upload, authorize)
	if err == nil {
		t.Fatal("upload replaced existing destination")
	}
	in.LocalPath = "saved.txt"
	downloaded, err := service.Transfer(t.Context(), in, Download, authorize)
	if err != nil {
		t.Fatal(err)
	}
	if downloaded.Size != 7 || downloaded.SHA256 != uploaded.SHA256 {
		t.Fatal(uploaded, downloaded)
	}
	_, err = service.Transfer(t.Context(), in, Download, authorize)
	if err == nil {
		t.Fatal("download replaced existing destination")
	}
	content, err := service.WS.ReadFile("saved.txt")
	if err != nil || string(content) != "payload" {
		t.Fatal(string(content), err)
	}
	in.LocalPath, in.MaxBytes = "too-small.txt", 3
	_, err = service.Transfer(t.Context(), in, Download, authorize)
	if err == nil {
		t.Fatal("download exceeded max_bytes")
	}
	_, err = service.WS.Stat(in.LocalPath)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatal("partial destination exists", err)
	}
	if actions[0] != "read_file:source.txt" || actions[2] != "write_file:saved.txt" {
		t.Fatal(actions)
	}
	files, err := service.WS.ReadDir(".")
	if err != nil || len(files) != 2 {
		t.Fatal("temporary files leaked", files, err)
	}
}

func TestHostKeyRejectionProfileSnapshotAndKeyAuth(t *testing.T) {
	server := serveSSH(t)
	service, filename := testService(t, server)
	err := os.WriteFile(filename, []byte(`{}`), 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Exec(t.Context(), ExecRequest{Connection: "test", Command: "ok"})
	if err != nil {
		t.Fatal("profile was not snapshotted", err)
	}

	profile := service.profiles["test"]
	block, err := gossh.MarshalPrivateKey(server.private, "test")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_ed25519")
	err = os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600)
	if err != nil {
		t.Fatal(err)
	}
	profile.PasswordEnv, profile.PrivateKey = "", keyPath
	_, _, err = profile.clientConfig()
	if err != nil {
		t.Fatal("private key parsing", err)
	}
	service.profiles["test"] = profile
	_, err = service.Exec(t.Context(), ExecRequest{Connection: "test", Command: "ok"})
	if err != nil {
		t.Fatal("private key authentication", err)
	}

	t.Setenv("GEKTOR_TEST_SSH_PASSPHRASE", "passphrase")
	block, err = gossh.MarshalPrivateKeyWithPassphrase(server.private, "test", []byte("passphrase"))
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(keyPath, pem.EncodeToMemory(block), 0600)
	if err != nil {
		t.Fatal(err)
	}
	profile.PassphraseEnv = "GEKTOR_TEST_SSH_PASSPHRASE"
	service.profiles["test"] = profile
	_, err = service.Exec(t.Context(), ExecRequest{Connection: "test", Command: "ok"})
	if err != nil {
		t.Fatal("encrypted key authentication", err)
	}

	err = os.WriteFile(profile.KnownHosts, nil, 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Exec(t.Context(), ExecRequest{Connection: "test", Command: "ok"})
	if err == nil || !strings.Contains(err.Error(), "knownhosts") {
		t.Fatal("unknown host key accepted", err)
	}

	_, changed, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	changedSigner, err := gossh.NewSignerFromKey(changed)
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(profile.KnownHosts, []byte(knownhosts.Line([]string{server.address}, changedSigner.PublicKey())+"\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	_, err = service.Exec(t.Context(), ExecRequest{Connection: "test", Command: "ok"})
	if err == nil || !strings.Contains(err.Error(), "key mismatch") {
		t.Fatal("changed host key accepted", err)
	}
}

func TestCancellationInterruptsHandshake(t *testing.T) {
	server := serveSSH(t)
	service, _ := testService(t, server)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		socket, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer socket.Close()
		// A broken client must fail the assertion instead of hanging the suite.
		socket.SetDeadline(time.Now().Add(2 * time.Second))
		io.Copy(io.Discard, socket)
	}()
	t.Cleanup(func() { listener.Close(); <-done })
	address := listener.Addr().(*net.TCPAddr)
	profile := service.profiles["test"]
	profile.Host, profile.Port = address.IP.String(), address.Port
	service.profiles["test"] = profile

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	started := time.Now()
	_, err = service.Exec(ctx, ExecRequest{Connection: "test", Command: "ok"})
	if err == nil || ctx.Err() == nil || time.Since(started) > time.Second {
		t.Fatal("handshake did not honor cancellation", err)
	}
}
