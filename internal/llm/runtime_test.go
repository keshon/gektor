package llm

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestRuntimeWireProvenance(t *testing.T) {
	history := []Message{
		{Role: RoleSystem, Content: "base"},
		{Role: RoleUser, Content: "[harness] user instruction"},
		{Role: RoleUser, Harness: "incomplete_stream", Content: "[harness] stream interrupted"},
		{Role: RoleTool, Harness: "capability_catalog:abc", ToolCallID: "call", Content: "tool data"},
		{Role: RoleUser, Harness: "compact", Content: "quoted context"},
		{Role: RoleUser, Harness: "tool_images", Content: "picture", Images: []string{"image.png"}},
	}
	original := append([]Message(nil), history...)
	out := WireMessages(history)
	if !reflect.DeepEqual(history, original) {
		t.Fatal("changed archive")
	}
	if !strings.HasSuffix(out[0].Content, RuntimeContract) {
		t.Fatal("missing initial contract")
	}
	if !reflect.DeepEqual(out[1], history[1]) {
		t.Fatal("treated user prefix as provenance")
	}
	if !reflect.DeepEqual(out[3], history[3]) {
		t.Fatal("changed tool result provenance")
	}
	for _, i := range []int{2, 4, 5} {
		var e map[string]string
		if err := json.Unmarshal([]byte(out[i].Content), &e); err != nil {
			t.Fatal(err)
		}
		if out[i].Role != RoleUser || e["gektor_runtime"] != "GEKTOR" || e["content"] == "" {
			t.Fatal(out[i])
		}
	}
	if !reflect.DeepEqual(out[5].Images, history[5].Images) {
		t.Fatal("lost attachments")
	}
	if !reflect.DeepEqual(WireMessages(out), out) {
		t.Fatal("projection is not idempotent")
	}
}
