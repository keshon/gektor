package tools

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/app"
	"github.com/keshon/gektor/internal/capabilities/ssh"
	"github.com/keshon/gektor/internal/credentials"
	"github.com/keshon/gektor/internal/permission"
	toolextension "github.com/keshon/gektor/internal/tools/extension"
	"github.com/keshon/gektor/internal/userdirs"
	"github.com/keshon/gektor/internal/workspace"
)

func TestCapabilityToolsAndNodesShareContractsAndResources(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	err := os.MkdirAll(userdirs.Sub(), 0700)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := json.Marshal(map[string]ssh.Profile{"test": {
		Host:        "example.com",
		User:        "tester",
		KnownHosts:  filepath.Join(home, "known_hosts"),
		PasswordEnv: "SSH_PASSWORD",
	}})
	if err != nil {
		t.Fatal(err)
	}
	err = os.WriteFile(userdirs.Sub("ssh.json"), profile, 0600)
	if err != nil {
		t.Fatal(err)
	}
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	c := &toolextension.Capabilities{Sources: WithBuiltinCapabilities(nil)}
	tool, err := app.NewWorkflow(ws, t.TempDir(), permission.Default(), nil)
	if err != nil {
		t.Fatal(err)
	}
	r, err := Compose(Dependencies{WS: ws, Vision: false, Workflow: tool}, c, nil)
	if err != nil {
		t.Fatal(err)
	}
	c.Registry = r
	toolSchemas := map[string]json.RawMessage{}
	descriptions := map[string]string{}
	for _, id := range []string{"native_documents", "native_files", "native_images", "native_media", "native_ssh", "social", "native_workflows"} {
		if _, err := c.Enable(t.Context(), id, nil); err != nil {
			t.Fatal(id, err)
		}
	}
	for _, def := range r.Defs() {
		toolSchemas[def.Name] = def.Parameters
		descriptions[def.Name] = def.Description
	}
	nodes, err := app.WorkflowRegistry(ws, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, args                      string
		readOnly, concurrent, retrySafe bool
	}{
		{"ssh_exec", `{"connection":"test","command":"uname -a"}`, false, false, false},
		{"ssh_upload", `{"connection":"test","local_path":"input.txt","remote_path":"/tmp/input.txt"}`, false, false, false},
		{"ssh_download", `{"connection":"test","local_path":"saved.txt","remote_path":"/tmp/input.txt"}`, false, false, false},
		{"docx_read", `{"path":"input.docx"}`, true, true, true},
		{"xlsx_read", `{"path":"input.xlsx"}`, true, true, true},
		{"pdf_read", `{"path":"input.pdf"}`, true, true, true},
		{"download_file", `{"url":"https://example.com/file","path":"download.bin"}`, false, false, false},
		{"archive_list", `{"path":"input.zip"}`, true, true, true},
		{"archive_create", `{"paths":["input.txt"],"path":"out.zip"}`, false, false, false},
		{"archive_extract", `{"path":"input.zip","out_dir":"out"}`, false, false, false},
		{"image_inspect", `{"paths":["input.png"]}`, true, true, true},
		{"image_preview", `{"paths":["input.png"]}`, true, true, true},
		{"image_compare", `{"paths":["a.png","b.png"]}`, true, true, true},
		{"image_variants", `{"paths":["input.png"],"widths":[8],"out_dir":"out"}`, false, false, false},
		{"media_status", `{}`, true, true, true},
		{"video_info", `{"url":"https://youtu.be/abcdefghijk"}`, true, true, true},
		{"video_transcript", `{"url":"https://youtu.be/abcdefghijk"}`, true, true, true},
		{"video_download", `{"url":"https://youtu.be/abcdefghijk","path":"video.webm"}`, false, false, false},
		{"media_inspect", `{"inputs":["input.wav"]}`, true, true, true},
		{"media_convert", `{"inputs":["input.wav"],"path":"out.flac","format":"flac"}`, false, false, false},
		{"media_clip", `{"inputs":["input.wav"],"path":"out.wav","format":"wav","duration_seconds":1}`, false, false, false},
		{"media_frame", `{"inputs":["input.mp4"],"path":"frame.png","format":"png"}`, false, false, false},
		{"media_merge", `{"inputs":["video.mp4","audio.wav"],"path":"out.mkv","format":"mkv"}`, false, false, false},
		{"social_read_post", `{"url":"https://x.com/test/status/20"}`, true, true, true},
		{"record_read", `{"kind":"notes","id":"state","allow_missing":true}`, true, true, true},
		{"record_write", `{"kind":"notes","id":"state","expected_revision":0,"data":{}}`, false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			node, err := nodes.Node(tc.name)
			if err != nil {
				t.Fatal(err)
			}
			toolName := node.Name
			if node.PermissionName != "" {
				toolName = node.PermissionName
			}
			var toolSchema, nodeSchema any
			if err := json.Unmarshal(toolSchemas[toolName], &toolSchema); err != nil {
				t.Fatal(toolName, err)
			}
			if err := json.Unmarshal(node.InputSchema, &nodeSchema); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(toolSchema, nodeSchema) {
				t.Fatal("tool and node argument contracts differ")
			}
			if descriptions[toolName] != node.Description {
				t.Fatal("tool and node descriptions differ")
			}
			if (r.ModeOf(toolName) == agent.Concurrent) != tc.concurrent || node.ReadOnly != tc.readOnly || node.RetrySafe != tc.retrySafe {
				t.Fatalf("execution policy differs: mode=%s readOnly=%v retrySafe=%v", r.ModeOf(toolName), node.ReadOnly, node.RetrySafe)
			}
			raw := json.RawMessage(tc.args)
			direct, err := (WorkspacePaths{WS: ws}).Resources(toolName, raw)
			for _, pack := range c.Native {
				for _, native := range pack.Tools {
					if native.Name() != toolName {
						continue
					}
					if scoped, ok := native.(interface {
						Resources(json.RawMessage) ([]string, error)
					}); ok {
						direct, err = scoped.Resources(raw)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			nested, err := node.Resources(raw)
			if err != nil || !reflect.DeepEqual(direct, nested) {
				t.Fatalf("resource mismatch: direct=%v node=%v error=%v", direct, nested, err)
			}
		})
	}

	for _, tc := range []struct{ operation, action string }{
		{"ssh_upload", "read_file"},
		{"ssh_download", "write_file"},
	} {
		t.Run(tc.operation+"_local_policy", func(t *testing.T) {
			policy := permission.Policy{Rules: []permission.Rule{{Tool: tc.action, Pattern: "*", Effect: permission.Deny}}}
			catalog := policyCatalog(t, Dependencies{WS: ws, Policy: policy, StoreDir: t.TempDir()})
			args := json.RawMessage(`{"connection":"test","local_path":"sensitive.env","remote_path":"/tmp/test"}`)
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			_, err := catalog.Registry.Run(ctx, tc.operation, args)
			if err == nil || !strings.Contains(err.Error(), "blocked by policy ("+tc.action) {
				t.Fatal("Agent bypassed local file policy", err)
			}

			svc, err := app.NewWorkflow(ws, t.TempDir(), policy, nil)
			if err != nil {
				t.Fatal(err)
			}
			source := []byte(`{"version":4,"name":"SSH","steps":[{"id":"transfer","tool":"` + tc.operation + `","args":` + string(args) + `}]}`)
			_, err = svc.RunSource(ctx, source, "ssh.json", nil)
			if err == nil || !strings.Contains(err.Error(), "blocked by policy ("+tc.action) {
				t.Fatal("Workflow bypassed local file policy", err)
			}
		})
	}
}

func TestSocialNodeHonorsExistingAgentPermissionName(t *testing.T) {
	ws, err := workspace.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Close()
	policy := permission.Policy{Rules: []permission.Rule{{Tool: "cap__social__read_post", Pattern: "*", Effect: permission.Deny}}}
	svc, err := app.NewWorkflow(ws, t.TempDir(), policy, nil)
	if err != nil {
		t.Fatal(err)
	}
	source := []byte(`{"version":4,"name":"Post","steps":[{"id":"post","tool":"social_read_post","args":{"url":"https://x.com/test/status/20"}}]}`)
	if _, err := svc.RunSource(t.Context(), source, "post.json", nil); err == nil || !strings.Contains(err.Error(), "blocked by policy") {
		t.Fatal("node bypassed the Agent permission identity", err)
	}
}

func TestSavedSSHCredentialsReachAgentAndWorkflowComposition(t *testing.T) {
	if !credentials.Encrypted() {
		t.Skip("requires encrypted storage")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	endpoint := listener.Addr().(*net.TCPAddr)
	err = listener.Close()
	if err != nil {
		t.Fatal(err)
	}
	known := filepath.Join(home, "known_hosts")
	err = os.WriteFile(known, nil, 0600)
	if err != nil {
		t.Fatal(err)
	}
	profiles := ssh.Profiles{Path: userdirs.Sub("ssh.json"), Credentials: credentials.Store{Directory: userdirs.Sub("credentials", "ssh")}}
	p := ssh.Profile{Host: "127.0.0.1", Port: endpoint.Port, User: "tester", KnownHosts: known}
	err = profiles.SaveWithSecret("saved", p, ssh.PasswordSecret, "saved-credential-sentinel")
	if err != nil {
		t.Fatal(err)
	}
	catalog := &toolextension.Capabilities{}
	tools, err := Compose(Dependencies{}, catalog, nil)
	if err != nil {
		t.Fatal(err)
	}
	catalog.Registry = tools
	_, err = catalog.Enable(t.Context(), "native_ssh", nil)
	if err != nil {
		t.Fatal(err)
	}
	nodes, err := app.WorkflowRegistry(nil, t.TempDir(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	node, err := nodes.Node("ssh_exec")
	if err != nil {
		t.Fatal(err)
	}
	args := json.RawMessage(`{"connection":"saved","command":"never-run","timeout_seconds":1}`)
	_, agentErr := tools.Run(t.Context(), "ssh_exec", args)
	_, nodeErr := node.Execute(t.Context(), args)
	// A refused dial proves credential configuration succeeded before networking.
	// Domain tests cover successful host-key verification and authentication.
	for _, failure := range []error{agentErr, nodeErr} {
		var dial *net.OpError
		if !errors.As(failure, &dial) || dial.Op != "dial" {
			t.Fatal("composition did not resolve saved credential", failure)
		}
	}
}
