package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

type deadlineCaller struct{ remaining time.Duration }

type deadlineTransport struct{ remaining time.Duration }

func (t *deadlineTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.Body == nil {
		return &http.Response{StatusCode: 204, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}, nil
	}
	deadline, _ := req.Context().Deadline()
	t.remaining = time.Until(deadline)
	var call rpcRequest
	json.NewDecoder(req.Body).Decode(&call)
	return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"result":{"content":[{"type":"text","text":"ok"}]}}`, call.ID)))}, nil
}

func TestHTTPMCPDoesNotShortenConfiguredDeadline(t *testing.T) {
	fake := &fakeHTTPMCP{t: t, mode: "json"}
	server := httptest.NewServer(fake.handler())
	defer server.Close()
	tools, client, err := StartMCPHTTP(t.Context(), "reader", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	transport := &deadlineTransport{}
	client.http.Transport = transport
	tool := tools[0].(*MCPTool)
	tool.timeout = 120 * time.Second
	if _, err := tool.Run(t.Context(), json.RawMessage(`{}`)); err != nil || transport.remaining < 110*time.Second {
		t.Fatalf("HTTP deadline=%v err=%v", transport.remaining, err)
	}
}

func (c *deadlineCaller) call(ctx context.Context, _ string, _ any, out any) error {
	deadline, _ := ctx.Deadline()
	c.remaining = time.Until(deadline)
	return json.Unmarshal([]byte(`{"content":[{"type":"text","text":"ok"}]}`), out)
}

func TestMCPConfiguredTimeoutAndParentDeadline(t *testing.T) {
	c := &deadlineCaller{}
	tool := &MCPTool{client: c, timeout: 120 * time.Second}
	if _, err := tool.Run(t.Context(), json.RawMessage(`{}`)); err != nil || c.remaining < 110*time.Second {
		t.Fatalf("deadline=%v err=%v", c.remaining, err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	if _, err := tool.Run(ctx, json.RawMessage(`{}`)); err != nil || c.remaining > time.Second {
		t.Fatalf("deadline=%v err=%v", c.remaining, err)
	}
}

func TestPerToolPolicySurvivesResumeAndChecksAllResources(t *testing.T) {
	read, concurrent := true, true
	config := CapabilityConfig{Packs: []CapabilityPack{{ID: "mixed", Version: "1", Type: "executable", Command: []string{"unused"}, Tools: []ExecutableToolDef{
		{Name: "read", Description: "read", InputSchema: json.RawMessage(`{"type":"object"}`)},
		{Name: "publish", Description: "publish", InputSchema: json.RawMessage(`{"type":"object"}`)},
	}, ToolPolicies: map[string]ToolPolicy{"cap__mixed__read": {ReadOnly: &read, Concurrent: &concurrent, ResourceFields: []string{"from", "to"}}}}}}
	sources, close := BuildCapabilities(config, t.TempDir())
	defer close()
	cat := &Capabilities{Sources: sources, Registry: agent.NewRegistry()}
	if _, err := cat.Enable(t.Context(), "mixed", nil); err != nil {
		t.Fatal(err)
	}
	state := cat.Save()
	resumed := &Capabilities{Sources: sources, Registry: agent.NewRegistry()}
	if err := resumed.Restore(t.Context(), state); err != nil {
		t.Fatal(err)
	}
	if resumed.Registry.ModeOf("cap__mixed__read") != agent.Concurrent || resumed.Registry.ModeOf("cap__mixed__publish") != agent.Exclusive {
		t.Fatal("policy lost")
	}
	tool := &capabilityTool{source: sources[0], def: state[0].Tools[1]}
	for _, def := range state[0].Tools {
		if def.Name == "cap__mixed__read" {
			tool.def = def
		}
	}
	resources, err := tool.Resources(json.RawMessage(`{"from":"a","to":"b"}`))
	if err != nil || len(resources) != 2 || !tool.ReadOnly() {
		t.Fatal(resources, err)
	}
	if _, err := tool.Resources(json.RawMessage(`{"from":"a"}`)); err == nil {
		t.Fatal("missing resource accepted")
	}
}

func TestExecutableCredentialsAreScopedAndRedacted(t *testing.T) {
	t.Setenv("TEST_READER_SECRET", "test-private-credential")
	t.Setenv("UNRELATED_API_KEY", "must-not-reach-adapter")
	tool := &ExecutableTool{Dir: t.TempDir(), Pack: CapabilityPack{ID: "reader", Command: []string{os.Args[0], "-test.run=^TestExecutableProtocolHelper$", "--", "--gektor-executable-helper", "credentials"}, EnvRefs: map[string]string{"READER_TOKEN": "TEST_READER_SECRET"}}, Def: ExecutableToolDef{Name: "read"}}
	out, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, "[redacted]") || strings.Contains(out, "must-not-reach") || strings.Contains(out, "test-private") {
		t.Fatal(out, err)
	}
	tool.Pack.EnvRefs["READER_TOKEN"] = "GEKTOR_TEST_MISSING_CREDENTIAL"
	t.Setenv("GEKTOR_TEST_MISSING_CREDENTIAL", "")
	if _, err := tool.Run(t.Context(), json.RawMessage(`{}`)); err == nil {
		t.Fatal("missing credential accepted")
	}
	if !errors.Is(redactCredentialError(context.Canceled, []string{"secret"}), context.Canceled) {
		t.Fatal("lost cancellation identity")
	}
}

func TestMCPCredentialsUseSameScopedEnvironment(t *testing.T) {
	t.Setenv(mcpHelperEnv, "1")
	t.Setenv("GEKTOR_MCP_ENV_TEST", "1")
	t.Setenv("TEST_READER_SECRET", "mcp-private-credential")
	t.Setenv("UNRELATED_API_KEY", "must-not-reach-adapter")
	list, client, err := startMCP(t.Context(), "reader", t.TempDir(), map[string]string{"READER_TOKEN": "TEST_READER_SECRET"}, os.Args[0])
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	out, err := list[0].Run(t.Context(), json.RawMessage(`{}`))
	if err != nil || out != "[redacted]" {
		t.Fatal(out, err)
	}
}
