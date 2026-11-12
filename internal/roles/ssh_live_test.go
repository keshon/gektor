package roles

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/llm/chatcompletions"
	"github.com/keshon/gektor/internal/permission"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

const fixtureDiskUsage = "Filesystem Size Used Avail Use% Mounted on\n/dev/vda2 15G 8.3G 5.8G 59% /\n"

func TestSSHRequestLive(t *testing.T) {
	url := os.Getenv("GEKTOR_DISCOVERY_LIVE_URL")
	if url == "" {
		t.Skip("set GEKTOR_DISCOVERY_LIVE_URL to exercise SSH routing with llama.cpp")
	}

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	profile, commands := serveDiskSSH(t, home)
	err := os.MkdirAll(filepath.Join(home, ".gektor"), 0700)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]ssh.Profile{"Koara": profile})
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(filepath.Join(home, ".gektor", "ssh.json"), data, 0600)
	if err != nil {
		t.Fatal(err)
	}
	decoys := "Host vpn2\n HostName vpn2.invalid\n User decoy\nHost smv\n HostName smv.invalid\n User decoy\n"
	err = os.WriteFile(filepath.Join(home, ".ssh", "config"), []byte(decoys), 0600)
	if err != nil {
		t.Fatal(err)
	}

	e := testEnv(t)
	e.Client = chatcompletions.NewLlamaClient(url, "local")
	e.MaxSteps, e.MaxTokens, e.ContextLimit = 8, 2048, 32768
	e.WorkflowDir = filepath.Join(home, "workflows")
	// Keep wrong routes observable without allowing a model to run local commands.
	e.Policy = permission.Policy{Rules: []permission.Rule{
		{Tool: "*", Pattern: "*", Effect: permission.Deny},
		{Tool: "discover_capabilities", Pattern: "*", Effect: permission.Allow},
		{Tool: "enable_capability", Pattern: "*", Effect: permission.Allow},
		{Tool: "ssh_connections", Pattern: "*", Effect: permission.Allow},
		{Tool: "ssh_exec", Pattern: "*", Effect: permission.Allow},
	}}
	var calls []llm.ToolCall
	var failed []agent.ToolResult
	targets := map[string]string{}
	successfulExec := 0
	e.OnStep = func(_ string, _ int, msg llm.Message) {
		calls = append(calls, msg.ToolCalls...)
		for _, call := range msg.ToolCalls {
			t.Logf("call %s %s", call.Name, call.Arguments)
			targets[call.ID] = call.Name
		}
	}
	e.OnResult = func(result agent.ToolResult) {
		t.Logf("result %s %s", result.Status, result.Output)
		if targets[result.CallID] == "ssh_exec" {
			if !result.Failed {
				successfulExec++
			}
			return
		}
		if result.Failed {
			failed = append(failed, result)
		}
	}
	e.OnUsage = func(step int, usage llm.Usage) {
		t.Logf("request=%d prompt=%d cached=%d completion=%d", step+1, usage.PromptTokens, usage.CachedTokens, usage.CompletionTokens)
	}
	a, err := Interactive(e, "", filepath.Join(home, "session", "state.json"), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 180*time.Second)
	defer cancel()
	answer, err := a.Run(ctx, "can you connect to Koara ssh and check the free space on the server?")
	t.Logf("answer=%s", answer)
	if err != nil {
		t.Fatal(err)
	}
	if len(failed) != 0 {
		t.Fatal("failed tool calls", failed, "remote commands", commands())
	}
	if !strings.Contains(answer, "5.8") || !strings.Contains(answer, "59%") {
		t.Fatal("answer did not report the fixture's available space and usage", answer)
	}
	if strings.Contains(answer, "vpn2") || strings.Contains(answer, "smv") {
		t.Fatal("reported aliases outside the GEKTOR registry", answer)
	}

	executed := false
	for _, call := range calls {
		switch call.Name {
		case "discover_capabilities", "enable_capability", "ssh_connections":
		case "ssh_exec":
			var request ssh.ExecRequest
			err = json.Unmarshal(call.Arguments, &request)
			if err != nil {
				t.Fatal(err)
			}
			if request.Connection != "Koara" {
				t.Fatal("wrong registry connection", request.Connection)
			}
			executed = true
		default:
			t.Fatalf("unnecessary configuration or setup detour: %s", call.Name)
		}
	}
	if !executed || successfulExec == 0 || len(commands()) == 0 {
		t.Fatal("did not discover and execute against the registered SSH connection", commands())
	}
}

