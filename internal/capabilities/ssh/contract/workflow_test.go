package contract_test

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
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

	capnodes "github.com/keshon/gektor/internal/capabilities/nodes"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/workflow"
	"github.com/keshon/gektor/internal/workflow/service"
	"github.com/keshon/gektor/internal/workflow/store"
	"github.com/keshon/gektor/internal/workspace"
	gossh "golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func serveWorkflowSSH(t *testing.T) (ssh.Profile, *atomic.Int32) {
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

	config := &gossh.ServerConfig{PasswordCallback: func(meta gossh.ConnMetadata, password []byte) (*gossh.Permissions, error) {
		if meta.User() != "tester" || string(password) != "fixture-private-value" {
			return nil, fmt.Errorf("invalid credentials")
		}
		return nil, nil
	}}
	config.AddHostKey(signer)

	var calls atomic.Int32
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
			mu.Lock()
			active = socket
			mu.Unlock()
			// Bound a broken fixture handshake so cleanup always joins its owner.
			socket.SetDeadline(time.Now().Add(5 * time.Second))
			connection, channels, requests, handshakeErr := gossh.NewServerConn(socket, config)
			if handshakeErr != nil {
				socket.Close()
				continue
			}
			requestsDone := make(chan struct{})
			go func() { gossh.DiscardRequests(requests); close(requestsDone) }()
			for incoming := range channels {
				channel, pending, channelErr := incoming.Accept()
				if channelErr != nil {
					continue
				}
				for request := range pending {
					if request.Type != "exec" {
						request.Reply(false, nil)
						continue
					}
					calls.Add(1)
					request.Reply(true, nil)
					io.WriteString(channel, "fixture disk report")
					channel.SendRequest("exit-status", false, gossh.Marshal(struct{ Status uint32 }{0}))
					break
				}
				channel.Close()
			}
			connection.Close()
			<-requestsDone
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

	host, portText, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}

	trust := filepath.Join(t.TempDir(), "known_hosts")
	err = os.WriteFile(trust, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("GEKTOR_WORKFLOW_SSH_FIXTURE", "fixture-private-value")
	return ssh.Profile{Host: host, Port: port, User: "tester", KnownHosts: trust, PasswordEnv: "GEKTOR_WORKFLOW_SSH_FIXTURE"}, &calls
}

func TestSSHWorkflowReviewRestartsWithoutReplayingCompletedCommands(t *testing.T) {
	for _, mode := range []string{"continue", "rebind", "revoke"} {
		t.Run(mode, func(t *testing.T) {
			profile, calls := serveWorkflowSSH(t)
			ws, err := workspace.New(t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			defer ws.Close()
			profiles := ssh.Profiles{Path: filepath.Join(t.TempDir(), "ssh.json")}
			err = profiles.Save(map[string]ssh.Profile{"fixture": profile})
			if err != nil {
				t.Fatal(err)
			}
			binding, err := profiles.Inspect("fixture")
			if err != nil {
				t.Fatal(err)
			}
			storeDir := t.TempDir()
			newService := func() service.Service {
				client := ssh.New(ssh.Config{Workspace: ws, ProfilesPath: profiles.Path})
				registry, registryErr := workflow.NewRegistry(capnodes.SSHDefinitions(client, nil)...)
				if registryErr != nil {
					t.Fatal(registryErr)
				}
				return service.Service{WS: ws, StoreDir: storeDir, Registry: registry, AuthorizeResource: func(context.Context, string, string, json.RawMessage) error { return nil }}
			}
			args, err := json.Marshal(ssh.ExecRequest{Connection: "fixture", ConnectionRevision: binding.Revision, Command: "df -h"})
			if err != nil {
				t.Fatal(err)
			}
			recipe := workflow.Definition{Version: 4, Name: "SSH acceptance", Steps: []workflow.Step{
				{ID: "before", Tool: "ssh_exec", Args: args},
				{ID: "after", Tool: "ssh_exec", Args: args, DependsOn: []string{"before"}, Review: true},
			}}
			raw, err := json.Marshal(recipe)
			if err != nil {
				t.Fatal(err)
			}
			first := newService()
			out, err := first.RunSource(t.Context(), raw, "ssh.json", nil)
			if err != nil || calls.Load() != 1 || !strings.Contains(out, "waiting_review") {
				t.Fatal(out, err, calls.Load())
			}
			var summary struct {
				ID string `json:"run_id"`
			}
			err = json.Unmarshal([]byte(out), &summary)
			if err != nil {
				t.Fatal(err)
			}
			switch mode {
			case "rebind":
				profile.User = "changed"
				err = profiles.Save(map[string]ssh.Profile{"fixture": profile})
			case "revoke":
				err = profiles.Revoke("fixture")
			}
			if err != nil {
				t.Fatal(err)
			}

			restarted := newService()
			out, err = restarted.Review(t.Context(), summary.ID, "after", true)
			if (err == nil) != (mode == "continue") {
				t.Fatal(out, err)
			}
			expected := int32(1)
			if mode == "continue" {
				expected = 2
			}
			if calls.Load() != expected {
				t.Fatal("completed command replayed or invalid binding used", calls.Load())
			}
			db, err := store.OpenStore(storeDir, ws.Root())
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			state, err := db.Run(summary.ID)
			if err != nil {
				t.Fatal(err)
			}
			if state.Record.Steps[0].Status != "completed" {
				t.Fatal("lost completed effect")
			}
			checkpoint, err := json.Marshal(state.Record)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(checkpoint), "fixture-private-value") {
				t.Fatal("secret retained in checkpoint")
			}
		})
	}
}
