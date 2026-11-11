package roles

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/keshon/gektor/internal/agent"
	"github.com/keshon/gektor/internal/conversation"
	"github.com/keshon/gektor/internal/llm"
)

func TestInteractiveViewsScopedAttachmentAndRestoresAfterCompaction(t *testing.T) {
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	e := testEnv(t)
	e.Vision = true
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewNRGBA(image.Rect(0, 0, 4, 4))); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "clipboard.png")
	if err := os.WriteFile(path, data.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	var results []agent.ToolResult
	e.OnResult = func(r agent.ToolResult) { results = append(results, r) }
	e.Client = &seqStub{steps: []llm.ChatResponse{
		extensionCall("enable", "enable_capability", `{"id":"native_images"}`),
		extensionCall("list", "image_attachments", `{}`),
		extensionCall("view", "image_view", `{"path":"attachment:1","crop":{"x":0,"y":0,"width":2,"height":2},"width":12}`),
		extensionCall("inspect", "image_inspect", `{"paths":["attachment:1"]}`),
		extensionCall("unknown", "image_view", `{"path":"attachment:99"}`), extensionAnswer(),
	}}
	state := filepath.Join(t.TempDir(), "state.json")
	interactiveAgent, interactiveAgentErr := Interactive(e, "", state, nil, nil)
	if interactiveAgentErr != nil {
		t.Fatal(interactiveAgentErr)
	}
	if _, err := interactiveAgent.Run(t.Context(), "Inspect this picture", path); err != nil {
		t.Fatal(err)
	}
	if len(results) != 5 {
		t.Fatal(results)
	}
	for i, r := range results {
		if r.Failed != (i == 4) {
			t.Fatal("incorrect tool outcome", r)
		}
	}
	if !strings.Contains(results[1].Output, "attachment:1") || !strings.Contains(results[2].Output, `"view_width":12`) {
		t.Fatal(results)
	}
	if _, _, _, err := agent.CompactFile(state, 1); err != nil {
		t.Fatal(err)
	}
	saved, err := conversation.Load(state)
	if err != nil {
		t.Fatal(err)
	}
	results = nil
	e.Client = &seqStub{steps: []llm.ChatResponse{extensionCall("again", "image_view", `{"path":"attachment:1","width":8}`), extensionAnswer()}}
	interactiveAgent2, interactiveAgent2Err := Interactive(e, "", state, nil, nil)
	if interactiveAgent2Err != nil {
		t.Fatal(interactiveAgent2Err)
	}
	if _, err := interactiveAgent2.Resume(t.Context(), saved.Context(), "Look at the same image again"); err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Failed || !strings.Contains(results[0].Output, `"view_width":8`) {
		t.Fatal(results)
	}
}