// The fixture owns its listener and transport goroutines; cleanup closes and joins
// them. Commands are syntax-checked but never executed; df responses are fixed.
func serveDiskSSH(t *testing.T, home string) (ssh.Profile, func() []string) {
	t.Helper()
	shell := "sh"
	if runtime.GOOS == "windows" {
		shell = filepath.Join(os.Getenv("ProgramFiles"), "Git", "bin", "bash.exe")
	}
	shell, err := exec.LookPath(shell)
	if err != nil {
		t.Skip("SSH fixture requires a POSIX syntax checker (Git Bash on Windows)", err)
	}
	t.Setenv("GEKTOR_SSH_LIVE_PASSWORD", "fixture-password")
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := gossh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	config := &gossh.ServerConfig{PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
		if meta.User() != "tester" || string(password) != "fixture-password" {
			return nil, fmt.Errorf("invalid fixture credentials")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex // guards active, closed and commands
	var active net.Conn
	var commands []string
	closed := false
	done := make(chan struct{})
	fixtureCtx := t.Context()
	go func() {
		defer close(done)
		for {
			socket, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			mu.Lock()
			if closed {
				mu.Unlock()
				_ = socket.Close() // Cleanup may have closed this socket already.
				return
			}
			active = socket
			mu.Unlock()
			serveDiskConnection(fixtureCtx, socket, config, shell, func(command string) {
				mu.Lock()
				commands = append(commands, command)
				mu.Unlock()
			})
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		closed = true
		if active != nil {
			_ = active.Close() // Shutdown is idempotent; request results own operation errors.
		}
		mu.Unlock()
		_ = listener.Close() // Closing the listener releases Accept.
		<-done
	})

	sshDir := filepath.Join(home, ".ssh")
	err = os.MkdirAll(sshDir, 0700)
	if err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(sshDir, "known_hosts")
	endpoint := listener.Addr()
	address := endpoint.String()
	publicKey := signer.PublicKey()
	hostLine := knownhosts.Line([]string{address}, publicKey)
	err = os.WriteFile(known, []byte(hostLine+"\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	profile := ssh.Profile{Host: host, Port: port, User: "tester", KnownHosts: known, PasswordEnv: "GEKTOR_SSH_LIVE_PASSWORD"}
	snapshot := func() []string {
		mu.Lock()
		defer mu.Unlock()
		return slices.Clone(commands)
	}
	return profile, snapshot
}

func serveDiskConnection(ctx context.Context, socket net.Conn, config *gossh.ServerConfig, shell string, record func(string)) {
	defer socket.Close() // The fixture owns accepted sockets even on handshake failure.
	connection, channels, requests, err := gossh.NewServerConn(socket, config)
	if err != nil {
		return
	}
	done := make(chan struct{})
	go func() { gossh.DiscardRequests(requests); close(done) }()
	defer func() {
		_ = connection.Close() // Release the global-request reader before joining it.
		<-done
	}()
	for incoming := range channels {
		if incoming.ChannelType() != "session" {
			err = incoming.Reject(gossh.UnknownChannelType, "session required")
			if err != nil {
				return
			}
			continue
		}
		channel, channelRequests, acceptErr := incoming.Accept()
		if acceptErr != nil {
			continue
		}
		serveDiskCommand(ctx, channel, channelRequests, shell, record)
	}
}

func serveDiskCommand(ctx context.Context, channel gossh.Channel, requests <-chan *gossh.Request, shell string, record func(string)) {
	defer channel.Close() // The connection owns transport shutdown.
	var err error
	for request := range requests {
		if request.Type != "exec" {
			err = request.Reply(false, nil)
			if err != nil {
				return
			}
			continue
		}
		var command struct{ Command string }
		err = gossh.Unmarshal(request.Payload, &command)
		if err != nil {
			err = request.Reply(false, nil)
			if err != nil {
				return
			}
			continue
		}
		record(command.Command)
		err = request.Reply(true, nil)
		if err != nil {
			return
		}
		status := uint32(0)
		// -n parses source without executing it. A minimal environment excludes
		// startup-script variables such as BASH_ENV and ENV.
		parseCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		parser := exec.CommandContext(parseCtx, shell, "-n", "-c", command.Command)
		parser.Env = []string{"PATH=" + filepath.Dir(shell), "SystemRoot=" + os.Getenv("SystemRoot")}
		diagnostic, syntaxErr := parser.CombinedOutput()
		cancel()
		fields := strings.Fields(command.Command)
		switch {
		case syntaxErr != nil:
			status = 2
			_, err = channel.Stderr().Write(diagnostic)
		case len(fields) > 0 && fields[0] == "df":
			_, err = io.WriteString(channel, fixtureDiskUsage)
		default:
			status = 64
			_, err = io.WriteString(channel.Stderr(), "unsupported fixture command: "+command.Command)
		}
		if err != nil {
			return
		}
		exitStatus := gossh.Marshal(struct{ Status uint32 }{status})
		_, err = channel.SendRequest("exit-status", false, exitStatus)
		if err != nil {
			return
		}
		return
	}
}

func TestDiskSSHFixture(t *testing.T) {
	home := t.TempDir()
	profile, _ := serveDiskSSH(t, home)
	data, err := json.Marshal(map[string]ssh.Profile{"Koara": profile})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "profiles.json")
	err = os.WriteFile(path, data, 0600)
	if err != nil {
		t.Fatal(err)
	}
	service := ssh.New(ssh.Config{ProfilesPath: path})

	for _, command := range []string{"df -h", "df -hT -x tmpfs -x overlay", "df -h; exit 99"} {
		result, execErr := service.Exec(t.Context(), ssh.ExecRequest{Connection: "Koara", Command: command})
		if execErr != nil || result.Stdout != fixtureDiskUsage {
			t.Fatal("fixture must return fixed data without executing source", command, result, execErr)
		}
	}
	result, err := service.Exec(t.Context(), ssh.ExecRequest{Connection: "Koara", Command: "df -h | grep -vE 'tmpfs'}\""})
	if err == nil || result.ExitCode != 2 || result.Stdout != "" {
		t.Fatal("malformed shell source must not produce a successful disk result", result, err)
	}
}
