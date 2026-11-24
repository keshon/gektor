package extension

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/keshon/gektor/internal/agent"
)

// Run against an empty local blog fixture, never a live publishing endpoint.
func TestLocalBlogEditingContract(t *testing.T) {
	endpoint := os.Getenv("GEKTOR_TEST_BLOG_URL")
	if endpoint == "" {
		t.Skip("set GEKTOR_TEST_BLOG_URL to an isolated loopback blog MCP")
	}
	if !strings.HasPrefix(endpoint, "http://127.0.0.1:") {
		t.Fatal("integration writes are restricted to an isolated loopback fixture")
	}
	tools, session, err := StartMCPHTTP(t.Context(), "blog", endpoint)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	byName := map[string]agent.Tool{}
	for _, tool := range tools {
		err = agent.ValidateToolSchema(tool.Schema())
		if err != nil {
			t.Fatalf("%s schema: %v", tool.Name(), err)
		}
		byName[tool.Name()] = tool
	}
	if !strings.Contains(session.Instructions(), "get_help") {
		t.Fatal("missing server instructions")
	}
	slug := fmt.Sprintf("adapter-fixture-%d", time.Now().UnixNano())
	call := func(name, args string) map[string]any {
		t.Helper()
		tool := byName["mcp__blog__"+name]
		if tool == nil {
			t.Fatalf("missing %s", name)
		}
		out, callErr := tool.Run(t.Context(), json.RawMessage(strings.ReplaceAll(args, "adapter-fixture", slug)))
		if callErr != nil {
			t.Fatalf("%s: %v", name, callErr)
		}
		var result map[string]any
		decodeErr := json.Unmarshal([]byte(out), &result)
		if decodeErr != nil {
			t.Fatalf("%s output is incomplete/duplicated JSON: %v", name, decodeErr)
		}
		return result
	}
	call("get_help", `{}`)
	result := call("create_article", `{"slug":"adapter-fixture","metadata":{"title":"Пример: без YAML","summary":"Проверка","category":"Заметки","date":"2026-11-24","sources":[{"name":"Источник","url":"https://example.com"}]},"body":"Одна точная фраза.\n"}`)
	args, err := json.Marshal(map[string]any{"slug": "adapter-fixture", "expected_revision": result["revision"], "changes": map[string]any{"tags": []string{"Тест"}}})
	if err != nil {
		t.Fatal(err)
	}
	result = call("update_metadata", string(args))
	if _, exists := result["body"]; exists {
		t.Fatal("metadata update returned article text")
	}
	args, err = json.Marshal(map[string]any{"slug": "adapter-fixture", "expected_revision": result["revision"], "old_text": "точная", "new_text": "новая"})
	if err != nil {
		t.Fatal(err)
	}
	call("replace_text", string(args))
	result = call("get_article", `{"slug":"adapter-fixture"}`)
	if result["text"] != "Одна новая фраза.\n" {
		t.Fatalf("unexpected text: %#v", result["text"])
	}
	result = call("review_draft", `{"slug":"adapter-fixture"}`)
	if result["valid"] != true || result["new_article"] != true {
		t.Fatal("invalid review")
	}
}
