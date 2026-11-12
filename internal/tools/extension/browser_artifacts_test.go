package extension

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/keshon/gektor/internal/llm"
	"github.com/keshon/gektor/internal/mcp"
	toolimages "github.com/keshon/gektor/internal/tools/images"
)

func browserArtifactFixture(t *testing.T, name, text string) string {
	t.Helper()
	root := t.TempDir()
	dir := filepath.Join(root, ".gektor", "browser")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestBrowserArtifactsDeliverCurrentBoundedSnapshot(t *testing.T) {
	root := browserArtifactFixture(t, "page-current.yml", "- heading \"Article fact: 42\"\n"+strings.Repeat("я", 20000))
	_ = os.WriteFile(filepath.Join(root, ".gektor", "browser", "page-old.yml"), []byte("stale unrelated content"), 0600)
	tool := &MCPTool{browserWorkspace: root, def: mcp.ToolDefinition{Name: "browser_navigate"}, client: contractMCP(func(_ context.Context, _ string, _ any, out any) error {
		data, _ := json.Marshal(map[string]any{"content": []any{map[string]string{"type": "text", "text": "### Page\n- Page URL: https://fixture.test\n### Snapshot\n- [Snapshot](.gektor\\browser\\page-current.yml)"}}})
		return json.Unmarshal(data, out)
	})}
	out, err := tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil || !strings.Contains(out, "Article fact: 42") || !strings.Contains(out, "truncated") || strings.Contains(out, "stale unrelated") || !utf8.ValidString(out) || len(out) > BrowserObservationLimit {
		t.Fatal(len(out), out, err)
	}
	// Generic MCP results cannot opt themselves into filesystem reads.
	tool.browserWorkspace = ""
	out, err = tool.Run(t.Context(), json.RawMessage(`{}`))
	if err != nil || strings.Contains(out, "Article fact: 42") {
		t.Fatal(out, err)
	}
}

func TestBrowserObservationsOmitEchoButKeepResultsAndErrors(t *testing.T) {
	root := browserArtifactFixture(t, "page-current.yml", "- button \"Save\" [ref=e1]")
	output := "### Result\nSaved successfully\n### Ran Playwright code\n```js\nawait page.click('Save');\n```\n### Page\n- Page URL: https://fixture.test\n### Errors\n- network failure\n- [Snapshot](.gektor/browser/page-current.yml)"
	out, _ := browserArtifacts(t.Context(), root, "browser_click", output, nil)
	if strings.Contains(out, "await page.click") || !strings.Contains(out, "Saved successfully") || !strings.Contains(out, "network failure") || !strings.Contains(out, "[ref=e1]") {
		t.Fatal(out)
	}
}

func TestBrowserArtifactsRejectEscapesAndMissingFiles(t *testing.T) {
	root := browserArtifactFixture(t, "page-current.yml", "legitimate")
	_ = os.WriteFile(filepath.Join(root, "page-secret.yml"), []byte("PRIVATE SECRET"), 0600)
	for _, path := range []string{".gektor/browser/../../page-secret.yml", ".gektor/browser/page-missing.yml", "file:///page-secret.yml", "https://site/page-secret.yml"} {
		out, _ := browserArtifacts(t.Context(), root, "browser_snapshot", "- [Snapshot]("+path+")", nil)
		if strings.Contains(out, "PRIVATE SECRET") {
			t.Fatal(out)
		}
		if strings.HasPrefix(path, ".gektor/") && !strings.Contains(out, "observation unavailable") {
			t.Fatal("failure was hidden", out)
		}
	}
	link := filepath.Join(root, ".gektor", "browser", "page-linked.yml")
	if err := os.Symlink(filepath.Join(root, "page-secret.yml"), link); err == nil {
		out, _ := browserArtifacts(t.Context(), root, "browser_snapshot", "- [Snapshot](.gektor/browser/page-linked.yml)", nil)
		if strings.Contains(out, "PRIVATE SECRET") || !strings.Contains(out, "unavailable") {
			t.Fatal(out)
		}
	}
}

func TestBrowserSavedScreenshotAttachesPixelsOnce(t *testing.T) {
	var pixels bytes.Buffer
	_ = png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 2, 2)))
	root := browserArtifactFixture(t, "capture.png", pixels.String())
	tool := &MCPTool{browserWorkspace: root, def: mcp.ToolDefinition{Name: "browser_take_screenshot"}, client: contractMCP(func(_ context.Context, _ string, _ any, out any) error {
		return json.Unmarshal([]byte(`{"content":[{"type":"text","text":"### Result\n- [Screenshot of viewport](.gektor/browser/capture.png)"}]}`), out)
	})}
	args := json.RawMessage(`{}`)
	out, err := tool.Run(toolimages.VisionContext(t.Context(), false), args)
	paths, _ := tool.Attach(args)
	if err != nil || len(paths) != 0 || !strings.Contains(out, "pixels were not delivered") {
		t.Fatal(out, paths, err)
	}
	out, err = tool.Run(toolimages.VisionContext(t.Context(), true), args)
	paths, _ = tool.Attach(args)
	if err != nil || len(paths) != 1 {
		t.Fatal(out, paths, err)
	}
	if err := llm.ValidateImage(paths[0]); err != nil {
		t.Fatal(err)
	}
	paths, _ = tool.Attach(args)
	if len(paths) != 0 {
		t.Fatal("replayed image", paths)
	}
}
