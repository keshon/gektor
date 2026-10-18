package agent

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/keshon/tars/internal/llm"
)

// Patching a file the run has already written is how a file gets fixed.
// The rewrite guard used to refuse patch_file and patch_lines with "do NOT
// rewrite it - patch it for edits", which is advice to call the tool that
// had just been blocked. A live model obeyed that for nine steps and then
// abandoned the file tools for shell heredocs.
func TestAgent_PatchIsNotBlockedByTheRewriteGuard(t *testing.T) {
	for _, tool := range []string{"patch_file", "patch_lines"} {
		t.Run(tool, func(t *testing.T) {
			var runs int32
			client := &stubClient{responses: []llm.ChatResponse{
				patchCall("1", tool, "a.go"),
				patchCall("2", tool, "a.go"),
				patchCall("3", tool, "a.go"),
				{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
			}}
			a := New(Config{
				Client:     client,
				Tools:      NewRegistry(countingStub{name: tool, runs: &runs}),
				System:     "sys",
				SkipVerify: true,
			})
			if _, err := a.Run(context.Background(), "task"); err != nil {
				t.Fatalf("Run: %v", err)
			}
			if got := atomic.LoadInt32(&runs); got != 3 {
				t.Fatalf("%s ran %d times, want 3: a patch must never be refused", tool, got)
			}
			for _, m := range client.lastHistory {
				if m.Role == llm.RoleTool && strings.Contains(m.Content, "already wrote") {
					t.Fatalf("%s was refused as a rewrite:\n%s", tool, m.Content)
				}
			}
		})
	}
}

// The guard still does its job. It exists because a weak model rewrote
// package.json with identical content in a loop, and every "ok" reset the
// stuck counter.
func TestAgent_SecondWriteOfTheSamePathIsStillRefused(t *testing.T) {
	var runs int32
	client := &stubClient{responses: []llm.ChatResponse{
		patchCall("1", "write_file", "a.go"),
		patchCall("1", "write_file", "a.go"),
		{Message: llm.Message{Role: llm.RoleAssistant, Content: "done"}},
	}}
	a := New(Config{
		Client:     client,
		Tools:      NewRegistry(countingStub{name: "write_file", runs: &runs}),
		System:     "sys",
		SkipVerify: true,
	})
	if _, err := a.Run(context.Background(), "task"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got := atomic.LoadInt32(&runs); got != 1 {
		t.Fatalf("write_file ran %d times, want 1", got)
	}
	var refusal string
	for _, m := range client.lastHistory {
		if m.Role == llm.RoleTool && strings.Contains(m.Content, "already wrote") {
			refusal = m.Content
		}
	}
	if refusal == "" {
		t.Fatal("the rewrite guard must still refuse a second whole-file write")
	}
	// And the advice it gives must name a tool that works.
	if !strings.Contains(refusal, "patch_file") {
		t.Fatalf("refusal must point at the patching tools: %s", refusal)
	}
}

// A registry that names an unexpected patch tool still gets the exemption,
// because PatchingTools is configuration rather than a hardcoded list.
func TestRewritesFile_ReadsConfiguration(t *testing.T) {
	a := New(Config{MutatingTools: []string{"write_file", "splice"}, PatchingTools: []string{"splice"}})
	if a.rewritesFile("splice") {
		t.Fatal("a configured patching tool must not count as a rewrite")
	}
	if !a.rewritesFile("write_file") {
		t.Fatal("write_file is a rewrite")
	}
	if !a.writesFiles("splice") {
		t.Fatal("splice is still mutating and must count for the self-check")
	}
	// New defaults both sets, so a bare config still exempts the project's
	// own patch tools. A caller that overrides MutatingTools without
	// PatchingTools gets the defaults too, which is the safe direction:
	// they refuse rather than silently permit.
	bare := New(Config{})
	if bare.rewritesFile("patch_file") {
		t.Fatal("the default PatchingTools must exempt patch_file")
	}
	if !bare.rewritesFile("write_file") {
		t.Fatal("write_file is a rewrite under the defaults")
	}
	// An explicit empty set cannot be expressed through the constructor, so
	// check the predicate directly: with no exemptions, a patch counts as
	// a rewrite and gets refused, which is the conservative direction.
	if (&Agent{cfg: Config{MutatingTools: []string{"patch_file"}}}).rewritesFile("patch_file") != true {
		t.Fatal("with no PatchingTools, every mutator is treated as a rewrite")
	}
}

type countingStub struct {
	name string
	runs *int32
}

func (s countingStub) Name() string            { return s.name }
func (s countingStub) Description() string     { return "stub" }
func (s countingStub) Mode() ToolMode          { return Exclusive }
func (s countingStub) Schema() json.RawMessage { return json.RawMessage(`{}`) }
func (s countingStub) Run(context.Context, json.RawMessage) (string, error) {
	atomic.AddInt32(s.runs, 1)
	return "ok", nil
}

func patchCall(id, tool, path string) llm.ChatResponse {
	return llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{
		{ID: id, Name: tool, Arguments: json.RawMessage(`{"path":"` + path + `","content":"x"}`)}}}}
}
