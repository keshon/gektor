package mcpmanager

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/permission"
	"github.com/keshon/gektor/internal/tools/extension"
)

func bindingReply(method string) json.RawMessage {
	switch method {
	case "initialize":
		return json.RawMessage(`{"serverInfo":{"name":"fixture","version":"1"}}`)
	case "tools/list":
		return json.RawMessage(`{"tools":[{"name":"probe","description":"fixture","inputSchema":{"type":"object"}}]}`)
	case "tools/call":
		return json.RawMessage(`{"content":[{"type":"text","text":"done"}]}`)
	}
	return json.RawMessage(`{}`)
}

func TestMCPBindingProcess(t *testing.T) {
	marker := slices.Index(os.Args, "--binding-fixture")
	if marker < 0 {
		return
	}
	decoder := json.NewDecoder(os.Stdin)
	encoder := json.NewEncoder(os.Stdout)
	for {
		var request struct {
			ID     int    `json:"id"`
			Method string `json:"method"`
		}
		err := decoder.Decode(&request)
		if err == io.EOF {
			os.Exit(0)
		}
		if err != nil {
			os.Exit(2)
		}
		if request.ID == 0 {
			continue
		}
		if request.Method == "tools/call" {
			file, openErr := os.OpenFile(os.Args[marker+1], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
			if openErr != nil {
				os.Exit(3)
			}
			_, err = file.WriteString("call\n")
			closeErr := file.Close()
			if err != nil || closeErr != nil {
				os.Exit(4)
			}
		}
		err = encoder.Encode(struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      int             `json:"id"`
			Result  json.RawMessage `json:"result"`
		}{"2.0", request.ID, bindingReply(request.Method)})
		if err != nil {
			os.Exit(5)
		}
	}
}

func TestSavedMCPBindingsBlockRetainedToolsAcrossManagers(t *testing.T) {
	for _, transport := range []string{"http", "stdio"} {
		for _, change := range []string{"rebind", "disable", "delete", "revoke"} {
			t.Run(transport+"/"+change, func(t *testing.T) {
				root := t.TempDir()
				dir := filepath.Join(root, "mcp")
				manager, err := New(dir, root, filepath.Join(root, "credentials"), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer manager.Close()

				var calls atomic.Int32
				logPath := filepath.Join(root, "calls")
				server := Server{ID: "fixture", Description: "fixture", Enabled: true}
				if transport == "http" {
					endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
						if r.Method == http.MethodDelete {
							w.WriteHeader(204)
							return
						}
						var request struct {
							ID     int    `json:"id"`
							Method string `json:"method"`
						}
						decodeErr := json.NewDecoder(r.Body).Decode(&request)
						if decodeErr != nil {
							t.Error(decodeErr)
							w.WriteHeader(400)
							return
						}
						if request.ID == 0 {
							w.WriteHeader(202)
							return
						}
						if request.Method == "tools/call" {
							calls.Add(1)
						}
						w.Header().Set("Mcp-Session-Id", "fixture")
						encodeErr := json.NewEncoder(w).Encode(struct {
							JSONRPC string          `json:"jsonrpc"`
							ID      int             `json:"id"`
							Result  json.RawMessage `json:"result"`
						}{"2.0", request.ID, bindingReply(request.Method)})
						if encodeErr != nil {
							t.Error(encodeErr)
						}
					}))
					defer endpoint.Close()
					server.URL = endpoint.URL
				} else {
					server.Command = []string{os.Args[0], "-test.run=^TestMCPBindingProcess$", "--", "--binding-fixture", logPath}
				}

				err = manager.Save(server)
				if err != nil {
					t.Fatal(err)
				}
				before, err := manager.Inspect("fixture")
				if err != nil || len(before.Revision) != 64 {
					t.Fatal(before, err)
				}

				catalog := &extension.Capabilities{Registry: agent.NewRegistry(), Sources: manager.Sources()}
				_, err = catalog.Enable(t.Context(), "fixture", nil)
				if err != nil {
					t.Fatal(err)
				}
				name := "mcp__fixture__probe"
				args := json.RawMessage(`{}`)
				_, err = catalog.Registry.Run(t.Context(), name, args)
				if err != nil {
					t.Fatal(err)
				}

				source := manager.Sources()[0]
				target := source.Binding.Target()
				var approvals permission.Approvals
				approvals.Remember(name, target)

				operator, err := New(dir, root, filepath.Join(root, "credentials"), nil)
				if err != nil {
					t.Fatal(err)
				}
				defer operator.Close()
				switch change {
				case "rebind":
					if transport == "http" {
						server.URL += "/changed"
					} else {
						server.Command = append(server.Command, "changed")
					}
					err = operator.Save(server)
				case "disable":
					server.Enabled = false
					err = operator.Save(server)
				case "delete":
					err = operator.Delete("fixture")
				case "revoke":
					err = operator.Revoke("fixture")
				}
				if err != nil {
					t.Fatal(err)
				}
				_, err = catalog.Registry.Run(t.Context(), name, args)
				if err == nil {
					t.Fatal("retained tool used stale binding")
				}
				if transport == "http" && calls.Load() != 1 {
					t.Fatal("blocked call reached HTTP server", calls.Load())
				}
				if transport == "stdio" {
					raw, readErr := os.ReadFile(logPath)
					if readErr != nil || string(raw) != "call\n" {
						t.Fatal("blocked call reached child", string(raw), readErr)
					}
				}
				err = source.Binding.Check()
				if err == nil {
					t.Fatal("binding review accepted stale metadata")
				}
				if change == "rebind" && approvals.Allows(name, operator.Sources()[0].Binding.Target()) {
					t.Fatal("approval survived binding change")
				}
				if change == "revoke" {
					err = operator.Delete("fixture")
					if err != nil {
						t.Fatal(err)
					}
					err = operator.Save(server)
					if err != nil {
						t.Fatal(err)
					}
					after, inspectErr := operator.Inspect("fixture")
					if inspectErr != nil || !after.Revoked || after.Revision != before.Revision {
						t.Fatal(after, inspectErr)
					}
					_, err = operator.Connect(t.Context(), "fixture")
					if err == nil || !strings.Contains(err.Error(), "revoked") {
						t.Fatal("recreated name bypassed revocation", err)
					}
				}
			})
		}
	}
}

func TestBindingInspectionDoesNotResolveSecretsOrExposeReferences(t *testing.T) {
	root := t.TempDir()
	manager, err := New(filepath.Join(root, "mcp"), root, filepath.Join(root, "credentials"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	server := Server{ID: "fixture", Description: "fixture", Enabled: true, Command: []string{"missing-command", "private-argument"}, EnvRefs: map[string]string{"TOKEN": "MISSING_PRIVATE_SOURCE"}}
	err = manager.Save(server)
	if err != nil {
		t.Fatal(err)
	}
	binding, err := manager.Inspect("fixture")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(binding)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{"MISSING_PRIVATE_SOURCE", "private-argument"} {
		if strings.Contains(string(raw), private) {
			t.Fatal("inspection exposed credential or command arguments")
		}
	}
	// Operator metadata edits keep the same pin; transport changes do not.
	server.OnLoad = true
	err = manager.Save(server)
	if err != nil {
		t.Fatal(err)
	}
	after, err := manager.Inspect("fixture")
	if err != nil || binding.Revision != after.Revision {
		t.Fatal(after, err)
	}
}

var _ extension.MCPBinding = savedBinding{}
